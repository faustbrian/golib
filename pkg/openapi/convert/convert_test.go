package convert_test

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/convert"
	openapischema "github.com/faustbrian/golib/pkg/openapi/jsonschema"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestConvertRejectsWideRootBeforeCopyingMembers(t *testing.T) {
	version, _ := jsonvalue.String("3.1.2")
	members := []jsonvalue.Member{{Name: "openapi", Value: version}}
	for index := range 4096 {
		members = append(members, jsonvalue.Member{
			Name: "x-wide-" + strconv.Itoa(index), Value: jsonvalue.Null(),
		})
	}
	raw, _ := jsonvalue.Object(members)
	source, err := openapi.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := openapi.ParseVersion("3.2.0")
	options := convert.DefaultOptions()
	options.MaxRootMembers = 1

	const repetitions = 16
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	for range repetitions {
		if _, err := convert.To(
			context.Background(), source, target, options,
		); !errors.Is(err, convert.ErrLimitExceeded) {
			t.Fatalf("wide conversion error = %v", err)
		}
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	allocated := (after.TotalAlloc - before.TotalAlloc) / repetitions
	if allocated > 64<<10 {
		t.Fatalf("wide rejected conversion allocated %d bytes per operation", allocated)
	}
}

func TestConvertPatchVersionPreservesDocumentValues(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.1.0",
		"info":{"title":"API","version":"1"},
		"x-exact":-0.00e+2,
		"paths":{}
	}`)
	target, _ := openapi.ParseVersion("3.1.2")
	result, err := convert.To(
		context.Background(), source, target, convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Source(), source) {
		t.Fatal("conversion did not retain source provenance")
	}
	if result.Document().SpecificationVersion().String() != "3.1.2" {
		t.Fatalf("target version = %s", result.Document().SpecificationVersion())
	}
	if len(result.Diagnostics()) != 0 {
		t.Fatalf("patch conversion diagnostics = %#v", result.Diagnostics())
	}
	exact, _ := result.Document().Raw().Lookup("x-exact")
	if number, _ := exact.NumberText(); number != "-0.00e+2" {
		t.Fatalf("exact number = %q", number)
	}
}

func TestConvertOpenAPI31To32PreservesSchemaDialect(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.1.2",
		"info":{"title":"API","version":"1"},
		"paths":{}
	}`)
	target, _ := openapi.ParseVersion("3.2.0")
	result, err := convert.To(
		context.Background(), source, target, convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	dialect, exists := result.Document().Raw().Lookup("jsonSchemaDialect")
	if text, valid := dialect.Text(); !exists || !valid ||
		text != "https://spec.openapis.org/oas/3.1/dialect/base" {
		t.Fatalf("preserved schema dialect = %q, %t, %t", text, exists, valid)
	}
	diagnostics := result.Diagnostics()
	if len(diagnostics) != 1 ||
		diagnostics[0].Kind != convert.ManualAction ||
		diagnostics[0].Code != "openapi.convert.minor-version-review" {
		t.Fatalf("conversion diagnostics = %#v", diagnostics)
	}
}

func TestConvertOpenAPI31To32RetainsExplicitSchemaDialect(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.1.2",
		"info":{"title":"API","version":"1"},
		"jsonSchemaDialect":"https://schemas.example.test/dialect",
		"paths":{}
	}`)
	target, _ := openapi.ParseVersion("3.2.0")
	result, err := convert.To(
		context.Background(), source, target, convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	dialect, _ := result.Document().Raw().Lookup("jsonSchemaDialect")
	if text, _ := dialect.Text(); text != "https://schemas.example.test/dialect" {
		t.Fatalf("schema dialect = %q", text)
	}
}

func TestConvertOpenAPI30To31TranslatesSchemaKeywords(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.0.4",
		"info":{"title":"API","version":"1"},
		"paths":{},
		"components":{"schemas":{"Measurement":{
			"type":"object",
			"properties":{
				"value":{
					"type":"number",
					"nullable":true,
					"minimum":-0.00e+2,
					"exclusiveMinimum":true,
					"maximum":10,
					"exclusiveMaximum":false
				}
			}
		}}},
		"x-schema-shaped":{"type":"string","nullable":true}
	}`)
	target, _ := openapi.ParseVersion("3.1.2")
	result, err := convert.To(
		context.Background(), source, target, convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	value := lookup(t, result.Document().Raw(),
		"components", "schemas", "Measurement", "properties", "value",
	)
	typeValue, _ := value.Lookup("type")
	types, _ := typeValue.Elements()
	if len(types) != 2 || text(t, types[0]) != "number" ||
		text(t, types[1]) != "null" {
		t.Fatalf("translated type = %#v", types)
	}
	if _, exists := value.Lookup("nullable"); exists {
		t.Fatal("nullable was retained")
	}
	if _, exists := value.Lookup("minimum"); exists {
		t.Fatal("minimum was retained with exclusiveMinimum")
	}
	exclusiveMinimum, _ := value.Lookup("exclusiveMinimum")
	if number, _ := exclusiveMinimum.NumberText(); number != "-0.00e+2" {
		t.Fatalf("exclusiveMinimum = %q", number)
	}
	if _, exists := value.Lookup("exclusiveMaximum"); exists {
		t.Fatal("false exclusiveMaximum was retained")
	}
	maximum, _ := value.Lookup("maximum")
	if number, _ := maximum.NumberText(); number != "10" {
		t.Fatalf("maximum = %q", number)
	}
	extension := lookup(t, result.Document().Raw(), "x-schema-shaped")
	if _, exists := extension.Lookup("nullable"); !exists {
		t.Fatal("schema-shaped extension was modified")
	}
	if len(result.Diagnostics()) != 0 {
		t.Fatalf("conversion diagnostics = %#v", result.Diagnostics())
	}
	compiler, err := openapischema.NewCompiler(openapischema.DialectOAS31)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(context.Background(), value)
	if err != nil {
		t.Fatal(err)
	}
	for instance, wantValid := range map[string]bool{
		"null": true, "1": true, "0": false, "10": true,
	} {
		validation, validateErr := compiled.Validate(
			context.Background(), []byte(instance),
		)
		if validateErr != nil {
			t.Fatal(validateErr)
		}
		if validation.Valid != wantValid {
			t.Fatalf("instance %s validity = %t", instance, validation.Valid)
		}
	}
}

