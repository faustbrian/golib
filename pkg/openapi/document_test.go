package openapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/oas30"
	"github.com/faustbrian/golib/pkg/openapi/oas31"
	"github.com/faustbrian/golib/pkg/openapi/oas32"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/swagger20"
)

func TestDecodeSelectsLosslessVersionSpecificModels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		version string
		assert  func(*testing.T, openapi.Document)
	}{
		{
			name:    "OpenAPI 3.2",
			raw:     `{"openapi":"3.2.0","info":{"title":"API","summary":null,"version":"1","x-exact":-0.0e+00},"components":{"schemas":{"Anything":true}},"tags":[{"name":"api","parent":"root","kind":"nav"}],"x-root":{"enabled":true}}`,
			version: "3.2.0",
			assert: func(t *testing.T, document openapi.Document) {
				t.Helper()
				typed, ok := document.(oas32.Document)
				if !ok {
					t.Fatalf("document type = %T", document)
				}
				info, ok := typed.Info().Value()
				if !ok {
					t.Fatal("Info() value missing")
				}
				if title, ok := info.Title().Value(); !ok || title != "API" {
					t.Fatalf("Info.Title() = %q, %t", title, ok)
				}
				if summary := info.Summary(); !summary.Present() || !summary.Null() {
					t.Fatal("Info.Summary() did not preserve explicit null")
				}
				if info.Extensions().Names()[0] != "x-exact" || typed.Extensions().Names()[0] != "x-root" {
					t.Fatal("extensions did not preserve names and placement")
				}
				components, _ := typed.Components().Value()
				schemas, _ := components.Schemas().Value()
				schema, _ := schemas.Lookup("Anything")
				if schema.Raw().Kind() != jsonvalue.BooleanKind {
					t.Fatalf("boolean Schema Object kind = %v", schema.Raw().Kind())
				}
				tags, _ := typed.Tags().Value()
				tag, _ := tags.At(0)
				if kind, ok := tag.Kind().Value(); !ok || kind != "nav" {
					t.Fatalf("Tag.Kind() = %q, %t", kind, ok)
				}
			},
		},
		{
			name:    "OpenAPI 3.1 patch compatibility",
			raw:     `{"openapi":"3.1.0","info":{"title":"API","summary":"summary","version":"1"},"paths":{}}`,
			version: "3.1.0",
			assert: func(t *testing.T, document openapi.Document) {
				t.Helper()
				typed, ok := document.(oas31.Document)
				if !ok {
					t.Fatalf("document type = %T", document)
				}
				info, _ := typed.Info().Value()
				if summary, ok := info.Summary().Value(); !ok || summary != "summary" {
					t.Fatalf("Info.Summary() = %q, %t", summary, ok)
				}
			},
		},
		{
			name:    "OpenAPI 3.0",
			raw:     `{"openapi":"3.0.4","info":{"title":"API","version":"1"},"paths":{}}`,
			version: "3.0.4",
			assert: func(t *testing.T, document openapi.Document) {
				t.Helper()
				if _, ok := document.(oas30.Document); !ok {
					t.Fatalf("document type = %T", document)
				}
			},
		},
		{
			name:    "Swagger 2.0",
			raw:     `{"swagger":"2.0","info":{"title":"API","version":"1"},"paths":{}}`,
			version: "2.0",
			assert: func(t *testing.T, document openapi.Document) {
				t.Helper()
				if _, ok := document.(swagger20.Document); !ok {
					t.Fatalf("document type = %T", document)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			value := parseJSONValue(t, test.raw)
			document, err := openapi.Decode(value)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if got := document.SpecificationVersion().String(); got != test.version {
				t.Fatalf("SpecificationVersion() = %q, want %q", got, test.version)
			}
			roundTrip, err := json.Marshal(document.Raw())
			if err != nil {
				t.Fatalf("Marshal(Raw()) error = %v", err)
			}
			if got := string(roundTrip); got != test.raw {
				t.Fatalf("lossless round trip = %s, want %s", got, test.raw)
			}
			test.assert(t, document)
		})
	}
}

func TestDecodePreservesCommonMarkDescriptionSource(t *testing.T) {
	t.Parallel()

	const description = "**strong** [link](https://example.test) `code`\n\n- item"
	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			descriptionJSON, err := json.Marshal(description)
			if err != nil {
				t.Fatal(err)
			}
			raw := `{"openapi":"` + version + `","info":{"title":"API",` +
				`"version":"1","description":` + string(descriptionJSON) + `},"paths":{}}`
			document, err := openapi.Decode(parseJSONValue(t, raw))
			if err != nil {
				t.Fatal(err)
			}
			info, exists := document.Raw().Lookup("info")
			if !exists {
				t.Fatal("decoded document has no info object")
			}
			value, exists := info.Lookup("description")
			if got, ok := value.Text(); !exists || !ok || got != description {
				t.Fatalf("description = %q, %t, %t", got, exists, ok)
			}
			encoded, err := json.Marshal(document.Raw())
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != raw {
				t.Fatalf("description round trip = %s", encoded)
			}
		})
	}
}

