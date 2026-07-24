package validate_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/reference"
	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestDocumentValidatesLinksAndCallbackExpressions(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2",
		"info":{"title":"API","version":"1"},
		"paths":{"/pets":{
			"get":{
				"operationId":"listPets",
				"callbacks":{"bad":{
					"{$request.bad}":{
						"post":{"responses":{"200":{"description":"ok"}}}
					}
				}},
				"responses":{"200":{
					"description":"ok",
					"links":{
						"conflict":{
							"operationId":"missingOperation",
							"operationRef":"#/paths/~1pets/get",
							"parameters":{"id":"$request.bad"}
						},
						"missing":{}
					}
				}}
			}
		}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"openapi.callback.expression.invalid": false,
		"openapi.link.operation.conflict":     false,
		"openapi.link.operation.missing":      false,
		"openapi.link.operation-id.unknown":   false,
		"openapi.link.expression.invalid":     false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if _, exists := want[diagnostic.Code]; exists {
			want[diagnostic.Code] = true
		}
	}
	for code, seen := range want {
		if !seen {
			t.Errorf("missing link diagnostic %q: %#v", code, report.Diagnostics())
		}
	}
}

func TestDocumentAcceptsLinksToCallbackOperations(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2",
		"info":{"title":"API","version":"1"},
		"paths":{ "/subscribe":{
			"post":{
				"callbacks":{"events":{
					"{$request.body#/callbackUrl}":{
						"post":{
							"operationId":"receiveEvent",
							"responses":{"204":{"description":"received"}}
						}
					}
				}},
				"responses":{"200":{
					"description":"subscribed",
					"links":{"callback":{
						"operationId":"receiveEvent",
						"parameters":{"id":"$request.body#/id"}
					}}
				}}
			}
		}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.link.operation-id.unknown" ||
			diagnostic.Code == "openapi.callback.expression.invalid" ||
			diagnostic.Code == "openapi.link.expression.invalid" {
			t.Fatalf("unexpected link diagnostic: %#v", diagnostic)
		}
	}
}

func TestDocumentResolvesLinkOperationIDsAcrossOpenAPIVersions(t *testing.T) {
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
				"paths":{"/pets":{"get":{
					"operationId":"readPet",
					"responses":{"200":{"description":"ok","links":{
						"exact":{"operationId":"readPet"},
						"wrongCase":{"operationId":"ReadPet"}
					}}}
				}}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			unknown := 0
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.link.operation-id.unknown" {
					unknown++
				}
			}
			if unknown != 1 {
				t.Fatalf("unknown operationId diagnostics = %d", unknown)
			}
		})
	}
}

func TestDocumentRejectsOperationRefToNonOperation(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2",
		"info":{"title":"API","version":"1"},
		"paths":{},
		"components":{
			"schemas":{"Pet":{"type":"object"}},
			"links":{"invalid":{"operationRef":"#/components/schemas/Pet"}}
		}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.link.operation-ref.invalid" {
			return
		}
	}
	t.Fatalf("missing invalid operationRef diagnostic: %#v", report.Diagnostics())
}

func TestDocumentRejectsMalformedAndAmbiguousLinkTargets(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0",
		"info":{"title":"API","version":"1"},
		"paths":{
			"/cats":{"get":{
				"operationId":"showPet",
				"responses":{"200":{"description":"ok"}}
			}},
			"/dogs":{"get":{
				"operationId":"showPet",
				"responses":{"200":{
					"description":"ok",
					"links":{
						"ambiguous":{"operationId":"showPet"},
						"malformed":{"operationRef":"https://[::1"},
						"empty":{"operationRef":""}
					}
				}}
			}}
		}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		"openapi.link.operation-id.ambiguous": 1,
		"openapi.link.operation-ref.invalid":  2,
	}
	for _, diagnostic := range report.Diagnostics() {
		if _, exists := want[diagnostic.Code]; exists {
			want[diagnostic.Code]--
		}
	}
	for code, remaining := range want {
		if remaining != 0 {
			t.Errorf(
				"unexpected diagnostic count for %q (%d remaining): %#v",
				code,
				remaining,
				report.Diagnostics(),
			)
		}
	}
}

