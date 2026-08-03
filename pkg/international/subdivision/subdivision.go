package subdivision

import (
	"strings"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/country"
)

type record struct {
	name   string
	status international.Status
}

// Code is an immutable ISO 3166-2-style identifier. Its zero value is absent.
type Code struct{ value string }

// ParseOptions controls opt-in acceptance of deleted identifiers.
type ParseOptions struct {
	AllowHistoric bool
}

// Parse accepts only current uppercase subdivision identifiers.
func Parse(input string) (Code, error) {
	return ParseWithOptions(input, ParseOptions{})
}

// ParseWithOptions parses a subdivision identifier under an explicit policy.
func ParseWithOptions(input string, options ParseOptions) (Code, error) {
	record, exists := subdivisionRecords[input]
	if !validCode(input) || !exists ||
		(record.status == international.StatusDeleted && !options.AllowHistoric) {
		return Code{}, international.NewParseError("subdivision code", "unaccepted ISO 3166-2 identifier")
	}
	return Code{value: input}, nil
}

// String returns the identifier or empty string for the zero value.
func (code Code) String() string { return code.value }

// IsZero reports whether the code represents an absent value.
func (code Code) IsZero() bool { return code.value == "" }

// Country returns the caller-visible country context encoded in the identifier.
func (code Code) Country() country.Code {
	if code.IsZero() {
		return country.Code{}
	}
	parent, _ := country.ParseWithOptions(code.value[:2], country.ParseOptions{
		AllowHistoric: true, AllowReserved: true, AllowUserAssigned: true,
	})
	return parent
}

// Suffix returns the country-local subdivision suffix.
func (code Code) Suffix() string {
	if code.IsZero() {
		return ""
	}
	return code.value[3:]
}

// Name returns current CLDR English display metadata when available.
func (code Code) Name() string { return subdivisionRecords[code.value].name }

// Status returns current or deleted registry metadata.
func (code Code) Status() international.Status { return subdivisionRecords[code.value].status }

// All returns an independent, sorted slice of current identifiers.
func All() []Code {
	codes := make([]Code, len(currentSubdivisionCodes))
	for index, value := range currentSubdivisionCodes {
		codes[index] = Code{value: value}
	}
	return codes
}

func validCode(value string) bool {
	if len(value) < 4 {
		return false
	}
	if len(value) > 6 {
		return false
	}
	if value[2] != '-' {
		return false
	}
	if !asciiUpper(value[0]) || !asciiUpper(value[1]) {
		return false
	}
	for _, character := range value[3:] {
		if !asciiUpperOrDigit(character) {
			return false
		}
	}
	return true
}

func asciiUpper(character byte) bool {
	return strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZ", rune(character))
}

func asciiUpperOrDigit(character rune) bool {
	return strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", character)
}
