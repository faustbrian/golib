// Package locale provides bounded BCP 47 tags with explicit canonicalization
// and fallback policies.
package locale

import (
	"strings"
	"unicode/utf8"

	international "github.com/faustbrian/golib/pkg/international"
	intlLanguage "github.com/faustbrian/golib/pkg/international/language"
	textlanguage "golang.org/x/text/language"
)

const (
	// MaxBytes is the accepted BCP 47 input byte bound recommended by CLDR.
	MaxBytes = 255
	// MaxSegments prevents extension-heavy tags from causing excessive work.
	MaxSegments = 32
)

// FallbackPolicy selects an explicit lossy fallback operation.
type FallbackPolicy uint8

const (
	// FallbackNone disables fallback.
	FallbackNone FallbackPolicy = iota
	// FallbackParent removes extensions first, then follows CLDR parent data.
	FallbackParent
	// FallbackLanguage removes all parts except the base language.
	FallbackLanguage
)

// Tag is an immutable, validated BCP 47 tag. String preserves the valid input
// spelling; Canonical performs standards-backed rewriting explicitly.
type Tag struct {
	source string
	parsed textlanguage.Tag
}

// Parse validates bounded BCP 47 input without rewriting its spelling.
func Parse(input string) (Tag, error) {
	if len(input) > MaxBytes || strings.Count(input, "-")+1 > MaxSegments {
		return Tag{}, international.ErrResourceLimit
	}
	if input == "" || !utf8.ValidString(input) || strings.Contains(input, "_") {
		return Tag{}, international.NewParseError("locale", "malformed BCP 47 tag")
	}
	parsed, err := textlanguage.Raw.Parse(input)
	if err != nil {
		return Tag{}, international.NewParseError("locale", "unknown or malformed BCP 47 tag")
	}
	return Tag{source: input, parsed: parsed}, nil
}

// String returns the original valid spelling or empty string for the zero tag.
func (tag Tag) String() string { return tag.source }

// IsZero reports whether the tag represents an absent value.
func (tag Tag) IsZero() bool { return tag.source == "" }

// Canonical applies all BCP 47 recommended canonicalizations explicitly.
func (tag Tag) Canonical() (Tag, error) {
	if tag.IsZero() {
		return Tag{}, international.NewParseError("locale", "absent tag")
	}
	canonical, _ := textlanguage.BCP47.Canonicalize(tag.parsed)
	return Tag{source: canonical.String(), parsed: canonical}, nil
}

// Language returns the tag's explicit base language.
func (tag Tag) Language() intlLanguage.Code {
	if tag.IsZero() {
		return intlLanguage.Code{}
	}
	base, _, _ := tag.parsed.Raw()
	code, _ := intlLanguage.ParseISO3(base.ISO3())
	return code
}

// Script returns the explicit ISO 15924 script subtag, if present.
func (tag Tag) Script() string {
	if tag.IsZero() {
		return ""
	}
	_, script, _ := tag.parsed.Raw()
	if script.String() == "Zzzz" {
		return ""
	}
	return script.String()
}

// Region returns the explicit region subtag, if present.
func (tag Tag) Region() string {
	if tag.IsZero() {
		return ""
	}
	_, _, region := tag.parsed.Raw()
	if region.String() == "ZZ" {
		return ""
	}
	return region.String()
}

// Variants returns an independent slice of registered variant subtags.
func (tag Tag) Variants() []string {
	if tag.IsZero() {
		return nil
	}
	variants := tag.parsed.Variants()
	result := make([]string, len(variants))
	for index, variant := range variants {
		result[index] = variant.String()
	}
	return result
}

// Extensions returns an independent slice including private-use extensions.
func (tag Tag) Extensions() []string {
	if tag.IsZero() {
		return nil
	}
	extensions := tag.parsed.Extensions()
	result := make([]string, len(extensions))
	for index, extension := range extensions {
		result[index] = extension.String()
	}
	return result
}

// HasPrivateUse reports whether the tag contains an x extension.
func (tag Tag) HasPrivateUse() bool {
	if tag.IsZero() {
		return false
	}
	_, ok := tag.parsed.Extension('x')
	return ok
}

// Fallback applies only the selected lossy policy.
func (tag Tag) Fallback(policy FallbackPolicy) (Tag, bool) {
	if tag.IsZero() {
		return Tag{}, false
	}
	switch policy {
	case FallbackNone:
		return Tag{}, false
	case FallbackParent:
		parent := parentTag(tag.parsed)
		if parent.String() == tag.parsed.String() || parent.IsRoot() {
			return Tag{}, false
		}
		return Tag{source: parent.String(), parsed: parent}, true
	case FallbackLanguage:
		base, _, _ := tag.parsed.Raw()
		if base.String() == "und" {
			return Tag{}, false
		}
		parsed := textlanguage.Raw.Make(base.String())
		return Tag{source: parsed.String(), parsed: parsed}, true
	default:
		return Tag{}, false
	}
}

func parentTag(tag textlanguage.Tag) textlanguage.Tag {
	if len(tag.Extensions()) > 0 {
		base, script, region := tag.Raw()
		parts := []any{base}
		if script.String() != "Zzzz" {
			parts = append(parts, script)
		}
		if region.String() != "ZZ" {
			parts = append(parts, region)
		}
		if variants := tag.Variants(); len(variants) > 0 {
			parts = append(parts, variants)
		}
		parent, _ := textlanguage.Raw.Compose(parts...)
		return parent
	}
	return tag.Parent()
}
