package diff_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/diff"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

func TestOperationsClassifiesAddedAndRemovedOperationSurface(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{
			"/removed":{"get":{"responses":{"200":{"description":"ok"}}}},
			"/shared":{
				"get":{"responses":{"200":{"description":"ok"}}},
				"post":{"responses":{"200":{"description":"ok"}}},
				"additionalOperations":{"COPY":{"responses":{"200":{"description":"ok"}}}}
			}
		},
		"webhooks":{"old":{"post":{"responses":{"200":{"description":"ok"}}}}}
	}`)
	right := diffDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"2"},
		"paths":{
			"/shared":{
				"get":{"responses":{"200":{"description":"ok"}}},
				"query":{"responses":{"200":{"description":"ok"}}},
				"additionalOperations":{"MOVE":{"responses":{"200":{"description":"ok"}}}}
			},
			"/added":{"put":{"responses":{"200":{"description":"ok"}}}}
		},
		"webhooks":{"new":{"post":{"responses":{"200":{"description":"ok"}}}}}
	}`)
	report, err := diff.Operations(context.Background(), left, right, diff.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		kind           diff.Kind
		classification diff.Classification
		pointer        string
	}{
		{kind: diff.PathRemoved, classification: diff.Breaking, pointer: "/paths/~1removed"},
		{kind: diff.OperationRemoved, classification: diff.Breaking, pointer: "/paths/~1shared/post"},
		{kind: diff.OperationRemoved, classification: diff.Breaking, pointer: "/paths/~1shared/additionalOperations/COPY"},
		{kind: diff.PathAdded, classification: diff.Additive, pointer: "/paths/~1added"},
		{kind: diff.OperationAdded, classification: diff.Additive, pointer: "/paths/~1shared/query"},
		{kind: diff.OperationAdded, classification: diff.Additive, pointer: "/paths/~1shared/additionalOperations/MOVE"},
		{kind: diff.WebhookRemoved, classification: diff.Breaking, pointer: "/webhooks/old"},
		{kind: diff.WebhookAdded, classification: diff.Additive, pointer: "/webhooks/new"},
	}
	changes := report.Changes()
	if len(changes) != len(want) {
		t.Fatalf("changes = %#v", changes)
	}
	for index, expected := range want {
		if changes[index].Kind() != expected.kind ||
			changes[index].Classification() != expected.classification ||
			changes[index].Pointer() != expected.pointer {
			t.Fatalf("change %d = %#v, want %#v", index, changes[index], expected)
		}
	}
	changes[0] = diff.Change{}
	if report.Changes()[0].Kind() == "" {
		t.Fatal("report exposed mutable change storage")
	}
}

func TestOperationsComparesOperationsInsideCommonWebhooks(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{},
		"webhooks":{"event":{
			"post":{},
			"additionalOperations":{"COPY":{}}
		}}
	}`)
	right := diffDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{},
		"webhooks":{"event":{
			"query":{},
			"additionalOperations":{"MOVE":{}}
		}}
	}`)
	report, err := diff.Operations(context.Background(), left, right, diff.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		kind    diff.Kind
		pointer string
	}{
		{kind: diff.OperationRemoved, pointer: "/webhooks/event/post"},
		{kind: diff.OperationRemoved, pointer: "/webhooks/event/additionalOperations/COPY"},
		{kind: diff.OperationAdded, pointer: "/webhooks/event/query"},
		{kind: diff.OperationAdded, pointer: "/webhooks/event/additionalOperations/MOVE"},
	}
	changes := report.Changes()
	if len(changes) != len(want) {
		t.Fatalf("changes = %#v", changes)
	}
	for index, expected := range want {
		if changes[index].Kind() != expected.kind || changes[index].Pointer() != expected.pointer {
			t.Fatalf("change %d = %#v, want %#v", index, changes[index], expected)
		}
	}
}

func TestOperationsComparesResolvedInternalReferenceSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		left    string
		right   string
		kind    diff.Kind
		pointer string
	}{
		{
			name: "parameter",
			left: `{"openapi":"3.1.2","paths":{"/pets":{"get":{
				"parameters":[{"$ref":"#/components/parameters/P"}]
			}}},"components":{"parameters":{"P":{
				"name":"value","in":"query","schema":{"type":"string"}
			}}}}`,
			right: `{"openapi":"3.1.2","paths":{"/pets":{"get":{
				"parameters":[{"$ref":"#/components/parameters/P"}]
			}}},"components":{"parameters":{"P":{
				"name":"value","in":"query","schema":{"type":"integer"}
			}}}}`,
			kind:    diff.ParameterSchemaChanged,
			pointer: "/paths/~1pets/get/parameters/0/schema",
		},
		{
			name: "request body",
			left: `{"openapi":"3.1.2","paths":{"/pets":{"post":{
				"requestBody":{"$ref":"#/components/requestBodies/B"}
			}}},"components":{"requestBodies":{"B":{"required":false}}}}`,
			right: `{"openapi":"3.1.2","paths":{"/pets":{"post":{
				"requestBody":{"$ref":"#/components/requestBodies/B"}
			}}},"components":{"requestBodies":{"B":{"required":true}}}}`,
			kind:    diff.RequestBodyRequired,
			pointer: "/paths/~1pets/post/requestBody/required",
		},
		{
			name: "response",
			left: `{"openapi":"3.1.2","paths":{"/pets":{"get":{"responses":{
				"200":{"$ref":"#/components/responses/R"}
			}}}},"components":{"responses":{"R":{"description":"ok","content":{
				"application/json":{"schema":false}
			}}}}}`,
			right: `{"openapi":"3.1.2","paths":{"/pets":{"get":{"responses":{
				"200":{"$ref":"#/components/responses/R"}
			}}}},"components":{"responses":{"R":{"description":"ok","content":{
				"application/json":{"schema":true}
			}}}}}`,
			kind:    diff.ResponseSchemaChanged,
			pointer: "/paths/~1pets/get/responses/200/content/application~1json/schema",
		},
		{
			name: "callback",
			left: `{"openapi":"3.1.2","paths":{"/pets":{"post":{"callbacks":{
				"again":{"$ref":"#/components/callbacks/C"}
			}}}},"components":{"callbacks":{"C":{"{$request.body#/url}":{
				"post":{},"delete":{}
			}}}}}`,
			right: `{"openapi":"3.1.2","paths":{"/pets":{"post":{"callbacks":{
				"again":{"$ref":"#/components/callbacks/C"}
			}}}},"components":{"callbacks":{"C":{"{$request.body#/url}":{
				"post":{}
			}}}}}`,
			kind:    diff.CallbackOperationRemoved,
			pointer: "/paths/~1pets/post/callbacks/again/{$request.body#~1url}/delete",
		},
		{
			name: "link",
			left: `{"openapi":"3.1.2","paths":{"/pets":{"get":{"responses":{
				"200":{"description":"ok","links":{"next":{
					"$ref":"#/components/links/L"
				}}}
			}}}},"components":{"links":{"L":{"operationId":"old"}}}}`,
			right: `{"openapi":"3.1.2","paths":{"/pets":{"get":{"responses":{
				"200":{"description":"ok","links":{"next":{
					"$ref":"#/components/links/L"
				}}}
			}}}},"components":{"links":{"L":{"operationId":"new"}}}}`,
			kind:    diff.LinkChanged,
			pointer: "/paths/~1pets/get/responses/200/links/next",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			report, err := diff.Operations(
				context.Background(), diffDocument(t, test.left),
				diffDocument(t, test.right), diff.DefaultOptions(),
			)
			if err != nil {
				t.Fatal(err)
			}
			changes := report.Changes()
			if len(changes) != 1 || changes[0].Kind() != test.kind ||
				changes[0].Pointer() != test.pointer {
				t.Fatalf("changes = %#v", changes)
			}
		})
	}
}

func TestOperationsComparesExtensionsWithCallerPolicy(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{
		"openapi":"3.2.0","x-root":{"value":1},
		"paths":{"/pets":{"x-path":"old","get":{"x-operation":"old"}}}
	}`)
	right := diffDocument(t, `{
		"openapi":"3.2.0","x-root":{"value":2},
		"paths":{"/pets":{"x-path":"new","get":{"x-operation":"new"}}}
	}`)
	for _, test := range []struct {
		name           string
		classification diff.Classification
	}{
		{name: "default", classification: diff.Conditional},
		{name: "caller policy", classification: diff.Breaking},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			options := diff.DefaultOptions()
			if test.name == "caller policy" {
				options.ExtensionClassification = diff.Breaking
			}
			report, err := diff.Operations(
				context.Background(), left, right, options,
			)
			if err != nil {
				t.Fatal(err)
			}
			changes := report.Changes()
			wantPointers := []string{
				"/x-root", "/paths/~1pets/x-path",
				"/paths/~1pets/get/x-operation",
			}
			if len(changes) != len(wantPointers) {
				t.Fatalf("changes = %#v", changes)
			}
			for index, pointer := range wantPointers {
				if changes[index].Kind() != diff.ExtensionChanged ||
					changes[index].Classification() != test.classification ||
					changes[index].Pointer() != pointer {
					t.Fatalf("change %d = %#v", index, changes[index])
				}
			}
		})
	}
}

func TestOperationsComparesNestedContractExtensions(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{
		"openapi":"3.1.2",
		"paths":{"/pets":{"post":{
			"parameters":[{"name":"p","in":"query","x-note":"left"}],
			"requestBody":{"x-note":"left","content":{"application/json":{
				"x-note":"left","schema":{"type":"string"}
			}}},
			"responses":{"200":{"description":"ok","x-note":"left",
				"headers":{"X-Result":{"schema":{"type":"string"},
					"x-note":"left"}}
			}}
		}}},
		"components":{"securitySchemes":{"Auth":{
			"type":"apiKey","name":"key","in":"header","x-note":"left"
		}}}
	}`)
	right := diffDocument(t, strings.ReplaceAll(
		mustRawDocument(t, left), `"left"`, `"right"`,
	))
	report, err := diff.Operations(
		context.Background(), left, right, diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"/components/securitySchemes/Auth/x-note",
		"/paths/~1pets/post/parameters/0/x-note",
		"/paths/~1pets/post/requestBody/x-note",
		"/paths/~1pets/post/requestBody/content/application~1json/x-note",
		"/paths/~1pets/post/responses/200/x-note",
		"/paths/~1pets/post/responses/200/headers/X-Result/x-note",
	}
	changes := report.Changes()
	if len(changes) != len(want) {
		t.Fatalf("changes = %#v", changes)
	}
	for index, pointer := range want {
		if changes[index].Kind() != diff.ExtensionChanged ||
			changes[index].Pointer() != pointer {
			t.Fatalf("change %d = %#v", index, changes[index])
		}
	}
}

