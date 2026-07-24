package authhttp_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authhttp"
)

type extractorFunc func(*http.Request) (authentication.Credential, error)

func (f extractorFunc) Extract(request *http.Request) (authentication.Credential, error) {
	return f(request)
}

type middlewareAuthenticatorFunc func(context.Context, authentication.Credential) (authentication.Result, error)

func (f middlewareAuthenticatorFunc) Authenticate(ctx context.Context, credential authentication.Credential) (authentication.Result, error) {
	return f(ctx, credential)
}

func TestMiddlewareAuthenticatesWithoutReadingBodyOrWrappingWriter(t *testing.T) {
	t.Parallel()

	principal, err := authentication.NewPrincipal(authentication.PrincipalSpec{Subject: "service", Method: "bearer"})
	if err != nil {
		t.Fatalf("NewPrincipal() error = %v", err)
	}
	result, err := authentication.NewAuthenticatedResult(principal)
	if err != nil {
		t.Fatalf("NewAuthenticatedResult() error = %v", err)
	}
	body := &observedBody{Reader: strings.NewReader("request payload")}
	extractor := extractorFunc(func(request *http.Request) (authentication.Credential, error) {
		if request.Body != body {
			t.Fatal("extractor received a replaced body")
		}
		return authentication.NewBearerCredential("token"), nil
	})
	authenticator := middlewareAuthenticatorFunc(func(context.Context, authentication.Credential) (authentication.Result, error) {
		return result, nil
	})
	middleware, err := authhttp.NewMiddleware(extractor, authenticator)
	if err != nil {
		t.Fatalf("NewMiddleware() error = %v", err)
	}
	recorder := &interfaceRecorder{ResponseRecorder: httptest.NewRecorder()}
	request := httptest.NewRequest(http.MethodPost, "/", body)
	handler := middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if writer != recorder {
			t.Fatalf("response writer was wrapped as %T", writer)
		}
		if _, ok := writer.(http.Flusher); !ok {
			t.Fatal("optional Flusher interface was not preserved")
		}
		got, ok := authentication.PrincipalFromContext(request.Context())
		if !ok || got.Subject() != "service" {
			t.Fatalf("PrincipalFromContext() = (%v, %v)", got, ok)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))

	handler.ServeHTTP(recorder, request)
	if body.read {
		t.Fatal("middleware read the request body")
	}
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestMiddlewarePropagatesRequestCancellation(t *testing.T) {
	t.Parallel()

	extractor := extractorFunc(func(*http.Request) (authentication.Credential, error) {
		return authentication.NewBearerCredential("token"), nil
	})
	authenticator := middlewareAuthenticatorFunc(func(ctx context.Context, _ authentication.Credential) (authentication.Result, error) {
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("Authenticate() context error = %v", ctx.Err())
		}
		return authentication.Result{}, authentication.NewFailure(
			authentication.FailureUnavailable,
			authentication.WithFailureCause(ctx.Err()),
		)
	})
	middleware, err := authhttp.NewMiddleware(extractor, authenticator)
	if err != nil {
		t.Fatalf("NewMiddleware() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler called")
	})).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}

