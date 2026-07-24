package localizedquery_test

import (
	"errors"
	"testing"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
	"github.com/faustbrian/golib/pkg/international/locale"
	localized "github.com/faustbrian/golib/pkg/localized"
	"github.com/faustbrian/golib/pkg/localized/localizedquery"
)

func TestExactValuePreservesMissingAndPresentEmpty(t *testing.T) {
	t.Parallel()

	value, err := localized.TextFromMap(map[string]string{"en": "Hello", "fi": ""})
	if err != nil {
		t.Fatal(err)
	}
	english, _ := locale.Parse("en")
	finnish, _ := locale.Parse("fi")
	swedish, _ := locale.Parse("sv")

	if got, present := localizedquery.ExactValue(value, english); !present || got.Type() != apiquery.TypeString || got.String() != "Hello" {
		t.Fatalf("ExactValue(en) = %v, %v", got, present)
	}
	if got, present := localizedquery.ExactValue(value, finnish); !present || got.Type() != apiquery.TypeString || got.String() != "" {
		t.Fatalf("ExactValue(fi) = %v, %v", got, present)
	}
	if _, present := localizedquery.ExactValue(value, swedish); present {
		t.Fatal("ExactValue(sv) present = true")
	}
}

func TestExactPredicateNeverAppliesFallback(t *testing.T) {
	t.Parallel()

	value, _ := localized.TextFromMap(map[string]string{"en": "Hello"})
	english, _ := locale.Parse("en")
	finnish, _ := locale.Parse("fi")
	predicate, err := localizedquery.ExactPredicate("title", apiquery.OpEqual, value, english)
	if err != nil {
		t.Fatal(err)
	}
	if predicate.Name != "title" || predicate.Operator != apiquery.OpEqual || len(predicate.Values) != 1 || predicate.Values[0].String() != "Hello" {
		t.Fatalf("predicate = %+v", predicate)
	}
	if _, err := localizedquery.ExactPredicate("title", apiquery.OpEqual, value, finnish); !errors.Is(err, localized.ErrMissingLocale) {
		t.Fatalf("missing predicate error = %v", err)
	}
}
