// Package postal provides bounded postal-code values with explicit country
// context. It makes no claim about syntax, deliverability, locality, address
// correctness, geocoding, or carrier acceptance.
package postal

import (
	"strings"
	"unicode"
	"unicode/utf8"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/country"
	"golang.org/x/text/unicode/norm"
)

// MaxBytes bounds stored input and Unicode normalization work.
const MaxBytes = 32

// SpacePolicy controls only explicit Unicode space handling.
type SpacePolicy uint8

const (
	// SpacesPreserve retains all caller-provided spacing.
	SpacesPreserve SpacePolicy = iota
	// SpacesCollapseASCII maps Unicode spaces to one trimmed ASCII space.
	SpacesCollapseASCII
)

// CasePolicy controls explicit ASCII casing only.
type CasePolicy uint8

const (
	// CasePreserve retains caller-provided casing.
	CasePreserve CasePolicy = iota
	// CaseUpperASCII uppercases only ASCII a-z without locale inference.
	CaseUpperASCII
)

// UnicodePolicy controls explicit Unicode normalization.
type UnicodePolicy uint8

const (
	// UnicodePreserve retains the caller-provided normalization form.
	UnicodePreserve UnicodePolicy = iota
	// UnicodeNFC applies Unicode NFC normalization.
	UnicodeNFC
)

// NormalizeOptions selects independent, deterministic transformations.
type NormalizeOptions struct {
	Spaces  SpacePolicy
	Case    CasePolicy
	Unicode UnicodePolicy
}

// Code is an immutable postal value with caller-supplied country context. Its
// zero value is absent. Default formatting is intentionally redacted.
type Code struct {
	value   string
	country country.Code
}

// Parse stores a bounded printable UTF-8 value without syntax normalization.
func Parse(input string, context country.Code) (Code, error) {
	if len(input) > MaxBytes {
		return Code{}, international.ErrResourceLimit
	}
	if input == "" || context.IsZero() || !utf8.ValidString(input) || hasControl(input) {
		return Code{}, international.NewParseError("postal code", "invalid bounded value or country context")
	}
	return Code{value: input, country: context}, nil
}

// Normalize returns a transformed copy under explicit policies.
func (code Code) Normalize(options NormalizeOptions) (Code, error) {
	if code.IsZero() {
		return Code{}, international.NewParseError("postal code", "absent value")
	}
	if options.Spaces > SpacesCollapseASCII || options.Case > CaseUpperASCII ||
		options.Unicode > UnicodeNFC {
		return Code{}, international.NewParseError("postal normalization", "unknown policy")
	}

	value := code.value
	if options.Unicode == UnicodeNFC {
		value = norm.NFC.String(value)
	}
	if options.Spaces == SpacesCollapseASCII {
		value = collapseSpaces(value)
	}
	if options.Case == CaseUpperASCII {
		value = upperASCII(value)
	}
	return Parse(value, code.country)
}

// Raw returns the caller value explicitly.
func (code Code) Raw() string { return code.value }

// Country returns the preserved caller-provided context.
func (code Code) Country() country.Code { return code.country }

// IsZero reports whether the code represents an absent value.
func (code Code) IsZero() bool { return code.value == "" }

// String returns a privacy-safe diagnostic instead of the postal value.
func (code Code) String() string { return "[postal]" }

// GoString returns a privacy-safe diagnostic instead of the postal value.
func (code Code) GoString() string { return "postal.Code{redacted}" }

func hasControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func collapseSpaces(value string) string {
	var result strings.Builder
	result.Grow(len(value))
	pendingSpace := false
	for _, character := range value {
		if unicode.IsSpace(character) {
			pendingSpace = result.Len() > 0
			continue
		}
		if pendingSpace {
			result.WriteByte(' ')
			pendingSpace = false
		}
		result.WriteRune(character)
	}
	return result.String()
}

func upperASCII(value string) string {
	var result strings.Builder
	result.Grow(len(value))
	for _, character := range value {
		if character >= 'a' && character <= 'z' {
			character -= 'a' - 'A'
		}
		result.WriteRune(character)
	}
	return result.String()
}
