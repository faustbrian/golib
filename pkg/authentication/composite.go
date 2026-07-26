package authentication

import (
	"context"
	"errors"
	"fmt"
)

// Binding associates a credential kind with one authenticator. Bindings of the
// same kind are evaluated in declaration order.
type Binding struct {
	Kind          CredentialKind
	Authenticator Authenticator
}

// Composite routes typed credentials to ordered authenticators. Only rejected
// credentials fall through; every other failure is terminal.
type Composite struct {
	authenticators map[CredentialKind][]Authenticator
}

// NewComposite validates and copies ordered authenticator bindings.
func NewComposite(bindings []Binding) (*Composite, error) {
	if len(bindings) == 0 {
		return nil, fmt.Errorf("%w: empty authenticator bindings", ErrInvalidConfiguration)
	}

	authenticators := make(map[CredentialKind][]Authenticator)
	for _, binding := range bindings {
		if binding.Kind == "" || isNilAuthenticator(binding.Authenticator) {
			return nil, fmt.Errorf("%w: invalid authenticator binding", ErrInvalidConfiguration)
		}
		authenticators[binding.Kind] = append(authenticators[binding.Kind], binding.Authenticator)
	}

	return &Composite{authenticators: authenticators}, nil
}

// Authenticate evaluates authenticators bound to the credential kind in
// deterministic declaration order.
func (c *Composite) Authenticate(ctx context.Context, credential Credential) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, NewFailure(FailureUnavailable, WithFailureCause(err))
	}
	if credential == nil {
		return Result{}, NewFailure(FailureInvalid)
	}

	authenticators := c.authenticators[credential.Kind()]
	if len(authenticators) == 0 {
		return Result{}, NewFailure(FailureUnavailable, WithFailureCause(ErrInvalidConfiguration))
	}

	var challenges []Challenge
	for _, authenticator := range authenticators {
		result, err := authenticator.Authenticate(ctx, credential)
		if err == nil {
			if principal, ok := result.Principal(); ok && result.State() == ResultAuthenticated && !principal.IsAnonymous() {
				return result, nil
			}
			return Result{}, NewFailure(FailureUnavailable, WithFailureCause(ErrInvalidConfiguration))
		}

		if errors.Is(err, ErrCredentialsRejected) {
			var failure *Failure
			if errors.As(err, &failure) {
				challenges = append(challenges, failure.Challenges()...)
			}
			continue
		}
		if isClassifiedFailure(err) {
			return Result{}, err
		}
		return Result{}, NewFailure(FailureUnavailable, WithFailureCause(err))
	}

	return Result{}, NewFailure(FailureRejected, WithChallenges(challenges...))
}

func isClassifiedFailure(err error) bool {
	return errors.Is(err, ErrCredentialsAbsent) ||
		errors.Is(err, ErrCredentialsInvalid) ||
		errors.Is(err, ErrAuthenticationUnavailable) ||
		errors.Is(err, ErrAmbiguousCredentials)
}

func isNilAuthenticator(authenticator Authenticator) bool {
	return isNil(authenticator)
}

var _ Authenticator = (*Composite)(nil)
