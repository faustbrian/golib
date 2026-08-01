package authhttp

import (
	"errors"
	"net/http"
	"net/url"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
)

type invariantSource struct {
	credential authentication.Credential
	present    bool
}

func (s invariantSource) extract(*http.Request) (authentication.Credential, bool, error) {
	return s.credential, s.present, nil
}

func (invariantSource) validate() error { return nil }

func TestExtractorContinuesAfterAbsentSource(t *testing.T) {
	t.Parallel()

	extractor, err := NewExtractor(
		invariantSource{},
		invariantSource{credential: authentication.NewBearerCredential("token"), present: true},
	)
	if err != nil {
		t.Fatalf("NewExtractor() error = %v", err)
	}
	credential, err := extractor.Extract(&http.Request{Header: make(http.Header), URL: &url.URL{}})
	if err != nil || credential == nil || credential.Kind() != authentication.CredentialBearer {
		t.Fatalf("Extract() = (%v, %v)", credential, err)
	}
}

func TestPrivateSourceInvariantsRejectInvalidLocationsAndKinds(t *testing.T) {
	t.Parallel()

	tests := []Source{
		authorizationSource{kind: authentication.CredentialAPIKey, maxBytes: 1},
		authorizationSource{kind: authentication.CredentialBearer, maxBytes: 0},
		bearerNamedSource{location: sourceLocation(99), name: "token", maxBytes: 1},
		bearerNamedSource{location: locationQuery, name: "token", maxBytes: 0},
		apiKeySource{location: sourceLocation(99), idName: "id", keyName: "key", maxIDBytes: 1, maxKeyBytes: 1},
		apiKeySource{location: locationHeader, idName: "id", keyName: "key", maxIDBytes: 0, maxKeyBytes: 1},
		apiKeySource{location: locationHeader, idName: "id", keyName: "key", maxIDBytes: 1, maxKeyBytes: 0},
	}
	for _, source := range tests {
		if err := source.validate(); !errors.Is(err, authentication.ErrInvalidConfiguration) {
			t.Fatalf("validate(%T) error = %v", source, err)
		}
	}
}

func TestPrivateTokenAndNameBoundaries(t *testing.T) {
	t.Parallel()

	for _, token := range []string{"az", "AZ", "09", "-._~+/", "ab=="} {
		if !validBearerToken(token, false) {
			t.Errorf("validBearerToken(%q) = false", token)
		}
	}
	for _, token := range []string{"`", "{", "@", "[", ":", "a=b=c"} {
		if validBearerToken(token, false) {
			t.Errorf("validBearerToken(%q) = true", token)
		}
	}
	for _, name := range []string{"!", "~", "X-API-Key"} {
		if !validName(name) {
			t.Errorf("validName(%q) = false", name)
		}
	}
	for _, name := range []string{" bad", "bad\x7f"} {
		if validName(name) {
			t.Errorf("validName(%q) = true", name)
		}
	}
}

func TestPrivateSourcesAcceptExactBounds(t *testing.T) {
	t.Parallel()

	request := &http.Request{Header: make(http.Header), URL: &url.URL{}}
	request.Header.Set("Authorization", "Bearer az09")
	if _, present, err := (authorizationSource{kind: authentication.CredentialBearer, maxBytes: 4}).extract(request); err != nil || !present {
		t.Fatalf("authorization exact bound = present %v error %v", present, err)
	}

	request.Header.Set("X-ID", "id")
	request.Header.Set("X-Key", "key")
	source := apiKeySource{location: locationHeader, idName: "X-ID", keyName: "X-Key", maxIDBytes: 2, maxKeyBytes: 3}
	credential, present, err := source.extract(request)
	if err != nil || !present || credential == nil || credential.Kind() != authentication.CredentialAPIKey {
		t.Fatalf("API key exact bound = (%v, %v, %v)", credential, present, err)
	}

	if err := (bearerNamedSource{location: locationCookie, name: "token", maxBytes: 1}).validate(); err != nil {
		t.Fatalf("bearer cookie boundary validate error = %v", err)
	}
	if err := (apiKeySource{location: locationCookie, idName: "id", keyName: "key", maxIDBytes: 1, maxKeyBytes: 1}).validate(); err != nil {
		t.Fatalf("API key cookie boundary validate error = %v", err)
	}
}

func TestNamedValuesRejectsUnknownInternalLocation(t *testing.T) {
	t.Parallel()

	request := &http.Request{Header: make(http.Header)}
	if _, err := namedValues(request, sourceLocation(99), "token"); !errors.Is(err, authentication.ErrCredentialsInvalid) {
		t.Fatalf("namedValues() error = %v", err)
	}
}
