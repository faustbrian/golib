package country

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	international "github.com/faustbrian/golib/pkg/international"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

type record struct {
	alpha3  string
	numeric int
	status  international.Status
}

// Code is an immutable ISO 3166-1 alpha-2 identifier. Its zero value is absent.
type Code struct{ value string }

// Alpha3 is an immutable ISO 3166-1 alpha-3 identifier.
type Alpha3 struct{ value string }

// Numeric is an immutable zero-padded ISO 3166-1 numeric identifier. It keeps
// the authoritative alpha-2 mapping used to disambiguate reassigned numbers.
type Numeric struct {
	value  string
	alpha2 string
}

// ParseOptions controls explicit acceptance outside current official codes.
type ParseOptions struct {
	AllowHistoric     bool
	AllowReserved     bool
	AllowUserAssigned bool
}

// Parse accepts only uppercase, current official alpha-2 identifiers.
func Parse(input string) (Code, error) {
	return ParseWithOptions(input, ParseOptions{})
}

// ParseWithOptions parses an exact alpha-2 identifier under an explicit policy.
func ParseWithOptions(input string, options ParseOptions) (Code, error) {
	record, exists := countryRecords[input]
	if !validAlpha(input, 2) || !exists || !allowed(record.status, options) {
		return Code{}, international.NewParseError("country code", "unaccepted alpha-2 identifier")
	}
	return Code{value: input}, nil
}

// Canonicalize applies ASCII case canonicalization before strict parsing.
func Canonicalize(input string) (Code, error) {
	if !utf8.ValidString(input) || len(input) != 2 {
		return Code{}, international.NewParseError("country code", "invalid canonicalization input")
	}
	for index := range input {
		character := input[index]
		if (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') {
			return Code{}, international.NewParseError("country code", "invalid canonicalization input")
		}
	}
	return Parse(strings.ToUpper(input))
}

// ParseAlpha3 accepts an exact current official alpha-3 identifier.
func ParseAlpha3(input string) (Alpha3, error) {
	return ParseAlpha3WithOptions(input, ParseOptions{})
}

// ParseAlpha3WithOptions parses an exact alpha-3 identifier under an explicit policy.
func ParseAlpha3WithOptions(input string, options ParseOptions) (Alpha3, error) {
	if !validAlpha(input, 3) {
		return Alpha3{}, international.NewParseError("country code", "invalid alpha-3 identifier")
	}
	for _, record := range countryRecords {
		if record.alpha3 == input && allowed(record.status, options) {
			return Alpha3{value: input}, nil
		}
	}
	return Alpha3{}, international.NewParseError("country code", "unknown alpha-3 identifier")
}

// ParseNumeric accepts an exact zero-padded current numeric identifier.
func ParseNumeric(input string) (Numeric, error) {
	return ParseNumericWithOptions(input, ParseOptions{})
}

// ParseNumericWithOptions parses an exact numeric identifier under an explicit policy.
func ParseNumericWithOptions(input string, options ParseOptions) (Numeric, error) {
	if len(input) != 3 || input[0] < '0' || input[0] > '9' ||
		input[1] < '0' || input[1] > '9' || input[2] < '0' || input[2] > '9' {
		return Numeric{}, international.NewParseError("country code", "invalid numeric identifier")
	}
	numeric, _ := strconv.Atoi(input)
	alpha2 := ""
	for code, record := range countryRecords {
		if record.numeric == numeric && allowed(record.status, options) {
			if alpha2 != "" {
				return Numeric{}, international.NewParseError(
					"country code",
					"ambiguous numeric identifier under selected status policy",
				)
			}
			alpha2 = code
		}
	}
	if alpha2 != "" {
		return Numeric{value: input, alpha2: alpha2}, nil
	}
	return Numeric{}, international.NewParseError("country code", "unknown numeric identifier")
}

// String returns the alpha-2 identifier or an empty string for the zero value.
func (code Code) String() string { return code.value }

