package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/apikey"
	authorization "github.com/faustbrian/golib/pkg/authorization"
	"github.com/faustbrian/golib/pkg/authorization/acl"
)

// ErrInvalidAccessDocument is a secret-safe static access configuration error.
var ErrInvalidAccessDocument = errors.New("server: invalid access document")

type accessDocument struct {
	Keys []accessKey   `json:"keys"`
	ACL  []accessGrant `json:"acl"`
}

type accessKey struct {
	ID      string `json:"id"`
	Key     string `json:"key"`
	Subject string `json:"subject"`
}

type accessGrant struct {
	ID           string `json:"id"`
	Subject      string `json:"subject"`
	Tenant       string `json:"tenant"`
	Action       string `json:"action"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	Effect       string `json:"effect"`
}

// LoadStaticAccessFile opens one access document without disclosing its path
// or contents through returned errors.
func LoadStaticAccessFile(path string, maxBytes int64) (*StaticAccess, error) {
	if strings.TrimSpace(path) == "" {
		return nil, ErrInvalidAccessDocument
	}
	file, err := os.Open(path) //nolint:gosec // The operator explicitly configures this access document path.
	if err != nil {
		return nil, ErrInvalidAccessDocument
	}
	defer func() { _ = file.Close() }()

	return LoadStaticAccess(file, maxBytes)
}

// LoadStaticAccess reads one strict, bounded JSON access document without
// retaining or formatting its source bytes in errors.
func LoadStaticAccess(reader io.Reader, maxBytes int64) (*StaticAccess, error) {
	if nilInterface(reader) || maxBytes < 1 {
		return nil, ErrInvalidAccessDocument
	}
	encoded, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil || int64(len(encoded)) > maxBytes {
		return nil, ErrInvalidAccessDocument
	}

	var document accessDocument
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, ErrInvalidAccessDocument
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidAccessDocument
	}

	keys := make([]apikey.Entry, len(document.Keys))
	for index, key := range document.Keys {
		keys[index] = apikey.Entry{
			ID:  key.ID,
			Key: key.Key,
			Principal: authentication.PrincipalSpec{
				Subject: key.Subject,
			},
		}
	}
	entries := make([]acl.Entry, len(document.ACL))
	for index, grant := range document.ACL {
		effect, ok := accessEffect(grant.Effect)
		if !ok {
			return nil, ErrInvalidAccessDocument
		}
		entries[index] = acl.Entry{
			ID: authorization.PolicyID(grant.ID),
			Subject: authorization.Subject{
				Kind: authorization.SubjectAPIKey,
				ID:   authorization.SubjectID(grant.Subject),
			},
			Action:       authorization.Action(grant.Action),
			ResourceType: authorization.ResourceType(grant.ResourceType),
			ResourceID:   authorization.ResourceID(grant.ResourceID),
			Tenant:       authorization.TenantID(grant.Tenant),
			Effect:       effect,
		}
	}
	access, err := NewStaticAccess(keys, entries)
	if err != nil {
		return nil, ErrInvalidAccessDocument
	}

	return access, nil
}

func accessEffect(value string) (authorization.Outcome, bool) {
	switch value {
	case "allow":
		return authorization.Allow, true
	case "deny":
		return authorization.Deny, true
	default:
		return authorization.NotApplicable, false
	}
}
