package internationaltest_test

import (
	"errors"
	"fmt"
	"testing"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/country"
	"github.com/faustbrian/golib/pkg/international/internationaltest"
	"github.com/faustbrian/golib/pkg/international/phone"
)

type recordingT struct{ message string }

func (test *recordingT) Helper() {}
func (test *recordingT) Fatalf(format string, arguments ...any) {
	test.message = fmt.Sprintf(format, arguments...)
}

func TestFixtureHelpersCoverEveryPrimitive(t *testing.T) {
	t.Parallel()
	finland := internationaltest.MustCountry(t, "FI")
	if internationaltest.MustCountryAlpha3(t, "FIN").String() != "FIN" ||
		internationaltest.MustCountryNumeric(t, "246").String() != "246" ||
		internationaltest.MustCurrencyNumeric(t, "978").String() != "978" ||
		internationaltest.MustSubdivision(t, "FI-18").String() != "FI-18" ||
		internationaltest.MustLanguage(t, "fi").String() != "fi" ||
		internationaltest.MustLocale(t, "fi-FI").String() != "fi-FI" ||
		internationaltest.MustCurrency(t, "EUR").String() != "EUR" ||
		internationaltest.MustPhone(t, "+16502530000", phone.ParseOptions{}).E164() != "+16502530000" ||
		internationaltest.MustPostal(t, "00100", finland).Raw() != "00100" {
		t.Fatal("fixture helper returned unexpected value")
	}
	internationaltest.AssertValidProvenance(t, country.DatasetProvenance())
}

func TestFormatFixtureErrorDoesNotExposeCause(t *testing.T) {
	t.Parallel()
	if internationaltest.FormatFixtureError("country", nil) != "" {
		t.Fatal("nil error was formatted")
	}
	if got := internationaltest.FormatFixtureError("country", errors.New("secret rejected value")); got != "invalid country fixture" {
		t.Fatalf("FormatFixtureError() = %q", got)
	}
}

func TestFixtureFailuresUseSafeDiagnostics(t *testing.T) {
	t.Parallel()
	recorder := &recordingT{}
	internationaltest.MustCountry(recorder, "secret invalid country")
	if recorder.message == "" {
		t.Fatal("invalid fixture did not fail")
	}
	recorder.message = ""
	internationaltest.AssertValidProvenance(recorder, international.Provenance{})
	if recorder.message == "" {
		t.Fatal("invalid provenance did not fail")
	}
}
