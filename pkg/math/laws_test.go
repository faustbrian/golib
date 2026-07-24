package gomath_test

import (
	"context"
	"math/big"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/bigfloat"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/math/integer"
	"github.com/faustbrian/golib/pkg/math/mathtest"
	"github.com/faustbrian/golib/pkg/math/rational"
)

func TestExactFamiliesObeyAdditionLaws(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	limits := gomath.DefaultLimits()
	integers := []integer.Integer{integer.New(-7), integer.Zero(), integer.New(11)}
	integerAdd := func(left, right integer.Integer) (integer.Integer, error) {
		return left.Add(ctx, right, limits)
	}
	mathtest.Commutative(t, integers, integerAdd)
	mathtest.Associative(t, integers, integerAdd)
	mathtest.Identity(t, integers, integer.Zero(), integerAdd)

	rationals := []rational.Rational{
		mustRational(t, -2, 3), rational.Zero(), mustRational(t, 5, 7),
	}
	rationalAdd := func(left, right rational.Rational) (rational.Rational, error) {
		return left.Add(ctx, right, limits)
	}
	mathtest.Commutative(t, rationals, rationalAdd)
	mathtest.Associative(t, rationals, rationalAdd)
	mathtest.Identity(t, rationals, rational.Zero(), rationalAdd)

	decimals := []decimal.Decimal{
		decimal.MustParse("-2.50"), decimal.New(0), decimal.MustParse("7.125"),
	}
	decimalAdd := func(left, right decimal.Decimal) (decimal.Decimal, error) {
		return left.AddExact(ctx, right, limits)
	}
	mathtest.Commutative(t, decimals, decimalAdd)
	mathtest.Associative(t, decimals, decimalAdd)
	mathtest.Identity(t, decimals, decimal.New(0), decimalAdd)
}

func TestFinitePrecisionAdditionIsNotAssociative(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	largeDecimal, err := decimal.FromBig(big.NewInt(1), 10, limits)
	if err != nil {
		t.Fatal(err)
	}
	decimalContext := decimal.Context{
		Precision: 3, MinExponent: -100, MaxExponent: 100,
		Rounding: decimal.HalfEven, Limits: limits,
	}
	decimalLeft := addDecimal(t, decimalContext, addDecimal(t, decimalContext, largeDecimal, largeDecimal.Neg()), decimal.New(1))
	decimalRight := addDecimal(t, decimalContext, largeDecimal, addDecimal(t, decimalContext, largeDecimal.Neg(), decimal.New(1)))
	if decimalLeft.Equal(decimalRight) || decimalLeft.String() != "1" || !decimalRight.IsZero() {
		t.Fatalf("decimal association = %s and %s, want 1 and 0", decimalLeft, decimalRight)
	}

	floatContext := bigfloat.Context{
		Precision: 8, Rounding: gomath.RoundHalfEven, Limits: limits,
	}
	largeFloat, err := bigfloat.NewInt64(1<<20, floatContext)
	if err != nil {
		t.Fatal(err)
	}
	oneFloat, err := bigfloat.NewInt64(1, floatContext)
	if err != nil {
		t.Fatal(err)
	}
	floatLeft := addFloat(t, floatContext, addFloat(t, floatContext, largeFloat.Value, largeFloat.Value.Neg()), oneFloat.Value)
	floatRight := addFloat(t, floatContext, largeFloat.Value, addFloat(t, floatContext, largeFloat.Value.Neg(), oneFloat.Value))
	if floatLeft.Equal(floatRight) || floatLeft.String() != "1" || floatRight.String() != "0" {
		t.Fatalf("float association = %s and %s, want 1 and 0", floatLeft, floatRight)
	}
}

func mustRational(t *testing.T, numerator, denominator int64) rational.Rational {
	t.Helper()
	value, err := rational.New(numerator, denominator)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func addDecimal(t *testing.T, operation decimal.Context, left, right decimal.Decimal) decimal.Decimal {
	t.Helper()
	result, err := operation.Add(context.Background(), left, right)
	if err != nil {
		t.Fatal(err)
	}
	return result.Value
}

func addFloat(t *testing.T, operation bigfloat.Context, left, right bigfloat.Float) bigfloat.Float {
	t.Helper()
	result, err := operation.Add(context.Background(), left, right)
	if err != nil {
		t.Fatal(err)
	}
	return result.Value
}
