package localizedtest_test

import (
	"testing"

	localized "github.com/faustbrian/golib/pkg/localized"
	"github.com/faustbrian/golib/pkg/localized/localizedtest"
	localizedmatch "github.com/faustbrian/golib/pkg/localized/match"
)

func TestBuilderAndAssertions(t *testing.T) {
	value := localizedtest.New(t).Add("fi", "Hei").Add("en", "Hello").Build()
	want, _ := localized.TextFromMap(map[string]string{"en": "Hello", "fi": "Hei"})
	localizedtest.AssertEqual(t, value, want)
	localizedtest.AssertExact(t, value, mustLocale(t, "en"), "Hello")
	localizedtest.AssertResult(t, localizedmatch.Result{Kind: localizedmatch.Exact, Locale: mustLocale(t, "en"), Text: "Hello", Present: true}, localizedmatch.Exact, mustLocale(t, "en"), "Hello")
}

func TestCanonicalizationVectorsAreCallerOwned(t *testing.T) {
	vectors := localizedtest.CanonicalizationVectors()
	if len(vectors) == 0 {
		t.Fatal("no vectors")
	}
	original := vectors[0]
	vectors[0].Input = "changed"
	again := localizedtest.CanonicalizationVectors()
	if again[0] != original {
		t.Fatal("vectors share caller mutation")
	}
}
