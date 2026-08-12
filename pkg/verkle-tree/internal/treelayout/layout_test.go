package treelayout

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestBuildCreatesCanonicalTopologyAndClassifiesPaths(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()

		layout := buildLayout(t, nil)
		assertLayoutCounts(t, layout, 0, 1, 0)
		if root := layout.nodes[0]; root.kind != KindInternal || root.depth != 0 {
			t.Fatalf("root = %#v, want depth-zero internal node", root)
		}
		assertLookup(t, layout, Stem{0x10}, Result{
			Match: MatchMissingChild,
			Depth: 1,
		})
	})

	t.Run("single stem", func(t *testing.T) {
		t.Parallel()

		stem := Stem{0x10, 0x20}
		layout := buildLayout(t, []Stem{stem})
		assertLayoutCounts(t, layout, 1, 2, 1)
		if got := layout.edges[0]; got.index != 0x10 {
			t.Fatalf("root edge index = %d, want 16", got.index)
		}
		leaf := layout.nodes[layout.edges[0].child]
		if leaf.kind != KindStem || leaf.depth != 1 || leaf.stem != stem {
			t.Fatalf("stem node = %#v, want stem at depth 1", leaf)
		}
		assertLookup(t, layout, stem, Result{
			Match:    MatchPresentStem,
			Depth:    1,
			Existing: stem,
		})
		assertLookup(t, layout, Stem{0x11}, Result{
			Match: MatchMissingChild,
			Depth: 1,
		})
		assertLookup(t, layout, Stem{0x10, 0x21}, Result{
			Match:    MatchDifferentStem,
			Depth:    1,
			Existing: stem,
		})
	})

	t.Run("collision", func(t *testing.T) {
		t.Parallel()

		first := Stem{0x10, 0x20}
		second := Stem{0x10, 0x30}
		layout := buildLayout(t, []Stem{second, first})
		assertLayoutCounts(t, layout, 2, 4, 3)

		child := layout.nodes[layout.edges[0].child]
		if child.kind != KindInternal || child.depth != 1 ||
			child.edgeCount != 2 {
			t.Fatalf("collision child = %#v, want depth-one internal", child)
		}
		assertLookup(t, layout, first, Result{
			Match:    MatchPresentStem,
			Depth:    2,
			Existing: first,
		})
		assertLookup(t, layout, Stem{0x10, 0x25}, Result{
			Match: MatchMissingChild,
			Depth: 2,
		})
		assertLookup(t, layout, Stem{0x10, 0x20, 0x01}, Result{
			Match:    MatchDifferentStem,
			Depth:    2,
			Existing: first,
		})
	})

	t.Run("maximum stem depth", func(t *testing.T) {
		t.Parallel()

		first := Stem{}
		second := Stem{}
		first[30] = 0x01
		second[30] = 0x02
		layout := buildLayout(t, []Stem{first, second})
		assertLayoutCounts(t, layout, 2, 33, 32)
		assertLookup(t, layout, first, Result{
			Match:    MatchPresentStem,
			Depth:    31,
			Existing: first,
		})
		assertLookup(t, layout, second, Result{
			Match:    MatchPresentStem,
			Depth:    31,
			Existing: second,
		})
		missing := Stem{}
		missing[30] = 0x03
		assertLookup(t, layout, missing, Result{
			Match: MatchMissingChild,
			Depth: 31,
		})
	})
}

func TestBuildIsDeterministicAndDoesNotMutateCallerInput(t *testing.T) {
	t.Parallel()

	first := Stem{0x01}
	second := Stem{0x02}
	reversed := []Stem{second, first}
	original := append([]Stem(nil), reversed...)

	left := buildLayout(t, reversed)
	right := buildLayout(t, []Stem{first, second})

	if !reflect.DeepEqual(reversed, original) {
		t.Fatalf("caller stems changed: %x, want %x", reversed, original)
	}
	if !reflect.DeepEqual(left.nodes, right.nodes) ||
		!reflect.DeepEqual(left.edges, right.edges) ||
		!reflect.DeepEqual(left.stems, right.stems) {
		t.Fatalf("canonical layouts differ:\nleft %#v\nright %#v", left, right)
	}
}

