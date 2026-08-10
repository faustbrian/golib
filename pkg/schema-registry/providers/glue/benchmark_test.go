package glue_test

import (
	"context"
	"testing"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	registryglue "github.com/faustbrian/golib/pkg/schema-registry/providers/glue"
)

func BenchmarkFrame(b *testing.B) {
	framer, err := registryglue.NewUncompressedFramer("bench", 4096)
	if err != nil {
		b.Fatalf("NewUncompressedFramer() error = %v", err)
	}
	id := schemaregistry.ProviderID{Provider: registryglue.ProviderName, Scope: "bench", Value: schemaVersionID}
	payload := make([]byte, 1024)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := framer.Frame(context.Background(), id, payload); err != nil {
			b.Fatal(err)
		}
	}
}
