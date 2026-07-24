package validate_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/reference"
	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestDocumentValidatesPathTemplatesAndParameters(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2",
		"info":{"title":"API","version":"1"},
		"paths":{
			"/pets/{id}":{
				"parameters":[
					{"name":"unused","in":"path","required":true,"schema":{"type":"string"}}
				],
				"get":{
					"operationId":"shared",
					"parameters":[
						{"name":"id","in":"path","required":false,"schema":{"type":"string"}},
						{"name":"id","in":"path","required":true,"schema":{"type":"integer"}}
					],
					"responses":{"200":{"description":"ok"}}
				}
			},
			"/pets/{name}":{
				"get":{
					"operationId":"shared",
					"responses":{"200":{"description":"ok"}}
				}
			},
			"/broken/{id":{
				"get":{"responses":{"200":{"description":"ok"}}}
			}
		}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"openapi.path.template.ambiguous":     false,
		"openapi.path.template.invalid":       false,
		"openapi.path.parameter.duplicate":    false,
		"openapi.path.parameter.missing":      false,
		"openapi.path.parameter.not-required": false,
		"openapi.path.parameter.unused":       false,
		"openapi.operation-id.duplicate":      false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if _, exists := want[diagnostic.Code]; exists {
			want[diagnostic.Code] = true
			if diagnostic.SpecificationVersion != "3.1.2" {
				t.Fatalf("diagnostic version = %q", diagnostic.SpecificationVersion)
			}
			if diagnostic.InstanceLocation == "" {
				t.Fatalf("diagnostic %q has no instance location", diagnostic.Code)
			}
		}
	}
	for code, seen := range want {
		if !seen {
			t.Errorf("missing semantic diagnostic %q: %#v", code, report.Diagnostics())
		}
	}
}

