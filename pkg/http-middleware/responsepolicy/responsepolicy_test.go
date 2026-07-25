package responsepolicy_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/faustbrian/golib/pkg/http-middleware/responsepolicy"
)

func TestNoStoreAppliesToEveryDownstreamStatus(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	responsepolicy.NoStore()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusFound) })).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("headers = %v", recorder.Header())
	}
}

func TestAdmissionRejectsUnsafeRetryAfter(t *testing.T) {
	t.Parallel()

	_, err := responsepolicy.Admission(responsepolicy.AdmissionPolicy{State: func(context.Context) responsepolicy.State { return responsepolicy.Maintenance }, RetryAfter: "1\r\nX-Evil: yes"})
	if !errors.Is(err, responsepolicy.ErrInvalidPolicy) {
		t.Fatalf("Admission() error = %v", err)
	}
}

func TestAdmissionStateShortCircuitsWithoutOwningHealthHandler(t *testing.T) {
	t.Parallel()

	middleware, err := responsepolicy.Admission(responsepolicy.AdmissionPolicy{
		State: func(context.Context) responsepolicy.State { return responsepolicy.Maintenance },
	})
	if err != nil {
		t.Fatalf("Admission() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("handler ran") })).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response = %d %v", recorder.Code, recorder.Header())
	}
}
