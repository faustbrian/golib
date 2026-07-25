package validate_test

import (
	"context"
	"strings"
	"testing"

	openapischema "github.com/faustbrian/golib/pkg/openapi/jsonschema"
	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestDocumentValidatesEmbeddedSchemaObjects(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2",
		"info":{"title":"API","version":"1"},
		"paths":{ "/pets":{
			"post":{
				"requestBody":{"content":{"application/json":{
					"schema":{"discriminator":{}}
				}}},
				"responses":{"200":{
					"description":"ok",
					"content":{"application/json":{"schema":true}}
				}}
			}
		}},
		"components":{"schemas":{
			"Pet":{"type":"object"},
			"Broken":{"discriminator":{}}
		}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/paths/~1pets/post/requestBody/content/application~1json/schema/discriminator": false,
		"/components/schemas/Broken/discriminator":                                      false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Source != validate.SourceSchema {
			continue
		}
		if _, exists := want[diagnostic.InstanceLocation]; exists {
			want[diagnostic.InstanceLocation] = true
		}
	}
	for pointer, seen := range want {
		if !seen {
			t.Errorf("missing schema diagnostic at %q: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestDocumentValidatesOpenAPI30SchemaObjects(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.0.4",
		"info":{"title":"API","version":"1"},
		"paths":{},
		"components":{"schemas":{
			"Valid":{"type":"string","nullable":true},
			"Broken":{"type":"string","const":"unsupported"}
		}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Source == validate.SourceSchema &&
			diagnostic.InstanceLocation == "/components/schemas/Broken/const" {
			return
		}
	}
	t.Fatalf("missing OpenAPI 3.0 schema diagnostic: %#v", report.Diagnostics())
}

func TestDocumentTreatsOpenAPI30SchemaReferencesAsReferences(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.0.4",
		"info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{"responses":{"200":{
			"description":"OK","content":{"application/json":{
				"schema":{"$ref":"#/components/schemas/Pet"}
			}}
		}}}}},
		"components":{"schemas":{
			"Pet":{"type":"object"},
			"PetAlias":{"$ref":"#/components/schemas/Pet"}
		}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() {
		t.Fatalf("schema references were rejected: %#v", report.Diagnostics())
	}
}

