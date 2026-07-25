package money

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/international/currency"
	gomath "github.com/faustbrian/golib/pkg/math"
)

func TestConversionRequiresInjectedAttributedExactRate(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	dollar, _ := currency.Parse("USD")
	euroContext, _ := DefaultContext(euro)
	dollarContext, _ := DefaultContext(dollar)
	value, _ := Parse("10.00", euro, euroContext)
	rate, _ := ParseRate("1.1")
	observed := time.Date(2026, 7, 19, 6, 0, 0, 0, time.UTC)
	exchange, err := NewExchangeRate(euro, dollar, rate, observed, "central-bank-daily")
	if err != nil {
		t.Fatalf("NewExchangeRate() error = %v", err)
	}

	result, err := Convert(context.Background(), value, exchange, dollarContext, gomath.RoundHalfEven)
	if err != nil || result.Converted().String() != "11.00 USD" {
		t.Fatalf("Convert() = %s, %v", result.Converted(), err)
	}
	if result.Rate().ObservedAt() != observed || result.Rate().Source() != "central-bank-daily" {
		t.Fatal("conversion metadata was not retained")
	}

	if _, err := Convert(context.Background(), result.Converted(), exchange, dollarContext, gomath.RoundHalfEven); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("Convert(wrong base) error = %v", err)
	}
}
