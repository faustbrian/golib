package bearer_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/bearer"
)

func TestStaticRotatesBoundedBearerKeysAtomically(t *testing.T) {
	t.Parallel()

	authenticator, err := bearer.NewStatic([]bearer.Entry{
		{Token: "current-token", Principal: authentication.PrincipalSpec{Subject: "current"}},
		{Token: "previous-token", Principal: authentication.PrincipalSpec{Subject: "previous"}},
	})
	if err != nil {
		t.Fatalf("NewStatic() error = %v", err)
	}
	for token, subject := range map[string]string{"current-token": "current", "previous-token": "previous"} {
		result, authenticateErr := authenticator.Authenticate(context.Background(), authentication.NewBearerCredential(token))
		if authenticateErr != nil {
			t.Fatalf("Authenticate(%s) error = %v", subject, authenticateErr)
		}
		principal, ok := result.Principal()
		if !ok || principal.Subject() != subject || principal.Method() != "bearer" {
			t.Fatalf("Authenticate(%s) principal = (%v, %v)", subject, principal, ok)
		}
	}

	if err := authenticator.Replace([]bearer.Entry{{
		Token: "next-token", Principal: authentication.PrincipalSpec{Subject: "next"},
	}}); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	if _, err := authenticator.Authenticate(context.Background(), authentication.NewBearerCredential("current-token")); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("removed token error = %v, want rejected", err)
	}
	if _, err := authenticator.Authenticate(context.Background(), authentication.NewBearerCredential("next-token")); err != nil {
		t.Fatalf("next token error = %v", err)
	}
}

func TestStaticBearerRotationIsRaceSafe(t *testing.T) {
	t.Parallel()

	authenticator, err := bearer.NewStatic([]bearer.Entry{{
		Token: "token-a", Principal: authentication.PrincipalSpec{Subject: "a"},
	}})
	if err != nil {
		t.Fatalf("NewStatic() error = %v", err)
	}
	var group sync.WaitGroup
	for range 4 {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 100 {
				_, _ = authenticator.Authenticate(context.Background(), authentication.NewBearerCredential("token-a"))
			}
		}()
	}
	for index := range 100 {
		token, subject := "token-a", "a"
		if index%2 == 1 {
			token, subject = "token-b", "b"
		}
		if err := authenticator.Replace([]bearer.Entry{{
			Token: token, Principal: authentication.PrincipalSpec{Subject: subject},
		}}); err != nil {
			t.Fatalf("Replace() error = %v", err)
		}
	}
	group.Wait()
}

func TestStaticBearerRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	tests := [][]bearer.Entry{
		nil,
		make([]bearer.Entry, bearer.MaxEntries+1),
		{{Principal: authentication.PrincipalSpec{Subject: "service"}}},
		{{Token: "token"}},
		{
			{Token: "same", Principal: authentication.PrincipalSpec{Subject: "a"}},
			{Token: "same", Principal: authentication.PrincipalSpec{Subject: "b"}},
		},
		{{Token: "token", Principal: authentication.PrincipalSpec{Subject: "service", Method: "api_key"}}},
	}
	for _, entries := range tests {
		if _, err := bearer.NewStatic(entries); !errors.Is(err, authentication.ErrInvalidConfiguration) {
			t.Errorf("NewStatic(%d entries) error = %v, want invalid configuration", len(entries), err)
		}
	}
}

