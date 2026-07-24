package validate_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestDocumentBoundsReferenceValidation(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{},"components":{"schemas":{
			"One":{"$ref":"#/components/schemas/Target"},
			"Two":{"$ref":"#/components/schemas/Target"},
			"Target":{"type":"string"}
		}}
	}`)
	options := validate.DefaultOptions()
	options.MaxReferences = 1
	if _, err := validate.DocumentWithOptions(
		context.Background(), document, options,
	); !errors.Is(err, reference.ErrLimitExceeded) {
		t.Fatalf("reference limit error = %v", err)
	}
	options = validate.DefaultOptions()
	options.ReferenceResourceURI = "https://api.example.test/openapi.json#bad"
	if _, err := validate.DocumentWithOptions(
		context.Background(), document, options,
	); err == nil {
		t.Fatal("fragment-bearing resource URI was accepted")
	}
}

func TestOpenAPI32RejectsInvalidExternalDocumentRoots(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{},"components":{"schemas":{"Invalid":{
			"$ref":"https://schemas.example.test/invalid.json"
		}}}
	}`)
	root, err := jsonvalue.Array([]jsonvalue.Value{jsonvalue.Boolean(true)})
	if err != nil {
		t.Fatal(err)
	}
	options := validate.DefaultOptions()
	options.ReferenceResolver = reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		return reference.Resource{RetrievalURI: identifier, Root: root}, nil
	})
	report, err := validate.DocumentWithOptions(
		context.Background(), document, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.reference.document-root.invalid" {
			return
		}
	}
	t.Fatalf("missing external document-root diagnostic: %#v",
		report.Diagnostics())
}

