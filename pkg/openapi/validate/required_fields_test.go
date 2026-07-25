package validate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestDocumentEnforcesRequiredOpenAPIObjectFields(t *testing.T) {
	t.Parallel()

	versions := []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	}
	for _, version := range versions {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			cases := []struct {
				name    string
				body    string
				pointer string
			}{
				{"root info", `"paths":{}`, ""},
				{"info title", `"info":{"version":"1"},"paths":{}`, "/info"},
				{"info version", `"info":{"title":"API"},"paths":{}`, "/info"},
				{"license name", `"info":{"title":"API","version":"1","license":{}},"paths":{}`, "/info/license"},
				{"parameter name", `"info":{"title":"API","version":"1"},"paths":{},"components":{"parameters":{"Broken":{"in":"query"}}}`, "/components/parameters/Broken"},
				{"parameter location", `"info":{"title":"API","version":"1"},"paths":{},"components":{"parameters":{"Broken":{"name":"value"}}}`, "/components/parameters/Broken"},
				{"request body content", `"info":{"title":"API","version":"1"},"paths":{},"components":{"requestBodies":{"Broken":{}}}`, "/components/requestBodies/Broken"},
				{"tag name", `"info":{"title":"API","version":"1"},"paths":{},"tags":[{}]`, "/tags/0"},
			}
			if version != "3.2.0" {
				cases = append(cases, struct {
					name    string
					body    string
					pointer string
				}{"response description", `"info":{"title":"API","version":"1"},"paths":{},"components":{"responses":{"Broken":{}}}`, "/components/responses/Broken"})
			}
			if strings.HasPrefix(version, "3.0.") {
				cases = append(cases, struct {
					name    string
					body    string
					pointer string
				}{"root paths", `"info":{"title":"API","version":"1"}`, ""})
				cases = append(cases, struct {
					name    string
					body    string
					pointer string
				}{"operation responses", `"info":{"title":"API","version":"1"},"paths":{"/pets":{"get":{}}}`, "/paths/~1pets/get"})
			}
			for _, test := range cases {
				test := test
				t.Run(test.name, func(t *testing.T) {
					t.Parallel()
					document := mustDocument(t, `{"openapi":"`+version+`",`+test.body+`}`)
					report, err := validate.Document(context.Background(), document)
					if err != nil {
						t.Fatal(err)
					}
					for _, diagnostic := range report.Diagnostics() {
						if diagnostic.Source == validate.SourceDocument &&
							diagnostic.InstanceLocation == test.pointer {
							return
						}
					}
					t.Fatalf("missing required-field diagnostic at %q: %#v",
						test.pointer, report.Diagnostics())
				})
			}
		})
	}
}

func TestReferenceObjectsRequireReferenceIdentifiers(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"3.1.0", "3.1.1", "3.1.2", "3.2.0"} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{},"components":{"callbacks":{
					"Broken":{"summary":"missing reference identifier"}
				}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Source == validate.SourceDocument &&
					strings.HasPrefix(
						diagnostic.InstanceLocation,
						"/components/callbacks/Broken",
					) {
					return
				}
			}
			t.Fatalf("missing Reference Object required-field diagnostic: %#v",
				report.Diagnostics())
		})
	}
}

func TestDocumentEnforcesRequiredSecuritySchemeFields(t *testing.T) {
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
				"paths":{},"components":{"securitySchemes":{
					"MissingType":{},
					"ApiKeyName":{"type":"apiKey","in":"header"},
					"ApiKeyIn":{"type":"apiKey","name":"X-Key"},
					"HTTP":{"type":"http"},
					"OAuthFlows":{"type":"oauth2"},
					"OpenID":{"type":"openIdConnect"},
					"OAuthAuthorization":{"type":"oauth2","flows":{
						"implicit":{"scopes":{}}
					}},
					"OAuthToken":{"type":"oauth2","flows":{
						"password":{"scopes":{}}
					}},
					"OAuthClient":{"type":"oauth2","flows":{
						"clientCredentials":{"scopes":{}}
					}},
					"OAuthCode":{"type":"oauth2","flows":{
						"authorizationCode":{"scopes":{}}
					}},
					"OAuthScopes":{"type":"oauth2","flows":{
						"implicit":{"authorizationUrl":"https://example.test/auth"}
					}}
				}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]bool{
				"/components/securitySchemes/MissingType":        false,
				"/components/securitySchemes/ApiKeyName":         false,
				"/components/securitySchemes/ApiKeyIn":           false,
				"/components/securitySchemes/HTTP":               false,
				"/components/securitySchemes/OAuthFlows":         false,
				"/components/securitySchemes/OpenID":             false,
				"/components/securitySchemes/OAuthAuthorization": false,
				"/components/securitySchemes/OAuthToken":         false,
				"/components/securitySchemes/OAuthClient":        false,
				"/components/securitySchemes/OAuthCode":          false,
				"/components/securitySchemes/OAuthScopes":        false,
			}
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Source != validate.SourceDocument {
					continue
				}
				for pointer := range want {
					if strings.HasPrefix(diagnostic.InstanceLocation, pointer) {
						want[pointer] = true
					}
				}
			}
			for pointer, found := range want {
				if !found {
					t.Errorf("missing security required-field diagnostic at %q: %#v",
						pointer, report.Diagnostics())
				}
			}
		})
	}
}

func TestDocumentAcceptsEmptyOAuthScopeMaps(t *testing.T) {
	t.Parallel()

	versions := []string{"3.0.3", "3.0.4", "3.1.0", "3.1.1", "3.1.2", "3.2.0"}
	for _, version := range versions {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{},"components":{"securitySchemes":{"OAuth":{
					"type":"oauth2","flows":{"implicit":{
						"authorizationUrl":"https://example.test/authorize",
						"scopes":{}
					}}
				}}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Severity == validate.SeverityError &&
					strings.HasPrefix(diagnostic.InstanceLocation,
						"/components/securitySchemes/OAuth") {
					t.Errorf("empty OAuth scope map rejected: %#v", diagnostic)
				}
			}
		})
	}
}

func TestOpenAPI32DeviceAuthorizationRequiresURL(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{},"components":{"securitySchemes":{
			"OAuthDevice":{"type":"oauth2","flows":{
				"deviceAuthorization":{"scopes":{}}
			}}
		}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Source == validate.SourceDocument &&
			strings.HasPrefix(
				diagnostic.InstanceLocation,
				"/components/securitySchemes/OAuthDevice",
			) {
			return
		}
	}
	t.Fatalf("missing device authorization URL diagnostic: %#v",
		report.Diagnostics())
}
