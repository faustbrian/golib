package merkletree

import (
	"context"
	"errors"
	"testing"
)

func TestRootBuilderRejectsInvalidStateLimitsAndContexts(t *testing.T) {
	t.Parallel()

	if _, err := NewRootBuilder(Profile{}, DefaultLimits()); !errors.Is(
		err,
		ErrUnsupportedProfile,
	) {
		t.Fatalf("NewRootBuilder(invalid profile) error = %v", err)
	}
	if _, err := NewRootBuilder(
		CanonicalProfile(),
		Limits{},
	); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("NewRootBuilder(invalid limits) error = %v", err)
	}

	var zero RootBuilder
	if err := zero.Append(
		context.Background(),
		NewRawLeaf(nil),
	); !errors.Is(err, ErrInvalidRootBuilder) {
		t.Fatalf("zero Append() error = %v", err)
	}
	if _, err := zero.Root(context.Background()); !errors.Is(
		err,
		ErrInvalidRootBuilder,
	) {
		t.Fatalf("zero Root() error = %v", err)
	}
	if err := (*RootBuilder)(nil).validate(); !errors.Is(
		err,
		ErrInvalidRootBuilder,
	) {
		t.Fatalf("nil validate() error = %v", err)
	}

	builder, err := NewRootBuilder(CanonicalProfile(), DefaultLimits())
	if err != nil {
		t.Fatalf("NewRootBuilder() error = %v", err)
	}
	var nilContext context.Context
	if err := builder.Append(nilContext, NewRawLeaf(nil)); !errors.Is(
		err,
		ErrInvalidContext,
	) {
		t.Fatalf("Append(nil context) error = %v", err)
	}
	if _, err := builder.Root(nilContext); !errors.Is(
		err,
		ErrInvalidContext,
	) {
		t.Fatalf("Root(nil context) error = %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := builder.Append(cancelled, NewRawLeaf(nil)); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("Append(cancelled) error = %v", err)
	}
	if _, err := builder.Root(cancelled); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("Root(cancelled) error = %v", err)
	}
}

func TestRootBuilderResourceBoundsAndAtomicFailures(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxLeaves = 2
	limits.MaxLeafBytes = 3
	limits.MaxTotalBytes = 6
	builder, err := NewRootBuilder(CanonicalProfile(), limits)
	if err != nil {
		t.Fatalf("NewRootBuilder() error = %v", err)
	}
	if err := builder.AppendBatch(context.Background(), []RawLeaf{
		NewRawLeaf([]byte("abc")),
		NewRawLeaf([]byte("def")),
	}); err != nil {
		t.Fatalf("AppendBatch(exact) error = %v", err)
	}
	before, err := builder.Root(context.Background())
	if err != nil {
		t.Fatalf("Root(before failures) error = %v", err)
	}

	tests := []struct {
		name    string
		builder *RootBuilder
		leaf    RawLeaf
		kind    ResourceKind
		actual  uint64
	}{
		{
			name:    "leaf count",
			builder: builder,
			leaf:    NewRawLeaf(nil),
			kind:    ResourceLeaves,
			actual:  3,
		},
		{
			name: "leaf bytes",
			builder: mustRootBuilder(t, Limits{
				MaxLeaves:     2,
				MaxLeafBytes:  3,
				MaxTotalBytes: 6,
			}),
			leaf:   NewRawLeaf([]byte("four")),
			kind:   ResourceLeafBytes,
			actual: 4,
		},
		{
			name: "total bytes",
			builder: appendedRootBuilder(
				t,
				Limits{
					MaxLeaves:     2,
					MaxLeafBytes:  3,
					MaxTotalBytes: 4,
				},
				NewRawLeaf([]byte("abc")),
			),
			leaf:   NewRawLeaf([]byte("de")),
			kind:   ResourceTotalBytes,
			actual: 5,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.builder.Append(context.Background(), test.leaf)
			var resourceErr *ResourceError
			if !errors.As(err, &resourceErr) ||
				resourceErr.Kind != test.kind ||
				resourceErr.Actual != test.actual {
				t.Fatalf(
					"Append() error = %v, want resource %d actual %d",
					err,
					test.kind,
					test.actual,
				)
			}
		})
	}

	after, err := builder.Root(context.Background())
	if err != nil {
		t.Fatalf("Root(after failures) error = %v", err)
	}
	if before.digest.value != after.digest.value ||
		before.treeSize != after.treeSize {
		t.Fatal("resource failure changed builder state")
	}

	varied := mustRootBuilder(t, Limits{
		MaxLeaves:     3,
		MaxLeafBytes:  3,
		MaxTotalBytes: 5,
	})
	err = varied.AppendBatch(context.Background(), []RawLeaf{
		NewRawLeaf([]byte("a")),
		NewRawLeaf([]byte("bb")),
		NewRawLeaf([]byte("ccc")),
	})
	var resourceErr *ResourceError
	if !errors.As(err, &resourceErr) ||
		resourceErr.Kind != ResourceTotalBytes ||
		resourceErr.Actual != 6 {
		t.Fatalf(
			"AppendBatch(varied bytes) error = %v, want total-byte actual 6",
			err,
		)
	}
}

