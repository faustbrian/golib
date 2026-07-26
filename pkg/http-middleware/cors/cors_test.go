package cors_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/faustbrian/golib/pkg/http-middleware/cors"
)

func TestCredentialedWildcardOriginIsRejected(t *testing.T) {
	t.Parallel()

	for _, policy := range []cors.Policy{
		{AllowedOrigins: []string{"*"}, AllowCredentials: true},
		{AllowedOrigins: []string{"https://app.example"}, AllowedMethods: []string{"*"}, AllowCredentials: true},
		{AllowedOrigins: []string{"https://app.example"}, AllowedHeaders: []string{"*"}, AllowCredentials: true},
		{AllowedOrigins: []string{"https://app.example"}, ExposedHeaders: []string{"*"}, AllowCredentials: true},
	} {
		_, err := cors.New(policy)
		if !errors.Is(err, cors.ErrInvalidPolicy) {
			t.Fatalf("New(%#v) error = %v", policy, err)
		}
	}
}

func TestNonCredentialedWildcardMethodAndHeadersEchoPreflight(t *testing.T) {
	t.Parallel()

	middleware, _ := cors.New(cors.Policy{AllowedOrigins: []string{"*"}, AllowedMethods: []string{"*"}, AllowedHeaders: []string{"*"}})
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://app.example")
	req.Header.Set("Access-Control-Request-Method", "PATCH")
	req.Header.Set("Access-Control-Request-Headers", "X-Custom")
	recorder := httptest.NewRecorder()
	middleware(http.NotFoundHandler()).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent || recorder.Header().Get("Access-Control-Allow-Methods") != "PATCH" || recorder.Header().Get("Access-Control-Allow-Headers") != "X-Custom" {
		t.Fatalf("response = %d %v", recorder.Code, recorder.Header())
	}
}

func TestSimpleOriginResponseAndVary(t *testing.T) {
	t.Parallel()

	middleware, err := cors.New(cors.Policy{
		AllowedOrigins: []string{"https://EXAMPLE.com:443"},
		ExposedHeaders: []string{"X-Request-ID"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Vary", "Accept-Encoding")
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, req)

	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := recorder.Header().Values("Vary"); !containsTokens(got, "Origin", "Accept-Encoding") {
		t.Fatalf("Vary = %v", got)
	}
}

func TestPreflightShortCircuitsWithValidatedMethodAndHeaders(t *testing.T) {
	t.Parallel()

	middleware, _ := cors.New(cors.Policy{
		AllowedOrigins: []string{"https://app.example"},
		AllowedMethods: []string{http.MethodPost},
		AllowedHeaders: []string{"Content-Type"},
		MaxAgeSeconds:  600,
	})
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://app.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	called := false
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(recorder, req)

	if called || recorder.Code != http.StatusNoContent {
		t.Fatalf("called = %v, status = %d", called, recorder.Code)
	}
	if recorder.Header().Get("Access-Control-Max-Age") != "600" {
		t.Fatalf("headers = %v", recorder.Header())
	}
}

func TestPreflightRejectsConflictingSingularFields(t *testing.T) {
	t.Parallel()

	middleware, _ := cors.New(cors.Policy{AllowedOrigins: []string{"https://app.example"}, AllowedMethods: []string{http.MethodPost}})
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://app.example")
	req.Header["Access-Control-Request-Method"] = []string{"POST", "DELETE"}
	recorder := httptest.NewRecorder()
	middleware(http.NotFoundHandler()).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestPreflightMethodComparisonIsCaseSensitive(t *testing.T) {
	t.Parallel()
	middleware, err := cors.New(cors.Policy{
		AllowedOrigins: []string{"https://app.example"},
		AllowedMethods: []string{http.MethodPost},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodOptions, "/", nil)
	request.Header.Set("Origin", "https://app.example")
	request.Header.Set("Access-Control-Request-Method", "post")
	recorder := httptest.NewRecorder()
	middleware(http.NotFoundHandler()).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestDynamicOriginErrorsFailClosed(t *testing.T) {
	t.Parallel()

	middleware, _ := cors.New(cors.Policy{AllowOrigin: func(context.Context, string) (bool, error) {
		return false, errors.New("internal")
	}})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://app.example")
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(recorder, req)
	if recorder.Header().Get("Access-Control-Allow-Origin") != "" || recorder.Code != http.StatusNoContent {
		t.Fatalf("response = %d %v", recorder.Code, recorder.Header())
	}
	if !containsTokens(recorder.Header().Values("Vary"), "Origin") {
		t.Fatalf("Vary = %v", recorder.Header().Values("Vary"))
	}
}

func containsTokens(values []string, expected ...string) bool {
	seen := map[string]bool{}
	for _, value := range values {
		for _, token := range value {
			_ = token
		}
	}
	joined := http.Header{"Vary": values}.Get("Vary")
	for _, candidate := range expected {
		for _, part := range splitComma(joined) {
			if part == candidate {
				seen[candidate] = true
			}
		}
	}
	for _, candidate := range expected {
		if !seen[candidate] {
			return false
		}
	}
	return true
}

func splitComma(value string) []string {
	var result []string
	start := 0
	for index, char := range value {
		if char == ',' {
			result = append(result, trim(value[start:index]))
			start = index + 1
		}
	}
	return append(result, trim(value[start:]))
}
func trim(value string) string {
	for len(value) > 0 && value[0] == ' ' {
		value = value[1:]
	}
	for len(value) > 0 && value[len(value)-1] == ' ' {
		value = value[:len(value)-1]
	}
	return value
}