func TestConvertOpenAPI30To31ReportsUnrepresentableExclusiveBounds(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.0.0",
		"info":{"title":"API","version":"1"},
		"paths":{},
		"components":{"schemas":{"Broken":{
			"type":"number",
			"exclusiveMinimum":true,
			"exclusiveMaximum":true
		}}}
	}`)
	target, _ := openapi.ParseVersion("3.1.0")
	result, err := convert.To(
		context.Background(), source, target, convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := result.Diagnostics()
	if len(diagnostics) != 2 || diagnostics[0].Kind != convert.Loss ||
		diagnostics[0].Pointer !=
			"/components/schemas/Broken/exclusiveMinimum" ||
		diagnostics[1].Pointer !=
			"/components/schemas/Broken/exclusiveMaximum" {
		t.Fatalf("conversion diagnostics = %#v", diagnostics)
	}
	schema := lookup(t, result.Document().Raw(),
		"components", "schemas", "Broken",
	)
	if _, exists := schema.Lookup("exclusiveMinimum"); exists {
		t.Fatal("boolean exclusiveMinimum was retained")
	}
	if _, exists := schema.Lookup("exclusiveMaximum"); exists {
		t.Fatal("boolean exclusiveMaximum was retained")
	}
}

func TestConvertOpenAPI30To31ReportsMalformedSchemaKeywords(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.0.0",
		"info":{"title":"API","version":"1"},
		"paths":{},
		"components":{"schemas":{"Broken":{
			"nullable":"yes",
			"exclusiveMinimum":1,
			"exclusiveMaximum":"yes"
		}}}
	}`)
	target, _ := openapi.ParseVersion("3.1.0")
	result, err := convert.To(
		context.Background(), source, target, convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if diagnostics := result.Diagnostics(); len(diagnostics) != 3 {
		t.Fatalf("conversion diagnostics = %#v", diagnostics)
	}
	schema := lookup(t, result.Document().Raw(),
		"components", "schemas", "Broken",
	)
	for _, name := range []string{
		"nullable", "exclusiveMinimum", "exclusiveMaximum",
	} {
		if _, exists := schema.Lookup(name); exists {
			t.Fatalf("malformed %s was retained", name)
		}
	}
}

func TestConvertOpenAPI30To31PreservesReferenceObjectSemantics(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.0.3",
		"info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{"responses":{"200":{
			"$ref":"#/components/responses/Ok",
			"description":"ignored"
		}}}}},
		"components":{"schemas":{
			"Internal":{"$ref":"#/components/schemas/Target","nullable":true},
			"External":{"$ref":"schemas.json#/External"},
			"Target":{"type":"string","nullable":true}
		},"responses":{"Ok":{"description":"ok"}},
		"examples":{"Remote":{"$ref":"examples.json#/Remote"}}}
	}`)
	target, _ := openapi.ParseVersion("3.1.1")
	result, err := convert.To(
		context.Background(), source, target, convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	internal := lookup(t, result.Document().Raw(),
		"components", "schemas", "Internal",
	)
	if members, _ := internal.Members(); len(members) != 1 ||
		members[0].Name != "$ref" {
		t.Fatalf("internal reference = %#v", members)
	}
	response := lookup(t, result.Document().Raw(),
		"paths", "/pets", "get", "responses", "200",
	)
	if members, _ := response.Members(); len(members) != 1 ||
		members[0].Name != "$ref" {
		t.Fatalf("response reference = %#v", members)
	}
	diagnostics := result.Diagnostics()
	want := map[string]convert.DiagnosticKind{
		"/paths/~1pets/get/responses/200/description": convert.Loss,
		"/components/schemas/Internal/nullable":       convert.Loss,
		"/components/schemas/External/$ref":           convert.ManualAction,
		"/components/examples/Remote/$ref":            convert.ManualAction,
	}
	if len(diagnostics) != len(want) {
		t.Fatalf("conversion diagnostics = %#v", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if want[diagnostic.Pointer] != diagnostic.Kind {
			t.Fatalf("conversion diagnostic = %#v", diagnostic)
		}
	}
}

func TestConvertOpenAPI30To31FindsEverySchemaObjectContext(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.0.2",
		"info":{"title":"API","version":"1"},
		"paths":{"/pets":{
			"parameters":[{"name":"root","in":"query","schema":{"type":"string","nullable":true}}],
			"post":{
				"parameters":[{"name":"operation","in":"query","content":{"text/plain":{"schema":{"type":"string","nullable":true}}}}],
				"requestBody":{"content":{"multipart/form-data":{
					"schema":{"allOf":[{"type":"string","nullable":true}],"anyOf":[{"type":"number","nullable":true}],"oneOf":[{"type":"integer","nullable":true}],"not":{"type":"boolean","nullable":true},"items":{"type":"string","nullable":true},"additionalProperties":{"type":"string","nullable":true}},
					"encoding":{"part":{"headers":{"X-Part":{"schema":{"type":"string","nullable":true}}}}}
				}}},
				"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"string","nullable":true}}},"headers":{"X-Result":{"schema":{"type":"string","nullable":true}}}}},
				"callbacks":{"again":{"{$request.body#/url}":{"post":{"responses":{"204":{"description":"ok","content":{"application/json":{"schema":{"type":"string","nullable":true}}}}}}}}}
			}
		}},
		"components":{
			"parameters":{"P":{"name":"p","in":"query","schema":{"type":"string","nullable":true}}},
			"headers":{"H":{"schema":{"type":"string","nullable":true}}},
			"requestBodies":{"B":{"content":{"application/json":{"schema":{"type":"string","nullable":true}}}}},
			"responses":{"R":{"description":"ok","content":{"application/json":{"schema":{"type":"string","nullable":true}}}}},
			"callbacks":{"C":{"{$request.body#/next}":{"post":{"responses":{"204":{"description":"ok","content":{"application/json":{"schema":{"type":"string","nullable":true}}}}}}}}},
			"schemas":{"Escaped/Name~":{"type":"object","properties":{"child":{"type":"string","nullable":true}}}}
		}
	}`)
	target, _ := openapi.ParseVersion("3.1.0")
	result, err := convert.To(
		context.Background(), source, target, convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := result.Document().Raw().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"nullable"`) {
		t.Fatalf("unconverted Schema Object: %s", raw)
	}
	if len(result.Diagnostics()) != 0 {
		t.Fatalf("conversion diagnostics = %#v", result.Diagnostics())
	}
}

func TestConvertOpenAPI30To31BoundsSchemaWork(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.0.0",
		"info":{"title":"API","version":"1"},
		"paths":{},
		"components":{"schemas":{"Root":{
			"type":"array","items":{"type":"string"}
		}}}
	}`)
	target, _ := openapi.ParseVersion("3.1.0")
	_, err := convert.To(context.Background(), source, target, convert.Options{
		MaxRootMembers: 100,
		MaxSchemaNodes: 1,
	})
	if !errors.Is(err, convert.ErrLimitExceeded) {
		t.Fatalf("schema limit error = %v", err)
	}
}

