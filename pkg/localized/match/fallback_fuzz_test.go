package match_test

import (
	"testing"

	language "github.com/faustbrian/golib/pkg/international/locale"
	localizedmatch "github.com/faustbrian/golib/pkg/localized/match"
)

func FuzzFallbackPlan(f *testing.F) {
	f.Add("en", "fi")
	f.Add("EN-us", "en-US")
	f.Add("x-private", "und")
	f.Fuzz(func(t *testing.T, first, second string) {
		firstTag, firstErr := language.Parse(first)
		secondTag, secondErr := language.Parse(second)
		if firstErr != nil || secondErr != nil {
			return
		}
		plan, err := localizedmatch.NewFallbackPlan([]language.Tag{firstTag, secondTag}, nil, 2)
		firstCanonical, _ := firstTag.Canonical()
		secondCanonical, _ := secondTag.Canonical()
		if firstCanonical.String() == secondCanonical.String() {
			if err == nil {
				t.Fatal("canonical duplicate accepted")
			}
			return
		}
		if err != nil {
			t.Fatalf("NewFallbackPlan() error = %v", err)
		}
		_ = plan
	})
}
