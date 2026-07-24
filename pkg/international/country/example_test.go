package country_test

import (
	"fmt"

	"github.com/faustbrian/golib/pkg/international/country"
)

func Example() {
	finland, _ := country.Parse("FI")
	alpha3, _ := finland.Alpha3()
	fmt.Println(finland, alpha3)
	// Output: FI FIN
}
