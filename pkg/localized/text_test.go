package localized_test

import (
	"errors"
	"reflect"
	"testing"

	language "github.com/faustbrian/golib/pkg/international/locale"
	localized "github.com/faustbrian/golib/pkg/localized"
)

func TestTextConstructionCanonicalizesAndOrdersLocales(t *testing.T) {
	t.Parallel()

	text, err := localized.NewText(
		localized.Entry{Locale: mustLocale(t, "fi-fi"), Text: "Hei"},
		localized.Entry{Locale: mustLocale(t, "EN-us"), Text: "Hello"},
	)
	if err != nil {
		t.Fatalf("NewText() error = %v", err)
	}

	if got, want := text.Locales(), []language.Tag{mustLocale(t, "en-US"), mustLocale(t, "fi-FI")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Locales() = %v, want %v", got, want)
	}
	if got, ok := text.Get(mustLocale(t, "en-US")); !ok || got != "Hello" {
		t.Fatalf("Get(en-US) = %q, %v; want Hello, true", got, ok)
	}
}

func TestTextDistinguishesMissingFromPresentEmpty(t *testing.T) {
	t.Parallel()

	text, err := localized.NewText(localized.Entry{Locale: mustLocale(t, "en"), Text: ""})
	if err != nil {
		t.Fatalf("NewText() error = %v", err)
	}

	if got, ok := text.Get(mustLocale(t, "en")); !ok || got != "" {
		t.Fatalf("Get(en) = %q, %v; want empty, true", got, ok)
	}
	if got, ok := text.Get(mustLocale(t, "fi")); ok || got != "" {
		t.Fatalf("Get(fi) = %q, %v; want empty, false", got, ok)
	}
}

func TestTextRejectsCanonicalDuplicate(t *testing.T) {
	t.Parallel()

	_, err := localized.NewText(
		localized.Entry{Locale: mustLocale(t, "EN-us"), Text: "first"},
		localized.Entry{Locale: mustLocale(t, "en-US"), Text: "second"},
	)
	if !errors.Is(err, localized.ErrDuplicateLocale) {
		t.Fatalf("NewText() error = %v, want ErrDuplicateLocale", err)
	}
}

func TestTextCopiesInputAndOutputSlices(t *testing.T) {
	t.Parallel()

	entries := []localized.Entry{{Locale: mustLocale(t, "en"), Text: "Hello"}}
	text, err := localized.NewText(entries...)
	if err != nil {
		t.Fatalf("NewText() error = %v", err)
	}
	entries[0] = localized.Entry{Locale: mustLocale(t, "fi"), Text: "Hei"}

	locales := text.Locales()
	locales[0] = mustLocale(t, "fi")

	if got, ok := text.Get(mustLocale(t, "en")); !ok || got != "Hello" {
		t.Fatalf("Get(en) = %q, %v; want Hello, true", got, ok)
	}
	if text.Has(mustLocale(t, "fi")) {
		t.Fatal("Has(fi) = true, want false")
	}
}

func TestTextZeroValueIsEmpty(t *testing.T) {
	t.Parallel()

	var text localized.Text
	if !text.IsEmpty() || text.Len() != 0 || len(text.Locales()) != 0 {
		t.Fatalf("zero Text = empty %v, len %d, locales %v", text.IsEmpty(), text.Len(), text.Locales())
	}
}