func TestDocumentValidatesSwagger20SchemaObjects(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0",
		"info":{"title":"API","version":"1"},
		"paths":{},
		"definitions":{
			"Valid":{"type":"string"},
			"Broken":{"oneOf":[{"type":"string"}]},
			"BadDefault":{"type":"integer","default":1.5}
		}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/definitions/Broken/oneOf":       false,
		"/definitions/BadDefault/default": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Source == validate.SourceSchema {
			if _, exists := want[diagnostic.InstanceLocation]; exists {
				want[diagnostic.InstanceLocation] = true
			}
		}
	}
	for pointer, found := range want {
		if !found {
			t.Fatalf("missing Swagger schema diagnostic at %s: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestDocumentUsesExplicitSchemaDialectLoader(t *testing.T) {
	t.Parallel()

	const identifier = "https://schemas.example.test/openapi-dialect"
	document := mustDocument(t, `{
		"openapi":"3.1.2",
		"jsonSchemaDialect":"https://schemas.example.test/openapi-dialect",
		"info":{"title":"API","version":"1"},
		"paths":{},
		"components":{"schemas":{"Pet":{"type":"string"}}}
	}`)
	loader := openapischema.ResourceLoaderFunc(func(
		_ context.Context,
		requested string,
	) ([]byte, error) {
		if requested != identifier {
			t.Fatalf("unexpected resource request %q", requested)
		}
		return []byte(`{
			"$id":"https://schemas.example.test/openapi-dialect",
			"$schema":"https://json-schema.org/draft/2020-12/schema",
			"$vocabulary":{
				"https://json-schema.org/draft/2020-12/vocab/core":true,
				"https://json-schema.org/draft/2020-12/vocab/validation":true
			},
			"type":["object","boolean"]
		}`), nil
	})
	options := validate.DefaultOptions()
	options.SchemaResourceLoader = loader
	report, err := validate.DocumentWithOptions(
		context.Background(), document, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() {
		t.Fatalf("valid custom-dialect document: %#v", report.Diagnostics())
	}
}

func TestDocumentRejectsReadOnlyAndWriteOnlySchemaProperties(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.0.4","info":{"title":"API","version":"1"},
		"paths":{},"components":{"schemas":{"Pet":{
			"type":"object","properties":{
				"secret":{"type":"string","readOnly":true,"writeOnly":true}
			}
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.schema.read-write-only" &&
			diagnostic.InstanceLocation == "/components/schemas/Pet/properties/secret" &&
			diagnostic.Source == validate.SourceSchema {
			return
		}
	}
	t.Fatalf("missing read/write-only diagnostic: %#v", report.Diagnostics())
}

func TestDocumentValidatesDiscriminatorPropertyRequirements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		document string
		code     string
		severity validate.Severity
	}{
		{
			name: "Swagger property must exist",
			document: `{
				"swagger":"2.0","info":{"title":"API","version":"1"},
				"paths":{},"definitions":{"Pet":{
					"type":"object","discriminator":"kind","required":["kind"]
				}}
			}`,
			code:     "openapi.schema.discriminator.property-missing",
			severity: validate.SeverityError,
		},
		{
			name: "Swagger property must be required",
			document: `{
				"swagger":"2.0","info":{"title":"API","version":"1"},
				"paths":{},"definitions":{"Pet":{
					"type":"object","properties":{"kind":{"type":"string"}},
					"discriminator":"kind"
				}}
			}`,
			code:     "openapi.schema.discriminator.not-required",
			severity: validate.SeverityError,
		},
		{
			name: "OpenAPI 3.0.3 property must be required",
			document: `{
				"openapi":"3.0.3","info":{"title":"API","version":"1"},
				"paths":{},"components":{"schemas":{"Pet":{
					"type":"object","properties":{"kind":{"type":"string"}},
					"discriminator":{"propertyName":"kind"}
				}}}
			}`,
			code:     "openapi.schema.discriminator.not-required",
			severity: validate.SeverityError,
		},
		{
			name: "OpenAPI 3.1.2 property must be required",
			document: `{
				"openapi":"3.1.2","info":{"title":"API","version":"1"},
				"paths":{},"components":{"schemas":{"Pet":{
					"type":"object","properties":{"kind":{"type":"string"}},
					"discriminator":{"propertyName":"kind"}
				}}}
			}`,
			code:     "openapi.schema.discriminator.not-required",
			severity: validate.SeverityError,
		},
		{
			name: "OpenAPI 3.0.4 property must be required",
			document: `{
				"openapi":"3.0.4","info":{"title":"API","version":"1"},
				"paths":{},"components":{"schemas":{"Pet":{
					"type":"object","properties":{"kind":{"type":"string"}},
					"discriminator":{"propertyName":"kind"}
				}}}
			}`,
			code:     "openapi.schema.discriminator.not-required",
			severity: validate.SeverityError,
		},
		{
			name: "OpenAPI 3.2 optional property needs a default mapping",
			document: `{
				"openapi":"3.2.0","info":{"title":"API","version":"1"},
				"paths":{},"components":{"schemas":{"Pet":{
					"type":"object","properties":{"kind":{"type":"string"}},
					"discriminator":{"propertyName":"kind"}
				}}}
			}`,
			code:     "openapi.schema.discriminator.default-mapping-missing",
			severity: validate.SeverityError,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, test.document)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == test.code &&
					diagnostic.InstanceLocation == schemaPointer(document.SpecificationVersion().String()) &&
					diagnostic.Severity == test.severity {
					return
				}
			}
			t.Fatalf("missing discriminator diagnostic: %#v", report.Diagnostics())
		})
	}
}

