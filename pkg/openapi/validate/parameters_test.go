package validate_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/reference"
	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestDocumentValidatesOpenAPIParameterSemantics(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2",
		"info":{"title":"API","version":"1"},
		"paths":{},
		"components":{"parameters":{
			"BadStyle":{
				"name":"q","in":"query","style":"simple",
				"schema":{"type":"string"},
				"content":{"text/plain":{"schema":{"type":"string"}}}
			},
			"BadContent":{
				"name":"h","in":"header","allowReserved":true,
				"content":{
					"text/plain":{"schema":{"type":"string"}},
					"application/json":{"schema":{"type":"string"}}
				},
				"example":"one","examples":{"named":{"value":"two"}}
			}
		}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"openapi.parameter.style.invalid-for-location": false,
		"openapi.parameter.schema-and-content":         false,
		"openapi.parameter.content.multiple":           false,
		"openapi.parameter.allow-reserved.invalid":     false,
		"openapi.parameter.examples.conflict":          false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.document.$ref" ||
			diagnostic.Code == "openapi.document.allOf" {
			t.Errorf("structural summary leaked: %#v", diagnostic)
		}
		if _, exists := want[diagnostic.Code]; exists {
			want[diagnostic.Code] = true
		}
	}
	for code, seen := range want {
		if !seen {
			t.Errorf("missing parameter diagnostic %q: %#v", code, report.Diagnostics())
		}
	}
}

