package money

import (
	"context"
	"fmt"
	"strings"

	"github.com/faustbrian/golib/pkg/math/decimal"
)

// MaxAmountDigits bounds coefficient input and diagnostic output.
const MaxAmountDigits = 256

// Amount is an immutable bounded monetary decimal. Exact decimal arithmetic is
// delegated to math; Amount adds only monetary resource policy.
type Amount struct{ value decimal.Decimal }

// ParseAmount parses strict decimal text without a float conversion.
func ParseAmount(input string) (Amount, error) {
	value, err := decimal.ParseWithOptions(input, decimal.ParseOptions{Limits: arithmeticLimits()})
	if err != nil {
		return Amount{}, fmt.Errorf("money: parse amount: %w", err)
	}

	return AmountFromDecimal(value)
}

// AmountFromDecimal applies monetary bounds to an exact math Decimal.
func AmountFromDecimal(value decimal.Decimal) (Amount, error) {
	if value.Scale() < 0 || value.Scale() > int32(MaxScale) {
		return Amount{}, ErrAmountLimit
	}
	digits := strings.TrimPrefix(strings.ReplaceAll(value.String(), ".", ""), "-")
	if len(digits) > MaxAmountDigits {
		return Amount{}, ErrAmountLimit
	}

	return Amount{value: value}, nil
}

// Decimal returns the immutable exact math value.
func (amount Amount) Decimal() decimal.Decimal { return amount.value }

// String returns exact non-exponent decimal text.
func (amount Amount) String() string { return amount.value.String() }

// Scale returns the retained number of fractional places.
func (amount Amount) Scale() int32 { return amount.value.Scale() }

// Sign returns -1, 0, or +1.
func (amount Amount) Sign() int { return amount.value.Sign() }

// IsZero reports numeric zero.
func (amount Amount) IsZero() bool { return amount.value.IsZero() }

// Equal reports numeric equality.
func (amount Amount) Equal(other Amount) bool { return amount.value.Equal(other.value) }

func (amount Amount) add(other Amount) (Amount, error) {
	value, err := amount.value.AddExact(context.Background(), other.value, arithmeticLimits())
	if err != nil {
		return Amount{}, err
	}

	return AmountFromDecimal(value)
}

func (amount Amount) sub(other Amount) (Amount, error) {
	value, err := amount.value.SubExact(context.Background(), other.value, arithmeticLimits())
	if err != nil {
		return Amount{}, err
	}

	return AmountFromDecimal(value)
}

func (amount Amount) neg() Amount { return Amount{value: amount.value.Neg()} }

func (amount Amount) abs() Amount { return Amount{value: amount.value.Abs()} }

func (amount Amount) cmp(other Amount) int { return amount.value.Cmp(other.value) }
