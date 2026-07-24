package specification

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"
)

func TestManifestRejectsEveryInvalidShape(t *testing.T) {
	t.Parallel()

	if err := VerifyPinnedInputs(nil); !errors.Is(err, ErrManifestRead) {
		t.Fatalf("nil filesystem error = %v", err)
	}
	tests := []struct {
		name     string
		manifest string
		files    fstest.MapFS
		want     error
	}{
		{name: "empty", manifest: `{}`, want: ErrManifestInvalid},
		{name: "invalid path", manifest: manifestJSON("../../../schema.json", strings.Repeat("a", 64)), want: ErrManifestInvalid},
		{name: "invalid digest length", manifest: manifestJSON("schema.json", "aa"), want: ErrManifestInvalid},
		{name: "invalid digest encoding", manifest: manifestJSON("schema.json", strings.Repeat("z", 64)), want: ErrManifestInvalid},
		{name: "missing input", manifest: manifestJSON("schema.json", strings.Repeat("a", 64)), want: ErrManifestRead},
		{name: "duplicate", manifest: `{"openrpc":{"files":[{"path":"schema.json","sha256":"44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"},{"path":"schema.json","sha256":"44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"}]}}`, files: fstest.MapFS{"specification/openrpc-1.4.1/schema.json": {Data: []byte(`{}`)}}, want: ErrManifestInvalid},
		{name: "meta dialect path only", manifest: `{"jsonSchema":{"openrpcMetaDialect":{"path":"schema.json"}}}`, want: ErrManifestInvalid},
		{name: "meta dialect digest only", manifest: `{"jsonSchema":{"openrpcMetaDialect":{"sha256":"44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"}}}`, want: ErrManifestInvalid},
	}
	for _, test := range tests {
		files := test.files
		if files == nil {
			files = fstest.MapFS{}
		}
		files["specification/manifest.json"] = &fstest.MapFile{Data: []byte(test.manifest)}
		if err := VerifyPinnedInputs(files); !errors.Is(err, test.want) {
			t.Errorf("%s error = %v", test.name, err)
		}
	}
}

func TestManifestAcceptsMetaDialectAsOnlyPinnedInput(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"specification/manifest.json": {Data: []byte(`{"jsonSchema":{"openrpcMetaDialect":{"path":"schema.json","sha256":"44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"}}}`)},
		"specification/schema.json":   {Data: []byte(`{}`)},
	}
	if err := VerifyPinnedInputs(files); err != nil {
		t.Fatal(err)
	}
}

func TestMatrixRejectsMalformedInputsAndEvidence(t *testing.T) {
	t.Parallel()

	if _, _, err := GenerateMatrices("MUST work.", []byte(`{`)); err == nil {
		t.Fatal("invalid schema JSON succeeded")
	}
	inventory := []byte("object\tfield\tshape\trequired\tnullable\tdefault\textensions\tunknownFields\tmodel\tvalidation\tevidence\tstatus\n#\tname\tstring\ttrue\tfalse\t\tfalse\treject\t\t\t\tunimplemented\n")
	reviewHeader := "object\tmodel\tvalidation\tevidence\tstatus\n"
	validReview := reviewHeader + "#\ta\tb\tc\tcomplete\n"
	for _, test := range []struct {
		inventory []byte
		review    []byte
	}{
		{inventory: inventory, review: []byte(reviewHeader + "#\ta\tb\tc\tcomplete\n#\ta\tb\tc\tcomplete\n")},
		{inventory: []byte("invalid\n"), review: []byte(validReview)},
		{inventory: []byte("object\tfield\tshape\trequired\tnullable\tdefault\textensions\tunknownFields\tmodel\tvalidation\tevidence\tstatus\nshort\n"), review: []byte(validReview)},
		{inventory: inventory, review: []byte(validReview + "unused\ta\tb\tc\tcomplete\n")},
	} {
		if _, err := ApplyFieldEvidence(test.inventory, test.review); err == nil {
			t.Fatalf("ApplyFieldEvidence accepted inventory %q review %q", test.inventory, test.review)
		}
	}
}

func TestSchemaClassificationCoversEveryShape(t *testing.T) {
	t.Parallel()

	if additionalPropertiesPolicy(map[string]any{"additionalProperties": map[string]any{}}) != "schema" {
		t.Fatal("schema additionalProperties was not classified")
	}
	shapes := []struct {
		schema map[string]any
		want   string
	}{
		{schema: nil, want: "any"},
		{schema: map[string]any{"$ref": "#/value"}, want: "#/value"},
		{schema: map[string]any{"oneOf": []any{}}, want: "oneOf"},
	}
	for _, shape := range shapes {
		if got := schemaShape(shape.schema); got != shape.want {
			t.Errorf("schemaShape(%#v) = %q", shape.schema, got)
		}
	}
	for _, schema := range []map[string]any{
		{"$ref": "#/value"},
		{"properties": map[string]any{}},
		{"type": 1},
		{"type": []any{"string"}},
	} {
		if isNullable(schema) {
			t.Errorf("isNullable(%#v) = true", schema)
		}
	}
	if isNullable(map[string]any{"type": []any{"string", "null"}}) != true {
		t.Fatal("nullable union was not classified")
	}
	if jsonValue(make(chan int)) != "" {
		t.Fatal("unencodable value produced JSON")
	}
}

func TestMatrixPreservesSentenceAndNestedPropertyBoundaries(t *testing.T) {
	t.Parallel()

	normative, fields, err := GenerateMatrices(
		"Clients MUST wait. Servers SHOULD reply! Implementations MAY retry? End.",
		[]byte(`{"properties":{"parent":{"properties":{"child":{"type":"string"}},"required":["child"]}}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, sentence := range []string{
		"Clients MUST wait.",
		"Servers SHOULD reply!",
		"Implementations MAY retry?",
	} {
		if strings.Count(string(normative), sentence) != 1 {
			t.Errorf("normative matrix does not contain %q exactly once:\n%s", sentence, normative)
		}
	}
	if strings.Count(string(fields), "#/properties/parent\tchild\t") != 1 {
		t.Fatalf("nested field was not emitted exactly once:\n%s", fields)
	}
}

func manifestJSON(file string, digest string) string {
	return `{"openrpc":{"files":[{"path":"` + file + `","sha256":"` + digest + `"}]}}`
}
