package schemaregistry_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

func TestCompileFingerprintIncludesPortableReferences(t *testing.T) {
	t.Parallel()

	left := compileSchema(t, schemaregistry.FormatAvro, []byte(`{"type":"record","name":"Left","fields":[]}`), nil)
	right := compileSchema(t, schemaregistry.FormatAvro, []byte(`{"type":"record","name":"Right","fields":[]}`), nil)
	content := []byte(`{"type":"record","name":"Root","fields":[]}`)

	withLeft := compileSchema(t, schemaregistry.FormatAvro, content, []schemaregistry.Reference{{
		Name:        "dependency.avsc",
		Fingerprint: left.Fingerprint(),
	}})
	withRight := compileSchema(t, schemaregistry.FormatAvro, content, []schemaregistry.Reference{{
		Name:        "dependency.avsc",
		Fingerprint: right.Fingerprint(),
	}})
	if withLeft.Fingerprint() == withRight.Fingerprint() {
		t.Fatal("fingerprint ignored referenced schema identity")
	}
}

func TestBundleBinaryRoundTripVerifiesContentAndProvenance(t *testing.T) {
	t.Parallel()

	leaf := compileSchema(t, schemaregistry.FormatJSONSchema, []byte(`{"type":"string"}`), nil)
	root := compileSchema(t, schemaregistry.FormatJSONSchema, []byte(`{"type":"object"}`), []schemaregistry.Reference{{
		Name: "leaf", Subject: "leaf", Version: 1, Fingerprint: leaf.Fingerprint(),
	}})
	limits := schemaregistry.GraphLimits{MaxSchemas: 2, MaxDepth: 2, MaxReferences: 1}
	bundle, err := schemaregistry.NewBundle(root, []schemaregistry.Schema{leaf}, limits, schemaregistry.Provenance{
		Source: "release-artifact", Revision: "sha256:manifest",
	})
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	encoded, err := bundle.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	canonicalizers := map[schemaregistry.Format]schemaregistry.Canonicalizer{
		schemaregistry.FormatJSONSchema: canonicalizerFunc(func(_ context.Context, definition schemaregistry.Definition) ([]byte, error) {
			return definition.Content, nil
		}),
	}
	loaded, err := schemaregistry.LoadBundle(context.Background(), encoded, canonicalizers, limits, 4096)
	if err != nil {
		t.Fatalf("LoadBundle() error = %v", err)
	}
	if loaded.Root().Fingerprint() != root.Fingerprint() || loaded.Provenance() != bundle.Provenance() {
		t.Fatalf("LoadBundle() = root %s provenance %+v", loaded.Root().Fingerprint(), loaded.Provenance())
	}

	tampered := []byte(strings.Replace(
		string(encoded),
		"eyJ0eXBlIjoic3RyaW5nIn0=",
		"eyJ0eXBlIjoibnVtYmVyIn0=",
		1,
	))
	_, err = schemaregistry.LoadBundle(context.Background(), tampered, canonicalizers, limits, 4096)
	if !errors.Is(err, schemaregistry.ErrFingerprintCollision) {
		t.Fatalf("LoadBundle(tampered) error = %v, want ErrFingerprintCollision", err)
	}
}

func TestParseFingerprintRejectsMalformedPortableIdentity(t *testing.T) {
	t.Parallel()

	fingerprint := compileSchema(t, schemaregistry.FormatAvro, []byte(`"string"`), nil).Fingerprint()
	parsed, err := schemaregistry.ParseFingerprint(fingerprint.String())
	if err != nil || parsed != fingerprint {
		t.Fatalf("ParseFingerprint() = (%s, %v), want %s", parsed, err, fingerprint)
	}
	for _, value := range []string{"", "md5:0000", "sha256:00"} {
		if _, err := schemaregistry.ParseFingerprint(value); !errors.Is(err, schemaregistry.ErrInvalidSchema) {
			t.Fatalf("ParseFingerprint(%q) error = %v, want ErrInvalidSchema", value, err)
		}
	}
}

func TestReferenceOrderDoesNotChangePortableSchemaIdentity(t *testing.T) {
	t.Parallel()

	left := compileSchema(t, schemaregistry.FormatJSONSchema, []byte(`{"type":"string"}`), nil)
	right := compileSchema(t, schemaregistry.FormatJSONSchema, []byte(`{"type":"number"}`), nil)
	first := compileSchema(t, schemaregistry.FormatJSONSchema, []byte(`{"type":"object"}`), []schemaregistry.Reference{
		{Name: "left", Subject: "subject-left", Version: 1, Fingerprint: left.Fingerprint()},
		{Name: "right", Subject: "subject-right", Version: 1, Fingerprint: right.Fingerprint()},
	})
	second := compileSchema(t, schemaregistry.FormatJSONSchema, []byte(`{"type":"object"}`), []schemaregistry.Reference{
		{Name: "right", Subject: "provider-specific-right", Version: 9, Fingerprint: right.Fingerprint()},
		{Name: "left", Subject: "provider-specific-left", Version: 7, Fingerprint: left.Fingerprint()},
	})
	if first.Fingerprint() != second.Fingerprint() {
		t.Fatal("reference order or provider coordinates changed portable identity")
	}
	_, err := schemaregistry.NewBundle(
		first,
		[]schemaregistry.Schema{second, left, right},
		schemaregistry.GraphLimits{MaxSchemas: 4, MaxDepth: 2, MaxReferences: 4},
		schemaregistry.Provenance{Source: "test", Revision: "1"},
	)
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
}