// IsZero reports whether the code represents an absent value.
func (code Code) IsZero() bool { return code.value == "" }

// Status returns authoritative registry metadata for the identifier.
func (code Code) Status() international.Status {
	return countryRecords[code.value].status
}

// Alpha3 returns the authoritative alpha-3 mapping when one exists.
func (code Code) Alpha3() (Alpha3, bool) {
	record, exists := countryRecords[code.value]
	if !exists || record.alpha3 == "" {
		return Alpha3{}, false
	}
	return Alpha3{value: record.alpha3}, true
}

// Numeric returns the authoritative numeric mapping when one exists.
func (code Code) Numeric() (Numeric, bool) {
	record, exists := countryRecords[code.value]
	if !exists || record.numeric <= 0 || record.numeric > 999 {
		return Numeric{}, false
	}
	return Numeric{value: fmt.Sprintf("%03d", record.numeric), alpha2: code.value}, true
}

// String returns the alpha-3 identifier.
func (code Alpha3) String() string { return code.value }

// IsZero reports whether the alpha-3 identifier is absent.
func (code Alpha3) IsZero() bool { return code.value == "" }

// Alpha2 returns the authoritative alpha-2 mapping when one exists.
func (code Alpha3) Alpha2() (Code, bool) {
	for alpha2, record := range countryRecords {
		if record.alpha3 == code.value {
			return Code{value: alpha2}, true
		}
	}
	return Code{}, false
}

// Status returns authoritative registry metadata for the identifier.
func (code Alpha3) Status() international.Status {
	alpha2, ok := code.Alpha2()
	if !ok {
		return international.StatusUnknown
	}
	return alpha2.Status()
}

// String returns the three-digit, zero-padded numeric identifier.
func (code Numeric) String() string {
	return code.value
}

// IsZero reports whether the numeric identifier is absent.
func (code Numeric) IsZero() bool { return code.value == "" }

// Alpha2 returns the authoritative alpha-2 mapping when one exists.
func (code Numeric) Alpha2() (Code, bool) {
	record, exists := countryRecords[code.alpha2]
	if !exists || fmt.Sprintf("%03d", record.numeric) != code.value {
		return Code{}, false
	}
	return Code{value: code.alpha2}, true
}

// Status returns authoritative registry metadata for the identifier.
func (code Numeric) Status() international.Status {
	alpha2, ok := code.Alpha2()
	if !ok {
		return international.StatusUnknown
	}
	return alpha2.Status()
}

// Int returns the numeric identifier as an integer, or zero when absent.
func (code Numeric) Int() int {
	value, _ := strconv.Atoi(code.value)
	return value
}

// Name returns CLDR display metadata in the requested language.
func Name(code Code, displayLanguage language.Tag) string {
	if code.IsZero() {
		return ""
	}
	region := language.MustParseRegion(code.value)
	namer := display.Regions(displayLanguage)
	if namer == nil {
		return ""
	}
	return namer.Name(region)
}

// All returns an independent, sorted slice of current official country codes.
func All() []Code {
	codes := make([]Code, len(officialCodes))
	for index, value := range officialCodes {
		codes[index] = Code{value: value}
	}
	return codes
}

func validAlpha(value string, length int) bool {
	if !utf8.ValidString(value) || len(value) != length {
		return false
	}
	for index := range value {
		if value[index] < 'A' || value[index] > 'Z' {
			return false
		}
	}
	return true
}

func allowed(status international.Status, options ParseOptions) bool {
	switch status {
	case international.StatusOfficial:
		return true
	case international.StatusDeleted, international.StatusHistoric, international.StatusTransitional:
		return options.AllowHistoric
	case international.StatusReserved:
		return options.AllowReserved
	case international.StatusUserAssigned:
		return options.AllowUserAssigned
	case international.StatusUnknown:
		return false
	default:
		return false
	}
}
