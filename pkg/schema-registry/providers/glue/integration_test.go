//go:build integration

package glue_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awsglue "github.com/aws/aws-sdk-go-v2/service/glue"
	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	registryavro "github.com/faustbrian/golib/pkg/schema-registry/formats/avro"
	registryglue "github.com/faustbrian/golib/pkg/schema-registry/providers/glue"
)

const (
	integrationRegistry = "faithful-registry"
	integrationSchema   = "events"
	integrationScope    = "eu-north-1:000000000000:faithful-registry"
	integrationVersion1 = "11111111-1111-4111-8111-111111111111"
	integrationVersion2 = "22222222-2222-4222-8222-222222222222"
	integrationAvroV1   = `{"type":"record","name":"Event","fields":[{"name":"id","type":"string"}]}`
	integrationAvroV2   = `{"type":"record","name":"Event","fields":[{"name":"id","type":"string"},{"name":"source","type":"string","default":""}]}`
)

type smithyExchange struct {
	target     string
	request    map[string]any
	status     int
	response   string
	started    chan struct{}
	cancelOnly bool
}

type smithyService struct {
	t         *testing.T
	mu        sync.Mutex
	exchanges []smithyExchange
	next      int
}

func (service *smithyService) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	service.t.Helper()
	service.mu.Lock()
	if service.next >= len(service.exchanges) {
		service.mu.Unlock()
		service.t.Errorf("unexpected AWS Glue request target %q", request.Header.Get("X-Amz-Target"))
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
	exchange := service.exchanges[service.next]
	service.next++
	service.mu.Unlock()

	if request.Method != http.MethodPost || request.URL.Path != "/" {
		service.t.Errorf("AWS Glue request = %s %s", request.Method, request.URL.Path)
	}
	if request.Header.Get("Content-Type") != "application/x-amz-json-1.1" {
		service.t.Errorf("AWS Glue content type = %q", request.Header.Get("Content-Type"))
	}
	if request.Header.Get("X-Amz-Target") != exchange.target {
		service.t.Errorf("AWS Glue target = %q, want %q", request.Header.Get("X-Amz-Target"), exchange.target)
	}
	if !strings.HasPrefix(request.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
		service.t.Error("AWS Glue request was not SigV4 signed")
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, 200_000))
	if err != nil {
		service.t.Errorf("read AWS Glue request: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		service.t.Errorf("decode AWS Glue request: %v", err)
	} else if !reflect.DeepEqual(decoded, exchange.request) {
		service.t.Errorf("AWS Glue request = %#v, want %#v", decoded, exchange.request)
	}
	if exchange.started != nil {
		close(exchange.started)
	}
	if exchange.cancelOnly {
		<-request.Context().Done()
		return
	}
	writer.Header().Set("Content-Type", "application/x-amz-json-1.1")
	if exchange.status >= 400 {
		writer.Header().Set("X-Amzn-Errortype", smithyErrorType(exchange.response))
	}
	writer.WriteHeader(exchange.status)
	_, _ = io.WriteString(writer, exchange.response)
}

func (service *smithyService) assertComplete() {
	service.t.Helper()
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.next != len(service.exchanges) {
		service.t.Errorf("AWS Glue exchanges consumed = %d, want %d", service.next, len(service.exchanges))
	}
}