func TestConvertSwaggerBoundsDocumentWork(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{"responses":{"200":{"description":"OK"}}}}}
	}`)
	target, _ := openapi.ParseVersion("3.0.4")
	_, err := convert.To(context.Background(), source, target, convert.Options{
		MaxRootMembers: 100, MaxDocumentNodes: 2, MaxSchemaNodes: 100,
	})
	if !errors.Is(err, convert.ErrLimitExceeded) {
		t.Fatalf("document limit error = %v", err)
	}
}

func TestConvertOpenAPI30To32ChainsSchemaMigrations(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.0.0",
		"info":{"title":"API","version":"1"},
		"paths":{},
		"components":{"schemas":{"Name":{"type":"string","nullable":true}}}
	}`)
	target, _ := openapi.ParseVersion("3.2.0")
	result, err := convert.To(
		context.Background(), source, target, convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	types, _ := lookup(
		t, result.Document().Raw(), "components", "schemas", "Name", "type",
	).Elements()
	if len(types) != 2 || text(t, types[0]) != "string" ||
		text(t, types[1]) != "null" {
		t.Fatalf("schema types = %#v", types)
	}
	if result.Document().SpecificationVersion().String() != "3.2.0" {
		t.Fatalf("version = %s", result.Document().SpecificationVersion())
	}
	if len(result.Diagnostics()) != 1 ||
		result.Diagnostics()[0].Code != "openapi.convert.minor-version-review" {
		t.Fatalf("diagnostics = %#v", result.Diagnostics())
	}
}

func TestConvertOpenAPI31To30ReportsLosses(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.1.2",
		"info":{"title":"API","summary":"Overview","version":"1"},
		"paths":{},
		"components":{"schemas":{
			"Value":{"type":["string","integer","null"],"const":"fixed"},
			"NullOnly":{"type":["null"]}
		}}
	}`)
	target, _ := openapi.ParseVersion("3.0.4")
	result, err := convert.To(
		context.Background(), source, target, convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Document().SpecificationVersion().String() != "3.0.4" {
		t.Fatalf("version = %s", result.Document().SpecificationVersion())
	}
	schema := lookup(t, result.Document().Raw(), "components", "schemas", "Value")
	alternatives, _ := lookup(t, schema, "anyOf").Elements()
	if len(alternatives) != 2 {
		t.Fatalf("schema alternatives = %#v", alternatives)
	}
	if nullable, _ := lookup(t, alternatives[0], "nullable").Bool(); !nullable {
		t.Fatalf("schema alternatives = %#v", alternatives)
	}
	if len(result.Diagnostics()) != 1 ||
		result.Diagnostics()[0].Pointer != "/info/summary" ||
		result.Diagnostics()[0].Kind != convert.Loss {
		t.Fatalf("diagnostics = %#v", result.Diagnostics())
	}
	report, err := validate.Document(context.Background(), result.Document())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() {
		t.Fatalf("validation diagnostics = %#v", report.Diagnostics())
	}
}

func TestConvertOpenAPI32To31PreservesItsSchemaDialect(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{},
		"components":{"schemas":{"Pet":{"type":"object",
			"required":["kind"],"properties":{"kind":{"type":"string"}},
			"xml":{"nodeType":"element"},
			"discriminator":{"propertyName":"kind","defaultMapping":"Pet"}
		}}}
	}`)
	target, _ := openapi.ParseVersion("3.1.2")
	result, err := convert.To(
		context.Background(), source, target, convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Document().SpecificationVersion().String() != "3.1.2" {
		t.Fatalf("version = %s", result.Document().SpecificationVersion())
	}
	if text(t, lookup(t, result.Document().Raw(), "jsonSchemaDialect")) !=
		"https://spec.openapis.org/oas/3.2/dialect/2025-09-17" {
		t.Fatalf("document = %#v", result.Document().Raw())
	}
	if len(result.Diagnostics()) != 0 {
		t.Fatalf("diagnostics = %#v", result.Diagnostics())
	}
	report, err := validate.Document(context.Background(), result.Document())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() {
		t.Fatalf("validation diagnostics = %#v", report.Diagnostics())
	}
}

