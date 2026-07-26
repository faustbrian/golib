package internationaltest

import (
	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/country"
	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/international/phone"
	"github.com/faustbrian/golib/pkg/international/postal"
	"github.com/faustbrian/golib/pkg/international/subdivision"
)

const (
	// CountryVectorSource identifies the independently frozen country fixtures.
	CountryVectorSource = "Unicode CLDR 48.2 region validity and territory mappings"
	// SubdivisionVectorSource identifies the independently frozen subdivision fixtures.
	SubdivisionVectorSource = "Unicode CLDR 48.2 subdivision validity and English names"
	// LanguageVectorSource identifies the independently frozen language fixtures.
	LanguageVectorSource = "IANA Language Subtag Registry 2026-06-14"
	// LocaleVectorSource identifies the independently frozen locale fixtures.
	LocaleVectorSource = "IETF BCP 47 examples and IANA registry 2026-06-14"
	// CurrencyVectorSource identifies the independently frozen currency fixtures.
	CurrencyVectorSource = "SIX ISO 4217 List One and List Three 2026-01-01"
	// PhoneVectorSource identifies the public upstream example-number fixtures.
	PhoneVectorSource = "libphonenumber v9.0.32 public example-number ranges"
	// PhoneMetadataVersion is the upstream behavior version frozen by PhoneVectors.
	PhoneMetadataVersion = "libphonenumber v9.0.32"
	// PostalVectorSource identifies the package-policy postal fixtures.
	PostalVectorSource = "international bounded opaque postal policy v1"
)

// CountryVector is an independent ISO 3166-1 mapping and status assertion.
type CountryVector struct {
	Alpha2  string
	Alpha3  string
	Numeric string
	Status  international.Status
	Options country.ParseOptions
}

// SubdivisionVector is an independent ISO 3166-2-derived assertion.
type SubdivisionVector struct {
	Code    string
	Country string
	Status  international.Status
	Options subdivision.ParseOptions
}

// LanguageVector is an independent canonical ISO 639 mapping assertion.
type LanguageVector struct {
	Code string
	ISO3 string
}

// LocaleVector is an independent BCP 47 canonicalization assertion.
type LocaleVector struct {
	Input     string
	Canonical string
}

// CurrencyVector is an independent ISO 4217 mapping and metadata assertion.
type CurrencyVector struct {
	Alphabetic    string
	Numeric       string
	MinorUnits    uint8
	HasMinorUnits bool
	Status        international.Status
	Options       currency.ParseOptions
}

// PhoneVector is an independent public libphonenumber behavior assertion.
type PhoneVector struct {
	MetadataVersion string
	Input           string
	Region          string
	E164            string
	Extension       string
	National        string
	International   string
	Type            phone.NumberType
	Possible        bool
	Valid           bool
}

// PostalVector is a package-policy normalization assertion without syntax or
// deliverability meaning.
type PostalVector struct {
	Country    string
	Input      string
	Normalized string
	Options    postal.NormalizeOptions
}

var countryVectors = [...]CountryVector{
	{Alpha2: "FI", Alpha3: "FIN", Numeric: "246", Status: international.StatusOfficial},
	{Alpha2: "US", Alpha3: "USA", Numeric: "840", Status: international.StatusOfficial},
	{Alpha2: "AN", Alpha3: "ANT", Numeric: "530", Status: international.StatusDeleted,
		Options: country.ParseOptions{AllowHistoric: true}},
	{Alpha2: "QM", Alpha3: "QMM", Numeric: "959", Status: international.StatusReserved,
		Options: country.ParseOptions{AllowReserved: true}},
}

var subdivisionVectors = [...]SubdivisionVector{
	{Code: "US-CA", Country: "US", Status: international.StatusOfficial},
	{Code: "FI-18", Country: "FI", Status: international.StatusOfficial},
	{Code: "FI-01", Country: "FI", Status: international.StatusDeleted,
		Options: subdivision.ParseOptions{AllowHistoric: true}},
}

var languageVectors = [...]LanguageVector{
	{Code: "en", ISO3: "eng"},
	{Code: "fi", ISO3: "fin"},
	{Code: "ace", ISO3: "ace"},
}