func TestProviderAgainstFaithfulAWSGlueService(t *testing.T) {
	exchanges := []smithyExchange{
		getVersionExchange(latestRequest(), integrationVersion1, integrationAvroV1, 1, "AVAILABLE"),
		getVersionExchange(map[string]any{"SchemaVersionId": integrationVersion1}, integrationVersion1, integrationAvroV1, 1, "AVAILABLE"),
		getByDefinitionExchange(integrationAvroV1, http.StatusOK, successByDefinition(integrationVersion1, 1)),
		getByDefinitionExchange(integrationAvroV2, http.StatusBadRequest, smithyError("EntityNotFoundException")),
		registerExchange(integrationAvroV2, http.StatusOK, `{"SchemaVersionId":"`+integrationVersion2+`","VersionNumber":2}`),
		getVersionExchange(versionRequest(2), integrationVersion2, integrationAvroV2, 2, "PENDING"),
		getVersionExchange(latestRequest(), integrationVersion2, integrationAvroV2, 2, "AVAILABLE"),
		getByDefinitionExchange(integrationAvroV2, http.StatusOK, successByDefinition(integrationVersion2, 2)),
	}
	provider := faithfulProvider(t, exchanges, time.Second, 1)
	ctx := context.Background()
	subject := schemaregistry.Subject{Registry: integrationRegistry, Name: integrationSchema}

	latest, err := provider.Resolve(ctx, schemaregistry.Latest(subject))
	if err != nil {
		t.Fatalf("resolve latest through SDK: %v", err)
	}
	if latest.ID.Value != integrationVersion1 || latest.Version.Number != 1 || latest.Lifecycle != schemaregistry.LifecycleAvailable {
		t.Fatalf("latest identity = %+v, version = %+v, lifecycle = %q", latest.ID, latest.Version, latest.Lifecycle)
	}
	byID, err := provider.Resolve(ctx, schemaregistry.ByProviderID(latest.ID))
	if err != nil {
		t.Fatalf("resolve by ID through SDK: %v", err)
	}
	if byID.ID != latest.ID || byID.Schema.Fingerprint() != latest.Schema.Fingerprint() {
		t.Fatalf("by-ID resolution = %+v fingerprint %s", byID.ID, byID.Schema.Fingerprint())
	}
	existing, err := provider.Register(ctx, schemaregistry.RegisterRequest{Subject: subject, Schema: latest.Schema})
	if err != nil || existing.Outcome != schemaregistry.RegistrationExisting || existing.ID != latest.ID {
		t.Fatalf("idempotent registration = %+v, %v", existing, err)
	}

	secondSchema, err := schemaregistry.Compile(ctx, schemaregistry.Definition{
		Format: schemaregistry.FormatAvro, Content: []byte(integrationAvroV2),
	}, registryavro.New(170_000))
	if err != nil {
		t.Fatalf("compile second schema: %v", err)
	}
	registered, err := provider.Register(ctx, schemaregistry.RegisterRequest{Subject: subject, Schema: secondSchema})
	if err != nil || registered.Outcome != schemaregistry.RegistrationUnknown || registered.ID.Value != integrationVersion2 || registered.Version.Number != 2 {
		t.Fatalf("new registration = %+v, %v", registered, err)
	}
	pending, err := provider.Resolve(ctx, schemaregistry.AtVersion(subject, schemaregistry.Version{Number: 2}))
	if err != nil || pending.Lifecycle != schemaregistry.LifecyclePending {
		t.Fatalf("pending version = %+v, %v", pending, err)
	}
	available, err := provider.Resolve(ctx, schemaregistry.Latest(subject))
	if err != nil || available.ID.Value != integrationVersion2 || available.Lifecycle != schemaregistry.LifecycleAvailable {
		t.Fatalf("available latest = %+v, %v", available, err)
	}
	duplicate, err := provider.Register(ctx, schemaregistry.RegisterRequest{Subject: subject, Schema: secondSchema})
	if err != nil || duplicate.Outcome != schemaregistry.RegistrationExisting || duplicate.ID.Value != integrationVersion2 {
		t.Fatalf("duplicate registration = %+v, %v", duplicate, err)
	}
}

