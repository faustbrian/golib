package country

import (
	"testing"

	international "github.com/faustbrian/golib/pkg/international"
)

func TestUnknownFutureStatusIsRejected(t *testing.T) {
	t.Parallel()
	if allowed(international.Status(255), ParseOptions{
		AllowHistoric: true, AllowReserved: true, AllowUserAssigned: true,
	}) {
		t.Fatal("unknown future status was accepted")
	}
}

func TestUnmappedInternalNumericHasNoAlpha2(t *testing.T) {
	t.Parallel()
	if _, ok := (Numeric{value: "000"}).Alpha2(); ok {
		t.Fatal("unmapped numeric returned alpha-2")
	}
}

func TestInternalIdentifierAndMappingBoundaries(t *testing.T) {
	for _, value := range []string{"AA", "AZ", "ZA", "ZZ"} {
		if !validAlpha(value, 2) {
			t.Errorf("validAlpha(%q, 2) = false", value)
		}
	}
	for _, value := range []string{"@A", "[A", "A@", "A[", "aa", "A"} {
		if validAlpha(value, 2) {
			t.Errorf("validAlpha(%q, 2) = true", value)
		}
	}

	keys := []string{"Q0", "Q1", "Q2"}
	previous := make(map[string]record, len(keys))
	present := make(map[string]bool, len(keys))
	for _, key := range keys {
		previous[key], present[key] = countryRecords[key]
	}
	t.Cleanup(func() {
		for _, key := range keys {
			if present[key] {
				countryRecords[key] = previous[key]
			} else {
				delete(countryRecords, key)
			}
		}
	})

	countryRecords["Q0"] = record{status: international.StatusOfficial}
	countryRecords["Q1"] = record{numeric: 0, status: international.StatusOfficial}
	countryRecords["Q2"] = record{numeric: 999, status: international.StatusOfficial}
	if _, ok := (Code{value: "Q0"}).Alpha3(); ok {
		t.Fatal("empty alpha-3 mapping was reported as present")
	}
	if _, ok := (Code{value: "Q1"}).Numeric(); ok {
		t.Fatal("zero numeric mapping was reported as present")
	}
	if numeric, ok := (Code{value: "Q2"}).Numeric(); !ok || numeric.String() != "999" {
		t.Fatalf("maximum numeric mapping = %q, %v", numeric, ok)
	}
}
