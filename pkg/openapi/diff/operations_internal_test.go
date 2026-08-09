package diff

import (
	"context"
	"errors"
	"runtime"
	"strconv"
	"strings"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

func TestDiffTraversalRejectsWideValuesBeforeCopyingChildren(t *testing.T) {
	members := make([]jsonvalue.Member, 4096)
	for index := range members {
		members[index] = jsonvalue.Member{
			Name: "x-wide-" + strconv.Itoa(index), Value: jsonvalue.Null(),
		}
	}
	wide, _ := jsonvalue.Object(members)
	tests := []struct {
		name string
		run  func() error
	}{
		{name: "input bound", run: func() error {
			return boundValue(context.Background(), wide, 1, 2)
		}},
		{name: "resolved bound", run: func() error {
			collector := changeCollector{
				ctx: context.Background(), remainingResolvedNodes: 1,
				referenceLimits: reference.Limits{MaxTraversalDepth: 2},
			}
			return collector.consumeResolvedValue(wide)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const repetitions = 16
			runtime.GC()
			var before runtime.MemStats
			runtime.ReadMemStats(&before)
			for range repetitions {
				if err := test.run(); !errors.Is(err, ErrLimitExceeded) {
					t.Fatalf("wide traversal error = %v", err)
				}
			}
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			allocated := (after.TotalAlloc - before.TotalAlloc) / repetitions
			if allocated > 64<<10 {
				t.Fatalf("wide rejected traversal allocated %d bytes per operation", allocated)
			}
		})
	}
}

func TestDiffChildrenFitExactBudgets(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		children int
		visited  int
		queued   int
		depth    int
		maxNodes int
		want     bool
	}{
		{name: "leaf at depth limit", depth: 3, maxNodes: 6, want: true},
		{name: "exact remaining nodes", children: 2, visited: 1, queued: 2, depth: 2, maxNodes: 5, want: true},
		{name: "node overflow", children: 3, visited: 1, queued: 2, depth: 2, maxNodes: 5},
		{name: "visited exhausted", children: 1, visited: 5, depth: 2, maxNodes: 5},
		{name: "queue exhausted", children: 1, visited: 1, queued: 4, depth: 2, maxNodes: 5},
		{name: "queue overflow", children: 1, visited: 1, queued: 5, depth: 2, maxNodes: 5},
		{name: "exact depth", children: 1, depth: 3, maxNodes: 6},
	} {
		if got := diffChildrenFit(
			test.children, test.visited, test.queued, test.depth,
			test.maxNodes, 3,
		); got != test.want {
			t.Fatalf("%s fit = %t, want %t", test.name, got, test.want)
		}
	}
}

func TestChangeCollectorObservesCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	collector := changeCollector{ctx: ctx, maximum: 1}
	if err := collector.append(Change{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("append() error = %v", err)
	}
}

func TestSemanticValueEqualCoversEveryJSONKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
		want  bool
	}{
		{name: "null", left: `null`, right: `null`, want: true},
		{name: "kind", left: `null`, right: `false`, want: false},
		{name: "boolean equal", left: `true`, right: `true`, want: true},
		{name: "boolean unequal", left: `true`, right: `false`, want: false},
		{name: "number equal", left: `1.0`, right: `1.0`, want: true},
		{name: "number unequal", left: `1`, right: `1.0`, want: false},
		{name: "string equal", left: `"a"`, right: `"a"`, want: true},
		{name: "string unequal", left: `"a"`, right: `"b"`, want: false},
		{name: "array equal", left: `[null,true]`, right: `[null,true]`, want: true},
		{name: "array length", left: `[null]`, right: `[]`, want: false},
		{name: "object order", left: `{"a":1,"b":2}`, right: `{"b":2,"a":1}`, want: true},
		{name: "object length", left: `{"a":1}`, right: `{}`, want: false},
		{name: "object name", left: `{"a":1}`, right: `{"b":1}`, want: false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := semanticValueEqual(testValue(t, test.left), testValue(t, test.right))
			if got != test.want {
				t.Fatalf("semanticValueEqual() = %t, want %t", got, test.want)
			}
		})
	}
	if semanticValueEqual(jsonvalue.Value{}, jsonvalue.Value{}) {
		t.Fatal("zero values compared equal")
	}
}

func TestParameterContractPropagatesCollectorLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		left    string
		right   string
		dialect openapi.Dialect
	}{
		{
			name: "style", left: `{"name":"p","in":"query"}`,
			right:   `{"name":"p","in":"query","style":"deepObject"}`,
			dialect: openapi.DialectOAS31,
		},
		{
			name: "explode", left: `{"name":"p","in":"query"}`,
			right:   `{"name":"p","in":"query","explode":false}`,
			dialect: openapi.DialectOAS31,
		},
		{
			name: "allow reserved", left: `{"name":"p","in":"query"}`,
			right:   `{"name":"p","in":"query","allowReserved":true}`,
			dialect: openapi.DialectOAS31,
		},
		{
			name: "schema", left: `{"name":"p","in":"query"}`,
			right:   `{"name":"p","in":"query","schema":{}}`,
			dialect: openapi.DialectOAS31,
		},
		{
			name: "Swagger collection", left: `{"name":"p","in":"query"}`,
			right:   `{"name":"p","in":"query","collectionFormat":"multi"}`,
			dialect: openapi.DialectSwagger20,
		},
		{
			name: "Swagger allow empty", left: `{"name":"p","in":"query"}`,
			right:   `{"name":"p","in":"query","allowEmptyValue":true}`,
			dialect: openapi.DialectSwagger20,
		},
		{
			name: "Swagger default", left: `{"name":"p","in":"query"}`,
			right:   `{"name":"p","in":"query","default":1}`,
			dialect: openapi.DialectSwagger20,
		},
		{
			name: "content", left: `{"name":"p","in":"query"}`,
			right:   `{"name":"p","in":"query","content":{}}`,
			dialect: openapi.DialectOAS31,
		},
		{
			name: "example", left: `{"name":"p","in":"query"}`,
			right:   `{"name":"p","in":"query","example":1}`,
			dialect: openapi.DialectOAS31,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			collector := changeCollector{ctx: context.Background(), maximum: 0}
			left := parameterEntry{
				value: testValue(t, test.left), location: "query", pointer: "/left",
			}
			right := parameterEntry{
				value: testValue(t, test.right), location: "query", pointer: "/right",
			}
			if err := compareParameterContract(
				&collector, left, right, test.dialect,
			); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("contract error = %v", err)
			}
		})
	}
}

func TestCallbackComparisonsPropagateCollectorLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(*changeCollector) error
	}{
		{
			name: "callback removed",
			run: func(collector *changeCollector) error {
				return compareCallbacks(
					collector,
					testValue(t, `{"callbacks":{"event":{}}}`),
					testValue(t, `{"callbacks":{}}`), "/operation",
					openapi.DialectOAS32,
				)
			},
		},
		{
			name: "callback changed",
			run: func(collector *changeCollector) error {
				return compareCallbacks(
					collector,
					testValue(t, `{"callbacks":{"event":{"$ref":"#/one"}}}`),
					testValue(t, `{"callbacks":{"event":{"$ref":"#/two"}}}`),
					"/operation", openapi.DialectOAS32,
				)
			},
		},
		{
			name: "callback expression",
			run: func(collector *changeCollector) error {
				return compareCallbacks(
					collector,
					testValue(t, `{"callbacks":{"event":{"one":{}}}}`),
					testValue(t, `{"callbacks":{"event":{}}}`), "/operation",
					openapi.DialectOAS32,
				)
			},
		},
		{
			name: "callback added",
			run: func(collector *changeCollector) error {
				return compareCallbacks(
					collector, testValue(t, `{"callbacks":{}}`),
					testValue(t, `{"callbacks":{"event":{}}}`), "/operation",
					openapi.DialectOAS32,
				)
			},
		},
		{
			name: "expression changed",
			run: func(collector *changeCollector) error {
				return compareCallbackExpressions(
					collector, testValue(t, `{"one":{"$ref":"#/one"}}`),
					testValue(t, `{"one":{"$ref":"#/two"}}`), "/callback",
					openapi.DialectOAS32,
				)
			},
		},
		{
			name: "operation comparison",
			run: func(collector *changeCollector) error {
				return compareCallbackExpressions(
					collector, testValue(t, `{"one":{"get":{}}}`),
					testValue(t, `{"one":{}}`), "/callback",
					openapi.DialectOAS32,
				)
			},
		},
		{
			name: "expression added",
			run: func(collector *changeCollector) error {
				return compareCallbackExpressions(
					collector, testValue(t, `{}`), testValue(t, `{"one":{}}`),
					"/callback", openapi.DialectOAS32,
				)
			},
		},
		{
			name: "operation removed",
			run: func(collector *changeCollector) error {
				return compareCallbackOperations(
					collector, testValue(t, `{"get":{}}`), testValue(t, `{}`),
					"/expression", openapi.DialectOAS32,
				)
			},
		},
		{
			name: "operation added",
			run: func(collector *changeCollector) error {
				return compareCallbackOperations(
					collector, testValue(t, `{}`), testValue(t, `{"get":{}}`),
					"/expression", openapi.DialectOAS32,
				)
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			collector := changeCollector{ctx: context.Background(), maximum: 0}
			if err := test.run(&collector); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("comparison error = %v", err)
			}
		})
	}
}

func TestAsymmetricResolutionChangesPropagateCollectorLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(*changeCollector) error
	}{
		{
			name: "callback",
			run: func(collector *changeCollector) error {
				return compareCallbacks(
					collector,
					testValue(t, `{"callbacks":{"event":{}}}`),
					testValue(t, `{"callbacks":{"event":{"$ref":"#/missing"}}}`),
					"/operation", openapi.DialectOAS32,
				)
			},
		},
		{
			name: "callback expression",
			run: func(collector *changeCollector) error {
				return compareCallbackExpressions(
					collector,
					testValue(t, `{"event":{}}`),
					testValue(t, `{"event":{"$ref":"#/missing"}}`),
					"/callback", openapi.DialectOAS32,
				)
			},
		},
		{
			name: "response",
			run: func(collector *changeCollector) error {
				empty := testValue(t, `{}`)
				return compareResponses(
					collector, empty, empty,
					testValue(t, `{"responses":{"200":{}}}`),
					testValue(t, `{"responses":{"200":{"$ref":"#/missing"}}}`),
					"/operation", openapi.DialectOAS32,
				)
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			collector := changeCollector{ctx: context.Background(), maximum: 0}
			if err := test.run(&collector); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("asymmetric resolution change error = %v", err)
			}
		})
	}
}

func TestResponseLinkComparisonPropagatesCollectorLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
	}{
		{name: "removed", left: `{"links":{"next":{}}}`, right: `{"links":{}}`},
		{name: "changed", left: `{"links":{"next":{"operationId":"one"}}}`, right: `{"links":{"next":{"operationId":"two"}}}`},
		{name: "added", left: `{"links":{}}`, right: `{"links":{"next":{}}}`},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			collector := changeCollector{ctx: context.Background(), maximum: 0}
			if err := compareResponseLinks(
				&collector, testValue(t, test.left), testValue(t, test.right),
				"/response",
			); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("comparison error = %v", err)
			}
		})
	}
}

func TestEqualObjectWithoutExtensionsDefensiveCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
		want  bool
	}{
		{name: "invalid", left: `false`, right: `{}`, want: false},
		{name: "length", left: `{"a":1}`, right: `{}`, want: false},
		{name: "name", left: `{"a":1}`, right: `{"b":1}`, want: false},
		{name: "value", left: `{"a":1}`, right: `{"a":2}`, want: false},
		{name: "equal", left: `{"a":1,"x-one":true}`, right: `{"x-one":false,"a":1}`, want: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := equalObjectWithoutExtensions(
				testValue(t, test.left), testValue(t, test.right),
			)
			if got != test.want {
				t.Fatalf("equalObjectWithoutExtensions() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestMediaTypeMetadataComparisonPropagatesCollectorLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
	}{
		{
			name: "encoding", left: `{"encoding":{}}`,
			right: `{"encoding":{"value":{}}}`,
		},
		{name: "example", left: `{"example":1}`, right: `{"example":2}`},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			collector := changeCollector{ctx: context.Background(), maximum: 0}
			left := []jsonvalue.Member{{Name: "application/json", Value: testValue(t, test.left)}}
			right := []jsonvalue.Member{{Name: "application/json", Value: testValue(t, test.right)}}
			if err := compareCommonMediaTypeMetadata(
				&collector, left, right, "/content", schemaInRequest,
			); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("comparison error = %v", err)
			}
		})
	}
}

func TestExtensionComparisonsPropagateCollectorLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(*changeCollector) error
	}{
		{
			name: "changed",
			run: func(collector *changeCollector) error {
				return compareExtensions(
					collector, testValue(t, `{"x-value":1}`),
					testValue(t, `{"x-value":2}`), "",
				)
			},
		},
		{
			name: "added",
			run: func(collector *changeCollector) error {
				return compareExtensions(
					collector, testValue(t, `{}`),
					testValue(t, `{"x-value":2}`), "",
				)
			},
		},
		{
			name: "container",
			run: func(collector *changeCollector) error {
				return compareCommonContainerExtensions(
					collector,
					[]jsonvalue.Member{{Name: "value", Value: testValue(t, `{"x-value":1}`)}},
					[]jsonvalue.Member{{Name: "value", Value: testValue(t, `{"x-value":2}`)}},
					"/values",
				)
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			collector := changeCollector{ctx: context.Background(), maximum: 0}
			if err := test.run(&collector); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("comparison error = %v", err)
			}
		})
	}
}

func TestResolvedComparisonDefensiveEquality(t *testing.T) {
	t.Parallel()

	collector := changeCollector{ctx: context.Background(), maximum: 8}
	empty := testValue(t, `{}`)
	invalid := testValue(t, `{"schema":1}`)
	if !equalResolvedSchemaMember(
		&collector, empty, empty, invalid, invalid, "schema", openapi.DialectOAS31,
	) {
		t.Fatal("equal invalid schema members differed")
	}
	if equalResolvedSchemaMember(
		&collector, empty, empty, invalid, empty, "schema", openapi.DialectOAS31,
	) {
		t.Fatal("present and absent schema members compared equal")
	}
	if !equalResolvedSchemaMember(
		&collector, empty, empty, empty, empty, "schema", openapi.DialectOAS31,
	) {
		t.Fatal("absent schema members differed")
	}
	valid := testValue(t, `{"schema":{}}`)
	if equalResolvedSchemaMember(
		&collector, empty, empty, valid, invalid, "schema", openapi.DialectOAS31,
	) {
		t.Fatal("valid left schema and invalid right schema compared equal")
	}
	if equalResolvedSchemaMember(
		&collector, empty, empty, invalid, valid, "schema", openapi.DialectOAS31,
	) {
		t.Fatal("invalid left schema and valid right schema compared equal")
	}

	target := testValue(t, `{"description":"old","value":1}`)
	usage := testValue(t, `{"description":"new"}`)
	overlaid := overlayReferenceMetadata(target, usage, []string{"description"})
	description, _ := overlaid.Lookup("description")
	text, _ := description.Text()
	if text != "new" {
		t.Fatalf("overlaid description = %q", text)
	}
}

func TestResponseComparisonDefensivePaths(t *testing.T) {
	t.Parallel()

	empty := testValue(t, `{}`)
	collector := changeCollector{ctx: context.Background(), maximum: 8}
	if err := compareResponseHeaders(
		&collector, empty, empty,
		testValue(t, `{"headers":{"Value":false}}`),
		testValue(t, `{"headers":{"value":false}}`),
		"/response", openapi.DialectOAS31,
	); err != nil {
		t.Fatal(err)
	}
	if len(collector.changes) != 0 {
		t.Fatalf("equal invalid headers changed: %#v", collector.changes)
	}

	collector = changeCollector{ctx: context.Background(), maximum: 0}
	if err := compareResponseHeaders(
		&collector, empty, empty,
		testValue(t, `{"headers":{"value":{"x-note":1}}}`),
		testValue(t, `{"headers":{"value":{"x-note":2}}}`),
		"/response", openapi.DialectOAS31,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("header extension error = %v", err)
	}

	collector = changeCollector{ctx: context.Background(), maximum: 8}
	if err := compareSwaggerResponseExamples(
		&collector,
		testValue(t, `{"examples":{"application/json":1}}`),
		testValue(t, `{"examples":{"application/json":1}}`),
		"/response",
	); err != nil || len(collector.changes) != 0 {
		t.Fatalf("equal examples = %#v, %v", collector.changes, err)
	}

	collector = changeCollector{ctx: context.Background(), maximum: 8}
	if err := compareResponseLinks(
		&collector, testValue(t, `{"links":false}`),
		testValue(t, `{"links":false}`), "/response",
	); err != nil || len(collector.changes) != 0 {
		t.Fatalf("equal malformed links = %#v, %v", collector.changes, err)
	}
}

