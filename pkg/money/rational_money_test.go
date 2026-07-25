package money

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
	gomath "github.com/faustbrian/golib/pkg/math"
)

func TestMoneyRateArithmeticStaysRationalUntilExplicitRounding(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := DefaultContext(euro)
	value, _ := Parse("5.00", euro, monetaryContext)
	rate, err := ParseRate("1.5")
	if err != nil {
		t.Fatalf("ParseRate() error = %v", err)
	}

	exact, err := value.Mul(context.Background(), rate)
	if err != nil {
		t.Fatalf("Mul() error = %v", err)
	}
	if exact.String() != "15/2 EUR" {
		t.Fatalf("Mul() = %s, want 15/2 EUR", exact)
	}

	rounded, result, err := exact.Round(monetaryContext, gomath.RoundHalfEven)
	if err != nil {
		t.Fatalf("Round() error = %v", err)
	}
	if rounded.String() != "7.50 EUR" || result.Inexact() {
		t.Fatalf("Round() = %s, %#v", rounded, result)
	}
}

func TestCashRoundingUsesTheConfiguredIncrement(t *testing.T) {
	t.Parallel()

	franc, _ := currency.Parse("CHF")
	inputContext, _ := DefaultContext(franc)
	cash, _ := CashContext(2, 5)
	one, _ := ParseRate("1")

	tests := []struct {
		input string
		want  string
	}{
		{input: "10.02", want: "10.00 CHF"},
		{input: "10.03", want: "10.05 CHF"},
		{input: "-10.03", want: "-10.05 CHF"},
	}
	for _, test := range tests {
		value, _ := Parse(test.input, franc, inputContext)
		exact, err := value.Mul(context.Background(), one)
		if err != nil {
			t.Fatalf("Mul(%s) error = %v", test.input, err)
		}
		rounded, result, err := exact.Round(cash, gomath.RoundHalfEven)
		if err != nil || rounded.String() != test.want || !result.Inexact() {
			t.Errorf("Round(%s) = %s, %#v, %v; want %s", test.input, rounded, result, err, test.want)
		}
	}
}

func TestDivisionAndMoneyRatiosRemainExact(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := DefaultContext(euro)
	five, _ := Parse("5.00", euro, monetaryContext)
	two, _ := Parse("2.00", euro, monetaryContext)
	rate, _ := ParseRate("1.5")

	quotient, err := five.Quo(context.Background(), rate)
	if err != nil || quotient.String() != "10/3 EUR" {
		t.Fatalf("Quo() = %s, %v", quotient, err)
	}
	ratio, err := five.Ratio(context.Background(), two)
	if err != nil || ratio.String() != "5/2" {
		t.Fatalf("Ratio() = %s, %v", ratio, err)
	}
}

func TestAutomaticRoundingOnlyAcceptsSafeTerminatingPrecision(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := DefaultContext(euro)
	one, _ := Parse("1.00", euro, monetaryContext)
	oneEighth, _ := ParseRate("1/8")
	exact, _ := one.Mul(context.Background(), oneEighth)

	value, result, err := exact.Round(AutomaticContext(), gomath.RoundHalfEven)
	if err != nil || value.String() != "0.125 EUR" || result.Inexact() || value.Context().Scale() != 3 {
		t.Fatalf("automatic Round() = %s, %#v, %v", value, result, err)
	}
	oneThird, _ := ParseRate("1/3")
	repeating, _ := one.Mul(context.Background(), oneThird)
	if _, _, err := repeating.Round(AutomaticContext(), gomath.RoundHalfEven); !errors.Is(err, ErrPrecisionLoss) {
		t.Fatalf("automatic repeating Round() error = %v", err)
	}
}