func TestLinkRequestParameterExpressionsRequireDeclarations(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		document := mustDocument(t, `{
			"openapi":"`+version+`","info":{"title":"API","version":"1"},
			"components":{"parameters":{"Query":{
				"name":"q","in":"query","schema":{"type":"string"}
			}}},
			"paths":{"/pets/{id}":{
				"parameters":[{"name":"id","in":"path","required":true,
					"schema":{"type":"string"}}],
				"get":{"operationId":"readPet","parameters":[
					{"$ref":"#/components/parameters/Query"},
					{"name":"X-Trace","in":"header","schema":{"type":"string"}}
				],"responses":{"200":{"description":"ok","links":{"next":{
					"operationId":"readPet","parameters":{
						"id":"$request.path.id",
						"q":"$request.query.q",
						"trace":"$request.header.x-trace",
						"missingPath":"$request.path.missing",
						"missingHeader":"$request.header.missing"
					}
				}}}}}
			}}
		}`)
		report, err := validate.Document(context.Background(), document)
		if err != nil {
			t.Fatal(err)
		}
		missing := 0
		for _, diagnostic := range report.Diagnostics() {
			if diagnostic.Code == "openapi.link.expression.parameter-undeclared" {
				missing++
			}
		}
		if missing != 2 {
			t.Errorf("version %s undeclared diagnostics = %d: %#v",
				version, missing, report.Diagnostics())
		}
	}
}

func TestLinkParametersPreferQualifiedTargetNames(t *testing.T) {
	t.Parallel()

	versions := []string{"3.0.4", "3.1.1", "3.1.2", "3.2.0"}
	for _, version := range versions {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{
					"/source":{"get":{"responses":{"200":{
						"description":"ok","links":{"next":{
							"operationId":"target","parameters":{
								"id":"ambiguous","path.id":"path",
								"query.path.id":"query"
							}
						}}
					}}}},
					"/target/{id}":{"get":{"operationId":"target","parameters":[
						{"name":"id","in":"path","required":true,
							"schema":{"type":"string"}},
						{"name":"id","in":"query","schema":{"type":"string"}},
						{"name":"path.id","in":"query","schema":{"type":"string"}}
					],"responses":{"200":{"description":"ok"}}}}
				}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}

			var found []validate.Diagnostic
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.link.parameter.ambiguous" {
					found = append(found, diagnostic)
				}
			}
			if len(found) != 1 || found[0].InstanceLocation !=
				"/paths/~1source/get/responses/200/links/next/parameters/id" {
				t.Fatalf("ambiguous target parameter diagnostics = %#v", found)
			}
		})
	}
}

func TestReferencedLinksUseParentOperationParameterDeclarations(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"components":{
			"links":{"Missing":{"operationId":"readPet","parameters":{
				"id":"$request.path.missing"
			}}},
			"responses":{"Shared":{"description":"ok","links":{
				"next":{"$ref":"#/components/links/Missing"}
			}}}
		},
		"paths":{"/pets":{"get":{"operationId":"readPet","responses":{
			"200":{"$ref":"#/components/responses/Shared"}
		}}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.link.expression.parameter-undeclared" &&
			diagnostic.InstanceLocation == "/paths/~1pets/get/responses/200/$ref" {
			return
		}
	}
	t.Fatalf("missing referenced-link declaration diagnostic: %#v",
		report.Diagnostics())
}

func TestExternalResponseLinksRetainTheirResourceContext(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{"operationId":"readPet","responses":{
			"200":{"$ref":"responses.json#/Shared"}
		}}}}
	}`)
	external := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"External","version":"1"},
		"paths":{},
		"Shared":{"description":"ok","links":{"next":{"$ref":"#/Missing"}}},
		"Missing":{"operationId":"readPet","parameters":{
			"id":"$request.query.missing"
		}}
	}`).Raw()
	options := validate.DefaultOptions()
	options.ReferenceResourceURI = "https://api.example.test/openapi.json"
	options.ReferenceResolver = reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		if identifier != "https://api.example.test/responses.json" {
			t.Fatalf("identifier = %q", identifier)
		}
		return reference.Resource{RetrievalURI: identifier, Root: external}, nil
	})
	report, err := validate.DocumentWithOptions(
		context.Background(),
		document,
		options,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.link.expression.parameter-undeclared" &&
			diagnostic.InstanceLocation == "/paths/~1pets/get/responses/200/$ref" {
			return
		}
	}
	t.Fatalf("missing external-link declaration diagnostic: %#v",
		report.Diagnostics())
}