func TestOperationsComparesExplicitExternalReferenceSemantics(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{"openapi":"3.1.2","paths":{
		"/pets":{"post":{"requestBody":{"$ref":"components.json#/B"}}},
		"/again":{"post":{"requestBody":{"$ref":"components.json#/B"}}}
	}}`)
	right := diffDocument(t, `{"openapi":"3.1.2","paths":{
		"/pets":{"post":{"requestBody":{"$ref":"components.json#/B"}}},
		"/again":{"post":{"requestBody":{"$ref":"components.json#/B"}}}
	}}`)
	leftExternal := diffDocument(t, `{
		"openapi":"3.1.2","B":{"required":false}
	}`)
	rightExternal := diffDocument(t, `{
		"openapi":"3.1.2","B":{"required":true}
	}`)
	options := diff.DefaultOptions()
	options.LeftResourceURI = "https://left.example.test/api.json"
	options.RightResourceURI = "https://right.example.test/api.json"
	leftCalls := 0
	options.LeftResolver = reference.ResolverFunc(func(
		_ context.Context, identifier string,
	) (reference.Resource, error) {
		if identifier != "https://left.example.test/components.json" {
			t.Fatalf("left identifier = %q", identifier)
		}
		leftCalls++
		return reference.Resource{
			RetrievalURI: identifier, Root: leftExternal.Raw(),
		}, nil
	})
	rightCalls := 0
	options.RightResolver = reference.ResolverFunc(func(
		_ context.Context, identifier string,
	) (reference.Resource, error) {
		if identifier != "https://right.example.test/components.json" {
			t.Fatalf("right identifier = %q", identifier)
		}
		rightCalls++
		return reference.Resource{
			RetrievalURI: identifier, Root: rightExternal.Raw(),
		}, nil
	})
	report, err := diff.Operations(context.Background(), left, right, options)
	if err != nil {
		t.Fatal(err)
	}
	changes := report.Changes()
	if len(changes) != 2 || changes[0].Kind() != diff.RequestBodyRequired ||
		changes[0].Pointer() != "/paths/~1pets/post/requestBody/required" ||
		changes[1].Kind() != diff.RequestBodyRequired ||
		changes[1].Pointer() != "/paths/~1again/post/requestBody/required" {
		t.Fatalf("changes = %#v", changes)
	}
	if leftCalls != 1 || rightCalls != 1 {
		t.Fatalf("resolver calls = %d, %d", leftCalls, rightCalls)
	}
}

func TestOperationsPropagatesExternalResolutionAndReferenceLimits(t *testing.T) {
	t.Parallel()

	external := diffDocument(t, `{"openapi":"3.1.2","paths":{"/pets":{"post":{
		"requestBody":{"$ref":"components.json#/B"}
	}}}}`)
	want := errors.New("external retrieval failed")
	options := diff.DefaultOptions()
	options.LeftResourceURI = "https://left.example.test/api.json"
	options.RightResourceURI = "https://right.example.test/api.json"
	options.LeftResolver = reference.ResolverFunc(func(
		context.Context, string,
	) (reference.Resource, error) {
		return reference.Resource{}, want
	})
	options.RightResolver = options.LeftResolver
	if _, err := diff.Operations(
		context.Background(), external, external, options,
	); !errors.Is(err, want) {
		t.Fatalf("resolver error = %v", err)
	}

	chain := diffDocument(t, `{
		"openapi":"3.1.2",
		"paths":{"/pets":{"post":{"requestBody":{
			"$ref":"#/components/requestBodies/A"
		}}}},
		"components":{"requestBodies":{
			"A":{"$ref":"#/components/requestBodies/B"},
			"B":{"required":true}
		}}
	}`)
	options = diff.DefaultOptions()
	options.MaxReferenceDepth = 1
	if _, err := diff.Operations(
		context.Background(), chain, chain, options,
	); !errors.Is(err, diff.ErrLimitExceeded) {
		t.Fatalf("reference limit error = %v", err)
	}
	options = diff.DefaultOptions()
	options.MaxResolvedNodes = 1
	if _, err := diff.Operations(
		context.Background(), chain, chain, options,
	); !errors.Is(err, diff.ErrLimitExceeded) {
		t.Fatalf("resolved node limit error = %v", err)
	}
}

func TestOperationsComparesResolvedPathItemSemantics(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{
		"openapi":"3.1.2",
		"paths":{"/pets":{"$ref":"#/components/pathItems/P"}},
		"components":{"pathItems":{"P":{"get":{}}}}
	}`)
	right := diffDocument(t, `{
		"openapi":"3.1.2",
		"paths":{"/pets":{"$ref":"#/components/pathItems/P"}},
		"components":{"pathItems":{"P":{"post":{}}}}
	}`)
	report, err := diff.Operations(
		context.Background(), left, right, diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	changes := report.Changes()
	if len(changes) != 2 || changes[0].Kind() != diff.OperationRemoved ||
		changes[0].Pointer() != "/paths/~1pets/get" ||
		changes[1].Kind() != diff.OperationAdded ||
		changes[1].Pointer() != "/paths/~1pets/post" {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestOperationsComparesResolvedCallbackPathItems(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{
		"openapi":"3.1.2",
		"paths":{"/pets":{"post":{"callbacks":{"again":{
			"{$request.body#/url}":{"$ref":"#/components/pathItems/P"}
		}}}}},
		"components":{"pathItems":{"P":{"get":{}}}}
	}`)
	right := diffDocument(t, `{
		"openapi":"3.1.2",
		"paths":{"/pets":{"post":{"callbacks":{"again":{
			"{$request.body#/url}":{"$ref":"#/components/pathItems/P"}
		}}}}},
		"components":{"pathItems":{"P":{"post":{}}}}
	}`)
	report, err := diff.Operations(
		context.Background(), left, right, diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	changes := report.Changes()
	prefix := "/paths/~1pets/post/callbacks/again/{$request.body#~1url}"
	if len(changes) != 2 ||
		changes[0].Kind() != diff.CallbackOperationRemoved ||
		changes[0].Pointer() != prefix+"/get" ||
		changes[1].Kind() != diff.CallbackOperationAdded ||
		changes[1].Pointer() != prefix+"/post" {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestOperationsComparesResolvedSchemaSemanticsByDirection(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{
		"openapi":"3.1.2",
		"paths":{"/pets":{"post":{
			"parameters":[{"name":"value","in":"query","schema":{
				"$ref":"#/components/schemas/P"
			}}],
			"requestBody":{"content":{"application/json":{"schema":{
				"$ref":"#/components/schemas/S"
			}}}},
			"responses":{"200":{"description":"ok","content":{
				"application/json":{"schema":{"$ref":"#/components/schemas/S"}}
			}}}
		}}},
		"components":{"schemas":{"P":{"type":"string"},"S":false}}
	}`)
	right := diffDocument(t, `{
		"openapi":"3.1.2",
		"paths":{"/pets":{"post":{
			"parameters":[{"name":"value","in":"query","schema":{
				"$ref":"#/components/schemas/P"
			}}],
			"requestBody":{"content":{"application/json":{"schema":{
				"$ref":"#/components/schemas/S"
			}}}},
			"responses":{"200":{"description":"ok","content":{
				"application/json":{"schema":{"$ref":"#/components/schemas/S"}}
			}}}
		}}},
		"components":{"schemas":{"P":{"type":"integer"},"S":true}}
	}`)
	report, err := diff.Operations(
		context.Background(), left, right, diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		kind           diff.Kind
		classification diff.Classification
		pointer        string
	}{
		{diff.ParameterSchemaChanged, diff.Unknown,
			"/paths/~1pets/post/parameters/0/schema"},
		{diff.RequestSchemaChanged, diff.Compatible,
			"/paths/~1pets/post/requestBody/content/application~1json/schema"},
		{diff.ResponseSchemaChanged, diff.Breaking,
			"/paths/~1pets/post/responses/200/content/application~1json/schema"},
	}
	changes := report.Changes()
	if len(changes) != len(want) {
		t.Fatalf("changes = %#v", changes)
	}
	for index, expected := range want {
		if changes[index].Kind() != expected.kind ||
			changes[index].Classification() != expected.classification ||
			changes[index].Pointer() != expected.pointer {
			t.Fatalf("change %d = %#v", index, changes[index])
		}
	}
}

func TestOperationsAppliesReferenceMetadataByDialect(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		version    string
		wantChange bool
	}{
		{name: "OpenAPI 3.0 ignores siblings", version: "3.0.4"},
		{name: "OpenAPI 3.1 applies siblings", version: "3.1.2", wantChange: true},
		{name: "OpenAPI 3.2 applies siblings", version: "3.2.0", wantChange: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			document := func(description string) openapi.Document {
				return diffDocument(t, fmt.Sprintf(`{
					"openapi":%q,
					"paths":{"/pets":{"get":{"responses":{"200":{
						"description":"ok","links":{"next":{
							"$ref":"#/components/links/L",
							"description":%q
						}}
					}}}}},
					"components":{"links":{"L":{"operationId":"next"}}}
				}`, test.version, description))
			}
			report, err := diff.Operations(
				context.Background(), document("old"), document("new"),
				diff.DefaultOptions(),
			)
			if err != nil {
				t.Fatal(err)
			}
			changes := report.Changes()
			if !test.wantChange && len(changes) != 0 {
				t.Fatalf("changes = %#v", changes)
			}
			if test.wantChange && (len(changes) != 1 ||
				changes[0].Kind() != diff.LinkChanged) {
				t.Fatalf("changes = %#v", changes)
			}
		})
	}
}

func TestOperationsIgnoresReferenceMetadataUnsupportedByTarget(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"3.1.2", "3.2.0"} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := func(summary string) openapi.Document {
				return diffDocument(t, fmt.Sprintf(`{
					"openapi":%q,"paths":{},"components":{"securitySchemes":{
						"Target":{"type":"http","scheme":"bearer"},
						"Alias":{"$ref":"#/components/securitySchemes/Target",
							"summary":%q}
					}}
				}`, version, summary))
			}
			report, err := diff.Operations(
				context.Background(), document("old"), document("new"),
				diff.DefaultOptions(),
			)
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Changes()) != 0 {
				t.Fatalf("unsupported Reference summary changed semantics: %#v",
					report.Changes())
			}
		})
	}
}

func TestOperationsComparesSwaggerResponseContracts(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{
		"swagger":"2.0","produces":["application/json"],
		"paths":{"/pets":{"get":{"responses":{"200":{
			"description":"ok",
			"schema":{"type":"string"},
			"headers":{
				"X-Removed":{"type":"string"},
				"X-Changed":{"type":"string"}
			},
			"examples":{
				"application/json":{"value":"old"},
				"application/xml":"old"
			}
		}}}}}
	}`)
	right := diffDocument(t, `{
		"swagger":"2.0","produces":["application/json"],
		"paths":{"/pets":{"get":{"responses":{"200":{
			"description":"ok",
			"schema":{"type":"integer"},
			"headers":{
				"X-Changed":{"type":"integer"},
				"X-Added":{"type":"string"}
			},
			"examples":{
				"application/json":{"value":"new"},
				"text/plain":"new"
			}
		}}}}}
	}`)
	report, err := diff.Operations(
		context.Background(), left, right, diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		kind           diff.Kind
		classification diff.Classification
		pointer        string
	}{
		{diff.ResponseSchemaChanged, diff.Unknown,
			"/paths/~1pets/get/responses/200/schema"},
		{diff.ResponseHeaderRemoved, diff.Breaking,
			"/paths/~1pets/get/responses/200/headers/X-Removed"},
		{diff.ResponseHeaderChanged, diff.Unknown,
			"/paths/~1pets/get/responses/200/headers/X-Changed"},
		{diff.ResponseHeaderAdded, diff.Compatible,
			"/paths/~1pets/get/responses/200/headers/X-Added"},
		{diff.ResponseExampleChanged, diff.Conditional,
			"/paths/~1pets/get/responses/200/examples/application~1json"},
		{diff.ResponseExampleChanged, diff.Conditional,
			"/paths/~1pets/get/responses/200/examples/application~1xml"},
		{diff.ResponseExampleChanged, diff.Conditional,
			"/paths/~1pets/get/responses/200/examples/text~1plain"},
	}
	changes := report.Changes()
	if len(changes) != len(want) {
		t.Fatalf("changes = %#v", changes)
	}
	for index, expected := range want {
		if changes[index].Kind() != expected.kind ||
			changes[index].Classification() != expected.classification ||
			changes[index].Pointer() != expected.pointer {
			t.Fatalf("change %d = %#v", index, changes[index])
		}
	}
}

func TestOperationsComparesOpenAPIResponseHeaderReferences(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{
		"openapi":"3.1.2","paths":{"/pets":{"get":{"responses":{"200":{
			"description":"ok","headers":{
				"X-Removed":{"schema":{"type":"string"}},
				"X-Changed":{"$ref":"#/components/headers/H"},
				"X-Stable":{"schema":{"type":"string"}}
			}
		}}}}},"components":{"headers":{"H":{"schema":{"type":"string"}}}}
	}`)
	right := diffDocument(t, `{
		"openapi":"3.1.2","paths":{"/pets":{"get":{"responses":{"200":{
			"description":"ok","headers":{
				"x-changed":{"$ref":"#/components/headers/H"},
				"x-stable":{"schema":{"type":"string"}},
				"X-Added":{"schema":{"type":"string"}}
			}
		}}}}},"components":{"headers":{"H":{"schema":{"type":"integer"}}}}
	}`)
	report, err := diff.Operations(
		context.Background(), left, right, diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	changes := report.Changes()
	if len(changes) != 3 ||
		changes[0].Kind() != diff.ResponseHeaderRemoved ||
		changes[0].Pointer() !=
			"/paths/~1pets/get/responses/200/headers/X-Removed" ||
		changes[1].Kind() != diff.ResponseHeaderChanged ||
		changes[1].Pointer() !=
			"/paths/~1pets/get/responses/200/headers/x-changed" ||
		changes[2].Kind() != diff.ResponseHeaderAdded ||
		changes[2].Pointer() !=
			"/paths/~1pets/get/responses/200/headers/X-Added" {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestOperationsBoundsLegacyResponseContractChanges(t *testing.T) {
	t.Parallel()

	for _, response := range []string{
		`{"description":"ok","headers":{
			"X-One":{"type":"string"},"X-Two":{"type":"string"}
		}}`,
		`{"description":"ok","examples":{
			"application/json":{},"application/xml":{}
		}}`,
		`{"description":"ok","schema":{"type":"string"},
			"headers":{"X-One":{"type":"string"}}}`,
	} {
		left := diffDocument(t, `{"swagger":"2.0","paths":{"/pets":{"get":{
			"responses":{"200":`+response+`}
		}}}}`)
		right := diffDocument(t, `{"swagger":"2.0","paths":{"/pets":{"get":{
			"responses":{"200":{"description":"ok"}}
		}}}}`)
		options := diff.DefaultOptions()
		options.MaxChanges = 1
		if _, err := diff.Operations(
			context.Background(), left, right, options,
		); !errors.Is(err, diff.ErrLimitExceeded) {
			t.Fatalf("limit error = %v", err)
		}
	}
}

func TestOperationsComparesSecuritySchemeDefinitions(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{
		"openapi":"3.1.2","paths":{},"components":{"securitySchemes":{
			"Removed":{"type":"apiKey","name":"key","in":"header"},
			"Changed":{"type":"http","scheme":"basic"},
			"Referenced":{"$ref":"security.yaml#/old"},
			"Stable":{"type":"mutualTLS"},"x-note":{"side":"left"}
		}}
	}`)
	right := diffDocument(t, `{
		"openapi":"3.1.2","paths":{},"components":{"securitySchemes":{
			"Changed":{"type":"http","scheme":"bearer"},
			"Referenced":{"$ref":"security.yaml#/new"},
			"Stable":{"type":"mutualTLS"},
			"Added":{"type":"apiKey","name":"key","in":"query"},
			"x-note":{"side":"right"}
		}}
	}`)
	report, err := diff.Operations(context.Background(), left, right, diff.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		kind           diff.Kind
		classification diff.Classification
		pointer        string
	}{
		{diff.SecuritySchemeRemoved, diff.Breaking, "/components/securitySchemes/Removed"},
		{diff.SecuritySchemeChanged, diff.Conditional, "/components/securitySchemes/Changed"},
		{diff.SecuritySchemeChanged, diff.Unknown, "/components/securitySchemes/Referenced"},
		{diff.SecuritySchemeAdded, diff.Additive, "/components/securitySchemes/Added"},
	}
	changes := report.Changes()
	if len(changes) != len(want) {
		t.Fatalf("changes = %#v", changes)
	}
	for index, expected := range want {
		if changes[index].Kind() != expected.kind ||
			changes[index].Classification() != expected.classification ||
			changes[index].Pointer() != expected.pointer {
			t.Fatalf("change %d = %#v, want %#v", index, changes[index], expected)
		}
	}
}

func TestOperationsComparesSwaggerSecurityDefinitions(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{
		"swagger":"2.0","paths":{},"securityDefinitions":{
			"Old":{"type":"basic"}
		}
	}`)
	right := diffDocument(t, `{
		"swagger":"2.0","paths":{},"securityDefinitions":{
			"New":{"type":"basic"}
		}
	}`)
	report, err := diff.Operations(context.Background(), left, right, diff.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	changes := report.Changes()
	if len(changes) != 2 ||
		changes[0].Kind() != diff.SecuritySchemeRemoved ||
		changes[0].Pointer() != "/securityDefinitions/Old" ||
		changes[1].Kind() != diff.SecuritySchemeAdded ||
		changes[1].Pointer() != "/securityDefinitions/New" {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestOperationsBoundsSecuritySchemeDefinitionChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
	}{
		{
			name:  "removals",
			left:  `"One":{"type":"http"},"Two":{"type":"http"}`,
			right: ``,
		},
		{
			name: "change after removal",
			left: `"Removed":{"type":"http"},` +
				`"Shared":{"type":"http","scheme":"basic"}`,
			right: `"Shared":{"type":"http","scheme":"bearer"}`,
		},
		{
			name:  "addition after removal",
			left:  `"Removed":{"type":"http"}`,
			right: `"Added":{"type":"http"}`,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			wrap := func(schemes string) string {
				return `{"openapi":"3.1.2","paths":{},` +
					`"components":{"securitySchemes":{` + schemes + `}}}`
			}
			options := diff.DefaultOptions()
			options.MaxChanges = 1
			_, err := diff.Operations(
				context.Background(), diffDocument(t, wrap(test.left)),
				diffDocument(t, wrap(test.right)), options,
			)
			if !errors.Is(err, diff.ErrLimitExceeded) {
				t.Fatalf("limit error = %v", err)
			}
		})
	}
}

func TestOperationsRejectsInvalidComparisonsAndBounds(t *testing.T) {
	t.Parallel()

	document := diffDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/one":{"get":{"responses":{"200":{"description":"ok"}}}}}
	}`)
	otherDialect := diffDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},"paths":{}
	}`)
	//lint:ignore SA1012 This assertion verifies the nil-context contract.
	//nolint:staticcheck // This assertion verifies the nil-context contract.
	if _, err := diff.Operations(nil, document, document, diff.DefaultOptions()); !errors.Is(err, diff.ErrInvalidInput) {
		t.Fatalf("nil context error = %v", err)
	}
	if _, err := diff.Operations(context.Background(), nil, document, diff.DefaultOptions()); !errors.Is(err, diff.ErrInvalidInput) {
		t.Fatalf("nil document error = %v", err)
	}
	if _, err := diff.Operations(context.Background(), document, otherDialect, diff.DefaultOptions()); !errors.Is(err, diff.ErrUnsupportedComparison) {
		t.Fatalf("dialect error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := diff.Operations(ctx, document, document, diff.DefaultOptions()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
	options := diff.DefaultOptions()
	options.MaxChanges = -1
	if _, err := diff.Operations(context.Background(), document, document, options); !errors.Is(err, diff.ErrInvalidOptions) {
		t.Fatalf("options error = %v", err)
	}
	options = diff.DefaultOptions()
	options.MaxNodes = -1
	if _, err := diff.Operations(context.Background(), document, document, options); !errors.Is(err, diff.ErrInvalidOptions) {
		t.Fatalf("node options error = %v", err)
	}
	options = diff.DefaultOptions()
	options.MaxDepth = -1
	if _, err := diff.Operations(context.Background(), document, document, options); !errors.Is(err, diff.ErrInvalidOptions) {
		t.Fatalf("depth options error = %v", err)
	}
	options = diff.DefaultOptions()
	options.MaxReferenceDepth = -1
	if _, err := diff.Operations(
		context.Background(), document, document, options,
	); !errors.Is(err, diff.ErrInvalidOptions) {
		t.Fatalf("reference options error = %v", err)
	}
	options = diff.DefaultOptions()
	options.MaxResolvedNodes = -1
	if _, err := diff.Operations(
		context.Background(), document, document, options,
	); !errors.Is(err, diff.ErrInvalidOptions) {
		t.Fatalf("resolved node options error = %v", err)
	}
	options = diff.DefaultOptions()
	options.LeftResourceURI = "https://example.test/api.json#fragment"
	if _, err := diff.Operations(
		context.Background(), document, document, options,
	); !errors.Is(err, diff.ErrInvalidOptions) {
		t.Fatalf("resource URI options error = %v", err)
	}
	options = diff.DefaultOptions()
	options.ExtensionClassification = "invalid"
	if _, err := diff.Operations(
		context.Background(), document, document, options,
	); !errors.Is(err, diff.ErrInvalidOptions) {
		t.Fatalf("extension options error = %v", err)
	}
	options = diff.DefaultOptions()
	options.MaxChanges = 1
	empty := diffDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{}
	}`)
	if _, err := diff.Operations(context.Background(), document, empty, options); err != nil {
		t.Fatalf("one change should fit: %v", err)
	}
	two := diffDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{
			"/one":{"get":{"responses":{"200":{"description":"ok"}}}},
			"/two":{"get":{"responses":{"200":{"description":"ok"}}}}
		}
	}`)
	if _, err := diff.Operations(context.Background(), two, empty, options); !errors.Is(err, diff.ErrLimitExceeded) {
		t.Fatalf("change limit error = %v", err)
	}
}