func TestMiddlewareFailsClosedWithChallengesAndRedaction(t *testing.T) {
	t.Parallel()

	challenge, err := authentication.NewChallenge("Bearer", map[string]string{"realm": "api"})
	if err != nil {
		t.Fatalf("NewChallenge() error = %v", err)
	}
	tests := []struct {
		name      string
		extract   error
		auth      error
		wantCode  int
		challenge bool
	}{
		{name: "absent", extract: authentication.NewFailure(authentication.FailureAbsent), wantCode: http.StatusUnauthorized, challenge: true},
		{name: "invalid", extract: authentication.NewFailure(authentication.FailureInvalid), wantCode: http.StatusUnauthorized, challenge: true},
		{name: "ambiguous", extract: authentication.NewFailure(authentication.FailureAmbiguous), wantCode: http.StatusUnauthorized, challenge: true},
		{name: "rejected", auth: authentication.NewFailure(authentication.FailureRejected, authentication.WithFailureCause(errors.New("secret-token"))), wantCode: http.StatusUnauthorized, challenge: true},
		{name: "unavailable", auth: authentication.NewFailure(authentication.FailureUnavailable, authentication.WithFailureCause(errors.New("secret-token"))), wantCode: http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			extractor := extractorFunc(func(*http.Request) (authentication.Credential, error) {
				if tt.extract != nil {
					return nil, tt.extract
				}
				return authentication.NewBearerCredential("secret-token"), nil
			})
			authenticator := middlewareAuthenticatorFunc(func(context.Context, authentication.Credential) (authentication.Result, error) {
				return authentication.Result{}, tt.auth
			})
			middleware, err := authhttp.NewMiddleware(extractor, authenticator, authhttp.WithChallenges(challenge))
			if err != nil {
				t.Fatalf("NewMiddleware() error = %v", err)
			}
			called := false
			recorder := httptest.NewRecorder()
			middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
			if called {
				t.Fatal("next handler called after authentication failure")
			}
			if recorder.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantCode)
			}
			if strings.Contains(recorder.Body.String(), "secret-token") {
				t.Fatalf("response disclosed credential: %q", recorder.Body.String())
			}
			values := recorder.Header().Values("WWW-Authenticate")
			if tt.challenge && (len(values) != 1 || values[0] != `Bearer realm="api"`) {
				t.Fatalf("WWW-Authenticate = %#v", values)
			}
			if !tt.challenge && len(values) != 0 {
				t.Fatalf("unexpected WWW-Authenticate = %#v", values)
			}
		})
	}
}

func TestOptionalMiddlewareAllowsOnlyAbsentCredentials(t *testing.T) {
	t.Parallel()

	challenge, err := authentication.NewChallenge("Basic", map[string]string{"realm": "api"})
	if err != nil {
		t.Fatalf("NewChallenge() error = %v", err)
	}
	tests := []struct {
		name     string
		extract  error
		wantNext bool
		wantCode int
	}{
		{name: "absent", extract: authentication.NewFailure(authentication.FailureAbsent), wantNext: true, wantCode: http.StatusNoContent},
		{name: "invalid", extract: authentication.NewFailure(authentication.FailureInvalid), wantCode: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			middleware, err := authhttp.NewMiddleware(
				extractorFunc(func(*http.Request) (authentication.Credential, error) { return nil, tt.extract }),
				middlewareAuthenticatorFunc(func(context.Context, authentication.Credential) (authentication.Result, error) {
					t.Fatal("authenticator called without credentials")
					return authentication.Result{}, nil
				}),
				authhttp.WithOptionalAnonymous(), authhttp.WithChallenges(challenge),
			)
			if err != nil {
				t.Fatalf("NewMiddleware() error = %v", err)
			}
			called := false
			recorder := httptest.NewRecorder()
			middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				called = true
				principal, ok := authentication.PrincipalFromContext(request.Context())
				if !ok || !principal.IsAnonymous() {
					t.Fatalf("anonymous principal = (%v, %v)", principal, ok)
				}
				writer.WriteHeader(http.StatusNoContent)
			})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
			if called != tt.wantNext || recorder.Code != tt.wantCode {
				t.Fatalf("called=%v status=%d", called, recorder.Code)
			}
		})
	}
}

func TestMiddlewareRejectsInvalidConfigurationAndResult(t *testing.T) {
	t.Parallel()

	authenticator := middlewareAuthenticatorFunc(func(context.Context, authentication.Credential) (authentication.Result, error) {
		return authentication.AnonymousResult(), nil
	})
	if _, err := authhttp.NewMiddleware(nil, authenticator); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("NewMiddleware(nil extractor) error = %v", err)
	}
	extractor := extractorFunc(func(*http.Request) (authentication.Credential, error) {
		return authentication.NewBearerCredential("token"), nil
	})
	if _, err := authhttp.NewMiddleware(extractor, nil); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("NewMiddleware(nil authenticator) error = %v", err)
	}
	var typedNil *structExtractor
	if _, err := authhttp.NewMiddleware(typedNil, authenticator); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("NewMiddleware(typed nil extractor) error = %v", err)
	}
	if _, err := authhttp.NewMiddleware(extractor, authenticator, authhttp.WithChallenges(authentication.Challenge{})); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("NewMiddleware(invalid challenge) error = %v", err)
	}
	middleware, err := authhttp.NewMiddleware(extractor, authenticator)
	if err != nil {
		t.Fatalf("NewMiddleware() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler called")
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}

