package validate_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/reference"
	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestDocumentValidatesSecurityRequirementConnections(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.0.4",
		"info":{"title":"API","version":"1"},
		"paths":{},
		"components":{"securitySchemes":{
			"ApiKey":{"type":"apiKey","name":"key","in":"header"},
			"OAuth":{"type":"oauth2","flows":{
				"authorizationCode":{
					"authorizationUrl":"https://auth.example.test/authorize",
					"tokenUrl":"https://auth.example.test/token",
					"scopes":{"read":"Read access"}
				}
			}}
		}},
		"security":[
			{"Missing":[]},
			{"ApiKey":["admin"]},
			{"OAuth":["write"]}
		]
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"openapi.security.scheme.unknown":      false,
		"openapi.security.roles.not-allowed":   false,
		"openapi.security.oauth-scope.unknown": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if _, exists := want[diagnostic.Code]; exists {
			want[diagnostic.Code] = true
		}
	}
	for code, seen := range want {
		if !seen {
			t.Errorf("missing security diagnostic %q: %#v", code, report.Diagnostics())
		}
	}
}

func TestSwaggerNonOAuthSecurityRequirementsHaveNoRoles(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"paths":{},"securityDefinitions":{
			"ApiKey":{"type":"apiKey","name":"key","in":"header"}
		},"security":[{"ApiKey":["admin"]}]
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.security.roles.not-allowed" &&
			diagnostic.InstanceLocation == "/security/0/ApiKey" {
			return
		}
	}
	t.Fatalf("missing Swagger non-OAuth role diagnostic: %#v", report.Diagnostics())
}

func TestOpenAPI31AllowsNonOAuthRoleRequirements(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{},"components":{"securitySchemes":{
			"ApiKey":{"type":"apiKey","name":"key","in":"header"}
		}},"security":[{"ApiKey":["admin"]}]
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() {
		t.Fatalf("OpenAPI 3.1 role labels were rejected: %#v", report.Diagnostics())
	}
}

