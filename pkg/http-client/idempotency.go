package httpclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

var (
	// ErrInvalidIdempotencyPolicy indicates malformed endpoint policy.
	ErrInvalidIdempotencyPolicy = errors.New("invalid HTTP idempotency policy")
	// ErrInvalidIdempotencyKey indicates a malformed or ambiguous key.
	ErrInvalidIdempotencyKey = errors.New("invalid HTTP idempotency key")
	// ErrIdempotencyKeyRequired indicates caller-required policy without a key.
	ErrIdempotencyKeyRequired = errors.New("HTTP idempotency key is required")
)

const (
	defaultIdempotencyHeader      = "Idempotency-Key"
	defaultIdempotencyMaxLength   = 255
	maximumIdempotencyKeyLength   = 1024
	idempotencyMiddlewarePriority = -1500
)

// IdempotencyMode controls whether a missing caller key is generated.
type IdempotencyMode uint8

const (
	// IdempotencyGenerateIfMissing uses a caller key or generates one.
	IdempotencyGenerateIfMissing IdempotencyMode = iota
	// IdempotencyRequireCaller rejects operations without a caller key.
	IdempotencyRequireCaller
)

// IdempotencyProvenance identifies how one operation key was selected.
type IdempotencyProvenance uint8

const (
	// IdempotencyGenerated indicates a generated operation key.
	IdempotencyGenerated IdempotencyProvenance = iota
	// IdempotencyCallerHeader indicates a key supplied through the endpoint header.
	IdempotencyCallerHeader
	// IdempotencyCallerContext indicates a key supplied through context.
	IdempotencyCallerContext
)

// IdempotencyKey is stable for one logical operation. String and GoString
// redact Value; callers must access Value explicitly when setting a request.
type IdempotencyKey struct {
	Value      string
	Provenance IdempotencyProvenance
}

// String returns a redacted representation.
func (key IdempotencyKey) String() string {
	return fmt.Sprintf("[REDACTED idempotency key provenance=%d]", key.Provenance)
}

// GoString returns a redacted Go-syntax representation.
func (key IdempotencyKey) GoString() string {
	return fmt.Sprintf("httpclient.IdempotencyKey{Value:[REDACTED], Provenance:%d}", key.Provenance)
}

// IdempotencyAttemptPolicy decides whether an attempt still represents the
// original operation for key propagation.
type IdempotencyAttemptPolicy interface {
	PreserveKey(original *http.Request, attempt *http.Request) bool
}

// IdempotencyAttemptPolicyFunc adapts a function to IdempotencyAttemptPolicy.
type IdempotencyAttemptPolicyFunc func(original *http.Request, attempt *http.Request) bool

// PreserveKey implements IdempotencyAttemptPolicy.
func (function IdempotencyAttemptPolicyFunc) PreserveKey(
	original *http.Request,
	attempt *http.Request,
) bool {
	return function(original, attempt)
}

// IdempotencyOptions configures explicit endpoint idempotency middleware.
type IdempotencyOptions struct {
	Name               string
	Layer              MiddlewareLayer
	Priority           int
	Mode               IdempotencyMode
	Header             string
	MaximumLength      int
	MinimumEntropyBits int
	Generator          IdentifierGenerator
	AttemptPolicy      IdempotencyAttemptPolicy
}

// IdempotencyError reports key selection or propagation failure without
// rendering the key, generated candidate, or underlying cause.
type IdempotencyError struct {
	Cause error
}

// Error implements error without rendering idempotency material.
func (*IdempotencyError) Error() string {
	return "HTTP idempotency policy failed"
}

// Unwrap returns the policy, generation, or validation failure.
func (err *IdempotencyError) Unwrap() error {
	return err.Cause
}

type idempotencyKeyContextKey struct{}
type idempotencyStateContextKey struct{}

type idempotencyState struct {
	key       IdempotencyKey
	original  *http.Request
	header    string
	attempt   IdempotencyAttemptPolicy
	operation OperationIdentity
}

