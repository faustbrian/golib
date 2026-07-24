// Package jsonschema provides lossless JSON Schema Draft 7 values used by
// OpenRPC. It supports both object and boolean schemas without reflection.
package jsonschema

import (
	"bytes"
	"errors"

	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

// ErrInvalidSchema reports a valid JSON value that is neither an object nor a
// boolean and therefore cannot be a Draft 7 schema.
var ErrInvalidSchema = errors.New("jsonschema: schema must be an object or boolean")

// Schema is an immutable object or boolean JSON Schema value.
type Schema struct {
	value     jsonvalue.Value
	boolean   bool
	boolValue bool
}

// Parse validates and preserves an object or boolean JSON Schema value under
// policy. Keyword and reference semantics are validated separately.
func Parse(input []byte, policy jsonvalue.Policy) (Schema, error) {
	value, err := jsonvalue.Parse(input, policy)
	if err != nil {
		return Schema{}, err
	}
	return FromValue(value)
}

// FromValue classifies an already validated JSON value as a Draft 7 schema.
func FromValue(value jsonvalue.Value) (Schema, error) {
	trimmed := bytes.TrimSpace(value.Bytes())
	if len(trimmed) == 0 {
		return Schema{}, ErrInvalidSchema
	}

	switch trimmed[0] {
	case '{':
		return Schema{value: value}, nil
	case 't':
		if bytes.Equal(trimmed, []byte("true")) {
			return Schema{value: value, boolean: true, boolValue: true}, nil
		}
	case 'f':
		if bytes.Equal(trimmed, []byte("false")) {
			return Schema{value: value, boolean: true}, nil
		}
	}
	return Schema{}, ErrInvalidSchema
}

// Boolean returns the schema value and true for a boolean schema. The second
// result is false for an object schema.
func (schema Schema) Boolean() (bool, bool) {
	return schema.boolValue, schema.boolean
}

// Bytes returns an owned copy of the exact schema JSON.
func (schema Schema) Bytes() []byte {
	return schema.value.Bytes()
}

// Value returns the immutable generic JSON representation.
func (schema Schema) Value() jsonvalue.Value {
	return schema.value
}

// MarshalJSON implements json.Marshaler.
func (schema Schema) MarshalJSON() ([]byte, error) {
	return schema.value.MarshalJSON()
}
