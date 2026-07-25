package localized_test

import (
	"bytes"
	"errors"
	"testing"

	language "github.com/faustbrian/golib/pkg/international/locale"
	localized "github.com/faustbrian/golib/pkg/localized"
)

func FuzzTextProperties(f *testing.F) {
	f.Add("EN-us", "Hello", "fi", "")
	f.Add("iw-IL", "shalom", "he-il", "preferred")
	f.Add("zh-Hant-TW", "您好", "x-tenant", "private")
	f.Add("en_", "invalid", "und", "unknown")

	f.Fuzz(func(t *testing.T, firstLocale, firstText, secondLocale, secondText string) {
		if len(firstLocale) > 255 || len(secondLocale) > 255 ||
			len(firstText) > 1024 || len(secondText) > 1024 {
			return
		}
		pairs := []localized.Pair{
			{Locale: firstLocale, Text: firstText},
			{Locale: secondLocale, Text: secondText},
		}
		value, err := localized.TextFromPairsWithOptions(
			localized.ConstructionOptions{Duplicates: localized.LastWins}, pairs...,
		)
		if err != nil {
			return
		}

		canonicalPairs := value.Pairs()
		for index, pair := range canonicalPairs {
			tag, parseErr := language.Parse(pair.Locale)
			if parseErr != nil {
				t.Fatalf("canonical locale %q no longer parses: %v", pair.Locale, parseErr)
			}
			canonical, canonicalErr := tag.Canonical()
			if canonicalErr != nil || canonical.String() != pair.Locale {
				t.Fatalf("canonicalization is not idempotent for %q", pair.Locale)
			}
			if index > 0 && canonicalPairs[index-1].Locale >= pair.Locale {
				t.Fatalf("pairs are not in strict canonical order: %v", canonicalPairs)
			}
		}

		encoded, encodeErr := localized.EncodeJSON(value)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		roundTrip, decodeErr := localized.DecodeJSON(encoded, localized.DecodeOptions{})
		if decodeErr != nil || !roundTrip.Equal(value) || roundTrip.Hash() != value.Hash() {
			t.Fatalf("canonical round trip changed value: %v", decodeErr)
		}
		reencoded, reencodeErr := localized.EncodeJSON(roundTrip)
		if reencodeErr != nil || !bytes.Equal(reencoded, encoded) {
			t.Fatalf("canonical encoding is not stable: %v", reencodeErr)
		}

		for _, policy := range []localized.MergePolicy{localized.LeftWins, localized.RightWins} {
			merged, mergeErr := value.Merge(value, policy)
			if mergeErr != nil || !merged.Equal(value) {
				t.Fatalf("merge policy %d is not idempotent: %v", policy, mergeErr)
			}
		}
		for _, operands := range [][2]localized.Text{
			{value, localized.Text{}},
			{localized.Text{}, value},
		} {
			merged, mergeErr := operands[0].Merge(operands[1], localized.LeftWins)
			if mergeErr != nil || !merged.Equal(value) {
				t.Fatalf("merge identity changed value: %v", mergeErr)
			}
		}

		reversed, reverseErr := localized.TextFromPairsWithOptions(
			localized.ConstructionOptions{Duplicates: localized.LastWins}, pairs[1], pairs[0],
		)
		if value.Len() == 2 && (reverseErr != nil || !reversed.Equal(value) || reversed.Hash() != value.Hash()) {
			t.Fatalf("input order changed a distinct-locale value: %v", reverseErr)
		}
		if value.Len() == 1 {
			_, strictErr := localized.TextFromPairs(pairs...)
			if !errors.Is(strictErr, localized.ErrDuplicateLocale) {
				t.Fatalf("canonical duplicate was not rejected: %v", strictErr)
			}
		}
	})
}
