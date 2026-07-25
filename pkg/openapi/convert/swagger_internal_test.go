package convert

import (
	"context"
	"errors"
	"strings"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestConvertSwaggerRootAndComponents(t *testing.T) {
	t.Parallel()

	source := swaggerDocument(t, `{
		"swagger":"2.0",
		"info":{"title":"API","version":"1"},
		"host":"api.example.test",
		"basePath":"/v1",
		"schemes":["https","http"],
		"paths":{},
		"definitions":{"Pet":{
			"type":"object",
			"discriminator":"kind",
			"properties":{
				"kind":{"type":"string"},
				"photo":{"type":"file"},
				"owner":{"$ref":"#/definitions/Owner"}
			}
		},"Owner":{"type":"string"}},
		"securityDefinitions":{
			"Basic":{"type":"basic","description":"Basic auth"},
			"Key":{"type":"apiKey","name":"X-Key","in":"header"},
			"OAuth":{"type":"oauth2","flow":"accessCode",
				"authUrl":"https://auth.example.test/authorize",
				"tokenUrl":"https://auth.example.test/token",
				"scopes":{"read":"Read pets"}}
		},
		"x-root":{"exact":-0.00e+2}
	}`)
	target, _ := openapi.ParseVersion("3.0.4")
	converted, diagnostics, err := convertSwagger20Root(
		context.Background(), source.Raw(), target, 1_000, 1_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if _, exists := converted.Lookup("swagger"); exists {
		t.Fatal("Swagger marker was retained")
	}
	if textValue(t, memberAt(t, converted, "openapi")) != "3.0.4" {
		t.Fatal("target marker was not written")
	}
	servers, _ := memberAt(t, converted, "servers").Elements()
	if len(servers) != 2 ||
		textValue(t, memberAt(t, servers[0], "url")) !=
			"https://api.example.test/v1" ||
		textValue(t, memberAt(t, servers[1], "url")) !=
			"http://api.example.test/v1" {
		t.Fatalf("servers = %#v", servers)
	}
	pet := memberAt(t, converted, "components", "schemas", "Pet")
	discriminator := memberAt(t, pet, "discriminator")
	if textValue(t, memberAt(t, discriminator, "propertyName")) != "kind" {
		t.Fatalf("discriminator = %#v", discriminator)
	}
	photo := memberAt(t, pet, "properties", "photo")
	if textValue(t, memberAt(t, photo, "type")) != "string" ||
		textValue(t, memberAt(t, photo, "format")) != "binary" {
		t.Fatalf("file schema = %#v", photo)
	}
	owner := memberAt(t, pet, "properties", "owner", "$ref")
	if textValue(t, owner) != "#/components/schemas/Owner" {
		t.Fatalf("owner reference = %#v", owner)
	}
	basic := memberAt(t, converted, "components", "securitySchemes", "Basic")
	if textValue(t, memberAt(t, basic, "type")) != "http" ||
		textValue(t, memberAt(t, basic, "scheme")) != "basic" ||
		textValue(t, memberAt(t, basic, "description")) != "Basic auth" {
		t.Fatalf("basic security = %#v", basic)
	}
	key := memberAt(t, converted, "components", "securitySchemes", "Key")
	if textValue(t, memberAt(t, key, "type")) != "apiKey" ||
		textValue(t, memberAt(t, key, "name")) != "X-Key" ||
		textValue(t, memberAt(t, key, "in")) != "header" {
		t.Fatalf("API key security = %#v", key)
	}
	oauth := memberAt(t, converted, "components", "securitySchemes", "OAuth")
	flow := memberAt(t, oauth, "flows", "authorizationCode")
	if textValue(t, memberAt(t, flow, "authorizationUrl")) !=
		"https://auth.example.test/authorize" ||
		textValue(t, memberAt(t, flow, "tokenUrl")) !=
			"https://auth.example.test/token" {
		t.Fatalf("OAuth flow = %#v", flow)
	}
	if textValue(t, memberAt(t, flow, "scopes", "read")) != "Read pets" {
		t.Fatalf("OAuth scopes = %#v", flow)
	}
	exact, _ := memberAt(t, converted, "x-root", "exact").NumberText()
	if exact != "-0.00e+2" {
		t.Fatalf("exact extension number = %q", exact)
	}
}

func TestConvertSwaggerSchemasRecursivelyAndEnforcesLimit(t *testing.T) {
	t.Parallel()

	source := swaggerDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},"paths":{},
		"definitions":{"Root":{"allOf":[
			{"$ref":"#/definitions/Base"},
			{"type":"array","items":{"$ref":"#/definitions/Item"}},
			{"type":"object","additionalProperties":{
				"type":"file","format":"legacy"
			}}
		]},"Base":{"type":"object"},"Item":{"type":"string"}}
	}`)
	target, _ := openapi.ParseVersion("3.0.4")
	converted, _, err := convertSwagger20Root(
		context.Background(), source.Raw(), target, 1_000, 100,
	)
	if err != nil {
		t.Fatal(err)
	}
	root := memberAt(t, converted, "components", "schemas", "Root")
	allOf, _ := memberAt(t, root, "allOf").Elements()
	if textValue(t, memberAt(t, allOf[0], "$ref")) !=
		"#/components/schemas/Base" {
		t.Fatalf("allOf reference = %#v", allOf[0])
	}
	if textValue(t, memberAt(t, allOf[1], "items", "$ref")) !=
		"#/components/schemas/Item" {
		t.Fatalf("items reference = %#v", allOf[1])
	}
	additional := memberAt(t, allOf[2], "additionalProperties")
	if textValue(t, memberAt(t, additional, "type")) != "string" ||
		textValue(t, memberAt(t, additional, "format")) != "binary" {
		t.Fatalf("additional properties = %#v", additional)
	}
	_, _, err = convertSwagger20Root(
		context.Background(), source.Raw(), target, 1_000, 1,
	)
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("schema limit error = %v", err)
	}
}

func TestConvertSwaggerServerForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		url    string
		exists bool
	}{
		{"network path", `{"host":"api.example.test","basePath":"/v1"}`,
			"//api.example.test/v1", true},
		{"relative path", `{"basePath":"/v1"}`, "/v1", true},
		{"absent", `{}`, "", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := swaggerDocument(t, `{"swagger":"2.0",`+
				`"info":{"title":"API","version":"1"},"paths":{},`+
				`"x-input":`+test.source+`}`).Raw()
			input := memberAt(t, root, "x-input")
			servers, exists := (&swagger20Converter{}).servers(input)
			if exists != test.exists {
				t.Fatalf("servers exist = %t", exists)
			}
			if !exists {
				return
			}
			elements, _ := servers.Elements()
			if len(elements) != 1 ||
				textValue(t, memberAt(t, elements[0], "url")) != test.url {
				t.Fatalf("servers = %#v", elements)
			}
		})
	}
}

func TestConvertSwaggerOAuthFlows(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"implicit":    "implicit",
		"password":    "password",
		"application": "clientCredentials",
		"accessCode":  "authorizationCode",
	}
	for swaggerFlow, openAPIFlow := range tests {
		t.Run(swaggerFlow, func(t *testing.T) {
			t.Parallel()
			root := swaggerDocument(t, `{
				"swagger":"2.0","info":{"title":"API","version":"1"},
				"paths":{},"x-scheme":{"type":"oauth2","flow":"`+
				swaggerFlow+`","scopes":{},"x-provider":"example"}
			}`).Raw()
			converter := swagger20Converter{}
			converted, err := converter.oauth2SecurityScheme(
				memberAt(t, root, "x-scheme"), "/securityDefinitions/OAuth",
			)
			if err != nil {
				t.Fatal(err)
			}
			memberAt(t, converted, "flows", openAPIFlow)
			if textValue(t, memberAt(t, converted, "x-provider")) != "example" {
				t.Fatalf("security scheme = %#v", converted)
			}
		})
	}
}

func TestConvertSwaggerHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	target, _ := openapi.ParseVersion("3.0.4")
	_, _, err := convertSwagger20Root(
		ctx,
		swaggerDocument(t, `{
			"swagger":"2.0","info":{"title":"API","version":"1"},
			"paths":{}
		}`).Raw(),
		target,
		100,
		100,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled conversion error = %v", err)
	}
}

func TestConvertSwaggerOperationsAndReusableComponents(t *testing.T) {
	t.Parallel()

	source := swaggerDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"host":"api.example.test","basePath":"/v1",
		"schemes":["http"],
		"consumes":["application/json"],
		"produces":["application/json"],
		"parameters":{
			"Trace":{"name":"X-Trace","in":"header","type":"string"},
			"Body":{"name":"payload","in":"body","required":true,
				"schema":{"$ref":"#/definitions/Pet"}}
		},
		"responses":{"Missing":{"description":"Missing",
			"schema":{"$ref":"#/definitions/Error"}}},
		"paths":{"/pets":{
			"parameters":[{"$ref":"#/parameters/Trace"}],
			"post":{
				"schemes":["https"],
				"parameters":[
					{"$ref":"#/parameters/Trace"},
					{"$ref":"#/parameters/Body"},
					{"name":"tags","in":"query","type":"array",
						"items":{"type":"string"},"collectionFormat":"multi"}
				],
				"responses":{
					"200":{"description":"OK","schema":{"type":"array",
						"items":{"$ref":"#/definitions/Pet"}},
						"examples":{"application/json":{"id":1}},
						"headers":{"X-Rate":{"type":"integer","format":"int32"}}},
					"404":{"$ref":"#/responses/Missing"}
				}
			}
		}},
		"definitions":{"Pet":{"type":"object"},"Error":{"type":"object"}}
	}`)
	target, _ := openapi.ParseVersion("3.0.4")
	converted, diagnostics, err := convertSwagger20Root(
		context.Background(), source.Raw(), target, 1_000, 1_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if _, exists := converted.Lookup("consumes"); exists {
		t.Fatal("root consumes was retained")
	}
	if _, exists := converted.Lookup("produces"); exists {
		t.Fatal("root produces was retained")
	}
	trace := memberAt(t, converted, "components", "parameters", "Trace")
	if textValue(t, memberAt(t, trace, "schema", "type")) != "string" {
		t.Fatalf("reusable parameter = %#v", trace)
	}
	body := memberAt(t, converted, "components", "requestBodies", "Body")
	if required, _ := memberAt(t, body, "required").Bool(); !required {
		t.Fatalf("reusable request body = %#v", body)
	}
	if textValue(t, memberAt(
		t, body, "content", "application/json", "schema", "$ref",
	)) != "#/components/schemas/Pet" {
		t.Fatalf("request body = %#v", body)
	}
	missing := memberAt(t, converted, "components", "responses", "Missing")
	if textValue(t, memberAt(
		t, missing, "content", "application/json", "schema", "$ref",
	)) != "#/components/schemas/Error" {
		t.Fatalf("reusable response = %#v", missing)
	}
	pathParameters, _ := memberAt(
		t, converted, "paths", "/pets", "parameters",
	).Elements()
	if textValue(t, memberAt(t, pathParameters[0], "$ref")) !=
		"#/components/parameters/Trace" {
		t.Fatalf("path parameter = %#v", pathParameters[0])
	}
	operation := memberAt(t, converted, "paths", "/pets", "post")
	parameters, _ := memberAt(t, operation, "parameters").Elements()
	if len(parameters) != 2 ||
		textValue(t, memberAt(t, parameters[0], "$ref")) !=
			"#/components/parameters/Trace" {
		t.Fatalf("operation parameters = %#v", parameters)
	}
	query := parameters[1]
	if textValue(t, memberAt(t, query, "style")) != "form" {
		t.Fatalf("query parameter = %#v", query)
	}
	if explode, _ := memberAt(t, query, "explode").Bool(); !explode {
		t.Fatalf("query parameter = %#v", query)
	}
	if textValue(t, memberAt(t, operation, "requestBody", "$ref")) !=
		"#/components/requestBodies/Body" {
		t.Fatalf("operation request body = %#v", operation)
	}
	servers, _ := memberAt(t, operation, "servers").Elements()
	if len(servers) != 1 ||
		textValue(t, memberAt(t, servers[0], "url")) !=
			"https://api.example.test/v1" {
		t.Fatalf("operation servers = %#v", servers)
	}
	response := memberAt(t, operation, "responses", "200")
	if textValue(t, memberAt(
		t, response, "content", "application/json", "schema", "items", "$ref",
	)) != "#/components/schemas/Pet" {
		t.Fatalf("response schema = %#v", response)
	}
	if number, _ := memberAt(
		t, response, "content", "application/json", "example", "id",
	).NumberText(); number != "1" {
		t.Fatalf("response example = %#v", response)
	}
	if textValue(t, memberAt(
		t, response, "headers", "X-Rate", "schema", "type",
	)) != "integer" {
		t.Fatalf("response header = %#v", response)
	}
	if textValue(t, memberAt(t, operation, "responses", "404", "$ref")) !=
		"#/components/responses/Missing" {
		t.Fatalf("response reference = %#v", operation)
	}
	assertValidConverted(t, converted)
}

func TestConvertSwaggerFormDataToRequestBody(t *testing.T) {
	t.Parallel()

	source := swaggerDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"paths":{"/upload":{"post":{
			"consumes":["multipart/form-data"],
			"parameters":[
				{"name":"photo","in":"formData","required":true,
					"description":"Pet photo","type":"file","x-upload":"image"},
				{"name":"labels","in":"formData","type":"array",
					"items":{"type":"string"},"collectionFormat":"multi"}
			],
			"responses":{"204":{"description":"Uploaded"}}
		}}}
	}`)
	target, _ := openapi.ParseVersion("3.0.4")
	converted, diagnostics, err := convertSwagger20Root(
		context.Background(), source.Raw(), target, 1_000, 1_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	operation := memberAt(t, converted, "paths", "/upload", "post")
	if _, exists := operation.Lookup("parameters"); exists {
		t.Fatal("form parameters were retained")
	}
	body := memberAt(t, operation, "requestBody")
	if required, _ := memberAt(t, body, "required").Bool(); !required {
		t.Fatalf("request body = %#v", body)
	}
	media := memberAt(t, body, "content", "multipart/form-data")
	photo := memberAt(t, media, "schema", "properties", "photo")
	if textValue(t, memberAt(t, photo, "type")) != "string" ||
		textValue(t, memberAt(t, photo, "format")) != "binary" ||
		textValue(t, memberAt(t, photo, "description")) != "Pet photo" ||
		textValue(t, memberAt(t, photo, "x-upload")) != "image" {
		t.Fatalf("photo property = %#v", photo)
	}
	required, _ := memberAt(t, media, "schema", "required").Elements()
	if len(required) != 1 || textValue(t, required[0]) != "photo" {
		t.Fatalf("required properties = %#v", required)
	}
	encoding := memberAt(t, media, "encoding", "labels")
	if textValue(t, memberAt(t, encoding, "style")) != "form" {
		t.Fatalf("labels encoding = %#v", encoding)
	}
	if explode, _ := memberAt(t, encoding, "explode").Bool(); !explode {
		t.Fatalf("labels encoding = %#v", encoding)
	}
	assertValidConverted(t, converted)
}

