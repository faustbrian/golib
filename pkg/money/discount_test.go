package money

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
	gomath "github.com/faustbrian/golib/pkg/math"
)

func TestDiscountRoundsTheComponentAndConservesTheOriginal(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := DefaultContext(euro)
	original, _ := Parse("1.00", euro, monetaryContext)
	rate, err := ParseDiscountRate("1/3")
	if err != nil {
		t.Fatalf("ParseDiscountRate() error = %v", err)
	}

	result, err := ApplyDiscount(context.Background(), original, rate, gomath.RoundHalfEven)
	if err != nil {
		t.Fatalf("ApplyDiscount() error = %v", err)
	}
	if result.Discount().String() != "0.33 EUR" || result.Final().String() != "0.67 EUR" || !result.Rounding().Inexact() {
		t.Fatalf("ApplyDiscount() = discount %s, final %s", result.Discount(), result.Final())
	}
	sum, _ := result.Final().Add(result.Discount())
	equal, _ := sum.Equal(result.Original())
	if !equal {
		t.Fatal("discount result did not conserve original")
	}

	if _, err := ParseDiscountRate("1.01"); !errors.Is(err, ErrInvalidRate) {
		t.Fatalf("ParseDiscountRate(over 100%%) error = %v", err)
	}
}