func TestOpenAPI32AcceptsBooleanSchemaDocumentRoots(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{},"components":{"schemas":{"Allowed":{
			"$ref":"https://schemas.example.test/boolean.json"
		}}}
	}`)
	options := validate.DefaultOptions()
	options.ReferenceResolver = reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		return reference.Resource{
			RetrievalURI: identifier,
			Root:         jsonvalue.Boolean(true),
		}, nil
	})
	report, err := validate.DocumentWithOptions(
		context.Background(), document, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.reference.document-root.invalid" {
			t.Fatalf("boolean Schema Object rejected: %#v", diagnostic)
		}
	}
}

func TestDocumentValidatesInternalReferenceTargets(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{"responses":{"200":{
			"$ref":"#/components/responses/Missing"}}}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.reference.target.missing" &&
			diagnostic.InstanceLocation ==
				"/paths/~1pets/get/responses/200/$ref" &&
			diagnostic.Source == validate.SourceReference {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics = %#v", report.Diagnostics())
	}
}

func TestDocumentValidatesReferenceTargetKinds(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{"responses":{
			"200":{"$ref":"#/components/parameters/Wrong"},
			"201":{"$ref":"#/components/responses/Right"}
		}}}},
		"components":{
			"parameters":{"Wrong":{"name":"limit","in":"query",
				"schema":{"type":"integer"}}},
			"responses":{"Right":{"description":"OK"}},
			"schemas":{"FromParameter":{
				"$ref":"#/components/parameters/Wrong/schema"}}
		}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.reference.target.type" &&
			diagnostic.InstanceLocation ==
				"/paths/~1pets/get/responses/200/$ref" {
			found = true
		}
		if diagnostic.Code == "openapi.reference.target.type" &&
			diagnostic.InstanceLocation ==
				"/paths/~1pets/get/responses/201/$ref" {
			t.Fatalf("compatible response reference rejected: %#v", diagnostic)
		}
		if diagnostic.Code == "openapi.reference.target.type" &&
			diagnostic.InstanceLocation ==
				"/components/schemas/FromParameter/$ref" {
			t.Fatalf("embedded schema reference rejected: %#v", diagnostic)
		}
	}
	if !found {
		t.Fatalf("diagnostics = %#v", report.Diagnostics())
	}
}

func TestOpenAPIPathItemReferencesRequireURIsAndPathItemTargets(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{
					"/wrong":{"$ref":"#/components/responses/Shared"},
					"/invalid":{"$ref":"%"},
					"/right":{"$ref":"#/paths/~1target"},
					"/target":{"get":{"responses":{"200":{"description":"ok"}}}}
				},
				"components":{"responses":{"Shared":{"description":"ok"}}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]bool{
				"/paths/~1wrong/$ref":   false,
				"/paths/~1invalid/$ref": false,
			}
			for _, diagnostic := range report.Diagnostics() {
				switch {
				case diagnostic.Code == "openapi.reference.target.type" &&
					diagnostic.InstanceLocation == "/paths/~1wrong/$ref":
					want[diagnostic.InstanceLocation] = true
				case diagnostic.Code == "openapi.reference.invalid" &&
					diagnostic.InstanceLocation == "/paths/~1invalid/$ref":
					want[diagnostic.InstanceLocation] = true
				}
			}
			for pointer, found := range want {
				if !found {
					t.Errorf("missing Path Item reference diagnostic at %s: %#v",
						pointer, report.Diagnostics())
				}
			}
		})
	}
}

func TestSwaggerPathItemReferencesRequirePathItemTargets(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"paths":{
			"/wrong":{"$ref":"#/responses/Shared"},
			"/right":{"$ref":"#/paths/~1target"},
			"/target":{"get":{"responses":{"200":{"description":"ok"}}}}
		},"responses":{"Shared":{"description":"ok"}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.reference.target.type" &&
			diagnostic.InstanceLocation == "/paths/~1wrong/$ref" {
			return
		}
	}
	t.Fatalf("missing Swagger path target diagnostic: %#v", report.Diagnostics())
}

func TestSwaggerExternalPathItemReferencesRequirePathItemTargets(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"paths":{"/wrong":{"$ref":"other.json#/responses/Shared"}}
	}`)
	description, _ := jsonvalue.String("ok")
	response, _ := jsonvalue.Object([]jsonvalue.Member{{
		Name: "description", Value: description,
	}})
	responses, _ := jsonvalue.Object([]jsonvalue.Member{{
		Name: "Shared", Value: response,
	}})
	external, _ := jsonvalue.Object([]jsonvalue.Member{{
		Name: "responses", Value: responses,
	}})
	options := validate.DefaultOptions()
	options.ReferenceResourceURI = "https://api.example.test/openapi.json"
	options.ReferenceResolver = reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		return reference.Resource{RetrievalURI: identifier, Root: external}, nil
	})
	report, err := validate.DocumentWithOptions(
		context.Background(), document, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.reference.target.type" &&
			diagnostic.InstanceLocation == "/paths/~1wrong/$ref" {
			return
		}
	}
	t.Fatalf("missing external Swagger path target diagnostic: %#v", report.Diagnostics())
}

func TestDocumentIgnoresReferenceObjectExtraProperties(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"3.1.2", "3.2.0"} {
		document := mustDocument(t, `{
			"openapi":"`+version+`","info":{"title":"API","version":"1"},
			"paths":{},"components":{"responses":{
				"Target":{"description":"OK"},
				"Alias":{"$ref":"#/components/responses/Target","ignored":true}
			}}
		}`)
		report, err := validate.Document(context.Background(), document)
		if err != nil {
			t.Fatal(err)
		}
		if !report.Valid() {
			t.Fatalf("%s diagnostics = %#v", version, report.Diagnostics())
		}
	}
}

