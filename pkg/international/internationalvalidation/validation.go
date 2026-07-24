// Package internationalvalidation integrates strict international parsers with
// validation without introducing validation dependencies into core types.
package internationalvalidation

import (
	"errors"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/country"
	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/international/language"
	"github.com/faustbrian/golib/pkg/international/locale"
	"github.com/faustbrian/golib/pkg/international/phone"
	"github.com/faustbrian/golib/pkg/international/postal"
	"github.com/faustbrian/golib/pkg/international/subdivision"
	validation "github.com/faustbrian/golib/pkg/validation"
)

// Country returns a strict current ISO 3166-1 alpha-2 rule.
func Country() validation.Validator[string] { return parserRule("country", country.Parse) }

// CountryAlpha3 returns a strict current ISO 3166-1 alpha-3 rule.
func CountryAlpha3() validation.Validator[string] {
	return parserRule("country_alpha3", country.ParseAlpha3)
}

// CountryNumeric returns a strict current ISO 3166-1 numeric rule.
func CountryNumeric() validation.Validator[string] {
	return parserRule("country_numeric", country.ParseNumeric)
}

// Subdivision returns a strict current ISO 3166-2 rule.
func Subdivision() validation.Validator[string] {
	return parserRule("subdivision", subdivision.Parse)
}

// Language returns a strict current canonical ISO 639 rule.
func Language() validation.Validator[string] { return parserRule("language", language.Parse) }

// Locale returns a bounded standards-aware BCP 47 rule.
func Locale() validation.Validator[string] { return parserRule("locale", locale.Parse) }

// Currency returns a strict active ISO 4217 alphabetic rule.
func Currency() validation.Validator[string] { return parserRule("currency", currency.Parse) }

// CurrencyNumeric returns a strict current ISO 4217 numeric rule.
func CurrencyNumeric() validation.Validator[string] {
	return parserRule("currency_numeric", currency.ParseNumeric)
}

// CallingCode returns a supported plus-prefixed ITU calling-code rule.
func CallingCode() validation.Validator[string] {
	return parserRule("calling_code", phone.ParseCallingCode)
}

// Phone returns a bounded libphonenumber-backed parseability rule. It does not
// claim ownership, reachability, identity, or assignment validity.
func Phone(options phone.ParseOptions) validation.Validator[string] {
	return parserRule("phone", func(value string) (phone.Number, error) {
		return phone.Parse(value, options)
	})
}

// ValidPhone additionally requires current metadata to classify the parsed
// number as valid. This still makes no ownership or reachability claim.
func ValidPhone(options phone.ParseOptions) validation.Validator[string] {
	return validation.ValidatorFunc[string](func(ctx validation.Context, value string) validation.Report {
		number, err := phone.Parse(value, options)
		if err != nil || !number.Valid() {
			return invalid(ctx, "valid_phone", err)
		}
		return validation.NewReport(ctx.Limits())
	})
}

// Postal returns a bounded value rule that preserves the explicit country
// context. It makes no syntax or deliverability claim.
func Postal(context country.Code) validation.Validator[string] {
	return parserRule("postal", func(value string) (postal.Code, error) {
		return postal.Parse(value, context)
	})
}

func parserRule[T any](code string, parse func(string) (T, error)) validation.Validator[string] {
	return validation.ValidatorFunc[string](func(ctx validation.Context, value string) validation.Report {
		if _, err := parse(value); err != nil {
			return invalid(ctx, code, err)
		}
		return validation.NewReport(ctx.Limits())
	})
}

func invalid(ctx validation.Context, code string, cause error) validation.Report {
	safeCause := international.ErrInvalid
	if errors.Is(cause, international.ErrResourceLimit) {
		safeCause = international.ErrResourceLimit
	}
	return validation.NewReport(ctx.Limits()).Add(validation.NewViolation(
		ctx.Path(), code, validation.Error, nil, safeCause,
	))
}
