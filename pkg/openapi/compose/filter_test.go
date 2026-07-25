package compose_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/compose"
	"github.com/faustbrian/golib/pkg/openapi/parse"
)

func TestFilterOperationsTraversesEveryOwnedPathItem(t *testing.T) {
	t.Parallel()

	document := composeDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/pets":{
			"parameters":[],"get":{"responses":{}},"post":{
				"callbacks":{"done":{"{$request.body#/url}":{
					"get":{"responses":{}},"delete":{"responses":{}}
				}}}
			},
			"additionalOperations":{"COPY":{"responses":{}},"MOVE":{"responses":{}}}
		}},
		"webhooks":{"event":{"get":{"responses":{}},"post":{"responses":{}}}},
		"components":{
			"pathItems":{"Shared":{"get":{"responses":{}},"put":{"responses":{}}}},
			"callbacks":{"Shared":{"expression":{"get":{"responses":{}},"patch":{"responses":{}}}}}
		},
		"x-root":true
	}`)
	result, err := compose.FilterOperations(
		context.Background(),
		document,
		func(operation compose.Operation) (bool, error) {
			return operation.Method() == "get", nil
		},
		compose.DefaultFilterOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	wantRemoved := []string{
		"/paths/~1pets/post",
		"/paths/~1pets/additionalOperations/COPY",
		"/paths/~1pets/additionalOperations/MOVE",
		"/webhooks/event/post",
		"/components/pathItems/Shared/put",
		"/components/callbacks/Shared/expression/patch",
	}
	removed := result.Removed()
	if len(removed) != len(wantRemoved) {
		t.Fatalf("removed = %#v", removed)
	}
	for index, pointer := range wantRemoved {
		if removed[index].Pointer() != pointer {
			t.Fatalf("removed %d pointer = %q, want %q", index, removed[index].Pointer(), pointer)
		}
		if removed[index].Source().Kind() == 0 {
			t.Fatalf("removed %d lost source value", index)
		}
	}
	encoded, err := json.Marshal(result.Document().Raw())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{"/pets":{"parameters":[],"get":{"responses":{}},"additionalOperations":{}}},"webhooks":{"event":{"get":{"responses":{}}}},"components":{"pathItems":{"Shared":{"get":{"responses":{}}}},"callbacks":{"Shared":{"expression":{"get":{"responses":{}}}}}},"x-root":true}`
	if string(encoded) != want {
		t.Fatalf("filtered document = %s\nwant = %s", encoded, want)
	}
	removed[0] = compose.Operation{}
	if result.Removed()[0].Pointer() == "" {
		t.Fatal("result exposed mutable removal storage")
	}
	original, _ := json.Marshal(document.Raw())
	if !strings.Contains(string(original), `"post"`) {
		t.Fatal("filter mutated the source document")
	}
}

func TestFilterOperationsSupportsAuthorizationPolicies(t *testing.T) {
	t.Parallel()

	document := composeDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/pets":{
			"get":{"security":[],"responses":{"200":{"description":"ok"}}},
			"post":{"security":[{"admin":[]}],
				"responses":{"204":{"description":"ok"}}}
		}}
	}`)
	result, err := compose.FilterOperations(
		context.Background(),
		document,
		func(operation compose.Operation) (bool, error) {
			security, protected := operation.Source().Lookup("security")
			if !protected {
				return true, nil
			}
			requirements, valid := security.Elements()
			return valid && len(requirements) == 0, nil
		},
		compose.DefaultFilterOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	removed := result.Removed()
	if len(removed) != 1 || removed[0].Pointer() != "/paths/~1pets/post" {
		t.Fatalf("removed operations = %#v", removed)
	}
	encoded, err := json.Marshal(result.Document().Raw())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"post"`) ||
		!strings.Contains(string(encoded), `"get"`) {
		t.Fatalf("authorization-filtered document = %s", encoded)
	}
}