func TestDocumentRequiresOperationIDsUniqueAcrossWebhooks(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2",
		"info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{
			"operationId":"receive",
			"responses":{"200":{"description":"ok"}}
		}}},
		"webhooks":{"event":{"post":{
			"operationId":"receive",
			"responses":{"204":{"description":"ok"}}
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.operation-id.duplicate" &&
			diagnostic.InstanceLocation == "/webhooks/event/post/operationId" {
			return
		}
	}
	t.Fatalf("missing cross-container operationId diagnostic: %#v", report.Diagnostics())
}

func TestDocumentRequiresOperationIDsUniqueAcrossOpenAPIVersions(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`",
				"info":{"title":"API","version":"1"},
				"paths":{
					"/first":{"get":{"operationId":"readPet",
						"responses":{"200":{"description":"ok"}}}},
					"/second":{"get":{"operationId":"readPet",
						"responses":{"200":{"description":"ok"}}}},
					"/case-sensitive":{"get":{"operationId":"ReadPet",
						"responses":{"200":{"description":"ok"}}}}
				}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			duplicates := 0
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.operation-id.duplicate" {
					duplicates++
				}
			}
			if duplicates != 1 {
				t.Fatalf("duplicate operationId diagnostics = %d", duplicates)
			}
		})
	}
}

func TestDocumentRequiresSwaggerOperationIDsUnique(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"paths":{
			"/first":{"get":{"operationId":"readPet",
				"responses":{"200":{"description":"ok"}}}},
			"/second":{"post":{"operationId":"readPet",
				"responses":{"200":{"description":"ok"}}}},
			"/case-sensitive":{"get":{"operationId":"ReadPet",
				"responses":{"200":{"description":"ok"}}}}
		}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	duplicates := 0
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.operation-id.duplicate" {
			duplicates++
		}
	}
	if duplicates != 1 {
		t.Fatalf("duplicate operationId diagnostics = %d: %#v",
			duplicates, report.Diagnostics())
	}
}

func TestDocumentRequiresOperationIDsUniqueAcrossExternalPathItems(t *testing.T) {
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
				"paths":{
					"/local":{"get":{"operationId":"readPet",
						"responses":{"200":{"description":"ok"}}}},
					"/external":{"$ref":"path-items.json#/Shared"}
				}
			}`)
			external := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"Paths","version":"1"},
				"paths":{},"Shared":{"get":{"operationId":"readPet",
					"responses":{"200":{"description":"ok"}}}}
			}`).Raw()
			options := validate.DefaultOptions()
			options.ReferenceResourceURI =
				"https://api.example.test/openapi.json"
			options.ReferenceResolver = reference.ResolverFunc(func(
				_ context.Context,
				identifier string,
			) (reference.Resource, error) {
				if identifier != "https://api.example.test/path-items.json" {
					return reference.Resource{}, fmt.Errorf(
						"unexpected resolved identifier %q", identifier,
					)
				}
				return reference.Resource{
					Root: external,
				}, nil
			})
			report, err := validate.DocumentWithOptions(
				context.Background(), document, options,
			)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.operation-id.duplicate" &&
					diagnostic.InstanceLocation ==
						"/paths/~1external/get/operationId" {
					return
				}
			}
			t.Fatalf("missing external duplicate: %#v", report.Diagnostics())
		})
	}
}

func TestDocumentCollectsOperationsFromExternalCallbacks(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{
			"/local":{"get":{"operationId":"receivePet",
				"responses":{"200":{"description":"ok"}}}},
			"/source":{"post":{"callbacks":{
				"Hook":{"$ref":"callbacks.json#/Hook"},
				"Again":{"$ref":"callbacks.json#/Hook"},
				"Loop":{"$ref":"callbacks.json#/Loop"},
				"Bad":{"$ref":"callbacks.json#/Scalar"},
				"Internal":{"$ref":"#/components/callbacks/Internal"},
				"Malformed":1
			},"responses":{"202":{"description":"accepted"}}}}
		},
		"components":{"callbacks":{"Internal":{
			"{$request.body#/internal}":{"post":{"operationId":"internalHook",
				"responses":{"204":{"description":"ok"}}}}
		}}}
	}`)
	external := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"Callbacks","version":"1"},
		"paths":{},
		"Hook":{"{$request.body#/url}":{
			"$ref":"path-items.json#/Shared"
		}},
		"Loop":{"$ref":"#/Loop"},
		"Scalar":"not a callback"
	}`).Raw()
	pathItems := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"Paths","version":"1"},
		"paths":{},"Shared":{"post":{"operationId":"receivePet",
			"responses":{"204":{"description":"ok"}}}}
	}`).Raw()
	options := validate.DefaultOptions()
	options.ReferenceResourceURI = "https://api.example.test/openapi.json"
	options.ReferenceResolver = reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		switch identifier {
		case "https://api.example.test/callbacks.json":
			return reference.Resource{
				CanonicalURI: identifier,
				Root:         external,
			}, nil
		case "https://api.example.test/path-items.json":
			return reference.Resource{Root: pathItems}, nil
		default:
			return reference.Resource{}, fmt.Errorf(
				"unexpected resolved identifier %q", identifier,
			)
		}
	})
	report, err := validate.DocumentWithOptions(
		context.Background(), document, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.operation-id.duplicate" &&
			diagnostic.InstanceLocation ==
				"/paths/~1source/post/callbacks/Hook/"+
					"{$request.body#~1url}/post/operationId" {
			return
		}
	}
	t.Fatalf("missing external callback duplicate: %#v", report.Diagnostics())
}