func TestDocumentValidatesInheritedDiscriminatorMappings(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"3.0.4", "3.1.2", "3.2.0"} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{},"components":{"schemas":{
					"Pet":{"type":"object","required":["kind"],
						"properties":{"kind":{"type":"string"}},
						"discriminator":{"propertyName":"kind","mapping":{
							"canine":"Dog","bird":"Bird"
						}}},
					"Cat":{"allOf":[{"$ref":"#/components/schemas/Pet"}]},
					"Dog":{"allOf":[{"$ref":"#/components/schemas/Pet"}]},
					"Bird":{"type":"object"}
				}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			var mappings []validate.Diagnostic
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.schema.discriminator.mapping-unlisted" {
					mappings = append(mappings, diagnostic)
				}
			}
			if len(mappings) != 1 || mappings[0].InstanceLocation !=
				"/components/schemas/Pet/discriminator/mapping/bird" {
				t.Fatalf("mapping diagnostics = %#v", mappings)
			}
		})
	}
}

func TestDocumentRequiresOpenAPIDiscriminatorPropertyName(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{},"components":{"schemas":{"Pet":{
					"type":"object","discriminator":{}
				}}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Source == validate.SourceSchema &&
					diagnostic.InstanceLocation ==
						"/components/schemas/Pet/discriminator" {
					return
				}
			}
			t.Fatalf("missing discriminator propertyName diagnostic: %#v",
				report.Diagnostics())
		})
	}
}

func schemaPointer(version string) string {
	if version == "2.0" {
		return "/definitions/Pet/discriminator"
	}
	return "/components/schemas/Pet/discriminator"
}

func TestDocumentAcceptsOptionalOpenAPI32DiscriminatorWithDefaultMapping(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{},"components":{"schemas":{"Pet":{
			"type":"object","properties":{"kind":{"type":"string"}},
			"discriminator":{"propertyName":"kind","defaultMapping":"Cat"}
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.schema.discriminator.default-mapping-missing" {
			t.Fatalf("valid optional discriminator rejected: %#v", diagnostic)
		}
	}
}

func TestDiscriminatorMappingsMustAppearInAdjacentAlternatives(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		document := mustDocument(t, `{
			"openapi":"`+version+`","info":{"title":"API","version":"1"},
			"paths":{},"components":{"schemas":{
				"Pet":{"type":"object","required":["kind"],
					"properties":{"kind":{"type":"string"}},
					"discriminator":{"propertyName":"kind","mapping":{
						"dog":"Dog","cat":"#/components/schemas/Cat",
						"bird":"Bird"
					}},
					"oneOf":[{"$ref":"#/components/schemas/Dog"}],
					"anyOf":[{"$ref":"#/components/schemas/Cat"}]
				},
				"Dog":{"type":"object"},"Cat":{"type":"object"},
				"Bird":{"type":"object"}
			}}
		}`)
		report, err := validate.Document(context.Background(), document)
		if err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, diagnostic := range report.Diagnostics() {
			if diagnostic.Code ==
				"openapi.schema.discriminator.mapping-unlisted" {
				found++
			}
		}
		if found != 1 {
			t.Errorf("version %s unlisted mappings = %d: %#v",
				version, found, report.Diagnostics())
		}
	}
}

func TestDiscriminatorListsKnownInheritedAlternativesExplicitly(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		document := mustDocument(t, `{
			"openapi":"`+version+`","info":{"title":"API","version":"1"},
			"paths":{},"components":{"schemas":{
				"Pet":{"type":"object","required":["kind"],
					"properties":{"kind":{"type":"string"}},
					"discriminator":{"propertyName":"kind","mapping":{
						"cat":"Cat"
					}},
					"oneOf":[{"$ref":"#/components/schemas/Dog"}]
				},
				"Dog":{"allOf":[{"$ref":"#/components/schemas/Pet"}]},
				"Cat":{"allOf":[{"$ref":"#/components/schemas/Pet"}]}
			}}
		}`)
		report, err := validate.Document(context.Background(), document)
		if err != nil {
			t.Fatalf("OpenAPI %s: %v", version, err)
		}
		want := map[string]bool{
			"openapi.schema.discriminator.mapping-unlisted":     false,
			"openapi.schema.discriminator.alternative-unlisted": false,
		}
		for _, diagnostic := range report.Diagnostics() {
			if _, exists := want[diagnostic.Code]; exists {
				want[diagnostic.Code] = true
			}
		}
		for code, found := range want {
			if !found {
				t.Errorf("OpenAPI %s missing %s: %#v",
					version, code, report.Diagnostics())
			}
		}
	}
}