func TestOperationsBoundsDocumentNodesAndDepth(t *testing.T) {
	t.Parallel()

	document := diffDocument(t, `{
		"openapi":"3.2.0","paths":{},"x-values":[[[true]],false]
	}`)
	options := diff.DefaultOptions()
	options.MaxNodes = 2
	if _, err := diff.Operations(
		context.Background(), document, document, options,
	); !errors.Is(err, diff.ErrLimitExceeded) {
		t.Fatalf("node limit error = %v", err)
	}
	options = diff.DefaultOptions()
	options.MaxDepth = 2
	if _, err := diff.Operations(
		context.Background(), document, document, options,
	); !errors.Is(err, diff.ErrLimitExceeded) {
		t.Fatalf("depth limit error = %v", err)
	}
	left := diffDocument(t, `{"openapi":"3.2.0","paths":{}}`)
	if _, err := diff.Operations(
		context.Background(), left, document, options,
	); !errors.Is(err, diff.ErrLimitExceeded) {
		t.Fatalf("right document limit error = %v", err)
	}
}

func TestOperationsUsesDialectSpecificOperationFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		left    string
		right   string
		pointer string
	}{
		{
			name: "Swagger ignores OpenAPI methods",
			left: `{
				"swagger":"2.0","info":{"title":"API","version":"1"},
				"paths":{"/pets":{"trace":{},"query":{},
					"additionalOperations":{"COPY":{}}}}
			}`,
			right: `{
				"swagger":"2.0","info":{"title":"API","version":"1"},
				"paths":{"/pets":{}}
			}`,
		},
		{
			name: "OpenAPI 3.0 includes trace",
			left: `{
				"openapi":"3.0.4","info":{"title":"API","version":"1"},
				"paths":{"/pets":{"trace":{}}}
			}`,
			right: `{
				"openapi":"3.0.0","info":{"title":"API","version":"1"},
				"paths":{"/pets":{}}
			}`,
			pointer: "/paths/~1pets/trace",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			report, err := diff.Operations(
				context.Background(),
				diffDocument(t, test.left),
				diffDocument(t, test.right),
				diff.Options{},
			)
			if err != nil {
				t.Fatal(err)
			}
			changes := report.Changes()
			if test.pointer == "" && len(changes) != 0 {
				t.Fatalf("changes = %#v, want none", changes)
			}
			if test.pointer != "" && (len(changes) != 1 || changes[0].Pointer() != test.pointer) {
				t.Fatalf("changes = %#v, want pointer %q", changes, test.pointer)
			}
		})
	}
}

func TestOperationsEnforcesLimitAcrossChangeCategories(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
	}{
		{
			name:  "removed operation",
			left:  `{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{"/pets":{"get":{},"post":{}}}}`,
			right: `{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{"/pets":{}}}`,
		},
		{
			name:  "added path",
			left:  `{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{}}`,
			right: `{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{"/one":{},"/two":{}}}`,
		},
		{
			name:  "added operation",
			left:  `{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{"/pets":{}}}`,
			right: `{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{"/pets":{"get":{},"post":{}}}}`,
		},
		{
			name:  "removed webhook",
			left:  `{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{},"webhooks":{"one":{},"two":{}}}`,
			right: `{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{},"webhooks":{}}`,
		},
		{
			name:  "added webhook",
			left:  `{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{},"webhooks":{}}`,
			right: `{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{},"webhooks":{"one":{},"two":{}}}`,
		},
		{
			name:  "removed webhook operation",
			left:  `{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{},"webhooks":{"event":{"get":{},"post":{}}}}`,
			right: `{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{},"webhooks":{"event":{}}}`,
		},
		{
			name:  "added webhook operation",
			left:  `{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{},"webhooks":{"event":{}}}`,
			right: `{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{},"webhooks":{"event":{"get":{},"post":{}}}}`,
		},
		{
			name:  "operation identifier after removal",
			left:  `{"openapi":"3.2.0","paths":{"/pets":{"get":{"operationId":"old"},"post":{}}}}`,
			right: `{"openapi":"3.2.0","paths":{"/pets":{"get":{"operationId":"new"}}}}`,
		},
		{
			name:  "request contract after identifier",
			left:  `{"openapi":"3.2.0","paths":{"/pets":{"post":{"operationId":"old","requestBody":{"required":false}}}}}`,
			right: `{"openapi":"3.2.0","paths":{"/pets":{"post":{"operationId":"new","requestBody":{"required":true}}}}}`,
		},
		{
			name:  "removed responses",
			left:  `{"openapi":"3.2.0","paths":{"/pets":{"get":{"responses":{"200":{},"404":{}}}}}}`,
			right: `{"openapi":"3.2.0","paths":{"/pets":{"get":{"responses":{}}}}}`,
		},
		{
			name:  "added responses",
			left:  `{"openapi":"3.2.0","paths":{"/pets":{"get":{"responses":{}}}}}`,
			right: `{"openapi":"3.2.0","paths":{"/pets":{"get":{"responses":{"200":{},"404":{}}}}}}`,
		},
		{
			name:  "changed response after identifier",
			left:  `{"openapi":"3.2.0","paths":{"/pets":{"get":{"operationId":"old","responses":{"200":{"$ref":"#/components/responses/One"}}}}}}`,
			right: `{"openapi":"3.2.0","paths":{"/pets":{"get":{"operationId":"new","responses":{"200":{"$ref":"#/components/responses/Two"}}}}}}`,
		},
		{
			name:  "removed request media types",
			left:  `{"openapi":"3.2.0","paths":{"/pets":{"post":{"requestBody":{"content":{"application/json":{},"application/xml":{}}}}}}}`,
			right: `{"openapi":"3.2.0","paths":{"/pets":{"post":{"requestBody":{"content":{}}}}}}`,
		},
		{
			name:  "added request media types",
			left:  `{"openapi":"3.2.0","paths":{"/pets":{"post":{"requestBody":{"content":{}}}}}}`,
			right: `{"openapi":"3.2.0","paths":{"/pets":{"post":{"requestBody":{"content":{"application/json":{},"application/xml":{}}}}}}}`,
		},
		{
			name:  "removed response media types",
			left:  `{"openapi":"3.2.0","paths":{"/pets":{"get":{"responses":{"200":{"content":{"application/json":{},"application/xml":{}}}}}}}}`,
			right: `{"openapi":"3.2.0","paths":{"/pets":{"get":{"responses":{"200":{"content":{}}}}}}}`,
		},
		{
			name:  "added response media types",
			left:  `{"openapi":"3.2.0","paths":{"/pets":{"get":{"responses":{"200":{"content":{}}}}}}}`,
			right: `{"openapi":"3.2.0","paths":{"/pets":{"get":{"responses":{"200":{"content":{"application/json":{},"application/xml":{}}}}}}}}`,
		},
		{
			name:  "webhook request contract",
			left:  `{"openapi":"3.2.0","paths":{},"webhooks":{"event":{"post":{"operationId":"old","requestBody":{"required":false}}}}}`,
			right: `{"openapi":"3.2.0","paths":{},"webhooks":{"event":{"post":{"operationId":"new","requestBody":{"required":true}}}}}`,
		},
		{
			name:  "invalid parameter collection after removal",
			left:  `{"openapi":"3.2.0","paths":{"/pets":{"get":{"parameters":false},"post":{}}}}`,
			right: `{"openapi":"3.2.0","paths":{"/pets":{"get":{"parameters":[]}}}}`,
		},
		{
			name:  "invalid parameter required after identifier",
			left:  `{"openapi":"3.2.0","paths":{"/pets":{"get":{"operationId":"old","parameters":[{"name":"limit","in":"query","required":"yes"}]}}}}`,
			right: `{"openapi":"3.2.0","paths":{"/pets":{"get":{"operationId":"new","parameters":[{"name":"limit","in":"query","required":false}]}}}}`,
		},
		{
			name:  "parameter required after identifier",
			left:  `{"openapi":"3.2.0","paths":{"/pets":{"get":{"operationId":"old","parameters":[{"name":"limit","in":"query","required":false}]}}}}`,
			right: `{"openapi":"3.2.0","paths":{"/pets":{"get":{"operationId":"new","parameters":[{"name":"limit","in":"query","required":true}]}}}}`,
		},
		{
			name:  "removed parameters",
			left:  `{"openapi":"3.2.0","paths":{"/pets":{"get":{"parameters":[{"name":"one","in":"query"},{"name":"two","in":"query"}]}}}}`,
			right: `{"openapi":"3.2.0","paths":{"/pets":{"get":{"parameters":[]}}}}`,
		},
		{
			name:  "added parameters",
			left:  `{"openapi":"3.2.0","paths":{"/pets":{"get":{"parameters":[]}}}}`,
			right: `{"openapi":"3.2.0","paths":{"/pets":{"get":{"parameters":[{"name":"one","in":"query"},{"name":"two","in":"query"}]}}}}`,
		},
		{
			name:  "changed parameter reference after identifier",
			left:  `{"openapi":"3.2.0","paths":{"/pets":{"get":{"operationId":"old","parameters":[{"$ref":"#/components/parameters/One"}]}}}}`,
			right: `{"openapi":"3.2.0","paths":{"/pets":{"get":{"operationId":"new","parameters":[{"$ref":"#/components/parameters/Two"}]}}}}`,
		},
		{
			name:  "removed unresolved parameters",
			left:  `{"openapi":"3.2.0","paths":{"/pets":{"get":{"parameters":[{"$ref":"#/components/parameters/One"},{"$ref":"#/components/parameters/Two"}]}}}}`,
			right: `{"openapi":"3.2.0","paths":{"/pets":{"get":{"parameters":[]}}}}`,
		},
		{
			name:  "added unresolved parameters",
			left:  `{"openapi":"3.2.0","paths":{"/pets":{"get":{"parameters":[]}}}}`,
			right: `{"openapi":"3.2.0","paths":{"/pets":{"get":{"parameters":[{"$ref":"#/components/parameters/One"},{"$ref":"#/components/parameters/Two"}]}}}}`,
		},
		{
			name: "two invalid parameter required changes",
			left: `{"openapi":"3.2.0","paths":{"/pets":{"get":{"parameters":[
				{"name":"one","in":"query","required":"yes"},
				{"name":"two","in":"query","required":"yes"}
			]}}}}`,
			right: `{"openapi":"3.2.0","paths":{"/pets":{"get":{"parameters":[
				{"name":"one","in":"query","required":false},
				{"name":"two","in":"query","required":false}
			]}}}}`,
		},
		{
			name: "two parameter required changes",
			left: `{"openapi":"3.2.0","paths":{"/pets":{"get":{"parameters":[
				{"name":"one","in":"query","required":false},
				{"name":"two","in":"query","required":false}
			]}}}}`,
			right: `{"openapi":"3.2.0","paths":{"/pets":{"get":{"parameters":[
				{"name":"one","in":"query","required":true},
				{"name":"two","in":"query","required":true}
			]}}}}`,
		},
		{
			name: "two changed parameter references",
			left: `{"openapi":"3.2.0","paths":{"/pets":{"get":{"parameters":[
				{"$ref":"#/components/parameters/One"},
				{"$ref":"#/components/parameters/Two"}
			]}}}}`,
			right: `{"openapi":"3.2.0","paths":{"/pets":{"get":{"parameters":[
				{"$ref":"#/components/parameters/Three"},
				{"$ref":"#/components/parameters/Four"}
			]}}}}`,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			options := diff.DefaultOptions()
			options.MaxChanges = 1
			_, err := diff.Operations(
				context.Background(),
				diffDocument(t, test.left),
				diffDocument(t, test.right),
				options,
			)
			if !errors.Is(err, diff.ErrLimitExceeded) {
				t.Fatalf("error = %v, want limit exceeded", err)
			}
		})
	}
}

func TestOperationsIgnoresNonSurfaceMembers(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{
			"x-path":{},"not-a-path":{},"/scalar":"ignored",
			"/pets":{"x-operation":{},"get":"ignored",
				"additionalOperations":{"COPY":"ignored"}}
		},
		"webhooks":{"x-hook":{},"scalar":"ignored"}
	}`)
	right := diffDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/pets":{}},"webhooks":{}
	}`)
	report, err := diff.Operations(context.Background(), left, right, diff.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if changes := report.Changes(); len(changes) != 1 ||
		changes[0].Kind() != diff.ExtensionChanged ||
		changes[0].Pointer() != "/paths/~1pets/x-operation" {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestOperationsObservesCancellationDuringCollection(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/one":{},"/two":{}}
	}`)
	right := diffDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{}
	}`)
	ctx := &cancelAfterInitialCheck{Context: context.Background()}
	if _, err := diff.Operations(ctx, left, right, diff.DefaultOptions()); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation", err)
	}
}

