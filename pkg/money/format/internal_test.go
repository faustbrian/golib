package format

import (
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/international/locale"
	"github.com/faustbrian/golib/pkg/money"
)

func TestFormattingBoundariesAndNumberShapes(t *testing.T) {
	t.Parallel()

	if Exact(money.Money{}) != "" {
		t.Fatal("Exact(zero) returned text")
	}
	english, _ := locale.Parse("en-US")
	if _, err := Locale(money.Money{}, english, Options{}); !errors.Is(err, ErrInvalidFormat) {
		t.Errorf("Locale(zero) error = %v", err)
	}
	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := money.DefaultContext(euro)
	value, _ := money.Parse("-123456.00", euro, monetaryContext)
	formatted, err := Locale(value, english, Options{})
	if err != nil || formatted != "EUR -123,456.00" {
		t.Fatalf("Locale(negative) = %q, %v", formatted, err)
	}
	if _, err := Locale(value, locale.Tag{}, Options{}); !errors.Is(err, ErrInvalidFormat) {
		t.Errorf("Locale(zero tag) error = %v", err)
	}

	digits := [10]string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}
	if got := localizeNumber("-12", digits, ".", ",", "-", 3, 3); got != "-12" {
		t.Fatalf("localizeNumber(-12) = %q", got)
	}
	if got := group("123", ",", 3, 3); got != "123" {
		t.Fatalf("group(123) = %q", got)
	}
	if got := group("123456", ",", 3, 3); got != "123,456" {
		t.Fatalf("group(123456) = %q", got)
	}
	if got := group("12345678", ",", 3, 2); got != "1,23,45,678" {
		t.Fatalf("group(12345678) = %q", got)
	}

	markka, _ := currency.ParseWithOptions("FIM", currency.ParseOptions{AllowHistoric: true})
	historicContext, _ := money.CustomContext(2)
	historic, _ := money.Parse("1.00", markka, historicContext)
	if _, err := Locale(historic, english, Options{Symbol: true}); err != nil {
		t.Fatalf("Locale(historic) error = %v", err)
	}
}

func TestFormattingFallbacksAndOutputBound(t *testing.T) {
	t.Parallel()

	if got := decimalSeparator("10"); got != "." {
		t.Fatalf("decimalSeparator(10) = %q", got)
	}
	if separator, primary, secondary := groupingPattern("1000", "123456789"); separator != "," || primary != 3 || secondary != 3 {
		t.Fatalf("groupingPattern(no separator) = %q, %d, %d", separator, primary, secondary)
	}
	if separator, primary, secondary := groupingPattern("1,000", "123456789"); separator != "," || primary != 3 || secondary != 3 {
		t.Fatalf("groupingPattern(no expanded groups) = %q, %d, %d", separator, primary, secondary)
	}
	if got := minusSign("1"); got != "-" {
		t.Fatalf("minusSign(1) = %q", got)
	}
	if got, err := boundedFormat("EUR 1.00"); err != nil || got != "EUR 1.00" {
		t.Fatalf("boundedFormat(valid) = %q, %v", got, err)
	}
	if _, err := boundedFormat(strings.Repeat("x", MaxFormattedBytes+1)); !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("boundedFormat(oversized) error = %v", err)
	}
}