func TestLayoutInsertAndDeleteReturnCanonicalImmutableValues(t *testing.T) {
	t.Parallel()

	first := Stem{}
	second := Stem{}
	first[30] = 0x01
	second[30] = 0x02

	empty := buildLayout(t, nil)
	one, inserted, err := empty.Insert(context.Background(), first)
	if err != nil || !inserted {
		t.Fatalf("insert first = inserted %t, error %v", inserted, err)
	}
	assertLayoutCounts(t, empty, 0, 1, 0)
	assertLayoutCounts(t, one, 1, 2, 1)

	two, inserted, err := one.Insert(context.Background(), second)
	if err != nil || !inserted {
		t.Fatalf("insert second = inserted %t, error %v", inserted, err)
	}
	assertLayoutCounts(t, one, 1, 2, 1)
	assertLayoutCounts(t, two, 2, 33, 32)

	unchanged, inserted, err := two.Insert(context.Background(), second)
	if err != nil || inserted {
		t.Fatalf("insert existing = inserted %t, error %v", inserted, err)
	}
	if !reflect.DeepEqual(unchanged, two) {
		t.Fatal("existing insert changed layout")
	}

	collapsed, deleted, err := two.Delete(context.Background(), second)
	if err != nil || !deleted {
		t.Fatalf("delete second = deleted %t, error %v", deleted, err)
	}
	assertLayoutCounts(t, two, 2, 33, 32)
	assertLayoutCounts(t, collapsed, 1, 2, 1)
	assertLookup(t, collapsed, first, Result{
		Match:    MatchPresentStem,
		Depth:    1,
		Existing: first,
	})

	unchanged, deleted, err = collapsed.Delete(context.Background(), second)
	if err != nil || deleted {
		t.Fatalf("delete absent = deleted %t, error %v", deleted, err)
	}
	if !reflect.DeepEqual(unchanged, collapsed) {
		t.Fatal("absent delete changed layout")
	}
}

