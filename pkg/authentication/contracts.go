package authentication

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	// MaxChallengeParameters bounds authentication challenge metadata.
	MaxChallengeParameters = 16
	// MaxChallengeSchemeBytes bounds an authentication scheme token.
	MaxChallengeSchemeBytes = 64
	// MaxChallengeNameBytes bounds an authentication parameter name.
	MaxChallengeNameBytes = 64
	// MaxChallengeValueBytes bounds an authentication parameter value.
	MaxChallengeValueBytes = 1024
)

var (
	// ErrCredentialsAbsent means no credential was supplied by an enabled source.
	ErrCredentialsAbsent = errors.New("authentication: credentials absent")
	// ErrCredentialsInvalid means credential syntax or protocol data was invalid.
	ErrCredentialsInvalid = errors.New("authentication: credentials invalid")
	// ErrCredentialsRejected means a validly formed credential was not accepted.
	ErrCredentialsRejected = errors.New("authentication: credentials rejected")
	// ErrAuthenticationUnavailable means validation could not be completed due
	// to a transient dependency or infrastructure failure.
	ErrAuthenticationUnavailable = errors.New("authentication: validation unavailable")
	// ErrAmbiguousCredentials means more than one credential was supplied where
	// exactly one was required.
	ErrAmbiguousCredentials = errors.New("authentication: ambiguous credentials")
	// ErrInvalidChallenge identifies invalid challenge protocol data.
	ErrInvalidChallenge = errors.New("authentication: invalid challenge")
	// ErrInvalidConfiguration identifies unsafe or incomplete authenticator
	// configuration.
	ErrInvalidConfiguration = errors.New("authentication: invalid configuration")
)

// FailureKind classifies authentication failures independently of transports.
type FailureKind string

const (
	FailureAbsent      FailureKind = "absent"
	FailureInvalid     FailureKind = "invalid"
	FailureRejected    FailureKind = "rejected"
	FailureUnavailable FailureKind = "unavailable"
	FailureAmbiguous   FailureKind = "ambiguous"
)

// Failure is a secret-safe, typed authentication failure.
type Failure struct {
	kind       FailureKind
	cause      error
	challenges []Challenge
}

// FailureOption configures a Failure.
type FailureOption func(*Failure)

// WithFailureCause preserves cause for errors.Is and errors.As without
// including the cause text in Failure.Error.
func WithFailureCause(cause error) FailureOption {
	return func(failure *Failure) { failure.cause = cause }
}

// WithChallenges associates transport-independent challenges with a failure.
func WithChallenges(challenges ...Challenge) FailureOption {
	return func(failure *Failure) {
		failure.challenges = append([]Challenge(nil), challenges...)
	}
}

// NewFailure creates a classified, secret-safe failure.
func NewFailure(kind FailureKind, options ...FailureOption) *Failure {
	failure := &Failure{kind: kind}
	for _, option := range options {
		if option != nil {
			option(failure)
		}
	}

	return failure
}

// Kind returns the stable failure classification.
func (f *Failure) Kind() FailureKind {
	if f == nil {
		return ""
	}
	return f.kind
}

// Error returns only the stable classification and never formats the cause.
func (f *Failure) Error() string {
	if f == nil {
		return "authentication: failure"
	}
	if sentinel := failureSentinel(f.kind); sentinel != nil {
		return sentinel.Error()
	}
	return "authentication: failure"
}

// Unwrap returns the underlying operational cause, if any.
func (f *Failure) Unwrap() error {
	if f == nil {
		return nil
	}
	return f.cause
}

// Is supports stable errors.Is matching by failure classification.
func (f *Failure) Is(target error) bool {
	if f == nil {
		return false
	}
	return target == failureSentinel(f.kind)
}

// Challenges returns a defensive copy of the associated challenges.
func (f *Failure) Challenges() []Challenge {
	if f == nil {
		return nil
	}
	return append([]Challenge(nil), f.challenges...)
}

func failureSentinel(kind FailureKind) error {
	switch kind {
	case FailureAbsent:
		return ErrCredentialsAbsent
	case FailureInvalid:
		return ErrCredentialsInvalid
	case FailureRejected:
		return ErrCredentialsRejected
	case FailureUnavailable:
		return ErrAuthenticationUnavailable
	case FailureAmbiguous:
		return ErrAmbiguousCredentials
	default:
		return nil
	}
}

// Challenge is an immutable authentication challenge. Transport adapters are
// responsible for serializing it according to their protocol.
type Challenge struct {
	scheme     string
	parameters map[string]string
}

// NewChallenge validates and copies challenge protocol data.
func NewChallenge(scheme string, parameters map[string]string) (Challenge, error) {
	if len(scheme) > MaxChallengeSchemeBytes || !isToken(scheme) {
		return Challenge{}, ErrInvalidChallenge
	}
	if len(parameters) > MaxChallengeParameters {
		return Challenge{}, ErrInvalidChallenge
	}

	copied := make(map[string]string, len(parameters))
	for name, value := range parameters {
		if len(name) > MaxChallengeNameBytes || !isToken(name) ||
			len(value) > MaxChallengeValueBytes || !validQuotedText(value) {
			return Challenge{}, ErrInvalidChallenge
		}
		copied[name] = value
	}

	return Challenge{scheme: scheme, parameters: copied}, nil
}

func validQuotedText(value string) bool {
	for _, character := range []byte(value) {
		if character != '\t' && (character < 0x20 || character == 0x7f) {
			return false
		}
	}
	return true
}

// Scheme returns the authentication scheme.
func (c Challenge) Scheme() string { return c.scheme }

// Parameters returns a defensive copy of the authentication parameters.
func (c Challenge) Parameters() map[string]string {
	copied := make(map[string]string, len(c.parameters))
	for name, value := range c.parameters {
		copied[name] = value
	}
	return copied
}

func isToken(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range []byte(value) {
		if character <= 0x20 || character >= 0x7f || strings.ContainsRune("()<>@,;:\\\"/[]?={}", rune(character)) {
			return false
		}
	}
	return true
}

// ResultState identifies whether authentication established an identity or an
// explicitly permitted anonymous state.
type ResultState string

const (
	ResultAuthenticated ResultState = "authenticated"
	ResultAnonymous     ResultState = "anonymous"
)

// Result is the outcome of successful authentication policy evaluation.
type Result struct {
	state     ResultState
	principal Principal
}

// NewAuthenticatedResult creates a result for a concrete authenticated identity.
func NewAuthenticatedResult(principal Principal) (Result, error) {
	if principal.IsAnonymous() {
		return Result{}, fmt.Errorf("%w: authenticated result requires an identity", ErrInvalidPrincipal)
	}
	return Result{state: ResultAuthenticated, principal: principal}, nil
}

// AnonymousResult creates an explicit anonymous result for optional routes.
func AnonymousResult() Result {
	return Result{state: ResultAnonymous, principal: AnonymousPrincipal()}
}

// State returns the result state.
func (r Result) State() ResultState { return r.state }

// Principal returns the identity and whether it is authenticated.
func (r Result) Principal() (Principal, bool) {
	return r.principal, r.state == ResultAuthenticated && !r.principal.IsAnonymous()
}

// Authenticator validates one typed credential.
type Authenticator interface {
	Authenticate(context.Context, Credential) (Result, error)
}

type principalContextKey struct{}

// ContextWithPrincipal returns a child context containing principal.
func ContextWithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

// PrincipalFromContext retrieves a principal stored by ContextWithPrincipal.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}
