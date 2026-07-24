package gomath

import "fmt"

// Limits bounds parsing, formatting, and potentially expensive arithmetic.
// It is copied by value and contains no mutable shared state.
type Limits struct {
	MaxInputDigits       int
	MaxOutputDigits      int
	MaxExponentMagnitude int32
	MaxPrecision         uint32
	MaxPowerExponent     uint64
	MaxRootDegree        uint32
	MaxRandomBits        int
	MaxRandomAttempts    int
	MaxIntermediateBits  int
	MaxDecimalExpansion  int
	MaxDiagnosticBytes   int
}

// DefaultLimits returns conservative general-purpose resource bounds.
func DefaultLimits() Limits {
	return Limits{
		MaxInputDigits:       100_000,
		MaxOutputDigits:      100_000,
		MaxExponentMagnitude: 1_000_000,
		MaxPrecision:         100_000,
		MaxPowerExponent:     1_000_000,
		MaxRootDegree:        1_000_000,
		MaxRandomBits:        1_000_000,
		MaxRandomAttempts:    128,
		MaxIntermediateBits:  4_000_000,
		MaxDecimalExpansion:  100_000,
		MaxDiagnosticBytes:   256,
	}
}

// Validate reports whether every limit is positive.
func (l Limits) Validate() error {
	if l.MaxInputDigits <= 0 {
		return fmt.Errorf("%w: MaxInputDigits must be positive", ErrInvalidArgument)
	}
	if l.MaxOutputDigits <= 0 {
		return fmt.Errorf("%w: MaxOutputDigits must be positive", ErrInvalidArgument)
	}
	if l.MaxExponentMagnitude <= 0 {
		return fmt.Errorf("%w: MaxExponentMagnitude must be positive", ErrInvalidArgument)
	}
	if l.MaxPrecision == 0 {
		return fmt.Errorf("%w: MaxPrecision must be positive", ErrInvalidArgument)
	}
	if l.MaxPowerExponent == 0 {
		return fmt.Errorf("%w: MaxPowerExponent must be positive", ErrInvalidArgument)
	}
	if l.MaxRootDegree == 0 {
		return fmt.Errorf("%w: MaxRootDegree must be positive", ErrInvalidArgument)
	}
	if l.MaxRandomBits <= 0 {
		return fmt.Errorf("%w: MaxRandomBits must be positive", ErrInvalidArgument)
	}
	if l.MaxRandomAttempts <= 0 {
		return fmt.Errorf("%w: MaxRandomAttempts must be positive", ErrInvalidArgument)
	}
	if l.MaxIntermediateBits <= 0 {
		return fmt.Errorf("%w: MaxIntermediateBits must be positive", ErrInvalidArgument)
	}
	if l.MaxDecimalExpansion <= 0 {
		return fmt.Errorf("%w: MaxDecimalExpansion must be positive", ErrInvalidArgument)
	}
	if l.MaxDiagnosticBytes <= 0 {
		return fmt.Errorf("%w: MaxDiagnosticBytes must be positive", ErrInvalidArgument)
	}

	return nil
}