func TestFilterOperationsUsesSpecificationMethodsOnly(t *testing.T) {
	t.Parallel()

	document := composeDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"post":{},"trace":{},"query":{},
			"additionalOperations":{"COPY":{}},"x-operation":{}}},
		"webhooks":{"event":{"post":{}}},
		"components":{"pathItems":{"Shared":{"post":{}}}}
	}`)
	result, err := compose.FilterOperations(
		context.Background(), document,
		func(compose.Operation) (bool, error) { return false, nil },
		compose.FilterOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if removed := result.Removed(); len(removed) != 1 || removed[0].Pointer() != "/paths/~1pets/post" {
		t.Fatalf("removed = %#v", removed)
	}
	encoded, _ := json.Marshal(result.Document().Raw())
	for _, retained := range []string{
		`"trace"`, `"query"`, `"additionalOperations"`, `"x-operation"`,
		`"webhooks"`, `"components"`,
	} {
		if !strings.Contains(string(encoded), retained) {
			t.Fatalf("filtered document lost non-Swagger member %s: %s", retained, encoded)
		}
	}
}

func TestFilterOperationsHonorsComponentSurfacesByDialect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		version        string
		wantRemoved    string
		wantPathItemOp bool
		wantWebhookOp  bool
	}{
		{name: "OpenAPI 3.0 callbacks only", version: "3.0.4", wantRemoved: "/components/callbacks/Shared/expression/post"},
		{name: "OpenAPI 3.1 full surfaces", version: "3.1.2", wantRemoved: "/webhooks/event/post", wantPathItemOp: true, wantWebhookOp: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			document := composeDocument(t, `{
				"openapi":"`+test.version+`","info":{"title":"API","version":"1"},
				"paths":{},"webhooks":{"event":{"post":{}}},
				"components":{
					"pathItems":{"Shared":{"post":{}}},
					"callbacks":{"Shared":{"expression":{"post":{}}}}
				}
			}`)
			result, err := compose.FilterOperations(
				context.Background(), document,
				func(compose.Operation) (bool, error) { return false, nil },
				compose.DefaultFilterOptions(),
			)
			if err != nil {
				t.Fatal(err)
			}
			removed := result.Removed()
			wantCount := 1
			if test.wantPathItemOp {
				wantCount++
			}
			if test.wantWebhookOp {
				wantCount++
			}
			if len(removed) != wantCount || removed[0].Pointer() != test.wantRemoved {
				t.Fatalf("removed = %#v", removed)
			}
			encoded, _ := json.Marshal(result.Document().Raw())
			if !test.wantWebhookOp && !strings.Contains(string(encoded), `"webhooks":{"event":{"post":{}}}`) {
				t.Fatalf("dialect-invalid webhooks were filtered: %s", encoded)
			}
			if !test.wantPathItemOp && !strings.Contains(string(encoded), `"pathItems":{"Shared":{"post":{}}}`) {
				t.Fatalf("OAS 3.0 pathItems were filtered: %s", encoded)
			}
		})
	}
}

func TestFilterOperationsRejectsInvalidInputsAndBoundsWork(t *testing.T) {
	t.Parallel()

	document := composeDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{},"post":{}}}
	}`)
	predicate := func(compose.Operation) (bool, error) { return true, nil }
	//lint:ignore SA1012 This assertion verifies the nil-context contract.
	//nolint:staticcheck // This assertion verifies the nil-context contract.
	if _, err := compose.FilterOperations(nil, document, predicate, compose.DefaultFilterOptions()); !errors.Is(err, compose.ErrInvalidInput) {
		t.Fatalf("nil context error = %v", err)
	}
	if _, err := compose.FilterOperations(context.Background(), nil, predicate, compose.DefaultFilterOptions()); !errors.Is(err, compose.ErrInvalidInput) {
		t.Fatalf("nil document error = %v", err)
	}
	if _, err := compose.FilterOperations(context.Background(), document, nil, compose.DefaultFilterOptions()); !errors.Is(err, compose.ErrInvalidInput) {
		t.Fatalf("nil predicate error = %v", err)
	}
	options := compose.DefaultFilterOptions()
	options.MaxOperations = -1
	if _, err := compose.FilterOperations(context.Background(), document, predicate, options); !errors.Is(err, compose.ErrInvalidOptions) {
		t.Fatalf("invalid options error = %v", err)
	}
	options = compose.DefaultFilterOptions()
	options.MaxDepth = -1
	if _, err := compose.FilterOperations(context.Background(), document, predicate, options); !errors.Is(err, compose.ErrInvalidOptions) {
		t.Fatalf("invalid depth error = %v", err)
	}
	options = compose.DefaultFilterOptions()
	options.MaxOperations = 1
	if _, err := compose.FilterOperations(context.Background(), document, predicate, options); !errors.Is(err, compose.ErrLimitExceeded) {
		t.Fatalf("operation limit error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := compose.FilterOperations(ctx, document, predicate, compose.DefaultFilterOptions()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
	injected := errors.New("policy failed")
	if _, err := compose.FilterOperations(
		context.Background(), document,
		func(compose.Operation) (bool, error) { return false, injected },
		compose.DefaultFilterOptions(),
	); !errors.Is(err, injected) {
		t.Fatalf("predicate error = %v", err)
	}
	options = compose.DefaultFilterOptions()
	options.MaxDepth = 1
	if _, err := compose.FilterOperations(context.Background(), document, predicate, options); !errors.Is(err, compose.ErrLimitExceeded) {
		t.Fatalf("depth limit error = %v", err)
	}
}

func TestFilterOperationsAcceptsExactOperationAndDepthLimits(t *testing.T) {
	t.Parallel()

	predicate := func(compose.Operation) (bool, error) { return true, nil }
	one := composeDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{}}}
	}`)
	options := compose.DefaultFilterOptions()
	options.MaxOperations = 1
	if _, err := compose.FilterOperations(
		context.Background(), one, predicate, options,
	); err != nil {
		t.Fatalf("exact operation limit error = %v", err)
	}
	empty := composeDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{}
	}`)
	options.MaxDepth = 2
	if _, err := compose.FilterOperations(
		context.Background(), empty, predicate, options,
	); err != nil {
		t.Fatalf("exact depth limit error = %v", err)
	}
}

