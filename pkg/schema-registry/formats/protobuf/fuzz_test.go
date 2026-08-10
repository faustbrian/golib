package protobuf_test

import (
	"context"
	"testing"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	registryprotobuf "github.com/faustbrian/golib/pkg/schema-registry/formats/protobuf"
)

func FuzzProtobufSchemas(f *testing.F) {
	f.Add([]byte(`syntax = "proto3"; message M {}`))
	f.Add([]byte(`syntax = "proto2"; message M { optional string v = 1; }`))
	canonicalizer, err := registryprotobuf.New(registryprotobuf.Config{
		Filename: "fuzz.proto", MaxSchemaBytes: 4096, MaxImports: 4,
	})
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, content []byte) {
		if len(content) == 0 || len(content) > 4096 {
			t.Skip()
		}
		_, _ = schemaregistry.Compile(context.Background(), schemaregistry.Definition{
			Format: schemaregistry.FormatProtobuf, Content: content,
		}, canonicalizer)
	})
}
