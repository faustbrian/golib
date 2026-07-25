package phone

import (
	"database/sql/driver"
	"strings"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/internal/codec"
)

const extensionSeparator = ";ext="

// MarshalText encodes canonical E.164 identity and a separate extension.
func (number Number) MarshalText() ([]byte, error) {
	return codec.MarshalText(number.encoded(), "phone number")
}

// UnmarshalText parses the package's canonical persistence representation.
func (number *Number) UnmarshalText(input []byte) error {
	parsed, err := parseEncodedNumber(string(input))
	if err == nil {
		*number = parsed
	}
	return err
}

// MarshalJSON encodes a present number as a string and an absent number as null.
func (number Number) MarshalJSON() ([]byte, error) { return codec.MarshalJSON(number.encoded()) }

// UnmarshalJSON accepts only a canonical persistence string or null.
func (number *Number) UnmarshalJSON(input []byte) error {
	parsed, absent, err := codec.DecodeJSON(input, "phone number", parseEncodedNumber)
	if err == nil {
		if absent {
			*number = Number{}
		} else {
			*number = parsed
		}
	}
	return err
}

// Value returns canonical persistence text or SQL NULL for the zero value.
func (number Number) Value() (driver.Value, error) {
	return codec.DatabaseValue(number.encoded())
}

// Scan accepts SQL NULL, string, or byte text.
func (number *Number) Scan(source any) error {
	parsed, absent, err := codec.Scan(source, "phone number", parseEncodedNumber)
	if err == nil {
		if absent {
			*number = Number{}
		} else {
			*number = parsed
		}
	}
	return err
}

// MarshalText encodes a present calling code as plus-prefixed text.
func (code CallingCode) MarshalText() ([]byte, error) {
	return codec.MarshalText(code.String(), "calling code")
}

// UnmarshalText parses a supported ITU calling code.
func (code *CallingCode) UnmarshalText(input []byte) error {
	parsed, err := ParseCallingCode(string(input))
	if err == nil {
		*code = parsed
	}
	return err
}

// MarshalJSON encodes a present calling code as a string and zero as null.
func (code CallingCode) MarshalJSON() ([]byte, error) { return codec.MarshalJSON(code.String()) }

// UnmarshalJSON accepts only a supported calling-code string or null.
func (code *CallingCode) UnmarshalJSON(input []byte) error {
	parsed, absent, err := codec.DecodeJSON(input, "calling code", ParseCallingCode)
	if err == nil {
		if absent {
			*code = CallingCode{}
		} else {
			*code = parsed
		}
	}
	return err
}

// Value returns plus-prefixed text or SQL NULL for the zero value.
func (code CallingCode) Value() (driver.Value, error) {
	return codec.DatabaseValue(code.String())
}

// Scan accepts SQL NULL, string, or byte text.
func (code *CallingCode) Scan(source any) error {
	parsed, absent, err := codec.Scan(source, "calling code", ParseCallingCode)
	if err == nil {
		if absent {
			*code = CallingCode{}
		} else {
			*code = parsed
		}
	}
	return err
}

func (number Number) encoded() string {
	if number.IsZero() || number.extension == "" {
		return number.e164
	}
	return number.e164 + extensionSeparator + number.extension
}

func parseEncodedNumber(input string) (Number, error) {
	e164, extension, hasExtension := strings.Cut(input, extensionSeparator)
	if strings.Contains(extension, extensionSeparator) || strings.Contains(e164, ";") {
		return Number{}, international.NewParseError("phone number", "malformed persistence text")
	}
	if !hasExtension {
		return ParseE164(input)
	}
	if extension == "" || len(extension) > MaxExtensionBytes || !decimal(extension) {
		return Number{}, international.NewParseError("phone number", "invalid extension")
	}
	parsed, err := Parse(e164+" ext. "+extension, ParseOptions{})
	if err != nil || parsed.e164 != e164 || parsed.extension != extension {
		return Number{}, international.NewParseError("phone number", "invalid canonical persistence text")
	}
	return parsed, nil
}
