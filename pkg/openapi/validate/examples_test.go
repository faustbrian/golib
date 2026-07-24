package validate_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/reference"
	"github.com/faustbrian/golib/pkg/openapi/validate"
)

type strictExampleCodec struct{}

func (strictExampleCodec) Encode(
	_ context.Context,
	value jsonvalue.Value,
) ([]byte, error) {
	if text, valid := value.Text(); valid && text == "unencodable" {
		return nil, errors.New("unsupported value")
	}
	return value.MarshalJSON()
}

func (strictExampleCodec) Decode(
	ctx context.Context,
	data []byte,
) (jsonvalue.Value, error) {
	return parse.JSON(ctx, strings.NewReader(string(data)), parse.DefaultLimits())
}

func TestDocumentValidatesExampleValueSources(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/items":{"get":{"responses":{"200":{
			"description":"ok","content":{"application/json":{"examples":{
				"Nested":{"value":{},"externalValue":"example.json"}
			}}}
		}}}}},
		"components":{"examples":{
			"DataConflict":{"value":{},"dataValue":{}},
			"SerializedConflict":{"serializedValue":"one","externalValue":"example.txt"},
			"ValidPair":{"dataValue":{},"serializedValue":"{}"}
		}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/components/examples/DataConflict":                                          false,
		"/components/examples/SerializedConflict":                                    false,
		"/paths/~1items/get/responses/200/content/application~1json/examples/Nested": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.example.value.conflict" {
			continue
		}
		if diagnostic.InstanceLocation == "/components/examples/ValidPair" {
			t.Fatalf("valid data and serialized value rejected: %#v", diagnostic)
		}
		if _, exists := want[diagnostic.InstanceLocation]; exists {
			want[diagnostic.InstanceLocation] = true
		}
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing example conflict at %s: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestDocumentRejectsSingularAndNamedExamplesTogether(t *testing.T) {
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
				"paths":{},"components":{
					"parameters":{"Limit":{"name":"limit","in":"query",
						"schema":{"type":"integer"},"example":1,
						"examples":{"one":{"value":1}}}},
					"headers":{"Rate":{"schema":{"type":"integer"},
						"example":1,"examples":{"one":{"value":1}}}},
					"requestBodies":{"Body":{"content":{"application/json":{
						"schema":{"type":"object"},"example":{},
						"examples":{"one":{"value":{}}}
					}}}}
				}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]string{
				"/components/parameters/Limit":                             "openapi.parameter.examples.conflict",
				"/components/headers/Rate":                                 "openapi.header.examples.conflict",
				"/components/requestBodies/Body/content/application~1json": "openapi.media-type.examples.conflict",
			}
			found := make(map[string]bool, len(want))
			for _, diagnostic := range report.Diagnostics() {
				code, exists := want[diagnostic.InstanceLocation]
				if !exists || diagnostic.Code != code {
					continue
				}
				found[diagnostic.InstanceLocation] = true
			}
			for pointer := range want {
				if !found[pointer] {
					t.Errorf("missing conflict at %s: %#v", pointer, report.Diagnostics())
				}
			}
		})
	}
}

