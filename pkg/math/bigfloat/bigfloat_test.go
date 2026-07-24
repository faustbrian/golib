package bigfloat_test

import (
	"context"
	"errors"
	"math/big"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/bigfloat"
)

func TestFloatDefensivelyCopiesBigFloat(t *testing.T) {
	t.Parallel()

	operation := testContext()
	source := new(big.Float).SetPrec(128).SetInt64(41)
	result, err := bigfloat.FromBig(source, operation)
	if err != nil {
		t.Fatalf("FromBig() error = %v", err)
	}
	source.SetInt64(99)
	exposed := result.Value.Big()
	exposed.SetInt64(100)

	if got := result.Value.String(); got != "41" {
		t.Fatalf("value changed through an alias: %s", got)
	}
	if result.Value.Precision() != operation.Precision {
		t.Fatalf("Precision() = %d", result.Value.Precision())
	}
}

func TestFloatArithmeticReportsAccuracy(t *testing.T) {
	t.Parallel()

	operation := testContext()
	one := mustInt(t, operation, 1)
	three := mustInt(t, operation, 3)
	result, err := operation.Quo(context.Background(), one, three)
	if err != nil {
		t.Fatalf("Quo() error = %v", err)
	}
	if !result.Conditions.Has(gomath.ConditionInexact) || result.Accuracy == big.Exact {
		t.Fatalf("Quo() conditions = %s, accuracy = %s", result.Conditions, result.Accuracy)
	}
	if one.String() != "1" || three.String() != "3" {
		t.Fatal("operation mutated an operand")
	}
}

func TestFloatRequiresExplicitSupportedContext(t *testing.T) {
	t.Parallel()

	invalid := bigfloat.Context{Precision: 0, Rounding: gomath.RoundHalfEven}
	if _, err := bigfloat.NewInt64(1, invalid); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("NewInt64() error = %v", err)
	}
	unsupported := testContext()
	unsupported.Rounding = gomath.RoundHalfDown
	if _, err := bigfloat.NewInt64(1, unsupported); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("NewInt64() rounding error = %v", err)
	}
}

func TestFloatExactConversionsNeverSilentlyRound(t *testing.T) {
	t.Parallel()

	operation := testContext()
	third, err := bigfloat.FromRat(big.NewRat(1, 3), operation)
	if err != nil {
		t.Fatalf("FromRat() error = %v", err)
	}
	if !third.Conditions.Has(gomath.ConditionInexact) {
		t.Fatalf("FromRat() conditions = %s", third.Conditions)
	}
	if _, err := third.Value.Int(); !errors.Is(err, gomath.ErrConversion) {
		t.Fatalf("Int() error = %v", err)
	}

	whole := mustInt(t, operation, 42)
	integer, err := whole.Int()
	if err != nil || integer.String() != "42" {
		t.Fatalf("Int() = %v, %v", integer, err)
	}
}

func TestFloatDivisionByZeroReportsCondition(t *testing.T) {
	t.Parallel()

	operation := testContext()
	one := mustInt(t, operation, 1)
	zero := mustInt(t, operation, 0)
	result, err := operation.Quo(context.Background(), one, zero)
	if err != nil || !result.Conditions.Has(gomath.ConditionDivisionByZero) || !result.Value.IsInf() {
		t.Fatalf("Quo(1, 0) = %s, %s, %v", result.Value, result.Conditions, err)
	}
}

func TestFloatDivisionPreservesNegativeZeroSign(t *testing.T) {
	t.Parallel()

	operation := testContext()
	one := mustInt(t, operation, 1)
	negativeZeroSource := new(big.Float).SetPrec(operation.Precision)
	negativeZeroSource.Neg(negativeZeroSource)
	negativeZero, err := bigfloat.FromBig(negativeZeroSource, operation)
	if err != nil {
		t.Fatal(err)
	}
	result, err := operation.Quo(context.Background(), one, negativeZero.Value)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Value.IsInf() || !result.Value.Signbit() {
		t.Fatalf("1 / -0 = %s, want -Inf", result.Value)
	}
}

func TestFloatExposesAndPreservesNegativeZero(t *testing.T) {
	t.Parallel()

	operation := testContext()
	source := new(big.Float).SetPrec(operation.Precision)
	source.Neg(source)
	result, err := bigfloat.FromBig(source, operation)
	if err != nil {
		t.Fatal(err)
	}
	if result.Value.Sign() != 0 || !result.Value.Signbit() || result.Value.String() != "-0" {
		t.Fatalf("negative zero = %s, sign %d, signbit %t", result.Value, result.Value.Sign(), result.Value.Signbit())
	}
	if result.Value.Abs().Signbit() || !result.Value.Neg().Equal(result.Value.Abs()) {
		t.Fatal("negative zero unary operations lost their sign contract")
	}
}

func testContext() bigfloat.Context {
	return bigfloat.Context{
		Precision: 64,
		Rounding:  gomath.RoundHalfEven,
		Limits:    gomath.DefaultLimits(),
	}
}

func mustInt(t *testing.T, operation bigfloat.Context, value int64) bigfloat.Float {
	t.Helper()
	result, err := bigfloat.NewInt64(value, operation)
	if err != nil {
		t.Fatalf("NewInt64() error = %v", err)
	}

	return result.Value
}
