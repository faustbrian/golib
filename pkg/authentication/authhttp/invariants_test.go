package authhttp

import (
	"errors"
	"net/http"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
)

func TestPrivateSourceInvariantsRejectInvalidLocationsAndKinds(t *testing.T) {
	t.Parallel()

	tests := []Source{
		authorizationSource{kind: authentication.CredentialAPIKey, maxBytes: 1},
		bearerNamedSource{location: sourceLocation(99), name: "token", maxBytes: 1},
		apiKeySource{location: sourceLocation(99), idName: "id", keyName: "key", maxIDBytes: 1, maxKeyBytes: 1},
	}
	for _, source := range tests {
		if err := source.validate(); !errors.Is(err, authentication.ErrInvalidConfiguration) {
			t.Fatalf("validate(%T) error = %v", source, err)
		}
	}
}

func TestNamedValuesRejectsUnknownInternalLocation(t *testing.T) {
	t.Parallel()

	request := &http.Request{Header: make(http.Header)}
	if _, err := namedValues(request, sourceLocation(99), "token"); !errors.Is(err, authentication.ErrCredentialsInvalid) {
		t.Fatalf("namedValues() error = %v", err)
	}
}