func TestExternalPathItemLinksValidateUseSiteParameters(t *testing.T) {
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
				"paths":{"/pets/{id}":{"$ref":"path-items.json#/Pets"}}
			}`)
			external := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"Paths","version":"1"},
				"paths":{},"Pets":{
					"parameters":[{"name":"id","in":"path","required":true,
						"schema":{"type":"string"}}],
					"get":{"operationId":"readPet","responses":{"200":{
						"description":"ok","links":{"next":{
							"operationId":"readPet",
							"parameters":{"id":"$request.query.missing"}
						}}
					}}}
				}
			}`).Raw()
			options := validate.DefaultOptions()
			options.ReferenceResourceURI =
				"https://api.example.test/openapi.json"
			options.ReferenceResolver = reference.ResolverFunc(func(
				_ context.Context,
				identifier string,
			) (reference.Resource, error) {
				return reference.Resource{
					RetrievalURI: identifier,
					Root:         external,
				}, nil
			})
			report, err := validate.DocumentWithOptions(
				context.Background(), document, options,
			)
			if err != nil {
				t.Fatal(err)
			}
			foundUndeclared := false
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.link.operation-id.unknown" ||
					diagnostic.Code == "openapi.link.operation-id.ambiguous" {
					t.Fatalf("external operationId was not unique: %#v", diagnostic)
				}
				if diagnostic.Code ==
					"openapi.link.expression.parameter-undeclared" &&
					diagnostic.InstanceLocation ==
						"/paths/~1pets~1{id}/get/responses/200/links/next/"+
							"parameters/id" {
					foundUndeclared = true
				}
			}
			if !foundUndeclared {
				t.Fatalf("missing external path link diagnostic: %#v",
					report.Diagnostics())
			}
		})
	}
}

func TestRelativeOperationRefsResolveAgainstTheDocumentBase(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		document := mustDocument(t, `{
			"openapi":"`+version+`","info":{"title":"API","version":"1"},
			"paths":{"/source":{"get":{"responses":{"200":{
				"description":"ok","links":{"next":{
					"operationRef":"operations.json#/paths/~1pets/get"
				}}
			}}}}}
		}`)
		external := mustDocument(t, `{
			"openapi":"`+version+`","info":{"title":"Operations","version":"1"},
			"paths":{"/pets":{"get":{"responses":{
				"200":{"description":"ok"}
			}}}}
		}`).Raw()
		options := validate.DefaultOptions()
		options.ReferenceResourceURI = "https://api.example.test/root.json"
		options.ReferenceResolver = reference.ResolverFunc(func(
			_ context.Context,
			identifier string,
		) (reference.Resource, error) {
			if identifier != "https://api.example.test/operations.json" {
				t.Fatalf("identifier = %q", identifier)
			}
			return reference.Resource{
				RetrievalURI: identifier,
				Root:         external,
			}, nil
		})
		report, err := validate.DocumentWithOptions(
			context.Background(), document, options,
		)
		if err != nil {
			t.Fatal(err)
		}
		for _, diagnostic := range report.Diagnostics() {
			if diagnostic.Code == "openapi.link.operation-ref.invalid" ||
				diagnostic.Code == "openapi.reference.unresolved" ||
				diagnostic.Code == "openapi.reference.target.type" {
				t.Errorf("version %s rejected relative operationRef: %#v",
					version, diagnostic)
			}
		}
	}
}
