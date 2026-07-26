package timeofday

import (
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
)

// RoundingMode defines fixed-duration rounding direction.
type RoundingMode uint8

const (
	// RoundFloor rounds toward negative infinity.
	RoundFloor RoundingMode = iota
	// RoundNearest rounds to nearest, with exact ties away from zero.
	RoundNearest
	// RoundCeil rounds toward positive infinity.
	RoundCeil
)

// Duration wraps time.Duration with checked arithmetic and explicit division
// remainder semantics.
type Duration struct {
	value time.Duration
}

// NewDuration wraps a standard fixed elapsed duration.
func NewDuration(value time.Duration) Duration {
	return Duration{value: value}
}

// ZeroDuration returns a zero fixed duration.
func ZeroDuration() Duration {
	return Duration{}
}

// Value returns the interoperable time.Duration value.
func (d Duration) Value() time.Duration {
	return d.value
}

// IsZero reports whether the duration is zero.
func (d Duration) IsZero() bool {
	return d.value == 0
}

// Compare returns -1, 0, or 1 by elapsed duration.
func (d Duration) Compare(other Duration) int {
	switch {
	case d.value < other.value:
		return -1
	case d.value > other.value:
		return 1
	default:
		return 0
	}
}

// Add returns the checked sum of d and others.
func (d Duration) Add(others ...Duration) (Duration, error) {
	result := d.value
	for _, other := range others {
		if other.value > 0 && result > time.Duration(1<<63-1)-other.value {
			return Duration{}, temporal.ErrOverflow
		}
		if other.value < 0 && result < time.Duration(-1<<63)-other.value {
			return Duration{}, temporal.ErrOverflow
		}
		result += other.value
	}
	return Duration{value: result}, nil
}

// Negate returns -d or ErrOverflow for the minimum duration.
func (d Duration) Negate() (Duration, error) {
	if d.value == time.Duration(-1<<63) {
		return Duration{}, temporal.ErrOverflow
	}
	return Duration{value: -d.value}, nil
}

// Abs returns the non-negative magnitude or ErrOverflow for the minimum.
func (d Duration) Abs() (Duration, error) {
	if d.value >= 0 {
		return d, nil
	}
	return d.Negate()
}

// Clamp restricts d to the inclusive range minimum through maximum.
func (d Duration) Clamp(minimum, maximum Duration) (Duration, error) {
	if minimum.value > maximum.value {
		return Duration{}, temporal.ErrStep
	}
	if d.value < minimum.value {
		return minimum, nil
	}
	if d.value > maximum.value {
		return maximum, nil
	}
	return d, nil
}

// Multiply returns d times factor with overflow detection.
func (d Duration) Multiply(factor int) (Duration, error) {
	return checkedDurationProduct(d.value, int64(factor))
}

func checkedDurationProduct(value time.Duration, factor int64) (Duration, error) {
	if value == 0 || factor == 0 {
		return Duration{}, nil
	}
	if value == time.Duration(-1<<63) && factor == -1 {
		return Duration{}, temporal.ErrOverflow
	}
	result := value * time.Duration(factor)
	if result/time.Duration(factor) != value {
		return Duration{}, temporal.ErrOverflow
	}
	return Duration{value: result}, nil
}

// Divide returns the quotient and Go-style signed remainder. Division truncates
// toward zero.
func (d Duration) Divide(factor int) (Duration, time.Duration, error) {
	if factor == 0 {
		return Duration{}, 0, temporal.ErrStep
	}
	converted := time.Duration(factor)
	if d.value == time.Duration(-1<<63) && converted == -1 {
		return Duration{}, 0, temporal.ErrOverflow
	}
	return Duration{value: d.value / converted}, d.value % converted, nil
}

// Round rounds d to a positive fixed unit according to mode.
func (d Duration) Round(unit time.Duration, mode RoundingMode) (Duration, error) {
	if unit <= 0 {
		return Duration{}, temporal.ErrStep
	}
	quotient := d.value / unit
	remainder := d.value % unit

	switch mode {
	case RoundFloor:
		if remainder < 0 {
			quotient--
		}
	case RoundCeil:
		if remainder > 0 {
			quotient++
		}
	case RoundNearest:
		magnitude := remainder
		if magnitude < 0 {
			magnitude = -magnitude
		}
		if magnitude > unit/2 || (unit%2 == 0 && magnitude == unit/2) {
			if remainder < 0 {
				quotient--
			} else {
				quotient++
			}
		}
	default:
		return Duration{}, temporal.ErrUnsupported
	}

	return checkedDurationProduct(unit, int64(quotient))
}
