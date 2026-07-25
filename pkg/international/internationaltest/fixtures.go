// Package internationaltest provides reusable authoritative fixture helpers
// and provenance assertions for downstream package tests.
package internationaltest

import (
	"fmt"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/country"
	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/international/language"
	"github.com/faustbrian/golib/pkg/international/locale"
	"github.com/faustbrian/golib/pkg/international/phone"
	"github.com/faustbrian/golib/pkg/international/postal"
	"github.com/faustbrian/golib/pkg/international/subdivision"
)

// TestingT is the subset of testing.TB used by helpers.
type TestingT interface {
	Helper()
	Fatalf(format string, arguments ...any)
}

// MustCountry parses a current country fixture or fails the test.
func MustCountry(test TestingT, value string) country.Code {
	parsed, err := country.Parse(value)
	return must(test, "country", parsed, err)
}

// MustCountryAlpha3 parses a current alpha-3 country fixture or fails the test.
func MustCountryAlpha3(test TestingT, value string) country.Alpha3 {
	parsed, err := country.ParseAlpha3(value)
	return must(test, "country alpha-3", parsed, err)
}

// MustCountryNumeric parses a current numeric country fixture or fails the test.
func MustCountryNumeric(test TestingT, value string) country.Numeric {
	parsed, err := country.ParseNumeric(value)
	return must(test, "country numeric", parsed, err)
}

// MustSubdivision parses a current subdivision fixture or fails the test.
func MustSubdivision(test TestingT, value string) subdivision.Code {
	parsed, err := subdivision.Parse(value)
	return must(test, "subdivision", parsed, err)
}

// MustLanguage parses a current language fixture or fails the test.
func MustLanguage(test TestingT, value string) language.Code {
	parsed, err := language.Parse(value)
	return must(test, "language", parsed, err)
}

// MustLocale parses a BCP 47 fixture or fails the test.
func MustLocale(test TestingT, value string) locale.Tag {
	parsed, err := locale.Parse(value)
	return must(test, "locale", parsed, err)
}

// MustCurrency parses an active currency fixture or fails the test.
func MustCurrency(test TestingT, value string) currency.Code {
	parsed, err := currency.Parse(value)
	return must(test, "currency", parsed, err)
}

// MustCurrencyNumeric parses a current numeric currency fixture or fails the test.
func MustCurrencyNumeric(test TestingT, value string) currency.Numeric {
	parsed, err := currency.ParseNumeric(value)
	return must(test, "currency numeric", parsed, err)
}

// MustPhone parses a phone fixture under explicit options or fails the test.
func MustPhone(test TestingT, value string, options phone.ParseOptions) phone.Number {
	parsed, err := phone.Parse(value, options)
	return must(test, "phone", parsed, err)
}

// MustPostal parses a bounded postal fixture under explicit country context.
func MustPostal(test TestingT, value string, context country.Code) postal.Code {
	parsed, err := postal.Parse(value, context)
	return must(test, "postal", parsed, err)
}

// AssertValidProvenance fails unless provenance is complete and reproducible.
func AssertValidProvenance(test TestingT, provenance international.Provenance) {
	test.Helper()
	if err := provenance.Validate(); err != nil {
		test.Fatalf("invalid dataset provenance: %v", err)
	}
}

func must[T any](test TestingT, kind string, value T, err error) T {
	test.Helper()
	if err != nil {
		test.Fatalf("parse %s fixture: %v", kind, err)
	}
	return value
}

// FormatFixtureError returns a value-safe diagnostic for fixture tooling.
func FormatFixtureError(kind string, err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("invalid %s fixture", kind)
}