func TestConvertOpenAPI32To30ChainsDowngrades(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{},
		"components":{"schemas":{"Pet":{"type":"object",
			"required":["kind"],"properties":{"kind":{"type":"string"}},
			"xml":{"nodeType":"attribute"},
			"discriminator":{"propertyName":"kind","defaultMapping":"Pet"}
		}}}
	}`)
	target, _ := openapi.ParseVersion("3.0.4")
	result, err := convert.To(
		context.Background(), source, target, convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Document().SpecificationVersion().String() != "3.0.4" {
		t.Fatalf("version = %s", result.Document().SpecificationVersion())
	}
	schema := lookup(t, result.Document().Raw(), "components", "schemas", "Pet")
	if attribute, _ := lookup(t, schema, "xml", "attribute").Bool(); !attribute {
		t.Fatalf("XML metadata = %#v", schema)
	}
	if _, exists := lookup(t, schema, "discriminator").Lookup("defaultMapping"); exists {
		t.Fatal("discriminator default mapping was retained")
	}
	want := map[string]convert.DiagnosticKind{
		"/jsonSchemaDialect": convert.Loss,
		"/components/schemas/Pet/discriminator/defaultMapping": convert.Loss,
	}
	if len(result.Diagnostics()) != len(want) {
		t.Fatalf("diagnostics = %#v", result.Diagnostics())
	}
	for _, diagnostic := range result.Diagnostics() {
		if kind, exists := want[diagnostic.Pointer]; !exists || kind != diagnostic.Kind {
			t.Fatalf("diagnostic = %#v", diagnostic)
		}
	}
	report, err := validate.Document(context.Background(), result.Document())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() {
		t.Fatalf("validation diagnostics = %#v", report.Diagnostics())
	}
}

func TestConvertOpenAPI30ToSwagger20RootAndSchemas(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.0.4","info":{"title":"API","version":"1"},
		"servers":[{"url":"https://api.example.test/v1"}],
		"paths":{"/pets":{"get":{"responses":{"200":{
			"description":"OK","content":{"application/json":{"schema":{
				"$ref":"#/components/schemas/Pet"}}}}}}}},
		"components":{"schemas":{"Pet":{"type":"object",
			"properties":{"name":{"type":"string"}}}}}
	}`)
	target, _ := openapi.ParseVersion("2.0")
	result, err := convert.To(
		context.Background(), source, target, convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Document().SpecificationVersion().String() != "2.0" {
		t.Fatalf("version = %s", result.Document().SpecificationVersion())
	}
	root := result.Document().Raw()
	if text(t, lookup(t, root, "host")) != "api.example.test" ||
		text(t, lookup(t, root, "basePath")) != "/v1" {
		t.Fatalf("server = %#v", root)
	}
	schemes, _ := lookup(t, root, "schemes").Elements()
	if len(schemes) != 1 || text(t, schemes[0]) != "https" {
		t.Fatalf("schemes = %#v", schemes)
	}
	if text(t, lookup(t, root, "definitions", "Pet", "type")) != "object" {
		t.Fatalf("definitions = %#v", root)
	}
	response := lookup(t, root, "paths", "/pets", "get", "responses", "200")
	if text(t, lookup(t, response, "schema", "$ref")) != "#/definitions/Pet" {
		t.Fatalf("response = %#v", response)
	}
	produces, _ := lookup(t, root, "paths", "/pets", "get", "produces").Elements()
	if len(produces) != 1 || text(t, produces[0]) != "application/json" {
		t.Fatalf("produces = %#v", produces)
	}
	if len(result.Diagnostics()) != 0 {
		t.Fatalf("diagnostics = %#v", result.Diagnostics())
	}
	report, err := validate.Document(context.Background(), result.Document())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() {
		t.Fatalf("validation diagnostics = %#v", report.Diagnostics())
	}
}

func TestConvertOpenAPI30ToSwagger20ServerVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		servers     string
		host        string
		basePath    string
		scheme      string
		diagnostics int
	}{
		{
			name: "relative",
			servers: `[{"url":"/v1","description":"local"},` +
				`{"url":"https://backup.example.test"}]`,
			basePath: "/v1", diagnostics: 2,
		},
		{
			name: "variables",
			servers: `[{"url":"https://{host}/{version}","variables":{` +
				`"host":{"default":"api.example.test"},` +
				`"version":{"default":"v2"}}}]`,
			host: "api.example.test", basePath: "/v2", scheme: "https",
			diagnostics: 1,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			source := mustDocument(t, `{"openapi":"3.0.4",`+
				`"info":{"title":"API","version":"1"},"servers":`+
				test.servers+`,"paths":{}}`)
			target, _ := openapi.ParseVersion("2.0")
			result, err := convert.To(
				context.Background(), source, target, convert.DefaultOptions(),
			)
			if err != nil {
				t.Fatal(err)
			}
			root := result.Document().Raw()
			if test.host != "" && text(t, lookup(t, root, "host")) != test.host {
				t.Fatalf("host = %#v", root)
			}
			if text(t, lookup(t, root, "basePath")) != test.basePath {
				t.Fatalf("base path = %#v", root)
			}
			if test.scheme != "" {
				schemes, _ := lookup(t, root, "schemes").Elements()
				if len(schemes) != 1 || text(t, schemes[0]) != test.scheme {
					t.Fatalf("schemes = %#v", schemes)
				}
			}
			if len(result.Diagnostics()) != test.diagnostics {
				t.Fatalf("diagnostics = %#v", result.Diagnostics())
			}
			report, err := validate.Document(context.Background(), result.Document())
			if err != nil {
				t.Fatal(err)
			}
			if !report.Valid() {
				t.Fatalf("validation diagnostics = %#v", report.Diagnostics())
			}
		})
	}
}

