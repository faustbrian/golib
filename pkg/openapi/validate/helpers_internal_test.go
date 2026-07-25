package validate

import (
	"context"
	"errors"
	"strings"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	openapischema "github.com/faustbrian/golib/pkg/openapi/jsonschema"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parameter"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

func TestEqualJSONValuesCoversExactJSONAlgebra(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
		want  bool
	}{
		{name: "null", left: `null`, right: `null`, want: true},
		{name: "kind", left: `null`, right: `false`},
		{name: "boolean equal", left: `true`, right: `true`, want: true},
		{name: "boolean unequal", left: `true`, right: `false`},
		{name: "number equivalent", left: `1.0`, right: `1`, want: true},
		{name: "number unequal", left: `1`, right: `2`},
		{name: "string equal", left: `"value"`, right: `"value"`, want: true},
		{name: "string unequal", left: `"one"`, right: `"two"`},
		{name: "array equal", left: `[1,true]`, right: `[1.0,true]`, want: true},
		{name: "array length", left: `[1]`, right: `[]`},
		{name: "array value", left: `[1]`, right: `[2]`},
		{name: "object equal", left: `{"a":1,"b":true}`, right: `{"b":true,"a":1.0}`, want: true},
		{name: "object length", left: `{"a":1}`, right: `{}`},
		{name: "object name", left: `{"a":1}`, right: `{"b":1}`},
		{name: "object value", left: `{"a":1}`, right: `{"a":2}`},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := equalJSONValues(
				testValidationValue(t, test.left),
				testValidationValue(t, test.right),
			); got != test.want {
				t.Fatalf("equalJSONValues() = %t, want %t", got, test.want)
			}
		})
	}
	if equalJSONValues(jsonvalue.Value{}, jsonvalue.Value{}) {
		t.Fatal("invalid values compared equal")
	}
}

