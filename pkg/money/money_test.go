package money

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
)

func TestParseProducesExactContextBoundMoney(t *testing.T) {
	t.Parallel()

	euro, err := currency.Parse("EUR")
	if err != nil {
		t.Fatalf("currency.Parse(EUR) error = %v", err)
	}
	context, err := DefaultContext(euro)
	if err != nil {
		t.Fatalf("DefaultContext(EUR) error = %v", err)
	}

	value, err := Parse("9007199254740993.10", euro, context)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := value.Amount().String(); got != "9007199254740993.10" {
		t.Fatalf("Amount() = %s", got)
	}
	if value.Currency() != euro {
		t.Fatalf("Currency() = %s, want EUR", value.Currency())
	}
	if value.Context() != context {
		t.Fatalf("Context() = %#v, want %#v", value.Context(), context)
	}

	if _, err := Parse("1.001", euro, context); !errors.Is(err, ErrPrecisionLoss) {
		t.Fatalf("Parse(excess scale) error = %v, want ErrPrecisionLoss", err)
	}
}

func TestArithmeticPreservesIdentityAndRejectsMismatches(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	dollar, _ := currency.Parse("USD")
	defaultEUR, _ := DefaultContext(euro)
	defaultUSD, _ := DefaultContext(dollar)
	custom, _ := CustomContext(3)

	left, _ := Parse("10.25", euro, defaultEUR)
	right, _ := Parse("2.10", euro, defaultEUR)

	sum, err := left.Add(right)
	if err != nil || sum.String() != "12.35 EUR" {
		t.Fatalf("Add() = %s, %v", sum, err)
	}
	difference, err := left.Sub(right)
	if err != nil || difference.String() != "8.15 EUR" {
		t.Fatalf("Sub() = %s, %v", difference, err)
	}
	if left.String() != "10.25 EUR" || right.String() != "2.10 EUR" {
		t.Fatal("arithmetic mutated an operand")
	}

	dollars, _ := Parse("2.10", dollar, defaultUSD)
	if _, err := left.Add(dollars); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("Add(currency mismatch) error = %v", err)
	}
	otherContext, _ := Parse("2.100", euro, custom)
	if _, err := left.Add(otherContext); !errors.Is(err, ErrContextMismatch) {
		t.Errorf("Add(context mismatch) error = %v", err)
	}
}

func TestComparisonAndUnaryOperationsAreExactAndImmutable(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	context, _ := DefaultContext(euro)
	negative, _ := Parse("-1.25", euro, context)
	positive, _ := Parse("2.00", euro, context)

	comparison, err := negative.Compare(positive)
	if err != nil || comparison >= 0 {
		t.Fatalf("Compare() = %d, %v", comparison, err)
	}
	equal, err := positive.Equal(positive)
	if err != nil || !equal {
		t.Fatalf("Equal() = %t, %v", equal, err)
	}
	absolute, err := negative.Abs()
	if err != nil || absolute.String() != "1.25 EUR" {
		t.Fatalf("Abs() = %s, %v", absolute, err)
	}
	negated, err := positive.Neg()
	if err != nil || negated.String() != "-2.00 EUR" {
		t.Fatalf("Neg() = %s, %v", negated, err)
	}
	if negative.Sign() != -1 || positive.Sign() != 1 || negative.IsZero() {
		t.Fatal("unexpected sign contract")
	}
	if negative.String() != "-1.25 EUR" || positive.String() != "2.00 EUR" {
		t.Fatal("unary operation mutated an operand")
	}
}