func TestDocumentValidatesXMLObjectConstraints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		document string
		want     map[string]bool
	}{
		{
			name: "Swagger namespace and wrapping",
			document: `{
				"swagger":"2.0","info":{"title":"API","version":"1"},
				"paths":{},"definitions":{
					"Named":{"type":"string","xml":{"namespace":"relative"}},
					"Wrapped":{"type":"string","xml":{"wrapped":true}}
				}
			}`,
			want: map[string]bool{
				"/definitions/Named/xml/namespace": false,
				"/definitions/Wrapped/xml/wrapped": false,
			},
		},
		{
			name: "OpenAPI 3.1 namespace and wrapping",
			document: `{
				"openapi":"3.1.2","info":{"title":"API","version":"1"},
				"paths":{},"components":{"schemas":{
					"Named":{"type":"string","xml":{"namespace":"relative"}},
					"Wrapped":{"type":"string","xml":{"wrapped":true}}
				}}
			}`,
			want: map[string]bool{
				"/components/schemas/Named/xml/namespace": false,
				"/components/schemas/Wrapped/xml/wrapped": false,
			},
		},
		{
			name: "OpenAPI 3.2 node type conflicts",
			document: `{
				"openapi":"3.2.0","info":{"title":"API","version":"1"},
				"paths":{},"components":{"schemas":{"Pet":{
					"type":"array","xml":{
						"nodeType":"element","attribute":false,"wrapped":true
					}
				}}}
			}`,
			want: map[string]bool{
				"/components/schemas/Pet/xml/attribute": false,
				"/components/schemas/Pet/xml/wrapped":   false,
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, test.document)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code != "openapi.xml.namespace.invalid" &&
					diagnostic.Code != "openapi.xml.wrapped.non-array" &&
					diagnostic.Code != "openapi.xml.node-type.conflict" {
					continue
				}
				if _, exists := test.want[diagnostic.InstanceLocation]; exists {
					test.want[diagnostic.InstanceLocation] = true
				}
			}
			for pointer, found := range test.want {
				if !found {
					t.Errorf("missing XML diagnostic at %s: %#v", pointer, report.Diagnostics())
				}
			}
		})
	}
}

func TestSwaggerDiscouragesRequiredReadOnlyProperties(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"paths":{},"definitions":{"Pet":{
			"type":"object","required":["id","name"],"properties":{
				"id":{"type":"integer","readOnly":true},
				"name":{"type":"string"}
			}
		}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.schema.read-only.required" &&
			diagnostic.InstanceLocation ==
				"/definitions/Pet/properties/id/readOnly" &&
			diagnostic.Severity == validate.SeverityWarning {
			return
		}
	}
	t.Fatalf("missing required read-only warning: %#v", report.Diagnostics())
}

func TestDocumentRejectsRelativeXMLNamespacesAcrossOpenAPIVersions(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{},"components":{"schemas":{"Pet":{
					"type":"string","xml":{"namespace":"relative"}
				}}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.xml.namespace.invalid" &&
					diagnostic.InstanceLocation ==
						"/components/schemas/Pet/xml/namespace" {
					return
				}
			}
			t.Fatalf("missing relative XML namespace diagnostic: %#v",
				report.Diagnostics())
		})
	}
}

func TestDocumentRestrictsXMLWrappingToArraysAcrossOpenAPIVersions(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{},"components":{"schemas":{"Pet":{
					"type":"string","xml":{"wrapped":true}
				}}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.xml.wrapped.non-array" &&
					diagnostic.InstanceLocation ==
						"/components/schemas/Pet/xml/wrapped" {
					return
				}
			}
			t.Fatalf("missing non-array XML wrapping diagnostic: %#v",
				report.Diagnostics())
		})
	}
}

