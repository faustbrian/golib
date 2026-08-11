package golib_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/cloudevents"
	golib "github.com/faustbrian/golib/pkg/cloudevents/adapters/golib"
	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

func TestSchemaValidatorsRejectNilContext(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	directSchema, err := compiler.Compile(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	adapter := newSchemaHardeningAdapter(t)
	schema := compileSchemaHardeningSchema(t, adapter)
	lookup := schemaHardeningLookup()
	cache := newSchemaHardeningCache(t, schemaHardeningResolverFunc(
		func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
			return schemaHardeningResult(schema), nil
		},
	))
	validator := newSchemaHardeningValidator(t, golib.RegistryJSONSchemaConfig{
		Cache:              cache,
		SchemaLookups:      map[string]schemaregistry.Lookup{"schema": lookup},
		Adapter:            adapter,
		AvailabilityPolicy: schemaregistry.FailClosed,
		Timeout:            time.Second,
	})
	directValidator := golib.JSONSchemaValidator{URI: "schema", Schema: directSchema}
	tests := map[string]func() error{
		"direct": func() error {
			//lint:ignore SA1012 Nil is the contract under test.
			return directValidator.Validate(nil, "schema", "application/json", []byte(`{}`)) //nolint:staticcheck // Nil is the contract under test.
		},
		"registry": func() error {
			//lint:ignore SA1012 Nil is the contract under test.
			return validator.Validate(nil, "schema", "application/json", []byte(`{}`)) //nolint:staticcheck // Nil is the contract under test.
		},
	}
	for name, validate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := validate(); !errors.Is(err, cloudevents.ErrContextRequired) {
				t.Fatalf("nil context error = %v, want %v", err, cloudevents.ErrContextRequired)
			}
		})
	}
}