func TestDocumentValidatesLegacyExampleValueSources(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.0.4","info":{"title":"API","version":"1"},
		"paths":{},"components":{"examples":{"Conflict":{
			"value":{},"externalValue":"example.json"
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.example.value.conflict" &&
			diagnostic.InstanceLocation == "/components/examples/Conflict" {
			return
		}
	}
	t.Fatalf("missing legacy example conflict: %#v", report.Diagnostics())
}

func TestSwaggerResponseExampleMediaTypesMatchEffectiveProduces(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"produces":["application/json"],
		"responses":{"Shared":{"description":"ok","examples":{"image/png":"data"}}},
		"paths":{
			"/inherited":{"get":{"responses":{"200":{
				"description":"ok","examples":{"application/json":{}}
			}}}},
			"/overridden":{"get":{
				"produces":["text/plain"],
				"responses":{"200":{"description":"ok","examples":{
					"text/plain":"ok","application/json":{}
				}}}
			}},
			"/referenced":{"get":{"responses":{"200":{"$ref":"#/responses/Shared"}}}}
		}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/paths/~1overridden/get/responses/200/examples/application~1json": false,
		"/paths/~1referenced/get/responses/200/$ref":                       false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.swagger.example.media-type" {
			continue
		}
		if _, exists := want[diagnostic.InstanceLocation]; !exists {
			t.Fatalf("unexpected example media diagnostic: %#v", diagnostic)
		}
		want[diagnostic.InstanceLocation] = true
	}
	for pointer, found := range want {
		if !found {
			t.Fatalf("missing Swagger example media diagnostic at %s: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestSwaggerResponseExamplesMatchResponseSchemas(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"produces":["application/json"],"paths":{"/pets":{"get":{
			"responses":{
				"200":{"description":"ok","schema":{"type":"integer"},
					"examples":{"application/json":"not an integer"}},
				"201":{"description":"ok","schema":{"type":"integer"},
					"examples":{"application/json":2}}
			}
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.swagger.example.schema" &&
			diagnostic.InstanceLocation ==
				"/paths/~1pets/get/responses/200/examples/application~1json" &&
			diagnostic.Severity == validate.SeverityWarning {
			return
		}
	}
	t.Fatalf("missing response example schema warning: %#v", report.Diagnostics())
}

func TestSwaggerExternalResponseExamplesMatchProducesAndSchema(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"produces":["application/json"],"paths":{
			"/pets":{"get":{
				"responses":{"200":{"$ref":"responses.json#/responses/Bad"}}
			}},
			"/external":{"$ref":"path-item.json#/paths/~1external"}
		}
	}`)
	external := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"Responses","version":"1"},
		"paths":{},"responses":{"Bad":{"description":"bad",
			"schema":{"type":"integer"},
			"examples":{"text/plain":"not an integer"}
		}}
	}`).Raw()
	externalPathItem := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"Path Item","version":"1"},
		"paths":{"/external":{"get":{"responses":{"200":{
			"description":"bad","schema":{"type":"integer"},
			"examples":{"text/plain":"not an integer"}
		}}}}}
	}`).Raw()
	options := validate.DefaultOptions()
	options.ReferenceResourceURI = "https://api.example.test/swagger.json"
	options.ReferenceResolver = reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		switch identifier {
		case "https://api.example.test/responses.json":
			return reference.Resource{
				RetrievalURI: identifier,
				Root:         external,
			}, nil
		case "https://api.example.test/path-item.json":
			return reference.Resource{
				RetrievalURI: identifier,
				Root:         externalPathItem,
			}, nil
		default:
			return reference.Resource{}, fmt.Errorf(
				"unexpected resolved identifier %q", identifier,
			)
		}
	})
	report, err := validate.DocumentWithOptions(
		context.Background(), document, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"openapi.swagger.example.media-type": false,
		"openapi.swagger.example.schema":     false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if _, tracked := want[diagnostic.Code]; tracked {
			want[diagnostic.Code] = true
		}
	}
	for code, found := range want {
		if !found {
			t.Errorf("missing %s: %#v", code, report.Diagnostics())
		}
	}
}

func TestSwaggerFileResponsesRecommendProduces(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"paths":{
			"/missing":{"get":{"responses":{"200":{
				"description":"ok","schema":{"type":"file"}
			}}}},
			"/present":{"get":{"produces":["application/octet-stream"],
				"responses":{"200":{"description":"ok","schema":{"type":"file"}}}
			}}
		}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.swagger.response.file.produces" {
			continue
		}
		count++
		if diagnostic.InstanceLocation !=
			"/paths/~1missing/get/responses/200/schema" {
			t.Errorf("unexpected file response warning: %#v", diagnostic)
		}
	}
	if count != 1 {
		t.Fatalf("file response warnings = %d: %#v", count, report.Diagnostics())
	}
}

func TestOpenAPIExamplesRecommendSchemaConformance(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.0.4","info":{"title":"API","version":"1"},
		"paths":{},"components":{
			"schemas":{"Integer":{"type":"integer"}},
			"examples":{"Bad":{"value":"many"}},
			"parameters":{"Limit":{"name":"limit","in":"query",
				"schema":{"$ref":"#/components/schemas/Integer"},"example":"many"}},
			"headers":{"Rate":{"schema":{"type":"integer"},
				"examples":{"bad":{"value":"many"},"good":{"value":2}}},
				"Referenced":{"schema":{"type":"integer"},
				"examples":{"bad":{"$ref":"#/components/examples/Bad"}}}},
			"requestBodies":{"Body":{"content":{"application/json":{
				"schema":{"type":"object","required":["id"],
					"properties":{"id":{"type":"integer"}}},
				"example":{"id":"one"}
			},"application/problem+json":{
				"schema":{"type":"object","required":["id"],
					"properties":{"id":{"type":"integer"}}},"examples":{
					"bad":{"value":{}},"external":{"externalValue":"example.json"}
				}
			}}}}
		}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/components/parameters/Limit/example":                                                false,
		"/components/headers/Rate/examples/bad/value":                                         false,
		"/components/headers/Referenced/examples/bad/$ref":                                    false,
		"/components/requestBodies/Body/content/application~1json/example":                    false,
		"/components/requestBodies/Body/content/application~1problem+json/examples/bad/value": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.example.schema" {
			continue
		}
		if diagnostic.Severity != validate.SeverityWarning {
			t.Errorf("example diagnostic is not a warning: %#v", diagnostic)
		}
		if _, exists := want[diagnostic.InstanceLocation]; !exists {
			t.Errorf("unexpected example schema warning: %#v", diagnostic)
			continue
		}
		want[diagnostic.InstanceLocation] = true
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing example schema warning at %s: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestExampleObjectSchemaWarningsCoverEarlierOpenAPIVersions(t *testing.T) {
	t.Parallel()

	const template = `{
		"openapi":"VERSION","info":{"title":"API","version":"1"},
		"paths":{},"components":{"headers":{"Rate":{
			"schema":{"type":"integer"},
			"examples":{"bad":{"value":"many"}}
		}}}
	}`
	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.1.0",
	} {
		document := mustDocument(t, strings.Replace(template, "VERSION", version, 1))
		report, err := validate.Document(context.Background(), document)
		if err != nil {
			t.Fatalf("OpenAPI %s: %v", version, err)
		}
		found := false
		for _, diagnostic := range report.Diagnostics() {
			if diagnostic.Code == "openapi.example.schema" &&
				diagnostic.InstanceLocation ==
					"/components/headers/Rate/examples/bad/value" {
				found = true
			}
		}
		if !found {
			t.Fatalf("OpenAPI %s diagnostics = %#v", version, report.Diagnostics())
		}
	}
}

func TestExternalSchemasDriveExampleValidation(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		field := `"value":"many"`
		if version == "3.2.0" {
			field = `"dataValue":"many"`
		}
		document := mustDocument(t, `{
			"openapi":"`+version+`","info":{"title":"API","version":"1"},
			"paths":{},"components":{
				"headers":{"Rate":{
					"schema":{"$ref":"schemas.json#/components/schemas/Integer"},
					"examples":{"bad":{`+field+`}}
				}},
				"parameters":{"Filter":{"name":"filter","in":"query",
					"style":"deepObject",
					"schema":{"$ref":"schemas.json#/components/schemas/Object"},
					"examples":{"bad":{`+field+`}}
				}}
			}
		}`)
		external := mustDocument(t, `{
			"openapi":"`+version+`","info":{"title":"Schemas","version":"1"},
			"paths":{},"components":{"schemas":{
				"Integer":{"type":"integer"},
				"Object":{"type":"object","properties":{"id":{"type":"integer"}}}
			}}
		}`).Raw()
		options := validate.DefaultOptions()
		options.ReferenceResourceURI = "https://api.example.test/openapi.json"
		options.ReferenceResolver = reference.ResolverFunc(func(
			_ context.Context,
			identifier string,
		) (reference.Resource, error) {
			if identifier != "https://api.example.test/schemas.json" {
				return reference.Resource{}, fmt.Errorf(
					"unexpected resolved identifier %q", identifier,
				)
			}
			return reference.Resource{RetrievalURI: identifier, Root: external}, nil
		})
		report, err := validate.DocumentWithOptions(
			context.Background(), document, options,
		)
		if err != nil {
			t.Fatalf("OpenAPI %s: %v", version, err)
		}
		want := map[string]bool{
			"openapi.example.schema":                  false,
			"openapi.example.parameter-serialization": false,
		}
		for _, diagnostic := range report.Diagnostics() {
			if _, tracked := want[diagnostic.Code]; tracked {
				want[diagnostic.Code] = true
			}
		}
		for code, found := range want {
			if !found {
				t.Errorf(
					"OpenAPI %s missing %s: %#v",
					version,
					code,
					report.Diagnostics(),
				)
			}
		}
	}
}

func TestOpenAPI32ParameterExternalExamplesUseDeclaredSerialization(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{},"components":{"parameters":{"Tags":{
			"name":"tags","in":"query","schema":{"type":"array","items":{"type":"string"}},
			"examples":{
				"matching":{"dataValue":["a","b"],"serializedValue":"tags=a&tags=b"},
				"mismatch":{"dataValue":["a","b"],"serializedValue":"tags=a&tags=c"},
				"external":{"dataValue":["a","b"],"externalValue":"examples/tags.txt"},
				"invalid":{"externalValue":"examples/invalid.txt"},
				"missing":{"externalValue":"examples/missing.txt"},
				"referenced":{"$ref":"#/components/examples/Serialized"},
				"externalReferenced":{"$ref":"#/components/examples/External"},
				"unresolvedReference":{"$ref":"#/components/examples/Missing"},
				"nonString":{"externalValue":1},
				"invalidURI":{"externalValue":"%zz"},
				"large":{"externalValue":"examples/large.txt"},
				"exact":{"externalValue":"examples/exact.txt"}
			}
		}},"examples":{
			"Serialized":{"dataValue":["a","b"],"serializedValue":"tags=a&tags=c"},
			"External":{"externalValue":"examples/invalid.txt"}
		}}
	}`)
	options := validate.DefaultOptions()
	options.ReferenceResourceURI = "https://api.example.test/openapi.json"
	options.MaxExternalExampleBytes = 20
	options.ExternalExampleResolver = validate.ExternalExampleResolverFunc(
		func(_ context.Context, identifier string) (
			validate.ExternalExampleResource,
			error,
		) {
			switch identifier {
			case "https://api.example.test/examples/tags.txt":
				return validate.ExternalExampleResource{Data: []byte("tags=a&tags=b")}, nil
			case "https://api.example.test/examples/invalid.txt":
				return validate.ExternalExampleResource{Data: []byte("not-tags")}, nil
			case "https://api.example.test/examples/large.txt":
				return validate.ExternalExampleResource{Data: []byte("123456789012345678901")}, nil
			case "https://api.example.test/examples/exact.txt":
				return validate.ExternalExampleResource{Data: []byte("tags=123456789012345")}, nil
			default:
				return validate.ExternalExampleResource{}, errors.New("missing")
			}
		},
	)
	report, err := validate.DocumentWithOptions(
		context.Background(), document, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"openapi.example.parameter-data-mismatch":   false,
		"openapi.example.parameter-serialization":   false,
		"openapi.example.external-value.unresolved": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if _, tracked := want[diagnostic.Code]; tracked {
			want[diagnostic.Code] = true
		}
	}
	for code, found := range want {
		if !found {
			t.Errorf("missing %s: %#v", code, report.Diagnostics())
		}
	}
	limitPointers := map[string]bool{
		"/components/parameters/Tags/examples/large/externalValue": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.example.external-value.limit" {
			continue
		}
		if diagnostic.InstanceLocation ==
			"/components/parameters/Tags/examples/exact/externalValue" {
			t.Errorf("exact-limit external example was rejected: %#v", diagnostic)
		}
		if _, tracked := limitPointers[diagnostic.InstanceLocation]; tracked {
			limitPointers[diagnostic.InstanceLocation] = true
		}
	}
	for pointer, found := range limitPointers {
		if !found {
			t.Errorf("missing byte-limit diagnostic at %s: %#v",
				pointer, report.Diagnostics())
		}
	}
}