func TestDocumentWarnsWhenArrayXMLNamesAreNotDeclared(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2",
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{},"components":{"schemas":{"Pets":{
					"type":"array","items":{"type":"string"},
					"xml":{"wrapped":true}
				}}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.xml.array-name.missing" &&
					diagnostic.InstanceLocation == "/components/schemas/Pets/xml" &&
					diagnostic.Severity == validate.SeverityWarning {
					return
				}
			}
			t.Fatalf("missing array XML name warning: %#v", report.Diagnostics())
		})
	}
}

func TestDocumentWarnsWhenXMLMetadataIsOutsidePropertySchemas(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2",
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{},"components":{"schemas":{"Pet":{
					"type":"object","xml":{"name":"pet"},"properties":{
						"name":{"type":"string","xml":{"name":"pet-name"}},
						"tags":{"type":"array","items":{
							"type":"string","xml":{"name":"tag"}
						}}
					}
				}}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]bool{
				"/components/schemas/Pet/xml":                       false,
				"/components/schemas/Pet/properties/tags/items/xml": false,
			}
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code != "openapi.xml.non-property" {
					continue
				}
				if diagnostic.InstanceLocation ==
					"/components/schemas/Pet/properties/name/xml" {
					t.Fatalf("property XML metadata was rejected: %#v", diagnostic)
				}
				if _, exists := want[diagnostic.InstanceLocation]; exists &&
					diagnostic.Severity == validate.SeverityWarning {
					want[diagnostic.InstanceLocation] = true
				}
			}
			for pointer, found := range want {
				if !found {
					t.Errorf("missing XML placement warning at %s: %#v",
						pointer, report.Diagnostics())
				}
			}
		})
	}
}

func TestSwaggerXMLAppliesOnlyToPropertySchemas(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"paths":{},"definitions":{
			"Root":{"type":"object","xml":{"name":"root"},"properties":{
				"name":{"type":"string","xml":{"name":"pet-name"}},
				"tags":{"type":"array","items":{
					"type":"string","xml":{"name":"tag"}
				}}
			}}
		}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/definitions/Root/xml":                       false,
		"/definitions/Root/properties/tags/items/xml": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.swagger.xml.non-property" {
			continue
		}
		if _, exists := want[diagnostic.InstanceLocation]; exists {
			want[diagnostic.InstanceLocation] = true
		} else {
			t.Errorf("unexpected XML placement diagnostic: %#v", diagnostic)
		}
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing XML placement diagnostic at %s: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestDocumentAcceptsArrayUnionXMLWrappingAndAbsoluteIRI(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{},"components":{"schemas":{"Items":{
			"type":["array","null"],
			"xml":{"namespace":"https://例え.example/名前","wrapped":true}
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.xml.namespace.invalid" ||
			diagnostic.Code == "openapi.xml.wrapped.non-array" {
			t.Fatalf("valid XML metadata rejected: %#v", diagnostic)
		}
	}
}

func TestOpenAPI32ValidatesXMLNodeNames(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{"responses":{"200":{
			"description":"ok","content":{
				"application/xml":{"schema":{
					"type":"string","xml":{"nodeType":"element"}
				}},
				"text/xml":{"schema":{
					"type":"string","xml":{"nodeType":"text","name":"ignored"}
				}}
			}
		}}}}},
		"components":{"schemas":{
			"Pet":{"type":"object","xml":{"nodeType":"element"},
				"properties":{"id":{"type":"string",
					"xml":{"nodeType":"attribute"}}}}
		}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]validate.Severity{
		"/paths/~1pets/get/responses/200/content/application~1xml/schema/xml": validate.SeverityError,
		"/paths/~1pets/get/responses/200/content/text~1xml/schema/xml/name":   validate.SeverityWarning,
	}
	for _, diagnostic := range report.Diagnostics() {
		if severity, exists := want[diagnostic.InstanceLocation]; exists &&
			diagnostic.Severity == severity {
			delete(want, diagnostic.InstanceLocation)
		}
		if diagnostic.Code == "openapi.xml.name.missing" &&
			strings.HasPrefix(diagnostic.InstanceLocation, "/components/schemas/Pet") {
			t.Fatalf("inferred XML node name was rejected: %#v", diagnostic)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing XML node name diagnostics: %#v", want)
	}
}
