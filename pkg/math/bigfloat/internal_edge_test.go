package bigfloat

import (
	"context"
	"errors"
	"math/big"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
)

var allocationErrorSink error

func TestValidationFailureBranches(t *testing.T) {
	limits := gomath.DefaultLimits()
	bad := limits
	bad.MaxInputDigits = 0
	invalid := Context{Precision: 8, Rounding: gomath.RoundHalfEven, Limits: bad}
	if _, err := FromBig(big.NewFloat(1), invalid); err == nil {
		t.Fatal("expected FromBig validation error")
	}
	if _, err := FromRat(big.NewRat(1, 2), invalid); err == nil {
		t.Fatal("expected FromRat validation error")
	}
	if _, err := Parse("1", 10, invalid); err == nil {
		t.Fatal("expected Parse validation error")
	}
	valid := Context{Precision: 8, Rounding: gomath.RoundHalfEven, Limits: limits}
	value, err := NewInt64(1, valid)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := valid.Quo(cancelled, value.Value, value.Value); err == nil {
		t.Fatal("expected quotient cancellation")
	}
	if _, err := valid.Sqrt(cancelled, value.Value); err == nil {
		t.Fatal("expected square-root cancellation")
	}
}

func TestRationalSourceBitLimitsAreIndependentAndInclusive(t *testing.T) {
	limits := gomath.DefaultLimits()
	limits.MaxIntermediateBits = 64
	operation := Context{
		Precision: 64,
		Rounding:  gomath.RoundHalfEven,
		Limits:    limits,
	}

	for _, value := range []*big.Rat{
		new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), 63)),
		new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).Lsh(big.NewInt(1), 63)),
	} {
		if _, err := FromRat(value, operation); err != nil {
			t.Fatalf("FromRat(%s) at bit limit error = %v", value, err)
		}
	}
	for _, value := range []*big.Rat{
		new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), 64)),
		new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).Lsh(big.NewInt(1), 64)),
	} {
		if _, err := FromRat(value, operation); !errors.Is(err, gomath.ErrLimitExceeded) {
			t.Fatalf("FromRat(%s) above bit limit error = %v", value, err)
		}
	}
}

func TestParseInputBoundariesAreIndependent(t *testing.T) {
	limits := gomath.DefaultLimits()
	limits.MaxInputDigits = 4
	operation := Context{
		Precision: 64,
		Rounding:  gomath.RoundHalfEven,
		Limits:    limits,
	}

	for _, text := range []string{"", " 1", "1 ", "1.250"} {
		if _, err := Parse(text, 10, operation); !errors.Is(err, gomath.ErrInvalidSyntax) {
			t.Fatalf("Parse(%q) error = %v, want ErrInvalidSyntax", text, err)
		}
	}
	if _, err := Parse("", 8, operation); !errors.Is(err, gomath.ErrInvalidSyntax) {
		t.Fatalf("Parse(empty, invalid base) error = %v, want ErrInvalidSyntax", err)
	}
	if result, err := Parse("1.25", 10, operation); err != nil || result.Value.String() != "1.25" {
		t.Fatalf("Parse at input limit = %s, %v", result.Value, err)
	}
}

func TestPrecisionAndNumericLimitsIncludeExactBoundaries(t *testing.T) {
	limits := gomath.DefaultLimits()
	limits.MaxPrecision = 64
	limits.MaxIntermediateBits = 128
	operation := Context{
		Precision: 64,
		Rounding:  gomath.RoundHalfEven,
		Limits:    limits,
	}
	if _, err := NewInt64(1, operation); err != nil {
		t.Fatalf("precision at limit error = %v", err)
	}
	operation.Precision++
	if _, err := NewInt64(1, operation); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("precision above limit error = %v", err)
	}

	limits = gomath.DefaultLimits()
	limits.MaxExponentMagnitude = 8
	operation = Context{
		Precision: 64,
		Rounding:  gomath.RoundHalfEven,
		Limits:    limits,
	}
	for _, exponent := range []int{-8, 8} {
		source := new(big.Float).SetPrec(64).SetMantExp(big.NewFloat(0.5), exponent)
		if _, err := FromBig(source, operation); err != nil {
			t.Fatalf("exponent %d at limit error = %v", exponent, err)
		}
	}
	for _, exponent := range []int{-9, 9} {
		source := new(big.Float).SetPrec(64).SetMantExp(big.NewFloat(0.5), exponent)
		if _, err := FromBig(source, operation); !errors.Is(err, gomath.ErrLimitExceeded) {
			t.Fatalf("exponent %d above limit error = %v", exponent, err)
		}
	}

	zero, err := NewInt64(0, operation)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := operation.Sqrt(context.Background(), zero.Value); err != nil || result.Value.Sign() != 0 {
		t.Fatalf("Sqrt(0) = %s, %v", result.Value, err)
	}
}

func TestRenderedDigitBoundsAndHelpers(t *testing.T) {
	for _, test := range []struct {
		value int
		want  int
	}{
		{value: 0, want: 1},
		{value: 9, want: 1},
		{value: 10, want: 2},
		{value: 99, want: 2},
		{value: 100, want: 3},
		{value: -9, want: 1},
		{value: -10, want: 2},
		{value: -100, want: 3},
	} {
		if got := integerDigits(test.value); got != test.want {
			t.Fatalf("integerDigits(%d) = %d, want %d", test.value, got, test.want)
		}
	}
	for _, test := range []struct {
		text string
		want int
	}{
		{text: "", want: 0},
		{text: "/09:", want: 2},
		{text: "-12.3e+4", want: 4},
	} {
		if got := renderedDigits(test.text); got != test.want {
			t.Fatalf("renderedDigits(%q) = %d, want %d", test.text, got, test.want)
		}
	}
	if got := maximumRenderedDigits(64, -123); got != 24 {
		t.Fatalf("maximumRenderedDigits(64, -123) = %d, want 24", got)
	}

	limits := gomath.DefaultLimits()
	limits.MaxOutputDigits = 5
	operation := Context{
		Precision: 64,
		Rounding:  gomath.RoundHalfEven,
		Limits:    limits,
	}
	if _, err := Parse("1.2345", 10, operation); err != nil {
		t.Fatalf("output at digit limit error = %v", err)
	}
	operation.Limits.MaxOutputDigits--
	if _, err := Parse("1.2345", 10, operation); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("output above digit limit error = %v", err)
	}
}

func TestConservativeOutputBoundAvoidsFormattingAllocation(t *testing.T) {
	value := new(big.Float).SetPrec(64).SetInt64(1)
	limits := gomath.DefaultLimits()
	limits.MaxOutputDigits = maximumRenderedDigits(value.Prec(), value.MantExp(nil))

	if allocations := testing.AllocsPerRun(100, func() {
		allocationErrorSink = checkValueLimits(value, limits)
	}); allocations != 0 {
		t.Fatalf("checkValueLimits at conservative bound allocated %.0f times", allocations)
	}
	if allocationErrorSink != nil {
		t.Fatalf("checkValueLimits at conservative bound error = %v", allocationErrorSink)
	}
}