func TestMediaTypeExamplesUseDeclaredFormatAcrossEarlierVersions(t *testing.T) {
	t.Parallel()

	const template = `{
		"openapi":"VERSION","info":{"title":"API","version":"1"},
		"paths":{},"components":{"requestBodies":{"Body":{"content":{
			"application/x-www-form-urlencoded":{
				"schema":{"type":"string"},"example":"?field=value"
			}
		}}}}
	}`
	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.1.0",
	} {
		document := mustDocument(t, strings.Replace(template, "VERSION", version, 1))
		report, err := validate.Document(context.Background(), document)
		if err != nil {
			t.Fatalf("OpenAPI %s: %v", version, err)
		}
		found := false
		for _, diagnostic := range report.Diagnostics() {
			if diagnostic.Code == "openapi.example.query-leading-delimiter" {
				found = true
			}
		}
		if !found {
			t.Errorf("OpenAPI %s diagnostics = %#v", version, report.Diagnostics())
		}
	}
}

func TestMediaTypeExampleOverridesSchemaExample(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2",
	} {
		document := mustDocument(t, `{
			"openapi":"`+version+`","info":{"title":"API","version":"1"},
			"paths":{},"components":{"requestBodies":{"Body":{"content":{
				"application/json":{"schema":{"type":"integer","example":"bad"},
					"example":1},
				"application/problem+json":{
					"schema":{"type":"integer","example":"bad"},
					"examples":{"valid":{"value":2}}}
			}}}}
		}`)
		report, err := validate.Document(context.Background(), document)
		if err != nil {
			t.Fatalf("OpenAPI %s: %v", version, err)
		}
		for _, diagnostic := range report.Diagnostics() {
			if diagnostic.Code == "openapi.example.schema" {
				t.Errorf("OpenAPI %s schema annotation overrode media examples: %#v",
					version, diagnostic)
			}
		}
	}
}

