package money

import (
	"fmt"
	"strings"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/rational"
)

// MaxRateMagnitude bounds generic multiplication and conversion rates.
const MaxRateMagnitude int64 = 1_000_000

// Rate is an immutable, exact, nonnegative rational multiplier.
type Rate struct {
	value rational.Rational
	valid bool
}

// ParseRate parses either exact decimal text or a numerator/denominator pair.
func ParseRate(input string) (Rate, error) {
	limits := arithmeticLimits()
	var value rational.Rational
	var err error
	if strings.Contains(input, "/") {
		value, err = rational.Parse(input, limits)
	} else {
		amount, amountErr := ParseAmount(input)
		if amountErr != nil {
			err = amountErr
		} else {
			value, err = rationalFromAmount(amount)
		}
	}
	if err != nil {
		return Rate{}, fmt.Errorf("%w: %v", ErrInvalidRate, err)
	}
	maximum, _ := rational.New(MaxRateMagnitude, 1)
	if value.Sign() < 0 || value.Cmp(maximum) > 0 {
		return Rate{}, ErrInvalidRate
	}

	return Rate{value: value, valid: true}, nil
}

// Rational returns the immutable exact math value.
func (rate Rate) Rational() rational.Rational { return rate.value }

// String returns the normalized exact fraction.
func (rate Rate) String() string { return rate.value.String() }

// IsZero reports whether the multiplier is zero.
func (rate Rate) IsZero() bool { return rate.valid && rate.value.Sign() == 0 }

// Valid reports whether rate was constructed through a validating API.
func (rate Rate) Valid() bool { return rate.valid }

func rationalFromAmount(amount Amount) (rational.Rational, error) {
	return rational.FromBig(amount.Decimal().BigRat(), arithmeticLimits())
}

func arithmeticLimits() gomath.Limits {
	limits := gomath.DefaultLimits()
	limits.MaxInputDigits = MaxAmountDigits
	limits.MaxOutputDigits = MaxAmountDigits
	limits.MaxDecimalExpansion = int(MaxScale)
	limits.MaxIntermediateBits = 8192
	limits.MaxDiagnosticBytes = 256

	return limits
}
