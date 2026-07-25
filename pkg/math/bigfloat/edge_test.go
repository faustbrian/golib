package bigfloat_test

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/bigfloat"
)

func TestFloatUnaryComparisonConversionAndEncoding(t *testing.T) {
	t.Parallel()

	operation := testContext()
	negative := mustInt(t, operation, -4)
	positive := negative.Abs()
	if positive.Sign() != 1 || positive.Neg().Cmp(negative) != 0 || !positive.Equal(mustInt(t, operation, 4)) {
		t.Fatal("unary or comparison contract failed")
	}
	if positive.Rounding() != gomath.RoundHalfEven {
		t.Fatalf("Rounding() = %s", positive.Rounding())
	}
	rational, err := positive.Rat()
	if err != nil || rational.Cmp(big.NewRat(4, 1)) != 0 {
		t.Fatalf("Rat() = %v, %v", rational, err)
	}
	text, err := positive.MarshalText()
	if err != nil || string(text) != "4" {
		t.Fatalf("MarshalText() = %q, %v", text, err)
	}
	data, err := json.Marshal(positive)
	if err != nil || string(data) != `"4"` {
		t.Fatalf("MarshalJSON() = %s, %v", data, err)
	}
}

func TestEverySupportedBinaryRoundingMode(t *testing.T) {
	t.Parallel()

	for _, mode := range []gomath.RoundingMode{
		gomath.RoundHalfEven, gomath.RoundHalfUp, gomath.RoundDown,
		gomath.RoundUp, gomath.RoundCeiling, gomath.RoundFloor,
	} {
		operation := testContext()
		operation.Rounding = mode
		if _, err := bigfloat.NewInt64(1, operation); err != nil {
			t.Fatalf("mode %s: %v", mode, err)
		}
	}
}

func TestFloatArithmeticAndSquareRoot(t *testing.T) {
	t.Parallel()

	operation := testContext()
	two := mustInt(t, operation, 2)
	four := mustInt(t, operation, 4)
	checks := []struct {
		want string
		run  func() (bigfloat.Result, error)
	}{
		{"6", func() (bigfloat.Result, error) { return operation.Add(context.Background(), two, four) }},
		{"2", func() (bigfloat.Result, error) { return operation.Sub(context.Background(), four, two) }},
		{"8", func() (bigfloat.Result, error) { return operation.Mul(context.Background(), two, four) }},
		{"2", func() (bigfloat.Result, error) { return operation.Sqrt(context.Background(), four) }},
	}
	for _, check := range checks {
		result, err := check.run()
		if err != nil || result.Value.String() != check.want {
			t.Fatalf("operation = %s, %v; want %s", result.Value, err, check.want)
		}
	}
	if result, err := operation.Sqrt(context.Background(), mustInt(t, operation, -1)); !errors.Is(err, gomath.ErrDomain) ||
		!result.Conditions.Has(gomath.ConditionInvalidOperation) {
		t.Fatalf("negative Sqrt() = %s, %v", result.Conditions, err)
	}
	zero := mustInt(t, operation, 0)
	if result, err := operation.Quo(context.Background(), zero, zero); !errors.Is(err, gomath.ErrDomain) ||
		!result.Conditions.Has(gomath.ConditionInvalidOperation) {
		t.Fatalf("zero Quo() = %s, %v", result.Conditions, err)
	}
}

func TestFloatValidationParsingAndTraps(t *testing.T) {
	t.Parallel()

	operation := testContext()
	if _, err := bigfloat.FromBig(nil, operation); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("FromBig(nil) error = %v", err)
	}
	if _, err := bigfloat.FromRat(nil, operation); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("FromRat(nil) error = %v", err)
	}
	if _, err := bigfloat.Parse(" 1", 10, operation); !errors.Is(err, gomath.ErrInvalidSyntax) {
		t.Fatalf("Parse(whitespace) error = %v", err)
	}
	if _, err := bigfloat.Parse("1", 8, operation); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("Parse(base) error = %v", err)
	}
	if _, err := bigfloat.Parse("not-a-float", 10, operation); !errors.Is(err, gomath.ErrInvalidSyntax) {
		t.Fatalf("Parse(syntax) error = %v", err)
	}
	parsed, err := bigfloat.Parse("0x1.8p+2", 0, operation)
	if err != nil || parsed.Value.String() != "6" {
		t.Fatalf("Parse(hex) = %s, %v", parsed.Value, err)
	}

	trapping := operation
	trapping.Traps = gomath.ConditionInexact | gomath.ConditionDivisionByZero
	one := mustInt(t, trapping, 1)
	three := mustInt(t, trapping, 3)
	if _, err := trapping.Quo(context.Background(), one, three); !errors.Is(err, gomath.ErrTrappedCondition) {
		t.Fatalf("inexact trap = %v", err)
	}
	zero := mustInt(t, trapping, 0)
	if _, err := trapping.Quo(context.Background(), one, zero); !errors.Is(err, gomath.ErrTrappedCondition) {
		t.Fatalf("division trap = %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := operation.Add(canceled, one, three); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
	var nilContext context.Context
	if _, err := operation.Add(nilContext, one, three); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("nil context error = %v", err)
	}
}

