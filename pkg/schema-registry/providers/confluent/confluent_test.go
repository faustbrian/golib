package confluent_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	"github.com/faustbrian/golib/pkg/schema-registry/providers/confluent"
)

type canonicalizerFunc func(context.Context, schemaregistry.Definition) ([]byte, error)

func (fn canonicalizerFunc) Canonicalize(ctx context.Context, definition schemaregistry.Definition) ([]byte, error) {
	return fn(ctx, definition)
}

func TestProviderExposesConfluentSemanticsAndResolvesByGlobalID(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/schemas/ids/7" {
			t.Fatalf("request path = %q", request.URL.Path)
		}
		if request.Header.Get("Accept") != "application/vnd.schemaregistry.v1+json" {
			t.Fatalf("Accept = %q", request.Header.Get("Accept"))
		}
		if attempts.Add(1) == 1 {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writer.Header().Set("Content-Type", "application/vnd.schemaregistry.v1+json")
		_, _ = writer.Write([]byte(`{"schema":"\"string\"","schemaType":"AVRO"}`))
	}))
	defer server.Close()

	provider, err := confluent.New(confluent.Config{
		Endpoint:            server.URL,
		Scope:               "cluster-a",
		Transport:           http.DefaultTransport,
		AllowHTTPForTesting: true,
		RequestTimeout:      time.Second,
		MaxResponseBytes:    4096,
		MaxAttempts:         2,
		MaxConcurrent:       2,
		ReferenceLimits:     schemaregistry.GraphLimits{MaxSchemas: 8, MaxDepth: 8, MaxReferences: 16},
		Canonicalizers: map[schemaregistry.Format]schemaregistry.Canonicalizer{
			schemaregistry.FormatAvro: canonicalizerFunc(func(_ context.Context, definition schemaregistry.Definition) ([]byte, error) {
				return definition.Content, nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	capabilities := provider.Capabilities()
	if capabilities.Provider != confluent.ProviderName || capabilities.RegistrationCreationOutcome {
		t.Fatalf("Capabilities() = %+v", capabilities)
	}

	result, err := provider.Resolve(context.Background(), schemaregistry.ByProviderID(schemaregistry.ProviderID{
		Provider: confluent.ProviderName,
		Scope:    "cluster-a",
		Value:    "7",
	}))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if result.ID.Value != "7" || result.Schema.Definition().Format != schemaregistry.FormatAvro || attempts.Load() != 2 {
		t.Fatalf("Resolve() = %+v, attempts=%d", result, attempts.Load())
	}
}

func TestProviderResolvesBoundedReferencesAndPreservesEscapedSubjects(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.EscapedPath() {
		case "/subjects/team%2Forders/versions/1":
			_, _ = writer.Write([]byte(`{"subject":"team/orders","version":1,"id":9,"schema":"root","schemaType":"PROTOBUF","references":[{"name":"common.proto","subject":"common","version":2}]}`))
		case "/subjects/common/versions/2":
			_, _ = writer.Write([]byte(`{"subject":"common","version":2,"id":8,"schema":"leaf","schemaType":"PROTOBUF"}`))
		default:
			t.Fatalf("escaped request path = %q", request.URL.EscapedPath())
		}
	}))
	defer server.Close()
	provider := newTestProviderWithFormats(t, server.URL, map[schemaregistry.Format]schemaregistry.Canonicalizer{
		schemaregistry.FormatProtobuf: canonicalizerFunc(func(_ context.Context, definition schemaregistry.Definition) ([]byte, error) {
			return definition.Content, nil
		}),
	})

	result, err := provider.Resolve(context.Background(), schemaregistry.AtVersion(
		schemaregistry.Subject{Name: "team/orders"}, schemaregistry.Version{Number: 1},
	))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	references := result.Schema.Definition().References
	if len(references) != 1 || references[0].Name != "common.proto" || references[0].Fingerprint == (schemaregistry.Fingerprint{}) {
		t.Fatalf("Resolve() references = %+v", references)
	}
}

func TestProviderRejectsReferenceCycles(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"subject":"cycle","version":1,"id":1,"schema":"root","schemaType":"PROTOBUF","references":[{"name":"self.proto","subject":"cycle","version":1}]}`))
	}))
	defer server.Close()
	provider := newTestProviderWithFormats(t, server.URL, map[schemaregistry.Format]schemaregistry.Canonicalizer{
		schemaregistry.FormatProtobuf: canonicalizerFunc(func(_ context.Context, definition schemaregistry.Definition) ([]byte, error) {
			return definition.Content, nil
		}),
	})
	_, err := provider.Resolve(context.Background(), schemaregistry.AtVersion(
		schemaregistry.Subject{Name: "cycle"}, schemaregistry.Version{Number: 1},
	))
	if !errors.Is(err, schemaregistry.ErrReferenceCycle) {
		t.Fatalf("Resolve() error = %v, want ErrReferenceCycle", err)
	}
}

func TestCompatibilityRequiresMatchingConfiguredMode(t *testing.T) {
	t.Parallel()

	var checks atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/config/orders-value":
			_, _ = writer.Write([]byte(`{"compatibilityLevel":"FULL"}`))
		case "/compatibility/subjects/orders-value/versions/latest":
			checks.Add(1)
			_, _ = writer.Write([]byte(`{"is_compatible":true}`))
		default:
			t.Fatalf("request path = %q", request.URL.Path)
		}
	}))
	defer server.Close()
	provider := newTestProvider(t, server.URL)
	candidate := compileAvro(t)

	unsupported, err := provider.CheckCompatibility(context.Background(), schemaregistry.CompatibilityRequest{
		Subject: schemaregistry.Subject{Name: "orders-value"}, Candidate: candidate,
		Mode: schemaregistry.CompatibilityForward,
	})
	if err != nil || unsupported.Supported || checks.Load() != 0 {
		t.Fatalf("CheckCompatibility(mismatch) = (%+v, %v), checks=%d", unsupported, err, checks.Load())
	}
	compatible, err := provider.CheckCompatibility(context.Background(), schemaregistry.CompatibilityRequest{
		Subject: schemaregistry.Subject{Name: "orders-value"}, Candidate: candidate,
		Mode: schemaregistry.CompatibilityFull,
	})
	if err != nil || !compatible.Supported || !compatible.Compatible || checks.Load() != 1 {
		t.Fatalf("CheckCompatibility(match) = (%+v, %v), checks=%d", compatible, err, checks.Load())
	}
}

func TestProviderListsBoundedlyAndDeletesOnlyMatchingFingerprint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/subjects":
			if request.URL.Query().Get("subjectPrefix") != "orders-" ||
				request.URL.Query().Get("offset") != "0" || request.URL.Query().Get("limit") != "2" {
				t.Fatalf("list query = %q", request.URL.RawQuery)
			}
			_, _ = writer.Write([]byte(`["orders-a","orders-b"]`))
		case request.Method == http.MethodGet && request.URL.Path == "/subjects/orders-a/versions/1":
			_, _ = writer.Write([]byte(`{"subject":"orders-a","version":1,"id":7,"schema":"\"string\"","schemaType":"AVRO"}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/subjects/orders-a/versions/1":
			if request.URL.Query().Get("permanent") != "true" {
				t.Fatal("hard deletion omitted permanent=true")
			}
			_, _ = writer.Write([]byte(`1`))
		default:
			t.Fatalf("request = %s %s", request.Method, request.URL.String())
		}
	}))
	defer server.Close()
	provider := newTestProvider(t, server.URL)
	page, err := provider.List(context.Background(), schemaregistry.ListRequest{SubjectPrefix: "orders-", Limit: 1})
	if err != nil || len(page.Schemas) != 1 || page.Schemas[0].Subject.Name != "orders-a" ||
		page.Schemas[0].Lifecycle != schemaregistry.LifecycleUnknown || page.NextPageToken != "1" {
		t.Fatalf("List() = (%+v, %v)", page, err)
	}
	schema := compileAvro(t)
	_, err = provider.Delete(context.Background(), schemaregistry.DeleteRequest{
		Subject: schemaregistry.Subject{Name: "orders-a"}, Version: schemaregistry.Version{Number: 1},
		Policy: schemaregistry.DeletionPolicy{Mode: schemaregistry.DeleteHard, ExpectedFingerprint: schema.Fingerprint()},
	})
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestProviderDoesNotClaimCreationAfterRacyConfluentRegistration(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/subjects/orders-value":
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte(`{"error_code":40403,"message":"not found"}`))
		case "/subjects/orders-value/versions":
			_, _ = writer.Write([]byte(`{"id":7}`))
		default:
			t.Fatalf("request path = %q", request.URL.Path)
		}
	}))
	defer server.Close()
	provider := newTestProvider(t, server.URL)
	schema := compileAvro(t)

	result, err := provider.Register(context.Background(), schemaregistry.RegisterRequest{
		Subject: schemaregistry.Subject{Name: "orders-value"},
		Schema:  schema,
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if result.Outcome != schemaregistry.RegistrationUnknown || result.ID.Value != "7" {
		t.Fatalf("Register() = %+v, want unknown creation outcome", result)
	}
}

func TestClassicFramerMatchesConfluentWireFormatAndBounds(t *testing.T) {
	t.Parallel()

	framer, err := confluent.NewClassicFramer("cluster-a", 16)
	if err != nil {
		t.Fatalf("NewClassicFramer() error = %v", err)
	}
	framed, err := framer.Frame(context.Background(), schemaregistry.ProviderID{
		Provider: confluent.ProviderName,
		Scope:    "cluster-a",
		Value:    "42",
	}, []byte{0xaa, 0xbb})
	if err != nil {
		t.Fatalf("Frame() error = %v", err)
	}
	want := []byte{0, 0, 0, 0, 42, 0xaa, 0xbb}
	if string(framed) != string(want) {
		t.Fatalf("Frame() = %v, want %v", framed, want)
	}
	id, payload, err := framer.Unframe(context.Background(), framed)
	if err != nil || id.Value != "42" || string(payload) != string([]byte{0xaa, 0xbb}) {
		t.Fatalf("Unframe() = (%+v, %v, %v)", id, payload, err)
	}
	framed[5] = 0
	if payload[0] != 0xaa {
		t.Fatal("Unframe() payload aliases frame")
	}

	_, _, err = framer.Unframe(context.Background(), []byte{1, 0, 0, 0, 1})
	if !errors.Is(err, confluent.ErrInvalidFrame) {
		t.Fatalf("Unframe(bad magic) error = %v, want ErrInvalidFrame", err)
	}
}

func TestProtobufFramerMatchesConfluentMessageIndexEncoding(t *testing.T) {
	t.Parallel()

	framer, err := confluent.NewProtobufFramer("cluster-a", 16, 4)
	if err != nil {
		t.Fatalf("NewProtobufFramer() error = %v", err)
	}
	id := schemaregistry.ProviderID{Provider: confluent.ProviderName, Scope: "cluster-a", Value: "42"}
	framed, err := framer.FrameMessage(context.Background(), id, []int{1, 0}, []byte{0xaa})
	if err != nil {
		t.Fatalf("FrameMessage() error = %v", err)
	}
	want := []byte{0, 0, 0, 0, 42, 4, 2, 0, 0xaa}
	if string(framed) != string(want) {
		t.Fatalf("FrameMessage() = %v, want %v", framed, want)
	}
	gotID, indexes, payload, err := framer.UnframeMessage(context.Background(), framed)
	if err != nil || gotID != id || len(indexes) != 2 || indexes[0] != 1 || indexes[1] != 0 || string(payload) != string([]byte{0xaa}) {
		t.Fatalf("UnframeMessage() = (%+v, %v, %v, %v)", gotID, indexes, payload, err)
	}
	optimized, err := framer.FrameMessage(context.Background(), id, []int{0}, nil)
	if err != nil || len(optimized) != 6 || optimized[5] != 0 {
		t.Fatalf("FrameMessage([0]) = (%v, %v)", optimized, err)
	}
	_, _, _, err = framer.UnframeMessage(context.Background(), []byte{0, 0, 0, 0, 42, 3})
	if !errors.Is(err, confluent.ErrInvalidFrame) {
		t.Fatalf("UnframeMessage(truncated) error = %v, want ErrInvalidFrame", err)
	}
}

func newTestProvider(t *testing.T, endpoint string) *confluent.Provider {
	return newTestProviderWithFormats(t, endpoint, map[schemaregistry.Format]schemaregistry.Canonicalizer{
		schemaregistry.FormatAvro: canonicalizerFunc(func(_ context.Context, definition schemaregistry.Definition) ([]byte, error) {
			return definition.Content, nil
		}),
	})
}

func newTestProviderWithFormats(
	t *testing.T,
	endpoint string,
	canonicalizers map[schemaregistry.Format]schemaregistry.Canonicalizer,
) *confluent.Provider {
	t.Helper()
	provider, err := confluent.New(confluent.Config{
		Endpoint:            endpoint,
		Scope:               "cluster-a",
		Transport:           http.DefaultTransport,
		AllowHTTPForTesting: true,
		RequestTimeout:      time.Second,
		MaxResponseBytes:    4096,
		MaxAttempts:         1,
		MaxConcurrent:       2,
		ReferenceLimits:     schemaregistry.GraphLimits{MaxSchemas: 8, MaxDepth: 8, MaxReferences: 16},
		Canonicalizers:      canonicalizers,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return provider
}

func compileAvro(t *testing.T) schemaregistry.Schema {
	t.Helper()
	schema, err := schemaregistry.Compile(
		context.Background(),
		schemaregistry.Definition{Format: schemaregistry.FormatAvro, Content: []byte(`"string"`)},
		canonicalizerFunc(func(_ context.Context, definition schemaregistry.Definition) ([]byte, error) {
			return definition.Content, nil
		}),
	)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return schema
}
