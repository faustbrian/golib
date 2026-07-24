package decimal_test

import (
	"context"
	"errors"
	"math/big"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/decimal"
)

func TestContextReportsRoundingConditions(t *testing.T) {
	t.Parallel()

	operation := decimal.Context{
		Precision:   3,
		MinExponent: -20,
		MaxExponent: 20,
		Rounding:    decimal.HalfEven,
	}
	result, err := operation.Quo(context.Background(), decimal.New(1), decimal.New(3))
	if err != nil {
		t.Fatalf("Quo() error = %v", err)
	}
	if result.Value.String() != "0.333" {
		t.Fatalf("Quo() value = %s, want 0.333", result.Value)
	}
	want := gomath.ConditionRounded | gomath.ConditionInexact
	if result.Conditions != want {
		t.Fatalf("Quo() conditions = %s, want %s", result.Conditions, want)
	}
}

func TestContextTrapsSelectedConditions(t *testing.T) {
	t.Parallel()

	operation := decimal.Context{
		Precision:   2,
		MinExponent: -20,
		MaxExponent: 20,
		Rounding:    decimal.HalfEven,
		Traps:       gomath.ConditionInexact,
	}
	result, err := operation.Quo(context.Background(), decimal.New(1), decimal.New(6))
	if !errors.Is(err, gomath.ErrTrappedCondition) {
		t.Fatalf("Quo() error = %v, want ErrTrappedCondition", err)
	}
	if !result.Conditions.Has(gomath.ConditionInexact) {
		t.Fatalf("Quo() conditions = %s", result.Conditions)
	}

	result, err = operation.Quo(context.Background(), decimal.New(1), decimal.New(0))
	if !errors.Is(err, gomath.ErrDivisionByZero) || !result.Conditions.Has(gomath.ConditionDivisionByZero) {
		t.Fatalf("division by zero = %s, %v", result.Conditions, err)
	}
}

func TestDecimalRepresentationAndBigOwnership(t *testing.T) {
	t.Parallel()

	coefficient := big.NewInt(100)
	value, err := decimal.FromBig(coefficient, -2, gomath.DefaultLimits())
	if err != nil {
		t.Fatalf("FromBig() error = %v", err)
	}
	coefficient.SetInt64(7)
	exposed := value.Coefficient()
	exposed.SetInt64(8)

	other := decimal.MustParse("1.0")
	if !value.Equal(other) || value.SameRepresentation(other) {
		t.Fatalf("numeric/representation equality failed: %s and %s", value, other)
	}
	if value.String() != "1.00" || value.Exponent() != -2 {
		t.Fatalf("value changed through alias: %s exp %d", value, value.Exponent())
	}
}

func TestDecimalContextRoundsEveryArithmeticOperation(t *testing.T) {
	t.Parallel()

	operation := decimal.Context{
		Precision:   3,
		MinExponent: -20,
		MaxExponent: 20,
		Rounding:    decimal.HalfEven,
	}
	a := decimal.MustParse("9.99")
	b := decimal.MustParse("0.006")

	added, err := operation.Add(context.Background(), a, b)
	if err != nil || added.Value.String() != "10.0" || !added.Conditions.Has(gomath.ConditionInexact) {
		t.Fatalf("Add() = %s, %s, %v", added.Value, added.Conditions, err)
	}
	multiplied, err := operation.Mul(context.Background(), a, decimal.MustParse("1.01"))
	if err != nil || multiplied.Value.String() != "10.1" || !multiplied.Conditions.Has(gomath.ConditionRounded) {
		t.Fatalf("Mul() = %s, %s, %v", multiplied.Value, multiplied.Conditions, err)
	}
}

func TestDecimalParserCanExplicitlyEnableExponentNotation(t *testing.T) {
	t.Parallel()

	value, err := decimal.ParseWithOptions("-1.20e+3", decimal.ParseOptions{
		AllowExponent: true,
		Limits:        gomath.DefaultLimits(),
	})
	if err != nil || value.String() != "-1200" || value.Exponent() != 1 {
		t.Fatalf("ParseWithOptions() = %s exp %d, %v", value, value.Exponent(), err)
	}
	if _, err := decimal.ParseWithOptions("1e9999999", decimal.ParseOptions{
		AllowExponent: true,
		Limits:        gomath.DefaultLimits(),
	}); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("ParseWithOptions() error = %v", err)
	}
}
