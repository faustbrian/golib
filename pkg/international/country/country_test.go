package country_test

import (
	"errors"
	"testing"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/country"
	"golang.org/x/text/language"
)

func TestParseOfficialCountryAndConvertAuthoritativeRepresentations(t *testing.T) {
	t.Parallel()

	code, err := country.Parse("FI")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := code.String(); got != "FI" {
		t.Fatalf("String() = %q, want FI", got)
	}
	if got := code.Status(); got != international.StatusOfficial {
		t.Fatalf("Status() = %v, want official", got)
	}

	alpha3, ok := code.Alpha3()
	if !ok || alpha3.String() != "FIN" {
		t.Fatalf("Alpha3() = %q, %v, want FIN, true", alpha3, ok)
	}
	numeric, ok := code.Numeric()
	if !ok || numeric.String() != "246" || numeric.Int() != 246 {
		t.Fatalf("Numeric() = %q (%d), %v, want 246, true", numeric, numeric.Int(), ok)
	}

	if got := country.Name(code, language.Finnish); got != "Suomi" {
		t.Fatalf("Name() = %q, want Suomi", got)
	}
}

func TestParsingIsStrictAndCanonicalizationIsExplicit(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", "F", "FIN", "fi", "F1", "ZZ", "AN", "XK", "\xffI"} {
		if _, err := country.Parse(input); !errors.Is(err, international.ErrInvalid) {
			t.Errorf("Parse(%q) error = %v, want ErrInvalid", input, err)
		}
	}

	canonical, err := country.Canonicalize("fi")
	if err != nil {
		t.Fatalf("Canonicalize() error = %v", err)
	}
	if canonical.String() != "FI" {
		t.Fatalf("Canonicalize() = %q, want FI", canonical)
	}
	for _, input := range []string{"F", "F1", "\xffI"} {
		if _, err := country.Canonicalize(input); !errors.Is(err, international.ErrInvalid) {
			t.Errorf("Canonicalize(%q) error = %v, want ErrInvalid", input, err)
		}
	}
}

func TestHistoricalAndUserAssignedCodesRequireOptIn(t *testing.T) {
	t.Parallel()

	historic, err := country.ParseWithOptions("AN", country.ParseOptions{AllowHistoric: true})
	if err != nil {
		t.Fatalf("ParseWithOptions(AN) error = %v", err)
	}
	if historic.Status() != international.StatusDeleted {
		t.Fatalf("AN status = %v, want deleted", historic.Status())
	}
	historicAlpha3, err := country.ParseAlpha3WithOptions("ANT", country.ParseOptions{AllowHistoric: true})
	if err != nil || historicAlpha3.Status() != international.StatusDeleted {
		t.Fatalf("historic alpha-3 = %q, %v", historicAlpha3, err)
	}
	historicNumeric, err := country.ParseNumericWithOptions("530", country.ParseOptions{AllowHistoric: true})
	if err != nil || historicNumeric.Status() != international.StatusDeleted {
		t.Fatalf("historic numeric = %q, %v", historicNumeric, err)
	}
	if alpha3, ok := historic.Alpha3(); !ok || alpha3.String() != "ANT" {
		t.Fatalf("AN Alpha3() = %q, %v, want ANT, true", alpha3, ok)
	}

	userAssigned, err := country.ParseWithOptions(
		"XK",
		country.ParseOptions{AllowUserAssigned: true},
	)
	if err != nil {
		t.Fatalf("ParseWithOptions(XK) error = %v", err)
	}
	if userAssigned.Status() != international.StatusUserAssigned {
		t.Fatalf("XK status = %v, want user-assigned", userAssigned.Status())
	}

	reserved, err := country.ParseWithOptions("QM", country.ParseOptions{AllowReserved: true})
	if err != nil {
		t.Fatalf("ParseWithOptions(QM) error = %v", err)
	}
	if reserved.Status() != international.StatusReserved {
		t.Fatalf("QM status = %v, want reserved", reserved.Status())
	}
}

