package validation

import "errors"

var (
	// ErrInvalid marks a non-empty validation result containing errors.
	ErrInvalid = errors.New("validation failed")
	// ErrLimitExceeded marks rejected work that exceeded a configured bound.
	ErrLimitExceeded = errors.New("validation limit exceeded")
	// ErrInvalidLimit marks an invalid Limits configuration.
	ErrInvalidLimit = errors.New("invalid validation limit")
	// ErrValidatorPanic is the safe cause used for an isolated custom panic.
	ErrValidatorPanic = errors.New("validator panicked")
	// ErrInvalidViolation marks an unsafe or malformed custom diagnostic.
	ErrInvalidViolation = errors.New("invalid validation diagnostic")
)

// InvalidError exposes a validation report through errors.As.
type InvalidError struct{ report Report }

// Error returns a value-safe summary.
func (e *InvalidError) Error() string { return e.report.String() }

// Unwrap makes InvalidError compatible with errors.Is and ErrInvalid.
func (e *InvalidError) Unwrap() error { return ErrInvalid }

// Report returns the immutable validation report.
func (e *InvalidError) Report() Report { return e.report }
