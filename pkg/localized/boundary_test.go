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