func TestOperationsClassifiesRequestAndResponseContracts(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{
		"openapi":"3.2.0","paths":{"/pets":{"post":{
			"operationId":"createPet",
			"requestBody":{"required":false,"content":{
				"application/json":{},"application/xml":{}
			}},
			"responses":{
				"200":{"description":"ok","content":{
					"application/json":{},"application/xml":{}
				}},
				"404":{"description":"missing"}
			}
		}}}
	}`)
	right := diffDocument(t, `{
		"openapi":"3.2.0","paths":{"/pets":{"post":{
			"operationId":"storePet",
			"requestBody":{"required":true,"content":{
				"application/json":{},"application/yaml":{}
			}},
			"responses":{
				"200":{"description":"ok","content":{
					"application/json":{},"application/protobuf":{}
				}},
				"201":{"description":"created"}
			}
		}}}
	}`)
	report, err := diff.Operations(context.Background(), left, right, diff.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		kind           diff.Kind
		classification diff.Classification
		pointer        string
	}{
		{diff.OperationIDChanged, diff.Conditional, "/paths/~1pets/post/operationId"},
		{diff.RequestBodyRequired, diff.Breaking, "/paths/~1pets/post/requestBody/required"},
		{diff.RequestMediaTypeRemoved, diff.Breaking, "/paths/~1pets/post/requestBody/content/application~1xml"},
		{diff.RequestMediaTypeAdded, diff.Additive, "/paths/~1pets/post/requestBody/content/application~1yaml"},
		{diff.ResponseRemoved, diff.Conditional, "/paths/~1pets/post/responses/404"},
		{diff.ResponseAdded, diff.Conditional, "/paths/~1pets/post/responses/201"},
		{diff.ResponseMediaTypeRemoved, diff.Breaking, "/paths/~1pets/post/responses/200/content/application~1xml"},
		{diff.ResponseMediaTypeAdded, diff.Additive, "/paths/~1pets/post/responses/200/content/application~1protobuf"},
	}
	changes := report.Changes()
	if len(changes) != len(want) {
		t.Fatalf("changes = %#v", changes)
	}
	for index, expected := range want {
		if changes[index].Kind() != expected.kind ||
			changes[index].Classification() != expected.classification ||
			changes[index].Pointer() != expected.pointer {
			t.Fatalf("change %d = %#v, want %#v", index, changes[index], expected)
		}
	}
}

func TestOperationsClassifiesRequestBodyPresence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		leftBody       string
		rightBody      string
		kind           diff.Kind
		classification diff.Classification
	}{
		{name: "optional added", rightBody: `,"requestBody":{"required":false}`, kind: diff.RequestBodyAdded, classification: diff.Additive},
		{name: "required added", rightBody: `,"requestBody":{"required":true}`, kind: diff.RequestBodyAdded, classification: diff.Breaking},
		{name: "removed", leftBody: `,"requestBody":{}`, kind: diff.RequestBodyRemoved, classification: diff.Conditional},
		{name: "required relaxed", leftBody: `,"requestBody":{"required":true}`, rightBody: `,"requestBody":{"required":false}`, kind: diff.RequestBodyOptional, classification: diff.Compatible},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			left := diffDocument(t, `{"openapi":"3.1.2","paths":{"/pets":{"post":{"responses":{}`+test.leftBody+`}}}}`)
			right := diffDocument(t, `{"openapi":"3.1.2","paths":{"/pets":{"post":{"responses":{}`+test.rightBody+`}}}}`)
			report, err := diff.Operations(context.Background(), left, right, diff.DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			changes := report.Changes()
			if len(changes) != 1 || changes[0].Kind() != test.kind ||
				changes[0].Classification() != test.classification {
				t.Fatalf("changes = %#v", changes)
			}
		})
	}
}

func TestOperationsReportsUnresolvedResponseChangesAsUnknown(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{"openapi":"3.0.4","paths":{"/pets":{"get":{"responses":{"200":{"$ref":"#/components/responses/One"}}}}}}`)
	right := diffDocument(t, `{"openapi":"3.0.4","paths":{"/pets":{"get":{"responses":{"200":{"$ref":"#/components/responses/Two"}}}}}}`)
	report, err := diff.Operations(context.Background(), left, right, diff.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	changes := report.Changes()
	if len(changes) != 1 || changes[0].Kind() != diff.ResponseChanged ||
		changes[0].Classification() != diff.Unknown ||
		changes[0].Pointer() != "/paths/~1pets/get/responses/200" {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestOperationsKeepsInvalidAndUnresolvedContentExplicit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		left           string
		right          string
		kind           diff.Kind
		classification diff.Classification
	}{
		{
			name:           "malformed added request body",
			left:           `{"openapi":"3.2.0","paths":{"/pets":{"post":{}}}}`,
			right:          `{"openapi":"3.2.0","paths":{"/pets":{"post":{"requestBody":null}}}}`,
			kind:           diff.RequestBodyAdded,
			classification: diff.Unknown,
		},
		{
			name:           "unresolved request reference",
			left:           `{"openapi":"3.2.0","paths":{"/pets":{"post":{"requestBody":{"$ref":"#/components/requestBodies/One"}}}}}`,
			right:          `{"openapi":"3.2.0","paths":{"/pets":{"post":{"requestBody":{"$ref":"#/components/requestBodies/Two"}}}}}`,
			kind:           diff.RequestBodyChanged,
			classification: diff.Unknown,
		},
		{
			name:           "invalid required value",
			left:           `{"openapi":"3.2.0","paths":{"/pets":{"post":{"requestBody":{"required":"yes"}}}}}`,
			right:          `{"openapi":"3.2.0","paths":{"/pets":{"post":{"requestBody":{"required":false}}}}}`,
			kind:           diff.RequestBodyChanged,
			classification: diff.Unknown,
		},
		{
			name:           "invalid operation identifier",
			left:           `{"openapi":"3.2.0","paths":{"/pets":{"get":{"operationId":false}}}}`,
			right:          `{"openapi":"3.2.0","paths":{"/pets":{"get":{"operationId":"getPets"}}}}`,
			kind:           diff.OperationIDChanged,
			classification: diff.Conditional,
		},
		{
			name:           "malformed response value",
			left:           `{"openapi":"3.2.0","paths":{"/pets":{"get":{"responses":{"200":false}}}}}`,
			right:          `{"openapi":"3.2.0","paths":{"/pets":{"get":{"responses":{"200":{}}}}}}`,
			kind:           diff.ResponseChanged,
			classification: diff.Unknown,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			report, err := diff.Operations(
				context.Background(),
				diffDocument(t, test.left),
				diffDocument(t, test.right),
				diff.DefaultOptions(),
			)
			if err != nil {
				t.Fatal(err)
			}
			changes := report.Changes()
			if len(changes) != 1 || changes[0].Kind() != test.kind ||
				changes[0].Classification() != test.classification {
				t.Fatalf("changes = %#v", changes)
			}
		})
	}

	swaggerLeft := diffDocument(t, `{"swagger":"2.0","paths":{"/pets":{"post":{"operationId":"old","requestBody":{"required":false},"responses":{"200":{"content":{"application/json":{}}}}}}}}`)
	swaggerRight := diffDocument(t, `{"swagger":"2.0","paths":{"/pets":{"post":{"operationId":"new","requestBody":{"required":true},"responses":{"200":{"content":{"application/xml":{}}}}}}}}`)
	report, err := diff.Operations(context.Background(), swaggerLeft, swaggerRight, diff.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if changes := report.Changes(); len(changes) != 1 || changes[0].Kind() != diff.OperationIDChanged {
		t.Fatalf("Swagger changes = %#v", changes)
	}
}

