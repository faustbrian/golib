package bigfloat

import (
	"context"
	"math/big"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
)

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
