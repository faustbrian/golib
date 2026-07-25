package subdivision_test

import (
	"errors"
	"testing"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/subdivision"
)

func TestParseCurrentSubdivisionAndCountryContext(t *testing.T) {
	t.Parallel()

	code, err := subdivision.Parse("US-CA")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if code.String() != "US-CA" || code.Suffix() != "CA" || code.Name() != "California" {
		t.Fatalf("code = %q, suffix %q, name %q", code, code.Suffix(), code.Name())
	}
	if code.Country().String() != "US" || code.Status() != international.StatusOfficial {
		t.Fatalf("country/status = %q, %v", code.Country(), code.Status())
	}
}

func TestSubdivisionSyntaxAndMembershipAreStrict(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", "US", "US-", "USA-CA", "us-ca", "US-CALI", "US-C@", "US-ZZ", "\xffS-CA"} {
		if _, err := subdivision.Parse(input); !errors.Is(err, international.ErrInvalid) {
			t.Errorf("Parse(%q) error = %v, want ErrInvalid", input, err)
		}
	}
}

func TestDeletedSubdivisionRequiresOptIn(t *testing.T) {
	t.Parallel()

	if _, err := subdivision.Parse("FI-01"); !errors.Is(err, international.ErrInvalid) {
		t.Fatalf("Parse(FI-01) error = %v, want ErrInvalid", err)
	}
	code, err := subdivision.ParseWithOptions(
		"FI-01",
		subdivision.ParseOptions{AllowHistoric: true},
	)
	if err != nil {
		t.Fatalf("ParseWithOptions(FI-01) error = %v", err)
	}
	if code.Status() != international.StatusDeleted || code.Name() != "" {
		t.Fatalf("historic code = %q, status %v, name %q", code, code.Status(), code.Name())
	}
}

func TestDeletedSubdivisionPersistenceRequiresExplicitOptions(t *testing.T) {
	t.Parallel()

	options := subdivision.ParseOptions{AllowHistoric: true}
	var fromText subdivision.Code
	if err := fromText.UnmarshalTextWithOptions([]byte("FI-01"), options); err != nil ||
		fromText.Status() != international.StatusDeleted {
		t.Fatalf("historic text = %q, %v", fromText, err)
	}
	var fromJSON subdivision.Code
	if err := fromJSON.UnmarshalJSONWithOptions([]byte(`"FI-01"`), options); err != nil ||
		fromJSON != fromText {
		t.Fatalf("historic JSON = %q, %v", fromJSON, err)
	}
	var fromSQL subdivision.Code
	if err := fromSQL.ScanWithOptions([]byte("FI-01"), options); err != nil ||
		fromSQL != fromText {
		t.Fatalf("historic SQL = %q, %v", fromSQL, err)
	}
}

func TestAllReturnsCompleteIndependentSortedCurrentSet(t *testing.T) {
	t.Parallel()

	first := subdivision.All()
	if len(first) != 5027 {
		t.Fatalf("len(All()) = %d, want 5027", len(first))
	}
	if first[0].String() != "AD-02" || first[len(first)-1].String() != "ZW-MW" {
		t.Fatalf("All() bounds = %q..%q", first[0], first[len(first)-1])
	}
	first[0] = subdivision.Code{}
	if subdivision.All()[0].String() != "AD-02" {
		t.Fatal("All() returned shared mutable backing storage")
	}
}

func TestZeroSubdivisionHasAbsentSemantics(t *testing.T) {
	t.Parallel()

	var code subdivision.Code
	if !code.IsZero() || code.String() != "" || code.Suffix() != "" || code.Name() != "" ||
		!code.Country().IsZero() || code.Status() != international.StatusUnknown {
		t.Fatalf("zero subdivision is not absent: %#v", code)
	}
}

func TestSubdivisionDatasetProvenanceIsPinned(t *testing.T) {
	t.Parallel()

	provenance := subdivision.DatasetProvenance()
	if err := provenance.Validate(); err != nil {
		t.Fatalf("DatasetProvenance().Validate() error = %v", err)
	}
	if provenance.UpstreamVersion != "CLDR 48.2" {
		t.Fatalf("version = %q", provenance.UpstreamVersion)
	}
}

func TestSubdivisionDatasetRecordsAreCompleteSortedAndIndependent(t *testing.T) {
	t.Parallel()

	first := subdivision.DatasetRecords()
	if len(first) != 5653 || first[0].ID != "AD-02" || first[len(first)-1].ID != "ZW-MW" {
		t.Fatalf("dataset record bounds = %d, %q..%q", len(first), first[0].ID, first[len(first)-1].ID)
	}
	if first[0].Fingerprint == "" || first[0].Status == international.StatusUnknown {
		t.Fatalf("first dataset record lacks review metadata: %#v", first[0])
	}
	first[0].ID = "changed"
	if subdivision.DatasetRecords()[0].ID != "AD-02" {
		t.Fatal("DatasetRecords returned shared mutable data")
	}
}
