package validate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestSwaggerOperationSummaryRecommendation(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{
			"summary":"`+strings.Repeat("界", 120)+`",
			"responses":{"200":{"description":"ok"}}
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.swagger.operation.summary.long" &&
			diagnostic.InstanceLocation == "/paths/~1pets/get/summary" &&
			diagnostic.Severity == validate.SeverityWarning {
			return
		}
	}
	t.Fatalf("missing long summary warning: %#v", report.Diagnostics())
}

func TestSwaggerRootAndOperationTransportRules(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0",
		"host":"https://api.example/{tenant}",
		"basePath":"v1/{tenant}?debug=true",
		"schemes":["ftp",false],
		"consumes":["not a media type"],
		"produces":["application/json","also invalid"],
		"paths":{"/pets":{"get":{
			"schemes":["smtp"],
			"consumes":["invalid"],
			"produces":["text/plain","invalid response"],
			"responses":{"200":{"description":"ok"}}
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"openapi.swagger.host.invalid":       false,
		"openapi.swagger.base-path.invalid":  false,
		"openapi.swagger.scheme.invalid":     false,
		"openapi.swagger.media-type.invalid": false,
	}
	locations := map[string]bool{
		"/host":                        false,
		"/basePath":                    false,
		"/schemes/0":                   false,
		"/consumes/0":                  false,
		"/produces/1":                  false,
		"/paths/~1pets/get/schemes/0":  false,
		"/paths/~1pets/get/consumes/0": false,
		"/paths/~1pets/get/produces/1": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if _, exists := want[diagnostic.Code]; exists {
			want[diagnostic.Code] = true
			if _, tracked := locations[diagnostic.InstanceLocation]; tracked {
				locations[diagnostic.InstanceLocation] = true
				if diagnostic.SpecificationSection != "swagger-transport" ||
					diagnostic.SpecificationVersion != "2.0" {
					t.Fatalf("diagnostic metadata = %#v", diagnostic)
				}
			}
		}
	}
	for code, found := range want {
		if !found {
			t.Errorf("missing diagnostic %q: %#v", code, report.Diagnostics())
		}
	}
	for pointer, found := range locations {
		if !found {
			t.Errorf("missing diagnostic at %q: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestSwaggerAcceptsValidTransportDeclarations(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0",
		"host":"[2001:db8::1]:8443",
		"basePath":"/v1",
		"schemes":["http","https","ws","wss"],
		"consumes":["text/plain; charset=utf-8"],
		"produces":["application/vnd.example+json"],
		"paths":{"/pets":{"post":{
			"schemes":["https"],
			"consumes":[],
			"produces":[],
			"responses":{"200":{"description":"ok"}}
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.swagger.host.invalid" ||
			diagnostic.Code == "openapi.swagger.base-path.invalid" ||
			diagnostic.Code == "openapi.swagger.scheme.invalid" ||
			diagnostic.Code == "openapi.swagger.media-type.invalid" {
			t.Fatalf("valid transport declaration rejected: %#v", diagnostic)
		}
	}
}

func TestSwaggerTransportRulesDoNotApplyToOpenAPI(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","host":"invalid/path","basePath":"invalid",
		"schemes":["ftp"],"consumes":["invalid"],"paths":{}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if len(diagnostic.Code) >= len("openapi.swagger.") &&
			diagnostic.Code[:len("openapi.swagger.")] == "openapi.swagger." {
			t.Fatalf("Swagger rule applied to OpenAPI: %#v", diagnostic)
		}
	}
}

func TestSwaggerDefaultsConformToDeclaredTypes(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0",
		"info":{"title":"API","version":"1"},
		"parameters":{
			"Limit":{"name":"limit","in":"query","type":"integer","default":1.5}
		},
		"responses":{
			"Shared":{"description":"ok","headers":{
				"X-Ready":{"type":"boolean","default":"yes"},
				"X-Object":{"type":"object"}
			}}
		},
		"paths":{ "/items":{"get":{
			"parameters":[{
				"name":"values","in":"query","type":"array",
				"items":{"type":"array","items":{
					"type":"string","default":1
				}}
			}],
			"responses":{"200":{"description":"ok","headers":{
				"X-Count":{"type":"integer","default":2.5}
			}}}
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/parameters/Limit/default":                                false,
		"/responses/Shared/headers/X-Ready/default":                false,
		"/paths/~1items/get/parameters/0/items/items/default":      false,
		"/paths/~1items/get/responses/200/headers/X-Count/default": false,
	}
	foundHeaderType := false
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "swagger.header.type.invalid" &&
			diagnostic.InstanceLocation ==
				"/responses/Shared/headers/X-Object/type" {
			foundHeaderType = true
		}
		if diagnostic.Code != "openapi.swagger.default.type" {
			continue
		}
		if _, exists := want[diagnostic.InstanceLocation]; exists {
			want[diagnostic.InstanceLocation] = true
		}
	}
	if !foundHeaderType {
		t.Errorf("missing Swagger header type diagnostic: %#v", report.Diagnostics())
	}
	for pointer, found := range want {
		if !found {
			t.Fatalf("missing Swagger default diagnostic at %s: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestSwaggerAcceptsDefaultsOfDeclaredTypes(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0",
		"info":{"title":"API","version":"1"},
		"parameters":{
			"Enabled":{"name":"enabled","in":"query","type":"boolean","default":true},
			"Count":{"name":"count","in":"query","type":"integer","default":1e3},
			"Ratio":{"name":"ratio","in":"query","type":"number","default":1.5},
			"Name":{"name":"name","in":"query","type":"string","default":"value"},
			"Values":{"name":"values","in":"query","type":"array","items":{"type":"string"},"default":[]}
		},
		"paths":{}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.swagger.default.type" {
			t.Fatalf("valid Swagger default rejected: %#v", diagnostic)
		}
	}
}