func TestHistoricCountryPersistenceRequiresExplicitOptions(t *testing.T) {
	t.Parallel()

	options := country.ParseOptions{AllowHistoric: true}
	var alpha2 country.Code
	if err := alpha2.UnmarshalTextWithOptions([]byte("AN"), options); err != nil ||
		alpha2.Status() != international.StatusDeleted {
		t.Fatalf("historic alpha-2 text = %q, %v", alpha2, err)
	}
	var alpha3 country.Alpha3
	if err := alpha3.UnmarshalJSONWithOptions([]byte(`"ANT"`), options); err != nil ||
		alpha3.Status() != international.StatusDeleted {
		t.Fatalf("historic alpha-3 JSON = %q, %v", alpha3, err)
	}
	var numeric country.Numeric
	if err := numeric.ScanWithOptions("530", options); err != nil ||
		numeric.Status() != international.StatusDeleted {
		t.Fatalf("historic numeric SQL = %q, %v", numeric, err)
	}

	var strict country.Code
	if err := strict.UnmarshalText([]byte("AN")); !errors.Is(err, international.ErrInvalid) {
		t.Fatalf("strict historic decode error = %v, want ErrInvalid", err)
	}
}

func TestParseAlpha3AndNumericUseTheSameMapping(t *testing.T) {
	t.Parallel()

	fromAlpha3, err := country.ParseAlpha3("USA")
	if err != nil {
		t.Fatalf("ParseAlpha3() error = %v", err)
	}
	fromNumeric, err := country.ParseNumeric("840")
	if err != nil {
		t.Fatalf("ParseNumeric() error = %v", err)
	}
	alpha2FromAlpha3, alpha3OK := fromAlpha3.Alpha2()
	alpha2FromNumeric, numericOK := fromNumeric.Alpha2()
	if fromAlpha3.String() != "USA" || fromNumeric.String() != "840" ||
		!alpha3OK || !numericOK || alpha2FromAlpha3 != alpha2FromNumeric ||
		alpha2FromAlpha3.String() != "US" || fromAlpha3.Status() != international.StatusOfficial ||
		fromNumeric.Status() != international.StatusOfficial {
		t.Fatalf("parsed representations = %q and %q", fromAlpha3, fromNumeric)
	}

	for _, input := range []string{"usa", "ZZZ", "US", "\xffSA"} {
		if _, err := country.ParseAlpha3(input); !errors.Is(err, international.ErrInvalid) {
			t.Errorf("ParseAlpha3(%q) error = %v, want ErrInvalid", input, err)
		}
	}
	for _, input := range []string{"84", "0840", "8A0", "999"} {
		if _, err := country.ParseNumeric(input); !errors.Is(err, international.ErrInvalid) {
			t.Errorf("ParseNumeric(%q) error = %v, want ErrInvalid", input, err)
		}
	}
}

func TestReusedNumericCountryCodesPreserveTheirMappedIdentity(t *testing.T) {
	t.Parallel()

	current, err := country.ParseNumeric("104")
	if err != nil {
		t.Fatalf("ParseNumeric(104) error = %v", err)
	}
	currentAlpha2, ok := current.Alpha2()
	if !ok || currentAlpha2.String() != "MM" || current.Status() != international.StatusOfficial {
		t.Fatalf("current 104 = %q, %v, status %v; want MM, true, official",
			currentAlpha2, ok, current.Status())
	}

	historic, err := country.ParseWithOptions("BU", country.ParseOptions{AllowHistoric: true})
	if err != nil {
		t.Fatalf("ParseWithOptions(BU) error = %v", err)
	}
	historicNumeric, ok := historic.Numeric()
	if !ok {
		t.Fatal("BU Numeric() ok = false")
	}
	historicAlpha2, ok := historicNumeric.Alpha2()
	if !ok || historicAlpha2.String() != "BU" || historicNumeric.Status() != international.StatusDeleted {
		t.Fatalf("historic 104 = %q, %v, status %v; want BU, true, deleted",
			historicAlpha2, ok, historicNumeric.Status())
	}

	if _, err := country.ParseNumericWithOptions(
		"104",
		country.ParseOptions{AllowHistoric: true},
	); !errors.Is(err, international.ErrInvalid) {
		t.Fatalf("ambiguous historic ParseNumericWithOptions(104) error = %v, want ErrInvalid", err)
	}
}

