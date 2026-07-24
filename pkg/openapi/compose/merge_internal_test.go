package compose

import (
	"context"
	"errors"
	"runtime"
	"strconv"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

type cancelAfterFirstMergeCheck struct {
	context.Context
	calls int
}

func (ctx *cancelAfterFirstMergeCheck) Err() error {
	ctx.calls++
	if ctx.calls > 1 {
		return context.Canceled
	}
	return nil
}

func TestMergeRejectsWideValuesBeforeCopyingChildren(t *testing.T) {
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
		{name: "rewrite", run: func() error {
			merger := documentMerger{
				ctx:     context.Background(),
				options: MergeOptions{MaxDepth: 2, MaxValueNodes: 1},
			}
			_, err := merger.rewriteIncoming(wide, "", 1, nil, nil)
			return err
		}},
		{name: "equality", run: func() error {
			merger := documentMerger{
				ctx:     context.Background(),
				options: MergeOptions{MaxDepth: 2, MaxValueNodes: 1},
			}
			_, err := merger.semanticEqual(wide, wide)
			return err
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
					t.Fatalf("wide merge operation error = %v", err)
				}
			}
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			allocated := (after.TotalAlloc - before.TotalAlloc) / repetitions
			if allocated > 64<<10 {
				t.Fatalf("wide rejected merge allocated %d bytes per operation", allocated)
			}
		})
	}
}

func TestMergeChildrenFitExactBudgets(t *testing.T) {
	t.Parallel()

	merger := documentMerger{
		options:    MergeOptions{MaxDepth: 3, MaxValueNodes: 6},
		valueNodes: 2,
	}
	for _, test := range []struct {
		name     string
		children int
		queued   int
		depth    int
		want     bool
	}{
		{name: "leaf at depth limit", depth: 3, want: true},
		{name: "exact remaining nodes", children: 2, queued: 2, depth: 2, want: true},
		{name: "node overflow", children: 3, queued: 2, depth: 2},
		{name: "queue exhausted", children: 1, queued: 4, depth: 2},
		{name: "queue overflow", children: 1, queued: 5, depth: 2},
		{name: "exact depth", children: 1, depth: 3},
	} {
		if got := merger.childrenFit(test.children, test.queued, test.depth); got != test.want {
			t.Fatalf("%s fit = %t, want %t", test.name, got, test.want)
		}
	}
}

func TestRewriteIncomingPropagatesChildCancellationAndNodeLimit(t *testing.T) {
	t.Parallel()

	array, _ := jsonvalue.Array([]jsonvalue.Value{jsonvalue.Null()})
	merger := documentMerger{
		ctx:     &cancelAfterFirstMergeCheck{Context: context.Background()},
		options: MergeOptions{MaxDepth: 2, MaxValueNodes: 2},
	}
	if _, err := merger.rewriteIncoming(
		array, "", 1, nil, nil,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("child cancellation error = %v", err)
	}

	merger.ctx = context.Background()
	merger.valueNodes = 1
	merger.options.MaxValueNodes = 1
	if _, err := merger.rewriteIncoming(
		jsonvalue.Null(), "", 1, nil, nil,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("node overflow error = %v", err)
	}
}

