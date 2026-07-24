// Package adapter names middleware owned by sibling packages without
// reimplementing their policy or state machines.
package adapter

import (
	"errors"
	"fmt"
	"net/http"

	middleware "github.com/faustbrian/golib/pkg/http-middleware"
)

// Concern is a bounded ownership and introspection name.
type Concern string

const (
	// RequestID identifies request identifier ownership.
	RequestID Concern = "request-id"
	// Recovery identifies panic recovery ownership.
	Recovery Concern = "recovery"
	// BodyLimit identifies request body limit ownership.
	BodyLimit Concern = "body-limit"
	// Authentication identifies authentication middleware ownership.
	Authentication Concern = "authentication"
	// Authorization identifies authorization middleware ownership.
	Authorization Concern = "authorization"
	// RateLimit identifies rate-limit middleware ownership.
	RateLimit Concern = "rate-limit"
	// Idempotency identifies idempotency middleware ownership.
	Idempotency Concern = "idempotency"
	// Telemetry identifies telemetry middleware ownership.
	Telemetry Concern = "telemetry"
)

// ErrDuplicateOwnership identifies a concern owned by two installed stacks.
var ErrDuplicateOwnership = errors.New("adapter: duplicate ownership")

// OwnershipError identifies an overlapping service and chain concern.
type OwnershipError struct{ Concern Concern }

func (e *OwnershipError) Error() string {
	return fmt.Sprintf("adapter: %s is installed by both service and the explicit chain", e.Concern)
}
func (e *OwnershipError) Unwrap() error { return ErrDuplicateOwnership }

// Named adapts an owning package's standard middleware into an inspectable
// descriptor. It never changes the middleware's behavior.
func Named(concern Concern, item func(http.Handler) http.Handler) (middleware.Descriptor, error) {
	if !validConcern(concern) {
		return middleware.Descriptor{}, fmt.Errorf("adapter: invalid concern %q", concern)
	}
	return middleware.Named(string(concern), middleware.Middleware(item))
}

// GoServiceDefaults lists the transport concerns currently owned by the
// service default server stack. Callers must validate before combining it
// with an explicit chain.
func GoServiceDefaults() []Concern { return []Concern{Recovery, RequestID, BodyLimit} }

// ValidateGoService rejects an explicit chain that duplicates a concern the
// selected service configuration owns.
func ValidateGoService(chain middleware.Chain, serviceOwns []Concern) error {
	owned := make(map[string]Concern, len(serviceOwns))
	for _, concern := range serviceOwns {
		if !validConcern(concern) {
			return fmt.Errorf("adapter: invalid service concern %q", concern)
		}
		owned[string(concern)] = concern
	}
	for _, descriptor := range chain.Descriptors() {
		if concern, exists := owned[descriptor.Name()]; exists {
			return &OwnershipError{Concern: concern}
		}
	}
	return nil
}

func validConcern(concern Concern) bool {
	switch concern {
	case RequestID, Recovery, BodyLimit, Authentication, Authorization, RateLimit, Idempotency, Telemetry:
		return true
	default:
		return false
	}
}
