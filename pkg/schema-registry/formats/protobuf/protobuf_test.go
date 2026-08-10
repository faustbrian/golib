package protobuf_test

import (
	"context"
	"errors"
	"testing"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	registryprotobuf "github.com/faustbrian/golib/pkg/schema-registry/formats/protobuf"
)

func TestCanonicalizerUsesDeterministicLinkedDescriptors(t *testing.T) {
	t.Parallel()

	left, err := registryprotobuf.New(registryprotobuf.Config{
		Filename:       "event.proto",
		MaxSchemaBytes: 4096,
		MaxImports:     4,
	})
	if err != nil {
		t.Fatalf("New(left) error = %v", err)
	}
	right, err := registryprotobuf.New(registryprotobuf.Config{
		Filename:       "event.proto",
		MaxSchemaBytes: 4096,
		MaxImports:     4,
	})
	if err != nil {
		t.Fatalf("New(right) error = %v", err)
	}
	leftSchema, err := schemaregistry.Compile(
		context.Background(),
		schemaregistry.Definition{Format: schemaregistry.FormatProtobuf, Content: []byte("syntax = \"proto3\"; // comment\nmessage Event { int64 id = 1; }")},
		left,
	)
	if err != nil {
		t.Fatalf("Compile(left) error = %v", err)
	}
	rightSchema, err := schemaregistry.Compile(
		context.Background(),
		schemaregistry.Definition{Format: schemaregistry.FormatProtobuf, Content: []byte("syntax=\"proto3\";message Event{int64 id=1;} ")},
		right,
	)
	if err != nil {
		t.Fatalf("Compile(right) error = %v", err)
	}
	if leftSchema.Fingerprint() != rightSchema.Fingerprint() {
		t.Fatalf("canonical fingerprints differ: %s != %s", leftSchema.Fingerprint(), rightSchema.Fingerprint())
	}

	_, err = schemaregistry.Compile(
		context.Background(),
		schemaregistry.Definition{Format: schemaregistry.FormatProtobuf, Content: []byte("syntax = \"proto3\"; message Event { Missing value = 1; }")},
		left,
	)
	if !errors.Is(err, schemaregistry.ErrInvalidSchema) {
		t.Fatalf("Compile(invalid) error = %v, want ErrInvalidSchema", err)
	}
}