func TestStaticBearerAcceptsExactLimitsAndKeepsEarlierMatch(t *testing.T) {
	t.Parallel()

	entries := make([]bearer.Entry, bearer.MaxEntries)
	for index := range entries {
		entries[index] = bearer.Entry{
			Token:     fmt.Sprintf("token-%d", index),
			Principal: authentication.PrincipalSpec{Subject: fmt.Sprintf("service-%d", index)},
		}
	}
	authenticator, err := bearer.NewStatic(entries)
	if err != nil {
		t.Fatalf("NewStatic() exact entry limit error = %v", err)
	}
	result, err := authenticator.Authenticate(
		context.Background(),
		authentication.NewBearerCredential("token-0"),
	)
	if err != nil {
		t.Fatalf("Authenticate() first entry error = %v", err)
	}
	principal, ok := result.Principal()
	if !ok || principal.Subject() != "service-0" {
		t.Fatalf("Authenticate() first entry principal = (%v, %v)", principal, ok)
	}

	entries = append(entries, bearer.Entry{
		Token:     "overflow-token",
		Principal: authentication.PrincipalSpec{Subject: "overflow"},
	})
	if _, err := bearer.NewStatic(entries); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("NewStatic() oversized entry error = %v", err)
	}

	maxToken := strings.Repeat("x", 8*1024)
	authenticator, err = bearer.NewStatic([]bearer.Entry{{
		Token:     maxToken,
		Principal: authentication.PrincipalSpec{Subject: "bounded"},
	}})
	if err != nil {
		t.Fatalf("NewStatic() exact token limit error = %v", err)
	}
	if _, err := authenticator.Authenticate(
		context.Background(),
		authentication.NewBearerCredential(maxToken),
	); err != nil {
		t.Fatalf("Authenticate() exact token limit error = %v", err)
	}
}

func TestStaticBearerFailedReplacementKeepsPreviousSet(t *testing.T) {
	t.Parallel()

	authenticator, err := bearer.NewStatic([]bearer.Entry{{
		Token: "active", Principal: authentication.PrincipalSpec{Subject: "service"},
	}})
	if err != nil {
		t.Fatalf("NewStatic() error = %v", err)
	}
	if err := authenticator.Replace(nil); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("Replace(nil) error = %v, want invalid configuration", err)
	}
	if _, err := authenticator.Authenticate(
		context.Background(),
		authentication.NewBearerCredential("active"),
	); err != nil {
		t.Fatalf("Authenticate(previous set) error = %v", err)
	}
}

func TestStaticBearerRejectsInvalidCredentialsAndUnavailableState(t *testing.T) {
	t.Parallel()

	var zero bearer.Static
	if _, err := zero.Authenticate(context.Background(), authentication.NewBearerCredential("token")); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("zero-value Authenticate() error = %v, want unavailable", err)
	}

	authenticator, err := bearer.NewStatic([]bearer.Entry{{
		Token: "token", Principal: authentication.PrincipalSpec{Subject: "service"},
	}})
	if err != nil {
		t.Fatalf("NewStatic() error = %v", err)
	}
	for _, credential := range []authentication.Credential{
		authentication.NewBasicCredential("user", "password"),
		authentication.NewBearerCredential(""),
		authentication.NewBearerCredential(strings.Repeat("x", 8*1024+1)),
	} {
		if _, err := authenticator.Authenticate(context.Background(), credential); !errors.Is(err, authentication.ErrCredentialsInvalid) {
			t.Errorf("Authenticate(%s) error = %v, want invalid", credential.Kind(), err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := authenticator.Authenticate(ctx, authentication.NewBearerCredential("token")); !errors.Is(err, context.Canceled) {
		t.Fatalf("Authenticate(canceled) error = %v, want canceled", err)
	}
}

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

func TestAuthenticatorAcceptsExactConfiguredTokenLimit(t *testing.T) {
	t.Parallel()

	const token = "12345678"
	authenticator, err := bearer.New(bearer.ValidatorFunc(func(_ context.Context, got string) (authentication.Principal, error) {
		if got != token {
			t.Fatalf("ValidateBearer() token = %q, want %q", got, token)
		}
		return mustPrincipal(t, "service", "bearer"), nil
	}), bearer.WithMaxTokenBytes(len(token)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := authenticator.Authenticate(
		context.Background(),
		authentication.NewBearerCredential(token),
	); err != nil {
		t.Fatalf("Authenticate() exact token limit error = %v", err)
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

	authenticator, err = bearer.New(bearer.ValidatorFunc(func(context.Context, string) (authentication.Principal, error) {
		return mustPrincipal(t, "service", "api_key"), nil
	}))
	if err != nil {
		t.Fatalf("New(wrong method validator) error = %v", err)
	}
	if _, err := authenticator.Authenticate(context.Background(), authentication.NewBearerCredential("token")); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("Authenticate(wrong method) error = %v, want unavailable", err)
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
