package jsonschema_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/faustbrian/golib/pkg/openrpc/jsonschema"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

func FuzzDraft7CompileAndValidateDeterministically(f *testing.F) {
	f.Add([]byte(`{"$schema":"http://json-schema.org/draft-07/schema#","type":"string"}`), []byte(`"value"`))
	f.Add([]byte(`true`), []byte(`null`))
	f.Fuzz(func(t *testing.T, schemaInput []byte, instanceInput []byte) {
		policy := jsonvalue.Policy{MaxBytes: 1 << 20, MaxDepth: 64, MaxTokens: 100_000}
		schema, err := jsonschema.Parse(schemaInput, policy)
		if err != nil {
			return
		}
		compiled, err := jsonschema.Compile(schema, jsonschema.DefaultValidationOptions())
		if err != nil {
			return
		}
		instance, err := jsonvalue.Parse(instanceInput, policy)
		if err != nil {
			return
		}
		first := compiled.Validate(context.Background(), instance)
		second := compiled.Validate(context.Background(), instance)
		if !reflect.DeepEqual(first.Issues(), second.Issues()) {
			t.Fatal("validation was not deterministic")
		}
	})
}
