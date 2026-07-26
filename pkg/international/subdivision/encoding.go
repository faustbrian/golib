package subdivision

import (
	"database/sql/driver"

	"github.com/faustbrian/golib/pkg/international/internal/codec"
)

// MarshalText encodes a present subdivision as canonical ISO 3166-2 text.
func (code Code) MarshalText() ([]byte, error) {
	return codec.MarshalText(code.value, "subdivision code")
}

// UnmarshalText parses a strict current subdivision code.
func (code *Code) UnmarshalText(input []byte) error {
	return code.UnmarshalTextWithOptions(input, ParseOptions{})
}

// UnmarshalTextWithOptions parses text under an explicit historic-code policy.
func (code *Code) UnmarshalTextWithOptions(input []byte, options ParseOptions) error {
	parsed, err := ParseWithOptions(string(input), options)
	if err == nil {
		*code = parsed
	}
	return err
}

// MarshalJSON encodes a present code as a string and an absent code as null.
func (code Code) MarshalJSON() ([]byte, error) { return codec.MarshalJSON(code.value) }

// UnmarshalJSON accepts only a current subdivision string or null.
func (code *Code) UnmarshalJSON(input []byte) error {
	return code.UnmarshalJSONWithOptions(input, ParseOptions{})
}

// UnmarshalJSONWithOptions decodes JSON under an explicit historic-code policy.
func (code *Code) UnmarshalJSONWithOptions(input []byte, options ParseOptions) error {
	parsed, absent, err := codec.DecodeJSON(input, "subdivision code", func(value string) (Code, error) {
		return ParseWithOptions(value, options)
	})
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
	return code.ScanWithOptions(source, ParseOptions{})
}

// ScanWithOptions decodes SQL text under an explicit historic-code policy.
func (code *Code) ScanWithOptions(source any, options ParseOptions) error {
	parsed, absent, err := codec.Scan(source, "subdivision code", func(value string) (Code, error) {
		return ParseWithOptions(value, options)
	})
	if err == nil {
		if absent {
			*code = Code{}
		} else {
			*code = parsed
		}
	}
	return err
}
