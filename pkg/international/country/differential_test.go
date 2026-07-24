package country_test

import (
	"fmt"
	"testing"

	"github.com/faustbrian/golib/pkg/international/country"
	textlanguage "golang.org/x/text/language"
)

func TestOfficialCountryMappingsDifferentialAgainstXText(t *testing.T) {
	t.Parallel()

	for _, code := range country.All() {
		independent, err := textlanguage.ParseRegion(code.String())
		if err != nil || !independent.IsCountry() {
			t.Fatalf("x/text region %s = %v, %v", code, independent, err)
		}
		alpha3, alpha3OK := code.Alpha3()
		numeric, numericOK := code.Numeric()
		if !alpha3OK || !numericOK || alpha3.String() != independent.ISO3() ||
			numeric.String() != fmt.Sprintf("%03d", independent.M49()) {
			t.Fatalf("country differential mismatch for %s", code)
		}
	}
}
