package money

import (
	"context"
	"fmt"
	"math/big"

	"github.com/faustbrian/golib/pkg/international/currency"
	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/rational"
)

// RationalMoney is an immutable exact fractional monetary result. It must be
// rounded explicitly before persistence as fixed-context Money.
type RationalMoney struct {
	amount   rational.Rational
	currency currency.Code
	source   Context
}

// Ratio is an immutable exact signed relationship between compatible monetary
// values.
type Ratio struct {
	value rational.Rational
	valid bool
}

// Rational returns the exact math fraction.
func (ratio Ratio) Rational() rational.Rational { return ratio.value }

// String returns the normalized fraction or empty text for an absent ratio.
func (ratio Ratio) String() string {
	if !ratio.valid {
		return ""
	}

	return ratio.value.String()
}

// RoundingResult describes whether an explicit boundary discarded value.
type RoundingResult struct{ conditions gomath.Condition }

// Conditions returns the math arithmetic conditions.
func (result RoundingResult) Conditions() gomath.Condition { return result.conditions }

// Inexact reports whether nonzero value was discarded.
func (result RoundingResult) Inexact() bool {
	return result.conditions.Has(gomath.ConditionInexact)
}

// String returns the normalized exact fraction and currency code.
func (money RationalMoney) String() string {
	if money.currency.IsZero() {
		return ""
	}

	return money.amount.String() + " " + money.currency.String()
}

// Rational returns the immutable exact math value.
func (money RationalMoney) Rational() rational.Rational { return money.amount }

// Currency returns the monetary identity.
func (money RationalMoney) Currency() currency.Code { return money.currency }

// Round creates fixed-context Money at an explicit rounding boundary.
func (money RationalMoney) Round(target Context, mode gomath.RoundingMode) (Money, RoundingResult, error) {
	if money.currency.IsZero() || target.IsZero() || !mode.Valid() {
		return Money{}, RoundingResult{}, ErrInvalidMoney
	}
	if target.kind == ContextAutomatic {
		scale, err := terminatingScale(money.amount)
		if err != nil {
			return Money{}, RoundingResult{}, err
		}
		target.scale = scale
	}
	if target.kind == ContextDefault && target.currency != money.currency {
		return Money{}, RoundingResult{}, ErrContextMismatch
	}
	text, conditions, err := roundedDecimal(money.amount, target, mode)
	if err != nil {
		return Money{}, RoundingResult{}, fmt.Errorf("money: round rational: %w", err)
	}
	value, err := Parse(text, money.currency, target)
	if err != nil {
		return Money{}, RoundingResult{}, err
	}

	return value, RoundingResult{conditions: conditions}, nil
}

func terminatingScale(value rational.Rational) (uint8, error) {
	denominator := value.Denominator()
	two := big.NewInt(2)
	five := big.NewInt(5)
	remainder := new(big.Int)
	twos, fives := uint8(0), uint8(0)
	for twos <= MaxScale {
		quotient, _ := new(big.Int).QuoRem(denominator, two, remainder)
		if remainder.Sign() != 0 {
			break
		}
		denominator = quotient
		twos++
	}
	for fives <= MaxScale {
		quotient, _ := new(big.Int).QuoRem(denominator, five, remainder)
		if remainder.Sign() != 0 {
			break
		}
		denominator = quotient
		fives++
	}
	if denominator.Cmp(big.NewInt(1)) != 0 || twos > MaxScale || fives > MaxScale {
		return 0, ErrPrecisionLoss
	}
	if fives > twos {
		return fives, nil
	}

	return twos, nil
}

func roundedDecimal(amount rational.Rational, target Context, mode gomath.RoundingMode) (string, gomath.Condition, error) {
	if target.kind != ContextCash {
		return amount.Decimal(int(target.scale), mode, arithmeticLimits())
	}

	denominator := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(target.scale)), nil)
	step := mustInvariant(rational.NewChecked(
		new(big.Int).SetUint64(target.cashStep), denominator, arithmeticLimits(),
	))
	var err error
	units, err := amount.Quo(context.Background(), step, arithmeticLimits())
	if err != nil {
		return "", 0, err
	}
	integerText, conditions, err := units.Decimal(0, mode, arithmeticLimits())
	if err != nil {
		return "", 0, err
	}
	integer := mustInvariant(rational.Parse(integerText, arithmeticLimits()))
	rounded := mustInvariant(integer.Mul(context.Background(), step, arithmeticLimits()))
	text, finalConditions, err := rounded.Decimal(int(target.scale), mode, arithmeticLimits())

	return text, conditions | finalConditions, err
}

// Mul returns an exact rational result without applying the Money context's
// fixed scale.
func (money Money) Mul(ctx context.Context, rate Rate) (RationalMoney, error) {
	if !money.Valid() {
		return RationalMoney{}, ErrInvalidMoney
	}
	if !rate.Valid() {
		return RationalMoney{}, ErrInvalidRate
	}
	amount, err := rationalFromAmount(money.amount)
	if err != nil {
		return RationalMoney{}, err
	}
	product, err := amount.Mul(ctx, rate.value, arithmeticLimits())
	if err != nil {
		return RationalMoney{}, fmt.Errorf("money: multiply: %w", err)
	}

	return RationalMoney{amount: product, currency: money.currency, source: money.context}, nil
}

// Quo divides by an exact nonzero rate without rounding.
func (money Money) Quo(ctx context.Context, rate Rate) (RationalMoney, error) {
	if !money.Valid() {
		return RationalMoney{}, ErrInvalidMoney
	}
	if !rate.Valid() || rate.IsZero() {
		return RationalMoney{}, ErrInvalidRate
	}
	amount, err := rationalFromAmount(money.amount)
	if err != nil {
		return RationalMoney{}, err
	}
	quotient, err := amount.Quo(ctx, rate.value, arithmeticLimits())
	if err != nil {
		return RationalMoney{}, fmt.Errorf("money: divide: %w", err)
	}

	return RationalMoney{amount: quotient, currency: money.currency, source: money.context}, nil
}

// Ratio returns money/other exactly after verifying currency and context.
func (money Money) Ratio(ctx context.Context, other Money) (Ratio, error) {
	if err := money.compatible(other); err != nil {
		return Ratio{}, err
	}
	left, err := rationalFromAmount(money.amount)
	if err != nil {
		return Ratio{}, err
	}
	right, err := rationalFromAmount(other.amount)
	if err != nil {
		return Ratio{}, err
	}
	value, err := left.Quo(ctx, right, arithmeticLimits())
	if err != nil {
		return Ratio{}, fmt.Errorf("money: ratio: %w", err)
	}

	return Ratio{value: value, valid: true}, nil
}
