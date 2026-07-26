package localized_test

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	localized "github.com/faustbrian/golib/pkg/localized"
)

func TestTextFromMapValidatesAndOwnsInput(t *testing.T) {
	t.Parallel()

	input := map[string]string{"EN-us": "Hello", "fi": "Hei"}
	text, err := localized.TextFromMap(input)
	if err != nil {
		t.Fatalf("TextFromMap() error = %v", err)
	}
	input["EN-us"] = "changed"

	if got, _ := text.Get(mustLocale(t, "en-US")); got != "Hello" {
		t.Fatalf("Get(en-US) = %q, want Hello", got)
	}
	if _, err := localized.TextFromMap(map[string]string{"not_a_tag": "value"}); !errors.Is(err, localized.ErrInvalidLocale) {
		t.Fatalf("invalid locale error = %v, want ErrInvalidLocale", err)
	}
	if _, err := localized.TextFromMap(map[string]string{"en": string([]byte{0xff})}); !errors.Is(err, localized.ErrInvalidUTF8) {
		t.Fatalf("invalid UTF-8 error = %v, want ErrInvalidUTF8", err)
	}
}

func TestTextFromPairsCanonicalizesAndAppliesOptions(t *testing.T) {
	t.Parallel()

	pairs := []localized.Pair{{Locale: "EN-us", Text: "first"}, {Locale: "en-US", Text: "last"}}
	value, err := localized.TextFromPairsWithOptions(
		localized.ConstructionOptions{Duplicates: localized.LastWins}, pairs...,
	)
	if err != nil {
		t.Fatal(err)
	}
	pairs[1].Text = "mutated"
	if got, present := value.Get(mustLocale(t, "en-US")); !present || got != "last" {
		t.Fatalf("Get(en-US) = %q, %v", got, present)
	}
	if _, err := localized.TextFromPairs(
		localized.Pair{Locale: "EN-us", Text: "first"},
		localized.Pair{Locale: "en-US", Text: "last"},
	); !errors.Is(err, localized.ErrDuplicateLocale) {
		t.Fatalf("TextFromPairs(duplicate) error = %v", err)
	}
	if _, err := localized.TextFromPairs(localized.Pair{Locale: "en_", Text: "bad"}); !errors.Is(err, localized.ErrInvalidLocale) {
		t.Fatalf("TextFromPairs(invalid) error = %v", err)
	}
	if _, err := localized.TextFromPairs(localized.Pair{Locale: "en-", Text: "bad"}); !errors.Is(err, localized.ErrInvalidLocale) {
		t.Fatalf("TextFromPairs(malformed) error = %v", err)
	}
	if _, err := localized.TextFromPairs(localized.Pair{Locale: strings.Repeat("a", 256), Text: "bad"}); !errors.Is(err, localized.ErrLimitExceeded) {
		t.Fatalf("TextFromPairs(long) error = %v", err)
	}
}

func TestTextEnforcesConstructionLimits(t *testing.T) {
	t.Parallel()

	limits := localized.Limits{MaxLocales: 1, MaxTagBytes: 8, MaxTextBytes: 3, MaxTotalBytes: 3}
	if _, err := localized.NewTextWithLimits(limits,
		localized.Entry{Locale: mustLocale(t, "en"), Text: "one"},
		localized.Entry{Locale: mustLocale(t, "fi"), Text: "two"},
	); !errors.Is(err, localized.ErrLimitExceeded) {
		t.Fatalf("locale limit error = %v, want ErrLimitExceeded", err)
	}
	if _, err := localized.NewTextWithLimits(limits,
		localized.Entry{Locale: mustLocale(t, "en"), Text: "four"},
	); !errors.Is(err, localized.ErrLimitExceeded) {
		t.Fatalf("text limit error = %v, want ErrLimitExceeded", err)
	}
	if _, err := localized.NewText(localized.Entry{Locale: mustLocale(t, "en"), Text: string([]byte{utf8.RuneSelf, 0})}); !errors.Is(err, localized.ErrInvalidUTF8) {
		t.Fatalf("UTF-8 error = %v, want ErrInvalidUTF8", err)
	}
}

