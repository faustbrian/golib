package authentication_test

import (
	"context"
	"errors"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
)

type authenticatorFunc func(context.Context, authentication.Credential) (authentication.Result, error)

func (f authenticatorFunc) Authenticate(ctx context.Context, credential authentication.Credential) (authentication.Result, error) {
	return f(ctx, credential)
}

func TestCompositeFallsThroughOnlyRejectedAuthenticators(t *testing.T) {
	t.Parallel()

	principal, err := authentication.NewPrincipal(authentication.PrincipalSpec{Subject: "service", Method: "bearer"})
	if err != nil {
		t.Fatalf("NewPrincipal() error = %v", err)
	}
	wanted, err := authentication.NewAuthenticatedResult(principal)
	if err != nil {
		t.Fatalf("NewAuthenticatedResult() error = %v", err)
	}
	var calls []string
	composite, err := authentication.NewComposite([]authentication.Binding{
		{Kind: authentication.CredentialBearer, Authenticator: authenticatorFunc(func(context.Context, authentication.Credential) (authentication.Result, error) {
			calls = append(calls, "first")
			return authentication.Result{}, authentication.NewFailure(authentication.FailureRejected)
		})},
		{Kind: authentication.CredentialBearer, Authenticator: authenticatorFunc(func(context.Context, authentication.Credential) (authentication.Result, error) {
			calls = append(calls, "second")
			return wanted, nil
		})},
		{Kind: authentication.CredentialBearer, Authenticator: authenticatorFunc(func(context.Context, authentication.Credential) (authentication.Result, error) {
			calls = append(calls, "third")
			return authentication.Result{}, errors.New("must not run")
		})},
	})
	if err != nil {
		t.Fatalf("NewComposite() error = %v", err)
	}

	result, err := composite.Authenticate(context.Background(), authentication.NewBearerCredential("token"))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if got, ok := result.Principal(); !ok || got.Subject() != "service" {
		t.Fatalf("Authenticate() principal = (%v, %v)", got, ok)
	}
	if len(calls) != 2 || calls[0] != "first" || calls[1] != "second" {
		t.Fatalf("authenticator calls = %#v", calls)
	}
}

func TestCompositeStopsOnNonRejectedFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "invalid", err: authentication.NewFailure(authentication.FailureInvalid), want: authentication.ErrCredentialsInvalid},
		{name: "ambiguous", err: authentication.NewFailure(authentication.FailureAmbiguous), want: authentication.ErrAmbiguousCredentials},
		{name: "unavailable", err: authentication.NewFailure(authentication.FailureUnavailable), want: authentication.ErrAuthenticationUnavailable},
		{name: "unclassified", err: errors.New("provider failed"), want: authentication.ErrAuthenticationUnavailable},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fallbackCalled := false
			composite, err := authentication.NewComposite([]authentication.Binding{
				{Kind: authentication.CredentialAPIKey, Authenticator: authenticatorFunc(func(context.Context, authentication.Credential) (authentication.Result, error) {
					return authentication.Result{}, tt.err
				})},
				{Kind: authentication.CredentialAPIKey, Authenticator: authenticatorFunc(func(context.Context, authentication.Credential) (authentication.Result, error) {
					fallbackCalled = true
					return authentication.Result{}, nil
				})},
			})
			if err != nil {
				t.Fatalf("NewComposite() error = %v", err)
			}
			_, err = composite.Authenticate(context.Background(), authentication.NewAPIKeyCredential("id", "key"))
			if !errors.Is(err, tt.want) {
				t.Fatalf("Authenticate() error = %v, want %v", err, tt.want)
			}
			if fallbackCalled {
				t.Fatal("fallback called after terminal failure")
			}
		})
	}
}

func TestCompositeCombinesRejectedChallenges(t *testing.T) {
	t.Parallel()

	first, err := authentication.NewChallenge("Bearer", map[string]string{"realm": "first"})
	if err != nil {
		t.Fatalf("NewChallenge() error = %v", err)
	}
	second, err := authentication.NewChallenge("Bearer", map[string]string{"realm": "second"})
	if err != nil {
		t.Fatalf("NewChallenge() error = %v", err)
	}
	composite, err := authentication.NewComposite([]authentication.Binding{
		{Kind: authentication.CredentialBearer, Authenticator: rejecting(first)},
		{Kind: authentication.CredentialBearer, Authenticator: rejecting(second)},
	})
	if err != nil {
		t.Fatalf("NewComposite() error = %v", err)
	}

	_, err = composite.Authenticate(context.Background(), authentication.NewBearerCredential("token"))
	if !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("Authenticate() error = %v, want rejected", err)
	}
	var failure *authentication.Failure
	if !errors.As(err, &failure) {
		t.Fatalf("Authenticate() error type = %T", err)
	}
	challenges := failure.Challenges()
	if len(challenges) != 2 || challenges[0].Parameters()["realm"] != "first" || challenges[1].Parameters()["realm"] != "second" {
		t.Fatalf("challenges = %#v", challenges)
	}
}

func TestCompositeRejectsInvalidConfigurationAndResults(t *testing.T) {
	t.Parallel()

	if _, err := authentication.NewComposite(nil); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("NewComposite(nil) error = %v", err)
	}
	if _, err := authentication.NewComposite([]authentication.Binding{{Kind: authentication.CredentialBearer}}); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("NewComposite(nil authenticator) error = %v", err)
	}
	composite, err := authentication.NewComposite([]authentication.Binding{{
		Kind: authentication.CredentialBearer,
		Authenticator: authenticatorFunc(func(context.Context, authentication.Credential) (authentication.Result, error) {
			return authentication.AnonymousResult(), nil
		}),
	}})
	if err != nil {
		t.Fatalf("NewComposite() error = %v", err)
	}
	if _, err := composite.Authenticate(context.Background(), authentication.NewBearerCredential("token")); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("Authenticate(anonymous) error = %v", err)
	}
	if _, err := composite.Authenticate(context.Background(), authentication.NewBasicCredential("user", "password")); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("Authenticate(unbound kind) error = %v", err)
	}
	if _, err := composite.Authenticate(context.Background(), nil); !errors.Is(err, authentication.ErrCredentialsInvalid) {
		t.Fatalf("Authenticate(nil) error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := composite.Authenticate(canceled, authentication.NewBearerCredential("token")); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("Authenticate(canceled) error = %v", err)
	}
}

func TestCompositeTreatsBareRejectionAndAbsentAsClassified(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "bare rejection", err: authentication.ErrCredentialsRejected, want: authentication.ErrCredentialsRejected},
		{name: "absent", err: authentication.ErrCredentialsAbsent, want: authentication.ErrCredentialsAbsent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			composite, err := authentication.NewComposite([]authentication.Binding{{
				Kind: authentication.CredentialBearer,
				Authenticator: authenticatorFunc(func(context.Context, authentication.Credential) (authentication.Result, error) {
					return authentication.Result{}, tt.err
				}),
			}})
			if err != nil {
				t.Fatalf("NewComposite() error = %v", err)
			}
			_, err = composite.Authenticate(context.Background(), authentication.NewBearerCredential("token"))
			if !errors.Is(err, tt.want) {
				t.Fatalf("Authenticate() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func rejecting(challenge authentication.Challenge) authentication.Authenticator {
	return authenticatorFunc(func(context.Context, authentication.Credential) (authentication.Result, error) {
		return authentication.Result{}, authentication.NewFailure(authentication.FailureRejected,
			authentication.WithChallenges(challenge))
	})
}