func TestParameterRepresentationChoiceAcrossPatchLines(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`",
				"info":{"title":"API","version":"1"},
				"paths":{},"components":{"parameters":{"Invalid":{
					"name":"q","in":"query",
					"schema":{"type":"string"},"content":{}
				}}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.parameter.schema-and-content" {
					return
				}
			}
			t.Fatalf("missing representation diagnostic: %#v", report.Diagnostics())
		})
	}
}

func TestCommonParameterAndHeaderFieldsWorkWithContent(t *testing.T) {
	t.Parallel()

	versions := []string{"3.0.4", "3.1.1", "3.1.2", "3.2.0"}
	for _, version := range versions {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{},"components":{
					"parameters":{"Query":{
						"name":"q","in":"query","description":"query value",
						"required":true,"deprecated":true,"allowEmptyValue":true,
						"content":{"application/json":{"schema":{"type":"string"}}}
					}},
					"headers":{"Trace":{
						"description":"trace value","required":true,"deprecated":true,
						"content":{"application/json":{"schema":{"type":"string"}}}
					}}
				}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Severity == validate.SeverityError &&
					(strings.HasPrefix(diagnostic.InstanceLocation,
						"/components/parameters/Query") ||
						strings.HasPrefix(diagnostic.InstanceLocation,
							"/components/headers/Trace")) {
					t.Errorf("common content field rejected: %#v", diagnostic)
				}
			}
		})
	}
}

func TestDocumentWarnsAboutIgnoredHeaderParameters(t *testing.T) {
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
				"paths":{},"components":{"parameters":{
					"Accept":{"name":"Accept","in":"header","schema":{"type":"string"}},
					"ContentType":{"name":"content-type","in":"header","schema":{"type":"string"}},
					"Authorization":{"name":"AUTHORIZATION","in":"header","schema":{"type":"string"}},
					"Custom":{"name":"X-Custom","in":"header","schema":{"type":"string"}}
				}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			found := 0
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code != "openapi.parameter.header.ignored" {
					continue
				}
				if diagnostic.Severity != validate.SeverityWarning {
					t.Errorf("ignored header severity = %s", diagnostic.Severity)
				}
				found++
			}
			if found != 3 {
				t.Fatalf("ignored header warnings = %d: %#v", found, report.Diagnostics())
			}
		})
	}
}

func TestIgnoredHeaderParametersAreExcludedFromSemanticConsumers(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		document := mustDocument(t, `{
			"openapi":"`+version+`","info":{"title":"API","version":"1"},
			"paths":{"/items":{"get":{"parameters":[
				{"name":"Accept","in":"header","style":"deepObject",
					"deprecated":true,"schema":{"type":"integer"},"example":"bad"},
				{"name":"accept","in":"header","style":"deepObject",
					"schema":{"type":"string"}},
				{"name":"X-Custom","in":"header","style":"deepObject",
					"schema":{"type":"string"}}
			],"responses":{"204":{"description":"ok"}}}}}
		}`)
		report, err := validate.Document(context.Background(), document)
		if err != nil {
			t.Fatalf("OpenAPI %s: %v", version, err)
		}
		ignoredWarnings := 0
		customStyle := false
		for _, diagnostic := range report.Diagnostics() {
			if diagnostic.Code == "openapi.parameter.header.ignored" {
				ignoredWarnings++
			}
			if diagnostic.Code == "openapi.parameter.style.invalid-for-location" &&
				diagnostic.InstanceLocation ==
					"/paths/~1items/get/parameters/2/style" {
				customStyle = true
			}
			if strings.HasPrefix(
				diagnostic.InstanceLocation,
				"/paths/~1items/get/parameters/0",
			) || strings.HasPrefix(
				diagnostic.InstanceLocation,
				"/paths/~1items/get/parameters/1",
			) {
				switch diagnostic.Code {
				case "openapi.parameter.duplicate",
					"openapi.parameter.style.invalid-for-location",
					"openapi.parameter.deprecated",
					"openapi.example.schema":
					t.Errorf("OpenAPI %s evaluated ignored parameter: %#v",
						version, diagnostic)
				}
			}
		}
		if ignoredWarnings != 2 || !customStyle {
			t.Errorf("OpenAPI %s ignored warnings %d, custom style %t: %#v",
				version, ignoredWarnings, customStyle, report.Diagnostics())
		}
	}
}

func TestDocumentRecommendsContentForCookieParameters(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"3.0.4", "3.1.1", "3.1.2"} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{},"components":{"parameters":{
					"Schema":{"name":"session","in":"cookie",
						"schema":{"type":"string"}},
					"Content":{"name":"session2","in":"cookie","content":{
						"text/plain":{"schema":{"type":"string"}}}}
				}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			found := 0
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code != "openapi.parameter.cookie.schema.nonportable" {
					continue
				}
				found++
				if diagnostic.InstanceLocation !=
					"/components/parameters/Schema/schema" ||
					diagnostic.Severity != validate.SeverityWarning ||
					diagnostic.SpecificationSection != "parameter-object" {
					t.Fatalf("diagnostic = %#v", diagnostic)
				}
			}
			if found != 1 {
				t.Fatalf("cookie schema warnings = %d: %#v", found, report.Diagnostics())
			}
		})
	}
}

func TestCookieSchemaRecommendationUsesOnlyApplicableRevisions(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		document := mustDocument(t, `{
			"openapi":"`+version+`","info":{"title":"API","version":"1"},
			"paths":{},"components":{"parameters":{"Cookie":{
				"name":"session","in":"cookie","schema":{"type":"string"}
			}}}
		}`)
		report, err := validate.Document(context.Background(), document)
		if err != nil {
			t.Fatal(err)
		}
		for _, diagnostic := range report.Diagnostics() {
			if diagnostic.Code == "openapi.parameter.cookie.schema.nonportable" {
				t.Fatalf("version %s diagnostic = %#v", version, diagnostic)
			}
		}
	}
}

func TestDocumentValidatesParametersAcrossEveryPathItemSurface(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{},
		"webhooks":{"event":{"parameters":[{
			"name":"hook","in":"header","content":{}
		}]}},
		"components":{
			"pathItems":{"Shared":{"get":{
				"parameters":[{"name":"shared","in":"query","content":{}}],
				"responses":{"204":{"description":"ok"}}
			}}},
			"callbacks":{"Event":{"{$request.body#/url}":{"post":{
				"parameters":[{"name":"callback","in":"query","content":{}}],
				"responses":{"204":{"description":"ok"}}
			}}}}
		}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/webhooks/event/parameters/0/content":                                        false,
		"/components/pathItems/Shared/get/parameters/0/content":                       false,
		"/components/callbacks/Event/{$request.body#~1url}/post/parameters/0/content": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.parameter.content.multiple" {
			if _, exists := want[diagnostic.InstanceLocation]; exists {
				want[diagnostic.InstanceLocation] = true
			}
		}
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing diagnostic at %s: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestDocumentAcceptsOpenAPI32CookieParameterStyle(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0",
		"info":{"title":"API","version":"1"},
		"paths":{},
		"components":{"parameters":{"Session":{
			"name":"session","in":"cookie","style":"cookie",
			"schema":{"type":"string"}
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.parameter.style.invalid-for-location" {
			t.Fatalf("OpenAPI 3.2 cookie style was rejected: %#v", diagnostic)
		}
	}
}

func TestDocumentValidatesOpenAPI32QueryStringRepresentation(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0",
		"info":{"title":"API","version":"1"},
		"paths":{},
		"components":{"parameters":{
			"Valid":{
				"name":"query","in":"querystring",
				"content":{"application/json":{"schema":{"type":"object"}}}
			},
			"Invalid":{
				"name":"query","in":"querystring","style":"form",
				"explode":true,"allowReserved":true,
				"schema":{"type":"object"}
			}
		}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"openapi.parameter.querystring.style":          false,
		"openapi.parameter.querystring.explode":        false,
		"openapi.parameter.querystring.allow-reserved": false,
		"openapi.parameter.querystring.schema":         false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if _, exists := want[diagnostic.Code]; exists {
			want[diagnostic.Code] = true
			if !strings.Contains(diagnostic.InstanceLocation, "/Invalid/") {
				t.Fatalf("valid querystring parameter was rejected: %#v", diagnostic)
			}
		}
	}
	for code, found := range want {
		if !found {
			t.Errorf("missing %s: %#v", code, report.Diagnostics())
		}
	}
}

func TestDocumentValidatesOpenAPI32QueryStringScope(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0",
		"info":{"title":"API","version":"1"},
		"paths":{"/items":{
			"parameters":[{
				"name":"whole","in":"querystring",
				"content":{"text/plain":{"schema":{"type":"string"}}}
			}],
			"get":{"parameters":[{
				"name":"page","in":"query","schema":{"type":"integer"}
			}],"responses":{"200":{"description":"ok"}}},
			"post":{"parameters":[{
				"name":"other","in":"querystring",
				"content":{"application/json":{"schema":{"type":"object"}}}
			}],"responses":{"200":{"description":"ok"}}}
		}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"openapi.parameter.querystring.with-query": false,
		"openapi.parameter.querystring.multiple":   false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if _, exists := want[diagnostic.Code]; exists {
			want[diagnostic.Code] = true
		}
	}
	for code, found := range want {
		if !found {
			t.Errorf("missing %s: %#v", code, report.Diagnostics())
		}
	}
}

func TestDocumentAllowsOperationToOverridePathQueryStringParameter(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0",
		"info":{"title":"API","version":"1"},
		"paths":{"/items":{
			"parameters":[{
				"name":"whole","in":"querystring",
				"content":{"text/plain":{"schema":{"type":"string"}}}
			}],
			"get":{"parameters":[{
				"name":"whole","in":"querystring",
				"content":{"application/json":{"schema":{"type":"object"}}}
			}],"responses":{"200":{"description":"ok"}}}
		}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.parameter.querystring.multiple" {
			t.Fatalf("operation override was counted twice: %#v", diagnostic)
		}
	}
}

func TestDocumentRejectsDuplicateParameterIdentitiesWithinOneList(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0",
		"info":{"title":"API","version":"1"},
		"paths":{"/items":{"get":{
			"parameters":[
				{"name":"page","in":"query","schema":{"type":"integer"}},
				{"name":"page","in":"query","schema":{"type":"string"}},
				{"name":"X-Trace","in":"header","schema":{"type":"string"}},
				{"name":"x-trace","in":"header","schema":{"type":"string"}}
			],
			"responses":{"200":{"description":"ok"}}
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	duplicates := 0
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.parameter.duplicate" {
			duplicates++
		}
	}
	if duplicates != 2 {
		t.Fatalf("duplicate diagnostics = %d, want 2: %#v", duplicates, report.Diagnostics())
	}
}

func TestDocumentValidatesSwagger20ParameterSemantics(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0",
		"info":{"title":"API","version":"1"},
		"parameters":{
			"BadMulti":{
				"name":"tags","in":"header","type":"array",
				"items":{"type":"string"},"collectionFormat":"multi"
			},
			"BadScalarFormat":{
				"name":"page","in":"query","type":"integer",
				"collectionFormat":"csv"
			},
			"BadEmpty":{
				"name":"id","in":"path","required":true,"type":"string",
				"allowEmptyValue":true
			}
		},
		"paths":{}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"swagger.parameter.collection-format.multi-location": false,
		"swagger.parameter.collection-format.non-array":      false,
		"swagger.parameter.allow-empty.invalid":              false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if _, exists := want[diagnostic.Code]; exists {
			want[diagnostic.Code] = true
		}
	}
	for code, found := range want {
		if !found {
			t.Errorf("missing %s: %#v", code, report.Diagnostics())
		}
	}
}

func TestDocumentWarnsAboutDeprecatedAllowEmptyValue(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		document := mustDocument(t, `{
			"openapi":"`+version+`","info":{"title":"API","version":"1"},
			"paths":{},"components":{"parameters":{"Empty":{
				"name":"value","in":"query","allowEmptyValue":true,
				"schema":{"type":"string"}
			}}}
		}`)
		report, err := validate.Document(context.Background(), document)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, diagnostic := range report.Diagnostics() {
			if diagnostic.Code == "openapi.parameter.allow-empty.deprecated" {
				found = true
				if diagnostic.Severity != validate.SeverityWarning {
					t.Fatalf("version %s diagnostic = %#v", version, diagnostic)
				}
			}
		}
		want := version != "3.0.0" && version != "3.0.1"
		if found != want {
			t.Fatalf("version %s deprecated diagnostic = %t, want %t",
				version, found, want)
		}
	}
}

func TestDocumentValidatesSwaggerParameterAndItemsTypes(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"parameters":{
			"Missing":{"name":"missing","in":"query"},
			"Object":{"name":"object","in":"query","type":"object"},
			"Items":{"name":"items","in":"query","type":"array",
				"items":{"type":"file"}}
		},"paths":{}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/parameters/Missing/type":     false,
		"/parameters/Object/type":      false,
		"/parameters/Items/items/type": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "swagger.parameter.type.invalid" &&
			diagnostic.Code != "swagger.items.type.invalid" {
			continue
		}
		if _, exists := want[diagnostic.InstanceLocation]; exists {
			want[diagnostic.InstanceLocation] = true
		}
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing Swagger type diagnostic at %s: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestDocumentAcceptsOptionalSwaggerNonPathParameters(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"parameters":{
			"Implicit":{"name":"implicit","in":"query","type":"string"},
			"Explicit":{"name":"explicit","in":"header","type":"string",
				"required":false}
		},"paths":{}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() {
		t.Fatalf("optional non-path parameters rejected: %#v", report.Diagnostics())
	}
}

func TestDocumentRequiresReusablePathParametersAcrossVersions(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"2.0", "3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			var raw string
			if version == "2.0" {
				raw = `{
					"swagger":"2.0","info":{"title":"API","version":"1"},
					"paths":{},"parameters":{"ID":{
						"name":"id","in":"path","required":false,"type":"string"
					}}
				}`
			} else {
				raw = `{
					"openapi":"` + version + `","info":{"title":"API","version":"1"},
					"paths":{},"components":{"parameters":{"ID":{
						"name":"id","in":"path","required":false,
						"schema":{"type":"string"}
					}}}
				}`
			}
			report, err := validate.Document(context.Background(), mustDocument(t, raw))
			if err != nil {
				t.Fatal(err)
			}
			diagnostics := report.Diagnostics()
			if len(diagnostics) != 1 ||
				diagnostics[0].Code != "openapi.path.parameter.not-required" {
				t.Fatalf("%s diagnostics = %#v", version, diagnostics)
			}
		})
	}
}

func TestDocumentValidatesSwaggerFileParameterTransport(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"consumes":["multipart/form-data"],
		"parameters":{
			"WrongLocation":{"name":"upload","in":"query","type":"file"},
			"Upload":{"name":"upload","in":"formData","type":"file"}
		},
		"paths":{
			"/wrong-location":{"post":{"parameters":[
				{"$ref":"#/parameters/WrongLocation"}
			],"responses":{"200":{"description":"ok"}}}},
			"/wrong-consumes":{"post":{
				"consumes":["application/json"],
				"parameters":[{"$ref":"#/parameters/Upload"}],
				"responses":{"200":{"description":"ok"}}
			}},
			"/mixed-consumes":{"post":{
				"consumes":["multipart/form-data","application/json"],
				"parameters":[{"name":"upload","in":"formData","type":"file"}],
				"responses":{"200":{"description":"ok"}}
			}}
		}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		"swagger.parameter.file.location": 1,
		"swagger.parameter.file.consumes": 2,
	}
	for _, diagnostic := range report.Diagnostics() {
		if _, exists := want[diagnostic.Code]; exists {
			want[diagnostic.Code]--
			if diagnostic.SpecificationSection != "swagger-parameter-object" {
				t.Fatalf("diagnostic metadata = %#v", diagnostic)
			}
		}
	}
	for code, remaining := range want {
		if remaining != 0 {
			t.Errorf("diagnostic %s remaining = %d: %#v", code, remaining, report.Diagnostics())
		}
	}
}

func TestDocumentAcceptsSwaggerFileParameterTransport(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"consumes":["application/x-www-form-urlencoded"],
		"paths":{"/upload":{"post":{"parameters":[
			{"name":"upload","in":"formData","type":"file"}
		],"responses":{"200":{"description":"ok"}}}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "swagger.parameter.file.location" ||
			diagnostic.Code == "swagger.parameter.file.consumes" {
			t.Fatalf("valid file parameter rejected: %#v", diagnostic)
		}
	}
}

func TestDocumentRejectsSwaggerBodyAndFormParameterConflicts(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"paths":{"/submit":{"post":{
			"parameters":[
				{"name":"first","in":"body","schema":{"type":"string"}},
				{"name":"second","in":"body","schema":{"type":"string"}},
				{"name":"field","in":"formData","type":"string"}
			],
			"responses":{"200":{"description":"ok"}}
		}},
		"/inherited":{
			"parameters":[{"name":"first","in":"body","schema":{"type":"string"}}],
			"post":{"parameters":[
				{"name":"second","in":"body","schema":{"type":"string"}}
			],"responses":{"200":{"description":"ok"}}}
		}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		"swagger.parameter.body.multiple":      2,
		"swagger.parameter.body-and-form-data": 1,
	}
	for _, diagnostic := range report.Diagnostics() {
		if _, exists := want[diagnostic.Code]; exists {
			want[diagnostic.Code]--
		}
	}
	for code, remaining := range want {
		if remaining != 0 {
			t.Errorf("diagnostic %s remaining = %d: %#v", code, remaining, report.Diagnostics())
		}
	}
}

func TestDocumentRejectsExternalSwaggerPayloadParameterConflicts(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"paths":{"/submit":{
			"parameters":[{"$ref":"parameters.json#/BodyA"}],
			"post":{"parameters":[
				{"$ref":"parameters.json#/BodyB"},
				{"$ref":"parameters.json#/Form"}
			],"responses":{"200":{"description":"ok"}}}
		}}
	}`)
	external := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"Parameters","version":"1"},
		"paths":{},
		"BodyA":{"name":"first","in":"body","schema":{"type":"string"}},
		"BodyB":{"name":"second","in":"body","schema":{"type":"string"}},
		"Form":{"name":"field","in":"formData","type":"string"}
	}`).Raw()
	options := validate.DefaultOptions()
	options.ReferenceResourceURI = "https://api.example.test/swagger.json"
	options.ReferenceResolver = reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		if identifier != "https://api.example.test/parameters.json" {
			return reference.Resource{}, fmt.Errorf(
				"unexpected resolved identifier %q", identifier,
			)
		}
		return reference.Resource{RetrievalURI: identifier, Root: external}, nil
	})
	report, err := validate.DocumentWithOptions(
		context.Background(), document, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"swagger.parameter.body.multiple":      false,
		"swagger.parameter.body-and-form-data": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if _, tracked := want[diagnostic.Code]; tracked {
			want[diagnostic.Code] = true
		}
	}
	for code, found := range want {
		if !found {
			t.Errorf("missing %s: %#v", code, report.Diagnostics())
		}
	}
}

func TestDocumentValidatesExternalSwaggerFileParameters(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"consumes":["application/json"],
		"paths":{"/upload":{"post":{"parameters":[
			{"$ref":"parameters.json#/parameters/WrongLocation"},
			{"$ref":"parameters.json#/parameters/WrongConsumes"},
			{"$ref":"parameters.json#/parameters/Missing"},
			null
		],"responses":{"200":{"description":"ok"}}}}}
	}`)
	external := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"Parameters","version":"1"},
		"paths":{},"parameters":{
			"WrongLocation":{"name":"upload","in":"query","type":"file"},
			"WrongConsumes":{"name":"other","in":"formData","type":"file"}
		}
	}`).Raw()
	options := validate.DefaultOptions()
	options.ReferenceResourceURI = "https://api.example.test/swagger.json"
	options.ReferenceResolver = reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		if identifier != "https://api.example.test/parameters.json" {
			return reference.Resource{}, fmt.Errorf(
				"unexpected resolved identifier %q", identifier,
			)
		}
		return reference.Resource{RetrievalURI: identifier, Root: external}, nil
	})
	report, err := validate.DocumentWithOptions(
		context.Background(), document, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"swagger.parameter.file.location": false,
		"swagger.parameter.file.consumes": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if _, tracked := want[diagnostic.Code]; tracked {
			want[diagnostic.Code] = true
		}
	}
	for code, found := range want {
		if !found {
			t.Errorf("missing %s: %#v", code, report.Diagnostics())
		}
	}
}

func TestDocumentAcceptsSeparateSwaggerBodyAndFormOperations(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"paths":{
			"/body":{"post":{"parameters":[
				{"name":"payload","in":"body","schema":{"type":"string"}}
			],"responses":{"200":{"description":"ok"}}}},
			"/form":{"post":{"parameters":[
				{"name":"field","in":"formData","type":"string"}
			],"responses":{"200":{"description":"ok"}}}}
		}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "swagger.parameter.body.multiple" ||
			diagnostic.Code == "swagger.parameter.body-and-form-data" {
			t.Fatalf("valid payload parameters rejected: %#v", diagnostic)
		}
	}
}

func TestDocumentRejectsSwaggerDuplicateParameterIdentities(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"paths":{"/items":{"get":{"parameters":[
			{"name":"page","in":"query","type":"integer"},
			{"name":"page","in":"query","type":"string"}
		],"responses":{"200":{"description":"ok"}}}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.parameter.duplicate" {
			return
		}
	}
	t.Fatalf("missing Swagger duplicate diagnostic: %#v", report.Diagnostics())
}

func TestDocumentResolvesInternalParameterReferencesForIdentity(t *testing.T) {
	t.Parallel()

	tests := []string{
		`{
			"swagger":"2.0","info":{"title":"API","version":"1"},
			"parameters":{"Page":{"name":"page","in":"query","type":"integer"}},
			"paths":{"/items":{"get":{"parameters":[
				{"$ref":"#/parameters/Page"},{"$ref":"#/parameters/Page"}
			],"responses":{"200":{"description":"ok"}}}}}
		}`,
		`{
			"openapi":"3.2.0","info":{"title":"API","version":"1"},
			"components":{"parameters":{"Page":{
				"name":"page","in":"query","schema":{"type":"integer"}
			}}},
			"paths":{"/items":{"get":{"parameters":[
				{"$ref":"#/components/parameters/Page"},
				{"$ref":"#/components/parameters/Page"}
			],"responses":{"200":{"description":"ok"}}}}}
		}`,
	}
	for _, raw := range tests {
		document := mustDocument(t, raw)
		report, err := validate.Document(context.Background(), document)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, diagnostic := range report.Diagnostics() {
			if diagnostic.Code == "openapi.parameter.duplicate" {
				found = true
			}
		}
		if !found {
			t.Errorf("missing referenced duplicate diagnostic: %#v", report.Diagnostics())
		}
	}
}
