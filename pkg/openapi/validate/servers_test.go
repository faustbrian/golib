package validate_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestDocumentValidatesServerVariablesAndNames(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0",
		"info":{"title":"API","version":"1"},
		"paths":{},
		"servers":[
			{
				"name":"primary",
				"url":"https://{region}.{region}.example.test?debug=1",
				"variables":{
					"region":{"default":"eu","enum":["us"]},
					"unused":{"default":"x"}
				}
			},
			{"name":"primary","url":"https://{missing}.example.test"}
		]
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"openapi.server.name.duplicate":               false,
		"openapi.server.url.query-or-fragment":        false,
		"openapi.server.variable.duplicate":           false,
		"openapi.server.variable.missing":             false,
		"openapi.server.variable.unused":              false,
		"openapi.server.variable.default-not-in-enum": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if _, exists := want[diagnostic.Code]; exists {
			want[diagnostic.Code] = true
		}
	}
	for code, seen := range want {
		if !seen {
			t.Errorf("missing server diagnostic %q: %#v", code, report.Diagnostics())
		}
	}
}

func TestOpenAPI30ServerEnumGuidanceIsWarning(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.0.4",
		"info":{"title":"API","version":"1"},
		"paths":{},
		"servers":[{
			"url":"https://{region}.example.test",
			"variables":{"region":{"default":"eu","enum":["us"]}}
		}]
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.server.variable.default-not-in-enum" {
			if diagnostic.Severity != validate.SeverityWarning {
				t.Fatalf("severity = %q", diagnostic.Severity)
			}
			return
		}
	}
	t.Fatalf("missing server enum diagnostic: %#v", report.Diagnostics())
}

func TestServerVariableEnumRulesAcrossPatchLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		version  string
		severity validate.Severity
		empty    bool
	}{
		{version: "3.0.3", severity: validate.SeverityWarning, empty: true},
		{version: "3.0.4", severity: validate.SeverityWarning, empty: true},
		{version: "3.1.0", severity: validate.SeverityError, empty: true},
		{version: "3.1.1", severity: validate.SeverityError, empty: true},
		{version: "3.1.2", severity: validate.SeverityError, empty: true},
		{version: "3.2.0", severity: validate.SeverityError, empty: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+test.version+`","info":{"title":"API","version":"1"},
				"paths":{},"servers":[{
					"url":"https://{region}.example.test",
					"variables":{"region":{"default":"eu","enum":[]}}
				}]
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			defaultFound := false
			emptyFound := false
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.server.variable.default-not-in-enum" {
					defaultFound = true
					if diagnostic.Severity != test.severity {
						t.Fatalf("default severity = %q", diagnostic.Severity)
					}
				}
				if diagnostic.Code == "openapi.server.variable.empty-enum" {
					emptyFound = true
					if diagnostic.Severity != test.severity {
						t.Fatalf("empty severity = %q", diagnostic.Severity)
					}
				}
			}
			if !defaultFound || test.empty != emptyFound {
				t.Fatalf("default=%t empty=%t diagnostics=%#v", defaultFound, emptyFound, report.Diagnostics())
			}
		})
	}
}

func TestDocumentValidatesServersAcrossEveryPathItemSurface(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{},
		"webhooks":{"event":{
			"servers":[{"url":"https://{webhook}.example.test"}],
			"post":{"responses":{"204":{"description":"ok"}}}
		}},
		"components":{
			"pathItems":{"Shared":{
				"servers":[{"url":"https://{shared}.example.test"}],
				"get":{"responses":{"200":{"description":"ok"}}}
			}},
			"callbacks":{"Event":{"{$request.body#/url}":{
				"post":{
					"servers":[{"url":"https://{callback}.example.test"}],
					"responses":{"204":{"description":"ok"}}
				}
			}}}
		}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/webhooks/event/servers/0/variables":                                        false,
		"/components/pathItems/Shared/servers/0/variables":                           false,
		"/components/callbacks/Event/{$request.body#~1url}/post/servers/0/variables": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.server.variable.missing" {
			if _, exists := want[diagnostic.InstanceLocation]; exists {
				want[diagnostic.InstanceLocation] = true
			}
		}
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing diagnostic at %q: %#v", pointer, report.Diagnostics())
		}
	}
}
