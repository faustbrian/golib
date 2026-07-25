package authentication

import "fmt"

// CredentialKind identifies a credential's protocol family.
type CredentialKind string

const (
	CredentialBasic  CredentialKind = "basic"
	CredentialBearer CredentialKind = "bearer"
	CredentialAPIKey CredentialKind = "api_key"
)

// Credential is a typed authentication credential. Implementations redact
// their secret-bearing representation.
type Credential interface {
	Kind() CredentialKind
	fmt.Stringer
}

// BasicCredential is a username and password extracted from Basic
// authentication. Its formatted representation is always redacted.
type BasicCredential struct {
	username string
	password string
}

// NewBasicCredential creates a Basic credential.
func NewBasicCredential(username, password string) BasicCredential {
	return BasicCredential{username: username, password: password}
}

func (BasicCredential) Kind() CredentialKind { return CredentialBasic }
func (BasicCredential) String() string       { return "basic credential [REDACTED]" }
func (BasicCredential) GoString() string     { return "authentication.BasicCredential{[REDACTED]}" }
func (c BasicCredential) Username() string   { return c.username }
func (c BasicCredential) Password() string   { return c.password }

// BearerCredential is an opaque bearer token. Its formatted representation is
// always redacted.
type BearerCredential struct{ token string }

// NewBearerCredential creates a bearer credential.
func NewBearerCredential(token string) BearerCredential { return BearerCredential{token: token} }

func (BearerCredential) Kind() CredentialKind { return CredentialBearer }
func (BearerCredential) String() string       { return "bearer credential [REDACTED]" }
func (BearerCredential) GoString() string     { return "authentication.BearerCredential{[REDACTED]}" }
func (c BearerCredential) Token() string      { return c.token }

// APIKeyCredential is an API key with an optional non-secret key identifier.
// Its formatted representation is always redacted.
type APIKeyCredential struct {
	keyID string
	key   string
}

// NewAPIKeyCredential creates an API-key credential.
func NewAPIKeyCredential(keyID, key string) APIKeyCredential {
	return APIKeyCredential{keyID: keyID, key: key}
}

func (APIKeyCredential) Kind() CredentialKind { return CredentialAPIKey }
func (APIKeyCredential) String() string       { return "api-key credential [REDACTED]" }
func (APIKeyCredential) GoString() string     { return "authentication.APIKeyCredential{[REDACTED]}" }
func (c APIKeyCredential) KeyID() string      { return c.keyID }
func (c APIKeyCredential) Key() string        { return c.key }
