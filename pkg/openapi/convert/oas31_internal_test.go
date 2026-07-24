package convert

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

func TestConvertOpenAPI31SchemasTo30(t *testing.T) {
	t.Parallel()

	source := swaggerDocument(t, `{
		"openapi":"3.1.2","jsonSchemaDialect":"https://example.test/schema",
		"info":{"title":"API","version":"1"},"paths":{},
		"components":{"schemas":{
			"Choice":{"type":["string","integer","null"],
				"exclusiveMinimum":-0.00e+2,"const":"fixed",
				"examples":["first","second"],"if":{"type":"string"}},
			"Never":false,
			"Alias":{"$ref":"#/components/schemas/Choice",
				"description":"meaningful in 3.1"},
			"External":{"$ref":"schemas.json#/$defs/Thing"}
		}}
	}`)
	converted, diagnostics, err := convertOAS31Document(
		context.Background(), source.Raw(), 1_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := converted.Lookup("jsonSchemaDialect"); exists {
		t.Fatal("root schema dialect was retained")
	}
	choice := memberAt(t, converted, "components", "schemas", "Choice")
	if _, exists := choice.Lookup("type"); exists {
		t.Fatal("multi-type keyword was retained")
	}
	anyOf, _ := memberAt(t, choice, "anyOf").Elements()
	if len(anyOf) != 2 ||
		textValue(t, memberAt(t, anyOf[0], "type")) != "string" ||
		textValue(t, memberAt(t, anyOf[1], "type")) != "integer" {
		t.Fatalf("choice alternatives = %#v", anyOf)
	}
	if nullable, _ := memberAt(t, anyOf[0], "nullable").Bool(); !nullable {
		t.Fatalf("choice alternatives = %#v", anyOf)
	}
	minimum, _ := memberAt(t, choice, "minimum").NumberText()
	if minimum != "-0.00e+2" {
		t.Fatalf("minimum = %q", minimum)
	}
	if exclusive, _ := memberAt(t, choice, "exclusiveMinimum").Bool(); !exclusive {
		t.Fatalf("choice schema = %#v", choice)
	}
	enum, _ := memberAt(t, choice, "enum").Elements()
	if len(enum) != 1 || textValue(t, enum[0]) != "fixed" {
		t.Fatalf("enum = %#v", enum)
	}
	if textValue(t, memberAt(t, choice, "example")) != "first" {
		t.Fatalf("choice example = %#v", choice)
	}
	never := memberAt(t, converted, "components", "schemas", "Never")
	if memberAt(t, never, "not").Kind() == 0 {
		t.Fatalf("false schema = %#v", never)
	}
	alias := memberAt(t, converted, "components", "schemas", "Alias")
	if _, exists := alias.Lookup("description"); exists {
		t.Fatal("OpenAPI 3.1 reference sibling was retained")
	}
	want := map[string]DiagnosticKind{
		"/jsonSchemaDialect":                    Loss,
		"/components/schemas/Choice/examples":   Loss,
		"/components/schemas/Choice/if":         Loss,
		"/components/schemas/Alias/description": Loss,
		"/components/schemas/External/$ref":     ManualAction,
	}
	if len(diagnostics) != len(want) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if kind, exists := want[diagnostic.Pointer]; !exists || kind != diagnostic.Kind {
			t.Fatalf("diagnostic = %#v", diagnostic)
		}
	}
}

