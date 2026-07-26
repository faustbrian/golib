package password

import (
	"errors"
	"fmt"
)

var (
	// ErrMismatch reports that a valid encoded hash does not match the password.
	ErrMismatch = errors.New("password: mismatch")
	// ErrMalformedHash reports invalid or non-canonical encoded-hash syntax.
	ErrMalformedHash = errors.New("password: malformed encoded hash")
	// ErrUnsupportedAlgorithm reports an encoded algorithm without an adapter.
	ErrUnsupportedAlgorithm = errors.New("password: unsupported algorithm")
	// ErrUnsupportedVersion reports an unsupported algorithm encoding version.
	ErrUnsupportedVersion = errors.New("password: unsupported version")
	// ErrInvalidPolicy reports an unsafe or incomplete configuration.
	ErrInvalidPolicy = errors.New("password: invalid policy")
	// ErrResourceRejected reports input or parameters outside configured bounds.
	ErrResourceRejected = errors.New("password: resource limit rejected")
	// ErrEntropy reports failure to obtain a complete cryptographic salt.
	ErrEntropy = errors.New("password: entropy failure")
	// ErrAdmission reports that the bounded operation queue is full.
	ErrAdmission = errors.New("password: operation not admitted")
	// ErrCanceled classifies cancellation before or while waiting for admission.
	ErrCanceled = errors.New("password: operation canceled")
	// ErrClosed reports that admission shutdown has begun.
	ErrClosed = errors.New("password: admission closed")
)

// Error is a classified, secret-safe password operation error. Error omits
// Cause text; callers can inspect Kind and Cause through errors.Is/errors.As.
type Error struct {
	kind      error
	operation string
	cause     error
}

func newError(kind error, operation string, cause error) *Error {
	return &Error{kind: kind, operation: operation, cause: cause}
}

// Kind returns the stable package error sentinel.
func (e *Error) Kind() error { return e.kind }

// Operation returns the bounded non-secret operation label.
func (e *Error) Operation() string { return e.operation }

// Cause returns the underlying cancellation, entropy, or primitive error.
// Its text is never included in Error.
func (e *Error) Cause() error { return e.cause }

// Error returns a stable classification without formatting Cause.
func (e *Error) Error() string { return fmt.Sprintf("password: %s: %v", e.operation, e.kind) }

// Unwrap exposes both classification and cause to errors.Is/errors.As.
func (e *Error) Unwrap() []error {
	if e.cause == nil {
		return []error{e.kind}
	}
	return []error{e.kind, e.cause}
}
