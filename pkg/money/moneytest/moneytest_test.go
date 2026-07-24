package moneytest_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/math/integer"
	"github.com/faustbrian/golib/pkg/money"
	"github.com/faustbrian/golib/pkg/money/moneytest"
)

func TestFixturesAndConservationAssertionsCoverCurrencyEdges(t *testing.T) {
	t.Parallel()

	fixtures := moneytest.CurrencyFixtures()
	if len(fixtures) < 4 {
		t.Fatalf("CurrencyFixtures() has %d entries", len(fixtures))
	}
	seen := map[string]bool{}
	for _, fixture := range fixtures {
		seen[fixture.Code.String()] = true
	}
	for _, code := range []string{"EUR", "JPY", "BHD", "FIM"} {
		if !seen[code] {
			t.Errorf("missing fixture %s", code)
		}
	}

	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := money.DefaultContext(euro)
	total := moneytest.MustParse(t, "10.00", euro, monetaryContext)
	allocation, err := total.Allocate(context.Background(), []integer.Integer{integer.New(1), integer.New(2)})
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}
	moneytest.AssertAllocationConserved(t, total, allocation)
	moneytest.AssertEqual(t, total, total)
}
