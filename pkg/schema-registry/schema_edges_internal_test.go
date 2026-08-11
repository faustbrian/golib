package schemaregistry

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type canonicalizerFunction func(context.Context, Definition) ([]byte, error)

func (function canonicalizerFunction) Canonicalize(ctx context.Context, definition Definition) ([]byte, error) {
	return function(ctx, definition)
}

func internalSchema(t *testing.T, format Format, content string, references []Reference) Schema {
	t.Helper()
	schema, err := Compile(context.Background(), Definition{Format: format, Content: []byte(content), References: references}, canonicalizerFunction(
		func(_ context.Context, definition Definition) ([]byte, error) { return definition.Content, nil },
	))
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestSchemaBoundaryContracts(t *testing.T) {
	t.Parallel()

	if _, err := ParseFingerprint("sha256:" + strings.Repeat("g", 64)); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("ParseFingerprint(invalid hex) error = %v", err)
	}
	definition := Definition{Format: FormatAvro, Content: []byte(`"string"`)}
	validLimits := CompileLimits{MaxSchemaBytes: 16, MaxCanonicalBytes: 16, MaxReferences: 2, MaxMetadata: 2}
	tests := []struct {
		name       string
		definition Definition
		limits     CompileLimits
		canonical  Canonicalizer
		want       error
	}{
		{"invalid limits", definition, CompileLimits{}, canonicalizerFunction(func(_ context.Context, definition Definition) ([]byte, error) { return definition.Content, nil }), ErrInvalidRequest},
		{"too many references", Definition{Format: FormatAvro, Content: definition.Content, References: []Reference{{}, {}, {}}}, validLimits, canonicalizerFunction(func(_ context.Context, definition Definition) ([]byte, error) { return definition.Content, nil }), ErrLimitExceeded},
		{"too much metadata", Definition{Format: FormatAvro, Content: definition.Content, Metadata: map[string]string{"a": "", "b": "", "c": ""}}, validLimits, canonicalizerFunction(func(_ context.Context, definition Definition) ([]byte, error) { return definition.Content, nil }), ErrLimitExceeded},
		{"reference text too large", Definition{Format: FormatAvro, Content: definition.Content, References: []Reference{{Name: strings.Repeat("r", validLimits.MaxSchemaBytes+1), Fingerprint: Fingerprint{sum: [32]byte{1}}}}}, validLimits, canonicalizerFunction(func(_ context.Context, definition Definition) ([]byte, error) { return definition.Content, nil }), ErrLimitExceeded},
		{"reference subject too large", Definition{Format: FormatAvro, Content: definition.Content, References: []Reference{{Name: "r", Subject: strings.Repeat("s", validLimits.MaxSchemaBytes), Fingerprint: Fingerprint{sum: [32]byte{1}}}}}, validLimits, canonicalizerFunction(func(_ context.Context, definition Definition) ([]byte, error) { return definition.Content, nil }), ErrLimitExceeded},
		{"metadata key too large", Definition{Format: FormatAvro, Content: definition.Content, Metadata: map[string]string{strings.Repeat("k", validLimits.MaxSchemaBytes+1): ""}}, validLimits, canonicalizerFunction(func(_ context.Context, definition Definition) ([]byte, error) { return definition.Content, nil }), ErrLimitExceeded},
		{"metadata text too large", Definition{Format: FormatAvro, Content: definition.Content, Metadata: map[string]string{"key": strings.Repeat("m", validLimits.MaxSchemaBytes)}}, validLimits, canonicalizerFunction(func(_ context.Context, definition Definition) ([]byte, error) { return definition.Content, nil }), ErrLimitExceeded},
		{"unresolved reference", Definition{Format: FormatAvro, Content: definition.Content, References: []Reference{{Name: "a"}}}, validLimits, canonicalizerFunction(func(_ context.Context, definition Definition) ([]byte, error) { return definition.Content, nil }), ErrInvalidSchema},
		{"duplicate reference", Definition{Format: FormatAvro, Content: definition.Content, References: []Reference{{Name: "a", Fingerprint: Fingerprint{sum: [32]byte{1}}}, {Name: "a", Fingerprint: Fingerprint{sum: [32]byte{2}}}}}, validLimits, canonicalizerFunction(func(_ context.Context, definition Definition) ([]byte, error) { return definition.Content, nil }), ErrInvalidSchema},
		{"canonical too large", definition, CompileLimits{MaxSchemaBytes: 16, MaxCanonicalBytes: 1, MaxReferences: 1, MaxMetadata: 1}, canonicalizerFunction(func(context.Context, Definition) ([]byte, error) { return []byte("xx"), nil }), ErrLimitExceeded},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := CompileWithLimits(context.Background(), test.definition, test.canonical, test.limits)
			if !errors.Is(err, test.want) {
				t.Fatalf("CompileWithLimits() error = %v, want %v", err, test.want)
			}
		})
	}
	left := Fingerprint{sum: [32]byte{1}}
	right := Fingerprint{sum: [32]byte{2}}
	schema, err := CompileWithLimits(context.Background(), Definition{
		Format: FormatAvro, Content: definition.Content,
		References: []Reference{{Name: "z", Fingerprint: right}, {Name: "a", Fingerprint: left}},
	}, canonicalizerFunction(func(_ context.Context, definition Definition) ([]byte, error) { return definition.Content, nil }), validLimits)
	if err != nil {
		t.Fatal(err)
	}
	if schema.Definition().References[0].Name != "a" || string(schema.Canonical()) != `"string"` {
		t.Fatalf("compiled schema = %+v", schema.Definition())
	}
	if compareReferenceNames(Reference{Name: "a"}, Reference{Name: "a"}) != 0 {
		t.Fatal("compareReferenceNames(equal) != 0")
	}
	canonical := schema.Canonical()
	canonical[0] = 'x'
	if string(schema.Canonical()) != `"string"` {
		t.Fatal("Canonical() returned aliased bytes")
	}
	exactReference, err := CompileWithLimits(context.Background(), Definition{
		Format: FormatAvro, Content: definition.Content,
		References: []Reference{{Name: strings.Repeat("n", 7), Subject: strings.Repeat("s", 9), Fingerprint: left}},
	}, canonicalizerFunction(func(_ context.Context, definition Definition) ([]byte, error) {
		return definition.Content, nil
	}), validLimits)
	if err != nil || len(exactReference.Definition().References) != 1 {
		t.Fatalf("CompileWithLimits(exact reference text) = (%+v, %v)", exactReference, err)
	}
	exactMetadata, err := CompileWithLimits(context.Background(), Definition{
		Format: FormatAvro, Content: definition.Content,
		Metadata: map[string]string{strings.Repeat("k", 7): strings.Repeat("v", 9)},
	}, canonicalizerFunction(func(_ context.Context, definition Definition) ([]byte, error) {
		return definition.Content, nil
	}), validLimits)
	if err != nil || len(exactMetadata.Definition().Metadata) != 1 {
		t.Fatalf("CompileWithLimits(exact metadata text) = (%+v, %v)", exactMetadata, err)
	}
}