func TestNewBundleRejectsMissingAndExcessiveGraphs(t *testing.T) {
	t.Parallel()

	missingFingerprint := compileSchema(t, schemaregistry.FormatJSONSchema, []byte(`{"type":"null"}`), nil).Fingerprint()
	rootWithMissing := compileSchema(t, schemaregistry.FormatJSONSchema, []byte(`{"$ref":"missing"}`), []schemaregistry.Reference{{
		Name:        "missing",
		Fingerprint: missingFingerprint,
	}})
	_, err := schemaregistry.NewBundle(
		rootWithMissing,
		nil,
		schemaregistry.GraphLimits{MaxSchemas: 4, MaxDepth: 4, MaxReferences: 4},
		schemaregistry.Provenance{Source: "incident-export", Revision: "2026-08-09"},
	)
	if !errors.Is(err, schemaregistry.ErrReferenceMissing) {
		t.Fatalf("NewBundle(missing) error = %v, want ErrReferenceMissing", err)
	}

	leaf := compileSchema(t, schemaregistry.FormatJSONSchema, []byte(`true`), nil)
	root := compileSchema(t, schemaregistry.FormatJSONSchema, []byte(`{"$ref":"leaf"}`), []schemaregistry.Reference{{
		Name:        "leaf",
		Fingerprint: leaf.Fingerprint(),
	}})
	_, err = schemaregistry.NewBundle(
		root,
		[]schemaregistry.Schema{leaf},
		schemaregistry.GraphLimits{MaxSchemas: 1, MaxDepth: 4, MaxReferences: 4},
		schemaregistry.Provenance{Source: "incident-export", Revision: "2026-08-09"},
	)
	if !errors.Is(err, schemaregistry.ErrReferenceLimit) {
		t.Fatalf("NewBundle(limit) error = %v, want ErrReferenceLimit", err)
	}

}

func TestBundleIsImmutableAndResolvesWithoutProviderIO(t *testing.T) {
	t.Parallel()

	leaf := compileSchema(t, schemaregistry.FormatJSONSchema, []byte(`{"type":"string"}`), nil)
	root := compileSchema(t, schemaregistry.FormatJSONSchema, []byte(`{"$ref":"leaf"}`), []schemaregistry.Reference{{
		Name:        "leaf",
		Fingerprint: leaf.Fingerprint(),
	}})
	bundle, err := schemaregistry.NewBundle(
		root,
		[]schemaregistry.Schema{leaf},
		schemaregistry.GraphLimits{MaxSchemas: 2, MaxDepth: 2, MaxReferences: 1},
		schemaregistry.Provenance{Source: "signed-export", Revision: "sha256:abc"},
	)
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}

	resolved, found := bundle.Resolve(leaf.Fingerprint())
	if !found {
		t.Fatal("Resolve() found = false")
	}
	content := resolved.Definition().Content
	content[0] = 'x'
	if string(mustResolve(t, bundle, leaf.Fingerprint()).Definition().Content) != `{"type":"string"}` {
		t.Fatal("bundle schema content was aliased")
	}
	if bundle.Provenance().Source != "signed-export" {
		t.Fatalf("Provenance().Source = %q", bundle.Provenance().Source)
	}
}

func compileSchema(
	t *testing.T,
	format schemaregistry.Format,
	content []byte,
	references []schemaregistry.Reference,
) schemaregistry.Schema {
	t.Helper()
	schema, err := schemaregistry.Compile(
		context.Background(),
		schemaregistry.Definition{Format: format, Content: content, References: references},
		canonicalizerFunc(func(_ context.Context, definition schemaregistry.Definition) ([]byte, error) {
			return definition.Content, nil
		}),
	)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return schema
}

func mustResolve(
	t *testing.T,
	bundle schemaregistry.Bundle,
	fingerprint schemaregistry.Fingerprint,
) schemaregistry.Schema {
	t.Helper()
	schema, found := bundle.Resolve(fingerprint)
	if !found {
		t.Fatal("Resolve() found = false")
	}
	return schema
}
