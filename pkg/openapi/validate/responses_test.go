package validate_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestDocumentRequiresEveryResponsesObjectToContainAResponseCode(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"Swagger 2.0": `{
			"swagger":"2.0","info":{"title":"API","version":"1"},
			"paths":{"/pets":{"get":{"responses":{
				"2XX":{"description":"range"},"x-note":true
			}}}}
		}`,
		"OpenAPI 3.0": `{
			"openapi":"3.0.4","info":{"title":"API","version":"1"},
			"paths":{"/pets":{"get":{"responses":{
				"600":{"description":"invalid"},"x-note":true
			}}}}
		}`,
		"OpenAPI 3.1": `{
			"openapi":"3.1.2","info":{"title":"API","version":"1"},
			"paths":{"/pets":{"get":{"responses":{
				"20X":{"description":"invalid"},"x-note":true
			}}}}
		}`,
		"OpenAPI 3.2": `{
			"openapi":"3.2.0","info":{"title":"API","version":"1"},
			"paths":{"/pets":{"get":{"responses":{"x-note":true}}}}
		}`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			document := mustDocument(t, source)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.responses.code.missing" &&
					diagnostic.InstanceLocation == "/paths/~1pets/get/responses" &&
					diagnostic.SpecificationSection == "responses-object" {
					return
				}
			}
			t.Fatalf("missing response-code diagnostic: %#v", report.Diagnostics())
		})
	}
}

func TestDocumentAcceptsDefaultAndResponseCodeEntries(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{
			"/default":{"get":{"responses":{"default":{"description":"fallback"}}}},
			"/exact":{"get":{"responses":{"204":{"description":"empty"}}}},
			"/range":{"get":{"responses":{"2XX":{"description":"success"}}}}
		}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.responses.code.missing" {
			t.Fatalf("valid response entry rejected: %#v", diagnostic)
		}
	}
}

func TestDocumentRejectsInvalidResponseCodeKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		marker  string
		invalid []string
	}{
		{"Swagger", `"swagger":"2.0"`, []string{"099", "2XX", "600", "2xx"}},
		{"OpenAPI 3.0", `"openapi":"3.0.4"`, []string{"099", "600", "2xx"}},
		{"OpenAPI 3.1", `"openapi":"3.1.2"`, []string{"099", "600", "2xx"}},
		{"OpenAPI 3.2", `"openapi":"3.2.0"`, []string{"099", "600", "2xx"}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{`+test.marker+`,
				"info":{"title":"API","version":"1"},
				"paths":{"/pets":{"get":{"responses":{
					"200":{"description":"ok"},
					"099":{"description":"invalid"},
					"2XX":{"description":"range"},
					"600":{"description":"invalid"},
					"2xx":{"description":"invalid"},
					"x-note":true
				}}}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			found := make(map[string]bool, len(test.invalid))
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.responses.code.invalid" {
					found[diagnostic.InstanceLocation] = true
				}
			}
			for _, code := range test.invalid {
				pointer := "/paths/~1pets/get/responses/" + code
				if !found[pointer] {
					t.Errorf("missing invalid code at %s: %#v", pointer, report.Diagnostics())
				}
			}
			if len(found) != len(test.invalid) {
				t.Errorf("unexpected invalid response codes: %#v", found)
			}
		})
	}
}

func TestDocumentRecommendsASuccessfulResponse(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"Swagger 2.0": `{
			"swagger":"2.0","info":{"title":"API","version":"1"},
			"paths":{"/pets":{"get":{"responses":{
				"default":{"description":"fallback"},
				"404":{"description":"missing"}
			}}}}
		}`,
		"OpenAPI 3.2": `{
			"openapi":"3.2.0","info":{"title":"API","version":"1"},
			"paths":{"/pets":{"get":{"responses":{
				"4XX":{"description":"client error"}
			}}}}
		}`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, source)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.responses.success.missing" &&
					diagnostic.InstanceLocation == "/paths/~1pets/get/responses" &&
					diagnostic.Severity == validate.SeverityWarning {
					return
				}
			}
			t.Fatalf("missing successful-response recommendation: %#v", report.Diagnostics())
		})
	}
}

func TestDocumentRecommendsRegisteredHTTPStatusCodes(t *testing.T) {
	t.Parallel()

	versions := []string{"3.0.4", "3.1.1", "3.1.2", "3.2.0"}
	for _, version := range versions {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{"/pets":{"get":{"responses":{
					"104":{"description":"temporary registered code"},
					"200":{"description":"ok"},
					"299":{"description":"unregistered code"},
					"2XX":{"description":"successful range"}
				}}}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}

			var found []validate.Diagnostic
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.responses.code.unregistered" {
					found = append(found, diagnostic)
				}
			}
			if len(found) != 1 {
				t.Fatalf("unregistered status-code diagnostics = %#v", found)
			}
			if found[0].InstanceLocation != "/paths/~1pets/get/responses/299" ||
				found[0].Severity != validate.SeverityWarning ||
				found[0].SpecificationSection != "http-status-codes" {
				t.Fatalf("unregistered status-code diagnostic = %#v", found[0])
			}
		})
	}
}
