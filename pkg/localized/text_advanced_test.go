package localized_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	language "github.com/faustbrian/golib/pkg/international/locale"
	localized "github.com/faustbrian/golib/pkg/localized"
)

func TestDuplicatePoliciesApplyAfterCanonicalization(t *testing.T) {
	t.Parallel()
	entries := []localized.Entry{
		{Locale: mustLocale(t, "EN-us"), Text: "first"},
		{Locale: mustLocale(t, "en-US"), Text: "last"},
	}
	first, err := localized.NewTextWithOptions(localized.ConstructionOptions{Duplicates: localized.FirstWins}, entries...)
	if err != nil {
		t.Fatal(err)
	}
	last, err := localized.NewTextWithOptions(localized.ConstructionOptions{Duplicates: localized.LastWins}, entries...)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := first.Get(mustLocale(t, "en-US")); got != "first" {
		t.Fatalf("first = %q", got)
	}
	if got, _ := last.Get(mustLocale(t, "en-US")); got != "last" {
		t.Fatalf("last = %q", got)
	}
	if _, err := localized.NewTextWithOptions(localized.ConstructionOptions{Duplicates: localized.DuplicatePolicy(99)}, entries...); !errors.Is(err, localized.ErrInvalidPolicy) {
		t.Fatalf("invalid policy error = %v", err)
	}
}

func TestLocaleAcceptancePolicyIsExplicit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		locale language.Tag
		policy localized.LocalePolicy
	}{
		{"und", mustLocale(t, "und"), localized.LocalePolicy{RejectUnd: true}},
		{"mul", mustLocale(t, "mul"), localized.LocalePolicy{RejectMul: true}},
		{"private", mustLocale(t, "en-x-tenant"), localized.LocalePolicy{RejectPrivateUse: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := localized.NewTextWithOptions(localized.ConstructionOptions{Locales: test.policy}, localized.Entry{Locale: test.locale, Text: "value"})
			if !errors.Is(err, localized.ErrLocaleRejected) {
				t.Fatalf("error = %v, want ErrLocaleRejected for %s", err, test.locale)
			}
		})
	}
	accepted, err := localized.NewText(
		localized.Entry{Locale: mustLocale(t, "und"), Text: "unknown"},
		localized.Entry{Locale: mustLocale(t, "mul"), Text: "multiple"},
		localized.Entry{Locale: mustLocale(t, "x-tenant"), Text: "private"},
	)
	if err != nil || accepted.Len() != 3 {
		t.Fatalf("default acceptance = %v, %v", accepted.Locales(), err)
	}
}

func TestBuilderPairsAndDeterministicIterationOwnData(t *testing.T) {
	t.Parallel()
	builder := localized.NewBuilder(localized.ConstructionOptions{})
	if err := builder.AddString("fi", "Hei"); err != nil {
		t.Fatal(err)
	}
	builder.Add(mustLocale(t, "en"), "Hello")
	value, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	builder.Add(mustLocale(t, "sv"), "Hej")

	pairs := value.Pairs()
	if want := []localized.Pair{{Locale: "en", Text: "Hello"}, {Locale: "fi", Text: "Hei"}}; !reflect.DeepEqual(pairs, want) {
		t.Fatalf("Pairs() = %+v", pairs)
	}
	seen := make([]string, 0, value.Len())
	for locale, text := range value.All() {
		seen = append(seen, locale.String()+"="+text)
	}
	if want := []string{"en=Hello", "fi=Hei"}; !reflect.DeepEqual(seen, want) {
		t.Fatalf("All() = %v", seen)
	}
}

func TestFilterAndMapReturnIndependentValues(t *testing.T) {
	t.Parallel()
	value, _ := localized.TextFromMap(map[string]string{"en": "hello", "fi": "hei"})
	filtered := value.Filter(func(locale language.Tag, _ string) bool { return locale != mustLocale(t, "fi") })
	mapped, err := value.Map(func(_ language.Tag, text string) (string, error) { return strings.ToUpper(text), nil })
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Len() != 1 || filtered.Has(mustLocale(t, "fi")) {
		t.Fatalf("Filter() = %v", filtered.Entries())
	}
	if got, _ := mapped.Get(mustLocale(t, "en")); got != "HELLO" {
		t.Fatalf("Map() en = %q", got)
	}
	if got, _ := value.Get(mustLocale(t, "en")); got != "hello" {
		t.Fatalf("original en = %q", got)
	}

	sentinel := errors.New("stop")
	if _, err := value.Map(func(language.Tag, string) (string, error) { return "", sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("Map() error = %v", err)
	}
}

func TestMergeResolverAndPresentEmptyPolicies(t *testing.T) {
	t.Parallel()
	left, _ := localized.TextFromMap(map[string]string{"en": "", "fi": "vasen"})
	right, _ := localized.TextFromMap(map[string]string{"en": "right", "fi": "oikea"})

	resolved, err := left.MergeWithOptions(right, localized.MergeOptions{
		Conflicts: localized.ResolveConflict,
		Empty:     localized.EmptyIsValue,
		Resolver:  func(_ language.Tag, left, right string) (string, error) { return left + "/" + right, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := resolved.Get(mustLocale(t, "fi")); got != "vasen/oikea" {
		t.Fatalf("resolved fi = %q", got)
	}
	if got, _ := resolved.Get(mustLocale(t, "en")); got != "/right" {
		t.Fatalf("resolved en = %q", got)
	}

	emptyAbsent, err := left.MergeWithOptions(right, localized.MergeOptions{Conflicts: localized.LeftWins, Empty: localized.EmptyIsAbsent})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := emptyAbsent.Get(mustLocale(t, "en")); got != "right" {
		t.Fatalf("empty-absent en = %q", got)
	}
	if _, err := left.MergeWithOptions(right, localized.MergeOptions{Conflicts: localized.ResolveConflict}); !errors.Is(err, localized.ErrResolverRequired) {
		t.Fatalf("missing resolver error = %v", err)
	}
}