func TestDocumentRecommendsPortableOperationIDs(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`",
				"info":{"title":"API","version":"1"},
				"paths":{
					"/portable":{"get":{"operationId":"read_pet2",
						"responses":{"200":{"description":"ok"}}}},
					"/nonportable":{"get":{"operationId":"read pet",
						"responses":{"200":{"description":"ok"}}}}
				}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			found := 0
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code != "openapi.operation-id.nonportable" {
					continue
				}
				found++
				if diagnostic.InstanceLocation !=
					"/paths/~1nonportable/get/operationId" ||
					diagnostic.Severity != validate.SeverityWarning ||
					diagnostic.SpecificationSection != "operation-object" {
					t.Fatalf("diagnostic = %#v", diagnostic)
				}
			}
			if found != 1 {
				t.Fatalf("nonportable diagnostics = %d: %#v", found, report.Diagnostics())
			}
		})
	}
}

func TestOpenAPI32RejectsFixedMethodsInAdditionalOperations(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"additionalOperations":{
			"POST":{"responses":{"200":{"description":"ok"}}},
			"PURGE":{"responses":{"200":{"description":"ok"}}},
			"post":{"responses":{"200":{"description":"lowercase custom"}}}
		}}},
		"components":{"pathItems":{"Shared":{"additionalOperations":{
			"QUERY":{"responses":{"200":{"description":"ok"}}}
		}}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/paths/~1pets/additionalOperations/POST":                 false,
		"/components/pathItems/Shared/additionalOperations/QUERY": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.path.additional-operation.fixed" {
			continue
		}
		if _, exists := want[diagnostic.InstanceLocation]; !exists {
			t.Fatalf("unexpected fixed-method diagnostic: %#v", diagnostic)
		}
		want[diagnostic.InstanceLocation] = true
		if diagnostic.SpecificationSection != "path-item-object" {
			t.Fatalf("diagnostic metadata = %#v", diagnostic)
		}
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing diagnostic at %q: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestAdditionalOperationRuleDoesNotApplyBeforeOpenAPI32(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},"paths":{},
		"components":{"pathItems":{"Shared":{"additionalOperations":{
			"POST":{"responses":{"200":{"description":"ok"}}}
		}}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.path.additional-operation.fixed" {
			t.Fatalf("OpenAPI 3.2 rule applied to 3.1: %#v", diagnostic)
		}
	}
}

func TestOpenAPI32RejectsRepeatedPathTemplateExpressions(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/pairs/{id}/{id}":{"get":{
			"parameters":[{"name":"id","in":"path","required":true,
				"schema":{"type":"string"}}],
			"responses":{"200":{"description":"ok"}}
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.path.template.duplicate" &&
			diagnostic.InstanceLocation == "/paths/~1pairs~1{id}~1{id}" &&
			diagnostic.SpecificationSection == "paths" {
			return
		}
	}
	t.Fatalf("missing repeated-template diagnostic: %#v", report.Diagnostics())
}

func TestRepeatedPathTemplateRuleDoesNotApplyBeforeOpenAPI32(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{"/pairs/{id}/{id}":{"get":{
			"parameters":[{"name":"id","in":"path","required":true,
				"schema":{"type":"string"}}],
			"responses":{"200":{"description":"ok"}}
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.path.template.duplicate" {
			t.Fatalf("OpenAPI 3.2 rule applied to 3.1: %#v", diagnostic)
		}
	}
}

func TestDocumentResolvesInternalPathParameterReferences(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"components":{"parameters":{"ID":{
			"name":"id","in":"path","required":true,
			"schema":{"type":"string"}
		}}},
		"paths":{"/pets/{id}":{"get":{
			"parameters":[
				{"$ref":"#/components/parameters/ID"},
				{"$ref":"#/components/parameters/ID"}
			],
			"responses":{"200":{"description":"ok"}}
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := false
	for _, diagnostic := range report.Diagnostics() {
		switch diagnostic.Code {
		case "openapi.path.parameter.duplicate":
			duplicate = true
		case "openapi.path.parameter.missing":
			t.Fatalf("resolved path parameter was reported missing: %#v", diagnostic)
		}
	}
	if !duplicate {
		t.Fatalf("missing referenced path duplicate: %#v", report.Diagnostics())
	}
}

func TestDocumentRejectsDuplicateExternalParameterReferences(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{"/pets/{id}":{
			"parameters":[
				{"$ref":"parameters.json#/ID"},
				{"$ref":"parameters.json#/ID"}
			],
			"get":{"parameters":[
				{"$ref":"parameters.json#/Query"},
				{"$ref":"parameters.json#/Query"}
			],"responses":{"200":{"description":"ok"}}}
		}}
	}`)
	external := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"Parameters","version":"1"},
		"paths":{},
		"ID":{"name":"id","in":"path","required":true,"schema":{"type":"string"}},
		"Query":{"name":"filter","in":"query","schema":{"type":"string"}}
	}`).Raw()
	options := validate.DefaultOptions()
	options.ReferenceResourceURI = "https://api.example.test/openapi.json"
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
		"/paths/~1pets~1{id}/parameters/1":     false,
		"/paths/~1pets~1{id}/get/parameters/1": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.path.parameter.duplicate" &&
			diagnostic.Code != "openapi.parameter.duplicate" {
			continue
		}
		if _, expected := want[diagnostic.InstanceLocation]; expected {
			want[diagnostic.InstanceLocation] = true
		}
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing external duplicate at %s: %#v",
				pointer, report.Diagnostics())
		}
	}
}

func TestDocumentValidatesExternalPathParameterSemantics(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{"/pets/{id}":{"get":{
			"parameters":[{"$ref":"parameters.json#/ID"}],
			"responses":{"200":{"description":"ok"}}
		}}}
	}`)
	external := mustDocument(t, `{
		"openapi":"3.1.2","paths":{},"ID":{
		"name":"wrong","in":"path","required":false,
		"schema":{"type":"string"}
	}}`).Raw()
	calls := 0
	options := validate.DefaultOptions()
	options.ReferenceResourceURI = "https://api.example.test/openapi.json"
	options.ReferenceResolver = reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		calls++
		if identifier != "https://api.example.test/parameters.json" {
			t.Fatalf("identifier = %q", identifier)
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
		"openapi.path.parameter.missing":      false,
		"openapi.path.parameter.not-required": false,
		"openapi.path.parameter.unused":       false,
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
	if calls != 1 {
		t.Fatalf("resolver calls = %d, want 1", calls)
	}
}

