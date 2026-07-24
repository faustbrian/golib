package currency_test

import (
	"fmt"

	"github.com/faustbrian/golib/pkg/international/currency"
)

func Example() {
	euro, _ := currency.Parse("EUR")
	numeric, _ := euro.Numeric()
	minorUnits, specified := euro.MinorUnits()
	fmt.Println(numeric, minorUnits, specified)
	// Output: 978 2 true
}