func TestDecodePreservesToolMetadataAndUndeclaredTagOrder(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		document, err := openapi.Decode(parseJSONValue(t, `{
			"openapi":"`+version+`",
			"info":{"title":"API","version":"1","x-tool":"metadata"},
			"tags":[{"name":"first"},{"name":"second"}],
			"paths":{"/pets":{"get":{"tags":["second","undeclared","first"],
				"responses":{"200":{"description":"ok"}}}}}
		}`))
		if err != nil {
			t.Fatal(err)
		}
		info, _ := document.Raw().Lookup("info")
		tool, _ := info.Lookup("x-tool")
		if value, ok := tool.Text(); !ok || value != "metadata" {
			t.Fatalf("version %s tool metadata = %#v", version, tool)
		}
		declared, _ := document.Raw().Lookup("tags")
		declaredTags, _ := declared.Elements()
		first, _ := declaredTags[0].Lookup("name")
		second, _ := declaredTags[1].Lookup("name")
		firstName, _ := first.Text()
		secondName, _ := second.Text()
		if firstName != "first" || secondName != "second" {
			t.Fatalf("version %s declared tags changed order", version)
		}
		paths, _ := document.Raw().Lookup("paths")
		pets, _ := paths.Lookup("/pets")
		get, _ := pets.Lookup("get")
		operationTags, _ := get.Lookup("tags")
		elements, _ := operationTags.Elements()
		want := []string{"second", "undeclared", "first"}
		for index, element := range elements {
			name, _ := element.Text()
			if name != want[index] {
				t.Fatalf("version %s operation tag %d = %q",
					version, index, name)
			}
		}
	}
}

func TestDecodeRequiresEverySupportedExactVersionMarker(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0",
		"3.0.1",
		"3.0.2",
		"3.0.3",
		"3.0.4",
		"3.1.0",
		"3.1.1",
		"3.1.2",
		"3.2.0",
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			raw := parseJSONValue(t, `{"openapi":"`+version+`"}`)
			document, err := openapi.Decode(raw)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if got := document.SpecificationVersion().String(); got != version {
				t.Fatalf("SpecificationVersion() = %q, want %q", got, version)
			}
		})
	}

	t.Run("2.0", func(t *testing.T) {
		t.Parallel()

		raw := parseJSONValue(t, `{"swagger":"2.0"}`)
		document, err := openapi.Decode(raw)
		if err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if got := document.SpecificationVersion().String(); got != "2.0" {
			t.Fatalf("SpecificationVersion() = %q, want %q", got, "2.0")
		}
	})
}

func TestDecodePreservesInvalidNestedTypedFields(t *testing.T) {
	t.Parallel()

	document, err := openapi.Decode(parseJSONValue(t, `{"openapi":"3.2.0","info":{"title":false,"version":"1"},"paths":{}}`))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	typed := document.(oas32.Document)
	info, _ := typed.Info().Value()
	field := info.Title()
	if !field.Present() || field.Valid() {
		t.Fatalf("invalid title present=%t valid=%t", field.Present(), field.Valid())
	}
	if raw, ok := field.Raw(); !ok || raw.Kind() != jsonvalue.BooleanKind {
		t.Fatalf("invalid title raw kind=%v ok=%t", raw.Kind(), ok)
	}
}

func TestDecodeRejectsAmbiguousAndUnsupportedVersions(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		`null`,
		`{"openapi":true}`,
		`{"openapi":"3.2.1"}`,
		`{"openapi":"3.2.0","swagger":"2.0"}`,
		`{"info":{}}`,
	} {
		_, err := openapi.Decode(parseJSONValue(t, raw))
		if !errors.Is(err, openapi.ErrInvalidDocument) {
			t.Errorf("Decode(%s) error = %v, want ErrInvalidDocument", raw, err)
		}
	}
}

func TestParseDocumentReportsRepresentationAndSelectionFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		parse func(string) (openapi.Document, error)
		raw   string
		want  error
	}{
		{
			name: "malformed JSON",
			parse: func(raw string) (openapi.Document, error) {
				return openapi.ParseJSON(context.Background(), strings.NewReader(raw), parse.DefaultLimits())
			},
			raw:  `[`,
			want: parse.ErrInvalidJSON,
		},
		{
			name: "unselectable JSON",
			parse: func(raw string) (openapi.Document, error) {
				return openapi.ParseJSON(context.Background(), strings.NewReader(raw), parse.DefaultLimits())
			},
			raw:  `{}`,
			want: openapi.ErrInvalidDocument,
		},
		{
			name: "malformed YAML",
			parse: func(raw string) (openapi.Document, error) {
				return openapi.ParseYAML(context.Background(), strings.NewReader(raw), parse.DefaultLimits())
			},
			raw:  `[`,
			want: parse.ErrInvalidYAML,
		},
		{
			name: "unselectable YAML",
			parse: func(raw string) (openapi.Document, error) {
				return openapi.ParseYAML(context.Background(), strings.NewReader(raw), parse.DefaultLimits())
			},
			raw:  `{}`,
			want: openapi.ErrInvalidDocument,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := test.parse(test.raw); !errors.Is(err, test.want) {
				t.Fatalf("parse error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestParseJSONAndYAMLShareVersionedDocumentSemantics(t *testing.T) {
	t.Parallel()

	jsonDocument, err := openapi.ParseJSON(
		context.Background(),
		strings.NewReader(`{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{}}`),
		parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("ParseJSON() error = %v", err)
	}
	yamlDocument, err := openapi.ParseYAML(
		context.Background(),
		strings.NewReader("openapi: 3.2.0\ninfo:\n  title: API\n  version: '1'\npaths: {}\n"),
		parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("ParseYAML() error = %v", err)
	}
	jsonRaw, _ := json.Marshal(jsonDocument.Raw())
	yamlRaw, _ := json.Marshal(yamlDocument.Raw())
	if string(jsonRaw) != string(yamlRaw) {
		t.Fatalf("JSON/YAML semantics differ:\nJSON %s\nYAML %s", jsonRaw, yamlRaw)
	}
}

func parseJSONValue(t *testing.T, raw string) jsonvalue.Value {
	t.Helper()
	value, err := parse.JSON(context.Background(), strings.NewReader(raw), parse.DefaultLimits())
	if err != nil {
		t.Fatalf("parse.JSON() error = %v", err)
	}
	return value
}