func TestFaithfulAWSGlueFailureSemantics(t *testing.T) {
	t.Run("SDK retries throttling then resolves", func(t *testing.T) {
		provider := faithfulProvider(t, []smithyExchange{
			{target: "AWSGlue.GetSchemaVersion", request: latestRequest(), status: http.StatusBadRequest, response: `{"__type":"ThrottlingException","Message":"throttled"}`},
			getVersionExchange(latestRequest(), integrationVersion1, integrationAvroV1, 1, "AVAILABLE"),
		}, time.Second, 2)
		result, err := provider.Resolve(context.Background(), schemaregistry.Latest(integrationSubject()))
		if err != nil || result.ID.Value != integrationVersion1 {
			t.Fatalf("resolve after throttling = %+v, %v", result, err)
		}
	})

	t.Run("quota response is rejected", func(t *testing.T) {
		provider := faithfulProvider(t, []smithyExchange{
			getByDefinitionExchange(integrationAvroV2, http.StatusBadRequest, smithyError("EntityNotFoundException")),
			registerExchange(integrationAvroV2, http.StatusBadRequest, `{"__type":"ResourceNumberLimitExceededException","Message":"quota"}`),
		}, time.Second, 1)
		_, err := provider.Register(context.Background(), registerRequest(t, integrationAvroV2))
		if !errors.Is(err, schemaregistry.ErrRejected) {
			t.Fatalf("quota error = %v", err)
		}
	})

	t.Run("malformed response fails closed", func(t *testing.T) {
		provider := faithfulProvider(t, []smithyExchange{{
			target: "AWSGlue.GetSchemaVersion", request: latestRequest(), status: http.StatusOK, response: `{`,
		}}, time.Second, 1)
		_, err := provider.Resolve(context.Background(), schemaregistry.Latest(integrationSubject()))
		if !errors.Is(err, schemaregistry.ErrUnavailable) {
			t.Fatalf("malformed response error = %v", err)
		}
	})

	t.Run("caller cancellation reaches transport", func(t *testing.T) {
		started := make(chan struct{})
		provider := faithfulProvider(t, []smithyExchange{{
			target: "AWSGlue.GetSchemaVersion", request: latestRequest(), started: started, cancelOnly: true,
		}}, time.Second, 1)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := provider.Resolve(ctx, schemaregistry.Latest(integrationSubject()))
			done <- err
		}()
		<-started
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error = %v", err)
		}
	})

	t.Run("provider deadline reaches transport", func(t *testing.T) {
		started := make(chan struct{})
		provider := faithfulProvider(t, []smithyExchange{{
			target: "AWSGlue.GetSchemaVersion", request: latestRequest(), started: started, cancelOnly: true,
		}}, 25*time.Millisecond, 1)
		done := make(chan error, 1)
		go func() {
			_, err := provider.Resolve(context.Background(), schemaregistry.Latest(integrationSubject()))
			done <- err
		}()
		<-started
		if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("deadline error = %v", err)
		}
	})

	t.Run("ambiguous registration reconciles by exact definition", func(t *testing.T) {
		provider := faithfulProvider(t, []smithyExchange{
			getByDefinitionExchange(integrationAvroV2, http.StatusBadRequest, smithyError("EntityNotFoundException")),
			registerExchange(integrationAvroV2, http.StatusInternalServerError, `{"__type":"InternalServiceException","Message":"outcome unknown"}`),
			getByDefinitionExchange(integrationAvroV2, http.StatusOK, successByDefinition(integrationVersion2, 2)),
		}, time.Second, 1)
		request := registerRequest(t, integrationAvroV2)
		unknown, err := provider.Register(context.Background(), request)
		if unknown.Outcome != schemaregistry.RegistrationUnknown || !errors.Is(err, schemaregistry.ErrUnknownOutcome) {
			t.Fatalf("ambiguous registration = %+v, %v", unknown, err)
		}
		reconciled, err := provider.Register(context.Background(), request)
		if err != nil || reconciled.Outcome != schemaregistry.RegistrationExisting || reconciled.ID.Value != integrationVersion2 {
			t.Fatalf("reconciled registration = %+v, %v", reconciled, err)
		}
	})
}