func TestConvertOpenAPI30ToSwagger20RequestInputs(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.0.4","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"post":{
			"parameters":[
				{"name":"tags","in":"query","style":"form","explode":false,
					"schema":{"type":"array","items":{"type":"string"}}},
				{"name":"X-Trace","in":"header","schema":{"type":"string"}}
			],
			"requestBody":{"required":true,"content":{"application/json":{
				"schema":{"$ref":"#/components/schemas/Pet"}}}},
			"responses":{"204":{"description":"Accepted"}}
		}}},
		"components":{"schemas":{"Pet":{"type":"object"}}}
	}`)
	target, _ := openapi.ParseVersion("2.0")
	result, err := convert.To(
		context.Background(), source, target, convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	operation := lookup(t, result.Document().Raw(), "paths", "/pets", "post")
	parameters, _ := lookup(t, operation, "parameters").Elements()
	if len(parameters) != 3 {
		t.Fatalf("parameters = %#v", parameters)
	}
	if text(t, lookup(t, parameters[0], "collectionFormat")) != "csv" ||
		text(t, lookup(t, parameters[0], "type")) != "array" {
		t.Fatalf("query parameter = %#v", parameters[0])
	}
	if text(t, lookup(t, parameters[1], "type")) != "string" {
		t.Fatalf("header parameter = %#v", parameters[1])
	}
	if text(t, lookup(t, parameters[2], "name")) != "body" ||
		text(t, lookup(t, parameters[2], "in")) != "body" ||
		text(t, lookup(t, parameters[2], "schema", "$ref")) !=
			"#/definitions/Pet" {
		t.Fatalf("body parameter = %#v", parameters[2])
	}
	consumes, _ := lookup(t, operation, "consumes").Elements()
	if len(consumes) != 1 || text(t, consumes[0]) != "application/json" {
		t.Fatalf("consumes = %#v", consumes)
	}
	if len(result.Diagnostics()) != 0 {
		t.Fatalf("diagnostics = %#v", result.Diagnostics())
	}
	report, err := validate.Document(context.Background(), result.Document())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() {
		t.Fatalf("validation diagnostics = %#v", report.Diagnostics())
	}
}

func TestConvertOpenAPI30ToSwagger20FormRequestBody(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.0.4","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"post":{
			"requestBody":{"required":true,"content":{"multipart/form-data":{
				"schema":{"type":"object","required":["name","photo"],
					"properties":{
						"name":{"type":"string"},
						"photo":{"type":"string","format":"binary"},
						"tags":{"type":"array","items":{"type":"string"}}
					}},
				"encoding":{"tags":{"style":"form","explode":true}}
			}}},
			"responses":{"204":{"description":"Accepted"}}
		}}}
	}`)
	target, _ := openapi.ParseVersion("2.0")
	result, err := convert.To(
		context.Background(), source, target, convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	operation := lookup(t, result.Document().Raw(), "paths", "/pets", "post")
	parameters, _ := lookup(t, operation, "parameters").Elements()
	if len(parameters) != 3 {
		t.Fatalf("parameters = %#v", parameters)
	}
	if text(t, lookup(t, parameters[0], "name")) != "name" ||
		text(t, lookup(t, parameters[0], "in")) != "formData" {
		t.Fatalf("name parameter = %#v", parameters[0])
	}
	if required, _ := lookup(t, parameters[0], "required").Bool(); !required {
		t.Fatalf("name parameter = %#v", parameters[0])
	}
	if text(t, lookup(t, parameters[1], "type")) != "file" {
		t.Fatalf("file parameter = %#v", parameters[1])
	}
	if text(t, lookup(t, parameters[2], "collectionFormat")) != "multi" {
		t.Fatalf("array parameter = %#v", parameters[2])
	}
	consumes, _ := lookup(t, operation, "consumes").Elements()
	if len(consumes) != 1 || text(t, consumes[0]) != "multipart/form-data" {
		t.Fatalf("consumes = %#v", consumes)
	}
	if len(result.Diagnostics()) != 0 {
		t.Fatalf("diagnostics = %#v", result.Diagnostics())
	}
	report, err := validate.Document(context.Background(), result.Document())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() {
		t.Fatalf("validation diagnostics = %#v", report.Diagnostics())
	}
}

func TestConvertOpenAPI30InlinesReusableSwaggerFormBody(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.0.4","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"post":{
			"requestBody":{"$ref":"#/components/requestBodies/Form"},
			"responses":{"204":{"description":"Accepted"}}}}},
		"components":{"requestBodies":{"Form":{"content":{
			"application/x-www-form-urlencoded":{"schema":{"type":"object",
				"properties":{"name":{"type":"string"}}}}}}}}
	}`)
	target, _ := openapi.ParseVersion("2.0")
	result, err := convert.To(
		context.Background(), source, target, convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	root := result.Document().Raw()
	parameters, _ := lookup(t, root, "paths", "/pets", "post", "parameters").Elements()
	if len(parameters) != 1 || text(t, lookup(t, parameters[0], "name")) != "name" ||
		text(t, lookup(t, parameters[0], "in")) != "formData" {
		t.Fatalf("parameters = %#v", parameters)
	}
	if len(result.Diagnostics()) != 1 ||
		result.Diagnostics()[0].Pointer != "/components/requestBodies/Form" ||
		result.Diagnostics()[0].Kind != convert.ManualAction {
		t.Fatalf("diagnostics = %#v", result.Diagnostics())
	}
	report, err := validate.Document(context.Background(), result.Document())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() {
		t.Fatalf("validation diagnostics = %#v", report.Diagnostics())
	}
}

func TestConvertOpenAPI30ToSwagger20ParameterEdges(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.0.4","info":{"title":"API","version":"1"},
		"paths":{"/pets/{ids}":{"get":{"parameters":[
			{"name":"ids","in":"path","required":true,
				"schema":{"type":"array","items":{"type":"string"}}},
			{"name":"tags","in":"query",
				"schema":{"type":"array","items":{"type":"string"}}},
			{"name":"filter","in":"query","content":{"application/json":{
				"schema":{"type":"string"}}}},
			{"name":"session","in":"cookie","schema":{"type":"string"}}
		],"responses":{"204":{"description":"OK"}}}}}
	}`)
	target, _ := openapi.ParseVersion("2.0")
	result, err := convert.To(
		context.Background(), source, target, convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	parameters, _ := lookup(
		t, result.Document().Raw(), "paths", "/pets/{ids}", "get", "parameters",
	).Elements()
	if len(parameters) != 3 {
		t.Fatalf("parameters = %#v", parameters)
	}
	if text(t, lookup(t, parameters[0], "collectionFormat")) != "csv" {
		t.Fatalf("path parameter = %#v", parameters[0])
	}
	if text(t, lookup(t, parameters[1], "collectionFormat")) != "multi" {
		t.Fatalf("query array parameter = %#v", parameters[1])
	}
	if text(t, lookup(t, parameters[2], "type")) != "string" {
		t.Fatalf("content parameter = %#v", parameters[2])
	}
	want := map[string]convert.DiagnosticKind{
		"/paths/~1pets~1{ids}/get/parameters/2/content": convert.Loss,
		"/paths/~1pets~1{ids}/get/parameters/3/in":      convert.Loss,
	}
	if len(result.Diagnostics()) != len(want) {
		t.Fatalf("diagnostics = %#v", result.Diagnostics())
	}
	for _, diagnostic := range result.Diagnostics() {
		if kind, exists := want[diagnostic.Pointer]; !exists || kind != diagnostic.Kind {
			t.Fatalf("diagnostic = %#v", diagnostic)
		}
	}
	report, err := validate.Document(context.Background(), result.Document())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() {
		t.Fatalf("validation diagnostics = %#v", report.Diagnostics())
	}
}