func TestMiddlewareAcceptsStructDependenciesAndNilOption(t *testing.T) {
	t.Parallel()

	principal, err := authentication.NewPrincipal(authentication.PrincipalSpec{Subject: "service", Method: "bearer"})
	if err != nil {
		t.Fatalf("NewPrincipal() error = %v", err)
	}
	result, err := authentication.NewAuthenticatedResult(principal)
	if err != nil {
		t.Fatalf("NewAuthenticatedResult() error = %v", err)
	}
	middleware, err := authhttp.NewMiddleware(
		structExtractor{credential: authentication.NewBearerCredential("token")},
		structAuthenticator{result: result},
		nil,
	)
	if err != nil {
		t.Fatalf("NewMiddleware() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestMiddlewareDropsInvalidFailureChallenges(t *testing.T) {
	t.Parallel()

	extractor := extractorFunc(func(*http.Request) (authentication.Credential, error) {
		return authentication.NewBearerCredential("token"), nil
	})
	tests := []error{
		authentication.ErrCredentialsRejected,
		authentication.NewFailure(authentication.FailureRejected,
			authentication.WithChallenges(authentication.Challenge{})),
	}
	for _, failure := range tests {
		authenticator := middlewareAuthenticatorFunc(func(context.Context, authentication.Credential) (authentication.Result, error) {
			return authentication.Result{}, failure
		})
		middleware, err := authhttp.NewMiddleware(extractor, authenticator)
		if err != nil {
			t.Fatalf("NewMiddleware() error = %v", err)
		}
		recorder := httptest.NewRecorder()
		middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatal("next handler called")
		})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		if recorder.Code != http.StatusUnauthorized || len(recorder.Header().Values("WWW-Authenticate")) != 0 {
			t.Fatalf("response = status %d headers %#v", recorder.Code, recorder.Header())
		}
	}
}

func TestMiddlewareUsesChallengeFromFailure(t *testing.T) {
	t.Parallel()

	challenge, err := authentication.NewChallenge("Bearer", map[string]string{"error": "invalid_token"})
	if err != nil {
		t.Fatalf("NewChallenge() error = %v", err)
	}
	extractor := extractorFunc(func(*http.Request) (authentication.Credential, error) {
		return nil, authentication.NewFailure(authentication.FailureRejected,
			authentication.WithChallenges(challenge))
	})
	authenticator := structAuthenticator{}
	middleware, err := authhttp.NewMiddleware(extractor, authenticator)
	if err != nil {
		t.Fatalf("NewMiddleware() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler called")
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := recorder.Header().Get("WWW-Authenticate"); got != `Bearer error="invalid_token"` {
		t.Fatalf("WWW-Authenticate = %q", got)
	}
}

type structExtractor struct {
	credential authentication.Credential
}

func (e structExtractor) Extract(*http.Request) (authentication.Credential, error) {
	return e.credential, nil
}

type structAuthenticator struct {
	result authentication.Result
}

func (a structAuthenticator) Authenticate(context.Context, authentication.Credential) (authentication.Result, error) {
	return a.result, nil
}

type observedBody struct {
	io.Reader
	read bool
}

func (body *observedBody) Read(buffer []byte) (int, error) {
	body.read = true
	return body.Reader.Read(buffer)
}

func (body *observedBody) Close() error { return nil }

type interfaceRecorder struct{ *httptest.ResponseRecorder }

func (recorder *interfaceRecorder) Flush() { recorder.ResponseRecorder.Flush() }