func TestOperationsClassifiesEffectiveParameters(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{
		"openapi":"3.2.0","paths":{"/pets/{id}":{
			"parameters":[
				{"name":"id","in":"path","required":true},
				{"name":"Trace","in":"header","required":false}
			],
			"get":{"parameters":[
				{"name":"limit","in":"query","required":false}
			]}
		}}
	}`)
	right := diffDocument(t, `{
		"openapi":"3.2.0","paths":{"/pets/{id}":{
			"parameters":[
				{"name":"id","in":"path","required":true},
				{"name":"trace","in":"header","required":true}
			],
			"get":{"parameters":[
				{"name":"expand","in":"query","required":false}
			]}
		}}
	}`)
	report, err := diff.Operations(context.Background(), left, right, diff.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		kind           diff.Kind
		classification diff.Classification
		pointer        string
	}{
		{diff.ParameterRequired, diff.Breaking, "/paths/~1pets~1{id}/parameters/1/required"},
		{diff.ParameterRemoved, diff.Conditional, "/paths/~1pets~1{id}/get/parameters/0"},
		{diff.ParameterAdded, diff.Additive, "/paths/~1pets~1{id}/get/parameters/0"},
	}
	changes := report.Changes()
	if len(changes) != len(want) {
		t.Fatalf("changes = %#v", changes)
	}
	for index, expected := range want {
		if changes[index].Kind() != expected.kind ||
			changes[index].Classification() != expected.classification ||
			changes[index].Pointer() != expected.pointer {
			t.Fatalf("change %d = %#v, want %#v", index, changes[index], expected)
		}
	}
}

func TestOperationsTreatsAbsentNonPathRequiredAsFalse(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		left := diffDocument(t, `{
			"openapi":"`+version+`","paths":{"/pets":{"get":{
				"parameters":[{"name":"limit","in":"query"}]
			}}}
		}`)
		right := diffDocument(t, `{
			"openapi":"`+version+`","paths":{"/pets":{"get":{
				"parameters":[{"name":"limit","in":"query","required":false}]
			}}}
		}`)
		report, err := diff.Operations(
			context.Background(), left, right, diff.DefaultOptions(),
		)
		if err != nil {
			t.Fatal(err)
		}
		if changes := report.Changes(); len(changes) != 0 {
			t.Errorf("version %s changes = %#v", version, changes)
		}
	}
}

func TestOperationsClassifiesParameterPresenceAndRelaxation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		left           string
		right          string
		kind           diff.Kind
		classification diff.Classification
	}{
		{
			name:           "required query added",
			left:           `[]`,
			right:          `[{"name":"limit","in":"query","required":true}]`,
			kind:           diff.ParameterAdded,
			classification: diff.Breaking,
		},
		{
			name:           "path added",
			left:           `[]`,
			right:          `[{"name":"id","in":"path"}]`,
			kind:           diff.ParameterAdded,
			classification: diff.Breaking,
		},
		{
			name:           "required relaxed",
			left:           `[{"name":"limit","in":"query","required":true}]`,
			right:          `[{"name":"limit","in":"query","required":false}]`,
			kind:           diff.ParameterOptional,
			classification: diff.Compatible,
		},
		{
			name:           "reference changed",
			left:           `[{"$ref":"#/components/parameters/One"}]`,
			right:          `[{"$ref":"#/components/parameters/Two"}]`,
			kind:           diff.ParameterChanged,
			classification: diff.Unknown,
		},
		{
			name:           "malformed collection",
			left:           `false`,
			right:          `[]`,
			kind:           diff.ParameterChanged,
			classification: diff.Unknown,
		},
		{
			name:           "malformed right collection",
			left:           `[]`,
			right:          `false`,
			kind:           diff.ParameterChanged,
			classification: diff.Unknown,
		},
		{
			name:           "invalid required addition",
			left:           `[]`,
			right:          `[{"name":"limit","in":"query","required":"yes"}]`,
			kind:           diff.ParameterAdded,
			classification: diff.Unknown,
		},
		{
			name:           "unresolved removed",
			left:           `[{"$ref":"#/components/parameters/One"}]`,
			right:          `[]`,
			kind:           diff.ParameterRemoved,
			classification: diff.Unknown,
		},
		{
			name:           "unresolved added",
			left:           `[]`,
			right:          `[{"$ref":"#/components/parameters/One"}]`,
			kind:           diff.ParameterAdded,
			classification: diff.Unknown,
		},
		{
			name:           "invalid ref differs from empty ref",
			left:           `[{"$ref":false}]`,
			right:          `[{"$ref":""}]`,
			kind:           diff.ParameterChanged,
			classification: diff.Unknown,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			left := diffDocument(t, `{"openapi":"3.1.2","paths":{"/pets":{"get":{"parameters":`+test.left+`}}}}`)
			right := diffDocument(t, `{"openapi":"3.1.2","paths":{"/pets":{"get":{"parameters":`+test.right+`}}}}`)
			report, err := diff.Operations(context.Background(), left, right, diff.DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			changes := report.Changes()
			if len(changes) != 1 || changes[0].Kind() != test.kind ||
				changes[0].Classification() != test.classification {
				t.Fatalf("changes = %#v", changes)
			}
		})
	}
}

func TestOperationsAppliesOperationParameterOverrides(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{"openapi":"3.0.4","paths":{"/pets":{
		"parameters":[{"name":"limit","in":"query","required":true}],
		"get":{"parameters":[{"name":"limit","in":"query","required":false}]}
	}}}`)
	right := diffDocument(t, `{"openapi":"3.0.4","paths":{"/pets":{
		"parameters":[{"name":"limit","in":"query","required":true}],
		"get":{"parameters":[{"name":"limit","in":"query","required":false}]}
	}}}`)
	report, err := diff.Operations(context.Background(), left, right, diff.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if changes := report.Changes(); len(changes) != 0 {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestOperationsClassifiesSwaggerParameters(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{"swagger":"2.0","paths":{"/pets":{"get":{"parameters":[]}}}}`)
	right := diffDocument(t, `{"swagger":"2.0","paths":{"/pets":{"get":{"parameters":[{"name":"limit","in":"query","required":true}]}}}}`)
	report, err := diff.Operations(context.Background(), left, right, diff.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	changes := report.Changes()
	if len(changes) != 1 || changes[0].Kind() != diff.ParameterAdded ||
		changes[0].Classification() != diff.Breaking {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestOperationsHandlesStableAndRelocatedUnresolvedParameters(t *testing.T) {
	t.Parallel()

	stable := diffDocument(t, `{"openapi":"3.2.0","paths":{"/pets":{"get":{"parameters":[{}, {"$ref":"#/components/parameters/One"}]}}}}`)
	report, err := diff.Operations(context.Background(), stable, stable, diff.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if changes := report.Changes(); len(changes) != 0 {
		t.Fatalf("stable unresolved changes = %#v", changes)
	}

	left := diffDocument(t, `{"openapi":"3.2.0","paths":{"/pets":{"parameters":false,"get":{}}}}`)
	right := diffDocument(t, `{"openapi":"3.2.0","paths":{"/pets":{"get":{"parameters":false}}}}`)
	report, err = diff.Operations(context.Background(), left, right, diff.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if changes := report.Changes(); len(changes) != 1 || changes[0].Kind() != diff.ParameterChanged {
		t.Fatalf("relocated invalid changes = %#v", changes)
	}

	sameInvalid := diffDocument(t, `{"openapi":"3.2.0","paths":{"/pets":{"get":{"parameters":false}}}}`)
	report, err = diff.Operations(context.Background(), sameInvalid, sameInvalid, diff.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if changes := report.Changes(); len(changes) != 0 {
		t.Fatalf("same invalid changes = %#v", changes)
	}
}

func TestOperationsComparesParameterSerializationAndSchemas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		version        string
		left           string
		right          string
		kind           diff.Kind
		classification diff.Classification
		pointer        string
	}{
		{
			name: "style", version: "3.1.2",
			left:           `{"name":"filter","in":"query","style":"form"}`,
			right:          `{"name":"filter","in":"query","style":"deepObject"}`,
			kind:           diff.ParameterSerializationChanged,
			classification: diff.Breaking, pointer: "/style",
		},
		{
			name: "explode", version: "3.1.2",
			left:           `{"name":"filter","in":"query","style":"form"}`,
			right:          `{"name":"filter","in":"query","style":"form","explode":false}`,
			kind:           diff.ParameterSerializationChanged,
			classification: diff.Breaking, pointer: "/explode",
		},
		{
			name: "allow reserved", version: "3.1.2",
			left:           `{"name":"filter","in":"query"}`,
			right:          `{"name":"filter","in":"query","allowReserved":true}`,
			kind:           diff.ParameterSerializationChanged,
			classification: diff.Conditional, pointer: "/allowReserved",
		},
		{
			name: "malformed style", version: "3.1.2",
			left:           `{"name":"filter","in":"query","style":"form"}`,
			right:          `{"name":"filter","in":"query","style":false}`,
			kind:           diff.ParameterSerializationChanged,
			classification: diff.Unknown, pointer: "/style",
		},
		{
			name: "malformed explode", version: "3.1.2",
			left:           `{"name":"filter","in":"query","explode":true}`,
			right:          `{"name":"filter","in":"query","explode":"yes"}`,
			kind:           diff.ParameterSerializationChanged,
			classification: diff.Unknown, pointer: "/explode",
		},
		{
			name: "malformed allow reserved", version: "3.1.2",
			left:           `{"name":"filter","in":"query","allowReserved":false}`,
			right:          `{"name":"filter","in":"query","allowReserved":"yes"}`,
			kind:           diff.ParameterSerializationChanged,
			classification: diff.Unknown, pointer: "/allowReserved",
		},
		{
			name: "schema", version: "3.1.2",
			left:           `{"name":"filter","in":"query","schema":{"type":"string"}}`,
			right:          `{"name":"filter","in":"query","schema":{"type":"integer"}}`,
			kind:           diff.ParameterSchemaChanged,
			classification: diff.Unknown, pointer: "/schema",
		},
		{
			name: "content", version: "3.1.2",
			left:           `{"name":"filter","in":"query","content":{"application/json":{"schema":{"type":"string"}}}}`,
			right:          `{"name":"filter","in":"query","content":{"text/plain":{"schema":{"type":"string"}}}}`,
			kind:           diff.ParameterContentChanged,
			classification: diff.Unknown, pointer: "/content",
		},
		{
			name: "parameter example", version: "3.1.2",
			left:           `{"name":"filter","in":"query","example":"old"}`,
			right:          `{"name":"filter","in":"query","example":"new"}`,
			kind:           diff.ParameterExampleChanged,
			classification: diff.Conditional, pointer: "/example",
		},
		{
			name: "parameter examples", version: "3.1.2",
			left:           `{"name":"filter","in":"query","examples":{"old":{"value":1}}}`,
			right:          `{"name":"filter","in":"query","examples":{"new":{"value":1}}}`,
			kind:           diff.ParameterExampleChanged,
			classification: diff.Conditional, pointer: "/examples",
		},
		{
			name: "Swagger collection format", version: "2.0",
			left:           `{"name":"filter","in":"query","type":"array","collectionFormat":"csv"}`,
			right:          `{"name":"filter","in":"query","type":"array","collectionFormat":"multi"}`,
			kind:           diff.ParameterSerializationChanged,
			classification: diff.Breaking, pointer: "/collectionFormat",
		},
		{
			name: "malformed Swagger collection format", version: "2.0",
			left:           `{"name":"filter","in":"query","type":"array"}`,
			right:          `{"name":"filter","in":"query","type":"array","collectionFormat":false}`,
			kind:           diff.ParameterSerializationChanged,
			classification: diff.Unknown, pointer: "/collectionFormat",
		},
		{
			name: "Swagger item schema", version: "2.0",
			left:           `{"name":"filter","in":"query","type":"array","items":{"type":"string"}}`,
			right:          `{"name":"filter","in":"query","type":"array","items":{"type":"integer"}}`,
			kind:           diff.ParameterSchemaChanged,
			classification: diff.Unknown, pointer: "/items",
		},
		{
			name: "Swagger default", version: "2.0",
			left:           `{"name":"filter","in":"query","type":"string","default":"old"}`,
			right:          `{"name":"filter","in":"query","type":"string","default":"new"}`,
			kind:           diff.ParameterDefaultChanged,
			classification: diff.Conditional, pointer: "/default",
		},
		{
			name: "Swagger enum", version: "2.0",
			left:           `{"name":"filter","in":"query","type":"string","enum":["one"]}`,
			right:          `{"name":"filter","in":"query","type":"string","enum":["two"]}`,
			kind:           diff.ParameterSchemaChanged,
			classification: diff.Unknown, pointer: "/enum",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			marker := `"openapi":"` + test.version + `"`
			if test.version == "2.0" {
				marker = `"swagger":"2.0"`
			}
			prefix := `{` + marker + `,"paths":{"/pets":{"get":{"parameters":[`
			suffix := `]}}}}`
			report, err := diff.Operations(
				context.Background(),
				diffDocument(t, prefix+test.left+suffix),
				diffDocument(t, prefix+test.right+suffix),
				diff.DefaultOptions(),
			)
			if err != nil {
				t.Fatal(err)
			}
			changes := report.Changes()
			if len(changes) != 1 || changes[0].Kind() != test.kind ||
				changes[0].Classification() != test.classification ||
				!strings.HasSuffix(changes[0].Pointer(), test.pointer) {
				t.Fatalf("changes = %#v", changes)
			}
		})
	}
}

func TestOperationsBoundsParameterContractChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
	}{
		{
			name:  "unchanged requiredness",
			left:  `{"name":"filter","in":"query","style":"form","schema":{"type":"string"}}`,
			right: `{"name":"filter","in":"query","style":"deepObject","schema":{"type":"integer"}}`,
		},
		{
			name:  "changed requiredness",
			left:  `{"name":"filter","in":"query","required":false,"style":"form"}`,
			right: `{"name":"filter","in":"query","required":true,"style":"deepObject"}`,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			prefix := `{"openapi":"3.1.2","paths":{"/pets":{"get":{"parameters":[`
			suffix := `]}}}}`
			options := diff.DefaultOptions()
			options.MaxChanges = 1
			_, err := diff.Operations(
				context.Background(),
				diffDocument(t, prefix+test.left+suffix),
				diffDocument(t, prefix+test.right+suffix),
				options,
			)
			if !errors.Is(err, diff.ErrLimitExceeded) {
				t.Fatalf("limit error = %v", err)
			}
		})
	}
}

func TestOperationsTreatsEffectiveDefaultsAndSchemaOrderAsEqual(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{"openapi":"3.1.2","paths":{"/pets":{"get":{"parameters":[
		{"name":"filter","in":"query","schema":{"type":"string","minLength":1}}
	]}}}}`)
	right := diffDocument(t, `{"openapi":"3.1.2","paths":{"/pets":{"get":{"parameters":[
		{"name":"filter","in":"query","style":"form","explode":true,
		 "allowReserved":false,"schema":{"minLength":1,"type":"string"}}
	]}}}}`)
	report, err := diff.Operations(
		context.Background(), left, right, diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if changes := report.Changes(); len(changes) != 0 {
		t.Fatalf("changes = %#v", changes)
	}

	left = diffDocument(t, `{"swagger":"2.0","paths":{"/pets":{"get":{"parameters":[
		{"name":"filter","in":"query","type":"array"}
	]}}}}`)
	right = diffDocument(t, `{"swagger":"2.0","paths":{"/pets":{"get":{"parameters":[
		{"name":"filter","in":"query","type":"array","collectionFormat":"csv"}
	]}}}}`)
	report, err = diff.Operations(
		context.Background(), left, right, diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if changes := report.Changes(); len(changes) != 0 {
		t.Fatalf("Swagger changes = %#v", changes)
	}
}

func TestOperationsClassifiesDirectionalMediaTypeSchemas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		request        bool
		left           string
		right          string
		kind           diff.Kind
		classification diff.Classification
	}{
		{name: "request schema added", request: true, left: `{}`, right: `{"schema":{"type":"string"}}`, kind: diff.RequestSchemaAdded, classification: diff.Breaking},
		{name: "request schema removed", request: true, left: `{"schema":{"type":"string"}}`, right: `{}`, kind: diff.RequestSchemaRemoved, classification: diff.Compatible},
		{name: "request narrowed", request: true, left: `{"schema":true}`, right: `{"schema":false}`, kind: diff.RequestSchemaChanged, classification: diff.Breaking},
		{name: "request widened", request: true, left: `{"schema":false}`, right: `{"schema":true}`, kind: diff.RequestSchemaChanged, classification: diff.Compatible},
		{name: "request unknown", request: true, left: `{"schema":{"type":"string"}}`, right: `{"schema":{"type":"integer"}}`, kind: diff.RequestSchemaChanged, classification: diff.Unknown},
		{name: "response schema added", left: `{}`, right: `{"schema":{"type":"string"}}`, kind: diff.ResponseSchemaAdded, classification: diff.Compatible},
		{name: "response schema removed", left: `{"schema":{"type":"string"}}`, right: `{}`, kind: diff.ResponseSchemaRemoved, classification: diff.Breaking},
		{name: "response narrowed", left: `{"schema":true}`, right: `{"schema":false}`, kind: diff.ResponseSchemaChanged, classification: diff.Compatible},
		{name: "response widened", left: `{"schema":false}`, right: `{"schema":true}`, kind: diff.ResponseSchemaChanged, classification: diff.Breaking},
		{name: "response unknown", left: `{"schema":{"type":"string"}}`, right: `{"schema":{"type":"integer"}}`, kind: diff.ResponseSchemaChanged, classification: diff.Unknown},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			leftMediaType := test.left
			rightMediaType := test.right
			if test.request {
				leftMediaType = `{}`
				rightMediaType = `{}`
			}
			leftOperation := `"responses":{"200":{"description":"ok","content":{"application/json":` + leftMediaType + `}}}`
			rightOperation := `"responses":{"200":{"description":"ok","content":{"application/json":` + rightMediaType + `}}}`
			if test.request {
				leftOperation = `"requestBody":{"content":{"application/json":` + test.left + `}},` + leftOperation
				rightOperation = `"requestBody":{"content":{"application/json":` + test.right + `}},` + rightOperation
			}
			wrap := func(operation string) string {
				return `{"openapi":"3.2.0","paths":{"/pets":{"post":{` + operation + `}}}}`
			}
			report, err := diff.Operations(
				context.Background(),
				diffDocument(t, wrap(leftOperation)),
				diffDocument(t, wrap(rightOperation)),
				diff.DefaultOptions(),
			)
			if err != nil {
				t.Fatal(err)
			}
			changes := report.Changes()
			if len(changes) != 1 || changes[0].Kind() != test.kind ||
				changes[0].Classification() != test.classification ||
				!strings.HasSuffix(changes[0].Pointer(), "/application~1json/schema") {
				t.Fatalf("changes = %#v", changes)
			}
		})
	}
}

func TestOperationsIgnoresMediaTypeSchemaMemberOrder(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{"openapi":"3.2.0","paths":{"/pets":{"get":{
		"responses":{"200":{"description":"ok","content":{"application/json":{
			"schema":{"type":"string","minLength":1}
		}}}}
	}}}}`)
	right := diffDocument(t, `{"openapi":"3.2.0","paths":{"/pets":{"get":{
		"responses":{"200":{"description":"ok","content":{"application/json":{
			"schema":{"minLength":1,"type":"string"}
		}}}}
	}}}}`)
	report, err := diff.Operations(
		context.Background(), left, right, diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if changes := report.Changes(); len(changes) != 0 {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestOperationsBoundsResponseSchemaChanges(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{"openapi":"3.2.0","paths":{"/pets":{"get":{
		"operationId":"left","responses":{"200":{"description":"ok",
		"content":{"application/json":{"schema":true}}}}
	}}}}`)
	right := diffDocument(t, `{"openapi":"3.2.0","paths":{"/pets":{"get":{
		"operationId":"right","responses":{"200":{"description":"ok",
		"content":{"application/json":{"schema":false}}}}
	}}}}`)
	options := diff.DefaultOptions()
	options.MaxChanges = 1
	if _, err := diff.Operations(
		context.Background(), left, right, options,
	); !errors.Is(err, diff.ErrLimitExceeded) {
		t.Fatalf("limit error = %v", err)
	}
}

func TestOperationsComparesEffectiveSecurityRequirements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		leftRoot       string
		rightRoot      string
		leftOperation  string
		rightOperation string
		kind           diff.Kind
		classification diff.Classification
	}{
		{
			name:           "security required",
			rightOperation: `"security":[{"oauth":["read"]}]`,
			kind:           diff.SecurityRequired, classification: diff.Breaking,
		},
		{
			name:           "security optional",
			leftOperation:  `"security":[{"oauth":["read"]}]`,
			rightOperation: `"security":[]`,
			kind:           diff.SecurityOptional, classification: diff.Compatible,
		},
		{
			name:           "scheme changed",
			leftOperation:  `"security":[{"oauth":["read"]}]`,
			rightOperation: `"security":[{"apiKey":[]}]`,
			kind:           diff.SecurityChanged, classification: diff.Conditional,
		},
		{
			name:           "scope changed",
			leftOperation:  `"security":[{"oauth":["read"]}]`,
			rightOperation: `"security":[{"oauth":["write"]}]`,
			kind:           diff.SecurityChanged, classification: diff.Conditional,
		},
		{
			name:      "inherited security required",
			rightRoot: `"security":[{"oauth":[]}]`,
			kind:      diff.SecurityRequired, classification: diff.Breaking,
		},
		{
			name:           "operation disables inherited security",
			leftRoot:       `"security":[{"oauth":[]}]`,
			rightRoot:      `"security":[{"oauth":[]}]`,
			rightOperation: `"security":[]`,
			kind:           diff.SecurityOptional, classification: diff.Compatible,
		},
		{
			name:           "malformed security",
			leftOperation:  `"security":[]`,
			rightOperation: `"security":false`,
			kind:           diff.SecurityChanged, classification: diff.Unknown,
		},
		{
			name:           "malformed requirement",
			leftOperation:  `"security":[]`,
			rightOperation: `"security":[false]`,
			kind:           diff.SecurityChanged, classification: diff.Unknown,
		},
		{
			name:           "malformed scopes",
			leftOperation:  `"security":[]`,
			rightOperation: `"security":[{"oauth":false}]`,
			kind:           diff.SecurityChanged, classification: diff.Unknown,
		},
		{
			name:           "malformed scope name",
			leftOperation:  `"security":[]`,
			rightOperation: `"security":[{"oauth":[false]}]`,
			kind:           diff.SecurityChanged, classification: diff.Unknown,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := func(extra string, operation string) string {
				if extra != "" {
					extra += `,`
				}
				if operation != "" {
					operation += `,`
				}
				return `{"openapi":"3.2.0",` + extra + `"paths":{"/pets":{"get":{` +
					operation + `"responses":{"200":{"description":"ok"}}}}}}`
			}
			report, err := diff.Operations(
				context.Background(),
				diffDocument(t, root(test.leftRoot, test.leftOperation)),
				diffDocument(t, root(test.rightRoot, test.rightOperation)),
				diff.DefaultOptions(),
			)
			if err != nil {
				t.Fatal(err)
			}
			changes := report.Changes()
			if len(changes) != 1 || changes[0].Kind() != test.kind ||
				changes[0].Classification() != test.classification ||
				changes[0].Pointer() != "/paths/~1pets/get/security" {
				t.Fatalf("changes = %#v", changes)
			}
		})
	}
}