var localeVectors = [...]LocaleVector{
	{Input: "EN-latn-us-u-ca-gregory-x-test", Canonical: "en-US-u-ca-gregory-x-test"},
	{Input: "de-CH-1901", Canonical: "de-CH-1901"},
	{Input: "sl-Latn-IT-rozaj-biske", Canonical: "sl-IT-rozaj-biske"},
}

var currencyVectors = [...]CurrencyVector{
	{Alphabetic: "EUR", Numeric: "978", MinorUnits: 2, HasMinorUnits: true,
		Status: international.StatusOfficial},
	{Alphabetic: "JPY", Numeric: "392", HasMinorUnits: true,
		Status: international.StatusOfficial},
	{Alphabetic: "XAU", Numeric: "959", Status: international.StatusOfficial},
	{Alphabetic: "FIM", Numeric: "246", Status: international.StatusHistoric,
		Options: currency.ParseOptions{AllowHistoric: true}},
}

var phoneVectors = [...]PhoneVector{
	{MetadataVersion: PhoneMetadataVersion,
		Input: "+1 650 253 0000", Region: "US", E164: "+16502530000",
		National: "(650) 253-0000", International: "+1 650-253-0000",
		Type: phone.TypeFixedLineOrMobile, Possible: true, Valid: true},
	{MetadataVersion: PhoneMetadataVersion,
		Input: "+44 20 7031 3000", Region: "GB", E164: "+442070313000",
		National: "020 7031 3000", International: "+44 20 7031 3000",
		Type: phone.TypeFixedLine, Possible: true, Valid: true},
	{MetadataVersion: PhoneMetadataVersion,
		Input: "+358 40 123 4567 ext. 12", Region: "FI", E164: "+358401234567",
		Extension: "12", National: "040 1234567 ext. 12",
		International: "+358 40 1234567 ext. 12",
		Type:          phone.TypeMobile, Possible: true, Valid: true},
	{MetadataVersion: PhoneMetadataVersion,
		Input: "+81 3 1234 5678", Region: "JP", E164: "+81312345678",
		National: "03-1234-5678", International: "+81 3-1234-5678",
		Type: phone.TypeFixedLine, Possible: true, Valid: true},
	{MetadataVersion: PhoneMetadataVersion,
		Input: "+61 2 9374 4000", Region: "AU", E164: "+61293744000",
		National: "(02) 9374 4000", International: "+61 2 9374 4000",
		Type: phone.TypeFixedLine, Possible: true, Valid: true},
}

var postalVectors = [...]PostalVector{
	{Country: "FI", Input: " 00100 ", Normalized: "00100",
		Options: postal.NormalizeOptions{Spaces: postal.SpacesCollapseASCII}},
	{Country: "CA", Input: "h2x\u00a01y4", Normalized: "H2X 1Y4",
		Options: postal.NormalizeOptions{Spaces: postal.SpacesCollapseASCII, Case: postal.CaseUpperASCII}},
}

// CountryVectors returns an independent copy of the governed country vectors.
func CountryVectors() []CountryVector { return copyVectors(countryVectors[:]) }

// SubdivisionVectors returns an independent copy of the governed subdivision vectors.
func SubdivisionVectors() []SubdivisionVector { return copyVectors(subdivisionVectors[:]) }

// LanguageVectors returns an independent copy of the governed language vectors.
func LanguageVectors() []LanguageVector { return copyVectors(languageVectors[:]) }

// LocaleVectors returns an independent copy of the governed locale vectors.
func LocaleVectors() []LocaleVector { return copyVectors(localeVectors[:]) }

// CurrencyVectors returns an independent copy of the governed currency vectors.
func CurrencyVectors() []CurrencyVector { return copyVectors(currencyVectors[:]) }

// PhoneVectors returns an independent copy of the governed public phone vectors.
func PhoneVectors() []PhoneVector { return copyVectors(phoneVectors[:]) }

// PostalVectors returns an independent copy of the governed postal policy vectors.
func PostalVectors() []PostalVector { return copyVectors(postalVectors[:]) }

func copyVectors[T any](vectors []T) []T { return append([]T(nil), vectors...) }
