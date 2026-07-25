package currency

import (
	"database/sql/driver"

	"github.com/faustbrian/golib/pkg/international/internal/codec"
)

// MarshalText encodes a present currency as canonical alphabetic text.
func (code Code) MarshalText() ([]byte, error) { return codec.MarshalText(code.value, "currency code") }

// UnmarshalText parses a strict active currency code.
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

// UnmarshalJSON accepts only an active currency string or null.
func (code *Code) UnmarshalJSON(input []byte) error {
	return code.UnmarshalJSONWithOptions(input, ParseOptions{})
}

// UnmarshalJSONWithOptions decodes JSON under an explicit historic-code policy.
func (code *Code) UnmarshalJSONWithOptions(input []byte, options ParseOptions) error {
	parsed, absent, err := codec.DecodeJSON(input, "currency code", func(value string) (Code, error) {
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
	parsed, absent, err := codec.Scan(source, "currency code", func(value string) (Code, error) {
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

// MarshalText encodes a present numeric currency identifier.
func (numeric Numeric) MarshalText() ([]byte, error) {
	return codec.MarshalText(numeric.value, "currency numeric code")
}

// UnmarshalText parses strict current numeric text.
func (numeric *Numeric) UnmarshalText(input []byte) error {
	return numeric.UnmarshalTextWithOptions(input, ParseOptions{})
}

// UnmarshalTextWithOptions parses numeric text under an explicit historic-code policy.
func (numeric *Numeric) UnmarshalTextWithOptions(input []byte, options ParseOptions) error {
	parsed, err := ParseNumericWithOptions(string(input), options)
	if err == nil {
		*numeric = parsed
	}
	return err
}

// MarshalJSON encodes a present numeric code as a string and zero as null.
func (numeric Numeric) MarshalJSON() ([]byte, error) { return codec.MarshalJSON(numeric.value) }

// UnmarshalJSON accepts only a current numeric string or null.
func (numeric *Numeric) UnmarshalJSON(input []byte) error {
	return numeric.UnmarshalJSONWithOptions(input, ParseOptions{})
}

// UnmarshalJSONWithOptions decodes numeric JSON under an explicit historic-code policy.
func (numeric *Numeric) UnmarshalJSONWithOptions(input []byte, options ParseOptions) error {
	parsed, absent, err := codec.DecodeJSON(input, "currency numeric code", func(value string) (Numeric, error) {
		return ParseNumericWithOptions(value, options)
	})
	if err == nil {
		if absent {
			*numeric = Numeric{}
		} else {
			*numeric = parsed
		}
	}
	return err
}

// Value returns zero-padded text or SQL NULL for the zero value.
func (numeric Numeric) Value() (driver.Value, error) { return codec.DatabaseValue(numeric.value) }

// Scan accepts SQL NULL, string, or byte numeric text.
func (numeric *Numeric) Scan(source any) error {
	return numeric.ScanWithOptions(source, ParseOptions{})
}

// ScanWithOptions decodes SQL numeric text under an explicit historic-code policy.
func (numeric *Numeric) ScanWithOptions(source any, options ParseOptions) error {
	parsed, absent, err := codec.Scan(source, "currency numeric code", func(value string) (Numeric, error) {
		return ParseNumericWithOptions(value, options)
	})
	if err == nil {
		if absent {
			*numeric = Numeric{}
		} else {
			*numeric = parsed
		}
	}
	return err
}
