package validate_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestDocumentRecommendsNonEmptyRequestBodyContent(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{},
		"webhooks":{"event":{"post":{
			"requestBody":{"content":{}},
			"responses":{"204":{"description":"ok"}}
		}}},
		"components":{"requestBodies":{"Shared":{"content":{}}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/webhooks/event/post/requestBody/content": false,
		"/components/requestBodies/Shared/content": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.request-body.content.empty" ||
			diagnostic.Severity != validate.SeverityWarning {
			continue
		}
		if _, exists := want[diagnostic.InstanceLocation]; exists {
			want[diagnostic.InstanceLocation] = true
		}
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing recommendation at %s: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestDocumentDoesNotApplyRequestBodyContentRecommendationBefore31Patch2(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.1","info":{"title":"API","version":"1"},
		"paths":{"/items":{"post":{
			"requestBody":{"content":{}},
			"responses":{"204":{"description":"ok"}}
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.request-body.content.empty" {
			t.Fatalf("3.1.2 recommendation applied to 3.1.1: %#v", diagnostic)
		}
	}
}

func TestDocumentWarnsAboutRequestBodiesWithUndefinedMethodSemantics(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{"/items":{
					"get":{"requestBody":{"content":{"application/json":{}}},"responses":{"204":{"description":"ok"}}},
					"head":{"requestBody":{"content":{"application/json":{}}},"responses":{"204":{"description":"ok"}}},
					"delete":{"requestBody":{"content":{"application/json":{}}},"responses":{"204":{"description":"ok"}}},
					"post":{"requestBody":{"content":{"application/json":{}}},"responses":{"204":{"description":"ok"}}}
				}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			found := make(map[string]bool)
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.operation.request-body.undefined" {
					found[diagnostic.InstanceLocation] = true
				}
			}
			for _, method := range []string{"get", "head", "delete"} {
				pointer := "/paths/~1items/" + method + "/requestBody"
				if !found[pointer] {
					t.Errorf("missing %s warning: %#v", method, report.Diagnostics())
				}
			}
			if len(found) != 3 {
				t.Errorf("unexpected method warnings: %#v", found)
			}
		})
	}
}

func TestOpenAPI30ConsumersIgnoreUndefinedMethodRequestBodies(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{"/items":{
					"get":{"requestBody":{"content":{"multipart/form-data":{}}},
						"responses":{"204":{"description":"ok"}}},
					"head":{"requestBody":{"content":{"multipart/form-data":{}}},
						"responses":{"204":{"description":"ok"}}},
					"delete":{"requestBody":{"content":{"multipart/form-data":{}}},
						"responses":{"204":{"description":"ok"}}},
					"options":{"requestBody":{"content":{"multipart/form-data":{}}},
						"responses":{"204":{"description":"ok"}}},
					"trace":{"requestBody":{"content":{"multipart/form-data":{}}},
						"responses":{"204":{"description":"ok"}}},
					"post":{"requestBody":{"content":{"multipart/form-data":{}}},
						"responses":{"204":{"description":"ok"}}}
				}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			var schemaDiagnostics []string
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code ==
					"openapi.media-type.multipart.schema-missing" {
					schemaDiagnostics = append(
						schemaDiagnostics,
						diagnostic.InstanceLocation,
					)
				}
			}
			want := "/paths/~1items/post/requestBody/content/multipart~1form-data"
			if len(schemaDiagnostics) != 1 || schemaDiagnostics[0] != want {
				t.Fatalf("semantic request bodies = %#v, want %q",
					schemaDiagnostics, want)
			}
		})
	}
}
