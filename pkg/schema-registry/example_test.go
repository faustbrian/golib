package schemaregistry_test

import (
	"context"
	"fmt"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	registryjsonschema "github.com/faustbrian/golib/pkg/schema-registry/formats/jsonschema"
)

func ExampleCompile() {
	adapter, _ := registryjsonschema.New(registryjsonschema.Config{
		Dialect: registryjsonschema.Draft202012, MaxSchemaBytes: 4096,
		MaxTotalSchemaBytes: 4096, MaxPayloadBytes: 4096, MaxResources: 4,
	})
	schema, _ := schemaregistry.Compile(
		context.Background(),
		schemaregistry.Definition{Format: schemaregistry.FormatJSONSchema, Content: []byte(`{"type":"string"}`)},
		adapter,
	)
	fmt.Println(schema.Fingerprint())
	// Output: sha256:82b5ce4e6a57d9d1707cab819781e7ace6a920b1da6e1def3de0fcd2bf91cd64
}
