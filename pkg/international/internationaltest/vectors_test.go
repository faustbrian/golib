package internationaltest_test

import (
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/international/country"
	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/international/internationaltest"
	"github.com/faustbrian/golib/pkg/international/language"
	"github.com/faustbrian/golib/pkg/international/locale"
	"github.com/faustbrian/golib/pkg/international/phone"
	"github.com/faustbrian/golib/pkg/international/postal"
	"github.com/faustbrian/golib/pkg/international/subdivision"
)

func TestAuthoritativeIdentifierVectors(t *testing.T) {
	t.Parallel()

	for _, vector := range internationaltest.CountryVectors() {
		code, err := country.ParseWithOptions(vector.Alpha2, vector.Options)
		if err != nil {
			t.Fatalf("country vector %s: %v", vector.Alpha2, err)
		}
		alpha3, alpha3OK := code.Alpha3()
		numeric, numericOK := code.Numeric()
		if !alpha3OK || !numericOK || alpha3.String() != vector.Alpha3 ||
			numeric.String() != vector.Numeric || code.Status() != vector.Status {
			t.Fatalf("country vector %s mismatch", vector.Alpha2)
		}
	}

	for _, vector := range internationaltest.SubdivisionVectors() {
		code, err := subdivision.ParseWithOptions(vector.Code, vector.Options)
		if err != nil || code.Country().String() != vector.Country || code.Status() != vector.Status {
			t.Fatalf("subdivision vector %s = %q, %v", vector.Code, code, err)
		}
	}

	for _, vector := range internationaltest.LanguageVectors() {
		code, err := language.Parse(vector.Code)
		if err != nil || code.ISO3() != vector.ISO3 {
			t.Fatalf("language vector %s = %q, %v", vector.Code, code.ISO3(), err)
		}
	}

	for _, vector := range internationaltest.LocaleVectors() {
		tag, err := locale.Parse(vector.Input)
		if err != nil {
			t.Fatalf("locale vector %s: %v", vector.Input, err)
		}
		canonical, err := tag.Canonical()
		if err != nil || canonical.String() != vector.Canonical {
			t.Fatalf("locale vector %s = %q, %v", vector.Input, canonical, err)
		}
	}

	for _, vector := range internationaltest.CurrencyVectors() {
		code, err := currency.ParseWithOptions(vector.Alphabetic, vector.Options)
		if err != nil {
			t.Fatalf("currency vector %s: %v", vector.Alphabetic, err)
		}
		numeric, numericOK := code.Numeric()
		minor, minorOK := code.MinorUnits()
		if !numericOK || numeric.String() != vector.Numeric || minor != vector.MinorUnits ||
			minorOK != vector.HasMinorUnits || code.Status() != vector.Status {
			t.Fatalf("currency vector %s mismatch", vector.Alphabetic)
		}
	}
}

func TestIndependentPhoneAndPostalVectors(t *testing.T) {
	t.Parallel()

	metadataVersion := phone.DatasetProvenance().UpstreamVersion
	for _, vector := range internationaltest.PhoneVectors() {
		if vector.MetadataVersion == "" ||
			!strings.Contains(metadataVersion, vector.MetadataVersion) {
			t.Fatalf("phone vector metadata version %q is not active", vector.MetadataVersion)
		}
		region, err := country.Parse(vector.Region)
		if err != nil {
			t.Fatal(err)
		}
		number, err := phone.Parse(vector.Input, phone.ParseOptions{RegionHint: region})
		if err != nil {
			t.Fatalf("phone vector %s: %v", vector.Region, err)
		}
		national, nationalErr := number.Format(phone.FormatNational)
		international, internationalErr := number.Format(phone.FormatInternational)
		if number.E164() != vector.E164 || number.Extension() != vector.Extension ||
			number.Possible() != vector.Possible || number.Valid() != vector.Valid ||
			number.Type() != vector.Type || national != vector.National ||
			international != vector.International || nationalErr != nil || internationalErr != nil {
			t.Fatalf("phone vector %s mismatch: %s / %s", vector.Region, national, international)
		}
	}

	for _, vector := range internationaltest.PostalVectors() {
		context, err := country.Parse(vector.Country)
		if err != nil {
			t.Fatal(err)
		}
		code, err := postal.Parse(vector.Input, context)
		if err != nil {
			t.Fatalf("postal vector %s: %v", vector.Country, err)
		}
		normalized, err := code.Normalize(vector.Options)
		if err != nil || normalized.Raw() != vector.Normalized || normalized.Country() != context {
			t.Fatalf("postal vector %s = %q, %v", vector.Country, normalized.Raw(), err)
		}
	}
}
