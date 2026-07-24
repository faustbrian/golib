// Package codec centralizes strict scalar encoding behavior for identifier
// packages. It is internal so public types retain their distinct contracts.
package codec

import (
	"database/sql/driver"
	"encoding/json"

	international "github.com/faustbrian/golib/pkg/international"
)

// MaxEncodedBytes bounds adapter work before a domain parser applies its own
// usually tighter limit.
const MaxEncodedBytes = 512

// MarshalText rejects absent values because text has no unambiguous null.
func MarshalText(value, kind string) ([]byte, error) {
	if value == "" {
		return nil, international.NewParseError(kind, "absent value has no text encoding")
	}
	return []byte(value), nil
}

// MarshalJSON maps an absent value to JSON null and a present value to a
// string.
func MarshalJSON(value string) ([]byte, error) {
	if value == "" {
		return []byte("null"), nil
	}
	return json.Marshal(value)
}

// DecodeJSON accepts only null or a bounded JSON string.
func DecodeJSON[T any](input []byte, kind string, parse func(string) (T, error)) (T, bool, error) {
	var zero T
	if len(input) > MaxEncodedBytes {
		return zero, false, international.ErrResourceLimit
	}
	var value *string
	if err := json.Unmarshal(input, &value); err != nil {
		return zero, false, international.NewParseError(kind, "expected JSON string or null")
	}
	if value == nil {
		return zero, true, nil
	}
	parsed, err := parse(*value)
	return parsed, false, err
}

// DatabaseValue maps an absent value to SQL NULL.
func DatabaseValue(value string) (driver.Value, error) {
	if value == "" {
		return nil, nil
	}
	return value, nil
}

// Scan accepts only SQL NULL, string, or bounded byte slices.
func Scan[T any](source any, kind string, parse func(string) (T, error)) (T, bool, error) {
	var zero T
	if source == nil {
		return zero, true, nil
	}
	var value string
	switch typed := source.(type) {
	case string:
		value = typed
	case []byte:
		value = string(typed)
	default:
		return zero, false, international.NewParseError(kind, "unsupported SQL source type")
	}
	if len(value) > MaxEncodedBytes {
		return zero, false, international.ErrResourceLimit
	}
	parsed, err := parse(value)
	return parsed, false, err
}
