package money

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
	gomath "github.com/faustbrian/golib/pkg/math"
)

func TestExclusiveAndInclusiveTaxPreserveTheirDocumentedTotals(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := DefaultContext(euro)
	rate, err := ParseTaxRate("0.24")
	if err != nil {
		t.Fatalf("ParseTaxRate() error = %v", err)
	}

	net, _ := Parse("0.05", euro, monetaryContext)
	exclusive, err := AddTax(context.Background(), net, rate, gomath.RoundHalfEven)
	if err != nil {
		t.Fatalf("AddTax() error = %v", err)
	}
	if exclusive.Net().String() != "0.05 EUR" || exclusive.Tax().String() != "0.01 EUR" || exclusive.Gross().String() != "0.06 EUR" {
		t.Fatalf("AddTax() = net %s, tax %s, gross %s", exclusive.Net(), exclusive.Tax(), exclusive.Gross())
	}

	inclusive, err := ExtractTax(context.Background(), exclusive.Gross(), rate, gomath.RoundHalfEven)
	if err != nil {
		t.Fatalf("ExtractTax() error = %v", err)
	}
	sum, _ := inclusive.Net().Add(inclusive.Tax())
	equal, compareErr := sum.Equal(inclusive.Gross())
	if compareErr != nil || !equal {
		t.Fatalf("inclusive components do not conserve gross: %s + %s != %s", inclusive.Net(), inclusive.Tax(), inclusive.Gross())
	}
}
