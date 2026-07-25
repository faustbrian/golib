package convert

import (
	"context"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

func TestConvertOpenAPI32DocumentFieldsTo31(t *testing.T) {
	t.Parallel()

	source := swaggerDocument(t, `{
		"openapi":"3.2.0","$self":"https://api.example.test/openapi.json",
		"info":{"title":"API","version":"1"},
		"servers":[{"url":"https://api.example.test","name":"production"}],
		"paths":{"/pets":{
			"query":{"responses":{"200":{"description":"query"}}},
			"additionalOperations":{"PURGE":{"responses":{"204":{
				"description":"purged"}}}},
			"get":{"responses":{"200":{"summary":"Success",
				"description":"OK"},"201":{"summary":"Created"},"202":{}}}
		}},
		"components":{"securitySchemes":{"OAuth":{
			"type":"oauth2","deprecated":true,
			"oauth2MetadataUrl":"https://auth.example.test/metadata",
			"flows":{"deviceAuthorization":{
				"deviceAuthorizationUrl":"https://auth.example.test/device",
				"tokenUrl":"https://auth.example.test/token","scopes":{}},
				"password":{"deviceAuthorizationUrl":"https://auth.example.test/device",
					"tokenUrl":"https://auth.example.test/token","scopes":{}}
			}
		}}},
		"tags":[{"name":"pets","summary":"Pets","kind":"nav",
			"parent":"root"}]
	}`)
	converted, diagnostics, err := convertOAS32Document(
		context.Background(), source.Raw(), 1_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := converted.Lookup("$self"); exists {
		t.Fatal("$self was retained")
	}
	if textValue(t, memberAt(t, converted, "jsonSchemaDialect")) !=
		"https://spec.openapis.org/oas/3.2/dialect/2025-09-17" {
		t.Fatalf("schema dialect = %#v", converted)
	}
	server := memberAt(t, converted, "servers")
	servers, _ := server.Elements()
	if _, exists := servers[0].Lookup("name"); exists {
		t.Fatal("server name was retained")
	}
	path := memberAt(t, converted, "paths", "/pets")
	if _, exists := path.Lookup("query"); exists {
		t.Fatal("query operation was retained")
	}
	if _, exists := path.Lookup("additionalOperations"); exists {
		t.Fatal("additional operations were retained")
	}
	response := memberAt(t, path, "get", "responses", "200")
	if _, exists := response.Lookup("summary"); exists {
		t.Fatal("response summary was retained")
	}
	if textValue(t, memberAt(t, path, "get", "responses", "201", "description")) !=
		"Created" {
		t.Fatal("response summary was not converted to a description")
	}
	if textValue(t, memberAt(t, path, "get", "responses", "202", "description")) !=
		"" {
		t.Fatal("required response description was not added")
	}
	tag := memberAt(t, converted, "tags")
	tags, _ := tag.Elements()
	for _, name := range []string{"summary", "kind", "parent"} {
		if _, exists := tags[0].Lookup(name); exists {
			t.Fatalf("tag %s was retained", name)
		}
	}
	security := memberAt(t, converted, "components", "securitySchemes", "OAuth")
	for _, name := range []string{"deprecated", "oauth2MetadataUrl"} {
		if _, exists := security.Lookup(name); exists {
			t.Fatalf("security field %s was retained", name)
		}
	}
	if _, exists := memberAt(t, security, "flows").Lookup("deviceAuthorization"); exists {
		t.Fatal("device authorization flow was retained")
	}
	if _, exists := memberAt(t, security, "flows", "password").Lookup(
		"deviceAuthorizationUrl",
	); exists {
		t.Fatal("device authorization URL was retained")
	}
	want := map[string]DiagnosticKind{
		"/$self":                                                                  Loss,
		"/servers/0/name":                                                         Loss,
		"/paths/~1pets/query":                                                     Loss,
		"/paths/~1pets/additionalOperations":                                      Loss,
		"/paths/~1pets/get/responses/200/summary":                                 Loss,
		"/paths/~1pets/get/responses/202/description":                             ManualAction,
		"/components/securitySchemes/OAuth/deprecated":                            Loss,
		"/components/securitySchemes/OAuth/oauth2MetadataUrl":                     Loss,
		"/components/securitySchemes/OAuth/flows/deviceAuthorization":             Loss,
		"/components/securitySchemes/OAuth/flows/password/deviceAuthorizationUrl": Loss,
		"/tags/0/summary":                                                         Loss,
		"/tags/0/kind":                                                            Loss,
		"/tags/0/parent":                                                          Loss,
	}
	if len(diagnostics) != len(want) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if kind, exists := want[diagnostic.Pointer]; !exists || kind != diagnostic.Kind {
			t.Fatalf("diagnostic = %#v", diagnostic)
		}
	}
	assertValidOpenAPI31(t, converted)
}

func TestConvertOpenAPI32MediaTypesTo31(t *testing.T) {
	t.Parallel()

	source := swaggerDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"post":{
			"parameters":[{"name":"filter","in":"query","content":{
				"application/json":{"schema":{"type":"string"}}}}],
			"requestBody":{"content":{"multipart/form-data":{
				"$ref":"#/components/mediaTypes/Shared"}}},
			"responses":{"200":{"description":"OK","content":{
				"application/json":{"schema":{"type":"string"}}}}}
		}}},
		"components":{"mediaTypes":{"Shared":{
			"schema":{"type":"object","properties":{"field":{"type":"string"}}},
			"itemSchema":{"type":"string"},
			"examples":{"one":{"dataValue":{"id":1}}},
			"prefixEncoding":[{}],"itemEncoding":{},
			"encoding":{"field":{"contentType":"text/plain",
				"headers":{"X-Meta":{"content":{"application/json":{
					"schema":{"type":"string"}}}}},
				"encoding":{},"prefixEncoding":[],"itemEncoding":{}}}
		}}}
	}`)
	converted, diagnostics, err := convertOAS32Document(
		context.Background(), source.Raw(), 1_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := memberAt(t, converted, "components").Lookup("mediaTypes"); exists {
		t.Fatal("Media Type components were retained")
	}
	media := memberAt(
		t, converted, "paths", "/pets", "post", "requestBody", "content",
		"multipart/form-data",
	)
	if textValue(t, memberAt(t, media, "schema", "type")) != "object" {
		t.Fatalf("inlined media type = %#v", media)
	}
	if number, _ := memberAt(t, media, "examples", "one", "value", "id").NumberText(); number != "1" {
		t.Fatalf("converted example = %#v", media)
	}
	for _, name := range []string{"itemSchema", "prefixEncoding", "itemEncoding"} {
		if _, exists := media.Lookup(name); exists {
			t.Fatalf("Media Type field %s was retained", name)
		}
	}
	encoding := memberAt(t, media, "encoding", "field")
	if textValue(t, memberAt(t, encoding, "contentType")) != "text/plain" {
		t.Fatalf("encoding = %#v", encoding)
	}
	if textValue(t, memberAt(
		t, encoding, "headers", "X-Meta", "content", "application/json",
		"schema", "type",
	)) != "string" {
		t.Fatalf("encoding headers = %#v", encoding)
	}
	for _, name := range []string{"encoding", "prefixEncoding", "itemEncoding"} {
		if _, exists := encoding.Lookup(name); exists {
			t.Fatalf("Encoding field %s was retained", name)
		}
	}
	parameters, _ := memberAt(
		t, converted, "paths", "/pets", "post", "parameters",
	).Elements()
	parameterType := memberAt(
		t, parameters[0], "content", "application/json", "schema", "type",
	)
	if textValue(t, parameterType) != "string" {
		t.Fatalf("parameter media type = %#v", parameters[0])
	}
	responseMedia := memberAt(
		t, converted, "paths", "/pets", "post", "responses", "200",
		"content", "application/json", "schema", "type",
	)
	if textValue(t, responseMedia) != "string" {
		t.Fatalf("response media type = %#v", responseMedia)
	}
	want := map[string]DiagnosticKind{
		"/components/mediaTypes":                                      Loss,
		"/components/mediaTypes/Shared/itemSchema":                    Loss,
		"/components/mediaTypes/Shared/prefixEncoding":                Loss,
		"/components/mediaTypes/Shared/itemEncoding":                  Loss,
		"/components/mediaTypes/Shared/encoding/field/encoding":       Loss,
		"/components/mediaTypes/Shared/encoding/field/prefixEncoding": Loss,
		"/components/mediaTypes/Shared/encoding/field/itemEncoding":   Loss,
	}
	if len(diagnostics) != len(want) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if kind, exists := want[diagnostic.Pointer]; !exists || kind != diagnostic.Kind {
			t.Fatalf("diagnostic = %#v", diagnostic)
		}
	}
	assertValidOpenAPI31(t, converted)
}

func TestConvertOpenAPI32MediaReferenceFailuresAndExamples(t *testing.T) {
	t.Parallel()

	source := swaggerDocument(t, `{
		"openapi":"3.2.0","jsonSchemaDialect":"https://example.test/dialect",
		"info":{"title":"API","version":"1"},
		"paths":{"/pets":{"post":{
			"requestBody":{"content":{
				"application/json":{"$ref":"#/components/mediaTypes/A",
					"summary":"ignored"},
				"text/plain":{"$ref":"media.json#/Text"}
			}},
			"responses":{"204":{"description":"OK"}}
		}}},
		"components":{
			"mediaTypes":{
				"A":{"$ref":"#/components/mediaTypes/B"},
				"B":{"$ref":"#/components/mediaTypes/A"}
			},
			"examples":{
				"Serialized":{"serializedValue":"id=1"},
				"Collision":{"value":"old","dataValue":"new"}
			}
		}
	}`)
	converted, diagnostics, err := convertOAS32Document(
		context.Background(), source.Raw(), 1_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	if textValue(t, memberAt(t, converted, "jsonSchemaDialect")) !=
		"https://example.test/dialect" {
		t.Fatalf("schema dialect = %#v", converted)
	}
	content := memberAt(t, converted, "paths", "/pets", "post", "requestBody", "content")
	for _, mediaType := range []string{"application/json", "text/plain"} {
		media := memberAt(t, content, mediaType)
		if members, _ := media.Members(); len(members) != 0 {
			t.Fatalf("failed media reference = %#v", media)
		}
	}
	serialized := memberAt(t, converted, "components", "examples", "Serialized")
	if members, _ := serialized.Members(); len(members) != 0 {
		t.Fatalf("serialized example = %#v", serialized)
	}
	collision := memberAt(t, converted, "components", "examples", "Collision")
	if textValue(t, memberAt(t, collision, "value")) != "old" {
		t.Fatalf("example collision = %#v", collision)
	}
	want := map[string]DiagnosticKind{
		"/components/mediaTypes":                                           Loss,
		"/components/mediaTypes/B/$ref":                                    Loss,
		"/paths/~1pets/post/requestBody/content/application~1json/summary": Loss,
		"/paths/~1pets/post/requestBody/content/text~1plain/$ref":          Loss,
		"/components/examples/Serialized/serializedValue":                  Loss,
		"/components/examples/Collision/dataValue":                         Loss,
	}
	if len(diagnostics) != len(want) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if kind, exists := want[diagnostic.Pointer]; !exists || kind != diagnostic.Kind {
			t.Fatalf("diagnostic = %#v", diagnostic)
		}
	}
}

func TestConvertOpenAPI32DocumentWorkIsBoundedAndCancelable(t *testing.T) {
	t.Parallel()

	source := swaggerDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"servers":[{"url":"/"}],"paths":{}
	}`)
	_, _, err := convertOAS32Document(context.Background(), source.Raw(), 1)
	if err != ErrLimitExceeded {
		t.Fatalf("document limit error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = convertOAS32Document(ctx, source.Raw(), 100)
	if err != context.Canceled {
		t.Fatalf("canceled conversion error = %v", err)
	}
}

func TestConvertOpenAPI32NestedComponentsAndCallbacks(t *testing.T) {
	t.Parallel()

	source := swaggerDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/pets":{
			"servers":[{"url":"/path","name":"path"}],
			"parameters":[{"$ref":"#/components/parameters/Filter"}],
			"post":{
				"servers":[{"url":"/operation","name":"operation"}],
				"callbacks":{"done":{"{$request.body#/url}":{
					"query":{"responses":{"200":{"description":"query"}}},
					"post":{"responses":{"200":{"summary":"Done",
						"description":"done"}}}
				}}},
				"responses":{"201":{"$ref":"#/components/responses/Result",
					"summary":"Override"},"204":{"description":"Accepted"}}
			}
		}},
		"components":{
			"parameters":{"Filter":{"name":"filter","in":"query",
				"examples":{"one":{"dataValue":"all"}},"schema":{"type":"string"}}},
			"headers":{"Result":{"content":{"application/json":{
				"schema":{"type":"string"}}}}},
			"requestBodies":{"Payload":{"content":{"application/json":{
				"schema":{"type":"object"}}}}},
			"responses":{"Result":{"summary":"Result","description":"OK"}},
			"callbacks":{"Shared":{"{$request.body#/url}":{
				"post":{"responses":{"200":{"summary":"Shared",
					"description":"OK"}}}}}},
			"pathItems":{"Shared":{
				"servers":[{"url":"/shared","name":"shared"}],
				"query":{"responses":{"200":{"description":"query"}}},
				"get":{"responses":{"200":{"description":"OK"}}}
			}}
		}
	}`)
	converted, diagnostics, err := convertOAS32Document(
		context.Background(), source.Raw(), 1_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	path := memberAt(t, converted, "paths", "/pets")
	pathServers, _ := memberAt(t, path, "servers").Elements()
	if _, exists := pathServers[0].Lookup("name"); exists {
		t.Fatal("path server name was retained")
	}
	operationServers, _ := memberAt(t, path, "post", "servers").Elements()
	if _, exists := operationServers[0].Lookup("name"); exists {
		t.Fatal("operation server name was retained")
	}
	callbackPath := memberAt(
		t, path, "post", "callbacks", "done", "{$request.body#/url}",
	)
	if _, exists := callbackPath.Lookup("query"); exists {
		t.Fatal("callback query operation was retained")
	}
	callbackResponse := memberAt(t, callbackPath, "post", "responses", "200")
	if _, exists := callbackResponse.Lookup("summary"); exists {
		t.Fatal("callback response summary was retained")
	}
	if textValue(t, memberAt(t, path, "post", "responses", "201", "summary")) !=
		"Override" {
		t.Fatal("Reference Object summary was removed")
	}
	filter := memberAt(t, converted, "components", "parameters", "Filter")
	if textValue(t, memberAt(t, filter, "examples", "one", "value")) != "all" {
		t.Fatalf("component parameter = %#v", filter)
	}
	componentResponse := memberAt(t, converted, "components", "responses", "Result")
	if _, exists := componentResponse.Lookup("summary"); exists {
		t.Fatal("component response summary was retained")
	}
	sharedPath := memberAt(t, converted, "components", "pathItems", "Shared")
	if _, exists := sharedPath.Lookup("query"); exists {
		t.Fatal("component query operation was retained")
	}
	sharedServers, _ := memberAt(t, sharedPath, "servers").Elements()
	if _, exists := sharedServers[0].Lookup("name"); exists {
		t.Fatal("component server name was retained")
	}
	sharedResponse := memberAt(
		t, converted, "components", "callbacks", "Shared", "{$request.body#/url}", "post",
		"responses", "200",
	)
	if _, exists := sharedResponse.Lookup("summary"); exists {
		t.Fatal("component callback response summary was retained")
	}
	wantPointers := []string{
		"/paths/~1pets/servers/0/name",
		"/paths/~1pets/post/servers/0/name",
		"/paths/~1pets/post/callbacks/done/{$request.body#~1url}/query",
		"/paths/~1pets/post/callbacks/done/{$request.body#~1url}/post/responses/200/summary",
		"/components/responses/Result/summary",
		"/components/callbacks/Shared/{$request.body#~1url}/post/responses/200/summary",
		"/components/pathItems/Shared/servers/0/name",
		"/components/pathItems/Shared/query",
	}
	if len(diagnostics) != len(wantPointers) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	want := make(map[string]struct{}, len(wantPointers))
	for _, pointer := range wantPointers {
		want[pointer] = struct{}{}
	}
	for _, diagnostic := range diagnostics {
		if _, exists := want[diagnostic.Pointer]; !exists {
			t.Fatalf("diagnostic = %#v", diagnostic)
		}
	}
	assertValidOpenAPI31(t, converted)
}

func assertValidOpenAPI31(t *testing.T, value jsonvalue.Value) {
	t.Helper()
	target, _ := openapi.ParseVersion("3.1.2")
	members, _ := value.Members()
	converted, err := replaceVersion(members, target)
	if err != nil {
		t.Fatal(err)
	}
	assertValidConverted(t, converted)
}
