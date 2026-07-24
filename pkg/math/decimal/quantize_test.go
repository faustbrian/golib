package decimal_test

import (
	"context"
	"errors"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/decimal"
)

func TestQuantizeUsesExplicitFractionalScale(t *testing.T) {
	t.Parallel()

	result, err := decimal.MustParse("3.335").Quantize(
		context.Background(),
		2,
		gomath.RoundHalfEven,
		gomath.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("Quantize() error = %v", err)
	}
	if result.Value.String() != "3.34" {
		t.Fatalf("Quantize() = %s, want 3.34", result.Value)
	}
	if result.Conditions != gomath.ConditionRounded|gomath.ConditionInexact {
		t.Fatalf("Quantize() conditions = %s", result.Conditions)
	}

	exact, err := decimal.New(12).Quantize(
		context.Background(),
		2,
		gomath.RoundHalfEven,
		gomath.DefaultLimits(),
	)
	if err != nil || exact.Value.String() != "12.00" || exact.Conditions != 0 {
		t.Fatalf("exact Quantize() = %s, %s, %v", exact.Value, exact.Conditions, err)
	}
}

func TestQuantizedQuoRoundsOnceAtTheRequestedScale(t *testing.T) {
	t.Parallel()

	result, err := decimal.QuantizedQuo(
		context.Background(),
		decimal.New(1),
		decimal.New(6),
		2,
		gomath.RoundHalfEven,
		gomath.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("QuantizedQuo() error = %v", err)
	}
	if result.Value.String() != "0.17" {
		t.Fatalf("QuantizedQuo() = %s, want 0.17", result.Value)
	}
	if result.Conditions != gomath.ConditionRounded|gomath.ConditionInexact {
		t.Fatalf("QuantizedQuo() conditions = %s", result.Conditions)
	}
}

func TestQuantizeZeroStillEnforcesOutputLimits(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	limits.MaxOutputDigits = 2
	_, err := decimal.New(0).Quantize(
		context.Background(), -3, decimal.HalfEven, limits,
	)
	if !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Quantize() error = %v, want ErrLimitExceeded", err)
	}
}