func TestFilterOperationsPreservesNonOperationAndReferenceMembers(t *testing.T) {
	t.Parallel()

	raw := `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{
			"x-path":{"get":{}},"not-a-path":{"get":{}},"/scalar":"value",
			"/pets":{"get":"invalid","post":{
				"callbacks":{
					"x-callback":{"expression":{"delete":{}}},
					"scalar":"invalid",
					"reference":{"$ref":"#/components/callbacks/Shared"},
					"actual":{"scalar":"invalid","expression":{"get":{}}}
				}
			},"additionalOperations":{"INVALID":"value","COPY":{}}}
		},
		"webhooks":"invalid",
		"components":{"schemas":{},"callbacks":"invalid"}
	}`
	document := composeDocument(t, raw)
	var pointers []string
	result, err := compose.FilterOperations(
		context.Background(), document,
		func(operation compose.Operation) (bool, error) {
			pointers = append(pointers, operation.Pointer())
			return true, nil
		},
		compose.DefaultFilterOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	wantPointers := []string{
		"/paths/~1pets/post",
		"/paths/~1pets/post/callbacks/actual/expression/get",
		"/paths/~1pets/additionalOperations/COPY",
	}
	if strings.Join(pointers, "\n") != strings.Join(wantPointers, "\n") {
		t.Fatalf("visited pointers = %#v, want %#v", pointers, wantPointers)
	}
	before, _ := json.Marshal(document.Raw())
	after, _ := json.Marshal(result.Document().Raw())
	if string(after) != string(before) {
		t.Fatalf("keep-all changed source semantics:\n%s\n%s", before, after)
	}
}

func TestFilterOperationsObservesCancellationDuringTraversal(t *testing.T) {
	t.Parallel()

	document := composeDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{},"post":{}}}
	}`)
	ctx := &cancelComposeContext{Context: context.Background(), allowed: 4}
	_, err := compose.FilterOperations(
		ctx, document,
		func(compose.Operation) (bool, error) { return true, nil },
		compose.DefaultFilterOptions(),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("mid-traversal cancellation error = %v", err)
	}
}

func TestFilterOperationsEnforcesDepthAtNestedSurfaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		raw      string
		maxDepth int
	}{
		{
			name:     "path item",
			raw:      `{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{"/pets":{}}}`,
			maxDepth: 2,
		},
		{
			name:     "standard operation",
			raw:      `{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{"/pets":{"get":{}}}}`,
			maxDepth: 3,
		},
		{
			name:     "additional operations",
			raw:      `{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{"/pets":{"additionalOperations":{}}}}`,
			maxDepth: 3,
		},
		{
			name:     "operation callbacks",
			raw:      `{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{"/pets":{"get":{"callbacks":{}}}}}`,
			maxDepth: 4,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			options := compose.DefaultFilterOptions()
			options.MaxDepth = test.maxDepth
			_, err := compose.FilterOperations(
				context.Background(), composeDocument(t, test.raw),
				func(compose.Operation) (bool, error) { return true, nil },
				options,
			)
			if !errors.Is(err, compose.ErrLimitExceeded) {
				t.Fatalf("depth error = %v", err)
			}
		})
	}
}

func TestFilterOperationsPropagatesAdditionalOperationPolicyFailure(t *testing.T) {
	t.Parallel()

	document := composeDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"additionalOperations":{"COPY":{}}}}
	}`)
	injected := errors.New("additional policy failed")
	_, err := compose.FilterOperations(
		context.Background(), document,
		func(compose.Operation) (bool, error) { return false, injected },
		compose.DefaultFilterOptions(),
	)
	if !errors.Is(err, injected) {
		t.Fatalf("additional operation error = %v", err)
	}
}

func TestFilterOperationsObservesCancellationInsidePathItem(t *testing.T) {
	t.Parallel()

	document := composeDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{}}}
	}`)
	ctx := &cancelComposeContext{Context: context.Background(), allowed: 6}
	_, err := compose.FilterOperations(
		ctx, document,
		func(compose.Operation) (bool, error) { return true, nil },
		compose.DefaultFilterOptions(),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("path item cancellation error = %v", err)
	}
}

func TestFilterOperationsPropagatesCancellationAtEveryTraversalBoundary(t *testing.T) {
	t.Parallel()

	document := composeDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/pets":{"get":{},"post":{}}},
		"webhooks":{"event":{"get":{}}}
	}`)
	for allowed := 1; allowed <= 40; allowed++ {
		ctx := &cancelComposeContext{Context: context.Background(), allowed: allowed}
		_, err := compose.FilterOperations(
			ctx, document,
			func(compose.Operation) (bool, error) { return true, nil },
			compose.DefaultFilterOptions(),
		)
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("boundary %d error = %v", allowed, err)
		}
	}
}

type cancelComposeContext struct {
	context.Context
	calls   int
	allowed int
}

func (ctx *cancelComposeContext) Err() error {
	ctx.calls++
	if ctx.calls > ctx.allowed {
		return context.Canceled
	}
	return nil
}

func composeDocument(t *testing.T, raw string) openapi.Document {
	t.Helper()
	document, err := openapi.ParseJSON(
		context.Background(), strings.NewReader(raw), parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return document
}
