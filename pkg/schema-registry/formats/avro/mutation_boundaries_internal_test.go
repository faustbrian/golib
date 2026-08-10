package avro

import (
	"context"
	"strings"
	"testing"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

func TestCanonicalizerAcceptsExactSchemaByteLimit(t *testing.T) {
	content := []byte(`"string"`)
	canonicalizer := New(len(content))
	if _, err := canonicalizer.Canonicalize(context.Background(), schemaregistry.Definition{Format: schemaregistry.FormatAvro, Content: content}); err != nil {
		t.Fatalf("Canonicalize(exact limit) error = %v", err)
	}
	if _, err := New(0).Canonicalize(context.Background(), schemaregistry.Definition{Format: schemaregistry.FormatAvro, Content: content}); err == nil || !strings.Contains(err.Error(), "invalid Avro canonicalizer") {
		t.Fatalf("Canonicalize(zero limit) error = %v", err)
	}
}
