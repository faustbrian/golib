package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authhttp"
	"github.com/faustbrian/golib/pkg/queue-control-plane/apihttp"
)

func TestAdministrativeHandlerRejectsInvalidComposition(t *testing.T) {
	t.Parallel()

	extractor, err := authhttp.NewExtractor(authhttp.BearerAuthorization())
	if err != nil {
		t.Fatalf("NewExtractor() error = %v", err)
	}
	authenticator := &authenticatorStub{}
	var typedNilLimiter *handlerRateLimiter
	tests := []struct {
		api           http.Handler
		extractor     authhttp.CredentialExtractor
		authenticator authentication.Authenticator
		security      apihttp.SecurityConfig
		wantErr       error
	}{
		{extractor: extractor, authenticator: authenticator, wantErr: ErrInvalidConfiguration},
		{api: http.NotFoundHandler(), authenticator: authenticator, wantErr: authentication.ErrInvalidConfiguration},
		{api: http.NotFoundHandler(), extractor: extractor, wantErr: authentication.ErrInvalidConfiguration},
		{
			api: http.NotFoundHandler(), extractor: extractor, authenticator: authenticator,
			security: apihttp.SecurityConfig{AllowedOrigins: []string{"invalid"}},
			wantErr:  apihttp.ErrInvalidSecurityConfiguration,
		},
		{
			api: http.NotFoundHandler(), extractor: extractor, authenticator: authenticator,
			security: apihttp.SecurityConfig{RateLimiter: typedNilLimiter},
			wantErr:  apihttp.ErrInvalidRateLimitConfiguration,
		},
	}
	for _, test := range tests {
		handler, err := NewAdministrativeHandler(
			test.api, test.extractor, test.authenticator, test.security,
		)
		if handler != nil || !errors.Is(err, test.wantErr) {
			t.Fatalf("NewAdministrativeHandler() = (%v, %v), want %v", handler, err, test.wantErr)
		}
	}
}

func TestAdministrativeHandlerRejectsInvalidCredentialsBeforeAPI(t *testing.T) {
	t.Parallel()

	extractor, err := authhttp.NewExtractor(authhttp.BearerAuthorization())
	if err != nil {
		t.Fatalf("NewExtractor() error = %v", err)
	}
	calls := 0
	handler, err := NewAdministrativeHandler(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }),
		extractor,
		&authenticatorStub{err: authentication.NewFailure(authentication.FailureRejected)},
		apihttp.SecurityConfig{AllowedOrigins: []string{"https://control.example.test"}},
	)
	if err != nil {
		t.Fatalf("NewAdministrativeHandler() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-1/workers", nil)
	request.Header.Set("Authorization", "Bearer rejected-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || calls != 0 ||
		response.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("response = %d, API calls = %d, headers = %v", response.Code, calls, response.Header())
	}

	preflight := httptest.NewRequest(http.MethodOptions, "/v1/tenants/tenant-1/workers", nil)
	preflight.Header.Set("Origin", "https://control.example.test")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodGet)
	preflightResponse := httptest.NewRecorder()
	handler.ServeHTTP(preflightResponse, preflight)
	if preflightResponse.Code != http.StatusNoContent || calls != 0 {
		t.Fatalf("preflight = %d, API calls = %d", preflightResponse.Code, calls)
	}
}

func TestAdministrativeHandlerAuthenticatesBearerAndAllowsAnonymousProbes(t *testing.T) {
	t.Parallel()

	principal, err := authentication.NewPrincipal(authentication.PrincipalSpec{
		Subject: "operator-1", Method: "bearer",
	})
	if err != nil {
		t.Fatalf("NewPrincipal() error = %v", err)
	}
	authenticator := &authenticatorStub{principal: principal}
	limiter := &handlerRateLimiter{allow: true}
	extractor, err := authhttp.NewExtractor(authhttp.BearerAuthorization())
	if err != nil {
		t.Fatalf("NewExtractor() error = %v", err)
	}
	handler, err := NewAdministrativeHandler(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			got, ok := authentication.PrincipalFromContext(request.Context())
			if request.URL.Path == "/health/live" {
				if !ok || !got.IsAnonymous() {
					t.Errorf("probe principal = (%v, %v), want anonymous", got, ok)
				}
			} else if !ok || got.Subject() != "operator-1" {
				t.Errorf("admin principal = (%v, %v)", got, ok)
			}
			writer.WriteHeader(http.StatusNoContent)
		}),
		extractor,
		authenticator,
		apihttp.SecurityConfig{RateLimiter: limiter},
	)
	if err != nil {
		t.Fatalf("NewAdministrativeHandler() error = %v", err)
	}

	probe := httptest.NewRecorder()
	handler.ServeHTTP(probe, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if probe.Code != http.StatusNoContent {
		t.Fatalf("probe status = %d", probe.Code)
	}
	adminRequest := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-1/workers", nil)
	adminRequest.Header.Set("Authorization", "Bearer valid-token")
	admin := httptest.NewRecorder()
	handler.ServeHTTP(admin, adminRequest)
	if admin.Code != http.StatusNoContent || authenticator.calls != 1 {
		t.Fatalf("admin status = %d, authentication calls = %d", admin.Code, authenticator.calls)
	}
	if limiter.key != "subject:operator-1" {
		t.Fatalf("rate limit key = %q, want authenticated subject", limiter.key)
	}
}

type handlerRateLimiter struct {
	allow bool
	key   string
}

func (limiter *handlerRateLimiter) Allow(_ context.Context, key string) bool {
	limiter.key = key

	return limiter.allow
}

type authenticatorStub struct {
	principal authentication.Principal
	err       error
	calls     int
}

func (authenticator *authenticatorStub) Authenticate(
	context.Context,
	authentication.Credential,
) (authentication.Result, error) {
	authenticator.calls++
	if authenticator.err != nil {
		return authentication.Result{}, authenticator.err
	}

	return authentication.NewAuthenticatedResult(authenticator.principal)
}
