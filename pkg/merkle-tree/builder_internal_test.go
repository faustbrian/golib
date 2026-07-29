package merkletree

import (
	"context"
	"errors"
	"testing"
)

func TestBuilderAppendBatchAndSnapshotHonorEveryCancellationCheckpoint(
	t *testing.T,
) {
	t.Parallel()

	limits := DefaultSnapshotLimits()
	batch := []RawLeaf{
		NewRawLeaf([]byte("second")),
		NewRawLeaf([]byte("third")),
		NewRawLeaf([]byte("fourth")),
		NewRawLeaf([]byte("fifth")),
	}
	appendSucceeded := false
	completed := &Builder{}
	for allowedCalls := 0; allowedCalls < 100; allowedCalls++ {
		builder, err := NewBuilder(CanonicalProfile(), limits)
		if err != nil {
			t.Fatalf("NewBuilder() error = %v", err)
		}
		if err := builder.Append(
			context.Background(),
			NewRawLeaf([]byte("first")),
		); err != nil {
			t.Fatalf("initial Append() error = %v", err)
		}
		ctx := &checkpointContext{
			remaining: allowedCalls,
			done:      make(chan struct{}),
		}
		err = builder.AppendBatch(ctx, batch)
		if err == nil {
			completed = builder
			appendSucceeded = true

			break
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("append checkpoint %d: error = %v", allowedCalls, err)
		}
		snapshot, snapshotErr := builder.Snapshot(context.Background())
		if snapshotErr != nil {
			t.Fatalf("snapshot after cancellation error = %v", snapshotErr)
		}
		root, rootErr := snapshot.Root()
		if rootErr != nil {
			t.Fatalf("root after cancellation error = %v", rootErr)
		}
		if root.TreeSize() != 1 {
			t.Fatalf(
				"append checkpoint %d changed size to %d",
				allowedCalls,
				root.TreeSize(),
			)
		}
	}
	if !appendSucceeded {
		t.Fatal("append never passed all cancellation checkpoints")
	}

	snapshotSucceeded := false
	for allowedCalls := 0; allowedCalls < 100; allowedCalls++ {
		ctx := &checkpointContext{
			remaining: allowedCalls,
			done:      make(chan struct{}),
		}
		_, err := completed.Snapshot(ctx)
		if err == nil {
			snapshotSucceeded = true

			break
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("snapshot checkpoint %d: error = %v", allowedCalls, err)
		}
	}
	if !snapshotSucceeded {
		t.Fatal("snapshot never passed all cancellation checkpoints")
	}
}

func TestBuilderSnapshotSupportsEmptyTreeAndRetainedNodeLimit(t *testing.T) {
	t.Parallel()

	builder, err := NewBuilder(CanonicalProfile(), DefaultSnapshotLimits())
	if err != nil {
		t.Fatalf("NewBuilder() error = %v", err)
	}
	empty, err := builder.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot(empty) error = %v", err)
	}
	root, err := empty.Root()
	if err != nil {
		t.Fatalf("Root(empty) error = %v", err)
	}
	if root.TreeSize() != 0 {
		t.Fatalf("empty tree size = %d, want 0", root.TreeSize())
	}

	limits := DefaultSnapshotLimits()
	limits.MaxRetainedNodes = 1
	builder, err = NewBuilder(CanonicalProfile(), limits)
	if err != nil {
		t.Fatalf("NewBuilder(retained limit) error = %v", err)
	}
	err = builder.AppendBatch(context.Background(), []RawLeaf{
		NewRawLeaf([]byte("first")),
		NewRawLeaf([]byte("second")),
	})
	var resourceErr *ResourceError
	if !errors.As(err, &resourceErr) ||
		resourceErr.Kind != ResourceRetainedNodes {
		t.Fatalf(
			"AppendBatch() error = %v, want retained-node ResourceError",
			err,
		)
	}
}

func TestBuilderValidationRejectsCorruptInternalState(t *testing.T) {
	t.Parallel()

	limits := DefaultSnapshotLimits()
	valid, err := NewBuilder(CanonicalProfile(), limits)
	if err != nil {
		t.Fatalf("NewBuilder() error = %v", err)
	}
	if err := (*Builder)(nil).validate(); !errors.Is(err, ErrInvalidBuilder) {
		t.Fatalf("nil validate() error = %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*Builder)
	}{
		{
			name: "empty nodes",
			mutate: func(builder *Builder) {
				builder.nodes = []snapshotNode{{}}
			},
		},
		{
			name: "missing nodes",
			mutate: func(builder *Builder) {
				builder.treeSize = 1
			},
		},
		{
			name: "wrong frontier cardinality",
			mutate: func(builder *Builder) {
				builder.treeSize = 1
				builder.nodes = []snapshotNode{{size: 1}}
			},
		},
		{
			name: "frontier index",
			mutate: func(builder *Builder) {
				builder.treeSize = 1
				builder.nodes = []snapshotNode{{size: 1}}
				builder.frontier = []uint64{1}
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			builder := *valid
			test.mutate(&builder)
			if err := builder.validate(); !errors.Is(
				err,
				ErrInvalidBuilder,
			) {
				t.Fatalf("validate() error = %v", err)
			}
		})
	}
}
