package secureheader_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/faustbrian/golib/pkg/http-middleware/secureheader"
)

func TestAPIDefaultsApplyBeforeDownstreamResponses(t *testing.T) {
	t.Parallel()

	middleware, err := secureheader.New(secureheader.APIDefaults())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadRequest) })).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Header().Get("X-Content-Type-Options") != "nosniff" || recorder.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("headers = %v", recorder.Header())
	}
}

func TestHSTSRequiresDeploymentAcknowledgement(t *testing.T) {
	t.Parallel()

	_, err := secureheader.New(secureheader.Policy{HSTS: "max-age=31536000"})
	if !errors.Is(err, secureheader.ErrInvalidPolicy) {
		t.Fatalf("New() error = %v", err)
	}
}

func TestHSTSAndFixedHeadersUseFieldSpecificGrammar(t *testing.T) {
	t.Parallel()

	for _, policy := range []secureheader.Policy{
		{HSTS: "max-age=invalid", AcknowledgeHSTS: true},
		{HSTS: "includeSubDomains", AcknowledgeHSTS: true},
		{XContentTypeOptions: "guess"},
		{FrameOptions: "ALLOW-FROM https://example.com"},
	} {
		if _, err := secureheader.New(policy); !errors.Is(err, secureheader.ErrInvalidPolicy) {
			t.Fatalf("New(%#v) error = %v", policy, err)
		}
	}
}

func TestConfiguredHeadersRejectResponseSplitting(t *testing.T) {
	t.Parallel()

	_, err := secureheader.New(secureheader.Policy{ContentSecurityPolicy: "default-src 'none'\r\nX-Evil: yes"})
	if !errors.Is(err, secureheader.ErrInvalidPolicy) {
		t.Fatalf("New() error = %v", err)
	}
}

func TestPreservePolicyKeepsDownstreamHeader(t *testing.T) {
	t.Parallel()

	policy := secureheader.APIDefaults()
	policy.Existing = secureheader.Preserve
	middleware, _ := secureheader.New(policy)
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Referrer-Policy", "same-origin")
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Header().Get("Referrer-Policy") != "same-origin" {
		t.Fatalf("headers = %v", recorder.Header())
	}
}
