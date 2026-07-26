package language

import (
	"database/sql/driver"

	"github.com/faustbrian/golib/pkg/international/internal/codec"
)

// MarshalText encodes a present language as canonical ISO 639 text.
func (code Code) MarshalText() ([]byte, error) { return codec.MarshalText(code.value, "language code") }

// UnmarshalText parses strict current canonical ISO 639 text.
func (code *Code) UnmarshalText(input []byte) error {
	parsed, err := Parse(string(input))
	if err == nil {
		*code = parsed
	}
	return err
}

// MarshalJSON encodes a present code as a string and an absent code as null.
func (code Code) MarshalJSON() ([]byte, error) { return codec.MarshalJSON(code.value) }

// UnmarshalJSON accepts only a current canonical ISO 639 string or null.
func (code *Code) UnmarshalJSON(input []byte) error {
	parsed, absent, err := codec.DecodeJSON(input, "language code", Parse)
	if err == nil {
		if absent {
			*code = Code{}
		} else {
			*code = parsed
		}
	}
	return err
}

// Value returns canonical text or SQL NULL for the zero value.
func (code Code) Value() (driver.Value, error) { return codec.DatabaseValue(code.value) }

// Scan accepts SQL NULL, string, or byte text.
func (code *Code) Scan(source any) error {
	parsed, absent, err := codec.Scan(source, "language code", Parse)
	if err == nil {
		if absent {
			*code = Code{}
		} else {
			*code = parsed
		}
	}
	return err
}
