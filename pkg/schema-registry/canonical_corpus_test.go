package schemaregistry_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	registryavro "github.com/faustbrian/golib/pkg/schema-registry/formats/avro"
	registryjsonschema "github.com/faustbrian/golib/pkg/schema-registry/formats/jsonschema"
	registryprotobuf "github.com/faustbrian/golib/pkg/schema-registry/formats/protobuf"
)

func TestCanonicalFingerprintCorpus(t *testing.T) {
	t.Parallel()

	encoded, err := os.ReadFile("testdata/canonical-fingerprints.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			Format     schemaregistry.Format `json:"format"`
			Equivalent []string              `json:"equivalent"`
			Different  string                `json:"different"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(encoded, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) != 3 {
		t.Fatalf("corpus cases = %d, want 3", len(corpus.Cases))
	}

	jsonCanonicalizer, err := registryjsonschema.New(registryjsonschema.Config{
		MaxSchemaBytes: 4096, MaxTotalSchemaBytes: 4096, MaxPayloadBytes: 4096, MaxResources: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	protobufCanonicalizer, err := registryprotobuf.New(registryprotobuf.Config{
		Filename: "event.proto", MaxSchemaBytes: 4096, MaxImports: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	canonicalizers := map[schemaregistry.Format]schemaregistry.Canonicalizer{
		schemaregistry.FormatAvro:       registryavro.New(4096),
		schemaregistry.FormatJSONSchema: jsonCanonicalizer,
		schemaregistry.FormatProtobuf:   protobufCanonicalizer,
	}

	for _, test := range corpus.Cases {
		test := test
		t.Run(string(test.Format), func(t *testing.T) {
			t.Parallel()
			if len(test.Equivalent) < 2 || test.Different == "" {
				t.Fatal("incomplete corpus case")
			}
			canonicalizer := canonicalizers[test.Format]
			first := compileCorpusSchema(t, test.Format, test.Equivalent[0], canonicalizer)
			for _, content := range test.Equivalent[1:] {
				equivalent := compileCorpusSchema(t, test.Format, content, canonicalizer)
				if equivalent.Fingerprint() != first.Fingerprint() {
					t.Fatalf("equivalent fingerprint = %s, want %s", equivalent.Fingerprint(), first.Fingerprint())
				}
			}
			different := compileCorpusSchema(t, test.Format, test.Different, canonicalizer)
			if different.Fingerprint() == first.Fingerprint() {
				t.Fatalf("non-equivalent schema reused fingerprint %s", first.Fingerprint())
			}
		})
	}
}

func compileCorpusSchema(t *testing.T, format schemaregistry.Format, content string, canonicalizer schemaregistry.Canonicalizer) schemaregistry.Schema {
	t.Helper()
	schema, err := schemaregistry.Compile(context.Background(), schemaregistry.Definition{Format: format, Content: []byte(content)}, canonicalizer)
	if err != nil {
		t.Fatalf("Compile(%s) error = %v", format, err)
	}
	return schema
}
