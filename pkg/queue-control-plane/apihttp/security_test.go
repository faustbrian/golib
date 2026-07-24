package apihttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
)

func TestSecurityMiddlewareAddsDefensiveHeaders(t *testing.T) {
	t.Parallel()

	middleware, err := NewSecurityMiddleware(SecurityConfig{})
	if err != nil {
		t.Fatalf("NewSecurityMiddleware() error = %v", err)
	}
	called := 0
	handler := middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		called++
		writer.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	if response.Code != http.StatusNoContent || called != 1 {
		t.Fatalf("response = %d, calls = %d, want 204 and 1", response.Code, called)
	}
	for name, want := range map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "no-referrer",
		"Cache-Control":           "no-store",
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
	} {
		if got := response.Header().Get(name); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestSecurityMiddlewareEnforcesExactOriginAndPreflight(t *testing.T) {
	t.Parallel()

	middleware, err := NewSecurityMiddleware(SecurityConfig{
		AllowedOrigins:   []string{"https://control.example.test"},
		AllowCredentials: true,
	})
	if err != nil {
		t.Fatalf("NewSecurityMiddleware() error = %v", err)
	}
	called := 0
	handler := middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called++ }))

	rejected := httptest.NewRequest(http.MethodGet, "/", nil)
	rejected.Header.Set("Origin", "https://attacker.example.test")
	rejectedResponse := httptest.NewRecorder()
	handler.ServeHTTP(rejectedResponse, rejected)
	if rejectedResponse.Code != http.StatusForbidden || called != 0 {
		t.Fatalf("rejected origin = (%d, %d calls), want 403 and 0", rejectedResponse.Code, called)
	}

	preflight := httptest.NewRequest(http.MethodOptions, "/v1/commands", nil)
	preflight.Header.Set("Origin", "https://control.example.test")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodPost)
	preflight.Header.Set(
		"Access-Control-Request-Headers",
		"authorization, content-type, x-csrf-token, x-queue-control-key-id, x-queue-control-key",
	)
	preflightResponse := httptest.NewRecorder()
	handler.ServeHTTP(preflightResponse, preflight)
	if preflightResponse.Code != http.StatusNoContent || called != 0 {
		t.Fatalf("preflight = (%d, %d calls), want 204 and 0", preflightResponse.Code, called)
	}
	if preflightResponse.Header().Get("Access-Control-Allow-Origin") != "https://control.example.test" ||
		preflightResponse.Header().Get("Access-Control-Allow-Credentials") != "true" ||
		preflightResponse.Header().Get("Access-Control-Allow-Methods") != "GET, POST" ||
		preflightResponse.Header().Get("Access-Control-Allow-Headers") !=
			"authorization, content-type, x-csrf-token, x-queue-control-key-id, x-queue-control-key" {
		t.Fatalf("preflight headers = %v", preflightResponse.Header())
	}
}

func TestSecurityMiddlewareRejectsUnsupportedPreflight(t *testing.T) {
	t.Parallel()

	middleware, err := NewSecurityMiddleware(SecurityConfig{AllowedOrigins: []string{"https://control.example.test"}})
	if err != nil {
		t.Fatalf("NewSecurityMiddleware() error = %v", err)
	}
	tests := map[string]struct {
		method  string
		headers string
	}{
		"method":  {method: http.MethodDelete},
		"headers": {method: http.MethodPost, headers: "x-unbounded-header"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequest(http.MethodOptions, "/", nil)
			request.Header.Set("Origin", "https://control.example.test")
			request.Header.Set("Access-Control-Request-Method", tt.method)
			request.Header.Set("Access-Control-Request-Headers", tt.headers)
			response := httptest.NewRecorder()
			middleware(http.NotFoundHandler()).ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", response.Code)
			}
		})
	}
}

