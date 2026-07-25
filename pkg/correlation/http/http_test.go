package httpcorrelation_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	correlation "github.com/faustbrian/golib/pkg/correlation"
	httpcorrelation "github.com/faustbrian/golib/pkg/correlation/http"
)

type generator struct {
	values []string
	index  int
}

func (generator *generator) New() (string, error) {
	value := generator.values[generator.index]
	generator.index++
	return value, nil
}

func TestMiddlewareTrustsOnlyDeclaredProxyAndCreatesRequestHop(t *testing.T) {
	factory, err := correlation.NewFactory(correlation.FactoryOptions{
		Generator: &generator{values: []string{"trusted-request", "new-flow", "untrusted-request"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	middleware, err := httpcorrelation.New(factory, httpcorrelation.Options{
		Trust: func(request *http.Request) bool { return request.RemoteAddr == "trusted-proxy:443" },
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := middleware.Wrap(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		values, ok := correlation.FromContext(request.Context())
		if !ok {
			t.Fatal("missing correlation context")
		}
		_, _ = writer.Write([]byte(values.CorrelationID.String() + "/" + values.RequestID.String() + "/" + values.CausationID.String()))
	}))

	trusted := httptest.NewRequest(http.MethodGet, "/", nil)
	trusted.RemoteAddr = "trusted-proxy:443"
	trusted.Header.Set(httpcorrelation.CorrelationHeader, "upstream-flow")
	trusted.Header.Set(httpcorrelation.RequestHeader, "upstream-request")
	trustedResponse := httptest.NewRecorder()
	handler.ServeHTTP(trustedResponse, trusted)
	if trustedResponse.Body.String() != "upstream-flow/trusted-request/upstream-request" {
		t.Fatalf("trusted response = %q", trustedResponse.Body.String())
	}

	untrusted := httptest.NewRequest(http.MethodGet, "/", nil)
	untrusted.RemoteAddr = "internet:443"
	untrusted.Header.Set(httpcorrelation.CorrelationHeader, "spoofed-flow")
	untrustedResponse := httptest.NewRecorder()
	handler.ServeHTTP(untrustedResponse, untrusted)
	if untrustedResponse.Body.String() != "new-flow/untrusted-request/" {
		t.Fatalf("untrusted response = %q", untrustedResponse.Body.String())
	}
}

func TestMiddlewareRejectsConflictingHeadersWhenConfigured(t *testing.T) {
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{
		Generator: &generator{values: []string{"unused"}},
	})
	middleware, err := httpcorrelation.New(factory, httpcorrelation.Options{Invalid: httpcorrelation.RejectInvalid})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header[httpcorrelation.CorrelationHeader] = []string{"one", "two"}
	response := httptest.NewRecorder()
	middleware.Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("invalid request reached handler")
	})).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestOutboundInjectionCreatesNextHopWithoutOverwriting(t *testing.T) {
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{
		Generator: &generator{values: []string{"outbound-request"}},
	})
	middleware, err := httpcorrelation.New(factory, httpcorrelation.Options{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://example.com/hook", nil)
	parent := correlation.Values{
		CorrelationID: correlation.MustCorrelationID("flow", correlation.Policy{}),
		RequestID:     correlation.MustRequestID("parent", correlation.Policy{}),
	}
	values, err := middleware.Inject(request, parent)
	if err != nil {
		t.Fatal(err)
	}
	if request.Header.Get(httpcorrelation.CorrelationHeader) != "flow" ||
		request.Header.Get(httpcorrelation.RequestHeader) != "outbound-request" ||
		request.Header.Get(httpcorrelation.CausationHeader) != "parent" ||
		values.RequestID.String() != "outbound-request" {
		t.Fatalf("headers = %v, values = %#v", request.Header, values)
	}
}
