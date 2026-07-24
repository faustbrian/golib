package validate

import (
	"context"
	"errors"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func TestReferenceValidationAcceptsExactResourceRootKinds(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		dialect specversion.Dialect
		root    jsonvalue.Value
		valid   bool
	}{
		{name: "3.2 object", dialect: specversion.DialectOAS32, root: testValidationValue(t, `{}`), valid: true},
		{name: "3.2 boolean", dialect: specversion.DialectOAS32, root: jsonvalue.Boolean(true), valid: true},
		{name: "3.2 scalar", dialect: specversion.DialectOAS32, root: testValidationValue(t, `1`)},
		{name: "3.1 scalar", dialect: specversion.DialectOAS31, root: testValidationValue(t, `1`), valid: true},
	} {
		wrapped := validationResolver(reference.ResolverFunc(func(
			context.Context, string,
		) (reference.Resource, error) {
			return reference.Resource{Root: test.root}, nil
		}), test.dialect)
		_, err := wrapped.Resolve(context.Background(), "resource")
		if (err == nil) != test.valid {
			t.Errorf("%s resolution error = %v", test.name, err)
		}
	}
}

func TestReferenceSourceClassificationBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		tokens  []string
		dialect openapi.Dialect
		want    string
	}{
		{name: "empty"},
		{name: "root reference", tokens: []string{"$ref"}},
		{name: "not reference", tokens: []string{"responses", "Value"}},
		{name: "component", tokens: []string{"components", "responses", "Value", "$ref"}, want: "responses"},
		{name: "schema component", tokens: []string{"components", "schemas", "Value", "$ref"}, want: "schemas"},
		{name: "Swagger parameter", tokens: []string{"parameters", "Value", "$ref"}, want: "parameters"},
		{name: "Swagger response", tokens: []string{"responses", "Value", "$ref"}, want: "responses"},
		{name: "Swagger security", tokens: []string{"securityDefinitions", "Value", "$ref"}, want: "securitySchemes"},
		{name: "path", tokens: []string{"paths", "/pets", "$ref"}, want: "pathItems"},
		{name: "webhook", tokens: []string{"webhooks", "event", "$ref"}, want: "pathItems"},
		{name: "callback path", tokens: []string{"callbacks", "event", "expression", "$ref"}, want: "pathItems"},
		{name: "request body", tokens: []string{"operation", "requestBody", "$ref"}, want: "requestBodies"},
		{name: "nested response", tokens: []string{"operation", "responses", "200", "$ref"}, want: "responses"},
		{name: "3.2 media", tokens: []string{"operation", "content", "application/json", "$ref"}, dialect: openapi.DialectOAS32, want: "mediaTypes"},
		{name: "3.1 media", tokens: []string{"operation", "content", "application/json", "$ref"}, dialect: openapi.DialectOAS31},
		{name: "schema property", tokens: []string{"schema", "properties", "value", "$ref"}, want: "schemas"},
	} {
		if got := referenceSourceKind(test.tokens, test.dialect); got != test.want {
			t.Errorf("%s source kind = %q, want %q", test.name, got, test.want)
		}
	}
}

func TestReferenceDataClassificationBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		tokens  []string
		dialect openapi.Dialect
		data    bool
	}{
		{name: "extension value", tokens: []string{"info", "x-data", "$ref"}, data: true},
		{name: "extension map key", tokens: []string{"components", "schemas", "x-name", "$ref"}},
		{name: "component extension map key", tokens: []string{"components", "examples", "x-name", "$ref"}},
		{name: "unknown component map key", tokens: []string{"components", "unknown", "x-name", "$ref"}},
		{name: "example", tokens: []string{"parameter", "example", "$ref"}, data: true},
		{name: "example value", tokens: []string{"components", "examples", "Value", "value", "$ref"}, data: true},
		{name: "link request body", tokens: []string{"components", "links", "Value", "requestBody", "$ref"}, data: true},
		{name: "Swagger response examples", tokens: []string{"responses", "200", "examples", "application/json", "$ref"}, dialect: openapi.DialectSwagger20, data: true},
		{name: "schema const", tokens: []string{"components", "schemas", "Value", "const", "$ref"}, data: true},
		{name: "root schema const", tokens: []string{"schema", "const", "$ref"}, data: true},
		{name: "schema property map key", tokens: []string{"components", "schemas", "Value", "properties", "x-name", "$ref"}},
	} {
		if got := referenceDataPointer(test.tokens, test.dialect); got != test.data {
			t.Errorf("%s data pointer = %t", test.name, got)
		}
	}
}

func TestReferenceSchemaHelpersCoverExactContexts(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		tokens []string
		start  int
	}{
		{start: -1},
		{tokens: []string{"$ref"}, start: -1},
		{tokens: []string{"components", "schemas", "Value", "$ref"}, start: 2},
		{tokens: []string{"definitions", "Value", "$ref"}, start: 1},
		{tokens: []string{"parameter", "schema", "$ref"}, start: 1},
		{tokens: []string{"parameter", "$ref"}, start: -1},
	} {
		if got := schemaPointerStart(test.tokens); got != test.start {
			t.Errorf("schemaPointerStart(%#v) = %d", test.tokens, got)
		}
	}

	for _, test := range []struct {
		raw   string
		valid bool
	}{
		{raw: `{}`, valid: true},
		{raw: `true`, valid: true},
		{raw: `1`},
	} {
		value, valid := resolveReferencedSchema(
			context.Background(), testValidationValue(t, `{}`),
			testValidationValue(t, test.raw),
		)
		if valid != test.valid || value.Kind() == jsonvalue.InvalidKind {
			t.Errorf("resolveReferencedSchema(%s) = %#v, %t", test.raw, value, valid)
		}
	}
}

func TestReferenceValidationAllowsExactOccurrenceLimit(t *testing.T) {
	t.Parallel()

	version, err := openapi.ParseVersion("3.1.2")
	if err != nil {
		t.Fatal(err)
	}
	document := validationDocument{
		version: version,
		raw: testValidationValue(t, `{
			"openapi":"3.1.2","info":{"title":"API","version":"1"},
			"paths":{},"components":{"schemas":{
				"Alias":{"$ref":"#/components/schemas/Target"},
				"Target":{"type":"string"}
			}}
		}`),
	}
	options := DefaultOptions()
	options.MaxReferences = 1
	if diagnostics, err := validateReferenceTargets(
		context.Background(), document, options,
	); err != nil || len(diagnostics) != 0 {
		t.Fatalf("exact reference limit = %#v, %v", diagnostics, err)
	}

	want := errors.New("resolver failure")
	resolver := validationResolver(reference.ResolverFunc(func(
		context.Context, string,
	) (reference.Resource, error) {
		return reference.Resource{}, want
	}), specversion.DialectOAS32)
	if _, err := resolver.Resolve(context.Background(), "missing"); !errors.Is(err, want) {
		t.Fatalf("resolver failure = %v", err)
	}
}