func TestParameterExamplesOverrideSchemaExample(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2",
	} {
		document := mustDocument(t, `{
			"openapi":"`+version+`","info":{"title":"API","version":"1"},
			"paths":{},"components":{"parameters":{
				"Direct":{"name":"direct","in":"query",
					"schema":{"type":"integer","example":"bad"},"example":1},
				"Named":{"name":"named","in":"query",
					"schema":{"type":"integer","example":"bad"},
					"examples":{"valid":{"value":2}}}
			}}
		}`)
		report, err := validate.Document(context.Background(), document)
		if err != nil {
			t.Fatalf("OpenAPI %s: %v", version, err)
		}
		for _, diagnostic := range report.Diagnostics() {
			if diagnostic.Code == "openapi.example.schema" {
				t.Errorf("OpenAPI %s schema annotation overrode parameter example: %#v",
					version, diagnostic)
			}
		}
	}
}

func TestHeaderExamplesOverrideSchemaExample(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"3.0.4", "3.1.1", "3.1.2"} {
		document := mustDocument(t, `{
			"openapi":"`+version+`","info":{"title":"API","version":"1"},
			"paths":{},"components":{"headers":{
				"Direct":{"schema":{"type":"integer","example":"bad"},
					"example":1},
				"Named":{"schema":{"type":"integer","example":"bad"},
					"examples":{"valid":{"value":2}}}
			}}
		}`)
		report, err := validate.Document(context.Background(), document)
		if err != nil {
			t.Fatalf("OpenAPI %s: %v", version, err)
		}
		for _, diagnostic := range report.Diagnostics() {
			if diagnostic.Code == "openapi.example.schema" {
				t.Errorf("OpenAPI %s schema annotation overrode header examples: %#v",
					version, diagnostic)
			}
		}
	}
}

func TestOpenAPI32ExampleDataValuesMustConformToSchemas(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{},"components":{
			"examples":{"Bad":{"dataValue":"many","serializedValue":"many"}},
			"headers":{
				"Direct":{"schema":{"type":"integer"},"examples":{
					"bad":{"dataValue":"many","serializedValue":"many"},
					"good":{"dataValue":2,"serializedValue":"2"}
				}},
				"Referenced":{"schema":{"type":"integer"},"examples":{
					"bad":{"$ref":"#/components/examples/Bad"}
				}}
			}
		}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/components/headers/Direct/examples/bad/dataValue": false,
		"/components/headers/Referenced/examples/bad/$ref":  false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.example.schema" {
			continue
		}
		if _, exists := want[diagnostic.InstanceLocation]; exists {
			want[diagnostic.InstanceLocation] = true
		}
		if diagnostic.InstanceLocation ==
			"/components/headers/Direct/examples/good/dataValue" {
			t.Fatalf("valid dataValue was rejected: %#v", diagnostic)
		}
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing dataValue diagnostic at %s: %#v",
				pointer, report.Diagnostics())
		}
	}
}