func TestConvertSwaggerReportsMigrationDecisions(t *testing.T) {
	t.Parallel()

	source := swaggerDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"schemes":["https"],
		"paths":{"/pets":{"post":{
			"parameters":[
				{"name":"first","in":"body","schema":{"type":"object"}},
				{"name":"second","in":"body","schema":{"type":"object"}},
				{"name":"form","in":"formData","type":"string"},
				{"name":"values","in":"query","type":"array",
					"items":{"type":"string"},"collectionFormat":"tsv"},
				{"name":"headerValues","in":"header","type":"array",
					"items":{"type":"string"},"collectionFormat":"ssv"}
			],
			"responses":{"200":{"description":"OK","schema":{"type":"object"}}}
		}}}
	}`)
	target, _ := openapi.ParseVersion("3.0.4")
	_, diagnostics, err := convertSwagger20Root(
		context.Background(), source.Raw(), target, 1_000, 1_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]DiagnosticKind{
		"/schemes":                                         Loss,
		"/paths/~1pets/post/parameters/0/schema":           ManualAction,
		"/paths/~1pets/post/parameters/1":                  Loss,
		"/paths/~1pets/post/parameters":                    Loss,
		"/paths/~1pets/post/parameters/3/collectionFormat": ManualAction,
		"/paths/~1pets/post/parameters/4/collectionFormat": ManualAction,
		"/paths/~1pets/post/responses/200/schema":          ManualAction,
	}
	if len(diagnostics) != len(want) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		kind, exists := want[diagnostic.Pointer]
		if !exists || kind != diagnostic.Kind || diagnostic.Code == "" ||
			diagnostic.Message == "" {
			t.Fatalf("diagnostic = %#v", diagnostic)
		}
	}
}

func TestSwaggerParameterCollectionStylesRespectLocations(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		location    string
		format      string
		explicit    bool
		wantStyle   string
		wantExplode bool
	}{
		"default query": {"query", "", false, "form", false},
		"csv header":    {"header", "csv", true, "simple", false},
		"multi query":   {"query", "multi", true, "form", true},
		"multi form":    {"formData", "multi", true, "form", true},
		"multi header":  {"header", "multi", true, "", false},
		"ssv query":     {"query", "ssv", true, "spaceDelimited", false},
		"ssv header":    {"header", "ssv", true, "", false},
		"pipes query":   {"query", "pipes", true, "pipeDelimited", false},
		"pipes header":  {"header", "pipes", true, "", false},
		"tsv query":     {"query", "tsv", true, "", false},
	} {
		t.Run(name, func(t *testing.T) {
			style, explode := parameterCollectionStyle(
				testCase.location, testCase.format, testCase.explicit,
			)
			if style != testCase.wantStyle || explode != testCase.wantExplode {
				t.Fatalf("style = %q, explode = %t", style, explode)
			}
		})
	}
}

func TestConvertSwaggerReportsDiscardedReferenceAndSecurityData(t *testing.T) {
	t.Parallel()

	source := swaggerDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"components":{"legacy":true},
		"definitions":{"Remote":{"$ref":"remote.json#/Thing",
			"description":"ignored"}},
		"parameters":{"Shared":{"name":"q","in":"query","type":"string"}},
		"securityDefinitions":{
			"Unknown":{"type":"custom"},
			"OAuth":{"type":"oauth2","flow":"unknown"}
		},
		"paths":{"/pets":{"get":{
			"parameters":[{"$ref":"#/parameters/Shared","description":"ignored"}],
			"responses":{"200":{"$ref":"#/responses/Missing","x-note":true}}
		}}}
	}`)
	target, _ := openapi.ParseVersion("3.0.4")
	converted, diagnostics, err := convertSwagger20Root(
		context.Background(), source.Raw(), target, 1_000, 1_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]DiagnosticKind{
		"/components":                                Loss,
		"/securityDefinitions/Unknown/type":          ManualAction,
		"/securityDefinitions/OAuth/flow":            Loss,
		"/securityDefinitions/OAuth/scopes":          Loss,
		"/definitions/Remote/$ref":                   ManualAction,
		"/definitions/Remote/description":            Loss,
		"/paths/~1pets/get/parameters/0/description": Loss,
		"/paths/~1pets/get/responses/200/x-note":     Loss,
	}
	if len(diagnostics) != len(want) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if kind, exists := want[diagnostic.Pointer]; !exists || kind != diagnostic.Kind {
			t.Fatalf("diagnostic = %#v", diagnostic)
		}
	}
	if _, exists := converted.Lookup("components"); !exists {
		t.Fatal("generated components are missing")
	}
	parameter := memberAt(t, converted, "paths", "/pets", "get", "parameters")
	elements, _ := parameter.Elements()
	if _, exists := elements[0].Lookup("description"); exists {
		t.Fatal("ignored parameter reference sibling was retained")
	}
	response := memberAt(t, converted, "paths", "/pets", "get", "responses", "200")
	if _, exists := response.Lookup("x-note"); exists {
		t.Fatal("ignored response reference sibling was retained")
	}
}

