package apikey

import (
	"context"
	"errors"
	"fmt"

	authentication "github.com/faustbrian/golib/pkg/authentication"
)

const (
	defaultMaxKeyIDBytes = 256
	defaultMaxKeyBytes   = 8 * 1024
)

// Validator validates an API key and returns an immutable principal.
type Validator interface {
	ValidateAPIKey(context.Context, string, string) (authentication.Principal, error)
}

// ValidatorFunc adapts a function to Validator.
type ValidatorFunc func(context.Context, string, string) (authentication.Principal, error)

// ValidateAPIKey calls f.
func (f ValidatorFunc) ValidateAPIKey(ctx context.Context, keyID, key string) (authentication.Principal, error) {
	return f(ctx, keyID, key)
}

type config struct {
	maxKeyIDBytes int
	maxKeyBytes   int
}

// Option configures an Authenticator.
type Option func(*config)

// WithMaxKeyIDBytes sets the inclusive key-ID size bound.
func WithMaxKeyIDBytes(maximum int) Option {
	return func(configuration *config) { configuration.maxKeyIDBytes = maximum }
}

// WithMaxKeyBytes sets the inclusive key size bound.
func WithMaxKeyBytes(maximum int) Option {
	return func(configuration *config) { configuration.maxKeyBytes = maximum }
}

// Authenticator validates API-key credentials through a callback or interface.
type Authenticator struct {
	validator     Validator
	maxKeyIDBytes int
	maxKeyBytes   int
}

// New creates a callback API-key authenticator.
func New(validator Validator, options ...Option) (*Authenticator, error) {
	configuration := config{
		maxKeyIDBytes: defaultMaxKeyIDBytes,
		maxKeyBytes:   defaultMaxKeyBytes,
	}
	for _, option := range options {
		if option != nil {
			option(&configuration)
		}
	}
	if validator == nil || configuration.maxKeyIDBytes <= 0 || configuration.maxKeyBytes <= 0 {
		return nil, fmt.Errorf("%w: API-key validator", authentication.ErrInvalidConfiguration)
	}

	return &Authenticator{
		validator:     validator,
		maxKeyIDBytes: configuration.maxKeyIDBytes,
		maxKeyBytes:   configuration.maxKeyBytes,
	}, nil
}

// Authenticate validates one bounded API-key credential.
func (a *Authenticator) Authenticate(ctx context.Context, credential authentication.Credential) (authentication.Result, error) {
	if err := ctx.Err(); err != nil {
		return authentication.Result{}, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	apiKey, ok := credential.(authentication.APIKeyCredential)
	if !ok || apiKey.KeyID() == "" || apiKey.Key() == "" ||
		len(apiKey.KeyID()) > a.maxKeyIDBytes || len(apiKey.Key()) > a.maxKeyBytes {
		return authentication.Result{}, authentication.NewFailure(authentication.FailureInvalid)
	}

	principal, err := a.validator.ValidateAPIKey(ctx, apiKey.KeyID(), apiKey.Key())
	if err != nil {
		var failure *authentication.Failure
		if errors.As(err, &failure) {
			return authentication.Result{}, err
		}
		return authentication.Result{}, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	if principal.IsAnonymous() || principal.Method() != "api_key" {
		return authentication.Result{}, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(authentication.ErrInvalidPrincipal))
	}

	return authentication.NewAuthenticatedResult(principal)
}

var _ authentication.Authenticator = (*Authenticator)(nil)
