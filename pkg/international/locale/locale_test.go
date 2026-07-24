package locale_test

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/locale"
)

func TestParsePreservesValidSpellingAndCanonicalizationIsExplicit(t *testing.T) {
	t.Parallel()

	const input = "EN-latn-us-u-ca-gregory-x-test"
	tag, err := locale.Parse(input)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if tag.String() != input {
		t.Fatalf("String() = %q, want original %q", tag, input)
	}
	canonical, err := tag.Canonical()
	if err != nil {
		t.Fatalf("Canonical() error = %v", err)
	}
	if canonical.String() != "en-US-u-ca-gregory-x-test" {
		t.Fatalf("Canonical() = %q", canonical)
	}
	if canonical == tag {
		t.Fatal("canonicalization silently reused the original representation")
	}
}

func TestLocalePartsRemainAvailableWithoutLossyFallback(t *testing.T) {
	t.Parallel()

	tag, err := locale.Parse("sl-Latn-IT-rozaj-biske-u-ca-gregory-x-private")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if tag.Language().String() != "sl" || tag.Script() != "Latn" || tag.Region() != "IT" {
		t.Fatalf("parts = %q, %q, %q", tag.Language(), tag.Script(), tag.Region())
	}
	wantVariants := []string{"rozaj", "biske"}
	if got := tag.Variants(); !equalStrings(got, wantVariants) {
		t.Fatalf("Variants() = %v, want %v", got, wantVariants)
	}
	wantExtensions := []string{"u-ca-gregory", "x-private"}
	if got := tag.Extensions(); !equalStrings(got, wantExtensions) {
		t.Fatalf("Extensions() = %v, want %v", got, wantExtensions)
	}
	if !tag.HasPrivateUse() {
		t.Fatal("HasPrivateUse() = false")
	}
}

func TestFallbackRequiresAnExplicitPolicy(t *testing.T) {
	t.Parallel()

	tag, err := locale.Parse("en-Latn-US-u-ca-gregory")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if _, ok := tag.Fallback(locale.FallbackNone); ok {
		t.Fatal("FallbackNone returned a value")
	}
	parent, ok := tag.Fallback(locale.FallbackParent)
	if !ok || parent.String() != "en-Latn-US" {
		t.Fatalf("parent = %q, %v, want en-Latn-US, true", parent, ok)
	}
	languageOnly, ok := tag.Fallback(locale.FallbackLanguage)
	if !ok || languageOnly.String() != "en" {
		t.Fatalf("language fallback = %q, %v, want en, true", languageOnly, ok)
	}
	withoutExtensions, err := locale.Parse("de-u-ca-gregory")
	if err != nil {
		t.Fatalf("Parse(de extension) error = %v", err)
	}
	parent, ok = withoutExtensions.Fallback(locale.FallbackParent)
	if !ok || parent.String() != "de" {
		t.Fatalf("extension parent = %q, %v, want de, true", parent, ok)
	}
	withoutRegion, err := locale.Parse("sr-Latn-u-ca-gregory")
	if err != nil {
		t.Fatalf("Parse(sr extension) error = %v", err)
	}
	parent, ok = withoutRegion.Fallback(locale.FallbackParent)
	if !ok || parent.String() != "sr-Latn" {
		t.Fatalf("script parent = %q, %v, want sr-Latn, true", parent, ok)
	}
	regionOnly, err := locale.Parse("en-US")
	if err != nil {
		t.Fatalf("Parse(en-US) error = %v", err)
	}
	parent, ok = regionOnly.Fallback(locale.FallbackParent)
	if !ok || parent.String() != "en" {
		t.Fatalf("region parent = %q, %v, want en, true", parent, ok)
	}
	baseOnly, err := locale.Parse("en")
	if err != nil {
		t.Fatalf("Parse(en) error = %v", err)
	}
	if _, ok := baseOnly.Fallback(locale.FallbackParent); ok {
		t.Fatal("base language returned a parent")
	}
	undetermined, err := locale.Parse("und-US")
	if err != nil {
		t.Fatalf("Parse(und-US) error = %v", err)
	}
	if _, ok := undetermined.Fallback(locale.FallbackLanguage); ok {
		t.Fatal("undetermined language returned a language fallback")
	}
	if _, ok := tag.Fallback(locale.FallbackPolicy(255)); ok {
		t.Fatal("unknown policy returned a fallback")
	}
	withVariant, err := locale.Parse("sl-Latn-IT-rozaj-u-ca-gregory")
	if err != nil {
		t.Fatalf("Parse(variant extension) error = %v", err)
	}
	parent, ok = withVariant.Fallback(locale.FallbackParent)
	if !ok || parent.String() != "sl-Latn-IT-rozaj" {
		t.Fatalf("variant parent = %q, %v, want sl-Latn-IT-rozaj, true", parent, ok)
	}
}

func TestAbsentLocalePartsAreEmpty(t *testing.T) {
	t.Parallel()

	tag, err := locale.Parse("en")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if tag.Script() != "" || tag.Region() != "" {
		t.Fatalf("parts = script %q, region %q, want empty", tag.Script(), tag.Region())
	}
}

func TestLocaleParsingRejectsMalformedAndUnboundedInput(t *testing.T) {
	t.Parallel()

	tooManySegments := strings.Repeat("a-", locale.MaxSegments) + "a"
	tooLong := strings.Repeat("a", locale.MaxBytes+1)
	inputs := []string{"", "en_US", "en--US", "en-unknown", "\xffn-US", tooManySegments, tooLong}
	for _, input := range inputs {
		_, err := locale.Parse(input)
		if !errors.Is(err, international.ErrInvalid) && !errors.Is(err, international.ErrResourceLimit) {
			t.Errorf("Parse(%q) error = %v, want invalid or resource limit", bounded(input), err)
		}
	}
}

func TestZeroLocaleHasAbsentSemantics(t *testing.T) {
	t.Parallel()

	var tag locale.Tag
	if !tag.IsZero() || tag.String() != "" || tag.Script() != "" || tag.Region() != "" {
		t.Fatalf("zero tag = %q, IsZero %v", tag, tag.IsZero())
	}
	if !tag.Language().IsZero() || tag.Variants() != nil || tag.Extensions() != nil || tag.HasPrivateUse() {
		t.Fatal("zero parts are not absent")
	}
	if _, err := tag.Canonical(); !errors.Is(err, international.ErrInvalid) {
		t.Fatalf("Canonical(zero) error = %v, want ErrInvalid", err)
	}
	if _, ok := tag.Fallback(locale.FallbackLanguage); ok {
		t.Fatal("Fallback(zero) returned a value")
	}
}

func TestLocaleOutputAlwaysRemainsValidUTF8(t *testing.T) {
	t.Parallel()

	tag, err := locale.Parse("de-CH-1901")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !utf8.ValidString(tag.String()) {
		t.Fatalf("String() = invalid UTF-8: %q", tag)
	}
}

func TestLocaleDatasetProvenanceMatchesLanguageRegistry(t *testing.T) {
	t.Parallel()

	provenance := locale.DatasetProvenance()
	if err := provenance.Validate(); err != nil {
		t.Fatalf("DatasetProvenance().Validate() error = %v", err)
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func bounded(value string) string {
	if len(value) > 80 {
		return value[:80]
	}
	return value
}