func TestOperationsBoundsSecurityChanges(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{"openapi":"3.2.0","paths":{"/pets":{"get":{
		"operationId":"left","security":[],"responses":{"200":{"description":"ok"}}
	}}}}`)
	right := diffDocument(t, `{"openapi":"3.2.0","paths":{"/pets":{"get":{
		"operationId":"right","security":[{"oauth":[]}],
		"responses":{"200":{"description":"ok"}}
	}}}}`)
	options := diff.DefaultOptions()
	options.MaxChanges = 1
	if _, err := diff.Operations(
		context.Background(), left, right, options,
	); !errors.Is(err, diff.ErrLimitExceeded) {
		t.Fatalf("limit error = %v", err)
	}
}

func TestOperationsNormalizesEquivalentSecurityAlternatives(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
	}{
		{
			name:  "requirements and scopes",
			left:  `[{"oauth":["write","read"]},{"apiKey":[]}]`,
			right: `[{"apiKey":[]},{"oauth":["read","write"]}]`,
		},
		{name: "anonymous forms", left: `[]`, right: `[{}]`},
		{name: "duplicate alternatives", left: `[{"apiKey":[]}]`, right: `[{"apiKey":[]},{"apiKey":[]}]`},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			wrap := func(security string) string {
				return `{"openapi":"3.2.0","paths":{"/pets":{"get":{
					"security":` + security + `,"responses":{"200":{"description":"ok"}}
				}}}}`
			}
			report, err := diff.Operations(
				context.Background(), diffDocument(t, wrap(test.left)),
				diffDocument(t, wrap(test.right)), diff.DefaultOptions(),
			)
			if err != nil {
				t.Fatal(err)
			}
			changes := report.Changes()
			if test.name == "same malformed value" && len(changes) != 0 {
				t.Fatalf("changes = %#v", changes)
			}
			if test.name == "extensions" && (len(changes) != 2 ||
				changes[0].Kind() != diff.ExtensionChanged ||
				changes[0].Pointer() !=
					"/paths/~1pets/post/callbacks/x-owner" ||
				changes[1].Kind() != diff.ExtensionChanged ||
				changes[1].Pointer() !=
					"/paths/~1pets/post/callbacks/event/x-note") {
				t.Fatalf("changes = %#v", changes)
			}
		})
	}
}

func TestOperationsComparesEffectiveServers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		leftRoot       string
		rightRoot      string
		leftPath       string
		rightPath      string
		leftOperation  string
		rightOperation string
		kind           diff.Kind
		classification diff.Classification
	}{
		{
			name:           "server added",
			leftOperation:  `"servers":[{"url":"https://one.example"}]`,
			rightOperation: `"servers":[{"url":"https://one.example"},{"url":"https://two.example"}]`,
			kind:           diff.ServerAdded, classification: diff.Additive,
		},
		{
			name:           "server removed",
			leftOperation:  `"servers":[{"url":"https://one.example"},{"url":"https://two.example"}]`,
			rightOperation: `"servers":[{"url":"https://two.example"}]`,
			kind:           diff.ServerRemoved, classification: diff.Breaking,
		},
		{
			name:           "server variables changed",
			leftOperation:  `"servers":[{"url":"https://{region}.example","variables":{"region":{"default":"eu"}}}]`,
			rightOperation: `"servers":[{"url":"https://{region}.example","variables":{"region":{"default":"us"}}}]`,
			kind:           diff.ServerChanged, classification: diff.Conditional,
		},
		{
			name:           "server order changed",
			leftOperation:  `"servers":[{"url":"https://one.example"},{"url":"https://two.example"}]`,
			rightOperation: `"servers":[{"url":"https://two.example"},{"url":"https://one.example"}]`,
			kind:           diff.ServerOrderChanged, classification: diff.Conditional,
		},
		{
			name:      "root inherited",
			leftRoot:  `"servers":[{"url":"https://one.example"}]`,
			rightRoot: `"servers":[{"url":"https://two.example"}]`,
			kind:      diff.ServerRemoved, classification: diff.Breaking,
		},
		{
			name:      "path override",
			leftRoot:  `"servers":[{"url":"https://root.example"}]`,
			rightRoot: `"servers":[{"url":"https://root.example"}]`,
			leftPath:  `"servers":[{"url":"https://one.example"}]`,
			rightPath: `"servers":[{"url":"https://two.example"}]`,
			kind:      diff.ServerRemoved, classification: diff.Breaking,
		},
		{
			name:           "operation override",
			leftPath:       `"servers":[{"url":"https://path.example"}]`,
			rightPath:      `"servers":[{"url":"https://path.example"}]`,
			leftOperation:  `"servers":[{"url":"https://one.example"}]`,
			rightOperation: `"servers":[{"url":"https://two.example"}]`,
			kind:           diff.ServerRemoved, classification: diff.Breaking,
		},
		{
			name:           "malformed servers",
			leftOperation:  `"servers":[{"url":"https://one.example"}]`,
			rightOperation: `"servers":false`,
			kind:           diff.ServerChanged, classification: diff.Unknown,
		},
		{
			name:           "missing server URL",
			leftOperation:  `"servers":[{"url":"https://one.example"}]`,
			rightOperation: `"servers":[{}]`,
			kind:           diff.ServerChanged, classification: diff.Unknown,
		},
		{
			name:           "duplicate server URL",
			leftOperation:  `"servers":[{"url":"https://one.example"}]`,
			rightOperation: `"servers":[{"url":"https://one.example"},{"url":"https://one.example"}]`,
			kind:           diff.ServerChanged, classification: diff.Unknown,
		},
		{
			name:           "malformed server variables",
			leftOperation:  `"servers":[{"url":"https://one.example"}]`,
			rightOperation: `"servers":[{"url":"https://one.example","variables":false}]`,
			kind:           diff.ServerChanged, classification: diff.Unknown,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			build := func(root, path, operation string) string {
				comma := func(value string) string {
					if value == "" {
						return ""
					}
					return value + `,`
				}
				return `{"openapi":"3.2.0",` + comma(root) +
					`"paths":{"/pets":{` + comma(path) + `"get":{` + comma(operation) +
					`"responses":{"200":{"description":"ok"}}}}}}`
			}
			report, err := diff.Operations(
				context.Background(),
				diffDocument(t, build(test.leftRoot, test.leftPath, test.leftOperation)),
				diffDocument(t, build(test.rightRoot, test.rightPath, test.rightOperation)),
				diff.DefaultOptions(),
			)
			if err != nil {
				t.Fatal(err)
			}
			changes := report.Changes()
			if len(changes) < 1 || changes[0].Kind() != test.kind ||
				changes[0].Classification() != test.classification ||
				!strings.HasPrefix(changes[0].Pointer(), "/paths/~1pets/get/servers") {
				t.Fatalf("changes = %#v", changes)
			}
		})
	}
}

func TestOperationsBoundsServerChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
	}{
		{
			name:  "removed",
			left:  `[{"url":"https://one.example"},{"url":"https://two.example"}]`,
			right: `[{"url":"https://two.example"}]`,
		},
		{
			name:  "changed",
			left:  `[{"url":"https://{region}.example","variables":{"region":{"default":"eu"}}}]`,
			right: `[{"url":"https://{region}.example","variables":{"region":{"default":"us"}}}]`,
		},
		{
			name:  "added",
			left:  `[{"url":"https://one.example"}]`,
			right: `[{"url":"https://one.example"},{"url":"https://two.example"}]`,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			wrap := func(servers string, operationID string) string {
				return `{"openapi":"3.2.0","paths":{"/pets":{"get":{
					"operationId":"` + operationID + `","servers":` + servers + `,
					"responses":{"200":{"description":"ok"}}
				}}}}`
			}
			options := diff.DefaultOptions()
			options.MaxChanges = 1
			_, err := diff.Operations(
				context.Background(), diffDocument(t, wrap(test.left, "left")),
				diffDocument(t, wrap(test.right, "right")), options,
			)
			if !errors.Is(err, diff.ErrLimitExceeded) {
				t.Fatalf("limit error = %v", err)
			}
		})
	}
}

func TestOperationsTreatsImplicitAndExplicitDefaultServerAsEqual(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{"openapi":"3.2.0","paths":{"/pets":{"get":{}}}}`)
	right := diffDocument(t, `{"openapi":"3.2.0","servers":[{"url":"/"}],
		"paths":{"/pets":{"get":{}}}}`)
	report, err := diff.Operations(
		context.Background(), left, right, diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if changes := report.Changes(); len(changes) != 0 {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestOperationsComparesCallbackContracts(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{"openapi":"3.2.0","paths":{"/pets":{"post":{
		"callbacks":{
			"removed":{"{$request.body#/url}":{"post":{}}},
			"shared":{
				"{$request.body#/removed}":{"post":{}},
				"{$request.body#/common}":{"post":{},"delete":{}},
				"{$request.body#/ref}":{"$ref":"#/components/pathItems/One"}
			}
		},"responses":{"200":{"description":"ok"}}
	}}}}`)
	right := diffDocument(t, `{"openapi":"3.2.0","paths":{"/pets":{"post":{
		"callbacks":{
			"shared":{
				"{$request.body#/added}":{"post":{}},
				"{$request.body#/common}":{"post":{},"put":{}},
				"{$request.body#/ref}":{"$ref":"#/components/pathItems/Two"}
			},
			"added":{"{$request.body#/url}":{"post":{}}}
		},"responses":{"200":{"description":"ok"}}
	}}}}`)
	report, err := diff.Operations(
		context.Background(), left, right, diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		kind           diff.Kind
		classification diff.Classification
		pointer        string
	}{
		{diff.CallbackRemoved, diff.Conditional, "/paths/~1pets/post/callbacks/removed"},
		{diff.CallbackExpressionRemoved, diff.Conditional, "/paths/~1pets/post/callbacks/shared/{$request.body#~1removed}"},
		{diff.CallbackOperationRemoved, diff.Conditional, "/paths/~1pets/post/callbacks/shared/{$request.body#~1common}/delete"},
		{diff.CallbackOperationAdded, diff.Conditional, "/paths/~1pets/post/callbacks/shared/{$request.body#~1common}/put"},
		{diff.CallbackExpressionChanged, diff.Unknown, "/paths/~1pets/post/callbacks/shared/{$request.body#~1ref}"},
		{diff.CallbackExpressionAdded, diff.Conditional, "/paths/~1pets/post/callbacks/shared/{$request.body#~1added}"},
		{diff.CallbackAdded, diff.Conditional, "/paths/~1pets/post/callbacks/added"},
	}
	changes := report.Changes()
	if len(changes) != len(want) {
		t.Fatalf("changes = %#v", changes)
	}
	for index, expected := range want {
		if changes[index].Kind() != expected.kind ||
			changes[index].Classification() != expected.classification ||
			changes[index].Pointer() != expected.pointer {
			t.Fatalf("change %d = %#v, want %#v", index, changes[index], expected)
		}
	}
}

func TestOperationsComparesCommonCallbackOperationContent(t *testing.T) {
	t.Parallel()

	wrap := func(operationID string, required bool) string {
		return `{"openapi":"3.2.0","paths":{"/pets":{"post":{
			"callbacks":{"event":{"{$request.body#/url}":{"post":{
				"operationId":"` + operationID + `",
				"requestBody":{"required":` + fmt.Sprint(required) +
			`,"content":{"application/json":{"schema":{"type":"object"}}}},
				"responses":{"204":{"description":"ok"}}
			}}}},"responses":{"204":{"description":"ok"}}
		}}}}`
	}
	left := diffDocument(t, wrap("receiveOld", false))
	right := diffDocument(t, wrap("receiveNew", true))
	report, err := diff.Operations(
		context.Background(), left, right, diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	changes := report.Changes()
	if len(changes) != 2 ||
		changes[0].Kind() != diff.OperationIDChanged ||
		changes[0].Pointer() !=
			"/paths/~1pets/post/callbacks/event/{$request.body#~1url}/post/operationId" ||
		changes[1].Kind() != diff.RequestBodyRequired ||
		changes[1].Classification() != diff.Breaking {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestOperationsBoundsCommonCallbackOperationContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		leftContent  string
		rightContent string
	}{
		{
			name: "parameters",
			leftContent: `"parameters":[
				{"name":"one","in":"query"},{"name":"two","in":"query"}
			],`,
		},
		{
			name: "operation content",
			leftContent: `"operationId":"old",
				"requestBody":{"required":false,"content":{}},`,
			rightContent: `"operationId":"new",
				"requestBody":{"required":true,"content":{}},`,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			wrap := func(content string) string {
				return `{"openapi":"3.2.0","paths":{"/pets":{"post":{
					"callbacks":{"event":{"expression":{"post":{` + content +
					`"responses":{"204":{"description":"ok"}}}}}},
					"responses":{"204":{"description":"ok"}}
				}}}}`
			}
			options := diff.DefaultOptions()
			options.MaxChanges = 1
			_, err := diff.Operations(
				context.Background(), diffDocument(t, wrap(test.leftContent)),
				diffDocument(t, wrap(test.rightContent)), options,
			)
			if !errors.Is(err, diff.ErrLimitExceeded) {
				t.Fatalf("limit error = %v", err)
			}
		})
	}
}

func TestOperationsRetainsAdditionalCallbackOperationPointers(t *testing.T) {
	t.Parallel()

	wrap := func(method string) string {
		return `{"openapi":"3.2.0","paths":{"/pets":{"post":{
			"callbacks":{"event":{"expression":{"additionalOperations":{"` +
			method + `":{"responses":{"204":{"description":"ok"}}}}}}},
			"responses":{"204":{"description":"ok"}}
		}}}}`
	}
	report, err := diff.Operations(
		context.Background(), diffDocument(t, wrap("COPY")),
		diffDocument(t, wrap("MOVE")), diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	changes := report.Changes()
	if len(changes) != 2 ||
		changes[0].Pointer() !=
			"/paths/~1pets/post/callbacks/event/expression/additionalOperations/COPY" ||
		changes[1].Pointer() !=
			"/paths/~1pets/post/callbacks/event/expression/additionalOperations/MOVE" {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestOperationsReportsMalformedCallbackCollections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
	}{
		{name: "callbacks", left: `false`, right: `{}`},
		{name: "callback", left: `{"event":false}`, right: `{"event":{}}`},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			wrap := func(callbacks string) string {
				return `{"openapi":"3.2.0","paths":{"/pets":{"post":{
					"callbacks":` + callbacks + `,"responses":{"200":{"description":"ok"}}
				}}}}`
			}
			report, err := diff.Operations(
				context.Background(), diffDocument(t, wrap(test.left)),
				diffDocument(t, wrap(test.right)), diff.DefaultOptions(),
			)
			if err != nil {
				t.Fatal(err)
			}
			changes := report.Changes()
			if len(changes) != 1 || changes[0].Kind() != diff.CallbackChanged ||
				changes[0].Classification() != diff.Unknown {
				t.Fatalf("changes = %#v", changes)
			}
		})
	}
}

func TestOperationsIgnoresStableMalformedCallbacksAndExtensions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
	}{
		{name: "same malformed value", left: `false`, right: `false`},
		{
			name:  "extensions",
			left:  `{"x-owner":"one","event":{"x-note":"left"}}`,
			right: `{"x-owner":"two","event":{"x-note":"right"}}`,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			wrap := func(callbacks string) string {
				return `{"openapi":"3.2.0","paths":{"/pets":{"post":{
					"callbacks":` + callbacks + `,"responses":{"200":{"description":"ok"}}
				}}}}`
			}
			report, err := diff.Operations(
				context.Background(), diffDocument(t, wrap(test.left)),
				diffDocument(t, wrap(test.right)), diff.DefaultOptions(),
			)
			if err != nil {
				t.Fatal(err)
			}
			changes := report.Changes()
			if test.name == "same malformed value" && len(changes) != 0 {
				t.Fatalf("changes = %#v", changes)
			}
			if test.name == "extensions" && (len(changes) != 2 ||
				changes[0].Kind() != diff.ExtensionChanged ||
				changes[0].Pointer() !=
					"/paths/~1pets/post/callbacks/x-owner" ||
				changes[1].Kind() != diff.ExtensionChanged ||
				changes[1].Pointer() !=
					"/paths/~1pets/post/callbacks/event/x-note") {
				t.Fatalf("changes = %#v", changes)
			}
		})
	}
}

func TestOperationsComparesResponseLinks(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{"openapi":"3.2.0","paths":{"/pets":{"get":{
		"responses":{"200":{"description":"ok","links":{
			"removed":{"operationId":"old"},
			"changed":{"operationId":"old"},
			"reference":{"$ref":"#/components/links/One"},
			"x-owner":"left"
		}}}
	}}}}`)
	right := diffDocument(t, `{"openapi":"3.2.0","paths":{"/pets":{"get":{
		"responses":{"200":{"description":"ok","links":{
			"changed":{"operationId":"new"},
			"reference":{"$ref":"#/components/links/Two"},
			"added":{"operationId":"new"},
			"x-owner":"right"
		}}}
	}}}}`)
	report, err := diff.Operations(
		context.Background(), left, right, diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		kind           diff.Kind
		classification diff.Classification
		pointer        string
	}{
		{diff.ExtensionChanged, diff.Conditional, "/paths/~1pets/get/responses/200/links/x-owner"},
		{diff.LinkRemoved, diff.Conditional, "/paths/~1pets/get/responses/200/links/removed"},
		{diff.LinkChanged, diff.Conditional, "/paths/~1pets/get/responses/200/links/changed"},
		{diff.LinkChanged, diff.Unknown, "/paths/~1pets/get/responses/200/links/reference"},
		{diff.LinkAdded, diff.Additive, "/paths/~1pets/get/responses/200/links/added"},
	}
	changes := report.Changes()
	if len(changes) != len(want) {
		t.Fatalf("changes = %#v", changes)
	}
	for index, expected := range want {
		if changes[index].Kind() != expected.kind ||
			changes[index].Classification() != expected.classification ||
			changes[index].Pointer() != expected.pointer {
			t.Fatalf("change %d = %#v, want %#v", index, changes[index], expected)
		}
	}
}

func TestOperationsReportsMalformedResponseLinks(t *testing.T) {
	t.Parallel()

	wrap := func(links string) string {
		return `{"openapi":"3.2.0","paths":{"/pets":{"get":{
			"responses":{"200":{"description":"ok","links":` + links + `}}
		}}}}`
	}
	report, err := diff.Operations(
		context.Background(), diffDocument(t, wrap(`false`)),
		diffDocument(t, wrap(`{}`)), diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	changes := report.Changes()
	if len(changes) != 1 || changes[0].Kind() != diff.LinkChanged ||
		changes[0].Classification() != diff.Unknown {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestOperationsIgnoresStableResponseLinksAndExtensions(t *testing.T) {
	t.Parallel()

	wrap := func(links string) string {
		return `{"openapi":"3.2.0","paths":{"/pets":{"get":{
			"responses":{"200":{"description":"ok","links":` + links + `}}
		}}}}`
	}
	tests := []struct {
		name  string
		left  string
		right string
	}{
		{name: "same malformed", left: `false`, right: `false`},
		{
			name:  "extensions and member order",
			left:  `{"x-owner":"left","next":{"operationId":"next","x-note":"left"}}`,
			right: `{"next":{"x-note":"right","operationId":"next"},"x-owner":"right"}`,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			report, err := diff.Operations(
				context.Background(), diffDocument(t, wrap(test.left)),
				diffDocument(t, wrap(test.right)), diff.DefaultOptions(),
			)
			if err != nil {
				t.Fatal(err)
			}
			changes := report.Changes()
			if test.name == "same malformed" && len(changes) != 0 {
				t.Fatalf("changes = %#v", changes)
			}
			if test.name == "extensions and member order" &&
				(len(changes) != 2 ||
					changes[0].Kind() != diff.ExtensionChanged ||
					changes[0].Pointer() !=
						"/paths/~1pets/get/responses/200/links/x-owner" ||
					changes[1].Kind() != diff.ExtensionChanged ||
					changes[1].Pointer() !=
						"/paths/~1pets/get/responses/200/links/next/x-note") {
				t.Fatalf("changes = %#v", changes)
			}
		})
	}
}

func TestOperationsBoundsResponseLinkChanges(t *testing.T) {
	t.Parallel()

	wrap := func(operationID string, links string) string {
		return `{"openapi":"3.2.0","paths":{"/pets":{"get":{
			"operationId":"` + operationID + `","responses":{"200":{
				"description":"ok","links":` + links + `
			}}
		}}}}`
	}
	options := diff.DefaultOptions()
	options.MaxChanges = 1
	if _, err := diff.Operations(
		context.Background(),
		diffDocument(t, wrap("left", `{"next":{"operationId":"next"}}`)),
		diffDocument(t, wrap("right", `{}`)), options,
	); !errors.Is(err, diff.ErrLimitExceeded) {
		t.Fatalf("limit error = %v", err)
	}
}

func TestOperationsComparesMediaTypeEncodingAndExamples(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		request        bool
		left           string
		right          string
		kind           diff.Kind
		classification diff.Classification
		pointer        string
	}{
		{
			name: "request encoding", request: true,
			left:           `{"encoding":{"file":{"contentType":"image/png"}}}`,
			right:          `{"encoding":{"file":{"contentType":"image/jpeg"}}}`,
			kind:           diff.RequestEncodingChanged,
			classification: diff.Breaking, pointer: "/encoding",
		},
		{
			name: "malformed request encoding", request: true,
			left: `{"encoding":{}}`, right: `{"encoding":false}`,
			kind:           diff.RequestEncodingChanged,
			classification: diff.Unknown, pointer: "/encoding",
		},
		{
			name:           "response encoding",
			left:           `{"encoding":{"file":{"contentType":"image/png"}}}`,
			right:          `{"encoding":{"file":{"contentType":"image/jpeg"}}}`,
			kind:           diff.ResponseEncodingChanged,
			classification: diff.Breaking, pointer: "/encoding",
		},
		{
			name: "request example", request: true,
			left:           `{"example":{"name":"one"}}`,
			right:          `{"example":{"name":"two"}}`,
			kind:           diff.RequestExampleChanged,
			classification: diff.Conditional, pointer: "/example",
		},
		{
			name:           "response examples",
			left:           `{"examples":{"one":{"value":1}}}`,
			right:          `{"examples":{"one":{"value":2}}}`,
			kind:           diff.ResponseExampleChanged,
			classification: diff.Conditional, pointer: "/examples",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			leftResponseMedia := test.left
			rightResponseMedia := test.right
			if test.request {
				leftResponseMedia = `{}`
				rightResponseMedia = `{}`
			}
			operation := func(requestMedia, responseMedia string) string {
				request := ""
				if test.request {
					request = `"requestBody":{"content":{"application/json":` + requestMedia + `}},`
				}
				return `{"openapi":"3.2.0","paths":{"/pets":{"post":{` + request +
					`"responses":{"200":{"description":"ok","content":{"application/json":` + responseMedia + `}}}` +
					`}}}}`
			}
			report, err := diff.Operations(
				context.Background(),
				diffDocument(t, operation(test.left, leftResponseMedia)),
				diffDocument(t, operation(test.right, rightResponseMedia)),
				diff.DefaultOptions(),
			)
			if err != nil {
				t.Fatal(err)
			}
			changes := report.Changes()
			if len(changes) != 1 || changes[0].Kind() != test.kind ||
				changes[0].Classification() != test.classification ||
				!strings.HasSuffix(changes[0].Pointer(), test.pointer) {
				t.Fatalf("changes = %#v", changes)
			}
		})
	}
}

func TestOperationsIgnoresMediaTypeMetadataMemberOrder(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{"openapi":"3.2.0","paths":{"/pets":{"post":{
		"requestBody":{"content":{"multipart/form-data":{
			"encoding":{"file":{"contentType":"image/png","headers":{}}},
			"examples":{"one":{"value":{"a":1,"b":2}}}
		}}},"responses":{"200":{"description":"ok"}}
	}}}}`)
	right := diffDocument(t, `{"openapi":"3.2.0","paths":{"/pets":{"post":{
		"requestBody":{"content":{"multipart/form-data":{
			"examples":{"one":{"value":{"b":2,"a":1}}},
			"encoding":{"file":{"headers":{},"contentType":"image/png"}}
		}}},"responses":{"200":{"description":"ok"}}
	}}}}`)
	report, err := diff.Operations(
		context.Background(), left, right, diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if changes := report.Changes(); len(changes) != 0 {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestOperationsBoundsMediaTypeMetadataChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
	}{
		{
			name:  "request schema",
			left:  `"requestBody":{"content":{"application/json":{"schema":true}}},`,
			right: `"requestBody":{"content":{"application/json":{"schema":false}}},`,
		},
		{
			name: "response encoding",
			left: ``, right: ``,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			responseMedia := func(name string) string {
				if test.name != "response encoding" {
					return `{}`
				}
				return `{"encoding":{"file":{"contentType":"image/` + name + `"}}}`
			}
			wrap := func(operationID, request string) string {
				return `{"openapi":"3.2.0","paths":{"/pets":{"post":{
					"operationId":"` + operationID + `",` + request +
					`"responses":{"200":{"description":"ok","content":{"application/json":` + responseMedia(operationID) + `}}}
				}}}}`
			}
			options := diff.DefaultOptions()
			options.MaxChanges = 1
			_, err := diff.Operations(
				context.Background(), diffDocument(t, wrap("left", test.left)),
				diffDocument(t, wrap("right", test.right)), options,
			)
			if !errors.Is(err, diff.ErrLimitExceeded) {
				t.Fatalf("limit error = %v", err)
			}
		})
	}
}

func TestOperationsComparesSwaggerEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		left           string
		right          string
		kind           diff.Kind
		classification diff.Classification
		pointer        string
	}{
		{
			name: "scheme removed",
			left: `"schemes":["https","http"]`, right: `"schemes":["https"]`,
			kind:           diff.SwaggerSchemeRemoved,
			classification: diff.Breaking, pointer: "/schemes/1",
		},
		{
			name: "scheme added",
			left: `"schemes":["https"]`, right: `"schemes":["https","wss"]`,
			kind:           diff.SwaggerSchemeAdded,
			classification: diff.Additive, pointer: "/schemes/1",
		},
		{
			name: "relative schemes made explicit",
			left: ``, right: `"schemes":["https"]`,
			kind:           diff.SwaggerSchemesChanged,
			classification: diff.Conditional, pointer: "/schemes",
		},
		{
			name: "malformed schemes",
			left: `"schemes":["https"]`, right: `"schemes":false`,
			kind:           diff.SwaggerSchemesChanged,
			classification: diff.Unknown, pointer: "/schemes",
		},
		{
			name: "malformed scheme element",
			left: `"schemes":["https"]`, right: `"schemes":[false]`,
			kind:           diff.SwaggerSchemesChanged,
			classification: diff.Unknown, pointer: "/schemes",
		},
		{
			name: "host changed",
			left: `"host":"old.example"`, right: `"host":"new.example"`,
			kind:           diff.SwaggerHostChanged,
			classification: diff.Breaking, pointer: "/host",
		},
		{
			name: "relative host made explicit",
			left: ``, right: `"host":"api.example"`,
			kind:           diff.SwaggerHostChanged,
			classification: diff.Conditional, pointer: "/host",
		},
		{
			name: "malformed host",
			left: `"host":"api.example"`, right: `"host":false`,
			kind:           diff.SwaggerHostChanged,
			classification: diff.Unknown, pointer: "/host",
		},
		{
			name: "base path changed",
			left: `"basePath":"/v1"`, right: `"basePath":"/v2"`,
			kind:           diff.SwaggerBasePathChanged,
			classification: diff.Breaking, pointer: "/basePath",
		},
		{
			name: "malformed base path",
			left: `"basePath":"/"`, right: `"basePath":false`,
			kind:           diff.SwaggerBasePathChanged,
			classification: diff.Unknown, pointer: "/basePath",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			wrap := func(endpoint string) string {
				if endpoint != "" {
					endpoint += `,`
				}
				return `{"swagger":"2.0",` + endpoint + `"paths":{}}`
			}
			report, err := diff.Operations(
				context.Background(), diffDocument(t, wrap(test.left)),
				diffDocument(t, wrap(test.right)), diff.DefaultOptions(),
			)
			if err != nil {
				t.Fatal(err)
			}
			changes := report.Changes()
			if len(changes) != 1 || changes[0].Kind() != test.kind ||
				changes[0].Classification() != test.classification ||
				changes[0].Pointer() != test.pointer {
				t.Fatalf("changes = %#v", changes)
			}
		})
	}
}

func TestOperationsNormalizesSwaggerEndpointDefaultsAndSchemeOrder(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{"swagger":"2.0","schemes":["https","http"],"paths":{}}`)
	right := diffDocument(t, `{"swagger":"2.0","basePath":"/","schemes":["http","https"],"paths":{}}`)
	report, err := diff.Operations(
		context.Background(), left, right, diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if changes := report.Changes(); len(changes) != 0 {
		t.Fatalf("changes = %#v", changes)
	}

	left = diffDocument(t, `{"swagger":"2.0","schemes":false,"paths":{}}`)
	right = diffDocument(t, `{"swagger":"2.0","schemes":false,"paths":{}}`)
	report, err = diff.Operations(
		context.Background(), left, right, diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if changes := report.Changes(); len(changes) != 0 {
		t.Fatalf("stable malformed changes = %#v", changes)
	}

	left = diffDocument(t, `{"swagger":"2.0","schemes":["https"],"paths":{}}`)
	right = diffDocument(t, `{"swagger":"2.0","schemes":["https","https"],"paths":{}}`)
	report, err = diff.Operations(
		context.Background(), left, right, diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if changes := report.Changes(); len(changes) != 0 {
		t.Fatalf("duplicate scheme changes = %#v", changes)
	}
}

func TestOperationsBoundsSwaggerEndpointChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
	}{
		{
			name: "scheme removals",
			left: `"schemes":["https","http"]`, right: `"schemes":[]`,
		},
		{
			name: "scheme additions",
			left: `"schemes":[]`, right: `"schemes":["https","http"]`,
		},
		{
			name:  "scheme then host",
			left:  `"host":"old.example"`,
			right: `"schemes":["https"],"host":"new.example"`,
		},
		{
			name:  "host then base path",
			left:  `"host":"old.example","basePath":"/v1"`,
			right: `"host":"new.example","basePath":"/v2"`,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			wrap := func(endpoint string) string {
				return `{"swagger":"2.0",` + endpoint + `,"paths":{}}`
			}
			options := diff.DefaultOptions()
			options.MaxChanges = 1
			_, err := diff.Operations(
				context.Background(), diffDocument(t, wrap(test.left)),
				diffDocument(t, wrap(test.right)), options,
			)
			if !errors.Is(err, diff.ErrLimitExceeded) {
				t.Fatalf("limit error = %v", err)
			}
		})
	}
}

func TestOperationsComparesEffectiveSwaggerMediaTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		leftRoot       string
		rightRoot      string
		leftOperation  string
		rightOperation string
		kind           diff.Kind
		classification diff.Classification
		pointer        string
	}{
		{
			name:           "consumes removed",
			leftOperation:  `"consumes":["application/json","application/xml"]`,
			rightOperation: `"consumes":["application/json"]`,
			kind:           diff.RequestMediaTypeRemoved,
			classification: diff.Breaking, pointer: "/consumes/1",
		},
		{
			name:           "consumes added",
			leftOperation:  `"consumes":["application/json"]`,
			rightOperation: `"consumes":["application/json","application/xml"]`,
			kind:           diff.RequestMediaTypeAdded,
			classification: diff.Additive, pointer: "/consumes/1",
		},
		{
			name:           "produces removed",
			leftOperation:  `"produces":["application/json","application/xml"]`,
			rightOperation: `"produces":["application/json"]`,
			kind:           diff.ResponseMediaTypeRemoved,
			classification: diff.Breaking, pointer: "/produces/1",
		},
		{
			name:           "produces added",
			leftOperation:  `"produces":["application/json"]`,
			rightOperation: `"produces":["application/json","application/xml"]`,
			kind:           diff.ResponseMediaTypeAdded,
			classification: diff.Additive, pointer: "/produces/1",
		},
		{
			name:           "root inherited consumes",
			leftRoot:       `"consumes":["application/json"]`,
			rightRoot:      `"consumes":["application/xml"]`,
			kind:           diff.RequestMediaTypeRemoved,
			classification: diff.Breaking, pointer: "/consumes/0",
		},
		{
			name:           "operation override",
			leftRoot:       `"produces":["application/json"]`,
			rightRoot:      `"produces":["application/json"]`,
			leftOperation:  `"produces":["application/xml"]`,
			rightOperation: `"produces":["text/plain"]`,
			kind:           diff.ResponseMediaTypeRemoved,
			classification: diff.Breaking, pointer: "/produces/0",
		},
		{
			name:           "relative consumes made explicit",
			rightOperation: `"consumes":["application/json"]`,
			kind:           diff.RequestMediaTypesChanged,
			classification: diff.Conditional, pointer: "/consumes",
		},
		{
			name:           "malformed produces",
			leftOperation:  `"produces":["application/json"]`,
			rightOperation: `"produces":false`,
			kind:           diff.ResponseMediaTypesChanged,
			classification: diff.Unknown, pointer: "/produces",
		},
		{
			name:           "malformed consumes element",
			leftOperation:  `"consumes":["application/json"]`,
			rightOperation: `"consumes":[false]`,
			kind:           diff.RequestMediaTypesChanged,
			classification: diff.Unknown, pointer: "/consumes",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			wrap := func(root, operation string) string {
				if root != "" {
					root += `,`
				}
				if operation != "" {
					operation += `,`
				}
				return `{"swagger":"2.0",` + root + `"paths":{"/pets":{"post":{` +
					operation + `"responses":{"200":{"description":"ok"}}}}}}`
			}
			report, err := diff.Operations(
				context.Background(),
				diffDocument(t, wrap(test.leftRoot, test.leftOperation)),
				diffDocument(t, wrap(test.rightRoot, test.rightOperation)),
				diff.DefaultOptions(),
			)
			if err != nil {
				t.Fatal(err)
			}
			changes := report.Changes()
			if len(changes) < 1 || changes[0].Kind() != test.kind ||
				changes[0].Classification() != test.classification ||
				!strings.HasSuffix(changes[0].Pointer(), test.pointer) {
				t.Fatalf("changes = %#v", changes)
			}
		})
	}
}

func TestOperationsNormalizesSwaggerMediaTypeOrderAndDuplicates(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{"swagger":"2.0","paths":{"/pets":{"get":{
		"consumes":["application/json","application/xml"],"produces":["text/plain"]
	}}}}`)
	right := diffDocument(t, `{"swagger":"2.0","paths":{"/pets":{"get":{
		"consumes":["application/xml","application/json","application/json"],
		"produces":["text/plain","text/plain"]
	}}}}`)
	report, err := diff.Operations(
		context.Background(), left, right, diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if changes := report.Changes(); len(changes) != 0 {
		t.Fatalf("changes = %#v", changes)
	}

	left = diffDocument(t, `{"swagger":"2.0","paths":{"/pets":{"get":{"produces":false}}}}`)
	right = diffDocument(t, `{"swagger":"2.0","paths":{"/pets":{"get":{"produces":false}}}}`)
	report, err = diff.Operations(
		context.Background(), left, right, diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if changes := report.Changes(); len(changes) != 0 {
		t.Fatalf("stable malformed changes = %#v", changes)
	}
}

func TestOperationsBoundsSwaggerMediaTypeChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
	}{
		{
			name: "after operation ID",
			left: ``, right: `"consumes":["application/json"],`,
		},
		{
			name:  "removals",
			left:  `"consumes":["application/json","application/xml"],`,
			right: `"consumes":[],`,
		},
		{
			name:  "additions",
			left:  `"consumes":[],`,
			right: `"consumes":["application/json","application/xml"],`,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			wrap := func(operationID, mediaTypes string) string {
				return `{"swagger":"2.0","paths":{"/pets":{"post":{` + mediaTypes +
					`"operationId":"` + operationID + `","responses":{"200":{"description":"ok"}}
				}}}}`
			}
			options := diff.DefaultOptions()
			options.MaxChanges = 1
			_, err := diff.Operations(
				context.Background(), diffDocument(t, wrap("left", test.left)),
				diffDocument(t, wrap("right", test.right)), options,
			)
			if !errors.Is(err, diff.ErrLimitExceeded) {
				t.Fatalf("limit error = %v", err)
			}
		})
	}
}

