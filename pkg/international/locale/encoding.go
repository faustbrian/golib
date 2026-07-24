package locale

import (
	"database/sql/driver"

	"github.com/faustbrian/golib/pkg/international/internal/codec"
)

// MarshalText encodes the preserved, standards-valid caller representation.
func (tag Tag) MarshalText() ([]byte, error) { return codec.MarshalText(tag.source, "locale tag") }

// UnmarshalText parses bounded BCP 47 text.
func (tag *Tag) UnmarshalText(input []byte) error {
	parsed, err := Parse(string(input))
	if err == nil {
		*tag = parsed
	}
	return err
}

// MarshalJSON encodes a present tag as a string and an absent tag as null.
func (tag Tag) MarshalJSON() ([]byte, error) { return codec.MarshalJSON(tag.source) }

// UnmarshalJSON accepts only a bounded BCP 47 string or null.
func (tag *Tag) UnmarshalJSON(input []byte) error {
	parsed, absent, err := codec.DecodeJSON(input, "locale tag", Parse)
	if err == nil {
		if absent {
			*tag = Tag{}
		} else {
			*tag = parsed
		}
	}
	return err
}

// Value returns preserved text or SQL NULL for the zero value.
func (tag Tag) Value() (driver.Value, error) { return codec.DatabaseValue(tag.source) }

// Scan accepts SQL NULL, string, or byte text.
func (tag *Tag) Scan(source any) error {
	parsed, absent, err := codec.Scan(source, "locale tag", Parse)
	if err == nil {
		if absent {
			*tag = Tag{}
		} else {
			*tag = parsed
		}
	}
	return err
}