// WithIdempotencyKey returns a context carrying a caller-supplied key. The
// endpoint policy still applies its configured maximum length.
func WithIdempotencyKey(ctx context.Context, key string) (context.Context, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: context is nil", ErrInvalidIdempotencyKey)
	}
	if !validIdempotencyKey(key, maximumIdempotencyKeyLength) {
		return nil, ErrInvalidIdempotencyKey
	}

	return context.WithValue(ctx, idempotencyKeyContextKey{}, IdempotencyKey{
		Value: key, Provenance: IdempotencyCallerContext,
	}), nil
}

// IdempotencyKeyFromContext returns the resolved logical-operation key.
func IdempotencyKeyFromContext(ctx context.Context) (IdempotencyKey, bool) {
	if ctx == nil {
		return IdempotencyKey{}, false
	}
	key, ok := ctx.Value(idempotencyKeyContextKey{}).(IdempotencyKey)

	return key, ok
}

func idempotencyPolicyApplied(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	state, ok := ctx.Value(idempotencyStateContextKey{}).(idempotencyState)
	if !ok {
		return false
	}
	key, keyOK := IdempotencyKeyFromContext(ctx)
	identity, identityOK := OperationIdentityFromContext(ctx)

	return keyOK && identityOK && key == state.key && identity == state.operation
}

// NewIdempotencyMiddleware creates paired operation and attempt middleware.
// Register it only for endpoints whose provider contract supports a key.
func NewIdempotencyMiddleware(options IdempotencyOptions) ([]Middleware, error) {
	resolved, err := resolveIdempotencyOptions(options)
	if err != nil {
		return nil, err
	}
	operation, err := NewRequestMiddleware(MiddlewareOptions{
		Name:     options.Name,
		Scope:    ScopeOperation,
		Layer:    options.Layer,
		Priority: idempotencyMiddlewarePriority + options.Priority,
	}, func(request *http.Request, next Next) (*http.Response, error) {
		identity, ok := OperationIdentityFromContext(request.Context())
		if !ok {
			return nil, &IdempotencyError{Cause: ErrInvalidOperationIdentity}
		}
		key, keyErr := selectIdempotencyKey(request, resolved)
		if keyErr != nil {
			return nil, &IdempotencyError{Cause: keyErr}
		}
		request.Header.Del(resolved.header)
		state := idempotencyState{
			key:       key,
			original:  snapshotRequest(request),
			header:    resolved.header,
			attempt:   resolved.attempt,
			operation: identity,
		}
		ctx := context.WithValue(request.Context(), idempotencyKeyContextKey{}, key)
		ctx = context.WithValue(ctx, idempotencyStateContextKey{}, state)

		return next(request.WithContext(ctx))
	})
	if err != nil {
		return nil, err
	}
	attempt := operation
	attempt.information.Scope = ScopeAttempt
	attempt.around = func(request *http.Request, next Next) (*http.Response, error) {
		state, ok := request.Context().Value(idempotencyStateContextKey{}).(idempotencyState)
		if !ok {
			return nil, &IdempotencyError{Cause: ErrInvalidIdempotencyPolicy}
		}
		identity, ok := OperationIdentityFromContext(request.Context())
		if !ok || identity != state.operation {
			return nil, &IdempotencyError{Cause: ErrInvalidOperationIdentity}
		}
		if state.attempt.PreserveKey(snapshotRequest(state.original), snapshotRequest(request)) {
			request.Header.Set(state.header, state.key.Value)
		} else {
			request.Header.Del(state.header)
		}

		return next(request)
	}

	return []Middleware{operation, attempt}, nil
}

type resolvedIdempotencyOptions struct {
	mode           IdempotencyMode
	header         string
	maximumLength  int
	minimumEntropy int
	generator      IdentifierGenerator
	attempt        IdempotencyAttemptPolicy
}

