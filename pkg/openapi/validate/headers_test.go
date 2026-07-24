package validate_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestDocumentValidatesHeaderContentCardinalityAcrossAllSurfaces(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/items":{"post":{
			"requestBody":{"content":{"multipart/form-data":{"encoding":{
				"item":{"headers":{"X-Part":{"content":{
					"text/plain":{},"application/json":{}
				}}}}
			}}}},
			"responses":{"204":{"description":"ok"}}
		}}},
		"webhooks":{"event":{"post":{"responses":{"200":{
			"description":"ok","headers":{"X-Hook":{"content":{
				"text/plain":{},"application/json":{}
			}}}
		}}}}},
		"components":{"headers":{"X-Shared":{"content":{
			"text/plain":{},"application/json":{}
		}}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/components/headers/X-Shared/content":                                                              false,
		"/webhooks/event/post/responses/200/headers/X-Hook/content":                                         false,
		"/paths/~1items/post/requestBody/content/multipart~1form-data/encoding/item/headers/X-Part/content": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.header.content.multiple" {
			continue
		}
		if _, exists := want[diagnostic.InstanceLocation]; exists {
			want[diagnostic.InstanceLocation] = true
		}
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing diagnostic at %s: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestDocumentValidatesHeaderParameterTraits(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{},"components":{"headers":{"Trace":{
			"name":"X-Trace","in":"query","style":"form",
			"allowEmptyValue":true,"allowReserved":true,
			"schema":{"type":"string"}
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"openapi.header.name.present":           false,
		"openapi.header.location.present":       false,
		"openapi.header.style.invalid":          false,
		"openapi.header.allow-empty.invalid":    false,
		"openapi.header.allow-reserved.invalid": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if _, exists := want[diagnostic.Code]; exists {
			want[diagnostic.Code] = true
		}
	}
	for code, found := range want {
		if !found {
			t.Errorf("missing header diagnostic %q: %#v", code, report.Diagnostics())
		}
	}
}

func TestHeaderTraitsAcrossPatchLines(t *testing.T) {
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
				"paths":{},"components":{"headers":{"Invalid":{
					"name":"X-Test","in":"query","style":"form",
					"schema":{"type":"string"}
				}}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]bool{
				"openapi.header.name.present":     false,
				"openapi.header.location.present": false,
				"openapi.header.style.invalid":    false,
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
		})
	}
}

func TestDocumentWarnsAboutIgnoredContentTypeHeaders(t *testing.T) {
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
				"paths":{"/pets":{"post":{
					"requestBody":{"content":{"multipart/form-data":{
						"schema":{"type":"object","properties":{"pet":{"type":"string"}}},
						"encoding":{"pet":{"headers":{
							"Content-Type":{"schema":{"type":"string"}}
						}}}
					}}},
					"responses":{"200":{
					"description":"ok","headers":{
						"CONTENT-TYPE":{"schema":{"type":"string"}},
						"X-Custom":{"schema":{"type":"string"}}
					}
				}}}}},
				"components":{"headers":{"content-type":{"schema":{"type":"string"}}}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			found := 0
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code != "openapi.header.content-type.ignored" {
					continue
				}
				if diagnostic.Severity != validate.SeverityWarning {
					t.Errorf("ignored header severity = %s", diagnostic.Severity)
				}
				found++
			}
			if found != 3 {
				t.Fatalf("ignored Content-Type warnings = %d: %#v", found, report.Diagnostics())
			}
		})
	}
}
