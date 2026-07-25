package rational

import (
	"context"
	"math/big"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
)

func TestInternalBoundaryBranches(t *testing.T) {
	limits := gomath.DefaultLimits()
	bad := limits
	bad.MaxInputDigits = 0
	if _, err := Parse("1", bad); err == nil {
		t.Fatal("expected parser limit validation")
	}
	if !validInteger("-1", 1) || validInteger("", 1) {
		t.Fatal("integer grammar mismatch")
	}
	value, err := New(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	var nilContext context.Context
	if _, err := value.Pow(nilContext, 2, limits); err == nil {
		t.Fatal("expected power context error")
	}
	if _, _, err := value.Decimal(2, gomath.RoundHalfEven, bad); err == nil {
		t.Fatal("expected decimal limit validation")
	}
	q, r, d := big.NewInt(1), big.NewInt(6), big.NewInt(10)
	if !shouldIncrement(q, r, d, 1, gomath.RoundHalfEven) {
		t.Fatal("greater-than-half should increment")
	}
	if shouldIncrement(q, big.NewInt(4), d, 1, gomath.RoundHalfEven) {
		t.Fatal("less-than-half incremented")
	}
	if !shouldIncrement(q, big.NewInt(5), d, 1, gomath.RoundHalfUp) {
		t.Fatal("half-up did not increment")
	}
	if shouldIncrement(q, big.NewInt(5), d, 1, gomath.RoundHalfDown) {
		t.Fatal("half-down incremented")
	}
	if _, err := binary(context.Background(), &value.value, &value.value, bad, (*big.Rat).Add); err == nil {
		t.Fatal("expected checked rational limit")
	}
}
