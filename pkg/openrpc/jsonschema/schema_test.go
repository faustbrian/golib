package jsonschema_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openrpc/jsonschema"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

func TestParsePreservesBooleanAndObjectSchemas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		boolean   bool
		boolValue bool
	}{
		{name: "true", input: ` true `, boolean: true, boolValue: true},
		{name: "false", input: `false`, boolean: true, boolValue: false},
		{name: "object", input: ` {"type":"integer","maximum":12345678901234567890} `},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			schema, err := jsonschema.Parse([]byte(test.input), jsonvalue.DefaultPolicy())
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(schema.Bytes(), []byte(test.input)) {
				t.Fatalf("Bytes() = %q", schema.Bytes())
			}
			if !bytes.Equal(schema.Value().Bytes(), []byte(test.input)) {
				t.Fatalf("Value() = %q", schema.Value().Bytes())
			}
			if encoded, err := schema.MarshalJSON(); err != nil || !bytes.Equal(encoded, []byte(test.input)) {
				t.Fatalf("MarshalJSON() = %q, %v", encoded, err)
			}
			value, isBoolean := schema.Boolean()
			if isBoolean != test.boolean || value != test.boolValue {
				t.Fatalf("Boolean() = (%t, %t)", value, isBoolean)
			}
		})
	}
}

func TestParseRejectsNonSchemaJSONKinds(t *testing.T) {
	t.Parallel()

	for _, input := range []string{`null`, `0`, `"schema"`, `[]`} {
		_, err := jsonschema.Parse([]byte(input), jsonvalue.DefaultPolicy())
		if !errors.Is(err, jsonschema.ErrInvalidSchema) {
			t.Errorf("Parse(%s) error = %v, want ErrInvalidSchema", input, err)
		}
	}
}

func TestParsePropagatesJSONErrorsAndRejectsZeroValues(t *testing.T) {
	t.Parallel()

	if _, err := jsonschema.Parse([]byte(`{`), jsonvalue.DefaultPolicy()); !errors.Is(err, jsonvalue.ErrInvalidJSON) {
		t.Fatalf("malformed schema error = %v", err)
	}
	if _, err := jsonschema.FromValue(jsonvalue.Value{}); !errors.Is(err, jsonschema.ErrInvalidSchema) {
		t.Fatalf("zero schema error = %v", err)
	}
}

func TestSchemaBytesReturnsOwnedStorage(t *testing.T) {
	t.Parallel()

	schema, err := jsonschema.Parse([]byte(`{"type":"string"}`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	returned := schema.Bytes()
	returned[0] = '['
	if schema.Bytes()[0] != '{' {
		t.Fatal("Bytes exposed mutable schema storage")
	}
}
