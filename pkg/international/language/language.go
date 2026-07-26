// Package language provides strict ISO 639 identifiers backed by the IANA
// Language Subtag Registry through a pinned golang.org/x/text release.
package language

import (
	"unicode/utf8"

	international "github.com/faustbrian/golib/pkg/international"
	textlanguage "golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// Code is an immutable canonical ISO 639 language identifier. Its zero value
// is absent. Two-letter identifiers are preferred where an ISO 639-1 mapping
// exists.
type Code struct{ value string }

// Parse accepts a canonical lowercase ISO identifier. It requires ISO 639-1
// when a two-letter representation exists and otherwise retains ISO 639-3.
func Parse(input string) (Code, error) {
	if !validLowerAlpha(input, 2) && !validLowerAlpha(input, 3) {
		return Code{}, international.NewParseError("language code", "invalid ISO 639 identifier")
	}
	base, err := textlanguage.ParseBase(input)
	canonical, canonicalErr := textlanguage.DeprecatedBase.Canonicalize(textlanguage.Raw.Make(input))
	if err != nil || canonicalErr != nil || base.String() != input || canonical.String() != input {
		return Code{}, international.NewParseError("language code", "unknown or obsolete identifier")
	}
	return Code{value: input}, nil
}

// ParseISO3 converts a canonical lowercase ISO 639 three-letter identifier.
func ParseISO3(input string) (Code, error) {
	if !validLowerAlpha(input, 3) {
		return Code{}, international.NewParseError("language code", "invalid ISO 639 three-letter identifier")
	}
	base, err := textlanguage.ParseBase(input)
	if err != nil || base.ISO3() != input {
		return Code{}, international.NewParseError("language code", "unknown or obsolete identifier")
	}
	return Code{value: base.String()}, nil
}

// String returns the canonical identifier or empty string for the zero value.
func (code Code) String() string { return code.value }

// IsZero reports whether the code represents an absent value.
func (code Code) IsZero() bool { return code.value == "" }

// ISO3 returns the authoritative three-letter mapping, or an empty string when
// the code is absent.
func (code Code) ISO3() string {
	if code.IsZero() {
		return ""
	}
	base, _ := textlanguage.ParseBase(code.value)
	return base.ISO3()
}

// Name returns CLDR display metadata in the requested language.
func Name(code Code, displayLanguage textlanguage.Tag) string {
	if code.IsZero() {
		return ""
	}
	namer := display.Languages(displayLanguage)
	if namer == nil {
		return ""
	}
	return namer.Name(textlanguage.Make(code.value))
}

func validLowerAlpha(value string, length int) bool {
	if !utf8.ValidString(value) || len(value) != length {
		return false
	}
	for index := range value {
		if value[index] < 'a' || value[index] > 'z' {
			return false
		}
	}
	return true
}