func TestDocumentValidatesOpenAPI32ReferencePositionKinds(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0",
		"info":{"title":"API","version":"1"},
		"paths":{"/subscribe":{"post":{
			"callbacks":{"event":{"{$request.body#/url}":{
				"$ref":"#/components/responses/Wrong"}}},
			"responses":{"200":{"description":"OK","content":{
				"application/json":{"$ref":"#/components/responses/Wrong"}
			}}}
		}}},
		"components":{"responses":{"Wrong":{"description":"wrong type"}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/paths/~1subscribe/post/callbacks/event/{$request.body#~1url}/$ref":   false,
		"/paths/~1subscribe/post/responses/200/content/application~1json/$ref": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.reference.target.type" {
			if _, exists := want[diagnostic.InstanceLocation]; exists {
				want[diagnostic.InstanceLocation] = true
			}
		}
	}
	for pointer, found := range want {
		if !found {
			t.Fatalf("missing target-kind diagnostic at %s: %#v", pointer,
				report.Diagnostics())
		}
	}
}

func TestDocumentDistinguishesReferenceCycles(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},"paths":{},
		"components":{
			"responses":{
				"One":{"$ref":"#/components/responses/Two"},
				"Two":{"$ref":"#/components/responses/One"}
			},
			"schemas":{
				"Node":{"type":"object","properties":{"next":{
					"$ref":"#/components/schemas/Node"}}}
			}
		}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	foundInvalid := false
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.reference.cycle.invalid" {
			continue
		}
		if diagnostic.InstanceLocation ==
			"/components/schemas/Node/properties/next/$ref" {
			t.Fatalf("recursive schema was rejected: %#v", diagnostic)
		}
		foundInvalid = true
	}
	if !foundInvalid {
		t.Fatalf("diagnostics = %#v", report.Diagnostics())
	}
}

func TestDocumentResolvesExternalReferencesOnlyWhenAuthorized(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{"responses":{"200":{
			"$ref":"responses.json#/Missing"}}}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.reference.target.missing" {
			t.Fatalf("disabled external reference was resolved: %#v", diagnostic)
		}
	}
	external, _ := jsonvalue.Object([]jsonvalue.Member{})
	options := validate.DefaultOptions()
	options.ReferenceResourceURI = "https://api.example.test/openapi.json"
	options.ReferenceResolver = reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		return reference.Resource{
			RetrievalURI: identifier,
			Root:         external,
		}, nil
	})
	report, err = validate.DocumentWithOptions(
		context.Background(), document, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.reference.target.missing" {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics = %#v", report.Diagnostics())
	}
}

func TestDocumentDeduplicatesExternalReferenceResources(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{"/one":{"get":{"responses":{"200":{
			"$ref":"responses.json#/Result"}}}},
			"/two":{"get":{"responses":{"200":{
				"$ref":"responses.json#/Result"}}}}}
	}`)
	description, _ := jsonvalue.String("OK")
	response, _ := jsonvalue.Object([]jsonvalue.Member{{
		Name: "description", Value: description,
	}})
	external, _ := jsonvalue.Object([]jsonvalue.Member{{
		Name: "Result", Value: response,
	}})
	calls := 0
	options := validate.DefaultOptions()
	options.ReferenceResourceURI = "https://api.example.test/openapi.json"
	options.ReferenceResolver = reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		calls++
		return reference.Resource{RetrievalURI: identifier, Root: external}, nil
	})
	report, err := validate.DocumentWithOptions(
		context.Background(), document, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() {
		t.Fatalf("diagnostics = %#v", report.Diagnostics())
	}
	if calls != 1 {
		t.Fatalf("resolver calls = %d, want 1", calls)
	}
}

func TestDocumentIgnoresReferenceShapedDataValues(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/receive":{"post":{"operationId":"receive",
			"responses":{"204":{"description":"done"}}}}},
		"components":{"examples":{"Payload":{"value":{
			"headers":{"trace":{"$ref":false}}
		}},"DataPayload":{"dataValue":{"$ref":"data-value.json#/literal"}}
		},"schemas":{"Payload":{"type":"object",
			"default":{"$ref":"default.json#/literal"},
			"examples":[{"$ref":"example.json#/literal"}]
		},"Actual":{"$ref":"#/components/schemas/Missing"}},
		"links":{"Payload":{"operationId":"receive",
			"requestBody":{"$ref":"body.json#/literal"}
		}}},
		"x-payload":{"responses":{"200":{"$ref":"other.json#/literal"}}}
	}`)
	calls := 0
	options := validate.DefaultOptions()
	options.ReferenceResourceURI = "https://api.example.test/openapi.json"
	options.ReferenceResolver = reference.ResolverFunc(func(
		context.Context,
		string,
	) (reference.Resource, error) {
		calls++
		return reference.Resource{}, errors.New("unexpected resolver call")
	})
	report, err := validate.DocumentWithOptions(
		context.Background(), document, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	foundMissing := false
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.reference.target.missing" &&
			diagnostic.InstanceLocation == "/components/schemas/Actual/$ref" {
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Fatalf("diagnostics = %#v", report.Diagnostics())
	}
	if calls != 0 {
		t.Fatalf("resolver calls = %d", calls)
	}
}

