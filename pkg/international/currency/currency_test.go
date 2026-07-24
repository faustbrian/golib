package currency_test

import (
	"errors"
	"testing"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/currency"
)

func TestParseActiveCurrencyWithNumericAndMinorUnits(t *testing.T) {
	t.Parallel()

	code, err := currency.Parse("EUR")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	numeric, numericOK := code.Numeric()
	if code.String() != "EUR" || !numericOK || numeric.String() != "978" || code.Status() != international.StatusOfficial {
		t.Fatalf("code = %q, numeric %q, status %v", code, numeric, code.Status())
	}
	minor, ok := code.MinorUnits()
	if !ok || minor != 2 {
		t.Fatalf("MinorUnits() = %d, %v, want 2, true", minor, ok)
	}
	if code.Name() != "Euro" || code.WithdrawalDate() != "" {
		t.Fatalf("metadata = name %q, withdrawal %q", code.Name(), code.WithdrawalDate())
	}
}

func TestMinorUnitsDistinguishZeroFromNotApplicable(t *testing.T) {
	t.Parallel()

	jpy, err := currency.Parse("JPY")
	if err != nil {
		t.Fatalf("Parse(JPY) error = %v", err)
	}
	minor, ok := jpy.MinorUnits()
	if !ok || minor != 0 {
		t.Fatalf("JPY MinorUnits() = %d, %v, want 0, true", minor, ok)
	}

	gold, err := currency.Parse("XAU")
	if err != nil {
		t.Fatalf("Parse(XAU) error = %v", err)
	}
	if _, ok := gold.MinorUnits(); ok {
		t.Fatal("XAU MinorUnits() ok = true, want not applicable")
	}
}

func TestHistoricCurrenciesRequireOptInAndPreserveWithdrawalText(t *testing.T) {
	t.Parallel()

	if _, err := currency.Parse("FIM"); !errors.Is(err, international.ErrInvalid) {
		t.Fatalf("Parse(FIM) error = %v, want ErrInvalid", err)
	}
	historic, err := currency.ParseWithOptions("FIM", currency.ParseOptions{AllowHistoric: true})
	if err != nil {
		t.Fatalf("ParseWithOptions(FIM) error = %v", err)
	}
	numeric, numericOK := historic.Numeric()
	if historic.Status() != international.StatusHistoric || !numericOK || numeric.String() != "246" {
		t.Fatalf("historic = %q, numeric %q, status %v", historic, numeric, historic.Status())
	}
	if historic.WithdrawalDate() != "2002-03" {
		t.Fatalf("WithdrawalDate() = %q, want 2002-03", historic.WithdrawalDate())
	}
	if historic.Name() != "Markka" {
		t.Fatalf("Name() = %q, want Markka", historic.Name())
	}
	if _, ok := historic.MinorUnits(); ok {
		t.Fatal("historic minor units unexpectedly inferred")
	}

	ruble, err := currency.ParseWithOptions("RUR", currency.ParseOptions{AllowHistoric: true})
	if err != nil {
		t.Fatalf("ParseWithOptions(RUR) error = %v", err)
	}
	if ruble.WithdrawalDate() != "" || len(ruble.WithdrawalDates()) < 2 {
		t.Fatalf("RUR withdrawal metadata = %q, %v", ruble.WithdrawalDate(), ruble.WithdrawalDates())
	}

	kuna, err := currency.ParseWithOptions("HRK", currency.ParseOptions{AllowHistoric: true})
	if err != nil {
		t.Fatalf("ParseWithOptions(HRK) error = %v", err)
	}
	if kuna.Name() != "" || len(kuna.History()) != 2 {
		t.Fatalf("HRK metadata = name %q, history %v", kuna.Name(), kuna.History())
	}
}

func TestHistoricCurrencyPersistenceRequiresExplicitOptions(t *testing.T) {
	t.Parallel()

	options := currency.ParseOptions{AllowHistoric: true}
	var fromText currency.Code
	if err := fromText.UnmarshalTextWithOptions([]byte("FIM"), options); err != nil ||
		fromText.Status() != international.StatusHistoric {
		t.Fatalf("historic text = %q, %v", fromText, err)
	}
	var fromJSON currency.Code
	if err := fromJSON.UnmarshalJSONWithOptions([]byte(`"FIM"`), options); err != nil ||
		fromJSON != fromText {
		t.Fatalf("historic JSON = %q, %v", fromJSON, err)
	}
	var fromSQL currency.Code
	if err := fromSQL.ScanWithOptions("FIM", options); err != nil || fromSQL != fromText {
		t.Fatalf("historic SQL = %q, %v", fromSQL, err)
	}

	numeric, err := currency.ParseNumericWithOptions("246", options)
	if err != nil {
		t.Fatalf("ParseNumericWithOptions(246) error = %v", err)
	}
	alphabetic, ok := numeric.Alphabetic()
	if !ok || alphabetic.String() != "FIM" || numeric.Status() != international.StatusHistoric {
		t.Fatalf("historic numeric = %q -> %q, status %v", numeric, alphabetic, numeric.Status())
	}
	var numericJSON currency.Numeric
	if err := numericJSON.UnmarshalJSONWithOptions([]byte(`"246"`), options); err != nil ||
		numericJSON != numeric {
		t.Fatalf("historic numeric JSON = %q, %v", numericJSON, err)
	}
	if _, err := currency.ParseNumericWithOptions("008", options); !errors.Is(err, international.ErrInvalid) {
		t.Fatalf("reused historic numeric error = %v, want ErrInvalid", err)
	}
}