func TestDocumentValidatesReferencedPathItemsAtUseSites(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.3", "3.0.4", "3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{
					"/pets/{id}":{"$ref":"path-items.json#/WithID"},
					"/owners/{id}":{"$ref":"path-items.json#/WithoutID"}
				}
			}`)
			pathItems := mustDocument(t, `{
				"openapi":"`+version+`","paths":{},
				"WithID":{"parameters":[{"$ref":"parameters.json#/ID"}],
					"get":{"responses":{"200":{"description":"ok"}}}},
				"WithoutID":{"get":{"responses":{"200":{"description":"ok"}}}}
			}`).Raw()
			parameters := mustDocument(t, `{
				"openapi":"`+version+`","paths":{},
				"ID":{"name":"id","in":"path","required":true,
					"schema":{"type":"string"}}
			}`).Raw()
			calls := make(map[string]int)
			options := validate.DefaultOptions()
			options.ReferenceResourceURI = "https://api.example.test/openapi.json"
			options.ReferenceResolver = reference.ResolverFunc(func(
				_ context.Context,
				identifier string,
			) (reference.Resource, error) {
				calls[identifier]++
				switch identifier {
				case "https://api.example.test/path-items.json":
					return reference.Resource{RetrievalURI: identifier, Root: pathItems}, nil
				case "https://api.example.test/parameters.json":
					return reference.Resource{RetrievalURI: identifier, Root: parameters}, nil
				default:
					t.Fatalf("identifier = %q", identifier)
					return reference.Resource{}, nil
				}
			})
			report, err := validate.DocumentWithOptions(
				context.Background(), document, options,
			)
			if err != nil {
				t.Fatal(err)
			}
			foundMissing := false
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code != "openapi.path.parameter.missing" {
					continue
				}
				if diagnostic.InstanceLocation == "/paths/~1pets~1{id}/get" {
					t.Fatalf("resolved path parameter was reported missing: %#v", diagnostic)
				}
				if diagnostic.InstanceLocation == "/paths/~1owners~1{id}/get" {
					foundMissing = true
				}
			}
			if !foundMissing {
				t.Fatalf("missing use-site diagnostic: %#v", report.Diagnostics())
			}
			for identifier, count := range calls {
				if count != 1 {
					t.Fatalf("resolver calls for %q = %d, want 1", identifier, count)
				}
			}
			if len(calls) != 2 {
				t.Fatalf("resolved resources = %#v", calls)
			}
		})
	}
}

func TestDocumentSkipsUnresolvedSwaggerPathItemsDuringSemanticChecks(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"$ref":"path-items.json#/Pets"}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.parameter.file.consumes" {
			t.Fatalf("unresolved path produced a file diagnostic: %#v", diagnostic)
		}
	}
}

func TestDocumentMatchesPathParameterNamesAcrossVersions(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"2.0", "3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			marker := `"openapi":"` + version + `"`
			representation := `"schema":{"type":"string"}`
			if version == "2.0" {
				marker = `"swagger":"2.0"`
				representation = `"type":"string"`
			}
			document := mustDocument(t, `{`+marker+`,
				"info":{"title":"API","version":"1"},
				"paths":{"/pets/{id}":{"get":{
					"parameters":[{
						"name":"ID","in":"path","required":true,
						`+representation+`
					}],"responses":{"200":{"description":"ok"}}
				}}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]bool{
				"openapi.path.parameter.missing": false,
				"openapi.path.parameter.unused":  false,
			}
			for _, diagnostic := range report.Diagnostics() {
				if _, exists := want[diagnostic.Code]; exists {
					want[diagnostic.Code] = true
				}
			}
			for code, found := range want {
				if !found {
					t.Errorf("%s missing %s: %#v", version, code, report.Diagnostics())
				}
			}
		})
	}
}

