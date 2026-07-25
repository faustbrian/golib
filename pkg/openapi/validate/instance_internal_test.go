package validate

import (
	"context"
	"errors"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	openapischema "github.com/faustbrian/golib/pkg/openapi/jsonschema"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

func TestSchemaInstanceRejectsInvalidInternalStates(t *testing.T) {
	t.Parallel()

	version, err := openapi.ParseVersion("3.1.2")
	if err != nil {
		t.Fatal(err)
	}
	document := validationDocument{
		raw: testValidationValue(t, `{
			"openapi":"3.1.2","info":{"title":"API","version":"1"},"paths":{}
		}`),
		version: version,
	}
	schema := testValidationValue(t, `{"type":"object"}`)
	instance := testValidationValue(t, `{}`)
	//lint:ignore SA1012 This assertion verifies the nil-context contract.
	//nolint:staticcheck // This assertion verifies the nil-context contract.
	if _, err := SchemaInstance(nil, document, schema, instance, InstanceOptions{}); err == nil {
		t.Fatal("nil context was accepted")
	}
	if _, err := SchemaInstance(
		context.Background(), nil, schema, instance, InstanceOptions{},
	); err == nil {
		t.Fatal("nil document was accepted")
	}
	if _, err := SchemaInstance(
		context.Background(), document, schema, instance,
		InstanceOptions{Direction: InstanceDirection("invalid")},
	); err == nil {
		t.Fatal("invalid direction was accepted")
	}
	if _, err := SchemaInstance(
		context.Background(), document, schema, instance,
		InstanceOptions{MaxNodes: -1},
	); err == nil {
		t.Fatal("negative node limit was accepted")
	}
	if _, err := SchemaInstance(
		context.Background(), document,
		testValidationValue(t, `{"$ref":"#/missing"}`), instance,
		InstanceOptions{},
	); err == nil {
		t.Fatal("unresolved schema was accepted")
	}
	if _, err := SchemaInstance(
		context.Background(), document,
		testValidationValue(t, `{"properties":{"value":{}}}`), instance,
		InstanceOptions{Direction: DirectionRequest, MaxNodes: 1},
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("direction transform limit error = %v", err)
	}
	if _, err := SchemaInstance(
		context.Background(), document, schema, jsonvalue.Value{}, InstanceOptions{},
	); err == nil {
		t.Fatal("invalid instance was serialized")
	}
	if _, err := SchemaInstance(
		context.Background(), validationDocument{raw: document.raw},
		schema, instance, InstanceOptions{},
	); err == nil {
		t.Fatal("unknown document dialect was accepted")
	}
	if _, err := SchemaInstance(
		context.Background(), document,
		testValidationValue(t, `{"type":1}`), instance, InstanceOptions{},
	); err == nil {
		t.Fatal("invalid Schema Object compiled")
	}
	if _, err := SchemaInstance(
		context.Background(), document,
		testValidationValue(t, `{"items":{}}`),
		testValidationValue(t, `[1,2]`),
		InstanceOptions{Direction: DirectionRequest, MaxNodes: 2},
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("direction diagnostics limit error = %v", err)
	}
	want := errors.New("instance dependency failure")
	if _, err := SchemaInstance(
		context.Background(), document, schema, instance,
		InstanceOptions{instanceValidator: func(
			*openapischema.Schema, context.Context, []byte,
		) (openapischema.Result, error) {
			return openapischema.Result{}, want
		}},
	); !errors.Is(err, want) {
		t.Fatalf("instance validator error = %v", err)
	}
	if _, err := SchemaInstance(
		context.Background(), document, schema, instance,
		InstanceOptions{
			Direction: DirectionRequest,
			instanceValidator: func(
				*openapischema.Schema, context.Context, []byte,
			) (openapischema.Result, error) {
				return openapischema.Result{Valid: true}, nil
			},
			directionMarshaller: func(jsonvalue.Value) ([]byte, error) {
				return nil, want
			},
		},
	); !errors.Is(err, want) {
		t.Fatalf("direction marshaller error = %v", err)
	}
}

func TestSchemaDirectionTraversalDefensiveStates(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := schemaDirectionDiagnostics(
		ctx, testValidationValue(t, `{}`), testValidationValue(t, `{}`),
		testValidationValue(t, `{}`), "3.1.2", openapi.DialectOAS31,
		DirectionRequest, 4,
		nil,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("direction cancellation error = %v", err)
	}
	if _, err := schemaDirectionDiagnostics(
		context.Background(), testValidationValue(t, `{}`),
		testValidationValue(t, `{}`), testValidationValue(t, `{}`),
		"3.1.2", openapi.DialectOAS31, DirectionRequest, 0,
		nil,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("direction node limit error = %v", err)
	}
	if diagnostics, err := schemaDirectionDiagnostics(
		context.Background(), testValidationValue(t, `{}`),
		testValidationValue(t, `{"$ref":"#/missing"}`),
		testValidationValue(t, `{}`), "3.1.2", openapi.DialectOAS31,
		DirectionRequest, 4,
		nil,
	); err != nil || len(diagnostics) != 0 {
		t.Fatalf("unresolved direction schema = %#v, %v", diagnostics, err)
	}
	_, err := schemaDirectionDiagnostics(
		context.Background(), testValidationValue(t, `{}`),
		testValidationValue(t, `{
			"allOf":[{"readOnly":true},{"readOnly":true}],
			"properties":{"missing":{"$ref":"#/missing"}}
		}`),
		testValidationValue(t, `{"missing":true}`),
		"3.1.2", openapi.DialectOAS31, DirectionRequest, 16,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestInstanceValidationExactWorkAndDialectBoundaries(t *testing.T) {
	t.Parallel()

	documentFor := func(versionText string) validationDocument {
		version, err := openapi.ParseVersion(versionText)
		if err != nil {
			t.Fatal(err)
		}
		return validationDocument{
			version: version,
			raw: testValidationValue(t, `{
				"openapi":"`+versionText+`","info":{"title":"API","version":"1"},
				"paths":{}
			}`),
		}
	}

	for _, test := range []struct {
		version string
		section string
	}{
		{version: "3.1.2", section: "working-with-binary-data"},
		{version: "3.2.0", section: "binary-streams"},
	} {
		report, err := BinaryMediaLength(
			context.Background(), documentFor(test.version),
			testValidationValue(t, `{"maxLength":1}`), 2,
		)
		if err != nil || len(report.diagnostics) != 1 ||
			report.diagnostics[0].SpecificationSection != test.section {
			t.Errorf("binary dialect %s report = %#v, %v", test.version, report.diagnostics, err)
		}
	}

	for _, limits := range []reference.Limits{
		{MaxTraversalDepth: -1},
		{MaxTraversalNodes: -1},
		{MaxReferenceDepth: -1},
	} {
		options := DefaultOptions()
		options.ReferenceLimits = limits
		if _, err := BinaryMediaLengthWithOptions(
			context.Background(), documentFor("3.2.0"),
			testValidationValue(t, `{}`), 0, options,
		); err == nil {
			t.Errorf("negative reference limits %#v were accepted", limits)
		}
	}
	options := DefaultOptions()
	options.ReferenceLimits = reference.Limits{}
	if report, err := BinaryMediaLengthWithOptions(
		context.Background(), documentFor("3.2.0"),
		testValidationValue(t, `{"maxLength":0}`), 0, options,
	); err != nil || !report.Valid() {
		t.Fatalf("zero reference limits and maxLength = %#v, %v", report.Diagnostics(), err)
	}

	remaining := 1
	constraints, err := binaryMaxLengthConstraints(
		context.Background(), reference.Resource{Root: testValidationValue(t, `{}`)},
		testValidationValue(t, `{"maxLength":1}`), openapi.DialectOAS31,
		nil, reference.Limits{MaxTraversalDepth: 1, MaxTraversalNodes: 1, MaxReferenceDepth: 1},
		map[string]struct{}{}, &remaining, 0,
	)
	if err != nil || len(constraints) != 1 || remaining != 0 {
		t.Fatalf("binary constraints at exact bounds = %#v, %d, %v", constraints, remaining, err)
	}
	root := testValidationValue(t, `{"Target":{"maxLength":1}}`)
	for _, test := range []struct {
		depth int
		valid bool
	}{
		{depth: 1},
		{depth: 2, valid: true},
	} {
		remaining = 2
		constraints, err = binaryMaxLengthConstraints(
			context.Background(), reference.Resource{Root: root},
			testValidationValue(t, `{"$ref":"#/Target"}`), openapi.DialectOAS31,
			nil, reference.Limits{
				MaxTraversalDepth: test.depth, MaxTraversalNodes: 2,
				MaxReferenceDepth: 2,
			}, map[string]struct{}{}, &remaining, 0,
		)
		if (err == nil) != test.valid {
			t.Errorf("reference depth %d constraints = %#v, %v", test.depth, constraints, err)
		}
	}

	sequential, err := SequentialMediaInstance(
		context.Background(), documentFor("3.2.0"), testValidationValue(t, `{}`),
		[]jsonvalue.Value{testValidationValue(t, `1`)}, InstanceOptions{MaxNodes: 1},
	)
	if err != nil || !sequential.Valid() {
		t.Fatalf("sequence at exact item limit = %#v, %v", sequential.Diagnostics(), err)
	}

	if diagnostics, err := schemaDirectionDiagnostics(
		context.Background(), testValidationValue(t, `{}`),
		testValidationValue(t, `{}`), testValidationValue(t, `{}`),
		"3.1.2", openapi.DialectOAS31, DirectionRequest, 1, nil,
	); err != nil || len(diagnostics) != 0 {
		t.Fatalf("direction traversal at exact limit = %#v, %v", diagnostics, err)
	}
}

func TestDirectionalAndSwaggerDiscriminatorHelpers(t *testing.T) {
	t.Parallel()

	visited := 0
	for _, value := range []jsonvalue.Value{
		jsonvalue.Boolean(true),
		testValidationValue(t, `{"$ref":"#/value"}`),
	} {
		result, err := schemaForInstanceDirection(
			value, DirectionRequest, 2, &visited,
		)
		if err != nil || result.Kind() != value.Kind() {
			t.Fatalf("terminal direction schema = %#v, %v", result, err)
		}
	}
	visited = 0
	if _, err := schemaForInstanceDirection(
		testValidationValue(t, `{"allOf":[{}]}`),
		DirectionRequest, 1, &visited,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("direction array child error = %v", err)
	}
	visited = 0
	result, err := transformDirectionalSchemaMap(
		jsonvalue.Boolean(true), DirectionRequest, 1, &visited,
	)
	if err != nil || result.Kind() != jsonvalue.BooleanKind {
		t.Fatalf("non-map direction transform = %#v, %v", result, err)
	}
	visited = 0
	if _, err := transformDirectionalSchemaMap(
		testValidationValue(t, `{"value":{}}`), DirectionRequest, 0, &visited,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("direction map child error = %v", err)
	}
	if result, err := directionalRequired(
		jsonvalue.Boolean(true), testValidationValue(t, `{}`), DirectionRequest,
	); err != nil || result.Kind() != jsonvalue.BooleanKind {
		t.Fatalf("non-array required = %#v, %v", result, err)
	}

	root := testValidationValue(t, `{
		"definitions":{"Base":{"type":"object","discriminator":"kind"}}
	}`)
	schema, _ := root.Lookup("definitions")
	schema, _ = schema.Lookup("Base")
	if got := swaggerDiscriminatorInstanceDiagnostics(
		root, schema, testValidationValue(t, `{}`), "", "2.0",
	); len(got) != 0 {
		t.Fatalf("missing discriminator value = %#v", got)
	}
	if got := swaggerDiscriminatorInstanceDiagnostics(
		root, schema, testValidationValue(t, `{"kind":"Base"}`), "", "2.0",
	); len(got) != 0 {
		t.Fatalf("inferred discriminator name = %#v", got)
	}
	if len(swaggerDiscriminatorNames(root, "")) != 0 ||
		len(swaggerDiscriminatorNames(testValidationValue(t, `{}`), "Base")) != 1 {
		t.Fatal("defensive discriminator names were incorrect")
	}
	if name := swaggerDefinitionName(root, jsonvalue.Value{}); name != "" {
		t.Fatalf("invalid Swagger definition name = %q", name)
	}
	for _, raw := range []string{
		`{"$ref":"#anchor"}`, `{"$ref":"#/%zz"}`,
		`{"$ref":"#/components/schemas/Value"}`,
	} {
		if name := swaggerDefinitionReferenceName(testValidationValue(t, raw)); name != "" {
			t.Errorf("invalid Swagger definition reference %s = %q", raw, name)
		}
	}

	diagnostic := instanceDiagnostic("3.1.2", "code", "/value", "message")
	if got := deduplicateInstanceDiagnostics([]Diagnostic{diagnostic, diagnostic}); len(got) != 1 {
		t.Fatalf("deduplicated diagnostics = %#v", got)
	}
}