func TestConvertOpenAPI30DropsUnsupportedReusableParameters(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.0.4","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{"parameters":[
			{"$ref":"#/components/parameters/Session"},
			{"name":"filter","in":"query","schema":{"type":"object"}}
		],"responses":{"204":{"description":"OK"}}}}},
		"components":{"parameters":{"Session":{"name":"session","in":"cookie",
			"schema":{"type":"string"}}}}
	}`)
	target, _ := openapi.ParseVersion("2.0")
	result, err := convert.To(
		context.Background(), source, target, convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	root := result.Document().Raw()
	if componentParameters, exists := root.Lookup("parameters"); exists {
		if members, _ := componentParameters.Members(); len(members) != 0 {
			t.Fatalf("component parameters = %#v", members)
		}
	}
	parameters, _ := lookup(t, root, "paths", "/pets", "get", "parameters").Elements()
	if len(parameters) != 0 {
		t.Fatalf("operation parameters = %#v", parameters)
	}
	want := map[string]struct{}{
		"/components/parameters/Session/in":     {},
		"/paths/~1pets/get/parameters/0/$ref":   {},
		"/paths/~1pets/get/parameters/1/schema": {},
	}
	if len(result.Diagnostics()) != len(want) {
		t.Fatalf("diagnostics = %#v", result.Diagnostics())
	}
	for _, diagnostic := range result.Diagnostics() {
		if _, exists := want[diagnostic.Pointer]; !exists {
			t.Fatalf("diagnostic = %#v", diagnostic)
		}
	}
	report, err := validate.Document(context.Background(), result.Document())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() {
		t.Fatalf("validation diagnostics = %#v", report.Diagnostics())
	}
}

func TestConvertOpenAPI30ToSwagger20ReusableComponents(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.0.4","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"post":{
			"parameters":[{"$ref":"#/components/parameters/Trace"}],
			"requestBody":{"$ref":"#/components/requestBodies/Payload"},
			"responses":{"200":{"$ref":"#/components/responses/Result"}},
			"security":[{"Basic":[]},{"Key":[]}]}}},
		"components":{
			"schemas":{"Pet":{"type":"object"}},
			"parameters":{"Trace":{"name":"X-Trace","in":"header",
				"schema":{"type":"string"}}},
			"requestBodies":{"Payload":{"required":true,"content":{
				"application/json":{"schema":{"$ref":"#/components/schemas/Pet"}}}}},
			"responses":{"Result":{"description":"OK","content":{
				"application/json":{"schema":{"$ref":"#/components/schemas/Pet"}}}}},
			"securitySchemes":{
				"Basic":{"type":"http","scheme":"basic"},
				"Key":{"type":"apiKey","name":"X-Key","in":"header"}
			}
		}
	}`)
	target, _ := openapi.ParseVersion("2.0")
	result, err := convert.To(
		context.Background(), source, target, convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	root := result.Document().Raw()
	if text(t, lookup(t, root, "parameters", "Trace", "type")) != "string" {
		t.Fatalf("parameter components = %#v", root)
	}
	if text(t, lookup(t, root, "parameters", "Payload", "in")) != "body" ||
		text(t, lookup(t, root, "parameters", "Payload", "schema", "$ref")) !=
			"#/definitions/Pet" {
		t.Fatalf("request body components = %#v", root)
	}
	if text(t, lookup(t, root, "responses", "Result", "schema", "$ref")) !=
		"#/definitions/Pet" {
		t.Fatalf("response components = %#v", root)
	}
	if text(t, lookup(t, root, "securityDefinitions", "Basic", "type")) !=
		"basic" {
		t.Fatalf("security components = %#v", root)
	}
	operation := lookup(t, root, "paths", "/pets", "post")
	parameters, _ := lookup(t, operation, "parameters").Elements()
	if len(parameters) != 2 ||
		text(t, lookup(t, parameters[0], "$ref")) != "#/parameters/Trace" ||
		text(t, lookup(t, parameters[1], "$ref")) != "#/parameters/Payload" {
		t.Fatalf("operation parameters = %#v", parameters)
	}
	if text(t, lookup(t, operation, "responses", "200", "$ref")) !=
		"#/responses/Result" {
		t.Fatalf("operation responses = %#v", operation)
	}
	if len(result.Diagnostics()) != 0 {
		t.Fatalf("diagnostics = %#v", result.Diagnostics())
	}
	report, err := validate.Document(context.Background(), result.Document())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() {
		t.Fatalf("validation diagnostics = %#v", report.Diagnostics())
	}
}

