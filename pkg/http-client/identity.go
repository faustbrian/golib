package httpclient

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

var (
	// ErrInvalidIdentifier indicates invalid identity generation policy.
	ErrInvalidIdentifier = errors.New("invalid HTTP operation identifier")
	// ErrInvalidOperationIdentity indicates a malformed logical operation ID.
	ErrInvalidOperationIdentity = errors.New("invalid HTTP operation identity")
)

const (
	defaultIdentifierEntropyBits = 128
	minimumIdentifierEntropyBits = 96
	maximumIdentifierEntropyBits = 512
	maximumOperationIDLength     = 128
	operationIdentityPriority    = -2000
)

// GeneratedIdentifier contains a generated value and its claimed entropy.
type GeneratedIdentifier struct {
	Value       string
	EntropyBits int
}

// IdentifierGenerator creates independent operation or idempotency values.
// Implementations must be safe for concurrent use and honor cancellation.
type IdentifierGenerator interface {
	Generate(context.Context) (GeneratedIdentifier, error)
}

// IdentifierGeneratorFunc adapts a function to IdentifierGenerator.
type IdentifierGeneratorFunc func(context.Context) (GeneratedIdentifier, error)

// Generate implements IdentifierGenerator.
func (function IdentifierGeneratorFunc) Generate(ctx context.Context) (GeneratedIdentifier, error) {
	return function(ctx)
}

// OperationIdentityProvenance identifies who selected a logical operation ID.
type OperationIdentityProvenance uint8

const (
	// IdentityGenerated indicates client-generated operation identity.
	IdentityGenerated OperationIdentityProvenance = iota
	// IdentityCaller indicates an explicitly supplied caller identity.
	IdentityCaller
)

// OperationIdentity is stable across all physical attempts in one Client.Do.
type OperationIdentity struct {
	ID         string
	Provenance OperationIdentityProvenance
}

// OperationIdentityError reports generation or validation failure without
// rendering the generated value or underlying cause.
type OperationIdentityError struct {
	Cause error
}

// Error implements error without rendering identity material.
func (*OperationIdentityError) Error() string {
	return "HTTP operation identity assignment failed"
}

// Unwrap returns the generation or validation failure.
func (err *OperationIdentityError) Unwrap() error {
	return err.Cause
}

type operationIdentityContextKey struct{}

// NewRandomIdentifierGenerator returns a cryptographically random URL-safe
// generator. Entropy must be between 96 and 512 bits.
func NewRandomIdentifierGenerator(entropyBits int) (IdentifierGenerator, error) {
	if entropyBits < minimumIdentifierEntropyBits || entropyBits > maximumIdentifierEntropyBits {
		return nil, fmt.Errorf("%w: entropy must be between 96 and 512 bits", ErrInvalidIdentifier)
	}

	return newRandomIdentifierGenerator(entropyBits), nil
}

func newRandomIdentifierGenerator(entropyBits int) IdentifierGenerator {
	return randomIdentifierGenerator{bytes: (entropyBits + 7) / 8, reader: rand.Reader}
}

type randomIdentifierGenerator struct {
	bytes  int
	reader io.Reader
}

func (generator randomIdentifierGenerator) Generate(ctx context.Context) (GeneratedIdentifier, error) {
	if ctx == nil {
		return GeneratedIdentifier{}, fmt.Errorf("%w: generation context is nil", ErrInvalidIdentifier)
	}
	if err := ctx.Err(); err != nil {
		return GeneratedIdentifier{}, err
	}
	random := make([]byte, generator.bytes)
	if _, err := io.ReadFull(generator.reader, random); err != nil {
		return GeneratedIdentifier{}, err
	}
	if err := ctx.Err(); err != nil {
		return GeneratedIdentifier{}, err
	}

	return GeneratedIdentifier{
		Value:       base64.RawURLEncoding.EncodeToString(random),
		EntropyBits: len(random) * 8,
	}, nil
}

// WithOperationIdentity returns a context carrying validated caller identity.
func WithOperationIdentity(ctx context.Context, identifier string) (context.Context, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: context is nil", ErrInvalidOperationIdentity)
	}
	if !validOperationID(identifier) {
		return nil, ErrInvalidOperationIdentity
	}

	return context.WithValue(ctx, operationIdentityContextKey{}, OperationIdentity{
		ID: identifier, Provenance: IdentityCaller,
	}), nil
}

// OperationIdentityFromContext returns resolved logical operation identity.
func OperationIdentityFromContext(ctx context.Context) (OperationIdentity, bool) {
	if ctx == nil {
		return OperationIdentity{}, false
	}
	identity, ok := ctx.Value(operationIdentityContextKey{}).(OperationIdentity)

	return identity, ok
}

func newOperationIdentityMiddleware(generator IdentifierGenerator) (Middleware, error) {
	if generator == nil {
		generator = randomIdentifierGenerator{bytes: defaultIdentifierEntropyBits / 8, reader: rand.Reader}
	} else if nilLike(generator) {
		return Middleware{}, fmt.Errorf("%w: generator is nil", ErrInvalidIdentifier)
	}

	return Middleware{
		information: MiddlewareInfo{
			Name:     "httpclient.operation-identity",
			Scope:    ScopeOperation,
			Layer:    MiddlewareClient,
			Stage:    StageRequest,
			Priority: operationIdentityPriority,
		},
		around: func(request *http.Request, next Next) (*http.Response, error) {
			identity, ok := OperationIdentityFromContext(request.Context())
			if ok {
				if !validOperationID(identity.ID) || identity.Provenance != IdentityCaller {
					return nil, &OperationIdentityError{Cause: ErrInvalidOperationIdentity}
				}
			} else {
				generated, err := generator.Generate(request.Context())
				if err != nil {
					return nil, &OperationIdentityError{Cause: err}
				}
				if generated.EntropyBits < minimumIdentifierEntropyBits || !validOperationID(generated.Value) {
					return nil, &OperationIdentityError{Cause: ErrInvalidOperationIdentity}
				}
				identity = OperationIdentity{ID: generated.Value, Provenance: IdentityGenerated}
			}

			return next(request.WithContext(context.WithValue(
				request.Context(), operationIdentityContextKey{}, identity,
			)))
		},
	}, nil
}

func validOperationID(identifier string) bool {
	if identifier == "" || len(identifier) > maximumOperationIDLength {
		return false
	}
	for _, character := range identifier {
		if character > 0x7f || !operationIDCharacter(byte(character)) {
			return false
		}
	}

	return true
}

func operationIDCharacter(character byte) bool {
	return character >= 'A' && character <= 'Z' ||
		character >= 'a' && character <= 'z' ||
		character >= '0' && character <= '9' ||
		strings.ContainsRune("-._~", rune(character))
}
