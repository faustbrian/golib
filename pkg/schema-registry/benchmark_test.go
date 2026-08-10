package schemaregistry_test

import (
	"context"
	"testing"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

func BenchmarkCompilePortableIdentity(b *testing.B) {
	definition := schemaregistry.Definition{Format: schemaregistry.FormatAvro, Content: []byte(`"string"`)}
	canonicalizer := canonicalizerFunc(func(_ context.Context, definition schemaregistry.Definition) ([]byte, error) {
		return definition.Content, nil
	})
	b.ReportAllocs()
	for b.Loop() {
		if _, err := schemaregistry.Compile(context.Background(), definition, canonicalizer); err != nil {
			b.Fatal(err)
		}
	}
}