func TestTextEntriesAndRequiredLookup(t *testing.T) {
	t.Parallel()

	text, _ := localized.NewText(localized.Entry{Locale: mustLocale(t, "en"), Text: "Hello"})
	entries := text.Entries()
	entries[0].Text = "changed"

	if got, err := text.Require(mustLocale(t, "en")); err != nil || got != "Hello" {
		t.Fatalf("Require(en) = %q, %v; want Hello, nil", got, err)
	}
	if _, err := text.Require(mustLocale(t, "fi")); !errors.Is(err, localized.ErrMissingLocale) {
		t.Fatalf("Require(fi) error = %v, want ErrMissingLocale", err)
	}
}

func TestTextPersistentUpdatesDoNotMutateOriginal(t *testing.T) {
	t.Parallel()

	original, _ := localized.NewText(localized.Entry{Locale: mustLocale(t, "en"), Text: "Hello"})
	updated, err := original.Set(mustLocale(t, "en"), "Hi")
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	updated, err = updated.Set(mustLocale(t, "fi"), "Hei")
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	removed := updated.Remove(mustLocale(t, "en"))

	if got, _ := original.Get(mustLocale(t, "en")); got != "Hello" || original.Len() != 1 {
		t.Fatalf("original changed: %q, len %d", got, original.Len())
	}
	if got, _ := updated.Get(mustLocale(t, "en")); got != "Hi" || updated.Len() != 2 {
		t.Fatalf("updated = %q, len %d", got, updated.Len())
	}
	if removed.Has(mustLocale(t, "en")) || !removed.Has(mustLocale(t, "fi")) {
		t.Fatalf("removed locales = %v", removed.Locales())
	}
}

func TestTextEqualityAndHashAreOrderIndependent(t *testing.T) {
	t.Parallel()

	left, _ := localized.NewText(
		localized.Entry{Locale: mustLocale(t, "fi"), Text: "Hei"},
		localized.Entry{Locale: mustLocale(t, "en"), Text: "Hello"},
	)
	right, _ := localized.NewText(
		localized.Entry{Locale: mustLocale(t, "en"), Text: "Hello"},
		localized.Entry{Locale: mustLocale(t, "fi"), Text: "Hei"},
	)
	changed, _ := right.Set(mustLocale(t, "en"), "Hi")

	if !left.Equal(right) || left.Hash() != right.Hash() {
		t.Fatal("equivalent values are not equal with identical hashes")
	}
	if left.Equal(changed) || left.Hash() == changed.Hash() {
		t.Fatal("changed value compares equal or has the same hash")
	}
}

func TestTextMergePolicies(t *testing.T) {
	t.Parallel()

	left, _ := localized.NewText(localized.Entry{Locale: mustLocale(t, "en"), Text: "left"})
	right, _ := localized.NewText(
		localized.Entry{Locale: mustLocale(t, "en"), Text: "right"},
		localized.Entry{Locale: mustLocale(t, "fi"), Text: "oikea"},
	)

	leftWins, err := left.Merge(right, localized.LeftWins)
	if err != nil {
		t.Fatalf("Merge(LeftWins) error = %v", err)
	}
	rightWins, err := left.Merge(right, localized.RightWins)
	if err != nil {
		t.Fatalf("Merge(RightWins) error = %v", err)
	}
	if got, _ := leftWins.Get(mustLocale(t, "en")); got != "left" {
		t.Fatalf("left-wins value = %q", got)
	}
	if got, _ := rightWins.Get(mustLocale(t, "en")); got != "right" {
		t.Fatalf("right-wins value = %q", got)
	}
	if _, err := left.Merge(right, localized.RejectConflict); !errors.Is(err, localized.ErrConflict) {
		t.Fatalf("Merge(RejectConflict) error = %v, want ErrConflict", err)
	}
}
