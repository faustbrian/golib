package validate_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestDocumentValidatesComponentNamesAcrossOpenAPIVersions(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			document := mustDocument(t, `{
				"openapi":"`+version+`",
				"info":{"title":"API","version":"1"},"paths":{},
				"components":{"schemas":{
					"bad name":{"type":"string"}
				}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.component.name.invalid" &&
					diagnostic.InstanceLocation == "/components/schemas/bad name" &&
					diagnostic.SpecificationVersion == version &&
					diagnostic.SpecificationSection == "components-object" {
					return
				}
			}
			t.Fatalf("missing component-name diagnostic: %#v", report.Diagnostics())
		})
	}
}

func TestDocumentAcceptsLegalComponentNamesAndIgnoresExtensions(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{},
		"components":{
			"schemas":{"Letters09._-":{"type":"string"}},
			"mediaTypes":{"application.json":{"schema":{"type":"string"}}},
			"x-private":{"bad name":true}
		}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.component.name.invalid" {
			t.Fatalf("legal component surface rejected: %#v", diagnostic)
		}
	}
}

func TestDocumentValidatesOpenAPI32RestrictedComponentRegistryNames(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{},
		"components":{
			"schemas":{"bad/schema":{}},
			"responses":{"bad response":{}},
			"parameters":{"bad:parameter":{}},
			"examples":{"bad@example":{}},
			"requestBodies":{"bad+body":{}},
			"headers":{"bad,header":{}},
			"securitySchemes":{"bad=security":{}},
			"links":{"bad$link":{}},
			"callbacks":{"bad~callback":{}},
			"pathItems":{"":{}},
			"mediaTypes":{"média":{}}
		}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	locations := map[string]bool{
		"/components/schemas/bad~1schema":     false,
		"/components/callbacks/bad~0callback": false,
		"/components/pathItems/":              false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.component.name.invalid" {
			continue
		}
		count++
		if _, tracked := locations[diagnostic.InstanceLocation]; tracked {
			locations[diagnostic.InstanceLocation] = true
		}
	}
	if count != 10 {
		t.Fatalf("component-name diagnostics = %d, want 10: %#v", count, report.Diagnostics())
	}
	for location, found := range locations {
		if !found {
			t.Errorf("missing diagnostic at %q", location)
		}
	}
}

func TestComponentNameRuleDoesNotApplyToSwaggerReusableObjects(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},"paths":{},
		"definitions":{"legacy name":{"type":"string"}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.component.name.invalid" {
			t.Fatalf("OpenAPI component rule applied to Swagger: %#v", diagnostic)
		}
	}
}
