package localized_test

import (
	"errors"
	"strings"
	"testing"

	language "github.com/faustbrian/golib/pkg/international/locale"
	localized "github.com/faustbrian/golib/pkg/localized"
)

func TestNilCallbacksAndBuildersArePanicSafe(t *testing.T) {
	t.Parallel()

	var builder *localized.Builder
	builder.Add(mustLocale(t, "en"), "ignored")
	if err := builder.AddString("en", "ignored"); !errors.Is(err, localized.ErrInvalidPolicy) {
		t.Fatalf("nil AddString() error = %v, want ErrInvalidPolicy", err)
	}

	value, err := localized.TextFromMap(map[string]string{"en": "Hello"})
	if err != nil {
		t.Fatal(err)
	}
	if filtered := value.Filter(nil); !filtered.Equal(value) {
		t.Fatalf("Filter(nil) = %v, want original", filtered.Entries())
	}
	if _, err := value.Map(nil); !errors.Is(err, localized.ErrInvalidPolicy) {
		t.Fatalf("Map(nil) error = %v, want ErrInvalidPolicy", err)
	}
}

func TestConstructionAndPersistentOperationBoundaries(t *testing.T) {
	t.Parallel()
	if _, err := localized.NewTextWithLimits(localized.Limits{MaxLocales: -1}); !errors.Is(err, localized.ErrLimitExceeded) {
		t.Fatalf("NewTextWithLimits(empty negative) error = %v", err)
	}
	if _, err := localized.NewText(localized.Entry{}); !errors.Is(err, localized.ErrInvalidLocale) {
		t.Fatalf("NewText(zero locale) error = %v", err)
	}

	entry := localized.Entry{Locale: mustLocale(t, "en"), Text: "four"}
	tests := []struct {
		name   string
		limits localized.Limits
		want   error
	}{
		{"negative", localized.Limits{MaxLocales: -1}, localized.ErrLimitExceeded},
		{"tag bytes", localized.Limits{MaxLocales: 1, MaxTagBytes: 1, MaxTextBytes: 8, MaxTotalBytes: 8}, localized.ErrLimitExceeded},
		{"total bytes", localized.Limits{MaxLocales: 1, MaxTagBytes: 8, MaxTextBytes: 8, MaxTotalBytes: 3}, localized.ErrLimitExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := localized.NewTextWithLimits(test.limits, entry)
			if !errors.Is(err, test.want) {
				t.Fatalf("NewTextWithLimits() error = %v, want %v", err, test.want)
			}
		})
	}

	if _, err := localized.TextFromMap(map[string]string{strings.Repeat("a", 256): "x"}); !errors.Is(err, localized.ErrLimitExceeded) {
		t.Fatalf("TextFromMap(long tag) error = %v", err)
	}
	if _, err := localized.TextFromMap(map[string]string{"en-": "x"}); !errors.Is(err, localized.ErrInvalidLocale) {
		t.Fatalf("TextFromMap(invalid tag) error = %v", err)
	}

	value, _ := localized.TextFromMap(map[string]string{"en": "Hello", "fi": "Hei"})
	if value.Has(language.Tag{}) {
		t.Fatal("Has(zero locale) = true")
	}
	if _, err := value.Set(language.Tag{}, "invalid"); !errors.Is(err, localized.ErrInvalidLocale) {
		t.Fatalf("Set(zero locale) error = %v", err)
	}
	count := 0
	for range value.All() {
		count++
		break
	}
	if count != 1 {
		t.Fatalf("early iterator count = %d, want 1", count)
	}
	if removed := value.Remove(mustLocale(t, "sv")); !removed.Equal(value) {
		t.Fatalf("Remove(absent) = %v", removed.Entries())
	}
	if value.Equal(localized.Text{}) {
		t.Fatal("different lengths compare equal")
	}
}

func TestConstructionAcceptsExactAndZeroResourceLimits(t *testing.T) {
	t.Parallel()

	exact, err := localized.NewTextWithLimits(
		localized.Limits{MaxLocales: 2, MaxTagBytes: 2, MaxTextBytes: 3, MaxTotalBytes: 6},
		localized.Entry{Locale: mustLocale(t, "en"), Text: "one"},
		localized.Entry{Locale: mustLocale(t, "fi"), Text: "two"},
	)
	if err != nil || exact.Len() != 2 {
		t.Fatalf("exact construction limits = %v, %v", exact.Entries(), err)
	}

	zeroLimits := []localized.Limits{
		{MaxLocales: 0, MaxTagBytes: 1, MaxTextBytes: 1, MaxTotalBytes: 1},
		{MaxLocales: 1, MaxTagBytes: 0, MaxTextBytes: 1, MaxTotalBytes: 1},
	}
	for _, limits := range zeroLimits {
		value, err := localized.NewTextWithLimits(limits)
		if err != nil || !value.IsEmpty() {
			t.Fatalf("zero resource limit %+v = %v, %v", limits, value.Entries(), err)
		}
	}
	value, err := localized.NewTextWithLimits(
		localized.Limits{MaxLocales: 1, MaxTagBytes: 2, MaxTextBytes: 0, MaxTotalBytes: 0},
		localized.Entry{Locale: mustLocale(t, "en"), Text: ""},
	)
	if err != nil || value.Len() != 1 {
		t.Fatalf("zero text limits = %v, %v", value.Entries(), err)
	}

	_, err = localized.NewTextWithLimits(
		localized.Limits{MaxLocales: 2, MaxTagBytes: 2, MaxTextBytes: 3, MaxTotalBytes: 5},
		localized.Entry{Locale: mustLocale(t, "en"), Text: "one"},
		localized.Entry{Locale: mustLocale(t, "fi"), Text: "two"},
	)
	if !errors.Is(err, localized.ErrLimitExceeded) {
		t.Fatalf("cumulative text limit error = %v, want ErrLimitExceeded", err)
	}
}