func TestOpenAPI32WarnsAboutSerializedJSONExamples(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/items":{"get":{"responses":{"200":{
			"description":"ok","content":{
				"application/json":{"examples":{
					"serialized":{"serializedValue":"{\"id\":1}"}
				}},
				"application/problem+json":{"examples":{
					"paired":{"dataValue":{"id":1},
						"serializedValue":"{\"id\":1}"}
				}},
				"text/plain":{"examples":{
					"allowed":{"serializedValue":"plain"}
				}}
			}
		}}}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/paths/~1items/get/responses/200/content/application~1json/examples/serialized/serializedValue":     false,
		"/paths/~1items/get/responses/200/content/application~1problem+json/examples/paired/serializedValue": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.example.serialized-json" {
			continue
		}
		if diagnostic.Severity != validate.SeverityWarning {
			t.Fatalf("serialized JSON diagnostic = %#v", diagnostic)
		}
		if _, exists := want[diagnostic.InstanceLocation]; exists {
			want[diagnostic.InstanceLocation] = true
		}
		if strings.Contains(diagnostic.InstanceLocation, "text~1plain") {
			t.Fatalf("text serialized example was discouraged: %#v", diagnostic)
		}
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing serialized JSON warning at %s: %#v",
				pointer, report.Diagnostics())
		}
	}
}

func TestOpenAPI32ValidatesSerializedJSONExampleData(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/items":{"get":{"responses":{"200":{
			"description":"ok","content":{"application/json":{
				"schema":{"type":"object","required":["id"],"properties":{
					"id":{"type":"integer"}
				}},
				"examples":{
					"matching":{"dataValue":{"id":1},
						"serializedValue":"{\n  \"id\": 1\n}"},
					"mismatch":{"dataValue":{"id":1},
						"serializedValue":"{\"id\":2}"},
					"invalidJSON":{"serializedValue":"{"},
					"invalidSchema":{"serializedValue":"{\"id\":\"one\"}"},
					"valid":{"serializedValue":"{\"id\":2}"}
				}
			}}
		}}}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"/paths/~1items/get/responses/200/content/application~1json/examples/mismatch/serializedValue":      "openapi.example.serialized-data-mismatch",
		"/paths/~1items/get/responses/200/content/application~1json/examples/invalidJSON/serializedValue":   "openapi.example.serialized-invalid",
		"/paths/~1items/get/responses/200/content/application~1json/examples/invalidSchema/serializedValue": "openapi.example.serialized-schema",
	}
	found := make(map[string]bool)
	for _, diagnostic := range report.Diagnostics() {
		code, exists := want[diagnostic.InstanceLocation]
		if !exists || diagnostic.Code != code {
			continue
		}
		found[diagnostic.InstanceLocation] = true
	}
	for pointer := range want {
		if !found[pointer] {
			t.Errorf("missing diagnostic at %s: %#v", pointer, report.Diagnostics())
		}
	}
	base := "/paths/~1items/get/responses/200/content/application~1json/examples/"
	for _, name := range []string{"matching", "valid"} {
		for _, diagnostic := range report.Diagnostics() {
			if strings.HasPrefix(diagnostic.InstanceLocation, base+name+"/") &&
				diagnostic.Code != "openapi.example.serialized-json" {
				t.Errorf("valid %s example rejected: %#v", name, diagnostic)
			}
		}
	}
}

func TestOpenAPI32ExternalExampleValuesAreURIReferences(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{},"components":{"examples":{
			"valid":{"externalValue":"../examples/item.json"},
			"invalidEscape":{"externalValue":"item%zz.json"},
			"control":{"externalValue":"item\u0000.json"},
			"leadingControl":{"externalValue":"\u0080item.json"}
		}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	found := make(map[string]bool)
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.example.external-value.invalid" {
			found[diagnostic.InstanceLocation] = true
		}
	}
	for _, name := range []string{"invalidEscape", "control", "leadingControl"} {
		pointer := "/components/examples/" + name + "/externalValue"
		if !found[pointer] {
			t.Errorf("missing diagnostic at %s: %#v", pointer, report.Diagnostics())
		}
	}
	if found["/components/examples/valid/externalValue"] || len(found) != 3 {
		t.Errorf("unexpected externalValue diagnostics: %#v", found)
	}
}