func TestDocumentValidatesSecurityAcrossOperationContainers(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2",
		"info":{"title":"API","version":"1"},
		"paths":{},
		"webhooks":{"event":{
			"post":{"security":[{"Missing":[]}],"responses":{"204":{"description":"ok"}}}
		}},
		"components":{"callbacks":{"Event":{
			"{$request.body#/url}":{
				"post":{"security":[{"Missing":[]}],"responses":{"204":{"description":"ok"}}}
			}
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.security.scheme.unknown" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("unknown security scheme diagnostics = %d: %#v", count, report.Diagnostics())
	}
}

func TestDocumentResolvesReferencedSecuritySchemes(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{},
		"components":{"securitySchemes":{
			"OAuth":{"type":"oauth2","flows":{"clientCredentials":{
				"tokenUrl":"https://auth.example.test/token",
				"scopes":{"read":"Read access"}
			}}},
			"Alias":{"$ref":"#/components/securitySchemes/OAuth"},
			"External":{"$ref":"other.yaml#/components/securitySchemes/Auth"}
		}},
		"security":[{"Alias":["read","missing"]},{"External":[]}]
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	unknownScope := 0
	for _, diagnostic := range report.Diagnostics() {
		switch diagnostic.Code {
		case "openapi.security.scheme.unknown":
			t.Fatalf("declared referenced scheme was reported unknown: %#v", diagnostic)
		case "openapi.security.oauth-scope.unknown":
			unknownScope++
		}
	}
	if unknownScope != 1 {
		t.Fatalf("unknown scopes = %d, want 1: %#v", unknownScope, report.Diagnostics())
	}
}

func TestDocumentValidatesExternalSecuritySchemeSemantics(t *testing.T) {
	t.Parallel()

	external := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"Schemes","version":"1"},
		"paths":{},"components":{"securitySchemes":{
			"OAuth":{"type":"oauth2","flows":{"clientCredentials":{
				"tokenUrl":"https://auth.example.test/token",
				"scopes":{"read":"Read access"}
			}}},
			"Key":{"type":"apiKey","name":"key","in":"header"},
			"Invalid":{"type":"not-a-scheme"}
		}}
	}`)
	resolver := reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		if identifier != "https://api.example.test/schemes.json" {
			return reference.Resource{}, fmt.Errorf(
				"unexpected resolved identifier %q", identifier,
			)
		}
		return reference.Resource{RetrievalURI: identifier, Root: external.Raw()}, nil
	})

	for _, test := range []struct {
		name     string
		document string
		want     map[string]int
	}{
		{
			name: "component references",
			document: `{
				"openapi":"3.0.4","info":{"title":"API","version":"1"},
				"paths":{},"components":{"securitySchemes":{
					"OAuth":{"$ref":"schemes.json#/components/securitySchemes/OAuth"},
					"Key":{"$ref":"schemes.json#/components/securitySchemes/Key"}
				}},"security":[{"OAuth":["read","missing"]},{"Key":["admin"]}]
			}`,
			want: map[string]int{
				"openapi.security.oauth-scope.unknown": 1,
				"openapi.security.roles.not-allowed":   1,
			},
		},
		{
			name: "OpenAPI 3.2 URI references",
			document: `{
				"openapi":"3.2.0","info":{"title":"API","version":"1"},
				"paths":{},"security":[
					{"schemes.json#/components/securitySchemes/OAuth":["missing"]},
					{"schemes.json#/components/securitySchemes/Invalid":[]}
				]
			}`,
			want: map[string]int{
				"openapi.security.oauth-scope.unknown": 1,
				"openapi.security.scheme-uri.invalid":  1,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			options := validate.DefaultOptions()
			options.ReferenceResourceURI = "https://api.example.test/openapi.json"
			options.ReferenceResolver = resolver
			report, err := validate.DocumentWithOptions(
				context.Background(), mustDocument(t, test.document), options,
			)
			if err != nil {
				t.Fatal(err)
			}
			actual := make(map[string]int)
			for _, diagnostic := range report.Diagnostics() {
				if _, tracked := test.want[diagnostic.Code]; tracked {
					actual[diagnostic.Code]++
				}
			}
			for code, count := range test.want {
				if actual[code] != count {
					t.Errorf("%s diagnostics = %d, want %d: %#v",
						code, actual[code], count, report.Diagnostics())
				}
			}
		})
	}
}

func TestDocumentValidatesSecuritySchemeURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		document string
		want     map[string]validate.Severity
	}{
		{
			name: "Swagger OAuth URLs are recommendations",
			document: `{
				"swagger":"2.0","info":{"title":"API","version":"1"},
				"paths":{},"securityDefinitions":{
					"OAuth":{"type":"oauth2","flow":"accessCode",
						"authorizationUrl":"bad URL","tokenUrl":"bad URL",
						"scopes":{}}
				}
			}`,
			want: map[string]validate.Severity{
				"/securityDefinitions/OAuth/authorizationUrl": validate.SeverityWarning,
				"/securityDefinitions/OAuth/tokenUrl":         validate.SeverityWarning,
			},
		},
		{
			name: "OpenAPI security URLs are requirements",
			document: `{
				"openapi":"3.2.0","info":{"title":"API","version":"1"},
				"paths":{},"components":{"securitySchemes":{
					"OpenID":{"type":"openIdConnect","openIdConnectUrl":"bad URL"},
					"OAuth":{"type":"oauth2","flows":{
						"authorizationCode":{"authorizationUrl":"bad URL",
							"tokenUrl":"bad URL","refreshUrl":"bad URL","scopes":{}},
						"deviceAuthorization":{"deviceAuthorizationUrl":"bad URL",
							"tokenUrl":"bad URL","scopes":{}}
					}},
					"Relative":{"type":"oauth2","flows":{
						"authorizationCode":{"authorizationUrl":"/authorize",
							"tokenUrl":"/token","refreshUrl":"/refresh","scopes":{}}
					}}
				}}
			}`,
			want: map[string]validate.Severity{
				"/components/securitySchemes/OpenID/openIdConnectUrl":                                validate.SeverityError,
				"/components/securitySchemes/OAuth/flows/authorizationCode/authorizationUrl":         validate.SeverityError,
				"/components/securitySchemes/OAuth/flows/authorizationCode/tokenUrl":                 validate.SeverityError,
				"/components/securitySchemes/OAuth/flows/authorizationCode/refreshUrl":               validate.SeverityError,
				"/components/securitySchemes/OAuth/flows/deviceAuthorization/deviceAuthorizationUrl": validate.SeverityError,
				"/components/securitySchemes/OAuth/flows/deviceAuthorization/tokenUrl":               validate.SeverityError,
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, test.document)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range report.Diagnostics() {
				severity, exists := test.want[diagnostic.InstanceLocation]
				if diagnostic.Code != "openapi.security.url.invalid" {
					continue
				}
				if !exists {
					t.Errorf("unexpected invalid URL diagnostic: %#v", diagnostic)
					continue
				}
				if diagnostic.Severity != severity {
					t.Errorf("%s severity = %s, want %s", diagnostic.InstanceLocation, diagnostic.Severity, severity)
				}
				delete(test.want, diagnostic.InstanceLocation)
			}
			for pointer := range test.want {
				t.Errorf("missing invalid URL diagnostic at %s: %#v", pointer, report.Diagnostics())
			}
		})
	}
}

func TestDocumentRecommendsRegisteredHTTPAuthenticationSchemes(t *testing.T) {
	t.Parallel()

	versions := []string{"3.0.3", "3.0.4", "3.1.0", "3.1.1", "3.1.2", "3.2.0"}
	for _, version := range versions {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{},"components":{"securitySchemes":{
					"Registered":{"type":"http","scheme":"bEaReR"},
					"Custom":{"type":"http","scheme":"custom"}
				}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}

			var found []validate.Diagnostic
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.security.http-scheme.unregistered" {
					found = append(found, diagnostic)
				}
			}
			if len(found) != 1 {
				t.Fatalf("unregistered authentication-scheme diagnostics = %#v", found)
			}
			if found[0].InstanceLocation !=
				"/components/securitySchemes/Custom/scheme" ||
				found[0].Severity != validate.SeverityWarning {
				t.Fatalf("unregistered authentication-scheme diagnostic = %#v", found[0])
			}
		})
	}
}

func TestOpenAPI32SecurityRequirementNamesPreferComponentsAndValidateURIs(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{},"components":{"securitySchemes":{
			"auth.example":{
				"type":"apiKey","name":"key","in":"header"
			},"ApiKey":{"type":"apiKey","name":"key","in":"header"}
		},"schemas":{"Pet":{"type":"object"}}},
		"security":[
			{"auth.example":[]},
			{"#/components/securitySchemes/ApiKey":[]},
			{"external.yaml#/scheme":[]},
			{"#/components/schemas/Pet":[]},
			{"bad URI":[]},
			{"":[]},
			{"#/missing":[]},
			{"#":[]}
		]
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/security/3/#~1components~1schemas~1Pet": false,
		"/security/4/bad URI":                     false,
		"/security/5/":                            false,
		"/security/6/#~1missing":                  false,
		"/security/7/#":                           false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.security.scheme-uri.invalid" {
			continue
		}
		if _, exists := want[diagnostic.InstanceLocation]; !exists {
			t.Errorf("valid component or URI rejected: %#v", diagnostic)
			continue
		}
		want[diagnostic.InstanceLocation] = true
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing invalid security URI at %s: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestOpenAPI32DiscouragesURILikeSecurityComponentNames(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{},"components":{"securitySchemes":{
			"https://auth.example/scheme":{
				"type":"apiKey","name":"key","in":"header"
			},
			"ApiKey":{"type":"apiKey","name":"key","in":"header"}
		}},
		"security":[{"https://auth.example/scheme":[]}]
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.component.name.invalid" {
			t.Fatalf("OpenAPI 3.2 security scheme URI name rejected: %#v", diagnostic)
		}
		if diagnostic.Code == "openapi.security.component-name.uri-like" &&
			diagnostic.InstanceLocation ==
				"/components/securitySchemes/https:~1~1auth.example~1scheme" &&
			diagnostic.Severity == validate.SeverityWarning {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing URI-like component warning: %#v", report.Diagnostics())
	}
}
