package apikey_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/apikey"
)

func TestAuthenticatorUsesCallbackValidator(t *testing.T) {
	t.Parallel()

	authenticator, err := apikey.New(apikey.ValidatorFunc(func(_ context.Context, keyID, key string) (authentication.Principal, error) {
		if keyID != "primary" || key != "valid-key" {
			return authentication.Principal{}, authentication.NewFailure(authentication.FailureRejected)
		}
		return apiKeyPrincipal(t, "service"), nil
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := authenticator.Authenticate(context.Background(), authentication.NewAPIKeyCredential("primary", "valid-key"))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	principal, ok := result.Principal()
	if !ok || principal.Subject() != "service" || principal.Method() != "api_key" {
		t.Fatalf("Authenticate() principal = (%v, %v)", principal, ok)
	}
}

func TestAuthenticatorBoundsInputAndClassifiesFailures(t *testing.T) {
	t.Parallel()

	dependencyError := errors.New("provider included secret-key in its error")
	called := 0
	authenticator, err := apikey.New(apikey.ValidatorFunc(func(context.Context, string, string) (authentication.Principal, error) {
		called++
		return authentication.Principal{}, dependencyError
	}), apikey.WithMaxKeyIDBytes(4), apikey.WithMaxKeyBytes(8))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	invalid := []authentication.Credential{
		authentication.NewAPIKeyCredential("", "key"),
		authentication.NewAPIKeyCredential("long-id", "key"),
		authentication.NewAPIKeyCredential("id", ""),
		authentication.NewAPIKeyCredential("id", "long-key-value"),
		authentication.NewBearerCredential("key"),
	}
	for _, credential := range invalid {
		if _, err := authenticator.Authenticate(context.Background(), credential); !errors.Is(err, authentication.ErrCredentialsInvalid) {
			t.Errorf("Authenticate(%s) error = %v, want invalid", credential.Kind(), err)
		}
	}
	if called != 0 {
		t.Fatalf("validator calls for invalid input = %d", called)
	}

	_, err = authenticator.Authenticate(context.Background(), authentication.NewAPIKeyCredential("id", "secret"))
	if !errors.Is(err, authentication.ErrAuthenticationUnavailable) || !errors.Is(err, dependencyError) {
		t.Fatalf("Authenticate() error = %v, want unavailable and dependency", err)
	}
	if strings.Contains(err.Error(), "secret-key") {
		t.Fatalf("Authenticate() disclosed provider details in %q", err)
	}
}

func TestAuthenticatorHonorsCancellationAndProviderFailures(t *testing.T) {
	t.Parallel()

	called := false
	authenticator, err := apikey.New(apikey.ValidatorFunc(func(context.Context, string, string) (authentication.Principal, error) {
		called = true
		return authentication.Principal{}, authentication.NewFailure(authentication.FailureRejected)
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := authenticator.Authenticate(ctx, authentication.NewAPIKeyCredential("id", "key")); !errors.Is(err, context.Canceled) || !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("Authenticate(canceled) error = %v", err)
	}
	if called {
		t.Fatal("validator called after cancellation")
	}

	_, err = authenticator.Authenticate(context.Background(), authentication.NewAPIKeyCredential("id", "key"))
	if !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("Authenticate(rejected) error = %v", err)
	}
}

func TestAuthenticatorRejectsUnsafeConfigurationAndProviderIdentity(t *testing.T) {
	t.Parallel()

	if _, err := apikey.New(nil); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("New(nil) error = %v", err)
	}
	if _, err := apikey.New(apikey.ValidatorFunc(func(context.Context, string, string) (authentication.Principal, error) {
		return authentication.Principal{}, nil
	}), apikey.WithMaxKeyIDBytes(0)); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("New(invalid bound) error = %v", err)
	}

	authenticator, err := apikey.New(apikey.ValidatorFunc(func(context.Context, string, string) (authentication.Principal, error) {
		return authentication.AnonymousPrincipal(), nil
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := authenticator.Authenticate(context.Background(), authentication.NewAPIKeyCredential("id", "key")); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("Authenticate(anonymous) error = %v", err)
	}
}

func apiKeyPrincipal(t *testing.T, subject string) authentication.Principal {
	t.Helper()
	principal, err := authentication.NewPrincipal(authentication.PrincipalSpec{Subject: subject, Method: "api_key"})
	if err != nil {
		t.Fatalf("NewPrincipal() error = %v", err)
	}
	return principal
}