func TestBuildRejectsDuplicatesAndInvalidInputs(t *testing.T) {
	t.Parallel()

	stem := Stem{0x01}
	if _, err := Build(context.Background(), []Stem{stem, stem}, testLimits()); !errors.Is(err, errDuplicateStem) {
		t.Fatalf("duplicate build error = %v, want %v", err, errDuplicateStem)
	}

	invalidLimits := []Limits{
		{},
		{MaxStems: 1, MaxNodes: 1, MaxEdges: 1},
		{MaxStems: 1, MaxNodes: 1, MaxTemporaryBytes: 1},
		{MaxStems: 1, MaxEdges: 1, MaxTemporaryBytes: 1},
		{
			MaxStems:          maxSupportedStemCount + 1,
			MaxNodes:          1,
			MaxEdges:          1,
			MaxTemporaryBytes: 1,
		},
		{
			MaxStems:          1,
			MaxNodes:          maxSupportedStemCount + 1,
			MaxEdges:          1,
			MaxTemporaryBytes: 1,
		},
		{
			MaxStems:          1,
			MaxNodes:          1,
			MaxEdges:          maxSupportedStemCount + 1,
			MaxTemporaryBytes: 1,
		},
	}
	for _, limits := range invalidLimits {
		if _, err := Build(context.Background(), nil, limits); !errors.Is(err, errInvalidLimits) {
			t.Fatalf("invalid limits error = %v, want %v", err, errInvalidLimits)
		}
	}
	maximumLimits := Limits{
		MaxStems:          maxSupportedStemCount,
		MaxNodes:          maxSupportedStemCount,
		MaxEdges:          maxSupportedStemCount,
		MaxTemporaryBytes: nodeWorkingBytes,
	}
	if err := maximumLimits.validate(); err != nil {
		t.Fatalf("maximum supported limits: %v", err)
	}

	var missingContext context.Context
	if _, err := Build(missingContext, nil, testLimits()); !errors.Is(err, errInvalidContext) {
		t.Fatalf("nil-context build error = %v, want %v", err, errInvalidContext)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Build(cancelled, nil, testLimits()); !errors.Is(err, context.Canceled) || !errors.Is(err, errCancelled) {
		t.Fatalf("cancelled build error = %v, want cancellation", err)
	}

	var zero Layout
	if _, err := zero.Lookup(context.Background(), Stem{}); !errors.Is(err, errInvalidLayout) {
		t.Fatalf("zero-layout lookup error = %v, want %v", err, errInvalidLayout)
	}
	if _, _, err := zero.Insert(context.Background(), Stem{}); !errors.Is(err, errInvalidLayout) {
		t.Fatalf("zero-layout insert error = %v, want %v", err, errInvalidLayout)
	}
	if _, _, err := zero.Delete(context.Background(), Stem{}); !errors.Is(err, errInvalidLayout) {
		t.Fatalf("zero-layout delete error = %v, want %v", err, errInvalidLayout)
	}
}

func TestLayoutRejectsCancelledOperations(t *testing.T) {
	t.Parallel()

	layout := buildLayout(t, []Stem{{0x01}})
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := layout.Lookup(cancelled, Stem{0x01}); !errors.Is(err, context.Canceled) || !errors.Is(err, errCancelled) {
		t.Fatalf("cancelled lookup error = %v, want cancellation", err)
	}
	if _, _, err := layout.Insert(cancelled, Stem{0x02}); !errors.Is(err, context.Canceled) || !errors.Is(err, errCancelled) {
		t.Fatalf("cancelled insert error = %v, want cancellation", err)
	}
	if _, _, err := layout.Delete(cancelled, Stem{0x01}); !errors.Is(err, context.Canceled) || !errors.Is(err, errCancelled) {
		t.Fatalf("cancelled delete error = %v, want cancellation", err)
	}
	var missingContext context.Context
	if _, err := layout.Lookup(missingContext, Stem{0x01}); !errors.Is(err, errInvalidContext) {
		t.Fatalf("nil-context lookup error = %v, want %v", err, errInvalidContext)
	}
}

func TestBuildEnforcesEveryResourceBudget(t *testing.T) {
	t.Parallel()

	first := Stem{0x10, 0x20}
	second := Stem{0x10, 0x30}
	tests := map[string]struct {
		limits Limits
		kind   ResourceKind
		actual uint64
	}{
		"stems": {
			limits: Limits{
				MaxStems:          1,
				MaxNodes:          4,
				MaxEdges:          3,
				MaxTemporaryBytes: 4096,
			},
			kind:   ResourceStems,
			actual: 2,
		},
		"initial temporary bytes": {
			limits: Limits{
				MaxStems:          2,
				MaxNodes:          4,
				MaxEdges:          3,
				MaxTemporaryBytes: 2*stemWorkingBytes - 1,
			},
			kind:   ResourceTemporaryBytes,
			actual: 2 * stemWorkingBytes,
		},
		"nodes": {
			limits: Limits{
				MaxStems:          2,
				MaxNodes:          3,
				MaxEdges:          3,
				MaxTemporaryBytes: 4096,
			},
			kind:   ResourceNodes,
			actual: 4,
		},
		"edges": {
			limits: Limits{
				MaxStems:          2,
				MaxNodes:          4,
				MaxEdges:          2,
				MaxTemporaryBytes: 4096,
			},
			kind:   ResourceEdges,
			actual: 3,
		},
		"complete temporary bytes": {
			limits: Limits{
				MaxStems: 2,
				MaxNodes: 4,
				MaxEdges: 3,
				MaxTemporaryBytes: 2*stemWorkingBytes +
					4*nodeWorkingBytes +
					3*edgeWorkingBytes - 1,
			},
			kind: ResourceTemporaryBytes,
			actual: 2*stemWorkingBytes +
				4*nodeWorkingBytes +
				3*edgeWorkingBytes,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := Build(
				context.Background(),
				[]Stem{first, second},
				test.limits,
			)
			var resourceErr *ResourceError
			if !errors.As(err, &resourceErr) {
				t.Fatalf("build error = %v, want ResourceError", err)
			}
			if resourceErr.Kind != test.kind ||
				resourceErr.Actual != test.actual {
				t.Fatalf(
					"resource error = %#v, want kind %d actual %d",
					resourceErr,
					test.kind,
					test.actual,
				)
			}
			if !errors.Is(err, errResourceExhausted) ||
				resourceErr.Error() == "" {
				t.Fatalf("resource error does not expose sentinel: %v", err)
			}
		})
	}
}

func TestLayoutReportsDeterministicConstructionBytes(t *testing.T) {
	t.Parallel()

	layout := buildLayout(t, []Stem{{0x10, 0x20}, {0x10, 0x30}})
	want := 2*2*uint64(len(Stem{})) + 4*nodeWorkingBytes + 3*edgeWorkingBytes
	if got := layout.TemporaryBytes(); got != want {
		t.Fatalf("temporary bytes = %d, want %d", got, want)
	}
	if got := layoutBytes(2, 4, 3); got != want {
		t.Fatalf("layout bytes = %d, want %d", got, want)
	}
}

