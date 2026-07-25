package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestClientAssignsStableDistinctLogicalOperationIdentity(t *testing.T) {
	t.Parallel()

	var generated atomic.Int64
	generator := IdentifierGeneratorFunc(func(context.Context) (GeneratedIdentifier, error) {
		return GeneratedIdentifier{
			Value:       fmt.Sprintf("operation-%d", generated.Add(1)),
			EntropyBits: 128,
		}, nil
	})
	var mutex sync.Mutex
	var observed []OperationIdentity
	transport := TransportFunc(func(request *http.Request) (*http.Response, error) {
		identity, ok := OperationIdentityFromContext(request.Context())
		if !ok {
			t.Fatal("transport request has no operation identity")
		}
		mutex.Lock()
		observed = append(observed, identity)
		mutex.Unlock()
		if request.URL.Path == "/start" {
			return &http.Response{
				StatusCode: http.StatusTemporaryRedirect,
				Header:     http.Header{"Location": {"/finish"}},
				Body:       http.NoBody,
			}, nil
		}

		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody}, nil
	})
	client, err := New(Config{
		Transport:                  transport,
		OperationIdentityGenerator: generator,
	})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close client: %v", err)
		}
	}()

	for range 2 {
		request, err := http.NewRequest(http.MethodPost, "https://api.example.test/start", nil)
		if err != nil {
			t.Fatalf("construct request: %v", err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("execute request: %v", err)
		}
		if err := response.Body.Close(); err != nil {
			t.Fatalf("close response: %v", err)
		}
		if _, ok := OperationIdentityFromContext(request.Context()); ok {
			t.Fatal("client mutated caller request context")
		}
	}

	mutex.Lock()
	defer mutex.Unlock()
	if len(observed) != 4 {
		t.Fatalf("observed identities = %#v", observed)
	}
	if observed[0].ID != "operation-1" || observed[1] != observed[0] {
		t.Fatalf("first operation identities = %#v", observed[:2])
	}
	if observed[2].ID != "operation-2" || observed[3] != observed[2] || observed[2] == observed[0] {
		t.Fatalf("second operation identities = %#v", observed[2:])
	}
	if observed[0].Provenance != IdentityGenerated || observed[2].Provenance != IdentityGenerated {
		t.Fatalf("identity provenance = %#v", observed)
	}
	inspection := client.InspectPipeline()
	if len(inspection.Operation) == 0 || inspection.Operation[0].Name != "httpclient.operation-identity" {
		t.Fatalf("operation pipeline = %#v", inspection.Operation)
	}
}

func TestCallerCanSupplyValidatedOperationIdentity(t *testing.T) {
	t.Parallel()

	ctx, err := WithOperationIdentity(context.Background(), "caller-operation")
	if err != nil {
		t.Fatalf("attach operation identity: %v", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.test", nil)
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}
	client, err := New(Config{Transport: TransportFunc(func(request *http.Request) (*http.Response, error) {
		identity, ok := OperationIdentityFromContext(request.Context())
		if !ok || identity.ID != "caller-operation" || identity.Provenance != IdentityCaller {
			t.Fatalf("operation identity = %#v, %v", identity, ok)
		}

		return &http.Response{StatusCode: http.StatusNoContent}, nil
	})})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close client: %v", err)
		}
	}()
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response: %v", err)
	}
}

func TestOperationIdentityRejectsInvalidPolicyAndValues(t *testing.T) {
	t.Parallel()

	for _, entropy := range []int{0, 95, 513} {
		if _, err := NewRandomIdentifierGenerator(entropy); !errors.Is(err, ErrInvalidIdentifier) {
			t.Fatalf("entropy %d error = %v", entropy, err)
		}
	}
	for _, identifier := range []string{"", "contains space", strings.Repeat("a", maximumOperationIDLength+1), "nön-ascii"} {
		if _, err := WithOperationIdentity(context.Background(), identifier); !errors.Is(err, ErrInvalidOperationIdentity) {
			t.Fatalf("identity %q error = %v", identifier, err)
		}
	}
	var nilContext context.Context
	if _, err := WithOperationIdentity(nilContext, "valid"); !errors.Is(err, ErrInvalidOperationIdentity) {
		t.Fatalf("nil context error = %v", err)
	}
	if _, ok := OperationIdentityFromContext(nilContext); ok {
		t.Fatal("nil context returned operation identity")
	}
	var typedNilGenerator *identityTestGenerator
	if _, err := New(Config{OperationIdentityGenerator: typedNilGenerator}); !errors.Is(err, ErrInvalidConfig) || !errors.Is(err, ErrInvalidIdentifier) {
		t.Fatalf("typed-nil generator error = %v", err)
	}
}

