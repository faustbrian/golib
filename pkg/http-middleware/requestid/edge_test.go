package requestid

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfigurationAndIdentifierBoundaries(t *testing.T) {
	t.Parallel()
	for _, policy := range []Policy{
		{Kind: "unknown"}, {Header: "bad header"}, {ResponseHeader: "bad:header"},
		{Header: strings.Repeat("a", 129)}, {MaxLength: -1}, {MaxLength: 1025},
		{Invalid: RejectInvalid + 1},
	} {
		_, err := New(policy)
		var configuration *ConfigError
		if !errors.As(err, &configuration) || !errors.Is(err, ErrInvalidPolicy) || configuration.Error() == "" {
			t.Fatalf("New(%+v) error = %v", policy, err)
		}
	}
	if value, ok := FromContext(context.Background(), Request); ok || value != "" {
		t.Fatalf("missing context = %q, %v", value, ok)
	}
	generated, err := randomIdentifier()
	if err != nil || len(generated) < 26 || !validIdentifier(generated, 128) {
		t.Fatalf("random identifier = %q, %v", generated, err)
	}
	for _, value := range []string{"", " value", "value ", "a\tb", "a\x7fb", "a\u0085b", "aéb", "toolong"} {
		if validIdentifier(value, 4) {
			t.Fatalf("validIdentifier(%q) = true", value)
		}
	}
}

func TestTrustedDefaultsAndInvalidGeneratedValue(t *testing.T) {
	t.Parallel()
	middleware, err := New(Policy{Kind: Correlation, TrustInbound: true})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Correlation-ID", "trusted")
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if value, ok := FromContext(r.Context(), Correlation); !ok || value != "trusted" {
			t.Fatalf("context = %q, %v", value, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, request)
	if got := recorder.Header().Get("X-Correlation-ID"); got != "trusted" {
		t.Fatalf("header = %q", got)
	}

	invalid, err := New(Policy{Generator: func() (string, error) { return " bad", nil }})
	if err != nil {
		t.Fatal(err)
	}
	recorder = httptest.NewRecorder()
	invalid(http.NotFoundHandler()).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", recorder.Code)
	}
}
