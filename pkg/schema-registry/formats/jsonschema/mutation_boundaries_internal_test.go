package jsonschema

import (
	"context"
	"strings"
	"testing"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

func TestAdapterAcceptsExactResourceSchemaAndPayloadLimits(t *testing.T) {
	valid := Config{Dialect: Draft202012, MaxSchemaBytes: 4, MaxTotalSchemaBytes: 4, MaxPayloadBytes: 4, MaxResources: 1}
	for _, mutate := range []func(*Config){
		func(config *Config) { config.MaxSchemaBytes = 0 },
		func(config *Config) { config.MaxTotalSchemaBytes = 0 },
		func(config *Config) { config.MaxPayloadBytes = 0 },
		func(config *Config) { config.MaxResources = 0 },
	} {
		config := valid
		mutate(&config)
		if _, err := New(config); err == nil || !strings.Contains(err.Error(), "limits must be positive") {
			t.Fatalf("New(%+v) error = %v", config, err)
		}
	}
	withResource := valid
	withResource.Resources = map[string][]byte{"urn:x": []byte("true")}
	if _, err := New(withResource); err != nil {
		t.Fatalf("New(exact resource limits) error = %v", err)
	}
	adapter, err := New(valid)
	if err != nil {
		t.Fatal(err)
	}
	definition := schemaregistry.Definition{Format: schemaregistry.FormatJSONSchema, Content: []byte("true")}
	if _, err := adapter.Canonicalize(context.Background(), definition); err != nil {
		t.Fatalf("Canonicalize(exact schema limit) error = %v", err)
	}
	schema, err := schemaregistry.Compile(context.Background(), definition, adapter)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Encode(context.Background(), schema, nil); err != nil {
		t.Fatalf("Encode(exact payload limit) error = %v", err)
	}
	var target any
	if err := adapter.Decode(context.Background(), schema, []byte("null"), &target); err != nil {
		t.Fatalf("Decode(exact payload limit) error = %v", err)
	}
}
