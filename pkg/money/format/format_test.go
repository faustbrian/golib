package format_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/international/locale"
	"github.com/faustbrian/golib/pkg/money"
	moneyformat "github.com/faustbrian/golib/pkg/money/format"
)

func TestLocaleFormattingKeepsExactValueAndSeparateIdentity(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := money.DefaultContext(euro)
	value, _ := money.Parse("1234567.89", euro, monetaryContext)
	finnish, _ := locale.Parse("fi-FI")

	formatted, err := moneyformat.Locale(value, finnish, moneyformat.Options{Symbol: true})
	if err != nil {
		t.Fatalf("Locale() error = %v", err)
	}
	if formatted != "€ 1\u00a0234\u00a0567,89" {
		t.Fatalf("Locale() = %q", formatted)
	}
	if moneyformat.Exact(value) != "1234567.89 EUR" {
		t.Fatalf("Exact() = %q", moneyformat.Exact(value))
	}
	if value.String() != "1234567.89 EUR" {
		t.Fatal("formatting changed monetary identity")
	}
}

func TestLocaleFormattingUsesTheLocalesGroupingPattern(t *testing.T) {
	t.Parallel()

	rupee, _ := currency.Parse("INR")
	monetaryContext, _ := money.DefaultContext(rupee)
	value, _ := money.Parse("12345678.90", rupee, monetaryContext)
	hindi, _ := locale.Parse("hi-IN")

	formatted, err := moneyformat.Locale(value, hindi, moneyformat.Options{})
	if err != nil {
		t.Fatalf("Locale() error = %v", err)
	}
	if formatted != "INR 1,23,45,678.90" {
		t.Fatalf("Locale() = %q", formatted)
	}
}
