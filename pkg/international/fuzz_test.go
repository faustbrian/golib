package international_test

import (
	"strings"
	"testing"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/country"
	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/international/language"
	"github.com/faustbrian/golib/pkg/international/locale"
	"github.com/faustbrian/golib/pkg/international/phone"
	"github.com/faustbrian/golib/pkg/international/postal"
	"github.com/faustbrian/golib/pkg/international/subdivision"
)

func FuzzTextParsers(fuzzer *testing.F) {
	for _, seed := range []string{
		"FI", "fi", "fi-FI", "EUR", "FI-18", "\xff", "",
		"Fİ", "ＦＩ", "FΙ", "fi\u200d", "e\u0301", " en-US ",
		"sl-Latn-IT-rozaj-biske-u-ca-gregory-x-private",
		strings.Repeat("a-", locale.MaxSegments+1),
		strings.Repeat("x", locale.MaxBytes+1),
	} {
		fuzzer.Add(seed)
	}
	fuzzer.Fuzz(func(_ *testing.T, input string) {
		_, _ = country.Parse(input)
		_, _ = country.ParseAlpha3(input)
		_, _ = country.ParseNumeric(input)
		_, _ = currency.Parse(input)
		_, _ = currency.ParseNumeric(input)
		_, _ = language.Parse(input)
		_, _ = language.ParseISO3(input)
		_, _ = locale.Parse(input)
		_, _ = subdivision.Parse(input)
		_, _ = phone.ParseCallingCode(input)
		_, _ = international.ParseStatus(input)
	})
}

func FuzzPhoneAndPostalBoundedParsing(fuzzer *testing.F) {
	fuzzer.Add("+16502530000", "00100")
	fuzzer.Add("\xff", "\xff")
	fuzzer.Add("+١٦٥٠٢٥٣٠٠٠٠", "A\u0301 1")
	fuzzer.Add("+1\u00a0650\u2007253 0000 ext. １２", "\u200d00100")
	fuzzer.Add(strings.Repeat("1", phone.MaxBytes+1), strings.Repeat("X", postal.MaxBytes+1))
	fuzzer.Fuzz(func(_ *testing.T, numberText, postalText string) {
		_, _ = phone.Parse(numberText, phone.ParseOptions{})
		finland, _ := country.Parse("FI")
		value, err := postal.Parse(postalText, finland)
		if err == nil {
			_, _ = value.Normalize(postal.NormalizeOptions{Spaces: postal.SpacesCollapseASCII, Case: postal.CaseUpperASCII, Unicode: postal.UnicodeNFC})
		}
	})
}

func FuzzPersistenceDecoders(fuzzer *testing.F) {
	fuzzer.Add([]byte(`"FI"`))
	fuzzer.Add([]byte("null"))
	fuzzer.Fuzz(func(_ *testing.T, input []byte) {
		var countryCode country.Code
		_ = countryCode.UnmarshalJSON(input)
		_ = countryCode.UnmarshalText(input)
		var alpha3 country.Alpha3
		_ = alpha3.UnmarshalJSON(input)
		_ = alpha3.UnmarshalText(input)
		var countryNumeric country.Numeric
		_ = countryNumeric.UnmarshalJSON(input)
		_ = countryNumeric.UnmarshalText(input)
		var currencyCode currency.Code
		_ = currencyCode.UnmarshalJSON(input)
		_ = currencyCode.UnmarshalText(input)
		var currencyNumeric currency.Numeric
		_ = currencyNumeric.UnmarshalJSON(input)
		_ = currencyNumeric.UnmarshalText(input)
		var languageCode language.Code
		_ = languageCode.UnmarshalJSON(input)
		_ = languageCode.UnmarshalText(input)
		var localeTag locale.Tag
		_ = localeTag.UnmarshalJSON(input)
		_ = localeTag.UnmarshalText(input)
		var subdivisionCode subdivision.Code
		_ = subdivisionCode.UnmarshalJSON(input)
		_ = subdivisionCode.UnmarshalText(input)
		var number phone.Number
		_ = number.UnmarshalJSON(input)
		_ = number.UnmarshalText(input)
		var callingCode phone.CallingCode
		_ = callingCode.UnmarshalJSON(input)
		_ = callingCode.UnmarshalText(input)
		var code postal.Code
		_ = code.UnmarshalJSON(input)
		_ = code.UnmarshalText(input)
	})
}
