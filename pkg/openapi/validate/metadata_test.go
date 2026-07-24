package validate_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestDocumentValidatesInfoMetadataAcrossVersions(t *testing.T) {
	t.Parallel()

	versions := []string{
		"2.0", "3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	}
	for _, version := range versions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			marker := `"openapi":"` + version + `"`
			terms := `,"termsOfService":"invalid terms"`
			if version == "2.0" {
				marker = `"swagger":"2.0"`
				terms = ""
			}
			document := mustDocument(t, `{`+marker+`,"info":{
				"title":"API","version":"1"`+terms+`,
				"contact":{"url":"invalid contact","email":"Jane <jane@example.test>"},
				"license":{"name":"Example","url":"invalid license"}
			},"paths":{}}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]bool{
				"openapi.contact.url.invalid":   false,
				"openapi.contact.email.invalid": false,
				"openapi.license.url.invalid":   false,
			}
			if version != "2.0" {
				want["openapi.info.terms-of-service.invalid"] = false
			}
			for _, diagnostic := range report.Diagnostics() {
				if _, exists := want[diagnostic.Code]; exists {
					want[diagnostic.Code] = true
					if diagnostic.SpecificationVersion != version ||
						diagnostic.SpecificationSection != "info-object" {
						t.Fatalf("diagnostic metadata = %#v", diagnostic)
					}
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

func TestOpenAPIMetadataAcceptsRelativeURLReferences(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		document := mustDocument(t, `{
			"openapi":"`+version+`","info":{"title":"API","version":"1",
				"termsOfService":"terms","contact":{"url":"contact"},
				"license":{"name":"Example","url":"licenses/example"}
			},"paths":{}
		}`)
		report, err := validate.Document(context.Background(), document)
		if err != nil {
			t.Fatal(err)
		}
		for _, diagnostic := range report.Diagnostics() {
			switch diagnostic.Code {
			case "openapi.info.terms-of-service.invalid",
				"openapi.contact.url.invalid", "openapi.license.url.invalid":
				t.Fatalf("%s relative URL rejected: %#v", version, diagnostic)
			}
		}
	}
}

func TestDocumentRejectsLicenseURLAndIdentifierTogether(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"3.1.0", "3.1.1", "3.1.2", "3.2.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{
					"title":"API","version":"1","license":{
						"name":"Apache 2.0","identifier":"Apache-2.0",
						"url":"https://www.apache.org/licenses/LICENSE-2.0"
					}
				},"paths":{}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.license.identifier-and-url" &&
					diagnostic.InstanceLocation == "/info/license" {
					return
				}
			}
			t.Fatalf("missing license exclusivity diagnostic: %#v", report.Diagnostics())
		})
	}
}

func TestDocumentAcceptsValidInfoMetadata(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{
			"title":"API","version":"1",
			"termsOfService":"https://example.test/terms",
			"contact":{"url":"https://example.test/contact",
				"email":"team+api@example.test"},
			"license":{"name":"Apache 2.0","identifier":"Apache-2.0"}
		},"paths":{}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		switch diagnostic.Code {
		case "openapi.info.terms-of-service.invalid",
			"openapi.contact.url.invalid",
			"openapi.contact.email.invalid",
			"openapi.license.url.invalid",
			"openapi.license.identifier-and-url":
			t.Fatalf("valid metadata rejected: %#v", diagnostic)
		}
	}
}
