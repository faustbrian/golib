package currency_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
	textcurrency "golang.org/x/text/currency"
)

func TestStableCurrentCurrencyVectorsDifferentialAgainstXText(t *testing.T) {
	t.Parallel()

	vectors := []struct {
		code  string
		minor uint8
	}{
		{code: "EUR", minor: 2},
		{code: "JPY", minor: 0},
		{code: "BHD", minor: 3},
	}
	for _, vector := range vectors {
		got, err := currency.Parse(vector.code)
		if err != nil {
			t.Fatal(err)
		}
		independent, err := textcurrency.ParseISO(vector.code)
		if err != nil || independent.String() != got.String() {
			t.Fatalf("currency differential %s = %s, %v", vector.code, independent, err)
		}
		minor, ok := got.MinorUnits()
		scale, _ := textcurrency.Standard.Rounding(independent)
		if !ok || minor != vector.minor || int(minor) != scale {
			t.Fatalf("minor-unit differential %s = %d / %d", vector.code, minor, scale)
		}
	}
}