func TestConvertSwaggerPathRequestParametersAreInheritedAndOverridden(t *testing.T) {
	t.Parallel()

	source := swaggerDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"consumes":["application/json"],
		"paths":{"/pets":{
			"parameters":[
				{"name":"trace","in":"query","type":"string"},
				{"name":"payload","in":"body","schema":{"type":"string"}}
			],
			"post":{"responses":{"204":{"description":"Created"}}},
			"put":{"parameters":[{"name":"payload","in":"body",
				"schema":{"type":"integer"}}],
				"responses":{"204":{"description":"Updated"}}}
		}}
	}`)
	target, _ := openapi.ParseVersion("3.0.4")
	converted, diagnostics, err := convertSwagger20Root(
		context.Background(), source.Raw(), target, 1_000, 1_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	path := memberAt(t, converted, "paths", "/pets")
	parameters, _ := memberAt(t, path, "parameters").Elements()
	if len(parameters) != 1 ||
		textValue(t, memberAt(t, parameters[0], "name")) != "trace" {
		t.Fatalf("path parameters = %#v", parameters)
	}
	postType := memberAt(
		t, path, "post", "requestBody", "content", "application/json",
		"schema", "type",
	)
	if textValue(t, postType) != "string" {
		t.Fatalf("inherited body type = %#v", postType)
	}
	putType := memberAt(
		t, path, "put", "requestBody", "content", "application/json",
		"schema", "type",
	)
	if textValue(t, putType) != "integer" {
		t.Fatalf("overridden body type = %#v", putType)
	}
	assertValidConverted(t, converted)
}

func assertValidConverted(t *testing.T, raw jsonvalue.Value) {
	t.Helper()
	document, err := openapi.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() {
		t.Fatalf("validation diagnostics = %#v", report.Diagnostics())
	}
}

func swaggerDocument(t *testing.T, source string) openapi.Document {
	t.Helper()
	document, err := openapi.ParseJSON(
		context.Background(), strings.NewReader(source), parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func memberAt(t *testing.T, value jsonvalue.Value, names ...string) jsonvalue.Value {
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

func textValue(t *testing.T, value jsonvalue.Value) string {
	t.Helper()
	text, valid := value.Text()
	if !valid {
		t.Fatalf("value kind = %v, want string", value.Kind())
	}
	return text
}
