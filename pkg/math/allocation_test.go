package gomath_test

import (
	"context"
	"math/big"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/bigfloat"
	"github.com/faustbrian/golib/pkg/math/decimal"
	mathencoding "github.com/faustbrian/golib/pkg/math/encoding"
	"github.com/faustbrian/golib/pkg/math/integer"
	"github.com/faustbrian/golib/pkg/math/rational"
)

func TestRepresentativeAllocationBudgets(t *testing.T) {
	if raceEnabled {
		t.Skip("race instrumentation changes allocation counts")
	}

	limits := gomath.DefaultLimits()
	integerRoot, err := integer.FromBig(new(big.Int).Lsh(big.NewInt(1), 4_096), limits)
	if err != nil {
		t.Fatal(err)
	}
	rationalValue, err := rational.New(1, 7)
	if err != nil {
		t.Fatal(err)
	}
	decimalValue := decimal.MustParse("12345678901234567890.123456789")
	decimalDivisor := decimal.MustParse("7.000000001")
	decimalContext := decimal.Context{
		Precision: 34, MinExponent: -1_000, MaxExponent: 1_000,
		Rounding: decimal.HalfEven,
	}
	floatContext := bigfloat.Context{
		Precision: 4_096, Rounding: gomath.RoundHalfEven, Limits: limits,
	}
	floatValue, err := bigfloat.FromRat(big.NewRat(2, 1), floatContext)
	if err != nil {
		t.Fatal(err)
	}

	assertAllocationsAtMost(t, "integer root", 3_000, func() {
		if _, err := integerRoot.Root(context.Background(), 17, limits); err != nil {
			t.Fatal(err)
		}
	})
	assertAllocationsAtMost(t, "rational expansion", 30, func() {
		if _, _, err := rationalValue.Decimal(1_000, gomath.RoundHalfEven, limits); err != nil {
			t.Fatal(err)
		}
	})
	assertAllocationsAtMost(t, "decimal division", 55, func() {
		if _, err := decimalContext.Quo(context.Background(), decimalValue, decimalDivisor); err != nil {
			t.Fatal(err)
		}
	})
	assertAllocationsAtMost(t, "float sqrt and conversion", 26, func() {
		result, err := floatContext.Sqrt(context.Background(), floatValue.Value)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := result.Value.Rat(); err != nil {
			t.Fatal(err)
		}
	})
	assertAllocationsAtMost(t, "binary conversion", 36, func() {
		data, err := mathencoding.MarshalDecimal(decimalValue)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := mathencoding.UnmarshalDecimal(data, limits); err != nil {
			t.Fatal(err)
		}
	})
}

func assertAllocationsAtMost(t *testing.T, name string, maximum float64, operation func()) {
	t.Helper()
	if allocations := testing.AllocsPerRun(10, operation); allocations > maximum {
		t.Errorf("%s allocated %.0f times, budget %.0f", name, allocations, maximum)
	}
}
