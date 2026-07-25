package currency

import (
	"strings"
	"unicode/utf8"

	international "github.com/faustbrian/golib/pkg/international"
)

type record struct {
	numeric       string
	minorUnits    uint8
	hasMinorUnits bool
	name          string
	status        international.Status
	history       string
}

// Code is an immutable ISO 4217 alphabetic identifier. Its zero value is
// absent and is distinct from the ISO 4217 XXX code.
type Code struct{ value string }

// Numeric is an immutable zero-padded ISO 4217 numeric identifier. It keeps
// the authoritative alphabetic mapping used to disambiguate reused numbers.
type Numeric struct {
	value      string
	alphabetic string
}

// ParseOptions controls opt-in acceptance of withdrawn identifiers.
type ParseOptions struct {
	AllowHistoric bool
}

// Parse accepts only uppercase currencies in the current SIX List One.
func Parse(input string) (Code, error) {
	return ParseWithOptions(input, ParseOptions{})
}

// ParseWithOptions parses an alphabetic identifier under an explicit policy.
func ParseWithOptions(input string, options ParseOptions) (Code, error) {
	record, exists := currencyRecords[input]
	if !validCode(input) || !exists ||
		(record.status == international.StatusHistoric && !options.AllowHistoric) {
		return Code{}, international.NewParseError("currency code", "unaccepted ISO 4217 identifier")
	}
	return Code{value: input}, nil
}

// ParseNumeric converts a current three-digit ISO 4217 mapping.
func ParseNumeric(input string) (Numeric, error) {
	return ParseNumericWithOptions(input, ParseOptions{})
}

// ParseNumericWithOptions converts an unambiguous numeric mapping under an
// explicit historic-code policy.
func ParseNumericWithOptions(input string, options ParseOptions) (Numeric, error) {
	if !validNumeric(input) {
		return Numeric{}, international.NewParseError("currency code", "invalid numeric identifier")
	}
	alphabetic := ""
	for code, record := range currencyRecords {
		allowed := record.status == international.StatusOfficial ||
			(record.status == international.StatusHistoric && options.AllowHistoric)
		if allowed && record.numeric == input {
			if alphabetic != "" {
				return Numeric{}, international.NewParseError(
					"currency code",
					"ambiguous numeric identifier under selected status policy",
				)
			}
			alphabetic = code
		}
	}
	if alphabetic != "" {
		return Numeric{value: input, alphabetic: alphabetic}, nil
	}
	return Numeric{}, international.NewParseError("currency code", "unknown numeric identifier")
}

// String returns the alphabetic identifier or empty string when absent.
func (code Code) String() string { return code.value }

// IsZero reports whether the code represents an absent value.
func (code Code) IsZero() bool { return code.value == "" }

// Numeric returns the authoritative numeric identifier when one exists.
func (code Code) Numeric() (Numeric, bool) {
	value := currencyRecords[code.value].numeric
	if value == "" {
		return Numeric{}, false
	}
	return Numeric{value: value, alphabetic: code.value}, true
}

// String returns the zero-padded numeric identifier.
func (numeric Numeric) String() string { return numeric.value }

// IsZero reports whether the numeric identifier is absent.
func (numeric Numeric) IsZero() bool { return numeric.value == "" }

// Alphabetic returns the authoritative alphabetic mapping retained at parse or conversion time.
func (numeric Numeric) Alphabetic() (Code, bool) {
	record, exists := currencyRecords[numeric.alphabetic]
	if !exists || record.numeric != numeric.value {
		return Code{}, false
	}
	return Code{value: numeric.alphabetic}, true
}

// Status returns authoritative registry metadata for the retained mapping.
func (numeric Numeric) Status() international.Status {
	alphabetic, ok := numeric.Alphabetic()
	if !ok {
		return international.StatusUnknown
	}
	return alphabetic.Status()
}

// MinorUnits returns the ISO minor-unit exponent and whether it is applicable.
func (code Code) MinorUnits() (uint8, bool) {
	record := currencyRecords[code.value]
	return record.minorUnits, record.hasMinorUnits
}

// Name returns official English SIX metadata only when it is unambiguous.
func (code Code) Name() string {
	record := currencyRecords[code.value]
	if record.name != "" {
		return record.name
	}
	history := code.History()
	if len(history) == 0 {
		return ""
	}
	name := history[0].Name
	for _, entry := range history[1:] {
		if entry.Name != name {
			return ""
		}
	}
	return name
}

// Status returns whether the identifier is current or historic.
func (code Code) Status() international.Status { return currencyRecords[code.value].status }

// WithdrawalDate returns the unmodified SIX date only when every listed entity
// shares one date. Use WithdrawalDates when entity dates differ.
func (code Code) WithdrawalDate() string {
	if code.Status() != international.StatusHistoric {
		return ""
	}
	dates := code.WithdrawalDates()
	if len(dates) != 1 {
		return ""
	}
	return dates[0]
}

// WithdrawalDates returns independent unmodified SIX date values. Multiple
// dates mean the currency was withdrawn at different times by listed entities.
func (code Code) WithdrawalDates() []string {
	history := code.History()
	if len(history) == 0 {
		return nil
	}
	dates := make([]string, 0, len(history))
	for _, entry := range history {
		dates = append(dates, entry.WithdrawalDate)
	}
	return dates
}

// HistoricMetadata preserves one authoritative List Three row projection.
type HistoricMetadata struct {
	Name           string
	WithdrawalDate string
}

// History returns independent historic metadata without choosing an epoch.
func (code Code) History() []HistoricMetadata {
	encoded := currencyRecords[code.value].history
	if encoded == "" {
		return nil
	}
	rows := strings.Split(encoded, "|")
	history := make([]HistoricMetadata, 0, len(rows))
	for _, row := range rows {
		name, withdrawal, _ := strings.Cut(row, "\t")
		history = append(history, HistoricMetadata{Name: name, WithdrawalDate: withdrawal})
	}
	return history
}

// All returns an independent, sorted slice of current identifiers.
func All() []Code {
	codes := make([]Code, len(activeCodes))
	for index, value := range activeCodes {
		codes[index] = Code{value: value}
	}
	return codes
}

func validCode(value string) bool {
	if !utf8.ValidString(value) || len(value) != 3 {
		return false
	}
	return value[0] >= 'A' && value[0] <= 'Z' && value[1] >= 'A' &&
		value[1] <= 'Z' && value[2] >= 'A' && value[2] <= 'Z'
}

func validNumeric(value string) bool {
	return len(value) == 3 && value[0] >= '0' && value[0] <= '9' &&
		value[1] >= '0' && value[1] <= '9' && value[2] >= '0' && value[2] <= '9'
}