func resolveIdempotencyOptions(options IdempotencyOptions) (resolvedIdempotencyOptions, error) {
	if options.Mode > IdempotencyRequireCaller {
		return resolvedIdempotencyOptions{}, fmt.Errorf("%w: unknown mode", ErrInvalidIdempotencyPolicy)
	}
	header := options.Header
	if header == "" {
		header = defaultIdempotencyHeader
	}
	canonicalHeader, err := validateHeaderName(header)
	if err != nil {
		return resolvedIdempotencyOptions{}, fmt.Errorf("%w: header is malformed", ErrInvalidIdempotencyPolicy)
	}
	maximumLength := options.MaximumLength
	if maximumLength == 0 {
		maximumLength = defaultIdempotencyMaxLength
	}
	if maximumLength < 1 || maximumLength > maximumIdempotencyKeyLength {
		return resolvedIdempotencyOptions{}, fmt.Errorf("%w: maximum length is invalid", ErrInvalidIdempotencyPolicy)
	}
	minimumEntropy := options.MinimumEntropyBits
	if minimumEntropy == 0 {
		minimumEntropy = defaultIdentifierEntropyBits
	}
	if minimumEntropy < minimumIdentifierEntropyBits || minimumEntropy > maximumIdentifierEntropyBits {
		return resolvedIdempotencyOptions{}, fmt.Errorf("%w: minimum entropy is invalid", ErrInvalidIdempotencyPolicy)
	}
	generator := options.Generator
	if options.Mode == IdempotencyRequireCaller {
		if generator != nil {
			return resolvedIdempotencyOptions{}, fmt.Errorf("%w: caller-required policy cannot generate", ErrInvalidIdempotencyPolicy)
		}
	} else if generator == nil {
		generator = newRandomIdentifierGenerator(minimumEntropy)
	} else if nilLike(generator) {
		return resolvedIdempotencyOptions{}, fmt.Errorf("%w: generator is nil", ErrInvalidIdempotencyPolicy)
	}
	attempt := options.AttemptPolicy
	if attempt == nil {
		attempt = sameOriginMethodAttemptPolicy{}
	} else if nilLike(attempt) {
		return resolvedIdempotencyOptions{}, fmt.Errorf("%w: attempt policy is nil", ErrInvalidIdempotencyPolicy)
	}

	return resolvedIdempotencyOptions{
		mode:           options.Mode,
		header:         canonicalHeader,
		maximumLength:  maximumLength,
		minimumEntropy: minimumEntropy,
		generator:      generator,
		attempt:        attempt,
	}, nil
}

func selectIdempotencyKey(request *http.Request, options resolvedIdempotencyOptions) (IdempotencyKey, error) {
	contextKey, contextSupplied := IdempotencyKeyFromContext(request.Context())
	headerValues := request.Header.Values(options.header)
	if len(headerValues) > 1 || contextSupplied && len(headerValues) == 1 {
		return IdempotencyKey{}, ErrInvalidIdempotencyKey
	}
	if contextSupplied {
		if !validIdempotencyKey(contextKey.Value, options.maximumLength) ||
			contextKey.Provenance != IdempotencyCallerContext {
			return IdempotencyKey{}, ErrInvalidIdempotencyKey
		}

		return contextKey, nil
	}
	if len(headerValues) == 1 {
		if !validIdempotencyKey(headerValues[0], options.maximumLength) {
			return IdempotencyKey{}, ErrInvalidIdempotencyKey
		}

		return IdempotencyKey{Value: headerValues[0], Provenance: IdempotencyCallerHeader}, nil
	}
	if options.mode == IdempotencyRequireCaller {
		return IdempotencyKey{}, ErrIdempotencyKeyRequired
	}
	generated, err := options.generator.Generate(request.Context())
	if err != nil {
		return IdempotencyKey{}, err
	}
	if generated.EntropyBits < options.minimumEntropy ||
		!validIdempotencyKey(generated.Value, options.maximumLength) {
		return IdempotencyKey{}, ErrInvalidIdempotencyKey
	}

	return IdempotencyKey{Value: generated.Value, Provenance: IdempotencyGenerated}, nil
}

type sameOriginMethodAttemptPolicy struct{}

func (sameOriginMethodAttemptPolicy) PreserveKey(original *http.Request, attempt *http.Request) bool {
	if original == nil || attempt == nil || original.Method != attempt.Method {
		return false
	}
	originalOrigin, originalErr := requestOrigin(original)
	attemptOrigin, attemptErr := requestOrigin(attempt)

	return originalErr == nil && attemptErr == nil && originalOrigin == attemptOrigin
}

func validIdempotencyKey(key string, maximumLength int) bool {
	if key == "" || len(key) > maximumLength {
		return false
	}
	for _, character := range key {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}

	return true
}
