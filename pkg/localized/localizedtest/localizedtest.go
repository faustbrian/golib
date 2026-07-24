// Package localizedtest provides consumer-facing builders, vectors, and assertions.
package localizedtest

import (
	"testing"

	"github.com/faustbrian/golib/pkg/international/locale"
	localized "github.com/faustbrian/golib/pkg/localized"
	localizedmatch "github.com/faustbrian/golib/pkg/localized/match"
)

// Builder fails its test immediately when fixture construction is invalid.
type Builder struct {
	t       testing.TB
	entries []localized.Entry
}

// New creates a localized fixture builder.
func New(t testing.TB) *Builder {
	t.Helper()
	return &Builder{t: t}
}

// Add parses and appends one strict BCP 47 fixture entry.
func (b *Builder) Add(rawLocale, text string) *Builder {
	b.t.Helper()
	tag, err := locale.Parse(rawLocale)
	if err != nil {
		b.t.Fatalf("localizedtest: invalid locale fixture")
	}
	b.entries = append(b.entries, localized.Entry{Locale: tag, Text: text})
	return b
}

// Build returns an immutable fixture or fails the test.
func (b *Builder) Build() localized.Text {
	b.t.Helper()
	value, err := localized.NewText(b.entries...)
	if err != nil {
		b.t.Fatalf("localizedtest: invalid text fixture: %v", err)
	}
	return value
}

// AssertEqual compares canonical locale and text identity.
func AssertEqual(t testing.TB, got, want localized.Text) {
	t.Helper()
	if !got.Equal(want) {
		t.Fatalf("localized values differ: got %v, want %v", got.Entries(), want.Entries())
	}
}

// AssertExact checks exact presence and text without matching or fallback.
func AssertExact(t testing.TB, value localized.Text, tag locale.Tag, want string) {
	t.Helper()
	got, ok := value.Get(tag)
	if !ok || got != want {
		t.Fatalf("localized exact value = %q, %v; want %q, true", got, ok, want)
	}
}

// AssertResult checks the stable public resolution fields.
func AssertResult(t testing.TB, result localizedmatch.Result, kind localizedmatch.Kind, tag locale.Tag, text string) {
	t.Helper()
	if result.Kind != kind || result.Locale != tag || result.Text != text || !result.Present {
		t.Fatalf("localized result = %+v; want kind %d, locale %s, text %q", result, kind, tag, text)
	}
}

// CanonicalizationVector is a standards-oriented parser fixture.
type CanonicalizationVector struct {
	Input     string
	Canonical string
	Source    string
}

// CanonicalizationVectors returns caller-owned BCP 47 canonicalization cases.
func CanonicalizationVectors() []CanonicalizationVector {
	return []CanonicalizationVector{
		{Input: "EN-us", Canonical: "en-US", Source: "BCP 47 casing"},
		{Input: "zh-hant-tw", Canonical: "zh-Hant-TW", Source: "BCP 47 script and region"},
		{Input: "iw-IL", Canonical: "he-IL", Source: "IANA preferred value"},
		{Input: "i-klingon", Canonical: "tlh", Source: "IANA grandfathered tag"},
		{Input: "x-acme", Canonical: "x-acme", Source: "BCP 47 private use"},
		{Input: "und", Canonical: "und", Source: "BCP 47 undetermined"},
		{Input: "mul", Canonical: "mul", Source: "ISO 639 multiple languages"},
	}
}