func TestSecurityMiddlewareProtectsCookieMutationsWithCSRF(t *testing.T) {
	t.Parallel()

	middleware, err := NewSecurityMiddleware(SecurityConfig{AllowedOrigins: []string{"https://control.example.test"}})
	if err != nil {
		t.Fatalf("NewSecurityMiddleware() error = %v", err)
	}
	tests := map[string]struct {
		method    string
		configure func(*http.Request)
		status    int
	}{
		"missing token": {
			configure: func(request *http.Request) {
				request.Header.Set("Origin", "https://control.example.test")
				request.AddCookie(&http.Cookie{Name: "session", Value: "opaque"})
			},
			status: http.StatusForbidden,
		},
		"empty token": {
			configure: func(request *http.Request) {
				request.Header.Set("Origin", "https://control.example.test")
				request.AddCookie(&http.Cookie{Name: "csrf_token", Value: ""})
			},
			status: http.StatusForbidden,
		},
		"mismatched token": {
			configure: func(request *http.Request) {
				request.Header.Set("Origin", "https://control.example.test")
				request.AddCookie(&http.Cookie{Name: "csrf_token", Value: "token-123"})
				request.Header.Set("X-CSRF-Token", "short")
			},
			status: http.StatusForbidden,
		},
		"valid token": {
			configure: func(request *http.Request) {
				request.Header.Set("Origin", "https://control.example.test")
				request.AddCookie(&http.Cookie{Name: "csrf_token", Value: "token-123"})
				request.Header.Set("X-CSRF-Token", "token-123")
			},
			status: http.StatusNoContent,
		},
		"bearer bypass": {
			configure: func(request *http.Request) {
				request.Header.Set("Authorization", "Bearer opaque")
				request.AddCookie(&http.Cookie{Name: "session", Value: "opaque"})
			},
			status: http.StatusNoContent,
		},
		"safe cookie request": {
			method: http.MethodGet,
			configure: func(request *http.Request) {
				request.AddCookie(&http.Cookie{Name: "session", Value: "opaque"})
			},
			status: http.StatusNoContent,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			method := tt.method
			if method == "" {
				method = http.MethodPost
			}
			request := httptest.NewRequest(method, "/", nil)
			tt.configure(request)
			response := httptest.NewRecorder()
			middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(http.StatusNoContent)
			})).ServeHTTP(response, request)
			if response.Code != tt.status {
				t.Fatalf("status = %d, want %d", response.Code, tt.status)
			}
		})
	}
}

func TestSecurityMiddlewareAppliesRateLimitByAuthenticatedSubject(t *testing.T) {
	t.Parallel()

	limiter := &rateLimiterStub{}
	middleware, err := NewSecurityMiddleware(SecurityConfig{RateLimiter: limiter})
	if err != nil {
		t.Fatalf("NewSecurityMiddleware() error = %v", err)
	}
	principal, err := authentication.NewPrincipal(authentication.PrincipalSpec{Subject: "operator-1", Method: "bearer"})
	if err != nil {
		t.Fatalf("NewPrincipal() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request = request.WithContext(authentication.ContextWithPrincipal(request.Context(), principal))
	response := httptest.NewRecorder()
	middleware(http.NotFoundHandler()).ServeHTTP(response, request)

	if response.Code != http.StatusTooManyRequests || limiter.key != "subject:operator-1" {
		t.Fatalf("rate limit = (%d, %q), want (429, subject:operator-1)", response.Code, limiter.key)
	}
	if response.Header().Get("Retry-After") != "1" {
		t.Fatalf("Retry-After = %q, want 1", response.Header().Get("Retry-After"))
	}
}

func TestSecurityMiddlewareUsesRemoteAddressForAnonymousRateLimit(t *testing.T) {
	t.Parallel()

	limiter := &rateLimiterStub{allow: true}
	middleware, err := NewSecurityMiddleware(SecurityConfig{RateLimiter: limiter})
	if err != nil {
		t.Fatalf("NewSecurityMiddleware() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "192.0.2.10:1234"
	response := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent || limiter.key != "address:192.0.2.10" {
		t.Fatalf("rate limit = (%d, %q), want (204, address:192.0.2.10)", response.Code, limiter.key)
	}
}

func TestRateLimitKeyHandlesUnsplitAndMissingRemoteAddress(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"local-socket": "address:local-socket",
		"":             "address:unknown",
	}
	for remote, want := range tests {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.RemoteAddr = remote
		if got := rateLimitKey(request); got != want {
			t.Fatalf("rateLimitKey(%q) = %q, want %q", remote, got, want)
		}
	}
}

func TestNewSecurityMiddlewareRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	var typedNil *rateLimiterStub
	tests := []SecurityConfig{
		{AllowedOrigins: []string{"not-an-origin"}},
		{AllowedOrigins: []string{"https://control.example.test/path"}},
		{RateLimiter: typedNil},
	}
	for _, config := range tests {
		if _, err := NewSecurityMiddleware(config); !errors.Is(err, ErrInvalidSecurityConfiguration) {
			t.Fatalf("NewSecurityMiddleware() error = %v, want ErrInvalidSecurityConfiguration", err)
		}
	}
}

type rateLimiterStub struct {
	allow bool
	key   string
}

func (s *rateLimiterStub) Allow(_ context.Context, key string) bool {
	s.key = key

	return s.allow
}