func TestSemanticEqualComparesEveryJSONKind(t *testing.T) {
	t.Parallel()

	one, _ := jsonvalue.Number("1")
	onePointZero, _ := jsonvalue.Number("1.0")
	a, _ := jsonvalue.String("a")
	b, _ := jsonvalue.String("b")
	arrayAB, _ := jsonvalue.Array([]jsonvalue.Value{a, b})
	arrayABCopy, _ := jsonvalue.Array([]jsonvalue.Value{a, b})
	arrayA, _ := jsonvalue.Array([]jsonvalue.Value{a})
	arrayBA, _ := jsonvalue.Array([]jsonvalue.Value{b, a})
	arrayBB, _ := jsonvalue.Array([]jsonvalue.Value{b, b})
	objectAB, _ := jsonvalue.Object([]jsonvalue.Member{
		{Name: "a", Value: one},
		{Name: "b", Value: jsonvalue.Boolean(true)},
	})
	objectBA, _ := jsonvalue.Object([]jsonvalue.Member{
		{Name: "b", Value: jsonvalue.Boolean(true)},
		{Name: "a", Value: one},
	})
	objectA, _ := jsonvalue.Object([]jsonvalue.Member{{Name: "a", Value: one}})
	objectAC, _ := jsonvalue.Object([]jsonvalue.Member{
		{Name: "a", Value: one},
		{Name: "c", Value: jsonvalue.Boolean(true)},
	})
	objectDifferent, _ := jsonvalue.Object([]jsonvalue.Member{
		{Name: "a", Value: onePointZero},
		{Name: "b", Value: jsonvalue.Boolean(true)},
	})

	tests := []struct {
		name  string
		left  jsonvalue.Value
		right jsonvalue.Value
		want  bool
	}{
		{name: "null", left: jsonvalue.Null(), right: jsonvalue.Null(), want: true},
		{name: "different kinds", left: jsonvalue.Null(), right: jsonvalue.Boolean(false)},
		{name: "boolean equal", left: jsonvalue.Boolean(true), right: jsonvalue.Boolean(true), want: true},
		{name: "boolean differs", left: jsonvalue.Boolean(true), right: jsonvalue.Boolean(false)},
		{name: "number exact", left: one, right: one, want: true},
		{name: "number spelling differs", left: one, right: onePointZero},
		{name: "string equal", left: a, right: a, want: true},
		{name: "string differs", left: a, right: b},
		{name: "array equal", left: arrayAB, right: arrayABCopy, want: true},
		{name: "array length differs", left: arrayAB, right: arrayA},
		{name: "array order differs", left: arrayAB, right: arrayBA},
		{name: "array first element differs", left: arrayAB, right: arrayBB},
		{name: "object order ignored", left: objectAB, right: objectBA, want: true},
		{name: "object length differs", left: objectAB, right: objectA},
		{name: "object member differs", left: objectAB, right: objectDifferent},
		{name: "object name differs", left: objectAB, right: objectAC},
		{name: "invalid values are not equal", left: jsonvalue.Value{}, right: jsonvalue.Value{}},
	}
	for _, test := range tests {
		merger := documentMerger{
			ctx: context.Background(),
			options: MergeOptions{
				MaxDepth:      256,
				MaxValueNodes: 1_000,
			},
		}
		got, err := merger.semanticEqual(test.left, test.right)
		if err != nil {
			t.Fatalf("%s: semanticEqual() error = %v", test.name, err)
		}
		if got != test.want {
			t.Errorf("%s: semanticEqual() = %t, want %t", test.name, got, test.want)
		}
	}
}

func TestSemanticEqualObservesCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	merger := documentMerger{
		ctx: ctx,
		options: MergeOptions{
			MaxDepth:      256,
			MaxValueNodes: 1_000,
		},
	}
	if _, err := merger.semanticEqual(jsonvalue.Null(), jsonvalue.Null()); !errors.Is(err, context.Canceled) {
		t.Fatalf("semanticEqual() error = %v", err)
	}
}

func TestRewriteIncomingPropagatesArrayElementLimits(t *testing.T) {
	t.Parallel()

	array, _ := jsonvalue.Array([]jsonvalue.Value{jsonvalue.Boolean(true)})
	merger := documentMerger{
		ctx: context.Background(),
		options: MergeOptions{
			MaxDepth:      1,
			MaxValueNodes: 1_000,
		},
	}
	if _, err := merger.rewriteIncoming(
		array, "", 1, nil, nil,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("rewriteIncoming() error = %v", err)
	}
}

func TestMergeCountersAcceptExactLimits(t *testing.T) {
	t.Parallel()

	merger := documentMerger{
		ctx: context.Background(),
		options: MergeOptions{
			MaxEntries: 1, MaxDepth: 1, MaxValueNodes: 1,
		},
	}
	if err := merger.countEntry(); err != nil {
		t.Fatalf("exact entry limit error = %v", err)
	}
	if !errors.Is(merger.countEntry(), ErrLimitExceeded) {
		t.Fatal("entry overflow was accepted")
	}
	merger.preparedEntries = 0
	if err := merger.countPreparedEntry(); err != nil {
		t.Fatalf("exact prepared-entry limit error = %v", err)
	}
	if !errors.Is(merger.countPreparedEntry(), ErrLimitExceeded) {
		t.Fatal("prepared-entry overflow was accepted")
	}
	merger.valueNodes = 0
	if got, err := merger.rewriteIncoming(
		jsonvalue.Null(), "", 1, nil, nil,
	); err != nil || got.Kind() != jsonvalue.NullKind {
		t.Fatalf("exact rewrite limits = %#v, %v", got, err)
	}
	merger.valueNodes = 0
	if equal, err := merger.semanticEqual(
		jsonvalue.Null(), jsonvalue.Null(),
	); err != nil || !equal {
		t.Fatalf("exact equality limits = %t, %v", equal, err)
	}
}

func TestRootRegistryRecognizesWebhooksByDialect(t *testing.T) {
	t.Parallel()

	for _, dialect := range []specversion.Dialect{
		specversion.DialectOAS31, specversion.DialectOAS32,
	} {
		if !(&documentMerger{dialect: dialect}).rootRegistry("webhooks") {
			t.Fatalf("dialect %q rejected webhooks", dialect)
		}
	}
	if (&documentMerger{dialect: specversion.DialectOAS30}).rootRegistry("webhooks") {
		t.Fatal("OpenAPI 3.0 accepted webhooks")
	}
}
