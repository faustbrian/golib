package apikey_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/apikey"
)

func TestStaticAuthenticatesKeyByDeterministicID(t *testing.T) {
	t.Parallel()

	authenticator, err := apikey.NewStatic([]apikey.Entry{
		{ID: "current", Key: "current-secret", Principal: authentication.PrincipalSpec{Subject: "service-current"}},
		{ID: "previous", Key: "previous-secret", Principal: authentication.PrincipalSpec{Subject: "service-previous"}},
	})
	if err != nil {
		t.Fatalf("NewStatic() error = %v", err)
	}

	result, err := authenticator.Authenticate(context.Background(),
		authentication.NewAPIKeyCredential("previous", "previous-secret"),
	)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	principal, ok := result.Principal()
	if !ok || principal.Subject() != "service-previous" || principal.Method() != "api_key" {
		t.Fatalf("Authenticate() principal = (%v, %v)", principal, ok)
	}
}

func TestStaticRejectsUnknownInvalidAndAmbiguousKeys(t *testing.T) {
	t.Parallel()

	authenticator, err := apikey.NewStatic([]apikey.Entry{{
		ID: "primary", Key: "api-secret", Principal: authentication.PrincipalSpec{Subject: "service"},
	}})
	if err != nil {
		t.Fatalf("NewStatic() error = %v", err)
	}

	tests := []struct {
		name       string
		credential authentication.Credential
		want       error
	}{
		{name: "wrong secret", credential: authentication.NewAPIKeyCredential("primary", "wrong"), want: authentication.ErrCredentialsRejected},
		{name: "unknown id", credential: authentication.NewAPIKeyCredential("unknown", "api-secret"), want: authentication.ErrCredentialsRejected},
		{name: "empty key", credential: authentication.NewAPIKeyCredential("primary", ""), want: authentication.ErrCredentialsInvalid},
		{name: "empty id", credential: authentication.NewAPIKeyCredential("", "api-secret"), want: authentication.ErrCredentialsInvalid},
		{name: "wrong kind", credential: authentication.NewBearerCredential("api-secret"), want: authentication.ErrCredentialsInvalid},
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

func TestStaticRotationAtomicallyReplacesActiveKeys(t *testing.T) {
	t.Parallel()

	authenticator, err := apikey.NewStatic([]apikey.Entry{{
		ID: "old", Key: "old-secret", Principal: authentication.PrincipalSpec{Subject: "old"},
	}})
	if err != nil {
		t.Fatalf("NewStatic() error = %v", err)
	}

	if err := authenticator.Replace([]apikey.Entry{{
		ID: "new", Key: "new-secret", Principal: authentication.PrincipalSpec{Subject: "new"},
	}}); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	if _, err := authenticator.Authenticate(context.Background(), authentication.NewAPIKeyCredential("old", "old-secret")); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("old key error = %v, want rejected", err)
	}
	if _, err := authenticator.Authenticate(context.Background(), authentication.NewAPIKeyCredential("new", "new-secret")); err != nil {
		t.Fatalf("new key error = %v", err)
	}
}

func TestStaticRotationIsRaceSafe(t *testing.T) {
	t.Parallel()

	authenticator, err := apikey.NewStatic([]apikey.Entry{{
		ID: "a", Key: "secret-a", Principal: authentication.PrincipalSpec{Subject: "a"},
	}})
	if err != nil {
		t.Fatalf("NewStatic() error = %v", err)
	}

	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			for j := 0; j < 100; j++ {
				id := "a"
				key := "secret-a"
				if (index+j)%2 == 1 {
					id = "b"
					key = "secret-b"
				}
				_, _ = authenticator.Authenticate(context.Background(), authentication.NewAPIKeyCredential(id, key))
			}
		}(i)
	}
	for i := 0; i < 100; i++ {
		entries := []apikey.Entry{{ID: "a", Key: "secret-a", Principal: authentication.PrincipalSpec{Subject: "a"}}}
		if i%2 == 1 {
			entries = []apikey.Entry{{ID: "b", Key: "secret-b", Principal: authentication.PrincipalSpec{Subject: "b"}}}
		}
		if err := authenticator.Replace(entries); err != nil {
			t.Fatalf("Replace() error = %v", err)
		}
	}
	group.Wait()
}

func TestStaticRejectsDuplicateKeyConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries []apikey.Entry
	}{
		{name: "empty"},
		{name: "empty id", entries: []apikey.Entry{{Key: "secret", Principal: authentication.PrincipalSpec{Subject: "subject"}}}},
		{name: "empty key", entries: []apikey.Entry{{ID: "id", Principal: authentication.PrincipalSpec{Subject: "subject"}}}},
		{name: "wrong method", entries: []apikey.Entry{{ID: "id", Key: "secret", Principal: authentication.PrincipalSpec{Subject: "subject", Method: "bearer"}}}},
		{name: "invalid principal", entries: []apikey.Entry{{ID: "id", Key: "secret"}}},
		{name: "duplicate id", entries: []apikey.Entry{
			{ID: "id", Key: "one", Principal: authentication.PrincipalSpec{Subject: "one"}},
			{ID: "id", Key: "two", Principal: authentication.PrincipalSpec{Subject: "two"}},
		}},
		{name: "duplicate key", entries: []apikey.Entry{
			{ID: "one", Key: "same", Principal: authentication.PrincipalSpec{Subject: "one"}},
			{ID: "two", Key: "same", Principal: authentication.PrincipalSpec{Subject: "two"}},
		}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := apikey.NewStatic(tt.entries)
			if !errors.Is(err, authentication.ErrInvalidConfiguration) {
				t.Fatalf("NewStatic() error = %v, want ErrInvalidConfiguration", err)
			}
		})
	}
}

func TestStaticHonorsCancellationAndRequiresInitialization(t *testing.T) {
	t.Parallel()

	authenticator, err := apikey.NewStatic([]apikey.Entry{{
		ID: "id", Key: "secret", Principal: authentication.PrincipalSpec{Subject: "service"},
	}})
	if err != nil {
		t.Fatalf("NewStatic() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := authenticator.Authenticate(ctx, authentication.NewAPIKeyCredential("id", "secret")); !errors.Is(err, authentication.ErrAuthenticationUnavailable) || !errors.Is(err, context.Canceled) {
		t.Fatalf("Authenticate(canceled) error = %v", err)
	}
	var uninitialized apikey.Static
	if _, err := uninitialized.Authenticate(context.Background(), authentication.NewAPIKeyCredential("id", "secret")); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("Authenticate(uninitialized) error = %v", err)
	}
}
