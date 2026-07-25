package locale_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/international/internationaltest"
	"github.com/faustbrian/golib/pkg/international/locale"
	textlanguage "golang.org/x/text/language"
)

func TestCanonicalizationDifferentialAgainstPinnedXText(t *testing.T) {
	t.Parallel()

	for _, vector := range internationaltest.LocaleVectors() {
		wrapped, err := locale.Parse(vector.Input)
		if err != nil {
			t.Fatal(err)
		}
		got, err := wrapped.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := textlanguage.Parse(vector.Input)
		if err != nil {
			t.Fatal(err)
		}
		independent, err := textlanguage.All.Canonicalize(parsed)
		if err != nil || got.String() != independent.String() || got.String() != vector.Canonical {
			t.Fatalf("locale differential %s = %s / %s, %v",
				vector.Input, got, independent, err)
		}
	}
}
