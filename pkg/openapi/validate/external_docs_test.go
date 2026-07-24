package validate_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestDocumentValidatesExternalDocumentationURLsAcrossVersions(t *testing.T) {
	t.Parallel()

	versions := []string{
		"2.0", "3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	}
	for _, version := range versions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			marker := `"openapi":"` + version + `"`
			if version == "2.0" {
				marker = `"swagger":"2.0"`
			}
			document := mustDocument(t, `{`+marker+`,
				"info":{"title":"API","version":"1"},"paths":{},
				"externalDocs":{"url":"relative docs"}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.external-docs.url.invalid" &&
					diagnostic.InstanceLocation == "/externalDocs/url" &&
					diagnostic.SpecificationVersion == version &&
					diagnostic.SpecificationSection == "external-documentation-object" {
					return
				}
			}
			t.Fatalf("missing external-documentation diagnostic: %#v", report.Diagnostics())
		})
	}
}

func TestDocumentRequiresExternalDocumentationURLAcrossVersions(t *testing.T) {
	t.Parallel()

	versions := []string{
		"2.0", "3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	}
	for _, version := range versions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			marker := `"openapi":"` + version + `"`
			if version == "2.0" {
				marker = `"swagger":"2.0"`
			}
			document := mustDocument(t, `{`+marker+`,
				"info":{"title":"API","version":"1"},"paths":{},
				"externalDocs":{}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.document.required" &&
					diagnostic.InstanceLocation == "/externalDocs" {
					return
				}
			}
			t.Fatalf("missing required URL diagnostic: %#v", report.Diagnostics())
		})
	}
}

func TestDocumentValidatesEveryExternalDocumentationSurface(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{
			"externalDocs":{"url":"invalid operation"},
			"responses":{"200":{"description":"ok"}}
		}}},
		"tags":[{"name":"pets","externalDocs":{"url":"invalid tag"}}],
		"components":{"schemas":{"Pet":{"type":"object","properties":{
			"name":{"type":"string","externalDocs":{"url":"invalid schema"}}
		},
		"$defs":{"Defined":{"externalDocs":{"url":"invalid defined"}}},
		"allOf":[{"externalDocs":{"url":"invalid composed"}}],
		"additionalProperties":{"externalDocs":{"url":"invalid additional"}},
		"dependentSchemas":[],"not":1
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/paths/~1pets/get/externalDocs/url":                            false,
		"/tags/0/externalDocs/url":                                      false,
		"/components/schemas/Pet/properties/name/externalDocs/url":      false,
		"/components/schemas/Pet/$defs/Defined/externalDocs/url":        false,
		"/components/schemas/Pet/allOf/0/externalDocs/url":              false,
		"/components/schemas/Pet/additionalProperties/externalDocs/url": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.external-docs.url.invalid" {
			continue
		}
		if _, exists := want[diagnostic.InstanceLocation]; exists {
			want[diagnostic.InstanceLocation] = true
		}
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing diagnostic at %q: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestDocumentValidatesSwaggerExternalDocumentationInSchemas(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"definitions":{"Pet":{"type":"object","properties":{
			"name":{"type":"string","externalDocs":{"url":"relative-definition"}}
		}}},
		"parameters":{"Payload":{"name":"payload","in":"body","schema":{
			"type":"string","externalDocs":{"url":"relative-parameter"}
		}}},
		"responses":{"Failure":{"description":"failed","schema":{
			"type":"string","externalDocs":{"url":"relative-response"}
		}}},
		"paths":{"/pets":{"post":{
			"parameters":[{"$ref":"#/parameters/Payload"}],
			"responses":{"400":{"$ref":"#/responses/Failure"}}
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/definitions/Pet/properties/name/externalDocs/url": false,
		"/parameters/Payload/schema/externalDocs/url":       false,
		"/responses/Failure/schema/externalDocs/url":        false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.external-docs.url.invalid" {
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

func TestDocumentAcceptsAbsoluteExternalDocumentationURL(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{},
		"externalDocs":{"url":"https://docs.example.test/guide?q=1#start"}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.external-docs.url.invalid" {
			t.Fatalf("absolute documentation URL rejected: %#v", diagnostic)
		}
	}
}

func TestOpenAPIExternalDocumentationAcceptsRelativeURLReferences(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		document := mustDocument(t, `{
			"openapi":"`+version+`","info":{"title":"API","version":"1"},
			"paths":{},"externalDocs":{"url":"docs/guide"}
		}`)
		report, err := validate.Document(context.Background(), document)
		if err != nil {
			t.Fatal(err)
		}
		for _, diagnostic := range report.Diagnostics() {
			if diagnostic.Code == "openapi.external-docs.url.invalid" {
				t.Fatalf("%s relative documentation URL rejected: %#v",
					version, diagnostic)
			}
		}
	}
}
