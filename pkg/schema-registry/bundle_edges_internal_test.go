package schemaregistry

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBundleConstructionBoundaries(t *testing.T) {
	t.Parallel()

	root := internalSchema(t, FormatAvro, `"string"`, nil)
	provenance := Provenance{Source: "test", Revision: "1"}
	valid := GraphLimits{MaxSchemas: 3, MaxDepth: 3, MaxReferences: 3}
	for _, test := range []struct {
		name         string
		root         Schema
		dependencies []Schema
		limits       GraphLimits
		provenance   Provenance
		want         error
	}{
		{"invalid limits", root, nil, GraphLimits{}, provenance, ErrInvalidRequest},
		{"invalid provenance", root, nil, valid, Provenance{}, ErrInvalidRequest},
		{"too many schemas", root, []Schema{root, root, root}, valid, provenance, ErrReferenceLimit},
		{"zero root", Schema{}, nil, valid, provenance, ErrInvalidSchema},
	} {
		_, err := NewBundle(test.root, test.dependencies, test.limits, test.provenance)
		if !errors.Is(err, test.want) {
			t.Fatalf("NewBundle(%s) error = %v, want %v", test.name, err, test.want)
		}
	}
	missing := Fingerprint{sum: [32]byte{9}}
	rootMissing := internalSchema(t, FormatAvro, `"string"`, []Reference{{Name: "missing", Fingerprint: missing}})
	if _, err := NewBundle(rootMissing, nil, valid, provenance); !errors.Is(err, ErrReferenceMissing) {
		t.Fatalf("NewBundle(missing) error = %v", err)
	}
	leaf := internalSchema(t, FormatAvro, `"long"`, nil)
	rootWithLeaf := internalSchema(t, FormatAvro, `"string"`, []Reference{{Name: "leaf", Fingerprint: leaf.Fingerprint()}})
	if _, err := NewBundle(rootWithLeaf, []Schema{leaf}, GraphLimits{MaxSchemas: 2, MaxDepth: 1, MaxReferences: 1}, provenance); !errors.Is(err, ErrReferenceLimit) {
		t.Fatalf("NewBundle(depth) error = %v", err)
	}
	if _, err := NewBundle(rootWithLeaf, []Schema{leaf}, GraphLimits{MaxSchemas: 2, MaxDepth: 2, MaxReferences: 0}, provenance); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("NewBundle(reference config) error = %v", err)
	}
	rootWithSharedLeaf := internalSchema(t, FormatAvro, `"bytes"`, []Reference{
		{Name: "a", Fingerprint: leaf.Fingerprint()}, {Name: "b", Fingerprint: leaf.Fingerprint()},
	})
	if _, err := NewBundle(rootWithSharedLeaf, []Schema{leaf}, GraphLimits{MaxSchemas: 2, MaxDepth: 2, MaxReferences: 1}, provenance); !errors.Is(err, ErrReferenceLimit) {
		t.Fatalf("NewBundle(reference count) error = %v", err)
	}
	if _, err := NewBundle(rootWithSharedLeaf, []Schema{leaf}, GraphLimits{MaxSchemas: 2, MaxDepth: 2, MaxReferences: 2}, provenance); err != nil {
		t.Fatalf("NewBundle(shared dependency) error = %v", err)
	}
	manualLeft := Schema{definition: Definition{Format: FormatAvro}, canonical: []byte("left"), fingerprint: Fingerprint{sum: [32]byte{1}}}
	manualRight := Schema{definition: Definition{Format: FormatAvro}, canonical: []byte("right"), fingerprint: manualLeft.fingerprint}
	if _, err := NewBundle(manualLeft, []Schema{manualRight}, valid, provenance); !errors.Is(err, ErrFingerprintCollision) {
		t.Fatalf("NewBundle(collision) error = %v", err)
	}
	aFingerprint := Fingerprint{sum: [32]byte{3}}
	bFingerprint := Fingerprint{sum: [32]byte{4}}
	a := Schema{definition: Definition{Format: FormatAvro, References: []Reference{{Name: "b", Fingerprint: bFingerprint}}}, canonical: []byte("a"), fingerprint: aFingerprint}
	b := Schema{definition: Definition{Format: FormatAvro, References: []Reference{{Name: "a", Fingerprint: aFingerprint}}}, canonical: []byte("b"), fingerprint: bFingerprint}
	if _, err := NewBundle(a, []Schema{b}, valid, provenance); !errors.Is(err, ErrReferenceCycle) {
		t.Fatalf("NewBundle(cycle) error = %v", err)
	}
	if !sameSchema(root, root) || sameSchema(root, leaf) {
		t.Fatal("sameSchema() identity mismatch")
	}
	leftReference := Schema{definition: Definition{Format: FormatAvro, References: []Reference{{Name: "a", Fingerprint: aFingerprint}}}, canonical: []byte("same")}
	rightReference := Schema{definition: Definition{Format: FormatAvro, References: []Reference{{Name: "b", Fingerprint: aFingerprint}}}, canonical: []byte("same")}
	if sameSchema(leftReference, rightReference) {
		t.Fatal("sameSchema() ignored different reference names")
	}
	if compareBundleSchemaFingerprints(bundleSchemaWire{Fingerprint: "a"}, bundleSchemaWire{Fingerprint: "b"}) != -1 ||
		compareBundleSchemaFingerprints(bundleSchemaWire{Fingerprint: "b"}, bundleSchemaWire{Fingerprint: "a"}) != 1 ||
		compareBundleSchemaFingerprints(bundleSchemaWire{Fingerprint: "a"}, bundleSchemaWire{Fingerprint: "a"}) != 0 {
		t.Fatal("compareBundleSchemaFingerprints() ordering is invalid")
	}
}

