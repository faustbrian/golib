package avro_test

import (
	"context"
	"errors"
	"testing"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	registryavro "github.com/faustbrian/golib/pkg/schema-registry/formats/avro"
)

func TestCanonicalizerUsesAvroParsingCanonicalForm(t *testing.T) {
	t.Parallel()

	canonicalizer := registryavro.New(4096)
	left, err := schemaregistry.Compile(
		context.Background(),
		schemaregistry.Definition{Format: schemaregistry.FormatAvro, Content: []byte(`{"type":"record","name":"Event","doc":"ignored","fields":[{"name":"id","type":"long"}]}`)},
		canonicalizer,
	)
	if err != nil {
		t.Fatalf("Compile(left) error = %v", err)
	}
	right, err := schemaregistry.Compile(
		context.Background(),
		schemaregistry.Definition{Format: schemaregistry.FormatAvro, Content: []byte(`{"name":"Event","type":"record","fields":[{"type":"long","name":"id"}]}`)},
		canonicalizer,
	)
	if err != nil {
		t.Fatalf("Compile(right) error = %v", err)
	}
	if left.Fingerprint() != right.Fingerprint() {
		t.Fatalf("canonical fingerprints differ: %s != %s", left.Fingerprint(), right.Fingerprint())
	}

	_, err = schemaregistry.Compile(
		context.Background(),
		schemaregistry.Definition{Format: schemaregistry.FormatAvro, Content: []byte(`{"type":"record"}`)},
		canonicalizer,
	)
	if !errors.Is(err, schemaregistry.ErrInvalidSchema) {
		t.Fatalf("Compile(invalid) error = %v, want ErrInvalidSchema", err)
	}
}