func TestStrictConstructorsAcceptMaximumLengthLocale(t *testing.T) {
	t.Parallel()

	raw := maximumLengthLocale()
	fromMap, err := localized.TextFromMap(map[string]string{raw: "map"})
	if err != nil || fromMap.Len() != 1 {
		t.Fatalf("TextFromMap(maximum locale) = %v, %v", fromMap.Entries(), err)
	}
	fromPairs, err := localized.TextFromPairs(localized.Pair{Locale: raw, Text: "pair"})
	if err != nil || fromPairs.Len() != 1 {
		t.Fatalf("TextFromPairs(maximum locale) = %v, %v", fromPairs.Entries(), err)
	}
}

func TestBuilderAndMergeFailureBoundaries(t *testing.T) {
	t.Parallel()

	builder := localized.NewBuilder(localized.ConstructionOptions{})
	if err := builder.AddString("en_US", "x"); !errors.Is(err, localized.ErrInvalidLocale) {
		t.Fatalf("AddString(underscore) error = %v", err)
	}
	if err := builder.AddString("en-", "x"); !errors.Is(err, localized.ErrInvalidLocale) {
		t.Fatalf("AddString(invalid) error = %v", err)
	}
	var nilBuilder *localized.Builder
	if value, err := nilBuilder.Build(); err != nil || !value.IsEmpty() {
		t.Fatalf("nil Build() = %v, %v", value.Entries(), err)
	}

	left, _ := localized.TextFromMap(map[string]string{"en": "left", "fi": ""})
	right, _ := localized.TextFromMap(map[string]string{"en": "right", "sv": ""})
	if _, err := left.MergeWithOptions(right, localized.MergeOptions{Conflicts: localized.MergePolicy(255)}); !errors.Is(err, localized.ErrInvalidPolicy) {
		t.Fatalf("Merge(invalid) error = %v", err)
	}
	merged, err := left.MergeWithOptions(right, localized.MergeOptions{Empty: localized.EmptyIsAbsent})
	if err != nil || merged.Has(mustLocale(t, "fi")) || merged.Has(mustLocale(t, "sv")) {
		t.Fatalf("Merge(empty absent) = %v, %v", merged.Entries(), err)
	}
	sentinel := errors.New("resolver failed")
	_, err = left.MergeWithOptions(right, localized.MergeOptions{
		Conflicts: localized.ResolveConflict,
		Resolver:  func(language.Tag, string, string) (string, error) { return "", sentinel },
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Merge(resolver) error = %v", err)
	}
	_, err = left.MergeWithOptions(right, localized.MergeOptions{
		Conflicts: localized.RightWins,
		Limits:    localized.Limits{MaxLocales: 1, MaxTagBytes: 8, MaxTextBytes: 16, MaxTotalBytes: 16},
	})
	if !errors.Is(err, localized.ErrLimitExceeded) {
		t.Fatalf("Merge(limit) error = %v", err)
	}
}

func TestMergeSkipsEmptyEntriesWithoutDiscardingLaterLocales(t *testing.T) {
	t.Parallel()

	left, err := localized.TextFromMap(map[string]string{"en": "", "fi": "left"})
	if err != nil {
		t.Fatal(err)
	}
	right, err := localized.TextFromMap(map[string]string{"de": "", "sv": "right"})
	if err != nil {
		t.Fatal(err)
	}
	merged, err := left.MergeWithOptions(right, localized.MergeOptions{Empty: localized.EmptyIsAbsent})
	if err != nil {
		t.Fatal(err)
	}
	if merged.Len() != 2 || !merged.Has(mustLocale(t, "fi")) || !merged.Has(mustLocale(t, "sv")) {
		t.Fatalf("Merge(empty absent) = %v, want later non-empty locales", merged.Entries())
	}
}

func maximumLengthLocale() string {
	parts := make([]string, 0, 28)
	for range 27 {
		parts = append(parts, "abcdefgh")
	}
	parts = append(parts, "abcdefg")
	return "en-x-" + strings.Join(parts, "-")
}
