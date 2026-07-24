package bearer_test

import (
	"context"
	"errors"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/bearer"
)

func TestAuthenticatorUsesCallbackValidator(t *testing.T) {
	t.Parallel()

	authenticator, err := bearer.New(bearer.ValidatorFunc(func(_ context.Context, token string) (authentication.Principal, error) {
		if token != "valid-token" {
			return authentication.Principal{}, authentication.NewFailure(authentication.FailureRejected)
		}
		return mustPrincipal(t, "service", "bearer"), nil
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := authenticator.Authenticate(context.Background(), authentication.NewBearerCredential("valid-token"))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	principal, ok := result.Principal()
	if !ok || principal.Subject() != "service" {
		t.Fatalf("Authenticate() principal = (%v, %v)", principal, ok)
	}
}

func TestAuthenticatorClassifiesValidationFailures(t *testing.T) {
	t.Parallel()

	dependencyError := errors.New("provider failed with token secret-token")
	authenticator, err := bearer.New(bearer.ValidatorFunc(func(_ context.Context, _ string) (authentication.Principal, error) {
		return authentication.Principal{}, dependencyError
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = authenticator.Authenticate(context.Background(), authentication.NewBearerCredential("secret-token"))
	if !errors.Is(err, authentication.ErrAuthenticationUnavailable) || !errors.Is(err, dependencyError) {
		t.Fatalf("Authenticate() error = %v, want unavailable wrapping dependency", err)
	}
	if contains(err.Error(), "secret-token") {
		t.Fatalf("Authenticate() disclosed token in %q", err)
	}
}

func TestAuthenticatorPreservesClassifiedRejection(t *testing.T) {
	t.Parallel()

	want := authentication.NewFailure(authentication.FailureRejected)
	authenticator, err := bearer.New(bearer.ValidatorFunc(func(context.Context, string) (authentication.Principal, error) {
		return authentication.Principal{}, want
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, got := authenticator.Authenticate(context.Background(), authentication.NewBearerCredential("token")); got != want {
		t.Fatalf("Authenticate() error = %v, want original failure", got)
	}
}

func TestAuthenticatorRejectsMalformedCredentialBeforeCallback(t *testing.T) {
	t.Parallel()

	called := false
	authenticator, err := bearer.New(bearer.ValidatorFunc(func(_ context.Context, _ string) (authentication.Principal, error) {
		called = true
		return authentication.Principal{}, nil
	}), bearer.WithMaxTokenBytes(8))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	credentials := []authentication.Credential{
		authentication.NewBearerCredential(""),
		authentication.NewBearerCredential("too-long-token"),
		authentication.NewBasicCredential("user", "password"),
	}
	for _, credential := range credentials {
		if _, err := authenticator.Authenticate(context.Background(), credential); !errors.Is(err, authentication.ErrCredentialsInvalid) {
			t.Errorf("Authenticate(%s) error = %v, want invalid", credential.Kind(), err)
		}
	}
	if called {
		t.Fatal("validator called for malformed credential")
	}
}

func TestAuthenticatorHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	called := false
	authenticator, err := bearer.New(bearer.ValidatorFunc(func(_ context.Context, _ string) (authentication.Principal, error) {
		called = true
		return authentication.Principal{}, nil
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = authenticator.Authenticate(ctx, authentication.NewBearerCredential("token"))
	if !errors.Is(err, authentication.ErrAuthenticationUnavailable) || !errors.Is(err, context.Canceled) {
		t.Fatalf("Authenticate() error = %v, want unavailable and canceled", err)
	}
	if called {
		t.Fatal("validator called after cancellation")
	}
}

func TestAuthenticatorRejectsInvalidConfigurationAndPrincipal(t *testing.T) {
	t.Parallel()

	if _, err := bearer.New(nil); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("New(nil) error = %v", err)
	}
	if _, err := bearer.New(bearer.ValidatorFunc(func(context.Context, string) (authentication.Principal, error) {
		return authentication.AnonymousPrincipal(), nil
	}), bearer.WithMaxTokenBytes(0)); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("New(max=0) error = %v", err)
	}

	authenticator, err := bearer.New(bearer.ValidatorFunc(func(context.Context, string) (authentication.Principal, error) {
		return authentication.AnonymousPrincipal(), nil
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := authenticator.Authenticate(context.Background(), authentication.NewBearerCredential("token")); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("Authenticate() error = %v, want unavailable", err)
	}
}

func mustPrincipal(t *testing.T, subject, method string) authentication.Principal {
	t.Helper()
	principal, err := authentication.NewPrincipal(authentication.PrincipalSpec{Subject: subject, Method: method})
	if err != nil {
		t.Fatalf("NewPrincipal() error = %v", err)
	}
	return principal
}

func contains(value, needle string) bool {
	for index := 0; index+len(needle) <= len(value); index++ {
		if value[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}
