package international_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/config/decode"
	"github.com/faustbrian/golib/pkg/international/country"
	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/international/language"
	"github.com/faustbrian/golib/pkg/international/locale"
	"github.com/faustbrian/golib/pkg/international/phone"
	"github.com/faustbrian/golib/pkg/international/postal"
	"github.com/faustbrian/golib/pkg/international/subdivision"
)

func TestGoConfigUsesStrictTextContracts(t *testing.T) {
	t.Parallel()
	type configuration struct {
		Country     country.Code
		Country3    country.Alpha3
		CountryNum  country.Numeric
		Currency    currency.Code
		CurrencyNum currency.Numeric
		Language    language.Code
		Locale      locale.Tag
		Phone       phone.Number
		CallingCode phone.CallingCode
		Postal      postal.Code
		Subdivision subdivision.Code
	}
	input := map[string]any{
		"country": "FI", "country3": "FIN", "countrynum": "246",
		"currency": "EUR", "currencynum": "978", "language": "fi",
		"locale": "fi-FI", "phone": "+16502530000",
		"callingcode": "+358", "postal": "FI\t00100",
		"subdivision": "FI-18",
	}
	var decoded configuration
	if err := decode.Into(input, &decoded); err != nil {
		t.Fatalf("decode.Into() error = %v", err)
	}
	if decoded.Country.String() != "FI" || decoded.Postal.Raw() != "00100" ||
		decoded.Phone.E164() != "+16502530000" {
		t.Fatalf("decoded configuration = %#v", decoded)
	}

	unchanged := decoded
	input["country"] = "fi"
	if err := decode.Into(input, &decoded); err == nil || decoded != unchanged {
		t.Fatalf("invalid atomic decode = %#v, %v", decoded, err)
	}
}
