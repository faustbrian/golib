package convert

import (
	"context"
	"testing"
)

func TestConvertOpenAPI30ToSwagger20WorkIsBoundedAndCancelable(t *testing.T) {
	t.Parallel()

	source := swaggerDocument(t, `{
		"openapi":"3.0.4","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{"responses":{"200":{"description":"OK"}}}}}
	}`)
	_, _, err := convertOAS30ToSwagger20(context.Background(), source.Raw(), 1)
	if err != ErrLimitExceeded {
		t.Fatalf("document limit error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = convertOAS30ToSwagger20(ctx, source.Raw(), 100)
	if err != context.Canceled {
		t.Fatalf("canceled conversion error = %v", err)
	}
}

func TestConvertOpenAPI30ToSwagger20ReportsFieldLosses(t *testing.T) {
	t.Parallel()

	source := swaggerDocument(t, `{
		"openapi":"3.0.4","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"summary":"Pets","trace":{"responses":{
			"204":{"description":"Traced"}}},"post":{
			"requestBody":{"content":{"application/json":{
				"schema":{"type":"object"},"example":{"id":1}}}},
			"responses":{"200":{"description":"OK","headers":{"X-List":{
				"required":true,"deprecated":true,"style":"simple",
				"schema":{"type":"array","items":{"type":"string"}}}},
				"content":{"application/json":{"schema":{"type":"string"},
					"example":"one","examples":{"named":{"value":"two"}},
					"encoding":{"field":{}}}}}}
		}}},
		"components":{"schemas":{
			"Kept":{"type":"string","nullable":false},
			"Loss":{"type":"string","nullable":true}
		}}
	}`)
	converted, diagnostics, err := convertOAS30ToSwagger20(
		context.Background(), source.Raw(), 1_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]DiagnosticKind{
		"/paths/~1pets/summary": Loss,
		"/paths/~1pets/trace":   Loss,
		"/paths/~1pets/post/requestBody/content/application~1json/example":    Loss,
		"/paths/~1pets/post/responses/200/headers/X-List/required":            Loss,
		"/paths/~1pets/post/responses/200/headers/X-List/deprecated":          Loss,
		"/paths/~1pets/post/responses/200/content/application~1json/examples": Loss,
		"/paths/~1pets/post/responses/200/content/application~1json/encoding": Loss,
		"/components/schemas/Loss/nullable":                                   Loss,
	}
	if len(diagnostics) != len(want) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if kind, exists := want[diagnostic.Pointer]; !exists || kind != diagnostic.Kind {
			t.Fatalf("diagnostic = %#v", diagnostic)
		}
	}
	if _, exists := memberAt(t, converted, "definitions", "Kept").Lookup(
		"nullable",
	); exists {
		t.Fatal("false nullable keyword was retained")
	}
	assertValidConverted(t, converted)
}

func TestConvertOpenAPI30ToSwagger20ConvertsNestedSchemas(t *testing.T) {
	t.Parallel()

	source := swaggerDocument(t, `{
		"openapi":"3.0.4","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"post":{
			"requestBody":{"content":{
				"application/json":{"schema":{"$ref":"#/components/schemas/Pet"}},
				"application/xml":{"schema":{"$ref":"#/components/schemas/Pet"}}}},
			"responses":{"200":{"description":"OK","content":{
				"application/json":{"schema":{"$ref":"#/components/schemas/Base"}},
				"application/xml":{"schema":{"$ref":"#/components/schemas/Base"}},
				"text/plain":{"schema":{"type":"integer"}}}}}
		}}},
		"components":{"schemas":{
			"Base":{"type":"string"},
			"External":{"$ref":"schemas.json#/Pet"},
			"Pet":{"type":"object","required":["kind"],
				"properties":{"kind":{"type":"string"},
					"secret":{"type":"string","writeOnly":true}},
				"allOf":[{"$ref":"#/components/schemas/Base"}],
				"oneOf":[{"type":"string"}],"anyOf":[{"type":"integer"}],
				"not":{},"deprecated":false,
				"discriminator":{"propertyName":"kind","mapping":{
					"pet":"#/components/schemas/Pet"}}}
		}}
	}`)
	converted, diagnostics, err := convertOAS30ToSwagger20(
		context.Background(), source.Raw(), 10_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]DiagnosticKind{
		"/paths/~1pets/post/responses/200/content/text~1plain/schema": Loss,
		"/components/schemas/Pet/properties/secret/writeOnly":         Loss,
		"/components/schemas/Pet/oneOf":                               Loss,
		"/components/schemas/Pet/anyOf":                               Loss,
		"/components/schemas/Pet/not":                                 Loss,
		"/components/schemas/Pet/discriminator/mapping":               Loss,
		"/components/schemas/External/$ref":                           ManualAction,
	}
	if len(diagnostics) != len(want) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if kind, exists := want[diagnostic.Pointer]; !exists || kind != diagnostic.Kind {
			t.Fatalf("diagnostic = %#v", diagnostic)
		}
	}
	pet := memberAt(t, converted, "definitions", "Pet")
	if textValue(t, memberAt(t, pet, "discriminator")) != "kind" {
		t.Fatalf("discriminator = %#v", pet)
	}
	allOf, _ := memberAt(t, pet, "allOf").Elements()
	if textValue(t, memberAt(t, allOf[0], "$ref")) != "#/definitions/Base" {
		t.Fatalf("allOf = %#v", allOf)
	}
	assertValidConverted(t, converted)
}

func TestConvertOpenAPI30ToSwagger20HandlesBodyAndServerExtensions(t *testing.T) {
	t.Parallel()

	source := swaggerDocument(t, `{
		"openapi":"3.0.4","info":{"title":"API","version":"1"},
		"servers":[{"url":"/v1","x-owner":"team"}],
		"paths":{
			"/json":{"post":{"requestBody":{"x-owner":"team","content":{
				"application/json":{"schema":{"type":"string"}}}},
				"responses":{"204":{"description":"OK"}}}},
			"/form":{"post":{"requestBody":{"x-owner":"team","content":{
				"multipart/form-data":{"schema":{"type":"object","properties":{
					"name":{"type":"string"}}}},
				"application/json":{"schema":{"type":"object"}}}},
				"responses":{"204":{"description":"OK"}}}}
		}
	}`)
	converted, diagnostics, err := convertOAS30ToSwagger20(
		context.Background(), source.Raw(), 10_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	jsonParameters, _ := memberAt(
		t, converted, "paths", "/json", "post", "parameters",
	).Elements()
	if owner, _ := memberAt(t, jsonParameters[0], "x-owner").Text(); owner != "team" {
		t.Fatalf("body parameter = %#v", jsonParameters[0])
	}
	want := map[string]DiagnosticKind{
		"/servers/0/x-owner":                                       Loss,
		"/paths/~1form/post/requestBody/x-owner":                   Loss,
		"/paths/~1form/post/requestBody/content/application~1json": Loss,
	}
	if len(diagnostics) != len(want) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if kind, exists := want[diagnostic.Pointer]; !exists || kind != diagnostic.Kind {
			t.Fatalf("diagnostic = %#v", diagnostic)
		}
	}
	assertValidConverted(t, converted)
}