func TestDocumentRejectsAmbiguousTemplatesAcrossPatchLines(t *testing.T) {
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
				"paths":{"/pets/{id}":{},"/pets/{name}":{}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.path.template.ambiguous" {
					return
				}
			}
			t.Fatalf("missing ambiguous-template diagnostic: %#v", report.Diagnostics())
		})
	}
}

func TestDocumentRequiresPathKeysToStartWithSlashAcrossVersions(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"2.0", "3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			marker := `"openapi":"` + version + `"`
			if version == "2.0" {
				marker = `"swagger":"2.0"`
			}
			document := mustDocument(t, `{`+marker+`,
				"info":{"title":"API","version":"1"},
				"paths":{"pets":{},"x-internal":{"enabled":true}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.InstanceLocation == "/paths/x-internal" {
					t.Fatalf("%s rejected a Paths extension: %#v", version, diagnostic)
				}
				if diagnostic.Code == "openapi.path.key.invalid" &&
					diagnostic.InstanceLocation == "/paths/pets" {
					found = true
					if diagnostic.SpecificationSection != "paths" {
						t.Fatalf("%s diagnostic metadata = %#v", version, diagnostic)
					}
				}
				if diagnostic.InstanceLocation == "/paths/pets" &&
					(diagnostic.Code == "openapi.document.additionalProperties" ||
						diagnostic.Code == "openapi.document.unevaluatedProperties") {
					t.Fatalf("%s duplicated the semantic finding: %#v", version, diagnostic)
				}
			}
			if !found {
				t.Fatalf("%s diagnostics = %#v", version, report.Diagnostics())
			}
		})
	}
}

func TestDocumentFailFastUsesSameFirstRule(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{"openapi":"3.2.0"}`)
	full, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	options := validate.DefaultOptions()
	options.FailFast = true
	fast, err := validate.DocumentWithOptions(context.Background(), document, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(fast.Diagnostics()) != 1 {
		t.Fatalf("fail-fast diagnostics = %#v", fast.Diagnostics())
	}
	if fast.Diagnostics()[0] != full.Diagnostics()[0] {
		t.Fatalf("fail-fast changed first rule\nfast: %#v\nfull: %#v", fast.Diagnostics()[0], full.Diagnostics()[0])
	}
}

func TestDocumentAcceptsEmptyPathsAndPathItems(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			for _, paths := range []string{`{}`, `{"/hidden":{}}`} {
				document := mustDocument(t, `{
					"openapi":"`+version+`","info":{"title":"API","version":"1"},
					"paths":`+paths+`
				}`)
				report, err := validate.Document(context.Background(), document)
				if err != nil {
					t.Fatal(err)
				}
				if !report.Valid() {
					t.Fatalf("empty path surface rejected: %#v", report.Diagnostics())
				}
			}
		})
	}
}