func TestOpenAPI32ExternalJSONExamplesUseExplicitResolver(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{},"components":{"requestBodies":{"Body":{"content":{
			"application/json":{"schema":{"type":"integer"},"examples":{
				"good":{"dataValue":1,"externalValue":"examples/good.json"},
				"mismatch":{"dataValue":2,"externalValue":"examples/mismatch.json"},
				"invalid":{"externalValue":"examples/invalid.json"},
				"missing":{"externalValue":"examples/missing.json"}
			}}
		}}}}
	}`)
	var identifiers []string
	options := validate.DefaultOptions()
	options.ReferenceResourceURI = "https://api.example.test/openapi.json"
	options.MaxExternalExampleBytes = 100
	options.ExternalExampleResolver = validate.ExternalExampleResolverFunc(
		func(_ context.Context, identifier string) (
			validate.ExternalExampleResource,
			error,
		) {
			identifiers = append(identifiers, identifier)
			switch identifier {
			case "https://api.example.test/examples/good.json",
				"https://api.example.test/examples/mismatch.json":
				return validate.ExternalExampleResource{
					RetrievalURI: identifier,
					ContentType:  "application/json",
					Data:         []byte(`1`),
				}, nil
			case "https://api.example.test/examples/invalid.json":
				return validate.ExternalExampleResource{
					RetrievalURI: identifier,
					ContentType:  "application/json",
					Data:         []byte(`"bad"`),
				}, nil
			default:
				return validate.ExternalExampleResource{}, errors.New("not found")
			}
		},
	)
	report, err := validate.DocumentWithOptions(
		context.Background(), document, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(identifiers) != 4 ||
		identifiers[0] != "https://api.example.test/examples/good.json" {
		t.Fatalf("resolved identifiers = %#v", identifiers)
	}
	want := map[string]string{
		"/components/requestBodies/Body/content/application~1json/examples/mismatch/externalValue": "openapi.example.external-data-mismatch",
		"/components/requestBodies/Body/content/application~1json/examples/invalid/externalValue":  "openapi.example.external-schema",
		"/components/requestBodies/Body/content/application~1json/examples/missing/externalValue":  "openapi.example.external-value.unresolved",
	}
	for _, diagnostic := range report.Diagnostics() {
		if code, exists := want[diagnostic.InstanceLocation]; exists && diagnostic.Code == code {
			delete(want, diagnostic.InstanceLocation)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing external example diagnostics: %#v; report %#v",
			want, report.Diagnostics())
	}
}

func TestNonJSONExamplesUseExplicitMediaTypeCodec(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{},"components":{"requestBodies":{"Body":{"content":{
			"application/vnd.example":{"schema":{"type":"integer"},"examples":{
				"mismatch":{"dataValue":1,"serializedValue":"2"},
				"invalid":{"serializedValue":"{"},
				"invalidSchema":{"serializedValue":"\"bad\""},
				"external":{"dataValue":1,"externalValue":"examples/value.txt"},
				"referenced":{"$ref":"#/components/examples/Referenced"},
				"externalReferenced":{"$ref":"#/components/examples/External"},
				"missing":{"$ref":"#/components/examples/Missing"}
			}}
		}}},"examples":{
			"Referenced":{"dataValue":1,"serializedValue":"2"},
			"External":{"dataValue":1,"externalValue":"examples/value.txt"}
		}}
	}`)
	options := validate.DefaultOptions()
	options.ReferenceResourceURI = "https://api.example.test/openapi.json"
	options.MediaTypeExampleCodecResolver =
		validate.MediaTypeExampleCodecResolverFunc(func(
			_ context.Context,
			mediaType string,
			_ jsonvalue.Value,
		) (validate.MediaTypeExampleCodec, error) {
			if mediaType != "application/vnd.example" {
				return nil, nil
			}
			return strictExampleCodec{}, nil
		})
	options.ExternalExampleResolver = validate.ExternalExampleResolverFunc(
		func(_ context.Context, identifier string) (
			validate.ExternalExampleResource,
			error,
		) {
			return validate.ExternalExampleResource{
				RetrievalURI: identifier,
				ContentType:  "application/vnd.example",
				Data:         []byte(`"bad"`),
			}, nil
		},
	)
	report, err := validate.DocumentWithOptions(
		context.Background(), document, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"openapi.example.serialized-data-mismatch": false,
		"openapi.example.serialized-invalid":       false,
		"openapi.example.serialized-schema":        false,
		"openapi.example.external-schema":          false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if _, tracked := want[diagnostic.Code]; tracked {
			want[diagnostic.Code] = true
		}
	}
	for code, found := range want {
		if !found {
			t.Errorf("missing %s: %#v", code, report.Diagnostics())
		}
	}
}

