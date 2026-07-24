// Package bearer provides validation adapters for opaque bearer tokens.
package bearer

import (
	"context"
	"errors"
	"fmt"

	authentication "github.com/faustbrian/golib/pkg/authentication"
)

const defaultMaxTokenBytes = 8 * 1024

// Validator validates an opaque bearer token and returns an immutable principal.
type Validator interface {
	ValidateBearer(context.Context, string) (authentication.Principal, error)
}

// ValidatorFunc adapts a function to Validator.
type ValidatorFunc func(context.Context, string) (authentication.Principal, error)

// ValidateBearer calls f.
func (f ValidatorFunc) ValidateBearer(ctx context.Context, token string) (authentication.Principal, error) {
	return f(ctx, token)
}

type config struct{ maxTokenBytes int }

// Option configures an Authenticator.
type Option func(*config)

// WithMaxTokenBytes sets the inclusive token-size bound.
func WithMaxTokenBytes(maximum int) Option {
	return func(configuration *config) { configuration.maxTokenBytes = maximum }
}

// Authenticator validates opaque bearer credentials through a callback or interface.
type Authenticator struct {
	validator     Validator
	maxTokenBytes int
}

// New creates an opaque bearer authenticator.
func New(validator Validator, options ...Option) (*Authenticator, error) {
	configuration := config{maxTokenBytes: defaultMaxTokenBytes}
	for _, option := range options {
		if option != nil {
			option(&configuration)
		}
	}
	if validator == nil || configuration.maxTokenBytes <= 0 {
		return nil, fmt.Errorf("%w: bearer validator", authentication.ErrInvalidConfiguration)
	}

	return &Authenticator{validator: validator, maxTokenBytes: configuration.maxTokenBytes}, nil
}

// Authenticate validates one bounded bearer credential.
func (a *Authenticator) Authenticate(ctx context.Context, credential authentication.Credential) (authentication.Result, error) {
	if err := ctx.Err(); err != nil {
		return authentication.Result{}, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	bearerCredential, ok := credential.(authentication.BearerCredential)
	if !ok || bearerCredential.Token() == "" || len(bearerCredential.Token()) > a.maxTokenBytes {
		return authentication.Result{}, authentication.NewFailure(authentication.FailureInvalid)
	}

	principal, err := a.validator.ValidateBearer(ctx, bearerCredential.Token())
	if err != nil {
		var failure *authentication.Failure
		if errors.As(err, &failure) {
			return authentication.Result{}, err
		}
		return authentication.Result{}, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	if principal.IsAnonymous() || principal.Method() != "bearer" {
		return authentication.Result{}, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(authentication.ErrInvalidPrincipal))
	}

	return authentication.NewAuthenticatedResult(principal)
}

var _ authentication.Authenticator = (*Authenticator)(nil)
