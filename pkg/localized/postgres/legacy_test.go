package postgres_test

import (
	"os"
	"path/filepath"
	"testing"

	language "github.com/faustbrian/golib/pkg/international/locale"
	localized "github.com/faustbrian/golib/pkg/localized"
)

func TestLegacyCompatibilityFixtures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		locale       language.Tag
		text         string
		presentEmpty bool
	}{
		{"spatie-translatable.json", mustLocale(t, "fi"), "", true},
		{"track.json", mustLocale(t, "sv-SE"), "Spåra försändelse", false},
		{"postal.json", mustLocale(t, "fi"), "Postitoimipaikka", false},
		{"location.json", mustLocale(t, "fi-FI"), "Noutopiste", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", test.name))
			if err != nil {
				t.Fatal(err)
			}
			value, err := localized.DecodeJSON(data, localized.DecodeOptions{})
			if err != nil {
				t.Fatalf("DecodeJSON() error = %v", err)
			}
			text, present := value.Get(test.locale)
			if !present || text != test.text {
				t.Fatalf("Get(%s) = %q, %v", test.locale, text, present)
			}
			if test.presentEmpty && (text != "" || !present) {
				t.Fatal("present-empty semantics lost")
			}
			encoded, err := localized.EncodeJSON(value)
			if err != nil {
				t.Fatal(err)
			}
			roundTrip, err := localized.DecodeJSON(encoded, localized.DecodeOptions{})
			if err != nil || !roundTrip.Equal(value) {
				t.Fatalf("round trip error = %v", err)
			}
		})
	}
}
