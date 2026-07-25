package responsepolicy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdmissionConfigurationAndRetryAfterGrammar(t *testing.T) {
	t.Parallel()
	for _, policy := range []AdmissionPolicy{{}, {State: readyState, Status: 399}, {State: readyState, Status: 600}, {State: readyState, RetryAfter: "86401"}, {State: readyState, RetryAfter: "bad\nvalue"}} {
		_, err := Admission(policy)
		var configuration *ConfigError
		if !errors.As(err, &configuration) || !errors.Is(err, ErrInvalidPolicy) || configuration.Error() == "" {
			t.Fatalf("Admission(%+v) error = %v", policy, err)
		}
	}
	for _, value := range []string{"0", "86400", "Sun, 06 Nov 1994 08:49:37 GMT"} {
		if !validRetryAfter(value) {
			t.Fatalf("validRetryAfter(%q) = false", value)
		}
	}
	for _, value := range []string{"-1", "999999999999999999999", "not-a-date"} {
		if validRetryAfter(value) {
			t.Fatalf("validRetryAfter(%q) = true", value)
		}
	}
}

func TestAdmissionReadyAndRetryResponse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		state State
		want  int
	}{{Ready, http.StatusNoContent}, {Maintenance, http.StatusTooManyRequests}, {State(99), http.StatusTooManyRequests}} {
		middleware, err := Admission(AdmissionPolicy{State: func(context.Context) State { return tc.state }, Status: http.StatusTooManyRequests, RetryAfter: "12"})
		if err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		if recorder.Code != tc.want {
			t.Fatalf("state %d status = %d", tc.state, recorder.Code)
		}
		if tc.state != Ready && recorder.Header().Get("Retry-After") != "12" {
			t.Fatalf("retry header = %q", recorder.Header().Get("Retry-After"))
		}
	}
}

func readyState(context.Context) State { return Ready }
