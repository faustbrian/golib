package secureheader

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPolicyValidationTruthTable(t *testing.T) {
	t.Parallel()
	for _, policy := range []Policy{
		{Existing: Preserve + 1}, {HSTS: "max-age=1"}, {HSTS: "unknown", AcknowledgeHSTS: true},
		{XContentTypeOptions: "sniff"}, {FrameOptions: "ALLOW-FROM example"}, {ReferrerPolicy: "sometimes"},
		{PermissionsPolicy: "bad\nvalue"}, {ContentSecurityPolicy: strings.Repeat("x", 4097)},
	} {
		_, err := New(policy)
		var configuration *ConfigError
		if !errors.As(err, &configuration) || !errors.Is(err, ErrInvalidPolicy) || configuration.Error() == "" {
			t.Fatalf("New(%+v) error = %v", policy, err)
		}
	}
	for _, value := range []string{"max-age=0", "MAX-AGE=315360000; includeSubDomains; preload", "max-age=1;preload"} {
		if !validHSTS(value) {
			t.Fatalf("validHSTS(%q) = false", value)
		}
	}
	for _, value := range []string{"", ";", "max-age", "max-age=x", "max-age=315360001", "max-age=1; max-age=2", "includeSubDomains=x", "preload=x"} {
		if validHSTS(value) {
			t.Fatalf("validHSTS(%q) = true", value)
		}
	}
	for _, value := range []string{"no-referrer-when-downgrade", "origin", "origin-when-cross-origin", "same-origin", "strict-origin", "strict-origin-when-cross-origin", "unsafe-url"} {
		if !validReferrerPolicy(value) {
			t.Fatalf("validReferrerPolicy(%q) = false", value)
		}
	}
}

func TestReplaceReassertsHeadersAtCommit(t *testing.T) {
	t.Parallel()
	middleware, err := New(Policy{XContentTypeOptions: "nosniff", Existing: Replace})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Content-Type-Options", "changed")
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("header = %q", got)
	}
}