func TestCollectorResolutionFailureStates(t *testing.T) {
	t.Parallel()

	want := errors.New("external failure")
	collector := changeCollector{
		ctx:                    context.Background(),
		maximum:                1,
		remainingResolvedNodes: 4,
	}
	collector.captureResolutionError(want, true)
	collector.captureResolutionError(context.Canceled, false)
	if !errors.Is(collector.resolutionErr, want) {
		t.Fatalf("resolution error = %v", collector.resolutionErr)
	}
	if err := collector.append(Change{}); !errors.Is(err, want) {
		t.Fatalf("append resolution error = %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	collector = changeCollector{
		ctx: canceled, remainingResolvedNodes: 1,
	}
	if err := collector.consumeResolvedValue(testValue(t, `{}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("consume cancellation error = %v", err)
	}

	collector = changeCollector{
		ctx: context.Background(), remainingResolvedNodes: 2,
	}
	collector.referenceLimits.MaxTraversalDepth = 1
	if err := collector.consumeResolvedValue(
		testValue(t, `{"nested":{}}`),
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("consume depth error = %v", err)
	}

	collector = changeCollector{}
	collector.captureResolutionError(context.DeadlineExceeded, false)
	if !errors.Is(collector.resolutionErr, context.DeadlineExceeded) {
		t.Fatalf("deadline error = %v", collector.resolutionErr)
	}
}

func TestExactTraversalAndComparisonBoundaries(t *testing.T) {
	t.Parallel()

	if err := boundValue(
		context.Background(), testValue(t, `{}`), 1, 1,
	); err != nil {
		t.Fatalf("scalar at exact bounds = %v", err)
	}
	if err := boundValue(
		context.Background(), testValue(t, `{"value":{}}`), 2, 2,
	); err != nil {
		t.Fatalf("object at exact bounds = %v", err)
	}
	if err := boundValue(
		context.Background(), testValue(t, `{"value":{}}`), 2, 1,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("object beyond depth bound = %v", err)
	}
	if err := boundValue(
		context.Background(), testValue(t, `{}`), 0, 1,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("root beyond node bound = %v", err)
	}
	for _, value := range []jsonvalue.Value{
		testValue(t, `[[{}]]`),
		testValue(t, `{"nested":{"leaf":{}}}`),
	} {
		if err := boundValue(
			context.Background(), value, 3, 2,
		); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("nested input beyond depth bound = %v", err)
		}
	}

	collector := changeCollector{
		ctx: context.Background(), remainingResolvedNodes: 1,
		referenceLimits: reference.Limits{MaxTraversalDepth: 1},
	}
	if err := collector.consumeResolvedValue(testValue(t, `{}`)); err != nil {
		t.Fatalf("resolved scalar at exact bounds = %v", err)
	}
	collector = changeCollector{
		ctx: context.Background(), remainingResolvedNodes: 2,
		referenceLimits: reference.Limits{MaxTraversalDepth: 2},
	}
	if err := collector.consumeResolvedValue(testValue(t, `[{}]`)); err != nil {
		t.Fatalf("resolved array at exact bounds = %v", err)
	}
	collector = changeCollector{
		ctx: context.Background(), remainingResolvedNodes: 2,
		referenceLimits: reference.Limits{MaxTraversalDepth: 1},
	}
	if err := collector.consumeResolvedValue(
		testValue(t, `[{}]`),
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("resolved array beyond depth bound = %v", err)
	}
	for _, value := range []jsonvalue.Value{
		testValue(t, `[[{}]]`),
		testValue(t, `{"nested":{"leaf":{}}}`),
	} {
		collector = changeCollector{
			ctx: context.Background(), remainingResolvedNodes: 3,
			referenceLimits: reference.Limits{MaxTraversalDepth: 2},
		}
		if err := collector.consumeResolvedValue(value); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("nested resolved value beyond depth bound = %v", err)
		}
	}

	collector = changeCollector{ctx: context.Background(), maximum: 0}
	if err := compareDocumentTags(
		&collector, testValue(t, `{"tags":[]}`),
		testValue(t, `{"tags":[{"name":"added"}]}`),
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("single added tag limit = %v", err)
	}

	empty := testValue(t, `{}`)
	for _, test := range []struct {
		name           string
		leftPath       jsonvalue.Value
		leftOperation  jsonvalue.Value
		rightPath      jsonvalue.Value
		rightOperation jsonvalue.Value
		wantPointer    string
	}{
		{
			name: "left invalid", leftPath: testValue(t, `{"parameters":false}`),
			leftOperation: empty, rightPath: empty, rightOperation: empty,
			wantPointer: "/path/parameters",
		},
		{
			name: "right invalid", leftPath: testValue(t, `{"parameters":false}`),
			leftOperation: empty, rightPath: empty,
			rightOperation: testValue(t, `{"parameters":false}`),
			wantPointer:    "/operation/parameters",
		},
	} {
		collector = changeCollector{ctx: context.Background(), maximum: 8}
		if err := compareParameters(
			&collector, empty, empty, test.leftPath, test.leftOperation,
			test.rightPath, test.rightOperation,
			"/path", "/operation", openapi.DialectOAS31,
		); err != nil {
			t.Fatalf("%s parameter comparison: %v", test.name, err)
		}
		if len(collector.changes) != 1 ||
			collector.changes[0].pointer != test.wantPointer {
			t.Errorf("%s parameter changes = %#v", test.name, collector.changes)
		}
	}

	collector = changeCollector{ctx: context.Background(), maximum: 8}
	malformedServers := testValue(t, `{"servers":false}`)
	if err := compareEffectiveServers(
		&collector, empty, empty, empty, empty,
		malformedServers, malformedServers, "/operation",
	); err != nil || len(collector.changes) != 0 {
		t.Fatalf("equal malformed servers = %#v, %v", collector.changes, err)
	}
	for _, raw := range []string{`[]`, `[{"url":1}]`} {
		if servers, valid := normalizeServers(testValue(t, raw), true); valid || servers != nil {
			t.Fatalf("malformed servers %s normalized as %#v, %t", raw, servers, valid)
		}
	}

	collector = changeCollector{ctx: context.Background(), maximum: 8}
	leftSecurity := testValue(t, `{"security":[{"oauth":["write","read"]}]}`)
	rightSecurity := testValue(t, `{"security":[{"oauth":["read","write"]}]}`)
	if err := compareEffectiveSecurity(
		&collector, empty, empty, leftSecurity, rightSecurity, "/operation",
	); err != nil || len(collector.changes) != 0 {
		t.Fatalf("normalized equal security = %#v, %v", collector.changes, err)
	}

	collector = changeCollector{ctx: context.Background(), maximum: 8}
	identicalSecurity := testValue(t, `{"security":[{"oauth":["read"]}]}`)
	if err := compareEffectiveSecurity(
		&collector, empty, empty, identicalSecurity, identicalSecurity, "/operation",
	); err != nil || len(collector.changes) != 0 {
		t.Fatalf("identical security = %#v, %v", collector.changes, err)
	}

	if members := namedObjectMembers(testValue(t, `{"content":[]}`), "content"); members != nil {
		t.Fatalf("malformed named object members = %#v", members)
	}
	if members := objectMembers(testValue(t, `{"paths":[]}`), "paths", true); members != nil {
		t.Fatalf("malformed root object members = %#v", members)
	}
}

func TestDocumentLevelComparisonsPropagateLimits(t *testing.T) {
	t.Parallel()

	left := testDocument(t, `{
		"openapi":"3.1.0","info":{"title":"API","version":"1"},
		"paths":{},"x-one":1,"x-two":1
	}`)
	right := testDocument(t, `{
		"openapi":"3.1.0","info":{"title":"API","version":"1"},
		"paths":{},"x-one":2,"x-two":2
	}`)
	options := DefaultOptions()
	options.MaxChanges = 1
	if _, err := Operations(
		context.Background(), left, right, options,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("root extension limit error = %v", err)
	}

	collector := changeCollector{ctx: context.Background(), maximum: 0}
	if err := compareSecuritySchemes(
		&collector,
		testValue(t, `{"components":{"securitySchemes":{"auth":{"x-note":1}}}}`),
		testValue(t, `{"components":{"securitySchemes":{"auth":{"x-note":2}}}}`),
		openapi.DialectOAS31,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("security extension error = %v", err)
	}

	collector = changeCollector{ctx: context.Background(), maximum: 8}
	invalidSecurity := testValue(t, `{
		"components":{"securitySchemes":{"auth":{"$ref":true}}}
	}`)
	if err := compareSecuritySchemes(
		&collector, invalidSecurity, invalidSecurity, openapi.DialectOAS31,
	); err != nil || len(collector.changes) != 0 {
		t.Fatalf("equal invalid security = %#v, %v", collector.changes, err)
	}

	collector = changeCollector{ctx: context.Background(), maximum: 8}
	if err := compareSecuritySchemes(
		&collector,
		testValue(t, `{"components":{"securitySchemes":{"auth":{"type":"http","scheme":"bearer"}}}}`),
		invalidSecurity,
		openapi.DialectOAS31,
	); err != nil || len(collector.changes) != 1 ||
		collector.changes[0].kind != SecuritySchemeChanged ||
		collector.changes[0].classification != Unknown {
		t.Fatalf("valid-to-invalid security changes = %#v, %v", collector.changes, err)
	}

	collector = changeCollector{ctx: context.Background(), maximum: 0}
	if err := compareDocumentTags(
		&collector,
		testValue(t, `{"tags":[{"name":"one","x-note":1}]}`),
		testValue(t, `{"tags":[{"name":"one","x-note":2}]}`),
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("tag extension error = %v", err)
	}

	for _, test := range []struct {
		name  string
		left  string
		right string
	}{
		{
			name: "path extension",
			left: `{
				"openapi":"3.1.0","info":{"title":"API","version":"1"},
				"components":{"securitySchemes":{"old":{}}},
				"paths":{"/value":{"x-note":1}}
			}`,
			right: `{
				"openapi":"3.1.0","info":{"title":"API","version":"1"},
				"paths":{"/value":{"x-note":2}}
			}`,
		},
		{
			name: "webhook extension",
			left: `{
				"openapi":"3.1.0","info":{"title":"API","version":"1"},
				"components":{"securitySchemes":{"old":{}}},"paths":{},
				"webhooks":{"event":{"x-note":1}}
			}`,
			right: `{
				"openapi":"3.1.0","info":{"title":"API","version":"1"},
				"paths":{},"webhooks":{"event":{"x-note":2}}
			}`,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			options := DefaultOptions()
			options.MaxChanges = 1
			if _, err := Operations(
				context.Background(), testDocument(t, test.left),
				testDocument(t, test.right), options,
			); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("container extension limit error = %v", err)
			}
		})
	}
}

func TestCallbackDefensiveAndExtensionPaths(t *testing.T) {
	t.Parallel()

	collector := changeCollector{ctx: context.Background(), maximum: 8}
	invalid := testValue(t, `{"callbacks":{"event":{"$ref":true}}}`)
	if err := compareCallbacks(
		&collector, invalid, invalid, "/operation", openapi.DialectOAS31,
	); err != nil || len(collector.changes) != 0 {
		t.Fatalf("equal invalid callbacks = %#v, %v", collector.changes, err)
	}

	collector = changeCollector{ctx: context.Background(), maximum: 8}
	invalid = testValue(t, `{"expression":false}`)
	if err := compareCallbackExpressions(
		&collector, invalid, invalid, "/callback", openapi.DialectOAS31,
	); err != nil || len(collector.changes) != 0 {
		t.Fatalf("equal invalid expressions = %#v, %v", collector.changes, err)
	}

	for _, test := range []struct {
		name string
		run  func(*changeCollector) error
	}{
		{
			name: "callbacks container",
			run: func(collector *changeCollector) error {
				return compareCallbacks(
					collector,
					testValue(t, `{"callbacks":{"x-note":1}}`),
					testValue(t, `{"callbacks":{"x-note":2}}`),
					"/operation", openapi.DialectOAS31,
				)
			},
		},
		{
			name: "callback value",
			run: func(collector *changeCollector) error {
				return compareCallbacks(
					collector,
					testValue(t, `{"callbacks":{"event":{"x-note":1}}}`),
					testValue(t, `{"callbacks":{"event":{"x-note":2}}}`),
					"/operation", openapi.DialectOAS31,
				)
			},
		},
		{
			name: "callback path item",
			run: func(collector *changeCollector) error {
				return compareCallbackOperations(
					collector, testValue(t, `{"x-note":1}`),
					testValue(t, `{"x-note":2}`), "/expression",
					openapi.DialectOAS31,
				)
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			collector := changeCollector{ctx: context.Background(), maximum: 0}
			if err := test.run(&collector); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("comparison error = %v", err)
			}
		})
	}
}

func TestResolverCacheAndAvailabilityDefensivePaths(t *testing.T) {
	t.Parallel()

	collector := changeCollector{
		ctx: context.Background(), maximum: 8,
		resolved: [2]map[string]resolvedReference{
			make(map[string]resolvedReference),
			make(map[string]resolvedReference),
		},
	}
	root := testValue(t, `{}`)
	value := testValue(t, `{"$ref":"#/missing"}`)
	if _, valid := resolveInternalComparable(
		&collector, root, value, openapi.DialectOAS31, leftComparison, nil,
	); valid {
		t.Fatal("missing reference resolved")
	}
	if _, valid := resolveInternalComparable(
		&collector, root, value, openapi.DialectOAS31, leftComparison, nil,
	); valid {
		t.Fatal("cached missing reference resolved")
	}
	if !externalReference("%", "https://example.test/schema") {
		t.Fatal("invalid base did not preserve external classification")
	}
	if externalReference("https://example.test/schema", "#%") {
		t.Fatal("invalid fragment-only reference was external")
	}
	if !resolverAvailable(valueResolver{}) {
		t.Fatal("value resolver was unavailable")
	}
	var nilResolver reference.ResolverFunc
	if resolverAvailable(nilResolver) {
		t.Fatal("typed nil resolver was available")
	}
}

func TestOperationAndResponseNestedFailures(t *testing.T) {
	t.Parallel()

	empty := testValue(t, `{}`)
	collector := changeCollector{ctx: context.Background(), maximum: 0}
	if err := compareOperationContent(
		&collector, empty, empty, empty, empty,
		testValue(t, `{"x-note":1}`), testValue(t, `{"x-note":2}`),
		"/operation", openapi.DialectOAS31,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("operation extension error = %v", err)
	}

	collector = changeCollector{ctx: context.Background(), maximum: 0}
	if err := compareRequestBody(
		&collector, empty, empty,
		testValue(t, `{"requestBody":{"x-note":1}}`),
		testValue(t, `{"requestBody":{"x-note":2}}`),
		"/operation", openapi.DialectOAS31,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("request body extension error = %v", err)
	}

	collector = changeCollector{ctx: context.Background(), maximum: 8}
	if err := compareRequestBody(
		&collector, empty, empty,
		testValue(t, `{"requestBody":{"content":{}}}`),
		testValue(t, `{"requestBody":{"$ref":true}}`),
		"/operation", openapi.DialectOAS31,
	); err != nil || len(collector.changes) != 1 ||
		collector.changes[0].kind != RequestBodyChanged ||
		collector.changes[0].classification != Unknown {
		t.Fatalf("valid-to-invalid request body changes = %#v, %v", collector.changes, err)
	}

	collector = changeCollector{ctx: context.Background(), maximum: 8}
	invalidResponses := testValue(t, `{"responses":{"200":{"$ref":true}}}`)
	if err := compareResponses(
		&collector, empty, empty, invalidResponses, invalidResponses,
		"/operation", openapi.DialectOAS31,
	); err != nil || len(collector.changes) != 0 {
		t.Fatalf("equal invalid responses = %#v, %v", collector.changes, err)
	}

	collector = changeCollector{ctx: context.Background(), maximum: 0}
	if err := compareResponses(
		&collector, empty, empty,
		testValue(t, `{"responses":{"200":{"headers":{"value":{}}}}}`),
		testValue(t, `{"responses":{"200":{"headers":{}}}}`),
		"/operation", openapi.DialectOAS31,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("nested response header error = %v", err)
	}

	collector = changeCollector{ctx: context.Background(), maximum: 8}
	invalidLinks := testValue(t, `{"links":{"next":{"$ref":true}}}`)
	if err := compareResponseLinks(
		&collector, invalidLinks, invalidLinks, "/response",
	); err != nil || len(collector.changes) != 0 {
		t.Fatalf("equal invalid links = %#v, %v", collector.changes, err)
	}

	collector = changeCollector{ctx: context.Background(), maximum: 8}
	if err := compareResponseLinksWithRoots(
		&collector, empty, empty,
		testValue(t, `{"links":{"next":{"operationId":"next"}}}`),
		testValue(t, `{"links":{"next":{"$ref":true}}}`),
		"/response", openapi.DialectOAS31,
	); err != nil || len(collector.changes) != 1 ||
		collector.changes[0].kind != LinkChanged ||
		collector.changes[0].classification != Unknown {
		t.Fatalf("valid-to-invalid link changes = %#v, %v", collector.changes, err)
	}

	for _, test := range []struct {
		name string
		run  func(*changeCollector) error
	}{
		{
			name: "response extension",
			run: func(collector *changeCollector) error {
				return compareResponses(
					collector, empty, empty,
					testValue(t, `{"responses":{"200":{"x-note":1}}}`),
					testValue(t, `{"responses":{"200":{"x-note":2}}}`),
					"/operation", openapi.DialectOAS31,
				)
			},
		},
		{
			name: "response header changed",
			run: func(collector *changeCollector) error {
				return compareResponseHeaders(
					collector, empty, empty,
					testValue(t, `{"headers":{"value":false}}`),
					testValue(t, `{"headers":{"value":true}}`),
					"/response", openapi.DialectOAS31,
				)
			},
		},
		{
			name: "response header added",
			run: func(collector *changeCollector) error {
				return compareResponseHeaders(
					collector, empty, empty, empty,
					testValue(t, `{"headers":{"value":{}}}`),
					"/response", openapi.DialectOAS31,
				)
			},
		},
		{
			name: "Swagger example added",
			run: func(collector *changeCollector) error {
				return compareSwaggerResponseExamples(
					collector, empty,
					testValue(t, `{"examples":{"application/json":1}}`),
					"/response",
				)
			},
		},
		{
			name: "media extension",
			run: func(collector *changeCollector) error {
				return compareCommonMediaTypeMetadata(
					collector,
					[]jsonvalue.Member{{Name: "application/json", Value: testValue(t, `{"x-note":1}`)}},
					[]jsonvalue.Member{{Name: "application/json", Value: testValue(t, `{"x-note":2}`)}},
					"/content", schemaInResponse,
				)
			},
		},
		{
			name: "links extension",
			run: func(collector *changeCollector) error {
				return compareResponseLinks(
					collector, testValue(t, `{"links":{"x-note":1}}`),
					testValue(t, `{"links":{"x-note":2}}`), "/response",
				)
			},
		},
		{
			name: "link extension",
			run: func(collector *changeCollector) error {
				return compareResponseLinks(
					collector,
					testValue(t, `{"links":{"next":{"x-note":1}}}`),
					testValue(t, `{"links":{"next":{"x-note":2}}}`),
					"/response",
				)
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			collector := changeCollector{ctx: context.Background(), maximum: 0}
			if err := test.run(&collector); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("comparison error = %v", err)
			}
		})
	}
}

func TestInvalidSchemaClassificationsAndExtensionEquality(t *testing.T) {
	t.Parallel()

	empty := testValue(t, `{}`)
	collector := changeCollector{ctx: context.Background(), maximum: 0}
	if err := compareSwaggerResponseContract(
		&collector, empty, empty,
		testValue(t, `{"schema":{"$ref":"#/missing"}}`),
		testValue(t, `{"schema":{"type":"string"}}`),
		"/response", openapi.DialectSwagger20,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("Swagger invalid schema error = %v", err)
	}

	collector = changeCollector{ctx: context.Background(), maximum: 8}
	unresolvedSchema := testValue(t, `{"schema":{"$ref":"#/missing"}}`)
	if err := compareSwaggerResponseContract(
		&collector, empty, empty, unresolvedSchema, unresolvedSchema,
		"/response", openapi.DialectSwagger20,
	); err != nil || len(collector.changes) != 0 {
		t.Fatalf("equal unresolved Swagger schemas = %#v, %v", collector.changes, err)
	}

	collector = changeCollector{ctx: context.Background(), maximum: 8}
	if err := compareSwaggerResponseContract(
		&collector, empty, empty,
		testValue(t, `{"schema":{"type":"string"}}`), unresolvedSchema,
		"/response", openapi.DialectSwagger20,
	); err != nil || len(collector.changes) != 1 ||
		collector.changes[0].classification != Unknown {
		t.Fatalf("valid-to-unresolved Swagger schema changes = %#v, %v", collector.changes, err)
	}

	collector = changeCollector{ctx: context.Background(), maximum: 8}
	left := []jsonvalue.Member{{
		Name:  "application/json",
		Value: testValue(t, `{"schema":{"$ref":"#/missing"}}`),
	}}
	right := []jsonvalue.Member{{
		Name:  "application/json",
		Value: testValue(t, `{"schema":{"type":"string"}}`),
	}}
	if err := compareCommonMediaTypeSchemas(
		&collector, empty, empty, left, right, "/content",
		schemaInResponse, openapi.DialectOAS31,
	); err != nil || len(collector.changes) != 1 ||
		collector.changes[0].classification != Unknown {
		t.Fatalf("invalid schema changes = %#v, %v", collector.changes, err)
	}

	collector = changeCollector{ctx: context.Background(), maximum: 8}
	equalUnresolved := []jsonvalue.Member{{
		Name: "application/json", Value: testValue(t, `{"schema":{"$ref":"#/missing"}}`),
	}}
	if err := compareCommonMediaTypeSchemas(
		&collector, empty, empty, equalUnresolved, equalUnresolved, "/content",
		schemaInResponse, openapi.DialectOAS31,
	); err != nil || len(collector.changes) != 0 {
		t.Fatalf("equal unresolved media schemas = %#v, %v", collector.changes, err)
	}

	collector = changeCollector{ctx: context.Background(), maximum: 8}
	left = []jsonvalue.Member{{Name: "application/json", Value: empty}}
	right = equalUnresolved
	if err := compareCommonMediaTypeSchemas(
		&collector, empty, empty, left, right, "/content",
		schemaInResponse, openapi.DialectOAS31,
	); err != nil || len(collector.changes) != 1 ||
		collector.changes[0].classification != Unknown {
		t.Fatalf("absent-to-unresolved media schema changes = %#v, %v", collector.changes, err)
	}

	collector = changeCollector{ctx: context.Background(), maximum: 1}
	if err := compareExtensions(
		&collector, testValue(t, `{"x-note":1}`),
		testValue(t, `{"x-note":1}`), "",
	); err != nil || len(collector.changes) != 0 {
		t.Fatalf("equal extensions = %#v, %v", collector.changes, err)
	}
}

func TestCircularComparableReferenceIsInvalid(t *testing.T) {
	t.Parallel()

	root := testValue(t, `{
		"components":{"schemas":{
			"A":{"$ref":"#/components/schemas/B"},
			"B":{"$ref":"#/components/schemas/A"}
		}}
	}`)
	collector := changeCollector{
		ctx: context.Background(), maximum: 8,
		resolved: [2]map[string]resolvedReference{
			make(map[string]resolvedReference), make(map[string]resolvedReference),
		},
	}
	if _, valid := resolveInternalComparable(
		&collector, root, testValue(t, `{"$ref":"#/components/schemas/A"}`),
		openapi.DialectOAS31, leftComparison, nil,
	); valid {
		t.Fatal("circular reference was comparable")
	}
}

func TestDiffPreservesSecurityAndMetadataTraversalAfterSkippedEntries(t *testing.T) {
	t.Parallel()

	left := testValue(t, `{"components":{"securitySchemes":{
		"Removed":{"type":"apiKey","in":"header","name":"X-Removed"},
		"Equal":{"type":"apiKey","in":"header","name":"X-Equal"},
		"Changed":{"type":"apiKey","in":"header","name":"X-Old"},
		"InvalidEqual":{"$ref":"#/components/securitySchemes/Missing"},
		"InvalidChanged":{"$ref":"#/components/securitySchemes/LeftMissing"}
	}}}`)
	right := testValue(t, `{"components":{"securitySchemes":{
		"Equal":{"type":"apiKey","in":"header","name":"X-Equal"},
		"Changed":{"type":"apiKey","in":"header","name":"X-New"},
		"InvalidEqual":{"$ref":"#/components/securitySchemes/Missing"},
		"InvalidChanged":{"$ref":"#/components/securitySchemes/RightMissing"},
		"Added":{"type":"apiKey","in":"header","name":"X-Added"}
	}}}`)
	collector := changeCollector{ctx: context.Background(), maximum: 16}
	if err := compareSecuritySchemes(
		&collector, left, right, openapi.DialectOAS31,
	); err != nil {
		t.Fatal(err)
	}
	want := []struct {
		kind           Kind
		classification Classification
		pointer        string
	}{
		{SecuritySchemeRemoved, Breaking, "/components/securitySchemes/Removed"},
		{SecuritySchemeChanged, Conditional, "/components/securitySchemes/Changed"},
		{SecuritySchemeChanged, Unknown, "/components/securitySchemes/InvalidChanged"},
		{SecuritySchemeAdded, Additive, "/components/securitySchemes/Added"},
	}
	if len(collector.changes) != len(want) {
		t.Fatalf("security changes = %#v", collector.changes)
	}
	for index, expected := range want {
		change := collector.changes[index]
		if change.kind != expected.kind ||
			change.classification != expected.classification ||
			change.pointer != expected.pointer {
			t.Fatalf("security change %d = %#v, want %#v", index, change, expected)
		}
	}

	for _, raw := range []string{
		`{"tags":[{"description":"missing"}]}`,
		`{"tags":[{"name":1}]}`,
		`{"tags":[{"name":"one"},{"name":"one"}]}`,
	} {
		if _, present, valid := documentTags(testValue(t, raw)); !present || valid {
			t.Fatalf("invalid tags accepted for %s", raw)
		}
	}

	tags, present, valid := documentTags(testValue(t, `{"tags":[
		{"name":"one"},{"name":"two"},{"name":"three"}
	]}`))
	if !present || !valid || len(tags) != 3 || tags[2].name != "three" ||
		tags[2].index != 2 {
		t.Fatalf("ordered tags = %#v, %t, %t", tags, present, valid)
	}

	schemes, present, valid := swaggerSchemes(testValue(t, `{"schemes":[
		"https","https","http"
	]}`))
	if !present || !valid || len(schemes) != 2 || schemes[0].name != "https" ||
		schemes[1].name != "http" || schemes[1].index != 2 {
		t.Fatalf("deduplicated schemes = %#v, %t, %t", schemes, present, valid)
	}
	if _, present, valid := swaggerSchemes(testValue(t, `{"schemes":["https",1]}`)); !present || valid {
		t.Fatal("non-string Swagger scheme was accepted")
	}
}

func TestDiffPreservesOperationAndParameterTraversalAfterSkippedEntries(t *testing.T) {
	t.Parallel()

	empty := testValue(t, `{}`)
	collector := changeCollector{ctx: context.Background(), maximum: 32}
	leftPaths := testValue(t, `{
		"/missing":{"get":{}},
		"/common":{"get":{},"post":{"operationId":"old"}}
	}`)
	rightPaths := testValue(t, `{
		"/common":{"post":{"operationId":"new"}}
	}`)
	leftMembers, _ := leftPaths.Members()
	rightMembers, _ := rightPaths.Members()
	if err := collectCommonOperationContent(
		&collector, empty, empty, leftMembers, rightMembers,
		"/paths", openapi.DialectOAS31,
	); err != nil {
		t.Fatal(err)
	}
	if len(collector.changes) != 1 ||
		collector.changes[0].kind != OperationIDChanged ||
		collector.changes[0].pointer != "/paths/~1common/post/operationId" {
		t.Fatalf("common operation changes = %#v", collector.changes)
	}

	collector = changeCollector{ctx: context.Background(), maximum: 32}
	leftOperation := testValue(t, `{"parameters":[
		{"name":"removed","in":"query"},
		{"name":"equal","in":"query","required":false},
		{"name":"required","in":"query","required":false},
		{"name":"invalid","in":"query","required":"yes"},
		{"$ref":"#/missing/equal"},
		{"$ref":"#/missing/left"}
	]}`)
	rightOperation := testValue(t, `{"parameters":[
		{"name":"equal","in":"query","required":false},
		{"name":"required","in":"query","required":true},
		{"name":"invalid","in":"query","required":false},
		{"name":"added","in":"query","required":true},
		{"$ref":"#/missing/equal"},
		{"$ref":"#/missing/right"}
	]}`)
	if err := compareParameters(
		&collector, empty, empty, empty, leftOperation, empty, rightOperation,
		"/path", "/operation", openapi.DialectOAS31,
	); err != nil {
		t.Fatal(err)
	}
	wantKinds := []Kind{
		ParameterRequired, ParameterChanged, ParameterRemoved,
		ParameterAdded, ParameterChanged,
	}
	if len(collector.changes) != len(wantKinds) {
		t.Fatalf("parameter changes = %#v", collector.changes)
	}
	for index, kind := range wantKinds {
		if collector.changes[index].kind != kind {
			t.Fatalf("parameter change %d = %#v, want %s", index, collector.changes[index], kind)
		}
	}

	set := parameterSet{}
	appendParameters(
		&collector, empty, &set, make(map[string]int),
		testValue(t, `{"parameters":[
			{"name":"value","in":"query","description":"first"},
			{"name":"value","in":"query","description":"replacement"},
			{"name":"later","in":"query"}
		]}`), "/operation/parameters", openapi.DialectOAS31, leftComparison,
	)
	if len(set.identified) != 2 {
		t.Fatalf("deduplicated parameters = %#v", set.identified)
	}
	text, _, _ := optionalText(set.identified[0].value, "description")
	if text != "replacement" || set.identified[1].key != "query\x00later" {
		t.Fatalf("deduplicated parameters = %#v", set.identified)
	}

	for _, test := range []struct {
		name string
		raw  string
	}{
		{name: "missing name", raw: `{"in":"query"}`},
		{name: "invalid name", raw: `{"name":1,"in":"query"}`},
		{name: "missing location", raw: `{"name":"value"}`},
		{name: "invalid location", raw: `{"name":"value","in":1}`},
	} {
		if _, _, identified := parameterIdentity(testValue(t, test.raw)); identified {
			t.Fatalf("%s parameter was identified", test.name)
		}
	}
	key, location, identified := parameterIdentity(testValue(t, `{"name":"X-ID","in":"header"}`))
	if !identified || key != "header\x00x-id" || location != "header" {
		t.Fatalf("header identity = %q, %q, %t", key, location, identified)
	}

	for _, test := range []struct {
		name  string
		left  string
		right string
	}{
		{name: "presence", left: `{}`, right: `{"operationId":"one"}`},
		{name: "validity", left: `{"operationId":1}`, right: `{"operationId":"one"}`},
		{name: "value", left: `{"operationId":"one"}`, right: `{"operationId":"two"}`},
	} {
		collector = changeCollector{ctx: context.Background(), maximum: 8}
		if err := compareOperationContent(
			&collector, empty, empty, empty, empty,
			testValue(t, test.left), testValue(t, test.right),
			"/operation", openapi.DialectOAS31,
		); err != nil {
			t.Fatal(err)
		}
		if len(collector.changes) != 1 || collector.changes[0].kind != OperationIDChanged {
			t.Fatalf("%s operation ID changes = %#v", test.name, collector.changes)
		}
	}
}

type valueResolver struct{}

func (valueResolver) Resolve(context.Context, string) (reference.Resource, error) {
	return reference.Resource{}, errors.New("resource unavailable")
}

func testDocument(t *testing.T, raw string) openapi.Document {
	t.Helper()
	document, err := openapi.ParseJSON(
		context.Background(), strings.NewReader(raw), parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func testValue(t *testing.T, raw string) jsonvalue.Value {
	t.Helper()
	value, err := parse.JSON(
		context.Background(), strings.NewReader(raw), parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
