package jsonschema_test

import (
	"context"
	"testing"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	registryjsonschema "github.com/faustbrian/golib/pkg/schema-registry/formats/jsonschema"
)

func FuzzJSONSchemas(f *testing.F) {
	f.Add([]byte(`true`))
	f.Add([]byte(`{"type":"string"}`))
	adapter, err := registryjsonschema.New(registryjsonschema.Config{
		Dialect: registryjsonschema.Draft202012, MaxSchemaBytes: 4096,
		MaxTotalSchemaBytes: 4096, MaxPayloadBytes: 4096, MaxResources: 4,
	})
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, content []byte) {
		if len(content) == 0 || len(content) > 4096 {
			t.Skip()
		}
		_, _ = schemaregistry.Compile(context.Background(), schemaregistry.Definition{
			Format: schemaregistry.FormatJSONSchema, Content: content,
		}, adapter)
	})
}