func TestConvertOpenAPI30RenamesCollidingSwaggerParameters(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.0.4","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"post":{
			"parameters":[{"$ref":"#/components/parameters/Common"}],
			"requestBody":{"$ref":"#/components/requestBodies/Common"},
			"responses":{"204":{"description":"OK"}}}}},
		"components":{
			"parameters":{"Common":{"name":"X-Trace","in":"header",
				"schema":{"type":"string"}}},
			"requestBodies":{"Common":{"content":{"application/json":{
				"schema":{"type":"object"}}}}}
		}
	}`)
	target, _ := openapi.ParseVersion("2.0")
	result, err := convert.To(
		context.Background(), source, target, convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	root := result.Document().Raw()
	if _, exists := lookup(t, root, "parameters").Lookup("CommonRequestBody"); !exists {
		t.Fatalf("parameters = %#v", lookup(t, root, "parameters"))
	}
	parameters, _ := lookup(t, root, "paths", "/pets", "post", "parameters").Elements()
	if text(t, lookup(t, parameters[1], "$ref")) !=
		"#/parameters/CommonRequestBody" {
		t.Fatalf("operation parameters = %#v", parameters)
	}
	if len(result.Diagnostics()) != 1 ||
		result.Diagnostics()[0].Kind != convert.ManualAction ||
		result.Diagnostics()[0].Pointer !=
			"/components/requestBodies/Common" {
		t.Fatalf("diagnostics = %#v", result.Diagnostics())
	}
	report, err := validate.Document(context.Background(), result.Document())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() {
		t.Fatalf("validation diagnostics = %#v", report.Diagnostics())
	}
}

func TestConvertOpenAPI31And32ToSwagger20(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		document    string
		diagnostics int
	}{
		{
			name: "OpenAPI 3.1",
			document: `{"openapi":"3.1.2","info":{"title":"API","version":"1"},
				"paths":{},"components":{"schemas":{"Pet":{"type":"string"}}}}`,
		},
		{
			name: "OpenAPI 3.2",
			document: `{"openapi":"3.2.0","info":{"title":"API","version":"1"},
				"paths":{},"components":{"schemas":{"Pet":{"type":"string"}}}}`,
			diagnostics: 1,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			target, _ := openapi.ParseVersion("2.0")
			result, err := convert.To(
				context.Background(), mustDocument(t, test.document), target,
				convert.DefaultOptions(),
			)
			if err != nil {
				t.Fatal(err)
			}
			if result.Document().SpecificationVersion().String() != "2.0" {
				t.Fatalf("version = %s", result.Document().SpecificationVersion())
			}
			if text(t, lookup(t, result.Document().Raw(), "definitions", "Pet", "type")) !=
				"string" {
				t.Fatalf("document = %#v", result.Document().Raw())
			}
			if len(result.Diagnostics()) != test.diagnostics {
				t.Fatalf("diagnostics = %#v", result.Diagnostics())
			}
			report, err := validate.Document(context.Background(), result.Document())
			if err != nil {
				t.Fatal(err)
			}
			if !report.Valid() {
				t.Fatalf("validation diagnostics = %#v", report.Diagnostics())
			}
		})
	}
}

func TestConvertOpenAPI30ToSwagger20SecuritySchemes(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.0.4","info":{"title":"API","version":"1"},"paths":{},
		"security":[{"OAuth":[]},{"Bearer":[]}],
		"components":{"securitySchemes":{
			"OAuth":{"type":"oauth2","flows":{"implicit":{
				"authorizationUrl":"https://auth.example.test/authorize",
				"scopes":{"read":"Read data"}}}},
			"Bearer":{"type":"http","scheme":"bearer"}
		}}
	}`)
	target, _ := openapi.ParseVersion("2.0")
	result, err := convert.To(
		context.Background(), source, target, convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	security := lookup(t, result.Document().Raw(), "securityDefinitions")
	if text(t, lookup(t, security, "OAuth", "flow")) != "implicit" ||
		text(t, lookup(t, security, "OAuth", "authorizationUrl")) !=
			"https://auth.example.test/authorize" {
		t.Fatalf("OAuth scheme = %#v", security)
	}
	if _, exists := security.Lookup("Bearer"); exists {
		t.Fatal("unsupported bearer scheme was retained")
	}
	requirements, _ := lookup(t, result.Document().Raw(), "security").Elements()
	if len(requirements) != 1 {
		t.Fatalf("security requirements = %#v", requirements)
	}
	if len(result.Diagnostics()) != 2 {
		t.Fatalf("diagnostics = %#v", result.Diagnostics())
	}
	want := map[string]struct{}{
		"/components/securitySchemes/Bearer/scheme": {},
		"/security/1/Bearer":                        {},
	}
	for _, diagnostic := range result.Diagnostics() {
		if _, exists := want[diagnostic.Pointer]; !exists {
			t.Fatalf("diagnostic = %#v", diagnostic)
		}
	}
	report, err := validate.Document(context.Background(), result.Document())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() {
		t.Fatalf("validation diagnostics = %#v", report.Diagnostics())
	}
}

func TestConvertOpenAPI30ToSwagger20ResponseDetails(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.0.4","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{"responses":{"200":{
			"description":"OK","headers":{"X-Rate":{"description":"Remaining",
				"schema":{"type":"integer","format":"int32"}}},
			"content":{"application/json":{"schema":{"type":"string"},
				"example":"ready"}},"links":{"next":{"operationId":"listPets"}}
		}}}}}
	}`)
	target, _ := openapi.ParseVersion("2.0")
	result, err := convert.To(
		context.Background(), source, target, convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	response := lookup(
		t, result.Document().Raw(), "paths", "/pets", "get", "responses", "200",
	)
	if text(t, lookup(t, response, "headers", "X-Rate", "type")) != "integer" {
		t.Fatalf("headers = %#v", response)
	}
	if text(t, lookup(t, response, "examples", "application/json")) != "ready" {
		t.Fatalf("examples = %#v", response)
	}
	if _, exists := response.Lookup("links"); exists {
		t.Fatal("response links were retained")
	}
	if len(result.Diagnostics()) != 1 ||
		result.Diagnostics()[0].Pointer !=
			"/paths/~1pets/get/responses/200/links" {
		t.Fatalf("diagnostics = %#v", result.Diagnostics())
	}
	report, err := validate.Document(context.Background(), result.Document())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() {
		t.Fatalf("validation diagnostics = %#v", report.Diagnostics())
	}
}

func TestConvertReturnsDefensiveDiagnostics(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.1.2",
		"info":{"title":"API","version":"1"},
		"paths":{}
	}`)
	target, _ := openapi.ParseVersion("3.2.0")
	result, err := convert.To(
		context.Background(), source, target, convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := result.Diagnostics()
	diagnostics[0].Code = "changed"
	if result.Diagnostics()[0].Code == "changed" {
		t.Fatal("Diagnostics exposed mutable result storage")
	}
}