func TestLayoutFailsClosedForCorruptOwnedTopology(t *testing.T) {
	t.Parallel()

	t.Run("invalid internal depth", func(t *testing.T) {
		t.Parallel()

		layout := cloneLayout(buildLayout(
			t,
			[]Stem{{0x10, 0x20}, {0x10, 0x30}},
		))
		layout.nodes[1].depth = 31
		if _, err := layout.Lookup(context.Background(), Stem{0x10, 0x20}); !errors.Is(err, errInvalidLayout) {
			t.Fatalf("lookup error = %v, want %v", err, errInvalidLayout)
		}
	})

	t.Run("invalid retained limits", func(t *testing.T) {
		t.Parallel()

		layout := cloneLayout(buildLayout(t, nil))
		layout.limits = Limits{}
		if _, err := layout.Lookup(context.Background(), Stem{}); !errors.Is(err, errInvalidLayout) {
			t.Fatalf("lookup error = %v, want %v", err, errInvalidLayout)
		}
	})

	t.Run("missing root", func(t *testing.T) {
		t.Parallel()

		layout := cloneLayout(buildLayout(t, nil))
		layout.nodes = nil
		if _, err := layout.Lookup(context.Background(), Stem{}); !errors.Is(err, errInvalidLayout) {
			t.Fatalf("lookup error = %v, want %v", err, errInvalidLayout)
		}
	})

	t.Run("invalid root kind", func(t *testing.T) {
		t.Parallel()

		layout := cloneLayout(buildLayout(t, nil))
		layout.nodes[0].kind = KindStem
		if _, err := layout.Lookup(context.Background(), Stem{}); !errors.Is(err, errInvalidLayout) {
			t.Fatalf("lookup error = %v, want %v", err, errInvalidLayout)
		}
	})

	t.Run("invalid root depth", func(t *testing.T) {
		t.Parallel()

		layout := cloneLayout(buildLayout(t, nil))
		layout.nodes[0].depth = 1
		if _, err := layout.Lookup(context.Background(), Stem{}); !errors.Is(err, errInvalidLayout) {
			t.Fatalf("lookup error = %v, want %v", err, errInvalidLayout)
		}
	})

	t.Run("invalid child kind", func(t *testing.T) {
		t.Parallel()

		layout := cloneLayout(buildLayout(t, []Stem{{0x10}}))
		layout.nodes[1].kind = Kind(0xff)
		if _, err := layout.Lookup(context.Background(), Stem{0x10}); !errors.Is(err, errInvalidLayout) {
			t.Fatalf("lookup error = %v, want %v", err, errInvalidLayout)
		}
	})

	t.Run("edge range", func(t *testing.T) {
		t.Parallel()

		layout := buildLayout(t, nil)
		_, _, err := layout.findChild(
			context.Background(),
			node{kind: KindInternal, firstEdge: 1, edgeCount: 1},
			0,
		)
		if !errors.Is(err, errInvalidLayout) {
			t.Fatalf("find child error = %v, want %v", err, errInvalidLayout)
		}
	})

	t.Run("edge range arithmetic overflow", func(t *testing.T) {
		t.Parallel()

		if _, _, ok := checkedEdgeRange(
			uint32(math.MaxInt32),
			1,
			0,
		); ok {
			t.Fatal("overflowing edge range accepted")
		}
		if _, _, ok := checkedEdgeRange(0, 0, -1); ok {
			t.Fatal("negative edge length accepted")
		}
		layout := buildLayout(t, nil)
		_, _, err := layout.findChild(
			context.Background(),
			node{
				kind:      KindInternal,
				firstEdge: uint32(math.MaxInt32),
				edgeCount: 1,
			},
			0,
		)
		if !errors.Is(err, errInvalidLayout) {
			t.Fatalf("find child error = %v, want %v", err, errInvalidLayout)
		}
	})

	t.Run("child index", func(t *testing.T) {
		t.Parallel()

		layout := cloneLayout(buildLayout(t, []Stem{{0x10}}))
		layout.edges[0].child = uint32(len(layout.nodes))
		if _, err := layout.Lookup(context.Background(), Stem{0x10}); !errors.Is(err, errInvalidLayout) {
			t.Fatalf("lookup error = %v, want %v", err, errInvalidLayout)
		}
	})

	t.Run("count mismatch", func(t *testing.T) {
		t.Parallel()

		layout := buildLayout(t, nil)
		if _, err := finalizeLayout(
			layout,
			topologyCounts{nodes: 2},
		); !errors.Is(err, errInvalidLayout) {
			t.Fatalf("finalize error = %v, want %v", err, errInvalidLayout)
		}
	})

	t.Run("impossible depth collision", func(t *testing.T) {
		t.Parallel()

		stems := []Stem{{}, {}}
		counts := topologyCounts{}
		if err := countChildren(
			context.Background(),
			stems,
			30,
			&counts,
		); !errors.Is(err, errDuplicateStem) {
			t.Fatalf("count error = %v, want %v", err, errDuplicateStem)
		}
	})
}