func faithfulProvider(t *testing.T, exchanges []smithyExchange, timeout time.Duration, maxAttempts int) *registryglue.Provider {
	t.Helper()
	service := &smithyService{t: t, exchanges: exchanges}
	server := httptest.NewServer(service)
	t.Cleanup(func() {
		server.Close()
		service.assertComplete()
	})
	client := awsglue.NewFromConfig(aws.Config{
		Region:      "eu-north-1",
		Credentials: credentials.NewStaticCredentialsProvider("faithful-access-key", "faithful-secret-key", ""),
		HTTPClient:  server.Client(),
	}, func(options *awsglue.Options) {
		options.BaseEndpoint = aws.String(server.URL)
		options.Retryer = retry.NewStandard(func(options *retry.StandardOptions) {
			options.MaxAttempts = maxAttempts
			options.Backoff = retry.BackoffDelayerFunc(func(int, error) (time.Duration, error) { return 0, nil })
		})
	})
	provider, err := registryglue.New(registryglue.Config{
		API: client, Scope: integrationScope, RequestTimeout: timeout, MaxConcurrent: 4,
		Canonicalizers: map[schemaregistry.Format]schemaregistry.Canonicalizer{
			schemaregistry.FormatAvro: registryavro.New(170_000),
		},
	})
	if err != nil {
		t.Fatalf("construct provider: %v", err)
	}
	return provider
}

func latestRequest() map[string]any {
	return map[string]any{
		"SchemaId":            map[string]any{"RegistryName": integrationRegistry, "SchemaName": integrationSchema},
		"SchemaVersionNumber": map[string]any{"LatestVersion": true},
	}
}

func versionRequest(version int) map[string]any {
	return map[string]any{
		"SchemaId":            map[string]any{"RegistryName": integrationRegistry, "SchemaName": integrationSchema},
		"SchemaVersionNumber": map[string]any{"VersionNumber": float64(version)},
	}
}

func getVersionExchange(request map[string]any, id, definition string, version int, status string) smithyExchange {
	return smithyExchange{
		target: "AWSGlue.GetSchemaVersion", request: request, status: http.StatusOK,
		response: `{"DataFormat":"AVRO","SchemaDefinition":` + quoteJSON(definition) + `,"SchemaVersionId":"` + id + `","Status":"` + status + `","VersionNumber":` + string(rune('0'+version)) + `}`,
	}
}

func getByDefinitionExchange(definition string, status int, response string) smithyExchange {
	return smithyExchange{
		target: "AWSGlue.GetSchemaByDefinition",
		request: map[string]any{
			"SchemaDefinition": definition,
			"SchemaId":         map[string]any{"RegistryName": integrationRegistry, "SchemaName": integrationSchema},
		},
		status: status, response: response,
	}
}

func registerExchange(definition string, status int, response string) smithyExchange {
	return smithyExchange{
		target: "AWSGlue.RegisterSchemaVersion",
		request: map[string]any{
			"SchemaDefinition": definition,
			"SchemaId":         map[string]any{"RegistryName": integrationRegistry, "SchemaName": integrationSchema},
		},
		status: status, response: response,
	}
}

func smithyError(errorType string) string {
	return `{"__type":"` + errorType + `","Message":"faithful service error"}`
}

func smithyErrorType(response string) string {
	var value struct {
		Type string `json:"__type"`
	}
	_ = json.Unmarshal([]byte(response), &value)
	return value.Type
}

func successByDefinition(id string, version int) string {
	return `{"SchemaVersionId":"` + id + `","Status":"AVAILABLE","VersionNumber":` + string(rune('0'+version)) + `}`
}

func quoteJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func integrationSubject() schemaregistry.Subject {
	return schemaregistry.Subject{Registry: integrationRegistry, Name: integrationSchema}
}

func registerRequest(t *testing.T, definition string) schemaregistry.RegisterRequest {
	t.Helper()
	schema, err := schemaregistry.Compile(context.Background(), schemaregistry.Definition{
		Format: schemaregistry.FormatAvro, Content: []byte(definition),
	}, registryavro.New(170_000))
	if err != nil {
		t.Fatalf("compile registration schema: %v", err)
	}
	return schemaregistry.RegisterRequest{Subject: integrationSubject(), Schema: schema}
}