func TestConvertRejectsInvalidOrUnsupportedRequests(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"openapi":"3.2.0",
		"info":{"title":"API","version":"1"},
		"paths":{}
	}`)
	oas31, _ := openapi.ParseVersion("3.1.2")
	tests := []struct {
		name    string
		ctx     context.Context
		source  openapi.Document
		target  openapi.Version
		options convert.Options
		want    error
	}{
		{"nil context", nil, source, oas31, convert.DefaultOptions(), convert.ErrInvalidInput},
		{"nil document", context.Background(), nil, oas31, convert.DefaultOptions(), convert.ErrInvalidInput},
		{"zero target", context.Background(), source, openapi.Version{}, convert.DefaultOptions(), convert.ErrInvalidInput},
		{"negative limit", context.Background(), source, oas31, convert.Options{MaxRootMembers: -1}, convert.ErrInvalidOptions},
		{"negative document limit", context.Background(), source, oas31, convert.Options{MaxDocumentNodes: -1}, convert.ErrInvalidOptions},
		{"negative schema limit", context.Background(), source, oas31, convert.Options{MaxSchemaNodes: -1}, convert.ErrInvalidOptions},
		{"member limit", context.Background(), source, oas31, convert.Options{MaxRootMembers: 1}, convert.ErrLimitExceeded},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := convert.To(test.ctx, test.source, test.target, test.options)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
	exact := convert.DefaultOptions()
	exact.MaxRootMembers = 3
	if _, err := convert.To(
		context.Background(), source, oas31, exact,
	); err != nil {
		t.Fatalf("exact root-member limit error = %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := convert.To(
		cancelled, source, source.SpecificationVersion(), convert.DefaultOptions(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled error = %v", err)
	}
}

func TestConvertExactVersionReturnsOriginalDocument(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"swagger":"2.0",
		"info":{"title":"API","version":"1"},
		"paths":{}
	}`)
	result, err := convert.To(
		context.Background(),
		source,
		source.SpecificationVersion(),
		convert.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Document(), source) ||
		!reflect.DeepEqual(result.Source(), source) {
		t.Fatal("exact conversion did not retain the original document")
	}
	if _, err := convert.To(
		context.Background(), source, source.SpecificationVersion(), convert.Options{},
	); err != nil {
		t.Fatalf("zero options did not select defaults: %v", err)
	}
}

func TestConvertSwaggerToEveryOpenAPILine(t *testing.T) {
	t.Parallel()

	source := mustDocument(t, `{
		"swagger":"2.0",
		"info":{"title":"API","version":"1"},
		"paths":{},
		"definitions":{"Upload":{"type":"file"}}
	}`)
	tests := []struct {
		target      string
		diagnostics int
	}{
		{"3.0.4", 0},
		{"3.1.2", 0},
		{"3.2.0", 1},
	}
	for _, test := range tests {
		t.Run(test.target, func(t *testing.T) {
			t.Parallel()
			target, _ := openapi.ParseVersion(test.target)
			result, err := convert.To(
				context.Background(), source, target, convert.DefaultOptions(),
			)
			if err != nil {
				t.Fatal(err)
			}
			if result.Document().SpecificationVersion().String() != test.target {
				t.Fatalf("version = %s", result.Document().SpecificationVersion())
			}
			upload := lookup(
				t, result.Document().Raw(), "components", "schemas", "Upload",
			)
			if text(t, lookup(t, upload, "type")) != "string" ||
				text(t, lookup(t, upload, "format")) != "binary" {
				t.Fatalf("upload schema = %#v", upload)
			}
			if len(result.Diagnostics()) != test.diagnostics {
				t.Fatalf("diagnostics = %#v", result.Diagnostics())
			}
			report, err := validate.Document(context.Background(), result.Document())
			if err != nil {
				t.Fatal(err)
			}
			if !report.Valid() {
				t.Fatalf("validation diagnostics = %#v", report.Diagnostics())
			}
		})
	}
}

func TestConvertRejectsDocumentImplementationsWithInvalidRawRoots(t *testing.T) {
	t.Parallel()

	sourceVersion, _ := openapi.ParseVersion("3.1.0")
	target, _ := openapi.ParseVersion("3.1.2")
	missingMarker, _ := jsonvalue.Object(nil)
	if _, err := convert.To(
		context.Background(),
		stubDocument{version: sourceVersion, raw: missingMarker},
		target,
		convert.DefaultOptions(),
	); !errors.Is(err, convert.ErrInvalidInput) {
		t.Fatalf("missing marker error = %v", err)
	}

	openAPIMarker, _ := jsonvalue.String("3.1.0")
	swaggerMarker, _ := jsonvalue.String("2.0")
	ambiguousRoot, _ := jsonvalue.Object([]jsonvalue.Member{
		{Name: "openapi", Value: openAPIMarker},
		{Name: "swagger", Value: swaggerMarker},
	})
	if _, err := convert.To(
		context.Background(),
		stubDocument{version: sourceVersion, raw: ambiguousRoot},
		target,
		convert.DefaultOptions(),
	); !errors.Is(err, openapi.ErrInvalidDocument) {
		t.Fatalf("ambiguous root error = %v", err)
	}
}

type stubDocument struct {
	version openapi.Version
	raw     jsonvalue.Value
}

func (document stubDocument) Raw() jsonvalue.Value {
	return document.raw
}

func (document stubDocument) SpecificationVersion() openapi.Version {
	return document.version
}

func mustDocument(t *testing.T, source string) openapi.Document {
	t.Helper()
	document, err := openapi.ParseJSON(
		context.Background(), strings.NewReader(source), parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func lookup(t *testing.T, value jsonvalue.Value, names ...string) jsonvalue.Value {
	t.Helper()
	for _, name := range names {
		var exists bool
		value, exists = value.Lookup(name)
		if !exists {
			t.Fatalf("missing member %q", name)
		}
	}
	return value
}

func text(t *testing.T, value jsonvalue.Value) string {
	t.Helper()
	result, valid := value.Text()
	if !valid {
		t.Fatalf("value kind = %v, want string", value.Kind())
	}
	return result
}