func TestLayoutChecksCancellationDuringBoundedWork(t *testing.T) {
	t.Parallel()

	stems := []Stem{{0x10, 0x20}, {0x10, 0x30}}

	tests := map[string]func(context.Context) error{
		"sort preflight": func(ctx context.Context) error {
			values := append([]Stem(nil), stems...)
			return sortStems(ctx, values)
		},
		"build owned sort": func(ctx context.Context) error {
			_, err := buildOwned(ctx, append([]Stem(nil), stems...), testLimits())
			return err
		},
		"count": func(ctx context.Context) error {
			counts := topologyCounts{nodes: 1}
			return countChildren(ctx, stems, 0, &counts)
		},
		"child first pass": func(ctx context.Context) error {
			layout := Layout{nodes: []node{{kind: KindInternal}}}
			return layout.buildChildren(ctx, 0, stems, 0)
		},
		"find child": func(ctx context.Context) error {
			layout := buildLayout(t, []Stem{{0x10}})
			_, _, err := layout.findChild(ctx, layout.nodes[0], 0x10)
			return err
		},
	}
	for name, operation := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := operation(cancelledContext()); !errors.Is(err, errCancelled) {
				t.Fatalf("operation error = %v, want cancellation", err)
			}
		})
	}

	t.Run("duplicate scan", func(t *testing.T) {
		t.Parallel()

		ctx := &cancelAfterContext{cancelAt: 3}
		_, err := buildOwned(
			ctx,
			[]Stem{{0x02}, {0x01}},
			testLimits(),
		)
		if !errors.Is(err, errCancelled) {
			t.Fatalf("build error = %v, want cancellation", err)
		}
	})

	t.Run("recursive count", func(t *testing.T) {
		t.Parallel()

		ctx := &cancelAfterContext{cancelAt: 2}
		counts := topologyCounts{nodes: 1}
		if err := countChildren(ctx, stems, 0, &counts); !errors.Is(err, errCancelled) {
			t.Fatalf("count error = %v, want cancellation", err)
		}
	})

	t.Run("child second pass", func(t *testing.T) {
		t.Parallel()

		ctx := &cancelAfterContext{cancelAt: 2}
		layout := Layout{nodes: []node{{kind: KindInternal}}}
		if err := layout.buildChildren(ctx, 0, []Stem{{0x10}}, 0); !errors.Is(err, errCancelled) {
			t.Fatalf("build children error = %v, want cancellation", err)
		}
	})

	t.Run("recursive child", func(t *testing.T) {
		t.Parallel()

		ctx := &cancelAfterContext{cancelAt: 3}
		layout := Layout{nodes: []node{{kind: KindInternal}}}
		if err := layout.buildChildren(ctx, 0, stems, 0); !errors.Is(err, errCancelled) {
			t.Fatalf("build children error = %v, want cancellation", err)
		}
	})

	t.Run("complete build sweep", func(t *testing.T) {
		t.Parallel()

		completed := false
		for cancelAt := 1; cancelAt < 1_000; cancelAt++ {
			_, err := Build(
				&cancelAfterContext{cancelAt: cancelAt},
				stems,
				testLimits(),
			)
			if err == nil {
				completed = true
				break
			}
			if !errors.Is(err, errCancelled) {
				t.Fatalf("cancellation at check %d = %v", cancelAt, err)
			}
		}
		if !completed {
			t.Fatal("cancellation sweep did not reach a completed build")
		}
	})

	t.Run("owned child construction", func(t *testing.T) {
		t.Parallel()

		ctx := &cancelAfterContext{cancelAt: 4}
		_, err := buildOwned(ctx, []Stem{{0x10}}, testLimits())
		if !errors.Is(err, errCancelled) {
			t.Fatalf("build error = %v, want cancellation", err)
		}
	})

	t.Run("owned count", func(t *testing.T) {
		t.Parallel()

		ctx := &cancelAfterContext{cancelAt: 3}
		_, err := buildOwned(ctx, []Stem{{0x10}}, testLimits())
		if !errors.Is(err, errCancelled) {
			t.Fatalf("build error = %v, want cancellation", err)
		}
	})

	t.Run("lookup loop", func(t *testing.T) {
		t.Parallel()

		layout := buildLayout(t, []Stem{{0x10}})
		if _, err := layout.Lookup(
			&cancelAfterContext{cancelAt: 2},
			Stem{0x10},
		); !errors.Is(err, errCancelled) {
			t.Fatalf("lookup error = %v, want cancellation", err)
		}
	})

	t.Run("lookup child search", func(t *testing.T) {
		t.Parallel()

		layout := buildLayout(t, []Stem{{0x10}})
		if _, err := layout.Lookup(
			&cancelAfterContext{cancelAt: 3},
			Stem{0x10},
		); !errors.Is(err, errCancelled) {
			t.Fatalf("lookup error = %v, want cancellation", err)
		}
	})

	t.Run("sort postcheck", func(t *testing.T) {
		t.Parallel()

		values := []Stem{{0x01}, {0x02}}
		if err := sortStems(
			&cancelAfterContext{cancelAt: 2},
			values,
		); !errors.Is(err, errCancelled) {
			t.Fatalf("sort error = %v, want cancellation", err)
		}
	})

	t.Run("sort work", func(t *testing.T) {
		t.Parallel()

		values := []Stem{{0x04}, {0x03}, {0x02}, {0x01}}
		if err := sortStems(
			&cancelAfterContext{cancelAt: 3},
			values,
		); !errors.Is(err, errCancelled) {
			t.Fatalf("sort error = %v, want cancellation during sorting", err)
		}
	})

	t.Run("sort chooses greater right child", func(t *testing.T) {
		t.Parallel()

		values := []Stem{{0x03}, {0x01}, {0x02}}
		if err := sortStems(context.Background(), values); err != nil {
			t.Fatalf("sort: %v", err)
		}
		if want := []Stem{{0x01}, {0x02}, {0x03}}; !reflect.DeepEqual(values, want) {
			t.Fatalf("sorted stems = %x, want %x", values, want)
		}
	})
}

