package geo

import (
	"errors"
	"fmt"
)

var (
	// ErrRange classifies finite-value and numeric-range violations.
	ErrRange = errors.New("geo: value outside permitted range")
	// ErrTopology classifies structurally invalid geometry.
	ErrTopology = errors.New("geo: invalid topology")
	// ErrCRS classifies invalid or incompatible coordinate reference systems.
	ErrCRS = errors.New("geo: invalid coordinate reference system")
	// ErrEncoding classifies malformed or resource-exhausting encoded input.
	ErrEncoding = errors.New("geo: invalid encoding")
	// ErrUnsupported classifies well-formed operations the package cannot perform.
	ErrUnsupported = errors.New("geo: unsupported operation")
)

// RangeError reports a rejected numeric value and its inclusive bounds.
type RangeError struct {
	ValueName string
	Value     float64
	Minimum   float64
	Maximum   float64
}

func (e *RangeError) Error() string {
	return fmt.Sprintf(
		"geo: %s %v outside inclusive range [%v, %v]",
		e.ValueName,
		e.Value,
		e.Minimum,
		e.Maximum,
	)
}

func (e *RangeError) Unwrap() error { return ErrRange }

// TopologyError reports a geometry invariant violation.
type TopologyError struct {
	Geometry string
	Problem  string
}

func (e *TopologyError) Error() string {
	return fmt.Sprintf("geo: invalid %s topology: %s", e.Geometry, e.Problem)
}

func (e *TopologyError) Unwrap() error { return ErrTopology }

// CRSError reports invalid or incompatible CRS metadata.
type CRSError struct {
	SRID    int32
	Problem string
}

func (e *CRSError) Error() string {
	return fmt.Sprintf("geo: invalid CRS SRID %d: %s", e.SRID, e.Problem)
}

func (e *CRSError) Unwrap() error { return ErrCRS }

// EncodingError reports malformed encoded input without retaining its content.
type EncodingError struct {
	Format  string
	Problem string
	Cause   error
}

func (e *EncodingError) Error() string {
	return fmt.Sprintf("geo: invalid %s encoding: %s", e.Format, e.Problem)
}

func (e *EncodingError) Unwrap() error { return e.Cause }

// Is classifies every EncodingError as ErrEncoding.
func (e *EncodingError) Is(target error) bool { return target == ErrEncoding }

// UnsupportedError reports a recognized operation that is not supported.
type UnsupportedError struct {
	Operation string
	Reason    string
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("geo: unsupported %s: %s", e.Operation, e.Reason)
}

func (e *UnsupportedError) Unwrap() error { return ErrUnsupported }