func TestMediaTypeExampleCodecResolutionIsExplicit(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{},"components":{"responses":{"Response":{
			"description":"ok","content":{
				"application/x-error":{"examples":{"value":{"dataValue":1}}},
				"application/x-none":{"examples":{"value":{"dataValue":1}}}
			}
		}}}
	}`)
	options := validate.DefaultOptions()
	options.MediaTypeExampleCodecResolver =
		validate.MediaTypeExampleCodecResolverFunc(func(
			_ context.Context,
			mediaType string,
			_ jsonvalue.Value,
		) (validate.MediaTypeExampleCodec, error) {
			if mediaType == "application/x-error" {
				return nil, errors.New("codec unavailable")
			}
			return nil, nil
		})
	report, err := validate.DocumentWithOptions(
		context.Background(), document, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.example.media-codec" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("codec diagnostics = %d: %#v", count, report.Diagnostics())
	}
}

func TestEarlierNonJSONExamplesUseExplicitMediaTypeCodec(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2",
	} {
		document := mustDocument(t, `{
			"openapi":"`+version+`","info":{"title":"API","version":"1"},
			"paths":{},"components":{"requestBodies":{"Body":{"content":{
				"application/vnd.example":{
					"schema":{"type":"string"},"example":"unencodable"
				}
			}}}}
		}`)
		options := validate.DefaultOptions()
		options.MediaTypeExampleCodecResolver =
			validate.MediaTypeExampleCodecResolverFunc(func(
				context.Context,
				string,
				jsonvalue.Value,
			) (validate.MediaTypeExampleCodec, error) {
				return strictExampleCodec{}, nil
			})
		report, err := validate.DocumentWithOptions(
			context.Background(), document, options,
		)
		if err != nil {
			t.Fatalf("OpenAPI %s: %v", version, err)
		}
		found := false
		for _, diagnostic := range report.Diagnostics() {
			if diagnostic.Code == "openapi.example.media-serialization" {
				found = true
			}
		}
		if !found {
			t.Errorf("OpenAPI %s diagnostics: %#v", version, report.Diagnostics())
		}
	}
}

func TestEarlierOpenAPIExternalJSONExamplesUseExplicitResolver(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2",
	} {
		document := mustDocument(t, `{
			"openapi":"`+version+`","info":{"title":"API","version":"1"},
			"paths":{},"components":{"requestBodies":{"Body":{"content":{
				"application/json":{"schema":{"type":"integer"},"examples":{
					"external":{"externalValue":"examples/value.json"}
				}}
			}}}}
		}`)
		var identifier string
		options := validate.DefaultOptions()
		options.ReferenceResourceURI = "https://api.example.test/openapi.json"
		options.ExternalExampleResolver = validate.ExternalExampleResolverFunc(
			func(_ context.Context, resolved string) (
				validate.ExternalExampleResource,
				error,
			) {
				identifier = resolved
				return validate.ExternalExampleResource{Data: []byte(`"bad"`)}, nil
			},
		)
		report, err := validate.DocumentWithOptions(
			context.Background(), document, options,
		)
		if err != nil {
			t.Fatalf("OpenAPI %s: %v", version, err)
		}
		if identifier != "https://api.example.test/examples/value.json" {
			t.Errorf("OpenAPI %s resolved %q", version, identifier)
		}
		found := false
		for _, diagnostic := range report.Diagnostics() {
			if diagnostic.Code == "openapi.example.external-schema" {
				found = true
			}
		}
		if !found {
			t.Errorf("OpenAPI %s diagnostics = %#v", version, report.Diagnostics())
		}
	}
}

func TestOpenAPI32ExternalJSONExamplesAreBoundedAndParsed(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{},"components":{"requestBodies":{"Body":{"content":{
			"application/json":{"examples":{
				"large":{"externalValue":"https://cdn.example.test/large.json"},
				"malformed":{"externalValue":"malformed.json"}
			}}
		}}}}
	}`)
	options := validate.DefaultOptions()
	options.ReferenceResourceURI = "https://api.example.test/openapi.json"
	options.MaxExternalExampleBytes = 3
	options.ExternalExampleResolver = validate.ExternalExampleResolverFunc(
		func(_ context.Context, identifier string) (
			validate.ExternalExampleResource,
			error,
		) {
			if identifier == "https://cdn.example.test/large.json" {
				return validate.ExternalExampleResource{Data: []byte(`1234`)}, nil
			}
			return validate.ExternalExampleResource{Data: []byte(`{`)}, nil
		},
	)
	report, err := validate.DocumentWithOptions(
		context.Background(), document, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"/components/requestBodies/Body/content/application~1json/examples/large/externalValue":     "openapi.example.external-value.limit",
		"/components/requestBodies/Body/content/application~1json/examples/malformed/externalValue": "openapi.example.external-value.invalid",
	}
	for _, diagnostic := range report.Diagnostics() {
		if code, exists := want[diagnostic.InstanceLocation]; exists && diagnostic.Code == code {
			delete(want, diagnostic.InstanceLocation)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing external example diagnostics: %#v; report %#v",
			want, report.Diagnostics())
	}
}

