package validate

import (
	"context"
	"errors"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/expression"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parameter"
	"github.com/faustbrian/golib/pkg/openapi/reference"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func TestValidationCollectionsRejectMalformedContainerShapes(t *testing.T) {
	t.Parallel()

	version, err := openapi.ParseVersion("3.2.0")
	if err != nil {
		t.Fatal(err)
	}
	document := validationDocument{
		version: version,
		raw: testValidationValue(t, `{
			"paths":true,
			"components":{"securitySchemes":true}
		}`),
	}
	if diagnostics := validatePaths(context.Background(), document, DefaultOptions()); len(diagnostics) != 0 {
		t.Fatalf("malformed paths diagnostics = %#v", diagnostics)
	}
	if diagnostics := deprecatedSecuritySchemeDiagnostics(document, "3.2.0"); len(diagnostics) != 0 {
		t.Fatalf("malformed deprecated security diagnostics = %#v", diagnostics)
	}
	if diagnostics := validateSecuritySchemeURLs(document); len(diagnostics) != 0 {
		t.Fatalf("malformed security URL diagnostics = %#v", diagnostics)
	}
	resource := reference.Resource{Root: document.raw}
	if schemes := securitySchemes(
		context.Background(), resource, specversion.DialectOAS32, nil,
		reference.DefaultLimits(),
	); len(schemes) != 0 {
		t.Fatalf("malformed security schemes = %#v", schemes)
	}
	if parameters, diagnostics := parametersAt(
		context.Background(), resource, nil, reference.DefaultLimits(),
		testValidationValue(t, `{"parameters":true}`), "/path", "3.2.0",
	); len(parameters) != 0 || len(diagnostics) != 0 {
		t.Fatalf("malformed parameters = %#v, %#v", parameters, diagnostics)
	}
}

func TestValidationPureHelpersRejectMalformedValues(t *testing.T) {
	t.Parallel()

	if plainStringMediaType(
		context.Background(), testValidationValue(t, `{}`),
		mediaTypeLocation{
			name:  "text/plain",
			value: testValidationValue(t, `{"schema":{"$ref":"#/missing"}}`),
		},
	) {
		t.Fatal("unresolved plain-text schema was accepted")
	}
	if diagnostics := validateEncodingProperties(
		context.Background(), reference.Resource{},
		mediaTypeLocation{value: testValidationValue(t, `{"schema":true}`)},
		testValidationValue(t, `{}`), "3.2.0", DefaultOptions(),
	); len(diagnostics) != 0 {
		t.Fatalf("scalar encoding schema diagnostics = %#v", diagnostics)
	}
	if diagnostics := validateServerVariable(
		testValidationValue(t, `{"enum":true}`), "/variable", "3.2.0",
		specversion.DialectOAS32,
	); len(diagnostics) != 0 {
		t.Fatalf("malformed server enum diagnostics = %#v", diagnostics)
	}
	if validServerURLTemplate32("https://example.test/%aZ") {
		t.Fatal("invalid second percent-escape digit was accepted")
	}
	if validServerURLTemplate32("https://example.test/%Za") {
		t.Fatal("invalid first percent-escape digit was accepted")
	}
	got, err := directionalRequired(
		testValidationValue(t, `["value"]`), jsonvalue.Boolean(true),
		DirectionRequest,
	)
	if err != nil || got.Kind() != jsonvalue.ArrayKind {
		t.Fatalf("scalar properties changed required fields: %#v, %v", got, err)
	}

	for _, target := range []string{
		"https://example.test/schema", "#anchor", "#/%", "#/components", "#/other/schemas/Value",
	} {
		if localComponentSchemaName(target) != "" {
			t.Errorf("invalid component target %q returned a name", target)
		}
	}
	for _, raw := range []string{
		`{}`, `{"$ref":"https://example.test/schema"}`,
		`{"$ref":"#anchor"}`, `{"$ref":"#/%"}`, `{"$ref":"#/definitions"}`,
		`{"$ref":"#/other/Value"}`,
	} {
		if swaggerDefinitionReferenceName(testValidationValue(t, raw)) != "" {
			t.Errorf("invalid Swagger definition reference %s returned a name", raw)
		}
	}
	if swaggerFileResponseSchema(schemaLocation{
		value: testValidationValue(t, `{"type":"file"}`), pointer: "/responses/200",
	}) {
		t.Fatal("response object was mistaken for its schema")
	}
	if schemaAllOfReferencesAny(
		testValidationValue(t, `{"allOf":true}`), map[string]struct{}{},
	) {
		t.Fatal("scalar allOf matched a reference")
	}
	if trueMember(testValidationValue(t, `{"enabled":1}`), "enabled") {
		t.Fatal("numeric member was treated as true")
	}
	template, err := expression.ParseTemplate("prefix{$url}")
	if err != nil {
		t.Fatal(err)
	}
	if singleExpression(template) {
		t.Fatal("mixed literal and dynamic template was treated as one expression")
	}
	validator := linkValidator{}
	validator.visitLinkWithParameters(jsonvalue.Boolean(true), "/link", nil)
	if len(validator.diagnostics) != 0 {
		t.Fatalf("scalar link diagnostics = %#v", validator.diagnostics)
	}
	if scopes := oauthScopes(
		testValidationValue(t, `{"flows":true}`), specversion.DialectOAS31,
	); len(scopes) != 0 {
		t.Fatalf("scalar OAuth flows = %#v", scopes)
	}
	scopes := make(map[string]struct{})
	collectScopeNames(scopes, testValidationValue(t, `{"scopes":true}`))
	if len(scopes) != 0 {
		t.Fatalf("scalar OAuth scopes = %#v", scopes)
	}
	if diagnostics := swaggerRequiredReadOnlyDiagnostics(schemaLocation{
		value: testValidationValue(t, `{"properties":{},"required":true}`),
	}, "2.0"); len(diagnostics) != 0 {
		t.Fatalf("scalar Swagger required diagnostics = %#v", diagnostics)
	}
}

func TestValidationTransportAndExampleGuardsRejectMalformedShapes(t *testing.T) {
	t.Parallel()

	for _, host := range []string{"", "bad/path", "[bad", "user@example.test", ":8080"} {
		if validSwaggerHost(host) {
			t.Errorf("invalid Swagger host %q was accepted", host)
		}
	}
	if !validSwaggerHost("api.example.test:8443") {
		t.Fatal("valid Swagger host was rejected")
	}
	owner := testValidationValue(t, `{"schemes":true,"produces":true}`)
	if diagnostics := validateSwaggerSchemes("2.0", owner, "schemes", "/schemes"); len(diagnostics) != 0 {
		t.Fatalf("scalar Swagger schemes diagnostics = %#v", diagnostics)
	}
	if diagnostics := validateSwaggerMediaTypes("2.0", owner, "produces", "/produces"); len(diagnostics) != 0 {
		t.Fatalf("scalar Swagger media diagnostics = %#v", diagnostics)
	}
	if validSwaggerMediaType("/json") {
		t.Fatal("empty Swagger media type was accepted")
	}

	operation := operationLocation{value: testValidationValue(t, `{"responses":true}`)}
	if diagnostics := validateSwaggerOperationExamples(
		context.Background(), reference.Resource{}, operation, nil, nil, "2.0", DefaultOptions(),
	); len(diagnostics) != 0 {
		t.Fatalf("scalar Swagger responses diagnostics = %#v", diagnostics)
	}
	operation.value = testValidationValue(t, `{"responses":{"200":{"examples":true}}}`)
	if diagnostics := validateSwaggerOperationExamples(
		context.Background(), reference.Resource{}, operation, nil, nil, "2.0", DefaultOptions(),
	); len(diagnostics) != 0 {
		t.Fatalf("scalar Swagger examples diagnostics = %#v", diagnostics)
	}

	external, err := jsonvalue.String("example.json")
	if err != nil {
		t.Fatal(err)
	}
	if diagnostics := appendParameterExternalSerializedPairDiagnostics(
		context.Background(), nil, jsonvalue.Member{}, external, "/example", "value",
		parameter.Primitive, parameter.Options{}, jsonvalue.Value{}, false, "3.2.0",
		DefaultOptions(),
	); len(diagnostics) != 0 {
		t.Fatalf("resolver-less external parameter diagnostics = %#v", diagnostics)
	}
}

func TestValidationSchemaAndTraversalDefensiveFailures(t *testing.T) {
	t.Parallel()

	remaining := 1
	if _, err := binaryMaxLengthConstraints(
		context.Background(), reference.Resource{},
		testValidationValue(t, `{"maxLength":1e999999999}`), specversion.DialectOAS31,
		nil, reference.DefaultLimits(), map[string]struct{}{}, &remaining, 0,
	); err == nil {
		t.Fatal("unrepresentable maxLength was accepted")
	}

	for _, raw := range []string{
		`{"schema":{"type":"object","required":["linkset"],"properties":{"linkset":{}},"allOf":[1]}}`,
		`{"schema":{"type":"object","required":["linkset"],"properties":{"linkset":{"type":"array","items":{"type":"object","properties":{"anchor":{"$ref":"#/missing"}}}}}}}`,
	} {
		mediaType := testValidationValue(t, raw)
		if validLinksetMediaTypeSchema(
			context.Background(), reference.Resource{Root: mediaType}, mediaType, DefaultOptions(),
		) {
			t.Errorf("incomplete linkset media schema %s was accepted", raw)
		}
	}
	relation := testValidationValue(t, `{
		"type":"array","items":{"type":"object","required":["href"],
		"properties":{"href":{}},"allOf":[1]}
	}`)
	if validLinksetRelationSchema(
		context.Background(), reference.Resource{Root: relation}, relation, DefaultOptions(),
	) {
		t.Fatal("incomplete relation target schema was accepted")
	}
}

func TestValidationDiscriminatorsIgnoreNonObjectInstances(t *testing.T) {
	t.Parallel()

	root := testValidationValue(t, `{"components":{"schemas":{"Value":{}}}}`)
	openAPISchema := testValidationValue(t, `{
		"discriminator":{"propertyName":"kind"}
	}`)
	if diagnostics := openAPIDiscriminatorInstanceDiagnostics(
		root, openAPISchema, jsonvalue.Boolean(true), "3.2.0",
	); len(diagnostics) != 0 {
		t.Fatalf("scalar OpenAPI discriminator diagnostics = %#v", diagnostics)
	}
	swaggerSchema := testValidationValue(t, `{"discriminator":"kind"}`)
	if diagnostics := swaggerDiscriminatorInstanceDiagnostics(
		testValidationValue(t, `{"definitions":{}}`), swaggerSchema,
		jsonvalue.Boolean(true), "Base", "2.0",
	); len(diagnostics) != 0 {
		t.Fatalf("scalar Swagger discriminator diagnostics = %#v", diagnostics)
	}
	if diagnostics := openAPIDiscriminatorInstanceDiagnostics(
		root,
		testValidationValue(t, `{
			"discriminator":{"propertyName":"kind"},
			"oneOf":[]
		}`),
		testValidationValue(t, `{"kind":"Value"}`), "3.2.0",
	); len(diagnostics) != 0 {
		t.Fatalf("empty discriminator alternatives = %#v", diagnostics)
	}
}

func TestReferenceValidationPropagatesDeadlineAndCancellation(t *testing.T) {
	t.Parallel()

	version, err := openapi.ParseVersion("3.1.2")
	if err != nil {
		t.Fatal(err)
	}
	document := validationDocument{
		version: version,
		raw: testValidationValue(t, `{
			"openapi":"3.1.2",
			"paths":{},
			"components":{"schemas":{"Value":{"$ref":"external.json#/Value"}}}
		}`),
	}
	for _, want := range []error{context.Canceled, context.DeadlineExceeded} {
		resolver := reference.ResolverFunc(func(context.Context, string) (reference.Resource, error) {
			return reference.Resource{}, want
		})
		options := DefaultOptions()
		options.ReferenceResolver = resolver
		_, err := validateReferenceTargets(
			context.Background(), document, options,
		)
		if !errors.Is(err, want) {
			t.Errorf("reference error = %v, want %v", err, want)
		}
	}
}

func TestLinksetSchemaValidationRejectsIncompleteReferenceShapes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, raw := range []string{
		`{"schema":{"$ref":"#/missing"}}`,
		`{"schema":{"type":"object"}}`,
		`{"schema":{"type":"object","required":["linkset"],"properties":true}}`,
		`{"schema":{"type":"object","required":["linkset"],"properties":{"linkset":{"$ref":"#/missing"}}}}`,
		`{"schema":{"type":"object","required":["linkset"],"properties":{"linkset":{"type":"array","items":{"$ref":"#/missing"}}}}}`,
	} {
		mediaType := testValidationValue(t, raw)
		if validLinksetMediaTypeSchema(
			ctx, reference.Resource{Root: mediaType}, mediaType, DefaultOptions(),
		) {
			t.Errorf("incomplete linkset schema %s was accepted", raw)
		}
	}
	for _, raw := range []string{
		`{"$ref":"#/missing"}`,
		`{"type":"array","items":{"$ref":"#/missing"}}`,
		`{"type":"array","items":{"type":"object","required":["href"],"properties":true}}`,
		`{"type":"array","items":{"type":"object","required":["href"],"properties":{"href":{"$ref":"#/missing"}}}}`,
	} {
		schema := testValidationValue(t, raw)
		if validLinksetRelationSchema(
			ctx, reference.Resource{Root: schema}, schema, DefaultOptions(),
		) {
			t.Errorf("incomplete relation schema %s was accepted", raw)
		}
	}
	if resolvedSchemaRequiresProperty(
		ctx, reference.Resource{}, testValidationValue(t, `{"required":true}`),
		"value", DefaultOptions(),
	) {
		t.Fatal("scalar required member matched a property")
	}
}