func TestAllReturnsSortedIndependentOfficialCodes(t *testing.T) {
	t.Parallel()

	first := country.All()
	if len(first) != 249 {
		t.Fatalf("len(All()) = %d, want 249", len(first))
	}
	if first[0].String() != "AD" || first[len(first)-1].String() != "ZW" {
		t.Fatalf("All() bounds = %q..%q, want AD..ZW", first[0], first[len(first)-1])
	}
	first[0] = country.Code{}
	if got := country.All()[0].String(); got != "AD" {
		t.Fatalf("All() shares mutable backing storage, first = %q", got)
	}
}

func TestZeroCountryHasExplicitAbsentSemantics(t *testing.T) {
	t.Parallel()

	var code country.Code
	if !code.IsZero() || code.String() != "" {
		t.Fatalf("zero code = %q, IsZero %v", code, code.IsZero())
	}
	if _, ok := code.Alpha3(); ok {
		t.Fatal("zero Alpha3() ok = true")
	}
	if _, ok := code.Numeric(); ok {
		t.Fatal("zero Numeric() ok = true")
	}
	var numeric country.Numeric
	var alpha3 country.Alpha3
	if !numeric.IsZero() || !alpha3.IsZero() || numeric.String() != "" || numeric.Int() != 0 {
		t.Fatalf("zero numeric = %q (%d), want empty (0)", numeric, numeric.Int())
	}
	if _, ok := alpha3.Alpha2(); ok || alpha3.Status() != international.StatusUnknown {
		t.Fatal("zero alpha-3 has mapping or status")
	}
	if _, ok := numeric.Alpha2(); ok || numeric.Status() != international.StatusUnknown {
		t.Fatal("zero numeric has mapping or status")
	}
	if code.Status() != international.StatusUnknown {
		t.Fatalf("zero status = %v, want unknown", code.Status())
	}
	if got := country.Name(code, language.English); got != "" {
		t.Fatalf("zero Name() = %q, want empty", got)
	}
	finland, err := country.Parse("FI")
	if err != nil {
		t.Fatal(err)
	}
	if got := country.Name(finland, language.Tag{}); got != "" {
		t.Fatalf("Name() with unsupported display locale = %q, want empty", got)
	}
}

func TestCountryDatasetProvenanceIsValidAndPinned(t *testing.T) {
	t.Parallel()

	provenance := country.DatasetProvenance()
	if err := provenance.Validate(); err != nil {
		t.Fatalf("DatasetProvenance().Validate() error = %v", err)
	}
	if provenance.UpstreamVersion != "CLDR 48.2" {
		t.Fatalf("upstream version = %q, want CLDR 48.2", provenance.UpstreamVersion)
	}
}

func TestCountryDatasetRecordsAreCompleteSortedAndIndependent(t *testing.T) {
	t.Parallel()

	first := country.DatasetRecords()
	if len(first) != 301 || first[0].ID != "AA" || first[len(first)-1].ID != "ZZ" {
		t.Fatalf("dataset record bounds = %d, %q..%q", len(first), first[0].ID, first[len(first)-1].ID)
	}
	if first[0].Fingerprint == "" || first[0].Status == international.StatusUnknown {
		t.Fatalf("first dataset record lacks review metadata: %#v", first[0])
	}
	first[0].ID = "changed"
	if country.DatasetRecords()[0].ID != "AA" {
		t.Fatal("DatasetRecords returned shared mutable data")
	}
}

func TestUnknownCountryCannotBeEnabledByOtherPolicies(t *testing.T) {
	t.Parallel()
	if _, err := country.ParseWithOptions("ZZ", country.ParseOptions{
		AllowHistoric: true, AllowReserved: true, AllowUserAssigned: true,
	}); err == nil {
		t.Fatal("unknown country was accepted")
	}
}
