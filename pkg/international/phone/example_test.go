package phone_test

import (
	"fmt"

	"github.com/faustbrian/golib/pkg/international/country"
	"github.com/faustbrian/golib/pkg/international/phone"
)

func Example() {
	finland, _ := country.Parse("FI")
	number, _ := phone.Parse("040 123 4567", phone.ParseOptions{RegionHint: finland})
	fmt.Println(number.E164(), number.Possible(), number.Valid())
	// Output: +358401234567 true true
}