func TestDirectionalSchemaHelpersTransformArraysAndLimits(t *testing.T) {
	t.Parallel()

	visited := 0
	transformed, err := transformDirectionalSchemaArray(
		testValidationValue(t, `[{
			"properties":{"visible":{"type":"string"},"secret":{"writeOnly":true}},
			"required":["visible","secret"]
		}]`),
		DirectionResponse, 16, &visited,
	)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := transformed.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"required":["visible"]`) {
		t.Fatalf("response required fields = %s", raw)
	}

	visited = 0
	if _, err := transformDirectionalSchemaArray(
		testValidationValue(t, `[{}]`), DirectionRequest, 0, &visited,
	); err != ErrLimitExceeded {
		t.Fatalf("array limit error = %v", err)
	}
	visited = 0
	nonArray := testValidationValue(t, `{}`)
	result, err := transformDirectionalSchemaArray(
		nonArray, DirectionRequest, 1, &visited,
	)
	if err != nil || result.Kind() != jsonvalue.ObjectKind {
		t.Fatalf("non-array transform = %#v, %v", result, err)
	}
}

func TestSwaggerDefinitionAndPrimitiveHelpers(t *testing.T) {
	t.Parallel()

	schema := testValidationValue(t, `{"type":"object"}`)
	root := testValidationValue(t, `{
		"definitions":{"Named":{"type":"object"},"Other":{"type":"string"}}
	}`)
	if name := swaggerDefinitionName(root, schema); name != "Named" {
		t.Fatalf("definition name = %q", name)
	}
	if name := swaggerDefinitionName(testValidationValue(t, `{}`), schema); name != "" {
		t.Fatalf("missing definition name = %q", name)
	}
	if name := swaggerDefinitionName(root, testValidationValue(t, `false`)); name != "" {
		t.Fatalf("unknown definition name = %q", name)
	}

	for _, value := range []byte{'0', '9', 'a', 'f', 'A', 'F'} {
		if !hexDigit(value) {
			t.Errorf("%q was not hexadecimal", value)
		}
	}
	for _, value := range []byte{'/', 'g', 'G'} {
		if hexDigit(value) {
			t.Errorf("%q was hexadecimal", value)
		}
	}
	for typeName, raw := range map[string]string{
		"array": `[]`, "boolean": `true`, "integer": `1`, "number": `1.5`,
		"object": `{}`, "string": `"value"`, "file": `"value"`, "unknown": `null`,
	} {
		if !swaggerValueMatchesType(typeName, testValidationValue(t, raw)) {
			t.Errorf("%s did not match %s", typeName, raw)
		}
	}
	if swaggerValueMatchesType("integer", testValidationValue(t, `1.5`)) ||
		swaggerValueMatchesType("string", testValidationValue(t, `false`)) {
		t.Fatal("mismatched Swagger values were accepted")
	}
}

func TestServerTemplateCharacterBoundaries(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"https://example.test/%20", "https://example.test/{value}",
		"https://example.test/é",
	} {
		if !validServerURLTemplate32(value) {
			t.Errorf("valid server URL %q was rejected", value)
		}
	}
	for _, value := range []string{"", "%", "%0g", "{value", "value "} {
		if validServerURLTemplate32(value) {
			t.Errorf("invalid server URL %q was accepted", value)
		}
	}
	if validServerLiteralRune(' ') || !validServerLiteralRune('!') ||
		validServerLiteralRune(0xd800) {
		t.Fatal("server literal rune boundaries were incorrect")
	}
}

func TestExampleSerializationHelpers(t *testing.T) {
	t.Parallel()

	for typeName, want := range map[string]parameter.Shape{
		"array": parameter.Array, "object": parameter.Object,
		"boolean": parameter.Primitive, "integer": parameter.Primitive,
		"number": parameter.Primitive, "string": parameter.Primitive,
	} {
		shape, ok := parameterShape(testValidationValue(
			t, `{"type":"`+typeName+`"}`,
		))
		if !ok || shape != want {
			t.Errorf("shape %s = %d, %t", typeName, shape, ok)
		}
	}
	for _, raw := range []string{`{}`, `{"type":"null"}`, `{"type":true}`} {
		if _, ok := parameterShape(testValidationValue(t, raw)); ok {
			t.Errorf("invalid parameter shape %s was accepted", raw)
		}
	}

	diagnostics := appendLeadingDelimiterDiagnostic(
		nil, serializedExample{value: "?value=1", pointer: "/example"}, "3.2.0",
	)
	if len(diagnostics) != 1 {
		t.Fatalf("leading delimiter diagnostics = %#v", diagnostics)
	}
	if got := appendLeadingDelimiterDiagnostic(
		diagnostics, serializedExample{value: "value=1"}, "3.2.0",
	); len(got) != 1 {
		t.Fatalf("plain example diagnostics = %#v", got)
	}
	if got := appendHeaderNameDiagnostic(
		nil, serializedExample{value: "X-Value: one", pointer: "/example"},
		"x-value", "3.2.0",
	); len(got) != 1 {
		t.Fatalf("header diagnostics = %#v", got)
	}
	for _, header := range []string{"", "Other"} {
		if got := appendHeaderNameDiagnostic(
			nil, serializedExample{value: "X-Value: one"}, header, "3.2.0",
		); len(got) != 0 {
			t.Errorf("header %q diagnostics = %#v", header, got)
		}
	}

	for _, test := range []struct {
		base      string
		reference string
		want      string
		valid     bool
	}{
		{reference: "https://example.test/value.json", want: "https://example.test/value.json", valid: true},
		{reference: "relative.json", want: "relative.json", valid: true},
		{base: "https://example.test/root/openapi.json", reference: "value.json", want: "https://example.test/root/value.json", valid: true},
		{base: "%", reference: "value.json"},
		{reference: "%"},
	} {
		got, valid := externalExampleIdentifier(test.base, test.reference)
		if got != test.want || valid != test.valid {
			t.Errorf("identifier(%q, %q) = %q, %t", test.base, test.reference, got, valid)
		}
	}

	for _, test := range []struct {
		raw     string
		want    []string
		present bool
	}{
		{raw: `{}`},
		{raw: `{"produces":true}`, present: true},
		{raw: `{"produces":["application/json","text/plain"]}`, want: []string{"application/json", "text/plain"}, present: true},
		{raw: `{"produces":[true]}`, present: true},
	} {
		got, present := swaggerProduces(testValidationValue(t, test.raw))
		if strings.Join(got, ",") != strings.Join(test.want, ",") || present != test.present {
			t.Errorf("swaggerProduces(%s) = %#v, %t", test.raw, got, present)
		}
	}
}

func TestReferenceClassificationHelpers(t *testing.T) {
	t.Parallel()

	limits := normalizedReferenceLimits(reference.Limits{MaxTraversalNodes: 7})
	if limits.MaxTraversalNodes != 7 || limits.MaxTraversalDepth == 0 ||
		limits.MaxReferenceDepth == 0 {
		t.Fatalf("normalized limits = %#v", limits)
	}
	defaults := normalizedReferenceLimits(reference.Limits{})
	if defaults.MaxTraversalNodes == 0 {
		t.Fatal("reference node limit was not defaulted")
	}

	for _, test := range []struct {
		tokens  []string
		dialect openapi.Dialect
		want    string
	}{
		{tokens: []string{"components", "schemas", "Value", "$ref"}, want: "schemas"},
		{tokens: []string{"components", "responses", "Value", "$ref"}, want: "responses"},
		{tokens: []string{"parameters", "Value", "$ref"}, want: "parameters"},
		{tokens: []string{"responses", "Value", "$ref"}, want: "responses"},
		{tokens: []string{"securityDefinitions", "Value", "$ref"}, want: "securitySchemes"},
		{tokens: []string{"paths", "/value", "$ref"}, want: "pathItems"},
		{tokens: []string{"paths", "/value", "get", "requestBody", "$ref"}, want: "requestBodies"},
		{tokens: []string{"paths", "/value", "get", "responses", "200", "$ref"}, want: "responses"},
		{tokens: []string{"paths", "/value", "get", "content", "application/json", "$ref"}, dialect: openapi.DialectOAS32, want: "mediaTypes"},
		{tokens: []string{"paths", "/value", "get", "content", "application/json", "$ref"}, dialect: openapi.DialectOAS31},
		{tokens: []string{"components", "schemas", "Value", "properties", "child", "$ref"}, want: "schemas"},
		{tokens: []string{"paths", "/value", "get", "properties", "child", "$ref"}, want: "schemas"},
		{tokens: []string{"unknown", "value", "$ref"}},
		{tokens: []string{"value", "$ref"}},
		{tokens: []string{"paths", "/value", "get"}},
		{tokens: []string{"x-data", "$ref"}},
	} {
		if got := referenceSourceKind(test.tokens, test.dialect); got != test.want {
			t.Errorf("referenceSourceKind(%v) = %q, want %q", test.tokens, got, test.want)
		}
	}

	for _, test := range []struct {
		raw  string
		want string
	}{
		{raw: "#/components/schemas/Value", want: "schemas"},
		{raw: "#/components/responses/Value", want: "responses"},
		{raw: "#/components/responses/Value/content/application~1json/schema", want: "schemas"},
		{raw: "#/components/responses/Value/content/application~1json"},
		{raw: "#/paths/~1value", want: "pathItems"},
		{raw: "#/definitions/Value", want: "schemas"},
		{raw: "#/parameters/Value", want: "parameters"},
		{raw: "#/parameters/Value/schema", want: "schemas"},
		{raw: "#/parameters/Value/unknown"},
		{raw: "#/securityDefinitions/Auth", want: "securitySchemes"},
		{raw: "#anchor"},
		{raw: "#/unknown/Value"},
	} {
		fragment, err := reference.ParseFragment(strings.TrimPrefix(test.raw, "#"))
		if err != nil {
			t.Fatal(err)
		}
		if got := referenceTargetKind(fragment); got != test.want {
			t.Errorf("referenceTargetKind(%s) = %q, want %q", test.raw, got, test.want)
		}
	}
	if !referenceDataPointer(
		[]string{"paths", "/value", "get", "example", "$ref"},
		openapi.DialectOAS31,
	) {
		t.Fatal("example reference data was classified as a Reference Object")
	}
	if start := schemaPointerStart([]string{"definitions", "Value", "$ref"}); start != 1 {
		t.Fatalf("Swagger schema pointer start = %d", start)
	}
}

func TestReferenceResolutionDefensiveStates(t *testing.T) {
	t.Parallel()

	want := errors.New("resolver failure")
	resolver := &validationReferenceResolver{
		resolver: reference.ResolverFunc(func(
			context.Context, string,
		) (reference.Resource, error) {
			return reference.Resource{}, want
		}),
		dialect: openapi.DialectOAS31,
	}
	if _, err := resolver.Resolve(
		context.Background(), "https://example.test/schema",
	); !errors.Is(err, want) {
		t.Fatalf("validation resolver error = %v", err)
	}
	var nilResolver reference.ResolverFunc
	if validationResolver(nilResolver, openapi.DialectOAS31) != nil {
		t.Fatal("typed nil validation resolver was wrapped")
	}
	if validationResolver(resolver, openapi.DialectOAS31) != resolver {
		t.Fatal("matching validation resolver was rewrapped")
	}

	root := testValidationValue(t, `{"scalar":1}`)
	for _, value := range []jsonvalue.Value{
		testValidationValue(t, `{"$ref":true}`),
		testValidationValue(t, `{"$ref":"#/scalar"}`),
	} {
		if _, ok := resolveReferencedSchema(context.Background(), root, value); ok {
			t.Fatalf("invalid referenced schema resolved: %#v", value)
		}
		if _, _, ok := resolveReferencedObjectResourceWithPolicy(
			context.Background(), reference.Resource{Root: root}, value, nil,
			reference.DefaultLimits(),
		); ok {
			t.Fatalf("invalid referenced object resolved: %#v", value)
		}
	}
}

func TestReferenceTargetValidationPropagatesWorkBounds(t *testing.T) {
	t.Parallel()

	version, err := openapi.ParseVersion("3.1.2")
	if err != nil {
		t.Fatal(err)
	}
	document := validationDocument{
		version: version,
		raw: testValidationValue(t, `{
			"openapi":"3.1.2",
			"paths":{"/value":{"get":{"responses":{"200":{"$ref":"#/components/responses/A"}}}}},
			"components":{"responses":{
				"A":{"$ref":"#/components/responses/B"},
				"B":{"description":"ok"}
			}}
		}`),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := validateReferenceTargets(
		ctx, document, DefaultOptions(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("scan cancellation error = %v", err)
	}

	options := DefaultOptions()
	options.ReferenceLimits.MaxReferenceDepth = 1
	if _, err := validateReferenceTargets(
		context.Background(), document, options,
	); !errors.Is(err, reference.ErrLimitExceeded) {
		t.Fatalf("reference depth error = %v", err)
	}

	invalid := validationDocument{version: version, raw: jsonvalue.Value{}}
	diagnostics, err := validateReferenceTargets(
		context.Background(), invalid, DefaultOptions(),
	)
	if err == nil || len(diagnostics) != 0 {
		t.Fatalf("invalid immutable root = %#v, %v", diagnostics, err)
	}
}

func TestPathRootAndTagHelpers(t *testing.T) {
	t.Parallel()

	for _, valid := range []string{"name", "na me", "é"} {
		if !validTemplateName(valid) {
			t.Errorf("valid template name %q was rejected", valid)
		}
	}
	for _, invalid := range []string{"", "a/b", "{a}", string([]byte{0xff})} {
		if validTemplateName(invalid) {
			t.Errorf("invalid template name %q was accepted", invalid)
		}
	}
	for _, test := range []struct {
		path       string
		normalized string
		repeated   bool
		valid      bool
	}{
		{path: "/values/{id}", normalized: "/values/{}", valid: true},
		{path: "/{id}/{id}", normalized: "/{}/{}", repeated: true, valid: true},
		{path: "/values/}"},
		{path: "/values/{id"},
		{path: "/values/{a/b}"},
	} {
		_, normalized, repeated, err := parsePathTemplate(test.path)
		if (err == nil) != test.valid || normalized != test.normalized || repeated != test.repeated {
			t.Errorf("parsePathTemplate(%q) = %q, %t, %v", test.path, normalized, repeated, err)
		}
	}

	for _, value := range []string{"", "https://example.test/openapi.json", "openapi.yaml", "%"} {
		if !standardEntryDocumentName(value) {
			t.Errorf("standard entry %q was rejected", value)
		}
	}
	if standardEntryDocumentName("https://example.test/api.json") {
		t.Fatal("non-standard entry name was accepted")
	}

	definitions := map[string]tagDefinition{
		"a": {name: "a", parent: "b"},
		"b": {name: "b", parent: "a"},
	}
	if !tagParentCycle("a", definitions) || tagParentCycle("missing", definitions) {
		t.Fatal("tag parent cycle classification failed")
	}
	if tagParentCycle("root", map[string]tagDefinition{
		"root": {name: "root"},
	}) {
		t.Fatal("terminal tag hierarchy was cyclic")
	}
}

func TestTraversalHelpersCoverMalformedContainers(t *testing.T) {
	t.Parallel()

	children := schemaChildren(schemaLocation{
		value: testValidationValue(t, `{
			"items":[{"type":"string"}]
		}`),
		pointer: "/schema",
	})
	if len(children) != 1 || children[0].pointer != "/schema/items/0" {
		t.Fatalf("tuple schema children = %#v", children)
	}
	if got := appendSchemaArray(
		nil, testValidationValue(t, `{}`), "/schema",
	); len(got) != 0 {
		t.Fatalf("non-array schemas = %#v", got)
	}

	headers, containers := appendHeaderMap(
		nil, nil,
		testValidationValue(t, `{
			"headers":{"scalar":false,"reference":{"$ref":"#/components/headers/X"}}
		}`),
		"headers", "/headers",
	)
	if len(headers) != 0 || len(containers) != 0 {
		t.Fatalf("malformed headers = %#v, %#v", headers, containers)
	}

	inventory := operationInventory{}
	inventory.callback(testValidationValue(t, `false`), "/callback")
	inventory.callback(
		testValidationValue(t, `{"$ref":"#/components/callbacks/X"}`),
		"/callback",
	)
	if len(inventory.operations) != 0 {
		t.Fatalf("malformed callback operations = %#v", inventory.operations)
	}

	if swaggerValueMatchesType("integer", jsonvalue.Boolean(true)) {
		t.Fatal("boolean matched Swagger integer")
	}
	version, err := openapi.ParseVersion("2.0")
	if err != nil {
		t.Fatal(err)
	}
	document := validationDocument{
		raw: testValidationValue(t, `{"swagger":"2.0"}`), version: version,
	}
	if diagnostics := validateSwaggerHeaders(document); len(diagnostics) != 0 {
		t.Fatalf("header diagnostics = %#v", diagnostics)
	}

	version, err = openapi.ParseVersion("3.2.0")
	if err != nil {
		t.Fatal(err)
	}
	document = validationDocument{
		raw:     testValidationValue(t, `{"openapi":"3.2.0","tags":[false]}`),
		version: version,
	}
	if diagnostics := validateTags(document); len(diagnostics) != 0 {
		t.Fatalf("scalar tag diagnostics = %#v", diagnostics)
	}
}

func TestServerValidationDefensiveStates(t *testing.T) {
	t.Parallel()

	if got := validateServer(
		testValidationValue(t, `{}`), "/server", "3.2.0", openapi.DialectOAS32,
	); len(got) != 0 {
		t.Fatalf("missing URL diagnostics = %#v", got)
	}
	if got := validateServer(
		testValidationValue(t, `{"url":"https://example.test/{"}`),
		"/server", "3.2.0", openapi.DialectOAS32,
	); len(got) != 1 || got[0].Code != "openapi.server.url.invalid-template" {
		t.Fatalf("invalid template diagnostics = %#v", got)
	}
	for _, raw := range []string{
		`false`, `{"enum":["one"]}`, `{"enum":[],"default":"one"}`,
	} {
		_ = validateServerVariable(
			testValidationValue(t, raw), "/variable", "3.2.0", openapi.DialectOAS32,
		)
	}
	if got := validateServerVariable(
		testValidationValue(t, `{"enum":["one"],"default":"one"}`),
		"/variable", "3.2.0", openapi.DialectOAS32,
	); len(got) != 0 {
		t.Fatalf("matching default diagnostics = %#v", got)
	}
	for _, value := range []string{"value}", "{value", "{}", "{{value}"} {
		if _, valid := serverTemplateVariables(value); valid {
			t.Errorf("invalid template %q was accepted", value)
		}
	}

	version, err := openapi.ParseVersion("3.2.0")
	if err != nil {
		t.Fatal(err)
	}
	document := validationDocument{
		raw:     testValidationValue(t, `{"openapi":"3.2.0","servers":[false]}`),
		version: version,
	}
	if diagnostics := validateServers(document); len(diagnostics) != 0 {
		t.Fatalf("scalar server diagnostics = %#v", diagnostics)
	}
}

func TestSecurityValidationMalformedRequirementsAndSchemes(t *testing.T) {
	t.Parallel()

	version, err := openapi.ParseVersion("3.0.4")
	if err != nil {
		t.Fatal(err)
	}
	document := validationDocument{
		raw: testValidationValue(t, `{
			"openapi":"3.0.4",
			"components":{"securitySchemes":{
				"scalar":false,
				"key":{"type":"apiKey","in":"header","name":"X-Key"},
				"oauth":{"type":"oauth2","flows":{"implicit":{
					"authorizationUrl":"https://example.test/auth",
					"scopes":{"read":"read"}
				}}},
				"oidc":{"type":"openIdConnect","openIdConnectUrl":"https://example.test/config"}
			}},
			"security":[false,{"key":true},{"key":["role"]},{"oidc":[]},{"oauth":[true,"missing"]}]
		}`),
		version: version,
	}
	diagnostics := validateSecurity(context.Background(), document, DefaultOptions())
	if len(diagnostics) < 2 {
		t.Fatalf("security diagnostics = %#v", diagnostics)
	}
	if got := validateSecurityURLFields(
		jsonvalue.Boolean(true), "/scheme", "3.0.4", SeverityError, "tokenUrl",
	); len(got) != 0 {
		t.Fatalf("scalar URL diagnostics = %#v", got)
	}
	result := map[string]struct{}{}
	collectScopeNames(result, jsonvalue.Boolean(true))
	if len(result) != 0 {
		t.Fatalf("scalar scopes = %#v", result)
	}
	emptyRoles := validationDocument{
		raw: testValidationValue(t, `{
			"openapi":"3.0.4",
			"components":{"securitySchemes":{"key":{
				"type":"apiKey","in":"header","name":"X-Key"
			}}},
			"security":[{"key":[]}]
		}`),
		version: version,
	}
	for _, diagnostic := range validateSecurity(
		context.Background(), emptyRoles, DefaultOptions(),
	) {
		if diagnostic.Code == "openapi.security.roles.not-allowed" {
			t.Fatalf("empty API key roles rejected: %#v", diagnostic)
		}
	}
}

func TestMediaAndSchemaCollectorsRejectMalformedOwners(t *testing.T) {
	t.Parallel()

	options := DefaultOptions()
	resource := reference.Resource{Root: testValidationValue(t, `{}`)}
	if describesMultipleBinaryFiles(
		context.Background(), resource, testValidationValue(t, `{}`), options,
	) || describesMultipleBinaryFiles(
		context.Background(), resource,
		testValidationValue(t, `{"schema":false}`), options,
	) {
		t.Fatal("missing or scalar schema described multiple files")
	}
	remaining := 1
	if schemaContainsBinaryFileArray(
		context.Background(), resource, jsonvalue.Boolean(true), nil,
		options.ReferenceLimits, map[string]struct{}{}, &remaining, 0,
	) {
		t.Fatal("scalar schema contained files")
	}
	remaining = 1
	if schemaContainsBinaryFileArray(
		context.Background(), resource, testValidationValue(t, `{}`), nil,
		options.ReferenceLimits, map[string]struct{}{}, &remaining, 0,
	) {
		t.Fatal("empty schema contained files")
	}
	if remaining != 0 {
		t.Fatalf("remaining traversal nodes = %d, want 0", remaining)
	}
	mediaCollector := mediaTypeCollector{}
	mediaCollector.requestBody(jsonvalue.Boolean(true), "/requestBody")
	mediaCollector.requestBody(
		testValidationValue(t, `{"$ref":"#/components/requestBodies/X"}`),
		"/requestBody",
	)
	mediaCollector.callback(jsonvalue.Boolean(true), "/callback")
	mediaCollector.callback(
		testValidationValue(t, `{"$ref":"#/components/callbacks/X"}`),
		"/callback",
	)
	mediaCollector.parameters(testValidationValue(t, `{"parameters":{}}`), "/parameters")
	mediaCollector.parameter(jsonvalue.Boolean(true), "/parameter")
	if len(mediaCollector.locations) != 0 {
		t.Fatalf("malformed media locations = %#v", mediaCollector.locations)
	}
	parameterCollector := mediaTypeCollector{}
	parameterCollector.parameters(testValidationValue(t, `{
		"parameters":[{"content":{"application/json":{}}}]
	}`), "/parameters")
	if len(parameterCollector.locations) != 1 ||
		parameterCollector.locations[0].pointer !=
			"/parameters/0/content/application~1json" {
		t.Fatalf("parameter media locations = %#v", parameterCollector.locations)
	}
	callbackCollector := mediaTypeCollector{}
	callbackCollector.callback(testValidationValue(t, `{
		"{$request.body#/url}":{"post":{"requestBody":{"content":{
			"application/json":{}
		}}}}
	}`), "/callback")
	if len(callbackCollector.locations) != 1 ||
		callbackCollector.locations[0].pointer !=
			"/callback/{$request.body#~1url}/post/requestBody/content/application~1json" {
		t.Fatalf("callback media locations = %#v", callbackCollector.locations)
	}

	schemaCollector := schemaCollector{dialect: openapi.DialectOAS31}
	schemaCollector.requestBody(jsonvalue.Boolean(true), "/requestBody")
	schemaCollector.callback(jsonvalue.Boolean(true), "/callback")
	schemaCollector.callback(
		testValidationValue(t, `{"$ref":"#/components/callbacks/X"}`),
		"/callback",
	)
	schemaCollector.parameters(testValidationValue(t, `{"parameters":{}}`), "/parameters")
	schemaCollector.schema(testValidationValue(t, `1`), "/schema")
	if len(schemaCollector.locations) != 0 {
		t.Fatalf("malformed schema locations = %#v", schemaCollector.locations)
	}
}

func TestOpenAPIDiscriminatorHelpersCoverDefensiveStates(t *testing.T) {
	t.Parallel()

	root := testValidationValue(t, `{
		"components":{"schemas":{
			"Pet":{"type":"object","discriminator":{"propertyName":"kind"}},
			"Cat":{"allOf":[true,{"$ref":"#/components/schemas/Pet"}]},
			"Bird":{"type":"object"}
		}}
	}`)
	petSchemas, _ := openAPIComponentSchemas(root)
	pet, _ := petSchemas.Lookup("Pet")
	for _, test := range []struct {
		name     string
		schema   jsonvalue.Value
		instance jsonvalue.Value
		count    int
	}{
		{name: "missing discriminator", schema: testValidationValue(t, `{}`), instance: testValidationValue(t, `{}`)},
		{name: "missing property name", schema: testValidationValue(t, `{"discriminator":{}}`), instance: testValidationValue(t, `{}`)},
		{name: "missing value", schema: pet, instance: testValidationValue(t, `{}`)},
		{name: "non-string value", schema: pet, instance: testValidationValue(t, `{"kind":true}`), count: 1},
		{name: "external target", schema: testValidationValue(t, `{"discriminator":{"propertyName":"kind","mapping":{"cat":"other.json#/Cat"}}}`), instance: testValidationValue(t, `{"kind":"cat"}`)},
		{name: "missing components", schema: pet, instance: testValidationValue(t, `{"kind":"Pet"}`), count: 1},
		{name: "base schema", schema: pet, instance: testValidationValue(t, `{"kind":"Pet"}`)},
		{name: "unlisted schema", schema: pet, instance: testValidationValue(t, `{"kind":"Bird"}`), count: 1},
	} {
		testRoot := root
		if test.name == "missing components" {
			testRoot = testValidationValue(t, `{}`)
		}
		got := openAPIDiscriminatorInstanceDiagnostics(
			testRoot, test.schema, test.instance, "3.2.0",
		)
		if len(got) != test.count {
			t.Errorf("%s diagnostics = %#v", test.name, got)
		}
	}

	for _, test := range []struct {
		target string
		name   string
	}{
		{target: "other.json#/Cat"},
		{target: "#anchor"},
		{target: "#/components/parameters/Pet"},
		{target: "#/components/schemas/Pet", name: "Pet"},
	} {
		if got := localComponentSchemaName(test.target); got != test.name {
			t.Errorf("localComponentSchemaName(%q) = %q", test.target, got)
		}
	}

	alternatives, base := openAPIDiscriminatorAlternatives(
		root, schemaLocation{value: testValidationValue(t, `{"type":"string"}`)},
	)
	if len(alternatives) != 0 || base != "" {
		t.Fatalf("unmatched schema alternatives = %#v, %q", alternatives, base)
	}
	if name := componentSchemaName(
		petSchemas,
		schemaLocation{value: testValidationValue(t, `{"type":"string"}`), pointer: "/other"},
	); name != "" {
		t.Fatalf("unmatched component schema = %q", name)
	}
	if schemaAllOfReferencesAny(
		testValidationValue(t, `{"allOf":[true,{"type":"string"}]}`),
		map[string]struct{}{"#/components/schemas/Pet": {}},
	) {
		t.Fatal("malformed allOf branch matched a parent")
	}
}

func TestExactRemainingDefensiveBranches(t *testing.T) {
	t.Parallel()

	remaining := 0
	if schemaContainsBinaryFileArray(
		context.Background(), reference.Resource{Root: testValidationValue(t, `{}`)},
		testValidationValue(t, `{}`), nil, reference.DefaultLimits(),
		map[string]struct{}{}, &remaining, 0,
	) {
		t.Fatal("exhausted traversal found a binary file")
	}
	if start := schemaPointerStart([]string{"definitions", "Pet"}); start != 1 {
		t.Fatalf("Swagger schema pointer start = %d", start)
	}
}

func TestBinaryFileSchemaTraversalCoversReferencesAndCompositions(t *testing.T) {
	t.Parallel()

	limits := reference.DefaultLimits()
	resource := reference.Resource{
		Root:         testValidationValue(t, `{}`),
		RetrievalURI: "https://example.test/openapi.json",
	}
	remaining := limits.MaxTraversalNodes
	if schemaContainsBinaryFileArray(
		context.Background(), resource,
		testValidationValue(t, `{"$ref":"#/missing"}`), nil, limits,
		map[string]struct{}{}, &remaining, 0,
	) {
		t.Fatal("unresolved reference contained a binary file array")
	}
	remaining = limits.MaxTraversalNodes
	if schemaContainsBinaryFileArray(
		context.Background(), resource,
		testValidationValue(t, `{"$ref":"#/missing"}`), nil, limits,
		map[string]struct{}{
			"https://example.test/openapi.json\x00#/missing": {},
		}, &remaining, 0,
	) {
		t.Fatal("visited reference contained a binary file array")
	}

	for _, raw := range []string{
		`{"type":"array","items":{"type":"array","items":{"type":"string","format":"binary"}}}`,
		`{"oneOf":[{"type":"array","items":{"type":"string","format":"binary"}}]}`,
	} {
		remaining = limits.MaxTraversalNodes
		if !schemaContainsBinaryFileArray(
			context.Background(), resource, testValidationValue(t, raw), nil,
			limits, map[string]struct{}{}, &remaining, 0,
		) {
			t.Errorf("binary file array was missed in %s", raw)
		}
	}

	nested := testValidationValue(t, `{
		"properties":{"files":{
			"type":"array","items":{"type":"string","format":"binary"}
		}}
	}`)
	limits.MaxTraversalDepth = 1
	remaining = limits.MaxTraversalNodes
	if schemaContainsBinaryFileArray(
		context.Background(), resource, nested, nil, limits,
		map[string]struct{}{}, &remaining, 0,
	) {
		t.Fatal("binary file array exceeded the traversal depth")
	}
	limits.MaxTraversalDepth = 2
	remaining = limits.MaxTraversalNodes
	if !schemaContainsBinaryFileArray(
		context.Background(), resource, nested, nil, limits,
		map[string]struct{}{}, &remaining, 0,
	) {
		t.Fatal("binary file array at the traversal boundary was missed")
	}
}

func TestEncodingPropertyValidationCoversUnresolvableSchemas(t *testing.T) {
	t.Parallel()

	root := testValidationValue(t, `{}`)
	encoding := testValidationValue(t, `{"value":{}}`)
	for _, schema := range []string{`{"$ref":"#/missing"}`, `{}`} {
		options := DefaultOptions()
		diagnostics := validateEncodingProperties(
			context.Background(), reference.Resource{Root: root},
			mediaTypeLocation{
				value:   testValidationValue(t, `{"schema":`+schema+`}`),
				pointer: "/content/application~1json",
			},
			encoding, "3.2.0", options,
		)
		want := 0
		if schema == `{}` {
			want = 1
		}
		if len(diagnostics) != want {
			t.Errorf("schema %s diagnostics = %#v", schema, diagnostics)
		}
	}
}

func TestEncodingSchemaPropertyTraversalBoundsAndCycles(t *testing.T) {
	t.Parallel()

	root := testValidationValue(t, `{
		"Loop":{"$ref":"#/Loop"},
		"Target":{"properties":{"external":{"type":"string"}}}
	}`)
	resource := reference.Resource{Root: root}
	options := DefaultOptions()

	properties, complete := encodingSchemaProperties(
		context.Background(),
		resource,
		testValidationValue(t, `{
			"properties":{"same":{"type":"string"}},
			"allOf":[
				true,
				null,
				{"properties":{"same":{"type":"integer"}}},
				{"$ref":"#/Target"},
				{"$ref":"#/Target"},
				{"$ref":"#/Loop"}
			]
		}`),
		options,
	)
	if complete {
		t.Fatal("non-schema composition element did not mark traversal incomplete")
	}
	if len(properties) != 2 || properties["same"].value.Kind() !=
		jsonvalue.ObjectKind {
		t.Fatalf("properties = %#v", properties)
	}

	for _, test := range []struct {
		name   string
		limits reference.Limits
	}{
		{name: "node limit", limits: reference.Limits{
			MaxTraversalDepth: 2, MaxTraversalNodes: 1, MaxReferenceDepth: 2,
		}},
		{name: "depth limit", limits: reference.Limits{
			MaxTraversalDepth: 1, MaxTraversalNodes: 2, MaxReferenceDepth: 2,
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			bounded := options
			bounded.ReferenceLimits = test.limits
			_, complete := encodingSchemaProperties(
				context.Background(),
				resource,
				testValidationValue(t, `{"allOf":[{}]}`),
				bounded,
			)
			if complete {
				t.Fatal("bounded traversal was reported complete")
			}
		})
	}

	exact := options
	exact.ReferenceLimits.MaxTraversalNodes = 1
	properties, complete = encodingSchemaProperties(
		context.Background(),
		resource,
		testValidationValue(t, `{"properties":{"exact":{}}}`),
		exact,
	)
	if !complete || len(properties) != 1 {
		t.Fatalf("exact node boundary = %#v, complete %t", properties, complete)
	}

	properties = make(map[string]encodingSchemaProperty)
	complete = true
	remaining := options.ReferenceLimits.MaxTraversalNodes
	collectEncodingSchemaProperties(
		context.Background(),
		reference.Resource{
			RetrievalURI: "https://example.test/schema.json",
			Root: testValidationValue(t, `{
				"Target":{"properties":{"retrieved":{}}}
			}`),
		},
		testValidationValue(t, `{"$ref":"#/Target"}`),
		nil,
		options.ReferenceLimits,
		map[string]struct{}{"\x00#/Target": {}},
		&remaining,
		0,
		properties,
		&complete,
	)
	if !complete || len(properties) != 1 {
		t.Fatalf("retrieval identity = %#v, complete %t", properties, complete)
	}
}

func TestSchemaProseHelpersCoverDialectEdges(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		raw     string
		wanted  string
		present bool
	}{
		{raw: `{}`, wanted: "array"},
		{raw: `{"type":true}`, wanted: "array"},
		{raw: `{"type":[true,"array"]}`, wanted: "array", present: true},
		{raw: `{"type":["string"]}`, wanted: "array"},
	} {
		if got := schemaHasType(testValidationValue(t, test.raw), test.wanted); got != test.present {
			t.Errorf("schemaHasType(%s, %q) = %t", test.raw, test.wanted, got)
		}
	}

	for _, test := range []struct {
		value string
		valid bool
	}{
		{value: "https://example.test/openapi.json", valid: true},
		{value: "%"},
		{value: "openapi.json"},
		{value: "https://example.test/openapi.json#schema"},
	} {
		if got := validSchemaBaseURI(test.value); got != test.valid {
			t.Errorf("validSchemaBaseURI(%q) = %t", test.value, got)
		}
	}

	for _, test := range []struct {
		pointer string
		want    bool
	}{
		{pointer: "/components/schemas/Value", want: true},
		{pointer: "/components/schemas/Value/properties/name", want: true},
		{pointer: "/components/schemas/Value/properties/list/items/items", want: true},
		{pointer: "/components/schemas/Value/properties/list/items/oneOf"},
		{pointer: "/components/responses/Value"},
	} {
		if got := xmlNodeNameInferred(test.pointer); got != test.want {
			t.Errorf("xmlNodeNameInferred(%q) = %t", test.pointer, got)
		}
	}

	for _, raw := range []string{`{}`, `{"required":true}`, `{"required":[true]}`} {
		if schemaRequiresProperty(testValidationValue(t, raw), "kind") {
			t.Errorf("schemaRequiresProperty(%s) was true", raw)
		}
	}
	if !schemaRequiresProperty(
		testValidationValue(t, `{"required":[true,"kind"]}`), "kind",
	) {
		t.Fatal("required property after malformed entry was missed")
	}
}

func TestSchemaCollectorCoversDialectSpecificContainers(t *testing.T) {
	t.Parallel()

	collector := schemaCollector{dialect: openapi.DialectOAS32}
	collector.document(testValidationValue(t, `{
		"components":{"mediaTypes":{"Reusable":{"schema":{"type":"string"}}}},
		"paths":{"/events":{"post":{
			"parameters":[{"schema":{"type":"integer"}}],
			"callbacks":{"notify":{"{$request.body#/url}":{"post":{
				"requestBody":{"content":{"application/json":{
					"schema":{"type":"boolean"}
				}}}
			}}}}
		}}}
	}`))
	want := map[string]bool{
		"/components/mediaTypes/Reusable/schema":   false,
		"/paths/~1events/post/parameters/0/schema": false,
		"/paths/~1events/post/callbacks/notify/{$request.body#~1url}/post/requestBody/content/application~1json/schema": false,
	}
	for _, location := range collector.locations {
		if _, exists := want[location.pointer]; exists {
			want[location.pointer] = true
		}
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing schema location %s: %#v", pointer, collector.locations)
		}
	}
}

func TestSchemaXMLCoversAllNodeSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		pointer string
		dialect openapi.Dialect
		codes   []string
	}{
		{
			name:    "Swagger non-property invalid namespace and wrapped",
			raw:     `{"type":"string","xml":{"namespace":"relative","wrapped":false}}`,
			pointer: "/definitions/Value", dialect: openapi.DialectSwagger20,
			codes: []string{"openapi.swagger.xml.non-property", "openapi.xml.namespace.invalid", "openapi.xml.wrapped.non-array"},
		},
		{
			name: "OAS 3.1 array omits its name",
			raw:  `{"type":"array","xml":{}}`, pointer: "/components/schemas/Value",
			dialect: openapi.DialectOAS31,
			codes:   []string{"openapi.xml.non-property", "openapi.xml.array-name.missing"},
		},
		{
			name:    "OAS 3.2 node type conflicts",
			raw:     `{"xml":{"nodeType":"element","attribute":false,"wrapped":false}}`,
			pointer: "/components/schemas/Value", dialect: openapi.DialectOAS32,
			codes: []string{"openapi.xml.wrapped.non-array", "openapi.xml.node-type.conflict", "openapi.xml.node-type.conflict"},
		},
		{
			name:    "OAS 3.2 text ignores name",
			raw:     `{"xml":{"nodeType":"text","name":"value"}}`,
			pointer: "/components/schemas/Value", dialect: openapi.DialectOAS32,
			codes: []string{"openapi.xml.name.ignored"},
		},
		{
			name:    "OAS 3.2 attribute inference requires name",
			raw:     `{"xml":{"attribute":true}}`,
			pointer: "/responses/Value/schema", dialect: openapi.DialectOAS32,
			codes: []string{"openapi.xml.name.missing"},
		},
		{
			name:    "OAS 3.2 wrapped array infers an element",
			raw:     `{"type":"array","xml":{"wrapped":true}}`,
			pointer: "/responses/Value/schema", dialect: openapi.DialectOAS32,
			codes: []string{"openapi.xml.name.missing"},
		},
		{
			name:    "OAS 3.2 reference infers none",
			raw:     `{"$ref":"#/components/schemas/Value","xml":{}}`,
			pointer: "/responses/Value/schema", dialect: openapi.DialectOAS32,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			diagnostics := validateSchemaXML(schemaLocation{
				value: testValidationValue(t, test.raw), pointer: test.pointer,
			}, "3.2.0", test.dialect)
			if len(diagnostics) != len(test.codes) {
				t.Fatalf("diagnostics = %#v, want codes %#v", diagnostics, test.codes)
			}
			for index, code := range test.codes {
				if diagnostics[index].Code != code {
					t.Errorf("diagnostic %d = %q, want %q", index, diagnostics[index].Code, code)
				}
			}
		})
	}
}

func TestDiscriminatorMappingsAndSchemaOutputDefensiveStates(t *testing.T) {
	t.Parallel()

	schema := schemaLocation{
		value: testValidationValue(t, `{
			"oneOf":[true,{"$ref":true},{"$ref":"#/components/schemas/Cat"}],
			"anyOf":{"$ref":"#/components/schemas/Dog"}
		}`),
		pointer: "/components/schemas/Pet",
	}
	if got := validateDiscriminatorMappings(
		testValidationValue(t, `{}`), schema,
		testValidationValue(t, `{"mapping":{"bad":true,"cat":"Cat","dog":"Dog"}}`),
		"3.1.2",
	); len(got) != 1 || got[0].Code != "openapi.schema.discriminator.mapping-unlisted" {
		t.Fatalf("mapping diagnostics = %#v", got)
	}
	if got := validateDiscriminatorMappings(
		testValidationValue(t, `{}`), schema, testValidationValue(t, `{}`), "3.1.2",
	); len(got) != 0 {
		t.Fatalf("missing mapping diagnostics = %#v", got)
	}
	if got := validateDiscriminatorMappings(
		testValidationValue(t, `{}`),
		schemaLocation{value: testValidationValue(t, `{}`)},
		testValidationValue(t, `{"mapping":{"cat":"Cat"}}`), "3.1.2",
	); len(got) != 0 {
		t.Fatalf("mapping without alternatives diagnostics = %#v", got)
	}

	valid := schemaObjectDiagnostics(openapischema.OutputUnit{Valid: true}, "/schema", "3.1.2")
	if len(valid) != 0 {
		t.Fatalf("valid schema diagnostics = %#v", valid)
	}
	invalid := schemaObjectDiagnostics(openapischema.OutputUnit{
		Valid: false,
		Errors: []openapischema.OutputUnit{
			{KeywordLocation: ""},
			{KeywordLocation: "/$ref", Error: "reference"},
			{KeywordLocation: "/allOf", Error: "composition"},
		},
	}, "/schema", "3.1.2")
	if len(invalid) != 1 || invalid[0].Code != "openapi.schema.invalid" {
		t.Fatalf("fallback schema diagnostics = %#v", invalid)
	}
	direct := schemaObjectDiagnostics(openapischema.OutputUnit{
		Valid: false, KeywordLocation: "/type", InstanceLocation: "/type",
		Error: "wrong type",
	}, "/schema", "3.1.2")
	if len(direct) != 1 || direct[0].Code != "openapi.schema.type" {
		t.Fatalf("direct schema diagnostics = %#v", direct)
	}
}

func TestValidateSchemasOwnsFallibleDependencies(t *testing.T) {
	t.Parallel()
	if diagnostics, err := validateSchemas(
		context.Background(),
		validationDocument{raw: testValidationValue(t, `{}`)},
		DefaultOptions(),
	); err != nil || len(diagnostics) != 0 {
		t.Fatalf("unknown dialect schemas = %#v, %v", diagnostics, err)
	}

	version, err := openapi.ParseVersion("3.1.2")
	if err != nil {
		t.Fatal(err)
	}
	document := validationDocument{
		version: version,
		raw: testValidationValue(t, `{
			"openapi":"3.1.2",
			"components":{"schemas":{
				"One":{"type":"string"},
				"Two":{"type":"string"}
			}}
		}`),
	}
	want := errors.New("schema dependency failure")

	options := DefaultOptions()
	options.schemaCompilerFactory = func(
		openapi.Document, ...openapischema.Option,
	) (*openapischema.Compiler, error) {
		return nil, want
	}
	if _, err := validateSchemas(
		context.Background(), document, options,
	); !errors.Is(err, want) {
		t.Fatalf("compiler error = %v", err)
	}

	options.schemaCompilerFactory = func(
		openapi.Document, ...openapischema.Option,
	) (*openapischema.Compiler, error) {
		return nil, nil
	}
	options.schemaMarshaller = func(jsonvalue.Value) ([]byte, error) {
		return nil, want
	}
	if _, err := validateSchemas(
		context.Background(), document, options,
	); !errors.Is(err, want) {
		t.Fatalf("marshaller error = %v", err)
	}

	options.schemaMarshaller = func(value jsonvalue.Value) ([]byte, error) {
		return value.MarshalJSON()
	}
	options.schemaValidator = func(
		*openapischema.Compiler, context.Context, jsonvalue.Value,
	) (openapischema.OutputUnit, error) {
		return openapischema.OutputUnit{}, want
	}
	if _, err := validateSchemas(
		context.Background(), document, options,
	); !errors.Is(err, want) {
		t.Fatalf("validator error = %v", err)
	}

	validations := 0
	options.schemaValidator = func(
		*openapischema.Compiler, context.Context, jsonvalue.Value,
	) (openapischema.OutputUnit, error) {
		validations++
		return openapischema.OutputUnit{Valid: true}, nil
	}
	diagnostics, err := validateSchemas(context.Background(), document, options)
	if err != nil || len(diagnostics) != 0 || validations != 1 {
		t.Fatalf("cached validation = %#v, %v, calls %d", diagnostics, err, validations)
	}
}

func TestParameterAndPathDefensiveStates(t *testing.T) {
	t.Parallel()

	root := testValidationValue(t, `{
		"parameters":{"value":{"name":"value","in":"query","type":"string"}}
	}`)
	if parameters := swaggerParameterObjects(root); len(parameters) != 1 {
		t.Fatalf("root Swagger parameters = %#v", parameters)
	}
	version, err := openapi.ParseVersion("2.0")
	if err != nil {
		t.Fatal(err)
	}
	document := validationDocument{raw: root, version: version}
	if diagnostics := validateSwaggerFileParameterConsumes(
		context.Background(), document, DefaultOptions(),
	); len(diagnostics) != 0 {
		t.Fatalf("pathless Swagger diagnostics = %#v", diagnostics)
	}
	container := testValidationValue(t, `{
		"parameters":[false,{"$ref":"#/missing"},{"in":"query"},{"name":"value"}]
	}`)
	if parameters := resolvedSwaggerParameters(
		context.Background(),
		reference.Resource{Root: testValidationValue(t, `{}`)},
		container,
		"/parameters",
		nil,
		reference.DefaultLimits(),
	); len(parameters) != 2 {
		t.Fatalf("resolved defensive parameters = %#v", parameters)
	}
	for _, raw := range []string{`{"consumes":true}`, `{"consumes":[true]}`} {
		if values, present := swaggerConsumes(testValidationValue(t, raw)); !present || len(values) != 0 {
			t.Errorf("swaggerConsumes(%s) = %#v, %t", raw, values, present)
		}
	}
	if diagnostics := parameterIdentityDiagnostics(
		context.Background(),
		reference.Resource{Root: testValidationValue(t, `{}`)},
		container,
		"/parameters",
		"3.1.2",
		openapi.DialectOAS31,
		nil,
		reference.DefaultLimits(),
	); len(diagnostics) != 0 {
		t.Fatalf("malformed identity diagnostics = %#v", diagnostics)
	}
	for _, test := range []struct {
		body      int
		inherited int
		override  int
		conflict  bool
	}{
		{body: 1},
		{body: 2, inherited: 2},
		{body: 2, override: 2},
		{body: 2, inherited: 1, override: 1, conflict: true},
	} {
		if got := effectiveSwaggerBodyConflict(
			test.body, test.inherited, test.override,
		); got != test.conflict {
			t.Errorf("effective body conflict for %#v = %t", test, got)
		}
	}
	validCollection := validateSwaggerParameter(
		testValidationValue(t, `{
			"name":"values","in":"query","type":"array",
			"items":{"type":"string"},"collectionFormat":"csv"
		}`),
		"/parameter", "2.0",
	)
	for _, diagnostic := range validCollection {
		if diagnostic.Code == "swagger.parameter.collection-format.non-array" {
			t.Fatalf("array collection format rejected: %#v", validCollection)
		}
	}
	scope := parameterLocations(testValidationValue(t, `{
		"parameters":[
			{"name":"X-Request-ID","in":"header"},
			{"name":"Search","in":"query"}
		]
	}`))
	if scope["header\x00x-request-id"] != "header" ||
		scope["query\x00Search"] != "query" || len(scope) != 2 {
		t.Fatalf("parameter scope = %#v", scope)
	}
	for _, test := range []struct {
		name  string
		scope parameterScope
		codes int
	}{
		{name: "empty", scope: parameterScope{}},
		{name: "query only", scope: parameterScope{"query\x00q": "query"}},
		{name: "querystring only", scope: parameterScope{"querystring\x00q": "querystring"}},
		{
			name: "mixed",
			scope: parameterScope{
				"query\x00q": "query", "querystring\x00all": "querystring",
			},
			codes: 1,
		},
	} {
		if got := queryStringScopeDiagnostics("3.2.0", "/parameters", test.scope); len(got) != test.codes {
			t.Errorf("%s querystring diagnostics = %#v", test.name, got)
		}
	}
	if locations := parameterLocations(container); len(locations) != 0 {
		t.Fatalf("malformed parameter locations = %#v", locations)
	}
	if parameters := appendParameter(nil, jsonvalue.Boolean(true), "/parameter"); len(parameters) != 0 {
		t.Fatalf("scalar parameter = %#v", parameters)
	}
	diagnostics := validateParameter(
		testValidationValue(t, `{
			"name":"value","in":"header","schema":{},"allowEmptyValue":true
		}`),
		"/parameter", "3.1.2", openapi.DialectOAS31,
	)
	if len(diagnostics) == 0 {
		t.Fatal("invalid allowEmptyValue produced no diagnostics")
	}
	if validStyle("unknown", "form", openapi.DialectOAS31) {
		t.Fatal("unknown parameter location accepted a style")
	}

	version, err = openapi.ParseVersion("3.1.2")
	if err != nil {
		t.Fatal(err)
	}
	document = validationDocument{
		raw:     testValidationValue(t, `{"openapi":"3.1.2","paths":{"/value":false}}`),
		version: version,
	}
	if diagnostics := validatePaths(
		context.Background(), document, DefaultOptions(),
	); len(diagnostics) != 0 {
		t.Fatalf("scalar path diagnostics = %#v", diagnostics)
	}
	parameters, pathDiagnostics := parametersAt(
		context.Background(), reference.Resource{Root: testValidationValue(t, `{}`)},
		nil, reference.DefaultLimits(), container, "/parameters", "3.1.2",
	)
	if len(parameters) != 0 || len(pathDiagnostics) != 0 {
		t.Fatalf("malformed path parameters = %#v, %#v", parameters, pathDiagnostics)
	}
	if value, exists := booleanMember(testValidationValue(t, `{}`), "required"); exists || value {
		t.Fatal("missing boolean member existed")
	}
	if quoted := safeValue(strings.Repeat("a", 81)); !strings.Contains(quoted, "...") {
		t.Fatalf("long safe value = %s", quoted)
	}
}

func testValidationValue(t *testing.T, raw string) jsonvalue.Value {
	t.Helper()
	value, err := parse.JSON(
		context.Background(), strings.NewReader(raw), parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
