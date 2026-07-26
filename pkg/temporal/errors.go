package temporal

import (
	"errors"
	"fmt"
)

var (
	// ErrBounds identifies an unknown or malformed bounds value.
	ErrBounds = errors.New("temporal: invalid bounds")
	// ErrLimit identifies a configured or operational resource limit violation.
	ErrLimit = errors.New("temporal: resource limit exceeded")
	// ErrReversed identifies an interval whose end precedes its start.
	ErrReversed = errors.New("temporal: reversed interval")
	// ErrEmpty identifies an operation that requires a non-empty interval.
	ErrEmpty = errors.New("temporal: empty interval")
	// ErrStep identifies a zero or negative iteration step.
	ErrStep = errors.New("temporal: invalid step")
	// ErrOverflow identifies arithmetic outside the supported representation.
	ErrOverflow = errors.New("temporal: arithmetic overflow")
	// ErrParse identifies malformed or unsupported notation.
	ErrParse = errors.New("temporal: parse error")
	// ErrPrecision identifies input whose fractional precision is unsupported.
	ErrPrecision = errors.New("temporal: precision exceeded")
	// ErrInvalidTime identifies an invalid local time-of-day value.
	ErrInvalidTime = errors.New("temporal: invalid local time")
	// ErrUnsupported identifies a conversion that cannot preserve semantics.
	ErrUnsupported = errors.New("temporal: unsupported operation")
)

// LimitError describes which resource limit was invalid or exceeded.
type LimitError struct {
	Field string
	Value int
	Max   int
}

// Error implements error.
func (e *LimitError) Error() string {
	return fmt.Sprintf("%s: %s=%d (maximum %d)", ErrLimit, e.Field, e.Value, e.Max)
}

// Unwrap makes LimitError discoverable with errors.Is.
func (e *LimitError) Unwrap() error {
	return ErrLimit
}
