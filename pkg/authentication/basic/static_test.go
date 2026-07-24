package basic_test

import (
	"context"
	"errors"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/basic"
)

func TestStaticAuthenticatesConfiguredBasicCredential(t *testing.T) {
	t.Parallel()

	authenticator, err := basic.NewStatic([]basic.Entry{{
		Username: "service",
		Password: "correct horse battery staple",
		Principal: authentication.PrincipalSpec{
			Subject:   "service-1",
			Issuer:    "local",
			Audiences: []string{"orders"},
		},
	}})
	if err != nil {
		t.Fatalf("NewStatic() error = %v", err)
	}

	result, err := authenticator.Authenticate(context.Background(),
		authentication.NewBasicCredential("service", "correct horse battery staple"),
	)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	principal, ok := result.Principal()
	if !ok || principal.Subject() != "service-1" || principal.Method() != "basic" {
		t.Fatalf("Authenticate() principal = (%v, %v)", principal, ok)
	}
}

func TestStaticRejectsUnknownOrMalformedBasicCredential(t *testing.T) {
	t.Parallel()

	authenticator, err := basic.NewStatic([]basic.Entry{{
		Username:  "service",
		Password:  "expected-password",
		Principal: authentication.PrincipalSpec{Subject: "service-1"},
	}})
	if err != nil {
		t.Fatalf("NewStatic() error = %v", err)
	}

	tests := []struct {
		name       string
		credential authentication.Credential
		want       error
	}{
		{name: "wrong password", credential: authentication.NewBasicCredential("service", "wrong"), want: authentication.ErrCredentialsRejected},
		{name: "unknown user", credential: authentication.NewBasicCredential("unknown", "expected-password"), want: authentication.ErrCredentialsRejected},
		{name: "empty username", credential: authentication.NewBasicCredential("", "expected-password"), want: authentication.ErrCredentialsInvalid},
		{name: "empty password", credential: authentication.NewBasicCredential("service", ""), want: authentication.ErrCredentialsInvalid},
		{name: "wrong kind", credential: authentication.NewBearerCredential("expected-password"), want: authentication.ErrCredentialsInvalid},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := authenticator.Authenticate(context.Background(), tt.credential)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Authenticate() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestStaticRejectsUnsafeConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries []basic.Entry
	}{
		{name: "empty"},
		{name: "empty username", entries: []basic.Entry{{Password: "secret", Principal: authentication.PrincipalSpec{Subject: "subject"}}}},
		{name: "empty password", entries: []basic.Entry{{Username: "user", Principal: authentication.PrincipalSpec{Subject: "subject"}}}},
		{name: "control in username", entries: []basic.Entry{{Username: "us\x00er", Password: "secret", Principal: authentication.PrincipalSpec{Subject: "subject"}}}},
		{name: "control in password", entries: []basic.Entry{{Username: "user", Password: "sec\x7fret", Principal: authentication.PrincipalSpec{Subject: "subject"}}}},
		{name: "wrong method", entries: []basic.Entry{{Username: "user", Password: "secret", Principal: authentication.PrincipalSpec{Subject: "subject", Method: "bearer"}}}},
		{name: "invalid principal", entries: []basic.Entry{{Username: "user", Password: "secret"}}},
		{name: "duplicate credential", entries: []basic.Entry{
			{Username: "user", Password: "secret", Principal: authentication.PrincipalSpec{Subject: "one"}},
			{Username: "user", Password: "secret", Principal: authentication.PrincipalSpec{Subject: "two"}},
		}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := basic.NewStatic(tt.entries)
			if !errors.Is(err, authentication.ErrInvalidConfiguration) {
				t.Fatalf("NewStatic() error = %v, want ErrInvalidConfiguration", err)
			}
		})
	}
}

func TestStaticHonorsCancellation(t *testing.T) {
	t.Parallel()

	authenticator, err := basic.NewStatic([]basic.Entry{{
		Username: "service", Password: "secret", Principal: authentication.PrincipalSpec{Subject: "service"},
	}})
	if err != nil {
		t.Fatalf("NewStatic() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := authenticator.Authenticate(ctx, authentication.NewBasicCredential("service", "secret")); !errors.Is(err, authentication.ErrAuthenticationUnavailable) || !errors.Is(err, context.Canceled) {
		t.Fatalf("Authenticate(canceled) error = %v", err)
	}
}
