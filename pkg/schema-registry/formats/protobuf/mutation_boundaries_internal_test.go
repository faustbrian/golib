package protobuf

import (
	"context"
	"testing"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

func TestCanonicalizerAcceptsExactImportAndSchemaLimits(t *testing.T) {
	valid := Config{Filename: "root.proto", MaxSchemaBytes: 18, MaxImports: 1}
	for _, mutate := range []func(*Config){
		func(config *Config) { config.MaxSchemaBytes = 0 },
		func(config *Config) { config.MaxImports = 0 },
	} {
		config := valid
		mutate(&config)
		if _, err := New(config); err == nil {
			t.Fatalf("New(%+v) error = nil", config)
		}
	}
	withImport := valid
	withImport.Imports = map[string]string{"unused.proto": `syntax = "proto3";`}
	withImport.MaxSchemaBytes = len("unused.proto") + len(`syntax = "proto3";`)
	if _, err := New(withImport); err != nil {
		t.Fatalf("New(exact import limits) error = %v", err)
	}
	canonicalizer, err := New(valid)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte(`syntax = "proto3";`)
	if len(content) != valid.MaxSchemaBytes {
		t.Fatalf("fixture length = %d", len(content))
	}
	if _, err := canonicalizer.Canonicalize(context.Background(), schemaregistry.Definition{Format: schemaregistry.FormatProtobuf, Content: content}); err != nil {
		t.Fatalf("Canonicalize(exact schema limit) error = %v", err)
	}
}
