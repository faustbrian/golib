package httpcorrelation

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	correlation "github.com/faustbrian/golib/pkg/correlation"
)

type edgeGenerator struct {
	values []string
	err    error
}

func (generator *edgeGenerator) New() (string, error) {
	if generator.err != nil {
		return "", generator.err
	}
	value := generator.values[0]
	generator.values = generator.values[1:]
	return value, nil
}

func TestHTTPAdapterConfigurationAndNilBoundaries(t *testing.T) {
	if _, err := New(nil, Options{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("New(nil) error = %v", err)
	}
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{})
	if _, err := New(factory, Options{Invalid: 99}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("invalid policy error = %v", err)
	}
	if _, err := New(factory, Options{Policy: correlation.Policy{MaxLength: -1}}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("invalid ID policy error = %v", err)
	}

	response := httptest.NewRecorder()
	(*Middleware)(nil).Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("nil middleware status = %d", response.Code)
	}
	middleware, _ := New(factory, Options{})
	response = httptest.NewRecorder()
	middleware.Wrap(nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("nil handler status = %d", response.Code)
	}
	if _, err := (*Middleware)(nil).Inject(httptest.NewRequest(http.MethodGet, "/", nil), correlation.Values{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("nil middleware Inject() error = %v", err)
	}
	if _, err := middleware.Inject(nil, correlation.Values{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("nil request Inject() error = %v", err)
	}
}

func TestHTTPAdapterReplacementGenerationAndNilHeader(t *testing.T) {
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{Generator: &edgeGenerator{values: []string{"fresh-flow", "fresh-request", "child"}}})
	middleware, _ := New(factory, Options{})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header[CorrelationHeader] = []string{"one", "two"}
	response := httptest.NewRecorder()
	middleware.Wrap(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		values, _ := correlation.FromContext(request.Context())
		if values.CorrelationID.String() != "fresh-flow" {
			t.Fatalf("replacement values = %#v", values)
		}
	})).ServeHTTP(response, request)

	outbound := httptest.NewRequest(http.MethodPost, "/", nil)
	outbound.Header = nil
	parent := correlation.Values{CorrelationID: "fresh-flow", RequestID: "fresh-request"}
	if _, err := middleware.Inject(outbound, parent); err != nil || outbound.Header.Get(RequestHeader) != "child" {
		t.Fatalf("nil-header Inject() = %v, %v", outbound.Header, err)
	}

	failingFactory, _ := correlation.NewFactory(correlation.FactoryOptions{Generator: &edgeGenerator{err: errors.New("entropy")}})
	failing, _ := New(failingFactory, Options{})
	response = httptest.NewRecorder()
	failing.Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("generation failure reached handler")
	})).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("generation failure status = %d", response.Code)
	}
}

func TestHTTPAdapterDoesNotTrustInboundByDefault(t *testing.T) {
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{Generator: &edgeGenerator{values: []string{"generated-flow", "generated-request"}}})
	middleware, _ := New(factory, Options{})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(CorrelationHeader, "spoofed-flow")
	request.Header.Set(RequestHeader, "spoofed-request")
	response := httptest.NewRecorder()
	middleware.Wrap(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		values, _ := correlation.FromContext(request.Context())
		if values.CorrelationID.String() != "generated-flow" || values.CausationID != "" {
			t.Fatalf("default trust values = %#v", values)
		}
	})).ServeHTTP(response, request)
}

func TestHeaderCarrierBoundsDuplicateCollectionBeforeCodecParsing(t *testing.T) {
	header := make(http.Header)
	header[CorrelationHeader] = make([]string, 4096)

	values := (headerCarrier{header: header}).Values(CorrelationHeader)
	if len(values) != 9 {
		t.Fatalf("collected %d values, want bounded rejection sentinel", len(values))
	}
}
