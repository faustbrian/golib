package money

import (
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/math/integer"
)

func TestMinorUnitConstructionRoundTripsWithoutNarrowing(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := DefaultContext(euro)
	minor := integer.New(-9007199254740993125)

	value, err := FromMinorUnits(minor, euro, monetaryContext)
	if err != nil {
		t.Fatalf("FromMinorUnits() error = %v", err)
	}
	if value.String() != "-90071992547409931.25 EUR" {
		t.Fatalf("FromMinorUnits() = %s", value)
	}
	roundTrip, err := value.MinorUnits()
	if err != nil || !roundTrip.Equal(minor) {
		t.Fatalf("MinorUnits() = %s, %v", roundTrip, err)
	}
}