func TestParseNumericUsesCurrentAuthoritativeMapping(t *testing.T) {
	t.Parallel()

	code, err := currency.ParseNumeric("840")
	if err != nil {
		t.Fatalf("ParseNumeric() error = %v", err)
	}
	alphabetic, ok := code.Alphabetic()
	if code.String() != "840" || !ok || alphabetic.String() != "USD" {
		t.Fatalf("ParseNumeric() = %q -> %q, want 840 -> USD", code, alphabetic)
	}
	noCurrency, err := currency.ParseNumeric("999")
	noCurrencyAlphabetic, ok := noCurrency.Alphabetic()
	if err != nil || noCurrency.String() != "999" || !ok || noCurrencyAlphabetic.String() != "XXX" {
		t.Fatalf("ParseNumeric(999) = %q -> %q, %v", noCurrency, noCurrencyAlphabetic, err)
	}
	for _, input := range []string{"84", "0840", "8A0", "000"} {
		if _, err := currency.ParseNumeric(input); !errors.Is(err, international.ErrInvalid) {
			t.Errorf("ParseNumeric(%q) error = %v, want ErrInvalid", input, err)
		}
	}
}

func TestCurrencyParsingIsStrict(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", "EU", "EURO", "eur", "ZZZ", "\xffUR"} {
		if _, err := currency.Parse(input); !errors.Is(err, international.ErrInvalid) {
			t.Errorf("Parse(%q) error = %v, want ErrInvalid", input, err)
		}
	}
}

func TestAllReturnsIndependentSortedActiveCodes(t *testing.T) {
	t.Parallel()

	first := currency.All()
	if len(first) < 150 {
		t.Fatalf("len(All()) = %d, want complete current list", len(first))
	}
	if first[0].String() != "AED" || first[len(first)-1].String() != "ZWG" {
		t.Fatalf("All() bounds = %q..%q, want AED..ZWG", first[0], first[len(first)-1])
	}
	first[0] = currency.Code{}
	if currency.All()[0].String() != "AED" {
		t.Fatal("All() returned shared mutable backing storage")
	}
}

func TestZeroCurrencyHasAbsentSemantics(t *testing.T) {
	t.Parallel()

	var code currency.Code
	if !code.IsZero() || code.String() != "" || code.Name() != "" ||
		code.WithdrawalDate() != "" || code.Status() != international.StatusUnknown {
		t.Fatalf("zero code metadata is not absent: %#v", code)
	}
	if _, ok := code.MinorUnits(); ok {
		t.Fatal("zero MinorUnits() ok = true")
	}
	if _, ok := code.Numeric(); ok {
		t.Fatal("zero Numeric() ok = true")
	}
	var numeric currency.Numeric
	if !numeric.IsZero() || numeric.String() != "" || numeric.Status() != international.StatusUnknown {
		t.Fatal("zero numeric is not absent")
	}
	if _, ok := numeric.Alphabetic(); ok {
		t.Fatal("zero numeric has an alphabetic mapping")
	}
	if code.WithdrawalDates() != nil || code.History() != nil {
		t.Fatal("zero historic metadata is not absent")
	}
}

func TestCurrencyDatasetProvenanceIsOfficialAndPinned(t *testing.T) {
	t.Parallel()

	provenance := currency.DatasetProvenance()
	if err := provenance.Validate(); err != nil {
		t.Fatalf("DatasetProvenance().Validate() error = %v", err)
	}
	if provenance.UpstreamVersion != "ISO 4217 2026-01-01" {
		t.Fatalf("version = %q", provenance.UpstreamVersion)
	}
}

func TestCurrencyDatasetRecordsAreCompleteSortedAndIndependent(t *testing.T) {
	t.Parallel()

	first := currency.DatasetRecords()
	if len(first) != 307 || first[0].ID != "ADP" || first[len(first)-1].ID != "ZWR" {
		t.Fatalf("dataset record bounds = %d, %q..%q", len(first), first[0].ID, first[len(first)-1].ID)
	}
	if first[0].Fingerprint == "" || first[0].Status == international.StatusUnknown {
		t.Fatalf("first dataset record lacks review metadata: %#v", first[0])
	}
	first[0].ID = "changed"
	if currency.DatasetRecords()[0].ID != "ADP" {
		t.Fatal("DatasetRecords returned shared mutable data")
	}
}