func TestOpenAPI32WarnsAboutAmbiguousLegacyExampleValues(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/items":{"get":{
			"parameters":[{"name":"filter","in":"query",
				"schema":{"type":"object"},
				"examples":{"legacy":{"value":{"id":1}}}
			}],
			"responses":{"200":{"description":"ok","content":{
				"application/xml":{"schema":{"type":"object"},
					"examples":{"legacy":{"value":{"id":1}}}},
				"application/json":{"schema":{"type":"object"},
					"examples":{"safe":{"value":{"id":1}}}},
				"text/plain":{"schema":{"type":"string"},
					"examples":{"safe":{"value":"plain"}}}
			}}
		}}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/paths/~1items/get/parameters/0/examples/legacy/value":                           false,
		"/paths/~1items/get/responses/200/content/application~1xml/examples/legacy/value": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.example.value.nonportable" {
			continue
		}
		if diagnostic.Severity != validate.SeverityWarning {
			t.Errorf("severity = %s", diagnostic.Severity)
		}
		if _, exists := want[diagnostic.InstanceLocation]; !exists {
			t.Errorf("unexpected warning: %#v", diagnostic)
			continue
		}
		want[diagnostic.InstanceLocation] = true
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing warning at %s: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestSerializedExamplesOmitContextDelimitersAndHeaderNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		version string
		fields  string
	}{
		{
			version: "3.1.2",
			fields:  `"example":"?color=blue"`,
		},
		{
			version: "3.2.0",
			fields:  `"examples":{"bad":{"serializedValue":"?color=blue"}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.version, func(t *testing.T) {
			t.Parallel()
			headerExample := `"example":"X-Trace: one"`
			formExample := `"example":"&color=blue"`
			if test.version == "3.2.0" {
				headerExample = `"examples":{"bad":{"serializedValue":"X-Trace: one"}}`
				formExample = `"examples":{"bad":{"serializedValue":"&color=blue"}}`
			}
			document := mustDocument(t, `{
				"openapi":"`+test.version+`","info":{"title":"API","version":"1"},
				"paths":{},"components":{
					"parameters":{"Color":{"name":"color","in":"query",
						"schema":{"type":"string"},`+test.fields+`},
						"Cookie":{"name":"color","in":"cookie",
						"schema":{"type":"string"},`+test.fields+`},
						"Trace":{"name":"X-Trace","in":"header",
						"schema":{"type":"string"},`+headerExample+`}},
					"headers":{"X-Trace":{"schema":{"type":"string"},`+
				headerExample+`}},
					"requestBodies":{"Form":{"content":{
						"application/x-www-form-urlencoded":{"schema":{
							"type":"object","properties":{"color":{"type":"string"}}
						},`+formExample+`}
					}}}
				}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]int{
				"openapi.example.query-leading-delimiter": 3,
				"openapi.example.header-name":             2,
			}
			for _, diagnostic := range report.Diagnostics() {
				if _, exists := want[diagnostic.Code]; exists {
					want[diagnostic.Code]--
				}
			}
			for code, remaining := range want {
				if remaining != 0 {
					t.Errorf("%s remaining diagnostics = %d: %#v",
						code, remaining, report.Diagnostics())
				}
			}
		})
	}
}

func TestFormExamplesOmitQueryPrefixesInClarifiedRevisions(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"3.0.4", "3.1.1"} {
		document := mustDocument(t, `{
			"openapi":"`+version+`","info":{"title":"API","version":"1"},
			"paths":{},"components":{"requestBodies":{"Form":{"content":{
				"application/x-www-form-urlencoded":{
					"schema":{"type":"string"},"example":"?color=blue"
				}
			}}}}
		}`)
		report, err := validate.Document(context.Background(), document)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, diagnostic := range report.Diagnostics() {
			if diagnostic.Code == "openapi.example.query-leading-delimiter" {
				found = true
			}
		}
		if !found {
			t.Errorf("version %s accepted a leading form prefix", version)
		}
	}
}

func TestParameterExamplesFollowTheirSerializationStrategy(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		example := `"example":{"nested":{"id":"42"}}`
		if version == "3.2.0" {
			example = `"examples":{"bad":{"dataValue":{"nested":{"id":"42"}}}}`
		}
		document := mustDocument(t, `{
			"openapi":"`+version+`","info":{"title":"API","version":"1"},
			"paths":{},"components":{"parameters":{"ID":{
				"name":"id","in":"path","required":true,"style":"matrix",
				"schema":{"type":"object"},`+example+`
			}}}
		}`)
		report, err := validate.Document(context.Background(), document)
		if err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, diagnostic := range report.Diagnostics() {
			if diagnostic.Code ==
				"openapi.example.parameter-serialization" {
				found++
			}
		}
		if found != 1 {
			t.Errorf("version %s serialization diagnostics = %d: %#v",
				version, found, report.Diagnostics())
		}
	}
}

func TestNamedParameterExamplesFollowTheirSerializationStrategy(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		field := `"value":{"nested":{"id":"42"}}`
		fieldName := "value"
		if version == "3.2.0" {
			field = `"dataValue":{"nested":{"id":"42"}}`
			fieldName = "dataValue"
		}
		document := mustDocument(t, `{
			"openapi":"`+version+`","info":{"title":"API","version":"1"},
			"paths":{},"components":{"parameters":{"ID":{
				"name":"id","in":"path","required":true,"style":"matrix",
				"schema":{"type":"object"},
				"examples":{"bad":{`+field+`}}
			}}}
		}`)
		report, err := validate.Document(context.Background(), document)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, diagnostic := range report.Diagnostics() {
			if diagnostic.Code == "openapi.example.parameter-serialization" &&
				diagnostic.InstanceLocation ==
					"/components/parameters/ID/examples/bad/"+
						fieldName {
				found = true
			}
		}
		if !found {
			t.Errorf("version %s accepted a malformed named example: %#v",
				version, report.Diagnostics())
		}
	}
}

func TestClarifiedHeaderExamplesOmitTheHeaderName(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"3.0.4", "3.1.1"} {
		document := mustDocument(t, `{
			"openapi":"`+version+`","info":{"title":"API","version":"1"},
			"paths":{},"components":{"headers":{"X-Trace":{
				"schema":{"type":"string"},"example":"X-Trace: one"
			}}}
		}`)
		report, err := validate.Document(context.Background(), document)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, diagnostic := range report.Diagnostics() {
			if diagnostic.Code == "openapi.example.header-name" {
				found = true
			}
		}
		if !found {
			t.Errorf("version %s accepted a serialized header name", version)
		}
	}
}
