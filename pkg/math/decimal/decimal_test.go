package decimal_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/decimal"
)

func TestExactArithmeticAndImmutability(t *testing.T) {
	t.Parallel()

	left := decimal.MustParse("12.50")
	right := decimal.MustParse("0.25")

	added, err := left.AddExact(context.Background(), right, gomath.DefaultLimits())
	if err != nil {
		t.Fatalf("AddExact() error = %v", err)
	}
	if got := added.String(); got != "12.75" {
		t.Fatalf("Add() = %s, want 12.75", got)
	}
	multiplied, err := left.MulExact(context.Background(), right, gomath.DefaultLimits())
	if err != nil {
		t.Fatalf("MulExact() error = %v", err)
	}
	if got := multiplied.String(); got != "3.1250" {
		t.Fatalf("Mul() = %s, want 3.1250", got)
	}
	if got := left.String(); got != "12.50" {
		t.Fatalf("operand mutated: got %s", got)
	}
}

func TestDecimalUsesSharedMathContracts(t *testing.T) {
	t.Parallel()

	if _, err := decimal.Parse("not-a-number"); !errors.Is(err, gomath.ErrInvalidSyntax) {
		t.Fatalf("Parse() error = %v, want shared ErrInvalidSyntax", err)
	}
	got, err := (decimal.Context{
		Precision: 1, MinExponent: -10, MaxExponent: 10,
		Rounding: gomath.RoundHalfEven,
	}).Quo(
		context.Background(),
		decimal.New(1),
		decimal.New(4),
	)
	if err != nil || got.Value.String() != "0.2" {
		t.Fatalf("Context.Quo() = %s, %v", got.Value, err)
	}
}

func TestDivisionRequiresExplicitRoundingForRepeatingResults(t *testing.T) {
	t.Parallel()

	one := decimal.New(1)
	three := decimal.New(3)

	if _, err := one.QuoExact(context.Background(), three, gomath.DefaultLimits()); !errors.Is(err, decimal.ErrNonTerminating) {
		t.Fatalf("QuoExact() error = %v, want ErrNonTerminating", err)
	}

	got, err := (decimal.Context{
		Precision: 3, MinExponent: -10, MaxExponent: 10,
		Rounding: decimal.HalfEven,
	}).Quo(context.Background(), one, three)
	if err != nil {
		t.Fatalf("Context.Quo() error = %v", err)
	}
	if got.Value.String() != "0.333" {
		t.Fatalf("Context.Quo() = %s, want 0.333", got.Value)
	}
}

func TestTextAndJSONNeverUseFloatingPoint(t *testing.T) {
	t.Parallel()

	value := decimal.MustParse("9007199254740993.125")
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if string(data) != `"9007199254740993.125"` {
		t.Fatalf("Marshal() = %s", data)
	}

	var decoded decimal.Decimal
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !decoded.Equal(value) {
		t.Fatalf("round trip = %s, want %s", decoded, value)
	}
}

func TestParserRejectsUnboundedOrNonCanonicalInput(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", " 1", "+1", "1e2", "NaN", "1.2.3"} {
		if _, err := decimal.Parse(input); !errors.Is(err, decimal.ErrInvalid) {
			t.Errorf("Parse(%q) error = %v, want ErrInvalid", input, err)
		}
	}
}
