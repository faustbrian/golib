package authentication_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
)

type externalPrincipalContextKey struct{}

func TestFailureSupportsStableErrorsIsAndErrorsAs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind authentication.FailureKind
		want error
	}{
		{kind: authentication.FailureAbsent, want: authentication.ErrCredentialsAbsent},
		{kind: authentication.FailureInvalid, want: authentication.ErrCredentialsInvalid},
		{kind: authentication.FailureRejected, want: authentication.ErrCredentialsRejected},
		{kind: authentication.FailureUnavailable, want: authentication.ErrAuthenticationUnavailable},
		{kind: authentication.FailureAmbiguous, want: authentication.ErrAmbiguousCredentials},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(string(tt.kind), func(t *testing.T) {
			t.Parallel()

			failure := authentication.NewFailure(tt.kind)
			if !errors.Is(failure, tt.want) {
				t.Errorf("errors.Is(%v, %v) = false", failure, tt.want)
			}
			var typed *authentication.Failure
			if !errors.As(failure, &typed) {
				t.Fatalf("errors.As(%T) = false", failure)
			}
			if typed.Kind() != tt.kind {
				t.Errorf("Failure.Kind() = %q, want %q", typed.Kind(), tt.kind)
			}
		})
	}
}

func TestFailureDiagnosticsDoNotIncludeSensitiveCause(t *testing.T) {
	t.Parallel()

	failure := authentication.NewFailure(authentication.FailureRejected,
		authentication.WithFailureCause(errors.New("token super-secret-token rejected")),
	)
	if got := failure.Error(); contains(got, "super-secret-token") {
		t.Fatalf("Failure.Error() disclosed cause: %q", got)
	}
	if !errors.Is(failure, authentication.ErrCredentialsRejected) {
		t.Fatal("failure does not match ErrCredentialsRejected")
	}
	if !errors.Is(failure, errors.Unwrap(failure)) {
		t.Fatal("failure does not preserve its cause for errors.Is")
	}
}

func TestFailureNilAndUnknownValuesRemainSafe(t *testing.T) {
	t.Parallel()

	var failure *authentication.Failure
	if failure.Kind() != "" || failure.Error() != "authentication: failure" ||
		failure.Unwrap() != nil || failure.Is(authentication.ErrCredentialsInvalid) ||
		failure.Challenges() != nil {
		t.Fatalf("nil failure methods were not safe: %#v", failure)
	}
	unknown := authentication.NewFailure(authentication.FailureKind("future"))
	if unknown.Error() != "authentication: failure" || unknown.Is(errors.New("future")) {
		t.Fatalf("unknown failure = %q", unknown.Error())
	}
}

func TestChallengeCopiesParameters(t *testing.T) {
	t.Parallel()

	parameters := map[string]string{"realm": "api", "scope": "orders:read"}
	challenge, err := authentication.NewChallenge("Bearer", parameters)
	if err != nil {
		t.Fatalf("NewChallenge() error = %v", err)
	}
	parameters["realm"] = "changed"
	returned := challenge.Parameters()
	returned["scope"] = "changed"

	if challenge.Scheme() != "Bearer" {
		t.Errorf("Challenge.Scheme() = %q", challenge.Scheme())
	}
	want := map[string]string{"realm": "api", "scope": "orders:read"}
	if got := challenge.Parameters(); !reflect.DeepEqual(got, want) {
		t.Errorf("Challenge.Parameters() = %#v, want %#v", got, want)
	}
}

func TestChallengeRejectsInvalidProtocolData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		scheme string
		params map[string]string
	}{
		{name: "empty scheme", params: map[string]string{}},
		{name: "invalid scheme", scheme: "Bear er", params: map[string]string{}},
		{name: "invalid parameter name", scheme: "Bearer", params: map[string]string{"bad name": "value"}},
		{name: "control in value", scheme: "Bearer", params: map[string]string{"realm": "bad\r\nvalue"}},
		{name: "non-newline control in value", scheme: "Bearer", params: map[string]string{"realm": "bad\x01value"}},
		{name: "delete in value", scheme: "Bearer", params: map[string]string{"realm": "bad\x7fvalue"}},
		{name: "oversized scheme", scheme: strings.Repeat("a", authentication.MaxChallengeSchemeBytes+1)},
		{name: "oversized parameter name", scheme: "Bearer", params: map[string]string{strings.Repeat("a", authentication.MaxChallengeNameBytes+1): "value"}},
		{name: "oversized parameter value", scheme: "Bearer", params: map[string]string{"realm": strings.Repeat("a", authentication.MaxChallengeValueBytes+1)}},
		{name: "too many parameters", scheme: "Bearer", params: challengeParameters(17)},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := authentication.NewChallenge(tt.scheme, tt.params)
			if !errors.Is(err, authentication.ErrInvalidChallenge) {
				t.Fatalf("NewChallenge() error = %v, want ErrInvalidChallenge", err)
			}
		})
	}
}

func challengeParameters(count int) map[string]string {
	parameters := make(map[string]string, count)
	for index := range count {
		parameters[fmt.Sprintf("p%d", index)] = "value"
	}
	return parameters
}

func TestResultDistinguishesAuthenticatedAndAnonymous(t *testing.T) {
	t.Parallel()

	principal, err := authentication.NewPrincipal(authentication.PrincipalSpec{Subject: "subject", Method: "api_key"})
	if err != nil {
		t.Fatalf("NewPrincipal() error = %v", err)
	}
	authenticated, err := authentication.NewAuthenticatedResult(principal)
	if err != nil {
		t.Fatalf("NewAuthenticatedResult() error = %v", err)
	}
	if authenticated.State() != authentication.ResultAuthenticated {
		t.Errorf("authenticated State() = %q", authenticated.State())
	}
	if got, ok := authenticated.Principal(); !ok || got.Subject() != "subject" {
		t.Errorf("authenticated Principal() = (%v, %v)", got, ok)
	}

	anonymous := authentication.AnonymousResult()
	if anonymous.State() != authentication.ResultAnonymous {
		t.Errorf("anonymous State() = %q", anonymous.State())
	}
	if got, ok := anonymous.Principal(); ok || !got.IsAnonymous() {
		t.Errorf("anonymous Principal() = (%v, %v)", got, ok)
	}
	if _, err := authentication.NewAuthenticatedResult(authentication.AnonymousPrincipal()); !errors.Is(err, authentication.ErrInvalidPrincipal) {
		t.Fatalf("NewAuthenticatedResult(anonymous) error = %v", err)
	}
}

func TestContextCarriesPrincipalWithPrivateKey(t *testing.T) {
	t.Parallel()

	principal, err := authentication.NewPrincipal(authentication.PrincipalSpec{Subject: "subject", Method: "basic"})
	if err != nil {
		t.Fatalf("NewPrincipal() error = %v", err)
	}
	externalKey := externalPrincipalContextKey{}
	ctx := context.WithValue(context.Background(), externalKey, "collision")
	ctx = authentication.ContextWithPrincipal(ctx, principal)

	got, ok := authentication.PrincipalFromContext(ctx)
	if !ok || got.Subject() != "subject" {
		t.Fatalf("PrincipalFromContext() = (%v, %v)", got, ok)
	}
	if got := ctx.Value(externalKey); got != "collision" {
		t.Errorf("external context value = %v", got)
	}
	if _, ok := authentication.PrincipalFromContext(context.Background()); ok {
		t.Fatal("PrincipalFromContext(empty) found a principal")
	}
}
