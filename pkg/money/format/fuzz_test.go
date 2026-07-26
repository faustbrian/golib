package format_test

import (
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/international/locale"
	"github.com/faustbrian/golib/pkg/money"
	moneyformat "github.com/faustbrian/golib/pkg/money/format"
)

func FuzzLocale(f *testing.F) {
	f.Add("1234567.89", "fi-FI", true)
	f.Add("-0.01", "ar-EG", false)
	f.Add("0", "en-US", true)

	euro, err := currency.Parse("EUR")
	if err != nil {
		f.Fatal(err)
	}
	monetaryContext, err := money.DefaultContext(euro)
	if err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, amount, languageTag string, symbol bool) {
		if len(amount) > money.MaxAmountDigits+2 || len(languageTag) > 128 {
			return
		}
		value, parseErr := money.Parse(amount, euro, monetaryContext)
		if parseErr != nil {
			return
		}
		tag, localeErr := locale.Parse(languageTag)
		if localeErr != nil {
			return
		}

		before := value.String()
		formatted, formatErr := moneyformat.Locale(value, tag, moneyformat.Options{Symbol: symbol})
		if formatErr != nil {
			return
		}
		if formatted == "" || len(formatted) > moneyformat.MaxFormattedBytes {
			t.Fatalf("Locale() returned an invalid bounded result: %q", formatted)
		}
		if !symbol && !strings.HasPrefix(formatted, "EUR ") {
			t.Fatalf("Locale() lost ISO identity: %q", formatted)
		}
		if value.String() != before {
			t.Fatalf("Locale() mutated money: before %q, after %q", before, value.String())
		}
	})
}