func TestDocumentIgnoresAdditionalReferenceObjectProperties(t *testing.T) {
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
				"paths":{},"components":{"responses":{
					"Target":{"description":"ok"},
					"Alias":{"$ref":"#/components/responses/Target",
						"unexpected":true,"x-extra":{"kept":false}}
				}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range report.Diagnostics() {
				if strings.HasPrefix(
					diagnostic.InstanceLocation,
					"/components/responses/Alias",
				) {
					t.Fatalf("additional Reference Object property was not ignored: %#v",
						diagnostic)
				}
			}
		})
	}
}

func TestDocumentIgnoresSwaggerReferenceShapedExamples(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},"paths":{},
		"responses":{"Payload":{"description":"OK","examples":{
			"application/json":{"$ref":"data.json#/literal"}
		}}}
	}`)
	calls := 0
	options := validate.DefaultOptions()
	options.ReferenceResourceURI = "https://api.example.test/swagger.json"
	options.ReferenceResolver = reference.ResolverFunc(func(
		context.Context,
		string,
	) (reference.Resource, error) {
		calls++
		return reference.Resource{}, errors.New("unexpected resolver call")
	})
	report, err := validate.DocumentWithOptions(
		context.Background(), document, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() || calls != 0 {
		t.Fatalf("calls = %d, diagnostics = %#v", calls, report.Diagnostics())
	}
}

func TestDocumentResolvesReferencesInXPrefixedMapEntries(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{},"components":{"schemas":{
			"x-Alias":{"$ref":"#/components/schemas/Missing"},
			"Container":{"type":"object","properties":{
				"x-child":{"$ref":"#/components/schemas/Missing"},
				"example":{"$ref":"#/components/schemas/Missing"},
				"default":{"$ref":"#/components/schemas/Missing"}
			}}
		}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{
		"/components/schemas/x-Alias/$ref":                      false,
		"/components/schemas/Container/properties/x-child/$ref": false,
		"/components/schemas/Container/properties/example/$ref": false,
		"/components/schemas/Container/properties/default/$ref": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.reference.target.missing" {
			if _, exists := found[diagnostic.InstanceLocation]; exists {
				found[diagnostic.InstanceLocation] = true
			}
		}
	}
	for pointer, seen := range found {
		if !seen {
			t.Fatalf("missing diagnostic at %s: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestDocumentUsesOpenAPI32SelfAsReferenceBase(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","$self":"https://api.example.test/spec/openapi.json",
		"info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{"responses":{"200":{
			"$ref":"responses.json#/Result"}}}}}
	}`)
	description, _ := jsonvalue.String("OK")
	response, _ := jsonvalue.Object([]jsonvalue.Member{{
		Name: "description", Value: description,
	}})
	external, _ := jsonvalue.Object([]jsonvalue.Member{{
		Name: "Result", Value: response,
	}})
	requested := ""
	options := validate.DefaultOptions()
	options.ReferenceResolver = reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		requested = identifier
		return reference.Resource{RetrievalURI: identifier, Root: external}, nil
	})
	report, err := validate.DocumentWithOptions(
		context.Background(), document, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() {
		t.Fatalf("diagnostics = %#v", report.Diagnostics())
	}
	if requested != "https://api.example.test/spec/responses.json" {
		t.Fatalf("requested URI = %q", requested)
	}
}