func TestLayoutMutationOperationsPropagateResourceAndCancellationErrors(
	t *testing.T,
) {
	t.Parallel()

	t.Run("insert stem count", func(t *testing.T) {
		t.Parallel()

		layout := cloneLayout(buildLayout(t, []Stem{{0x01}}))
		layout.limits.MaxStems = 1
		if _, _, err := layout.Insert(context.Background(), Stem{0x02}); !errors.Is(err, errResourceExhausted) {
			t.Fatalf("insert error = %v, want resource exhaustion", err)
		}
	})

	t.Run("insert bytes", func(t *testing.T) {
		t.Parallel()

		layout := cloneLayout(buildLayout(t, []Stem{{0x01}}))
		layout.limits.MaxTemporaryBytes = 1
		if _, _, err := layout.Insert(context.Background(), Stem{0x02}); !errors.Is(err, errResourceExhausted) {
			t.Fatalf("insert error = %v, want resource exhaustion", err)
		}
	})

	t.Run("insert rebuild", func(t *testing.T) {
		t.Parallel()

		layout := buildLayout(t, nil)
		if _, _, err := layout.Insert(
			&cancelAfterContext{cancelAt: 2},
			Stem{0x01},
		); !errors.Is(err, errCancelled) {
			t.Fatalf("insert error = %v, want cancellation", err)
		}
	})

	t.Run("delete bytes", func(t *testing.T) {
		t.Parallel()

		layout := cloneLayout(buildLayout(
			t,
			[]Stem{{0x01}, {0x02}},
		))
		layout.limits.MaxTemporaryBytes = 1
		if _, _, err := layout.Delete(context.Background(), Stem{0x02}); !errors.Is(err, errResourceExhausted) {
			t.Fatalf("delete error = %v, want resource exhaustion", err)
		}
	})

	t.Run("delete rebuild", func(t *testing.T) {
		t.Parallel()

		layout := buildLayout(t, []Stem{{0x01}, {0x02}})
		if _, _, err := layout.Delete(
			&cancelAfterContext{cancelAt: 2},
			Stem{0x02},
		); !errors.Is(err, errCancelled) {
			t.Fatalf("delete error = %v, want cancellation", err)
		}
	})
}

