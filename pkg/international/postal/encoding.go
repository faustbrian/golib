package postal

import (
	"database/sql/driver"
	"strings"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/country"
	"github.com/faustbrian/golib/pkg/international/internal/codec"
)

const contextSeparator = "\t"

// MarshalText encodes explicit country context and the preserved caller value.
func (code Code) MarshalText() ([]byte, error) {
	return codec.MarshalText(code.encoded(), "postal code")
}

// UnmarshalText parses the package's context-preserving representation.
func (code *Code) UnmarshalText(input []byte) error {
	parsed, err := parseEncoded(string(input))
	if err == nil {
		*code = parsed
	}
	return err
}

// MarshalJSON encodes a present value as a string and an absent value as null.
func (code Code) MarshalJSON() ([]byte, error) { return codec.MarshalJSON(code.encoded()) }

// UnmarshalJSON accepts only context-preserving text or null.
func (code *Code) UnmarshalJSON(input []byte) error {
	parsed, absent, err := codec.DecodeJSON(input, "postal code", parseEncoded)
	if err == nil {
		if absent {
			*code = Code{}
		} else {
			*code = parsed
		}
	}
	return err
}

// Value returns context-preserving text or SQL NULL for the zero value.
func (code Code) Value() (driver.Value, error) { return codec.DatabaseValue(code.encoded()) }

// Scan accepts SQL NULL, string, or byte text.
func (code *Code) Scan(source any) error {
	parsed, absent, err := codec.Scan(source, "postal code", parseEncoded)
	if err == nil {
		if absent {
			*code = Code{}
		} else {
			*code = parsed
		}
	}
	return err
}

func (code Code) encoded() string {
	if code.IsZero() {
		return ""
	}
	return code.country.String() + contextSeparator + code.value
}

func parseEncoded(input string) (Code, error) {
	countryText, value, found := strings.Cut(input, contextSeparator)
	if !found || strings.Contains(value, contextSeparator) {
		return Code{}, international.NewParseError("postal code", "missing or repeated country context")
	}
	context, err := country.Parse(countryText)
	if err != nil {
		return Code{}, international.NewParseError("postal code", "invalid country context")
	}
	return Parse(value, context)
}
