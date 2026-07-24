package authentication_test

import (
	"fmt"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
)

func TestCredentialsExposeKindWithoutFormattingSecrets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		credential authentication.Credential
		kind       authentication.CredentialKind
		secret     string
	}{
		{name: "basic", credential: authentication.NewBasicCredential("alice", "correct horse"), kind: authentication.CredentialBasic, secret: "correct horse"},
		{name: "bearer", credential: authentication.NewBearerCredential("opaque-token"), kind: authentication.CredentialBearer, secret: "opaque-token"},
		{name: "api key", credential: authentication.NewAPIKeyCredential("primary", "api-secret"), kind: authentication.CredentialAPIKey, secret: "api-secret"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.credential.Kind(); got != tt.kind {
				t.Errorf("Credential.Kind() = %q, want %q", got, tt.kind)
			}
			if got := fmt.Sprintf("%v", tt.credential); got == tt.secret {
				t.Fatalf("formatted credential disclosed secret %q", got)
			}
			if got := fmt.Sprintf("%#v", tt.credential); contains(got, tt.secret) {
				t.Fatalf("Go-syntax credential disclosed secret in %q", got)
			}
		})
	}
}

func TestCredentialAccessorsPreserveTypedValues(t *testing.T) {
	t.Parallel()

	basic := authentication.NewBasicCredential("service", "password")
	if basic.Username() != "service" || basic.Password() != "password" {
		t.Fatalf("Basic credential = (%q, %q)", basic.Username(), basic.Password())
	}
	bearer := authentication.NewBearerCredential("token")
	if bearer.Token() != "token" {
		t.Fatalf("Bearer token = %q", bearer.Token())
	}
	apiKey := authentication.NewAPIKeyCredential("key-1", "secret")
	if apiKey.KeyID() != "key-1" || apiKey.Key() != "secret" {
		t.Fatalf("API key = (%q, %q)", apiKey.KeyID(), apiKey.Key())
	}
}

func contains(value, secret string) bool {
	for i := 0; i+len(secret) <= len(value); i++ {
		if value[i:i+len(secret)] == secret {
			return true
		}
	}

	return false
}