func TestBundleSerializationBoundaries(t *testing.T) {
	t.Parallel()

	if _, err := (Bundle{}).MarshalBinary(); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("MarshalBinary(empty) error = %v", err)
	}
	limits := GraphLimits{MaxSchemas: 2, MaxDepth: 2, MaxReferences: 2}
	canonicalizers := map[Format]Canonicalizer{
		FormatAvro: canonicalizerFunction(func(_ context.Context, definition Definition) ([]byte, error) { return definition.Content, nil }),
	}
	for _, test := range []struct {
		name    string
		ctx     context.Context
		encoded []byte
		max     int
		want    error
	}{
		{"empty", context.Background(), nil, 16, ErrInvalidRequest},
		{"invalid max", context.Background(), []byte("x"), 0, ErrInvalidRequest},
		{"too large", context.Background(), []byte("xx"), 1, ErrLimitExceeded},
		{"invalid JSON", context.Background(), []byte("x"), 16, ErrInvalidSchema},
		{"trailing JSON", context.Background(), []byte(`{"version":1} {}`), 64, ErrInvalidSchema},
		{"wrong version", context.Background(), []byte(`{"version":2,"root":"x","schemas":[{}]}`), 128, ErrInvalidSchema},
	} {
		if test.name == "empty" || test.name == "invalid max" || test.name == "too large" || test.name == "invalid JSON" || test.name == "trailing JSON" || test.name == "wrong version" {
			_, err := LoadBundle(test.ctx, test.encoded, canonicalizers, limits, test.max)
			if !errors.Is(err, test.want) {
				t.Fatalf("LoadBundle(%s) error = %v, want %v", test.name, err, test.want)
			}
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LoadBundle(canceled, []byte(`{}`), canonicalizers, limits, 16); !errors.Is(err, context.Canceled) {
		t.Fatalf("LoadBundle(canceled) error = %v", err)
	}
	root := internalSchema(t, FormatAvro, `"string"`, nil)
	bundle, err := NewBundle(root, nil, limits, Provenance{Source: "test", Revision: "1"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := bundle.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	unknownFormat := []byte(strings.Replace(string(encoded), `"format":"avro"`, `"format":"protobuf"`, 1))
	if _, err := LoadBundle(context.Background(), unknownFormat, canonicalizers, limits, 4096); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("LoadBundle(unsupported format) error = %v", err)
	}
	missingRoot := []byte(strings.Replace(string(encoded), root.Fingerprint().String(), strings.Repeat("sha256:0", 1), 1))
	if _, err := LoadBundle(context.Background(), missingRoot, canonicalizers, limits, 4096); err == nil {
		t.Fatal("LoadBundle(malformed root) error = nil")
	}
	withoutRoot := []byte(strings.Replace(string(encoded), `"root":"`+root.Fingerprint().String()+`"`, `"root":"sha256:`+strings.Repeat("0", 64)+`"`, 1))
	if _, err := LoadBundle(context.Background(), withoutRoot, canonicalizers, limits, 4096); !errors.Is(err, ErrReferenceMissing) {
		t.Fatalf("LoadBundle(missing root) error = %v", err)
	}
	invalidEntry := []byte(strings.Replace(string(encoded), `"fingerprint":"`+root.Fingerprint().String()+`"`, `"fingerprint":"invalid"`, 1))
	if _, err := LoadBundle(context.Background(), invalidEntry, canonicalizers, limits, 4096); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("LoadBundle(invalid entry fingerprint) error = %v", err)
	}
	invalidContent := []byte(strings.Replace(string(encoded), `"content":"InN0cmluZyI="`, `"content":""`, 1))
	if _, err := LoadBundle(context.Background(), invalidContent, canonicalizers, limits, 4096); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("LoadBundle(invalid content) error = %v", err)
	}
	leaf := internalSchema(t, FormatAvro, `"long"`, nil)
	rootWithLeaf := internalSchema(t, FormatAvro, `"bytes"`, []Reference{{Name: "leaf", Fingerprint: leaf.Fingerprint()}})
	withReference, err := NewBundle(rootWithLeaf, []Schema{leaf}, limits, Provenance{Source: "test", Revision: "2"})
	if err != nil {
		t.Fatal(err)
	}
	referenceEncoded, err := withReference.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	invalidReference := []byte(strings.Replace(
		string(referenceEncoded),
		`"name":"leaf","fingerprint":"`+leaf.Fingerprint().String()+`"`,
		`"name":"leaf","fingerprint":"invalid"`,
		1,
	))
	if _, err := LoadBundle(context.Background(), invalidReference, canonicalizers, limits, 4096); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("LoadBundle(invalid reference fingerprint) error = %v", err)
	}
}