func TestOperationsComparesDocumentAndOperationTags(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{"openapi":"3.2.0","tags":[
		{"name":"removed","description":"old"},
		{"name":"changed","description":"old"}
	],"paths":{"/pets":{"get":{
		"tags":["removed","shared"],"responses":{"200":{"description":"ok"}}
	}}}}`)
	right := diffDocument(t, `{"openapi":"3.2.0","tags":[
		{"name":"changed","description":"new"},
		{"name":"added","description":"new"}
	],"paths":{"/pets":{"get":{
		"tags":["shared","added"],"responses":{"200":{"description":"ok"}}
	}}}}`)
	report, err := diff.Operations(
		context.Background(), left, right, diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		kind    diff.Kind
		pointer string
	}{
		{diff.TagRemoved, "/tags/0"},
		{diff.TagChanged, "/tags/0"},
		{diff.TagAdded, "/tags/1"},
		{diff.TagRemoved, "/paths/~1pets/get/tags/0"},
		{diff.TagAdded, "/paths/~1pets/get/tags/1"},
	}
	changes := report.Changes()
	if len(changes) != len(want) {
		t.Fatalf("changes = %#v", changes)
	}
	for index, expected := range want {
		if changes[index].Kind() != expected.kind ||
			changes[index].Classification() != diff.Conditional ||
			changes[index].Pointer() != expected.pointer {
			t.Fatalf("change %d = %#v, want %#v", index, changes[index], expected)
		}
	}
}

func TestOperationsNormalizesTagOrderAndExtensions(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{"openapi":"3.2.0","tags":[
		{"name":"one","description":"same","x-owner":"left"},{"name":"two"}
	],"paths":{"/pets":{"get":{"tags":["one","two"]}}}}`)
	right := diffDocument(t, `{"openapi":"3.2.0","tags":[
		{"name":"two"},{"x-owner":"right","description":"same","name":"one"}
	],"paths":{"/pets":{"get":{"tags":["two","one","one"]}}}}`)
	report, err := diff.Operations(
		context.Background(), left, right, diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if changes := report.Changes(); len(changes) != 1 ||
		changes[0].Kind() != diff.ExtensionChanged ||
		changes[0].Pointer() != "/tags/1/x-owner" {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestOperationsReportsMalformedTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
	}{
		{
			name: "document", left: `"tags":false,`, right: `"tags":[],`,
		},
		{
			name: "operation", left: ``, right: ``,
		},
		{
			name: "missing document tag name",
			left: `"tags":[{}],`, right: `"tags":[],`,
		},
		{
			name:  "duplicate document tag",
			left:  `"tags":[{"name":"one"},{"name":"one"}],`,
			right: `"tags":[{"name":"one"}],`,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			operationTags := func(side string) string {
				if test.name != "operation" {
					return ""
				}
				return `"tags":` + side + `,`
			}
			wrap := func(root, operation string) string {
				return `{"openapi":"3.2.0",` + root + `"paths":{"/pets":{"get":{` +
					operation + `"responses":{"200":{"description":"ok"}}}}}}`
			}
			leftRoot, rightRoot := test.left, test.right
			leftOperation, rightOperation := "", ""
			if test.name == "operation" {
				leftRoot, rightRoot = "", ""
				leftOperation = operationTags(`false`)
				rightOperation = operationTags(`[]`)
			}
			report, err := diff.Operations(
				context.Background(), diffDocument(t, wrap(leftRoot, leftOperation)),
				diffDocument(t, wrap(rightRoot, rightOperation)), diff.DefaultOptions(),
			)
			if err != nil {
				t.Fatal(err)
			}
			changes := report.Changes()
			if len(changes) != 1 || changes[0].Kind() != diff.TagChanged ||
				changes[0].Classification() != diff.Unknown {
				t.Fatalf("changes = %#v", changes)
			}
		})
	}
}

