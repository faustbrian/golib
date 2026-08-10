package schemaregistry

import "errors"

var (
	// ErrUnauthorized marks authentication or authorization rejection.
	ErrUnauthorized = errors.New("schema registry: unauthorized")
	// ErrIncompatible marks provider-enforced schema incompatibility.
	ErrIncompatible = errors.New("schema registry: incompatible")
	// ErrRejected marks a definitive provider rejection other than
	// incompatibility or authorization.
	ErrRejected = errors.New("schema registry: rejected")
	// ErrUnknownOutcome marks an operation whose effect cannot be determined.
	ErrUnknownOutcome = errors.New("schema registry: unknown outcome")
)
