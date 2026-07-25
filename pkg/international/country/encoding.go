package country

import (
	"database/sql/driver"

	"github.com/faustbrian/golib/pkg/international/internal/codec"
)

// MarshalText encodes a present country code as canonical alpha-2 text.
func (code Code) MarshalText() ([]byte, error) { return codec.MarshalText(code.value, "country code") }

// UnmarshalText parses strict current alpha-2 text without changing the receiver on error.
func (code *Code) UnmarshalText(input []byte) error {
	return code.UnmarshalTextWithOptions(input, ParseOptions{})
}

// UnmarshalTextWithOptions parses alpha-2 text under an explicit status policy.
func (code *Code) UnmarshalTextWithOptions(input []byte, options ParseOptions) error {
	parsed, err := ParseWithOptions(string(input), options)
	if err == nil {
		*code = parsed
	}
	return err
}

// MarshalJSON encodes a present code as a string and an absent code as null.
func (code Code) MarshalJSON() ([]byte, error) { return codec.MarshalJSON(code.value) }

// UnmarshalJSON accepts only a current alpha-2 string or null.
func (code *Code) UnmarshalJSON(input []byte) error {
	return code.UnmarshalJSONWithOptions(input, ParseOptions{})
}

// UnmarshalJSONWithOptions decodes alpha-2 JSON under an explicit status policy.
func (code *Code) UnmarshalJSONWithOptions(input []byte, options ParseOptions) error {
	parsed, absent, err := codec.DecodeJSON(input, "country code", func(value string) (Code, error) {
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

// ScanWithOptions decodes SQL text under an explicit status policy.
func (code *Code) ScanWithOptions(source any, options ParseOptions) error {
	parsed, absent, err := codec.Scan(source, "country code", func(value string) (Code, error) {
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

// MarshalText encodes a present alpha-3 country identifier.
func (code Alpha3) MarshalText() ([]byte, error) {
	return codec.MarshalText(code.value, "country alpha-3 code")
}

// UnmarshalText parses strict current alpha-3 text.
func (code *Alpha3) UnmarshalText(input []byte) error {
	return code.UnmarshalTextWithOptions(input, ParseOptions{})
}

// UnmarshalTextWithOptions parses alpha-3 text under an explicit status policy.
func (code *Alpha3) UnmarshalTextWithOptions(input []byte, options ParseOptions) error {
	parsed, err := ParseAlpha3WithOptions(string(input), options)
	if err == nil {
		*code = parsed
	}
	return err
}

// MarshalJSON encodes a present alpha-3 code as a string and zero as null.
func (code Alpha3) MarshalJSON() ([]byte, error) { return codec.MarshalJSON(code.value) }

// UnmarshalJSON accepts only a current alpha-3 string or null.
func (code *Alpha3) UnmarshalJSON(input []byte) error {
	return code.UnmarshalJSONWithOptions(input, ParseOptions{})
}

// UnmarshalJSONWithOptions decodes alpha-3 JSON under an explicit status policy.
func (code *Alpha3) UnmarshalJSONWithOptions(input []byte, options ParseOptions) error {
	parsed, absent, err := codec.DecodeJSON(input, "country alpha-3 code", func(value string) (Alpha3, error) {
		return ParseAlpha3WithOptions(value, options)
	})
	if err == nil {
		if absent {
			*code = Alpha3{}
		} else {
			*code = parsed
		}
	}
	return err
}

// Value returns alpha-3 text or SQL NULL for the zero value.
func (code Alpha3) Value() (driver.Value, error) { return codec.DatabaseValue(code.value) }

// Scan accepts SQL NULL, string, or byte alpha-3 text.
func (code *Alpha3) Scan(source any) error {
	return code.ScanWithOptions(source, ParseOptions{})
}

// ScanWithOptions decodes SQL alpha-3 text under an explicit status policy.
func (code *Alpha3) ScanWithOptions(source any, options ParseOptions) error {
	parsed, absent, err := codec.Scan(source, "country alpha-3 code", func(value string) (Alpha3, error) {
		return ParseAlpha3WithOptions(value, options)
	})
	if err == nil {
		if absent {
			*code = Alpha3{}
		} else {
			*code = parsed
		}
	}
	return err
}

// MarshalText encodes a present numeric country identifier.
func (code Numeric) MarshalText() ([]byte, error) {
	return codec.MarshalText(code.String(), "country numeric code")
}

// UnmarshalText parses strict current numeric text.
func (code *Numeric) UnmarshalText(input []byte) error {
	return code.UnmarshalTextWithOptions(input, ParseOptions{})
}

// UnmarshalTextWithOptions parses numeric text under an explicit status policy.
func (code *Numeric) UnmarshalTextWithOptions(input []byte, options ParseOptions) error {
	parsed, err := ParseNumericWithOptions(string(input), options)
	if err == nil {
		*code = parsed
	}
	return err
}

// MarshalJSON encodes a present numeric code as a string and zero as null.
func (code Numeric) MarshalJSON() ([]byte, error) { return codec.MarshalJSON(code.String()) }

// UnmarshalJSON accepts only a current numeric string or null.
func (code *Numeric) UnmarshalJSON(input []byte) error {
	return code.UnmarshalJSONWithOptions(input, ParseOptions{})
}

// UnmarshalJSONWithOptions decodes numeric JSON under an explicit status policy.
func (code *Numeric) UnmarshalJSONWithOptions(input []byte, options ParseOptions) error {
	parsed, absent, err := codec.DecodeJSON(input, "country numeric code", func(value string) (Numeric, error) {
		return ParseNumericWithOptions(value, options)
	})
	if err == nil {
		if absent {
			*code = Numeric{}
		} else {
			*code = parsed
		}
	}
	return err
}

// Value returns zero-padded text or SQL NULL for the zero value.
func (code Numeric) Value() (driver.Value, error) { return codec.DatabaseValue(code.String()) }

// Scan accepts SQL NULL, string, or byte numeric text.
func (code *Numeric) Scan(source any) error {
	return code.ScanWithOptions(source, ParseOptions{})
}

// ScanWithOptions decodes SQL numeric text under an explicit status policy.
func (code *Numeric) ScanWithOptions(source any, options ParseOptions) error {
	parsed, absent, err := codec.Scan(source, "country numeric code", func(value string) (Numeric, error) {
		return ParseNumericWithOptions(value, options)
	})
	if err == nil {
		if absent {
			*code = Numeric{}
		} else {
			*code = parsed
		}
	}
	return err
}
