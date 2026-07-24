// Package requestid propagates bounded request and correlation identifiers.
// Identifiers are metadata and are never authorization evidence.
package requestid

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/faustbrian/golib/pkg/http-middleware/internal/httpx"
)

// Kind distinguishes identifier purposes.
type Kind string

const (
	// Request identifies a request identifier.
	Request Kind = "request"
	// Correlation identifies a cross-request correlation identifier.
	Correlation Kind = "correlation"
	// Operation identifies an application operation identifier.
	Operation Kind = "operation"
)

// InvalidPolicy controls trusted invalid inbound identifiers.
type InvalidPolicy uint8

const (
	// ReplaceInvalid replaces invalid trusted inbound identifiers.
	ReplaceInvalid InvalidPolicy = iota
	// RejectInvalid rejects invalid trusted inbound identifiers.
	RejectInvalid
)

// ErrInvalidPolicy identifies invalid identifier policy configuration.
var ErrInvalidPolicy = errors.New("requestid: invalid policy")

// ConfigError reports invalid construction input.
// ConfigError reports an invalid identifier policy field.
type ConfigError struct{ Field string }

func (e *ConfigError) Error() string { return fmt.Sprintf("requestid: invalid %s", e.Field) }
func (e *ConfigError) Unwrap() error { return ErrInvalidPolicy }

// Policy is copied by New and is safe to reuse after construction.
type Policy struct {
	Kind           Kind
	Header         string
	ResponseHeader string
	TrustInbound   bool
	Invalid        InvalidPolicy
	MaxLength      int
	Generator      func() (string, error)
}

type contextKey struct{ kind Kind }

// New constructs identifier middleware. Defaults are X-Request-ID, 128 bytes,
// untrusted inbound values, replacement on invalid input, and 128 random bits.
// Empty, non-ASCII, surrounding-space, control-character, and oversized
// values are invalid.
func New(policy Policy) (func(http.Handler) http.Handler, error) {
	if policy.Kind == "" {
		policy.Kind = Request
	}
	if policy.Kind != Request && policy.Kind != Correlation && policy.Kind != Operation {
		return nil, &ConfigError{Field: "kind"}
	}
	if policy.Header == "" {
		if policy.Kind == Correlation {
			policy.Header = "X-Correlation-ID"
		} else {
			policy.Header = "X-Request-ID"
		}
	}
	if policy.ResponseHeader == "" {
		policy.ResponseHeader = policy.Header
	}
	if !validHeaderName(policy.Header) || !validHeaderName(policy.ResponseHeader) {
		return nil, &ConfigError{Field: "header"}
	}
	if policy.MaxLength == 0 {
		policy.MaxLength = 128
	}
	if policy.MaxLength < 1 || policy.MaxLength > 1024 {
		return nil, &ConfigError{Field: "max length"}
	}
	if policy.Invalid > RejectInvalid {
		return nil, &ConfigError{Field: "invalid policy"}
	}
	if policy.Generator == nil {
		policy.Generator = randomIdentifier
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identifier := ""
			values := r.Header.Values(policy.Header)
			if policy.TrustInbound && len(values) == 1 && validIdentifier(values[0], policy.MaxLength) {
				identifier = values[0]
			} else if policy.TrustInbound && len(values) > 0 && policy.Invalid == RejectInvalid {
				httpx.SafeError(w, http.StatusBadRequest, "invalid request identifier\n")
				return
			}
			if identifier == "" {
				generated, err := policy.Generator()
				if err != nil || !validIdentifier(generated, policy.MaxLength) {
					httpx.SafeError(w, http.StatusInternalServerError, "internal server error\n")
					return
				}
				identifier = generated
			}
			w.Header().Set(policy.ResponseHeader, identifier)
			ctx := context.WithValue(r.Context(), contextKey{kind: policy.Kind}, identifier)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}, nil
}

// FromContext returns an identifier stored by this package.
func FromContext(ctx context.Context, kind Kind) (string, bool) {
	value, ok := ctx.Value(contextKey{kind: kind}).(string)
	return value, ok
}

func randomIdentifier() (string, error) {
	return rand.Text(), nil
}

func validIdentifier(value string, maximum int) bool {
	if value == "" || len(value) > maximum || strings.TrimSpace(value) != value {
		return false
	}
	for _, char := range value {
		if char < 0x21 || char > 0x7e {
			return false
		}
	}
	return true
}

func validHeaderName(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if !strings.ContainsRune("!#$%&'*+-.^_`|~0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ", char) {
			return false
		}
	}
	return true
}
