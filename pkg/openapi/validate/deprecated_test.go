package validate_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestDocumentWarnsAboutDeprecatedOpenAPISurfaces(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{"deprecated":true,
			"parameters":[{"name":"trace","in":"query","deprecated":true,
				"schema":{"type":"string"}}],
			"responses":{"200":{"description":"ok","headers":{
				"X-Old":{"deprecated":true,"schema":{"type":"string"}}
			}}}}}},
		"components":{"securitySchemes":{"oldAuth":{
			"type":"apiKey","name":"key","in":"header","deprecated":true
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"openapi.operation.deprecated":       "/paths/~1pets/get/deprecated",
		"openapi.parameter.deprecated":       "/paths/~1pets/get/parameters/0/deprecated",
		"openapi.header.deprecated":          "/paths/~1pets/get/responses/200/headers/X-Old/deprecated",
		"openapi.security-scheme.deprecated": "/components/securitySchemes/oldAuth/deprecated",
	}
	for _, diagnostic := range report.Diagnostics() {
		pointer, exists := want[diagnostic.Code]
		if !exists {
			continue
		}
		if diagnostic.InstanceLocation != pointer ||
			diagnostic.Severity != validate.SeverityWarning {
			t.Fatalf("deprecated diagnostic = %#v", diagnostic)
		}
		delete(want, diagnostic.Code)
	}
	if len(want) != 0 {
		t.Fatalf("missing deprecated diagnostics: %#v; got %#v",
			want, report.Diagnostics())
	}
}

func TestDocumentWarnsAboutDeprecatedOpenAPI30Schemas(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.0.4","info":{"title":"API","version":"1"},
		"paths":{},"components":{"schemas":{"Old":{
			"type":"object","deprecated":true,
			"properties":{"old":{"type":"string","deprecated":true}}
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/components/schemas/Old/deprecated":                false,
		"/components/schemas/Old/properties/old/deprecated": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.schema.deprecated" {
			continue
		}
		if _, exists := want[diagnostic.InstanceLocation]; !exists ||
			diagnostic.Severity != validate.SeverityWarning {
			t.Fatalf("deprecated schema diagnostic = %#v", diagnostic)
		}
		want[diagnostic.InstanceLocation] = true
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing deprecated schema at %s: %#v",
				pointer, report.Diagnostics())
		}
	}
}
