package httpcorrelation_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	correlation "github.com/faustbrian/golib/pkg/correlation"
	httpcorrelation "github.com/faustbrian/golib/pkg/correlation/http"
)

func FuzzInboundHeaders(fuzz *testing.F) {
	fuzz.Add("flow", "request")
	fuzz.Add("bad value", "control\n")
	fuzz.Fuzz(func(t *testing.T, correlationValue, requestValue string) {
		factory, err := correlation.NewFactory(correlation.FactoryOptions{})
		if err != nil {
			t.Fatal(err)
		}
		middleware, err := httpcorrelation.New(factory, httpcorrelation.Options{Invalid: httpcorrelation.RejectInvalid})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header[httpcorrelation.CorrelationHeader] = []string{correlationValue}
		request.Header[httpcorrelation.RequestHeader] = []string{requestValue}
		response := httptest.NewRecorder()
		middleware.Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(response, request)
		if response.Code != http.StatusOK && response.Code != http.StatusBadRequest {
			t.Fatalf("unexpected status %d", response.Code)
		}
	})
}
