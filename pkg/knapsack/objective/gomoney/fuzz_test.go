package gomoney_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/knapsack/objective/gomoney"
	"github.com/faustbrian/golib/pkg/money"
)

func FuzzCostTypeIdentifiers(f *testing.F) {
	f.Add("box")
	f.Add(" ")
	f.Add(strings.Repeat("x", 33))

	euro, err := currency.Parse("EUR")
	if err != nil {
		f.Fatalf("parse currency: %v", err)
	}
	moneyContext, err := money.DefaultContext(euro)
	if err != nil {
		f.Fatalf("create money context: %v", err)
	}
	cost, err := money.Parse("1.00", euro, moneyContext)
	if err != nil {
		f.Fatalf("parse cost: %v", err)
	}

	f.Fuzz(func(t *testing.T, typeID string) {
		if len(typeID) > 4_096 {
			t.Skip()
		}

		costs, newErr := gomoney.NewWithLimits(
			map[string]money.Money{typeID: cost},
			gomoney.Limits{MaxTypes: 1, MaxIDBytes: 32},
		)
		valid := strings.TrimSpace(typeID) != "" && len(typeID) <= 32
		if !valid {
			if !errors.Is(newErr, gomoney.ErrInvalidCosts) {
				t.Fatalf("NewWithLimits(%q) error = %v", typeID, newErr)
			}

			return
		}
		if newErr != nil {
			t.Fatalf("NewWithLimits(%q) error = %v", typeID, newErr)
		}
		if !costs.Valid() {
			t.Fatalf("NewWithLimits(%q) returned invalid costs", typeID)
		}
	})
}