func TestConvertOpenAPI31SchemaDowngradeEdges(t *testing.T) {
	t.Parallel()

	source := swaggerDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},"paths":{},
		"components":{"schemas":{
			"Scalar":{"nullable":false,"type":["string","null"]},
			"OnlyNull":{"type":["null"]},
			"InvalidType":{"type":[1,"integer"]},
			"Always":true,
			"Bounds":{"minimum":2e0,"exclusiveMinimum":1.0,
				"maximum":5,"exclusiveMaximum":1e1},
			"Nested":{"type":"object","properties":{
				"items":{"type":"array","items":false},
				"map":{"additionalProperties":true}
			}},
			"Composed":{"type":["integer","number"],
				"anyOf":[{"minimum":0}],"allOf":[{"maximum":10}]},
			"Constant":{"enum":["other","fixed"],"const":"fixed"},
			"Impossible":{"enum":["other"],"const":"fixed"},
			"Annotated":{"examples":["new"],"example":"existing"},
			"EmptyExamples":{"examples":[]}
		}}
	}`)
	converted, diagnostics, err := convertOAS31Document(
		context.Background(), source.Raw(), 1_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	schemas := memberAt(t, converted, "components", "schemas")
	scalar := memberAt(t, schemas, "Scalar")
	if textValue(t, memberAt(t, scalar, "type")) != "string" {
		t.Fatalf("scalar = %#v", scalar)
	}
	if nullable, _ := memberAt(t, scalar, "nullable").Bool(); !nullable {
		t.Fatalf("scalar = %#v", scalar)
	}
	if memberAt(t, schemas, "Always").Kind() != jsonvalue.ObjectKind {
		t.Fatalf("true schema = %#v", memberAt(t, schemas, "Always"))
	}
	onlyNullAllOf, _ := memberAt(t, schemas, "OnlyNull", "allOf").Elements()
	if len(onlyNullAllOf) != 1 {
		t.Fatalf("null-only schema = %#v", onlyNullAllOf)
	}
	bounds := memberAt(t, schemas, "Bounds")
	minimum, _ := memberAt(t, bounds, "minimum").NumberText()
	maximum, _ := memberAt(t, bounds, "maximum").NumberText()
	if minimum != "2e0" || maximum != "5" {
		t.Fatalf("bounds = %#v", bounds)
	}
	if exclusive, _ := memberAt(t, bounds, "exclusiveMinimum").Bool(); exclusive {
		t.Fatalf("lower bound = %#v", bounds)
	}
	if exclusive, _ := memberAt(t, bounds, "exclusiveMaximum").Bool(); exclusive {
		t.Fatalf("upper bound = %#v", bounds)
	}
	memberAt(t, schemas, "Nested", "properties", "items", "items", "not")
	additional := memberAt(
		t, schemas, "Nested", "properties", "map", "additionalProperties",
	)
	if additional.Kind() != jsonvalue.ObjectKind {
		t.Fatalf("true additionalProperties = %#v", additional)
	}
	composed := memberAt(t, schemas, "Composed")
	allOf, _ := memberAt(t, composed, "allOf").Elements()
	if len(allOf) != 2 {
		t.Fatalf("composed allOf = %#v", allOf)
	}
	typeAlternatives, _ := memberAt(t, allOf[1], "anyOf").Elements()
	if len(typeAlternatives) != 2 {
		t.Fatalf("type alternatives = %#v", typeAlternatives)
	}
	constant, _ := memberAt(t, schemas, "Constant", "enum").Elements()
	if len(constant) != 1 || textValue(t, constant[0]) != "fixed" {
		t.Fatalf("constant enum = %#v", constant)
	}
	impossible := memberAt(t, schemas, "Impossible")
	memberAt(t, impossible, "not")
	impossibleEnum, _ := memberAt(t, impossible, "enum").Elements()
	if len(impossibleEnum) != 1 || textValue(t, impossibleEnum[0]) != "other" {
		t.Fatalf("impossible enum = %#v", impossibleEnum)
	}
	if textValue(t, memberAt(t, schemas, "Annotated", "example")) != "existing" {
		t.Fatalf("annotated schema = %#v", memberAt(t, schemas, "Annotated"))
	}
	want := map[string]DiagnosticKind{
		"/components/schemas/InvalidType/type":       Loss,
		"/components/schemas/EmptyExamples/examples": Loss,
		"/components/schemas/Annotated/examples":     Loss,
	}
	if len(diagnostics) != len(want) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if kind, exists := want[diagnostic.Pointer]; !exists || kind != diagnostic.Kind {
			t.Fatalf("diagnostic = %#v", diagnostic)
		}
	}
}

func TestConvertOpenAPI31SchemaDowngradeIsBoundedAndCancelable(t *testing.T) {
	t.Parallel()

	source := swaggerDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},"paths":{},
		"components":{"schemas":{"Root":{"type":"array",
			"items":{"type":"string"}}}}
	}`)
	_, _, err := convertOAS31Document(context.Background(), source.Raw(), 1)
	if err != ErrLimitExceeded {
		t.Fatalf("schema limit error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = convertOAS31Document(ctx, source.Raw(), 100)
	if err != context.Canceled {
		t.Fatalf("canceled conversion error = %v", err)
	}
}

func TestConvertOpenAPI31RemovesUnsupportedDocumentFields(t *testing.T) {
	t.Parallel()

	source := swaggerDocument(t, `{
		"openapi":"3.1.2",
		"info":{"title":"API","summary":"Summary","version":"1",
			"license":{"name":"MIT","identifier":"MIT"}},
		"paths":{"/pets":{"summary":"Pets","description":"Pet operations",
			"get":{"responses":{"200":{"description":"OK"}}}}},
		"webhooks":{"event":{"post":{"responses":{"204":{
			"description":"Accepted"}}}}},
		"components":{
			"pathItems":{"Shared":{"get":{"responses":{"200":{
				"description":"OK"}}}}},
			"securitySchemes":{"TLS":{"type":"mutualTLS"}}
		}
	}`)
	converted, diagnostics, err := convertOAS31Document(
		context.Background(), source.Raw(), 1_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := converted.Lookup("webhooks"); exists {
		t.Fatal("webhooks were retained")
	}
	if _, exists := memberAt(t, converted, "info").Lookup("summary"); exists {
		t.Fatal("Info summary was retained")
	}
	if _, exists := memberAt(t, converted, "info", "license").Lookup("identifier"); exists {
		t.Fatal("license identifier was retained")
	}
	components := memberAt(t, converted, "components")
	if _, exists := components.Lookup("pathItems"); exists {
		t.Fatal("reusable path items were retained")
	}
	security := memberAt(t, components, "securitySchemes")
	if _, exists := security.Lookup("TLS"); exists {
		t.Fatal("mutual TLS security scheme was retained")
	}
	want := map[string]DiagnosticKind{
		"/info/summary":                   Loss,
		"/info/license/identifier":        Loss,
		"/webhooks":                       Loss,
		"/components/pathItems":           Loss,
		"/components/securitySchemes/TLS": Loss,
		"/paths/~1pets/summary":           Loss,
		"/paths/~1pets/description":       Loss,
	}
	if len(diagnostics) != len(want) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if kind, exists := want[diagnostic.Pointer]; !exists || kind != diagnostic.Kind {
			t.Fatalf("diagnostic = %#v", diagnostic)
		}
	}
}

func TestConvertOpenAPI31ReferenceObjects(t *testing.T) {
	t.Parallel()

	source := swaggerDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{"responses":{"200":{
			"$ref":"#/components/responses/Remote"}}}}},
		"components":{"responses":{"Remote":{
			"$ref":"responses.json#/Remote","summary":"Remote",
			"description":"Remote response"
		}}}
	}`)
	converted, diagnostics, err := convertOAS31Document(
		context.Background(), source.Raw(), 1_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	remote := memberAt(t, converted, "components", "responses", "Remote")
	if textValue(t, memberAt(t, remote, "$ref")) != "responses.json#/Remote" {
		t.Fatalf("response reference = %#v", remote)
	}
	if members, _ := remote.Members(); len(members) != 1 {
		t.Fatalf("response reference = %#v", remote)
	}
	want := map[string]DiagnosticKind{
		"/components/responses/Remote/$ref":        ManualAction,
		"/components/responses/Remote/summary":     Loss,
		"/components/responses/Remote/description": Loss,
	}
	if len(diagnostics) != len(want) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if kind, exists := want[diagnostic.Pointer]; !exists || kind != diagnostic.Kind {
			t.Fatalf("diagnostic = %#v", diagnostic)
		}
	}
}

func TestConvertOpenAPI31ConstEnumSemanticEquality(t *testing.T) {
	t.Parallel()

	source := swaggerDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},"paths":{},
		"components":{"schemas":{
			"Null":{"const":null,"enum":[null]},
			"Boolean":{"const":true,"enum":[true]},
			"Number":{"const":1,"enum":[1.0]},
			"String":{"const":"value","enum":["value"]},
			"Array":{"const":[1,"x"],"enum":[[1.0,"x"]]},
			"Object":{"const":{"a":1,"b":true},
				"enum":[{"b":true,"a":1.0}]},
			"Different":{"const":[1],"enum":[[2]]}
		}}
	}`)
	converted, diagnostics, err := convertOAS31Document(
		context.Background(), source.Raw(), 1_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	schemas := memberAt(t, converted, "components", "schemas")
	for _, name := range []string{
		"Null", "Boolean", "Number", "String", "Array", "Object",
	} {
		if _, exists := memberAt(t, schemas, name).Lookup("not"); exists {
			t.Fatalf("equivalent const and enum rejected for %s", name)
		}
	}
	memberAt(t, schemas, "Different", "not")
}
