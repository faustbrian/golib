// Package correlation provides transport-neutral correlation, request, and
// causation identifiers. Identifiers are diagnostic metadata only; they are
// never authentication, authorization, tenancy, replay, or idempotency proof.
package correlation

import (
	"context"
	"errors"
	"fmt"
)

const defaultMaxLength = 128

// ErrInvalidID reports an identifier that violates its validation policy.
var ErrInvalidID = errors.New("correlation: invalid identifier")

// Policy bounds and validates an identifier. The zero value accepts the
// canonical ASCII alphabet [A-Za-z0-9_-] up to 128 bytes.
type Policy struct {
	MaxLength int
}

// CorrelationID groups work in one logical interaction or workflow.
//
//revive:disable-next-line:exported semantic name intentionally remains explicit
type CorrelationID string

// RequestID identifies one transport request or delivery attempt.
type RequestID string

// CausationID identifies the immediate parent request, message, or event.
type CausationID string

// Values carries the three deliberately distinct identifier semantics.
type Values struct {
	CorrelationID CorrelationID
	RequestID     RequestID
	CausationID   CausationID
}

// ParseCorrelationID validates and returns a correlation identifier.
func ParseCorrelationID(value string, policy Policy) (CorrelationID, error) {
	if err := validate(value, policy); err != nil {
		return "", err
	}
	return CorrelationID(value), nil
}

// ParseRequestID validates and returns a request identifier.
func ParseRequestID(value string, policy Policy) (RequestID, error) {
	if err := validate(value, policy); err != nil {
		return "", err
	}
	return RequestID(value), nil
}

// ParseCausationID validates and returns a causation identifier.
func ParseCausationID(value string, policy Policy) (CausationID, error) {
	if err := validate(value, policy); err != nil {
		return "", err
	}
	return CausationID(value), nil
}

// MustCorrelationID is ParseCorrelationID for static configuration and tests.
func MustCorrelationID(value string, policy Policy) CorrelationID {
	id, err := ParseCorrelationID(value, policy)
	if err != nil {
		panic(err)
	}
	return id
}

// MustRequestID is ParseRequestID for static configuration and tests.
func MustRequestID(value string, policy Policy) RequestID {
	id, err := ParseRequestID(value, policy)
	if err != nil {
		panic(err)
	}
	return id
}

// MustCausationID is ParseCausationID for static configuration and tests.
func MustCausationID(value string, policy Policy) CausationID {
	id, err := ParseCausationID(value, policy)
	if err != nil {
		panic(err)
	}
	return id
}

func (id CorrelationID) String() string { return string(id) }
func (id RequestID) String() string     { return string(id) }
func (id CausationID) String() string   { return string(id) }

type contextKey struct{}

// WithValues returns a derived context carrying a copy of values.
func WithValues(ctx context.Context, values Values) context.Context {
	return context.WithValue(ctx, contextKey{}, values)
}

// FromContext returns values installed by WithValues.
func FromContext(ctx context.Context) (Values, bool) {
	if ctx == nil {
		return Values{}, false
	}
	values, ok := ctx.Value(contextKey{}).(Values)
	return values, ok
}

func validate(value string, policy Policy) error {
	maximum := policy.MaxLength
	if maximum == 0 {
		maximum = defaultMaxLength
	}
	if maximum < 1 || maximum > 1024 || len(value) == 0 || len(value) > maximum {
		return fmt.Errorf("%w: length", ErrInvalidID)
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') && char != '-' && char != '_' {
			return fmt.Errorf("%w: alphabet", ErrInvalidID)
		}
	}
	return nil
}