func TestSortStemsMatchesCanonicalOrderForSmallPermutations(t *testing.T) {
	t.Parallel()

	const count = 6
	values := make([]Stem, count)
	for index := range values {
		values[index][0] = uint8(index)
	}

	var verify func(int)
	verify = func(position int) {
		if position == len(values) {
			got := append([]Stem(nil), values...)
			if err := sortStems(context.Background(), got); err != nil {
				t.Fatalf("sort permutation %x: %v", values, err)
			}
			for index := range got {
				if got[index][0] != uint8(index) {
					t.Fatalf(
						"sorted permutation %x = %x",
						values,
						got,
					)
				}
			}
			return
		}
		for index := position; index < len(values); index++ {
			values[position], values[index] = values[index], values[position]
			verify(position + 1)
			values[position], values[index] = values[index], values[position]
		}
	}
	verify(0)
}

func TestSortStemsHonorsEveryCancellationBoundary(t *testing.T) {
	t.Parallel()

	original := []Stem{{0x08}, {0x07}, {0x06}, {0x05}, {0x04}, {0x03}, {0x02}, {0x01}}
	completed := false
	for cancelAt := 1; cancelAt < 1_000; cancelAt++ {
		got := append([]Stem(nil), original...)
		err := sortStems(&cancelAfterContext{cancelAt: cancelAt}, got)
		if err == nil {
			for index := range got {
				if got[index][0] != uint8(index+1) {
					t.Fatalf("sorted stems = %x", got)
				}
			}
			completed = true
			break
		}
		if !errors.Is(err, errCancelled) {
			t.Fatalf("cancellation at check %d = %v", cancelAt, err)
		}
	}
	if !completed {
		t.Fatal("cancellation sweep did not reach a completed sort")
	}
}

func buildLayout(t *testing.T, stems []Stem) Layout {
	t.Helper()

	layout, err := Build(context.Background(), stems, testLimits())
	if err != nil {
		t.Fatalf("build layout: %v", err)
	}

	return layout
}

func cloneLayout(layout Layout) Layout {
	layout.stems = append([]Stem(nil), layout.stems...)
	layout.nodes = append([]node(nil), layout.nodes...)
	layout.edges = append([]edge(nil), layout.edges...)

	return layout
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	return ctx
}

type cancelAfterContext struct {
	calls    int
	cancelAt int
}

func (*cancelAfterContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (*cancelAfterContext) Done() <-chan struct{} {
	return nil
}

func (ctx *cancelAfterContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.cancelAt {
		return context.Canceled
	}

	return nil
}

func (*cancelAfterContext) Value(any) any {
	return nil
}

func testLimits() Limits {
	return Limits{
		MaxStems:          64,
		MaxNodes:          2048,
		MaxEdges:          2048,
		MaxTemporaryBytes: 1 << 20,
	}
}

func assertLayoutCounts(
	t *testing.T,
	layout Layout,
	stems int,
	nodes int,
	edges int,
) {
	t.Helper()

	if layout.StemCount() != stems ||
		layout.NodeCount() != nodes ||
		layout.EdgeCount() != edges {
		t.Fatalf(
			"layout counts = stems %d nodes %d edges %d, want %d/%d/%d",
			layout.StemCount(),
			layout.NodeCount(),
			layout.EdgeCount(),
			stems,
			nodes,
			edges,
		)
	}
}

func assertLookup(t *testing.T, layout Layout, stem Stem, want Result) {
	t.Helper()

	got, err := layout.Lookup(context.Background(), stem)
	if err != nil {
		t.Fatalf("lookup %x: %v", stem, err)
	}
	if got != want {
		t.Fatalf("lookup %x = %#v, want %#v", stem, got, want)
	}
}