func TestRootBuilderHonorsEveryCancellationCheckpoint(t *testing.T) {
	t.Parallel()

	batch := make([]RawLeaf, 65)
	for index := range batch {
		batch[index] = NewRawLeaf([]byte{byte(index)})
	}
	appendSucceeded := false
	var completed *RootBuilder
	for allowedCalls := 0; allowedCalls < 300; allowedCalls++ {
		builder, err := NewRootBuilder(CanonicalProfile(), DefaultLimits())
		if err != nil {
			t.Fatalf("NewRootBuilder() error = %v", err)
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
		root, rootErr := builder.Root(context.Background())
		if rootErr != nil {
			t.Fatalf("Root() after cancellation error = %v", rootErr)
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
	if completed == nil {
		t.Fatal("successful append did not retain the completed builder")
	}

	rootSucceeded := false
	for allowedCalls := 0; allowedCalls < 100; allowedCalls++ {
		ctx := &checkpointContext{
			remaining: allowedCalls,
			done:      make(chan struct{}),
		}
		_, err := completed.Root(ctx)
		if err == nil {
			rootSucceeded = true

			break
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("root checkpoint %d: error = %v", allowedCalls, err)
		}
	}
	if !rootSucceeded {
		t.Fatal("Root never passed all cancellation checkpoints")
	}
}

func TestRootBuilderValidationRejectsCorruptState(t *testing.T) {
	t.Parallel()

	valid, err := NewRootBuilder(CanonicalProfile(), DefaultLimits())
	if err != nil {
		t.Fatalf("NewRootBuilder() error = %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*RootBuilder)
	}{
		{
			name: "invalid profile",
			mutate: func(builder *RootBuilder) {
				builder.profile = Profile{}
			},
		},
		{
			name: "invalid limits",
			mutate: func(builder *RootBuilder) {
				builder.limits = Limits{}
			},
		},
		{
			name: "tree size limit",
			mutate: func(builder *RootBuilder) {
				builder.treeSize = builder.limits.MaxLeaves + 1
			},
		},
		{
			name: "total byte limit",
			mutate: func(builder *RootBuilder) {
				builder.totalBytes = builder.limits.MaxTotalBytes + 1
			},
		},
		{
			name: "empty frontier",
			mutate: func(builder *RootBuilder) {
				builder.frontier = append(
					builder.frontier,
					[32]byte{},
				)
			},
		},
		{
			name: "wrong frontier cardinality",
			mutate: func(builder *RootBuilder) {
				builder.treeSize = 1
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			builder := *valid
			test.mutate(&builder)
			if err := builder.validate(); !errors.Is(
				err,
				ErrInvalidRootBuilder,
			) {
				t.Fatalf("validate() error = %v", err)
			}
		})
	}
}

func mustRootBuilder(t *testing.T, limits Limits) *RootBuilder {
	t.Helper()

	builder, err := NewRootBuilder(CanonicalProfile(), limits)
	if err != nil {
		t.Fatalf("NewRootBuilder() error = %v", err)
	}

	return builder
}

func appendedRootBuilder(
	t *testing.T,
	limits Limits,
	leaf RawLeaf,
) *RootBuilder {
	t.Helper()

	builder := mustRootBuilder(t, limits)
	if err := builder.Append(context.Background(), leaf); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	return builder
}