func TestInfiniteFloatConversionsFail(t *testing.T) {
	t.Parallel()

	operation := testContext()
	one := mustInt(t, operation, 1)
	zero := mustInt(t, operation, 0)
	infinite, err := operation.Quo(context.Background(), one, zero)
	if err != nil {
		t.Fatalf("Quo() error = %v", err)
	}
	if _, err := infinite.Value.Int(); !errors.Is(err, gomath.ErrConversion) {
		t.Fatalf("Inf.Int() error = %v", err)
	}
	if _, err := infinite.Value.Rat(); !errors.Is(err, gomath.ErrConversion) {
		t.Fatalf("Inf.Rat() error = %v", err)
	}
}

func TestFloatEnforcesEveryResourceLimit(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	limits.MaxExponentMagnitude = 8
	operation := bigfloat.Context{
		Precision: 64, Rounding: gomath.RoundHalfEven, Limits: limits,
	}
	if _, err := bigfloat.Parse("1e100", 10, operation); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Parse(exponent) error = %v, want ErrLimitExceeded", err)
	}

	broad := operation
	broad.Limits = gomath.DefaultLimits()
	largeSource := new(big.Float).SetPrec(64).SetMantExp(big.NewFloat(0.5), 8)
	large, err := bigfloat.FromBig(largeSource, broad)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := operation.Mul(context.Background(), large.Value, large.Value); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Mul(exponent) error = %v, want ErrLimitExceeded", err)
	}

	intermediateLimited := operation
	intermediateLimited.Limits.MaxIntermediateBits = 32
	if _, err := bigfloat.NewInt64(1, intermediateLimited); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("NewInt64(intermediate bits) error = %v, want ErrLimitExceeded", err)
	}
	sourceLimited := operation
	sourceLimited.Limits.MaxIntermediateBits = 64
	highPrecisionSource := new(big.Float).SetPrec(128).SetInt64(1)
	if _, err := bigfloat.FromBig(highPrecisionSource, sourceLimited); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("FromBig(intermediate bits) error = %v, want ErrLimitExceeded", err)
	}
	hugeNumerator := new(big.Int).Lsh(big.NewInt(1), 65)
	if _, err := bigfloat.FromRat(new(big.Rat).SetInt(hugeNumerator), sourceLimited); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("FromRat(intermediate bits) error = %v, want ErrLimitExceeded", err)
	}
	broadPrecision := sourceLimited
	broadPrecision.Precision = 128
	broadPrecision.Limits.MaxIntermediateBits = 128
	broadValue, err := bigfloat.NewInt64(1, broadPrecision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sourceLimited.Add(context.Background(), broadValue.Value, broadValue.Value); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Add(oversized operand) error = %v, want ErrLimitExceeded", err)
	}
	narrowValue, err := bigfloat.NewInt64(1, sourceLimited)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sourceLimited.Add(context.Background(), narrowValue.Value, broadValue.Value); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Add(oversized right operand) error = %v, want ErrLimitExceeded", err)
	}
	if _, err := sourceLimited.Quo(context.Background(), broadValue.Value, narrowValue.Value); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Quo(oversized numerator) error = %v, want ErrLimitExceeded", err)
	}
	if _, err := sourceLimited.Quo(context.Background(), narrowValue.Value, broadValue.Value); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Quo(oversized denominator) error = %v, want ErrLimitExceeded", err)
	}
	if _, err := sourceLimited.Sqrt(context.Background(), broadValue.Value); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Sqrt(oversized operand) error = %v, want ErrLimitExceeded", err)
	}

	outputLimited := operation
	outputLimited.Limits.MaxOutputDigits = 3
	if _, err := bigfloat.NewInt64(1, outputLimited); err != nil {
		t.Fatalf("NewInt64(compact output) error = %v", err)
	}
	if _, err := bigfloat.Parse("1.2345", 10, outputLimited); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Parse(output digits) error = %v, want ErrLimitExceeded", err)
	}
}
