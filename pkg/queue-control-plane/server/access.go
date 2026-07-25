package server

import (
	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/apikey"
	"github.com/faustbrian/golib/pkg/authentication/authhttp"
	authorization "github.com/faustbrian/golib/pkg/authorization"
	"github.com/faustbrian/golib/pkg/authorization/acl"
	controlauthz "github.com/faustbrian/golib/pkg/queue-control-plane/authz"
)

const (
	// APIKeyIDHeader carries the non-secret static key identifier.
	APIKeyIDHeader = "X-Queue-Control-Key-ID" //nolint:gosec // A protocol header name, not a credential.
	// APIKeySecretHeader carries the static key credential.
	APIKeySecretHeader = "X-Queue-Control-Key" //nolint:gosec // A protocol header name, not a credential.
)

// StaticAccess is a coherent authentication and authorization configuration.
type StaticAccess struct {
	Extractor     *authhttp.Extractor
	Authenticator authentication.Authenticator
	Authorizer    *controlauthz.Authorizer
}

// NewStaticAccess builds bounded API-key authentication and deny-overrides ACL
// authorization from immutable startup configuration.
func NewStaticAccess(keys []apikey.Entry, entries []acl.Entry) (*StaticAccess, error) {
	authenticator, err := apikey.NewStatic(keys)
	if err != nil {
		return nil, err
	}
	extractor := must(authhttp.NewExtractor(
		authhttp.APIKeyHeader(APIKeyIDHeader, APIKeySecretHeader),
	))
	evaluator, err := acl.New(entries)
	if err != nil {
		return nil, err
	}
	snapshot := must(authorization.NewSnapshot(
		1,
		authorization.DenyOverrides,
		authorization.PolicyDefinition{
			ID:        "queue-control-plane-acl",
			Evaluator: evaluator,
		},
	))
	engine := must(authorization.NewEngine(snapshot))
	authorizer := must(controlauthz.New(engine, authorization.SubjectAPIKey))

	return &StaticAccess{
		Extractor:     extractor,
		Authenticator: authenticator,
		Authorizer:    authorizer,
	}, nil
}

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}

	return value
}
