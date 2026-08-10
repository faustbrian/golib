package schemaregistry_test

import (
	"context"
	"testing"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

func FuzzCompileDefinitions(f *testing.F) {
	f.Add(uint8(0), []byte(`{"type":"string"}`))
	f.Add(uint8(1), []byte(`"string"`))
	f.Add(uint8(2), []byte(`syntax = "proto3"; message M {}`))
	formats := []schemaregistry.Format{
		schemaregistry.FormatJSONSchema, schemaregistry.FormatAvro, schemaregistry.FormatProtobuf,
	}
	f.Fuzz(func(t *testing.T, formatIndex uint8, content []byte) {
		if len(content) == 0 || len(content) > 4096 {
			t.Skip()
		}
		_, _ = schemaregistry.CompileWithLimits(
			context.Background(),
			schemaregistry.Definition{Format: formats[int(formatIndex)%len(formats)], Content: content},
			canonicalizerFunc(func(_ context.Context, definition schemaregistry.Definition) ([]byte, error) {
				return definition.Content, nil
			}),
			schemaregistry.CompileLimits{MaxSchemaBytes: 4096, MaxCanonicalBytes: 4096, MaxReferences: 16, MaxMetadata: 16},
		)
	})
}

func FuzzReferenceGraphs(f *testing.F) {
	f.Add([]byte{1, 2, 0})
	f.Add([]byte{0})
	f.Fuzz(func(t *testing.T, edges []byte) {
		if len(edges) == 0 || len(edges) > 64 {
			t.Skip()
		}
		nodes := make([]schemaregistry.ReferenceCoordinate, len(edges))
		for index := range nodes {
			nodes[index] = schemaregistry.ReferenceCoordinate{
				Subject: schemaregistry.Subject{Name: string(rune('a' + index))},
				Version: schemaregistry.Version{Number: 1},
			}
		}
		resolver := referenceResolverFunc(func(_ context.Context, coordinate schemaregistry.ReferenceCoordinate) (schemaregistry.ReferenceDocument, error) {
			for index, node := range nodes {
				if node == coordinate {
					target := nodes[int(edges[index])%len(nodes)]
					return schemaregistry.ReferenceDocument{
						Coordinate: coordinate,
						References: []schemaregistry.ProviderReference{{Name: "edge", Target: target}},
					}, nil
				}
			}
			return schemaregistry.ReferenceDocument{}, schemaregistry.ErrReferenceMissing
		})
		_, _ = schemaregistry.BuildReferenceGraph(
			context.Background(), nodes[:1], resolver,
			schemaregistry.GraphLimits{MaxSchemas: len(nodes), MaxDepth: len(nodes), MaxReferences: len(nodes)},
		)
	})
}

func FuzzBundleLoading(f *testing.F) {
	f.Add([]byte(`{"version":1}`))
	f.Add([]byte(`not-json`))
	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) == 0 || len(encoded) > 8192 {
			t.Skip()
		}
		canonicalizers := map[schemaregistry.Format]schemaregistry.Canonicalizer{}
		for _, format := range []schemaregistry.Format{schemaregistry.FormatJSONSchema, schemaregistry.FormatAvro, schemaregistry.FormatProtobuf} {
			canonicalizers[format] = canonicalizerFunc(func(_ context.Context, definition schemaregistry.Definition) ([]byte, error) {
				return definition.Content, nil
			})
		}
		_, _ = schemaregistry.LoadBundle(
			context.Background(), encoded, canonicalizers,
			schemaregistry.GraphLimits{MaxSchemas: 16, MaxDepth: 16, MaxReferences: 32}, 8192,
		)
	})
}
