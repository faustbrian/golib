package jsonschema_test

import (
	"context"
	"errors"
	"testing"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	registryjsonschema "github.com/faustbrian/golib/pkg/schema-registry/formats/jsonschema"
)

func TestCanonicalizerValidatesWithGolibJSONSchemaAndNormalizesJSON(t *testing.T) {
	t.Parallel()

	canonicalizer, err := registryjsonschema.New(registryjsonschema.Config{
		MaxSchemaBytes:      4096,
		MaxTotalSchemaBytes: 8192,
		MaxPayloadBytes:     4096,
		MaxResources:        4,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	left, err := schemaregistry.Compile(
		context.Background(),
		schemaregistry.Definition{Format: schemaregistry.FormatJSONSchema, Content: []byte(`{"type":"object","properties":{"id":{"type":"integer"}}}`)},
		canonicalizer,
	)
	if err != nil {
		t.Fatalf("Compile(left) error = %v", err)
	}
	right, err := schemaregistry.Compile(
		context.Background(),
		schemaregistry.Definition{Format: schemaregistry.FormatJSONSchema, Content: []byte("{\n  \"properties\": {\"id\": {\"type\": \"integer\"}},\n  \"type\": \"object\"\n}")},
		canonicalizer,
	)
	if err != nil {
		t.Fatalf("Compile(right) error = %v", err)
	}
	if left.Fingerprint() != right.Fingerprint() {
		t.Fatalf("equivalent fingerprints differ: %s != %s", left.Fingerprint(), right.Fingerprint())
	}

	_, err = schemaregistry.Compile(
		context.Background(),
		schemaregistry.Definition{Format: schemaregistry.FormatJSONSchema, Content: []byte(`{"type":"not-a-type"}`)},
		canonicalizer,
	)
	if !errors.Is(err, schemaregistry.ErrInvalidSchema) {
		t.Fatalf("Compile(invalid) error = %v, want ErrInvalidSchema", err)
	}
}

func TestCodecValidatesEncodedAndDecodedPayloads(t *testing.T) {
	t.Parallel()

	adapter, err := registryjsonschema.New(registryjsonschema.Config{
		MaxSchemaBytes: 4096, MaxTotalSchemaBytes: 8192, MaxPayloadBytes: 64, MaxResources: 4,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	schema, err := schemaregistry.Compile(
		context.Background(),
		schemaregistry.Definition{Format: schemaregistry.FormatJSONSchema, Content: []byte(`{"type":"object","required":["id"],"properties":{"id":{"type":"integer"}}}`)},
		adapter,
	)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	payload, err := adapter.Encode(context.Background(), schema, map[string]any{"id": 7})
	if err != nil || string(payload) != `{"id":7}` {
		t.Fatalf("Encode(valid) = (%s, %v)", payload, err)
	}
	_, err = adapter.Encode(context.Background(), schema, map[string]any{"id": "wrong"})
	if !errors.Is(err, registryjsonschema.ErrPayloadInvalid) {
		t.Fatalf("Encode(invalid) error = %v, want ErrPayloadInvalid", err)
	}

	var target map[string]any
	if err := adapter.Decode(context.Background(), schema, []byte(`{"id":"wrong"}`), &target); !errors.Is(err, registryjsonschema.ErrPayloadInvalid) {
		t.Fatalf("Decode(invalid) error = %v, want ErrPayloadInvalid", err)
	}
}

func TestCanonicalizerUsesOnlyExplicitLocalReferenceResourcesAndDialect(t *testing.T) {
	t.Parallel()

	adapter, err := registryjsonschema.New(registryjsonschema.Config{
		Dialect:             registryjsonschema.Draft7,
		MaxSchemaBytes:      4096,
		MaxTotalSchemaBytes: 8192,
		MaxPayloadBytes:     64,
		MaxResources:        2,
		Resources: map[string][]byte{
			"https://schemas.example/common": []byte(`{"$id":"https://schemas.example/common","type":"string"}`),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = schemaregistry.Compile(
		context.Background(),
		schemaregistry.Definition{
			Format:  schemaregistry.FormatJSONSchema,
			Content: []byte(`{"$schema":"http://json-schema.org/draft-07/schema#","$ref":"https://schemas.example/common"}`),
		},
		adapter,
	)
	if err != nil {
		t.Fatalf("Compile(local reference) error = %v", err)
	}
	_, err = schemaregistry.Compile(
		context.Background(),
		schemaregistry.Definition{Format: schemaregistry.FormatJSONSchema, Content: []byte(`{"$ref":"https://schemas.example/missing"}`)},
		adapter,
	)
	if !errors.Is(err, schemaregistry.ErrInvalidSchema) {
		t.Fatalf("Compile(missing reference) error = %v, want ErrInvalidSchema", err)
	}
}
