package confluent_test

import (
	"context"
	"testing"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	"github.com/faustbrian/golib/pkg/schema-registry/providers/confluent"
)

func BenchmarkClassicFrame(b *testing.B) {
	framer, err := confluent.NewClassicFramer("bench", 4096)
	if err != nil {
		b.Fatalf("NewClassicFramer() error = %v", err)
	}
	id := schemaregistry.ProviderID{Provider: confluent.ProviderName, Scope: "bench", Value: "1"}
	payload := make([]byte, 1024)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := framer.Frame(context.Background(), id, payload); err != nil {
			b.Fatal(err)
		}
	}
}