func TestOperationsIgnoresStableMalformedTags(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{"openapi":"3.2.0","tags":false,
		"paths":{"/pets":{"get":{"tags":false}}}}`)
	right := diffDocument(t, `{"openapi":"3.2.0","tags":false,
		"paths":{"/pets":{"get":{"tags":false}}}}`)
	report, err := diff.Operations(
		context.Background(), left, right, diff.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if changes := report.Changes(); len(changes) != 0 {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestOperationsBoundsTagChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
	}{
		{
			name:  "document removals",
			left:  `"tags":[{"name":"one"},{"name":"two"}],`,
			right: `"tags":[],`,
		},
		{
			name:  "document changed after removal",
			left:  `"tags":[{"name":"removed"},{"name":"shared","description":"left"}],`,
			right: `"tags":[{"name":"shared","description":"right"}],`,
		},
		{
			name:  "document added after removal",
			left:  `"tags":[{"name":"removed"}],`,
			right: `"tags":[{"name":"added"}],`,
		},
		{
			name: "operation removed",
			left: ``, right: ``,
		},
		{
			name: "operation added",
			left: ``, right: ``,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			operationTags := func(side string) string {
				switch test.name {
				case "operation removed":
					if side == "left" {
						return `"tags":["one"],`
					}
				case "operation added":
					if side == "right" {
						return `"tags":["one"],`
					}
				}
				return ""
			}
			wrap := func(root, side string) string {
				return `{"openapi":"3.2.0",` + root + `"paths":{"/pets":{"get":{` +
					operationTags(side) + `"operationId":"` + side + `"}}}}`
			}
			options := diff.DefaultOptions()
			options.MaxChanges = 1
			_, err := diff.Operations(
				context.Background(), diffDocument(t, wrap(test.left, "left")),
				diffDocument(t, wrap(test.right, "right")), options,
			)
			if !errors.Is(err, diff.ErrLimitExceeded) {
				t.Fatalf("limit error = %v", err)
			}
		})
	}
}

func TestOperationsBoundsCallbackChanges(t *testing.T) {
	t.Parallel()

	left := diffDocument(t, `{"openapi":"3.2.0","paths":{"/pets":{"post":{
		"operationId":"left","callbacks":{"event":{"expression":{"post":{}}}},
		"responses":{"200":{"description":"ok"}}
	}}}}`)
	right := diffDocument(t, `{"openapi":"3.2.0","paths":{"/pets":{"post":{
		"operationId":"right","callbacks":{},"responses":{"200":{"description":"ok"}}
	}}}}`)
	options := diff.DefaultOptions()
	options.MaxChanges = 1
	if _, err := diff.Operations(
		context.Background(), left, right, options,
	); !errors.Is(err, diff.ErrLimitExceeded) {
		t.Fatalf("limit error = %v", err)
	}
}

type cancelAfterInitialCheck struct {
	context.Context
	calls int
}

func (ctx *cancelAfterInitialCheck) Err() error {
	ctx.calls++
	if ctx.calls > 1 {
		return context.Canceled
	}
	return nil
}

func diffDocument(t *testing.T, raw string) openapi.Document {
	t.Helper()
	document, err := openapi.ParseJSON(
		context.Background(),
		strings.NewReader(raw),
		parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func mustRawDocument(t *testing.T, document openapi.Document) string {
	t.Helper()
	raw, err := document.Raw().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