func TestRandomIdentifierGeneratorHonorsEntropyErrorsAndCancellation(t *testing.T) {
	t.Parallel()

	generator, err := NewRandomIdentifierGenerator(97)
	if err != nil {
		t.Fatalf("construct generator: %v", err)
	}
	generated, err := generator.Generate(context.Background())
	if err != nil {
		t.Fatalf("generate identifier: %v", err)
	}
	if generated.EntropyBits != 104 || !validOperationID(generated.Value) {
		t.Fatalf("generated identifier = %#v", generated)
	}
	var nilContext context.Context
	if _, err := generator.Generate(nilContext); !errors.Is(err, ErrInvalidIdentifier) {
		t.Fatalf("nil generation context error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := generator.Generate(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled generation error = %v", err)
	}

	secretCause := errors.New("random source do-not-render")
	failing := randomIdentifierGenerator{bytes: 16, reader: errorReader{err: secretCause}}
	if _, err := failing.Generate(context.Background()); !errors.Is(err, secretCause) {
		t.Fatalf("random reader error = %v", err)
	}
	ctx, cancelAfterRead := context.WithCancel(context.Background())
	canceling := randomIdentifierGenerator{
		bytes: 16,
		reader: readerFunc(func(target []byte) (int, error) {
			clear(target)
			cancelAfterRead()

			return len(target), nil
		}),
	}
	if _, err := canceling.Generate(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("post-read cancellation error = %v", err)
	}
}

func TestOperationIdentityFailuresAreTypedAndSecretSafe(t *testing.T) {
	t.Parallel()

	secretCause := errors.New("identity generator leaked do-not-render")
	tests := []IdentifierGenerator{
		IdentifierGeneratorFunc(func(context.Context) (GeneratedIdentifier, error) {
			return GeneratedIdentifier{}, secretCause
		}),
		IdentifierGeneratorFunc(func(context.Context) (GeneratedIdentifier, error) {
			return GeneratedIdentifier{Value: "low-entropy", EntropyBits: 8}, nil
		}),
		IdentifierGeneratorFunc(func(context.Context) (GeneratedIdentifier, error) {
			return GeneratedIdentifier{Value: "invalid value", EntropyBits: 128}, nil
		}),
	}
	for _, generator := range tests {
		client, err := New(Config{
			OperationIdentityGenerator: generator,
			Transport: TransportFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("transport must not run")

				return nil, nil
			}),
		})
		if err != nil {
			t.Fatalf("construct client: %v", err)
		}
		request, err := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
		if err != nil {
			t.Fatalf("construct request: %v", err)
		}
		_, err = client.Do(request)
		var identityError *OperationIdentityError
		if !errors.As(err, &identityError) {
			t.Fatalf("operation identity error = %#v", err)
		}
		if strings.Contains(err.Error(), "do-not-render") {
			t.Fatalf("operation identity error rendered cause: %q", err)
		}
		if closeErr := client.Close(); closeErr != nil {
			t.Fatalf("close client: %v", closeErr)
		}
	}

	tampered := context.WithValue(context.Background(), operationIdentityContextKey{}, OperationIdentity{
		ID: "generated-looking", Provenance: IdentityGenerated,
	})
	request, err := http.NewRequestWithContext(tampered, http.MethodGet, "https://api.example.test", nil)
	if err != nil {
		t.Fatalf("construct tampered request: %v", err)
	}
	client, err := New(Config{Transport: TransportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("transport must not run")

		return nil, nil
	})})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close client: %v", err)
		}
	}()
	_, err = client.Do(request)
	if !errors.Is(err, ErrInvalidOperationIdentity) {
		t.Fatalf("tampered identity error = %v", err)
	}
}

type identityTestGenerator struct{}

func (*identityTestGenerator) Generate(context.Context) (GeneratedIdentifier, error) {
	return GeneratedIdentifier{}, nil
}

type errorReader struct{ err error }

func (reader errorReader) Read([]byte) (int, error) { return 0, reader.err }

type readerFunc func([]byte) (int, error)

func (function readerFunc) Read(target []byte) (int, error) { return function(target) }

var _ io.Reader = readerFunc(nil)
