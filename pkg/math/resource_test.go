package gomath_test

import (
	"context"
	"errors"
	"math/big"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/bigfloat"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/math/integer"
	"github.com/faustbrian/golib/pkg/math/rational"
)

func TestResourceCancellationAndArithmeticErrorsRemainDistinct(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	tiny := limits
	tiny.MaxPowerExponent = 2
	if _, err := integer.New(2).Pow(context.Background(), 3, tiny); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("power error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := integer.New(2).Pow(cancelled, 2, limits); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
	if _, err := integer.New(1).Quo(context.Background(), integer.Zero(), limits); !errors.Is(err, gomath.ErrDivisionByZero) {
		t.Fatalf("arithmetic error = %v", err)
	}
}

func TestExpensiveOperationsRejectWorkOutsideLimits(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	tiny := limits
	tiny.MaxIntermediateBits = 32
	tiny.MaxPowerExponent = 1_000_000
	largeRational, err := rational.NewChecked(
		new(big.Int).Lsh(big.NewInt(1), 31), big.NewInt(3), limits,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := largeRational.Pow(context.Background(), 10_000, tiny); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("rational power error = %v", err)
	}
	tiny.MaxRootDegree = 3
	if _, err := integer.New(64).Root(context.Background(), 4, tiny); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("root error = %v", err)
	}
	tiny.MaxDecimalExpansion = 4
	third, err := rational.New(1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := third.Decimal(5, gomath.RoundHalfEven, tiny); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("decimal expansion error = %v", err)
	}
	tiny.MaxExponentMagnitude = 2
	if _, err := decimal.MustParse("1").Quantize(context.Background(), 3, decimal.HalfEven, tiny); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("quantize error = %v", err)
	}
	tiny.MaxPrecision = 8
	if _, err := bigfloat.NewInt64(1, bigfloat.Context{
		Precision: 9, Rounding: gomath.RoundHalfEven, Limits: tiny,
	}); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("float precision error = %v", err)
	}
}
