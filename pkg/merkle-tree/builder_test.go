package merkletree_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	merkletree "github.com/faustbrian/golib/pkg/merkle-tree"
)

func TestBuilderAppendSnapshotsMatchBatchConstruction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := merkletree.CanonicalProfile()
	limits := merkletree.DefaultSnapshotLimits()
	builder, err := merkletree.NewBuilder(profile, limits)
	if err != nil {
		t.Fatalf("NewBuilder() error = %v", err)
	}

	leaves := []merkletree.RawLeaf{
		merkletree.NewRawLeaf([]byte("alpha")),
		merkletree.NewRawLeaf([]byte("beta")),
		merkletree.NewRawLeaf([]byte("gamma")),
		merkletree.NewRawLeaf([]byte("delta")),
		merkletree.NewRawLeaf([]byte("epsilon")),
	}
	if err := builder.Append(ctx, leaves[0]); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	first, err := builder.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot() after Append error = %v", err)
	}
	if err := builder.AppendBatch(ctx, leaves[1:]); err != nil {
		t.Fatalf("AppendBatch() error = %v", err)
	}
	current, err := builder.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot() after AppendBatch error = %v", err)
	}
	want, err := merkletree.NewSnapshot(ctx, profile, leaves, limits)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}

	assertSameRoot(t, current, want)

	firstRoot, err := first.Root()
	if err != nil {
		t.Fatalf("first.Root() error = %v", err)
	}
	wantFirst, err := merkletree.ComputeRoot(
		ctx,
		profile,
		leaves[:1],
		limits.Construction,
	)
	if err != nil {
		t.Fatalf("ComputeRoot(first) error = %v", err)
	}
	if !bytes.Equal(firstRoot.Digest().Bytes(), wantFirst.Digest().Bytes()) {
		t.Fatal("an earlier snapshot changed after a later append")
	}

	for index, leaf := range leaves {
		proof, proofErr := current.InclusionProof(
			ctx,
			uint64(index),
			merkletree.DefaultProofLimits(),
		)
		if proofErr != nil {
			t.Fatalf("InclusionProof(%d) error = %v", index, proofErr)
		}
		if verifyErr := merkletree.VerifyInclusion(
			ctx,
			proof,
			leaf,
			merkletree.DefaultProofLimits(),
		); verifyErr != nil {
			t.Fatalf("VerifyInclusion(%d) error = %v", index, verifyErr)
		}
	}
}

func TestBuilderMatchesBatchConstructionForEverySmallPrefixAndProfile(
	t *testing.T,
) {
	t.Parallel()

	rfc, err := merkletree.RFC9162Profile(merkletree.HashSHA256)
	if err != nil {
		t.Fatalf("RFC9162Profile() error = %v", err)
	}
	profiles := []merkletree.Profile{
		merkletree.CanonicalProfile(),
		rfc,
	}
	leaves := make([]merkletree.RawLeaf, 65)
	for index := range leaves {
		leaves[index] = merkletree.NewRawLeaf([]byte{
			byte(index),
			byte(index * 17),
		})
	}

	for _, profile := range profiles {
		builder, builderErr := merkletree.NewBuilder(
			profile,
			merkletree.DefaultSnapshotLimits(),
		)
		if builderErr != nil {
			t.Fatalf("NewBuilder() error = %v", builderErr)
		}
		for size := 0; ; {
			got, snapshotErr := builder.Snapshot(context.Background())
			if snapshotErr != nil {
				t.Fatalf(
					"Snapshot(profile %d, size %d) error = %v",
					profile.ID(),
					size,
					snapshotErr,
				)
			}
			want, wantErr := merkletree.NewSnapshot(
				context.Background(),
				profile,
				leaves[:size],
				merkletree.DefaultSnapshotLimits(),
			)
			if wantErr != nil {
				t.Fatalf(
					"NewSnapshot(profile %d, size %d) error = %v",
					profile.ID(),
					size,
					wantErr,
				)
			}
			assertSameRoot(t, got, want)
			if size == len(leaves) {
				break
			}

			end := min(size+1+(size%3), len(leaves))
			if appendErr := builder.AppendBatch(
				context.Background(),
				leaves[size:end],
			); appendErr != nil {
				t.Fatalf(
					"AppendBatch(profile %d, size %d) error = %v",
					profile.ID(),
					size,
					appendErr,
				)
			}
			size = end
		}
	}
}

func TestBuilderBatchAppendIsAtomic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	limits := merkletree.DefaultSnapshotLimits()
	limits.Construction.MaxLeaves = 2
	limits.Construction.MaxLeafBytes = 4
	limits.Construction.MaxTotalBytes = 6
	limits.MaxRetainedNodes = 3
	builder, err := merkletree.NewBuilder(merkletree.CanonicalProfile(), limits)
	if err != nil {
		t.Fatalf("NewBuilder() error = %v", err)
	}
	if err := builder.Append(ctx, merkletree.NewRawLeaf([]byte("one"))); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	before, err := builder.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot(before) error = %v", err)
	}
	err = builder.AppendBatch(ctx, []merkletree.RawLeaf{
		merkletree.NewRawLeaf([]byte("two")),
		merkletree.NewRawLeaf([]byte("toolong")),
	})
	var resourceErr *merkletree.ResourceError
	if !errors.As(err, &resourceErr) ||
		resourceErr.Kind != merkletree.ResourceLeaves {
		t.Fatalf("AppendBatch() error = %v, want leaves ResourceError", err)
	}
	after, err := builder.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot(after) error = %v", err)
	}
	assertSameRoot(t, after, before)

	if err := builder.AppendBatch(ctx, nil); err != nil {
		t.Fatalf("AppendBatch(nil) error = %v", err)
	}
}

func TestBuilderAllowsExactResourceBoundsAndCountsBatchBytes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	exact := merkletree.DefaultSnapshotLimits()
	exact.Construction.MaxLeaves = 2
	exact.Construction.MaxLeafBytes = 3
	exact.Construction.MaxTotalBytes = 6
	exact.MaxRetainedNodes = 3
	builder, err := merkletree.NewBuilder(
		merkletree.CanonicalProfile(),
		exact,
	)
	if err != nil {
		t.Fatalf("NewBuilder(exact) error = %v", err)
	}
	if err := builder.AppendBatch(ctx, []merkletree.RawLeaf{
		merkletree.NewRawLeaf([]byte("abc")),
		merkletree.NewRawLeaf([]byte("def")),
	}); err != nil {
		t.Fatalf("AppendBatch(exact) error = %v", err)
	}
	if _, err := builder.Snapshot(ctx); err != nil {
		t.Fatalf("Snapshot(exact) error = %v", err)
	}

	cumulative := exact
	cumulative.Construction.MaxLeaves = 3
	cumulative.Construction.MaxTotalBytes = 5
	cumulative.MaxRetainedNodes = 5
	builder, err = merkletree.NewBuilder(
		merkletree.CanonicalProfile(),
		cumulative,
	)
	if err != nil {
		t.Fatalf("NewBuilder(cumulative) error = %v", err)
	}
	err = builder.AppendBatch(ctx, []merkletree.RawLeaf{
		merkletree.NewRawLeaf([]byte("abc")),
		merkletree.NewRawLeaf([]byte("def")),
	})
	var resourceErr *merkletree.ResourceError
	if !errors.As(err, &resourceErr) ||
		resourceErr.Kind != merkletree.ResourceTotalBytes ||
		resourceErr.Actual != 6 {
		t.Fatalf(
			"AppendBatch(cumulative) error = %v, want total-byte actual 6",
			err,
		)
	}

	cumulative.Construction.MaxTotalBytes = 7
	builder, err = merkletree.NewBuilder(
		merkletree.CanonicalProfile(),
		cumulative,
	)
	if err != nil {
		t.Fatalf("NewBuilder(successful cumulative) error = %v", err)
	}
	if err := builder.AppendBatch(ctx, []merkletree.RawLeaf{
		merkletree.NewRawLeaf([]byte("abc")),
		merkletree.NewRawLeaf([]byte("def")),
	}); err != nil {
		t.Fatalf("AppendBatch(successful cumulative) error = %v", err)
	}
	err = builder.Append(ctx, merkletree.NewRawLeaf([]byte("xy")))
	if !errors.As(err, &resourceErr) ||
		resourceErr.Kind != merkletree.ResourceTotalBytes ||
		resourceErr.Actual != 8 {
		t.Fatalf(
			"Append(after cumulative) error = %v, want total-byte actual 8",
			err,
		)
	}
}

func TestBuilderRejectsInvalidStateLimitsAndCancellation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var zero merkletree.Builder
	if err := zero.Append(ctx, merkletree.NewRawLeaf(nil)); !errors.Is(
		err,
		merkletree.ErrInvalidBuilder,
	) {
		t.Fatalf("zero Append() error = %v, want ErrInvalidBuilder", err)
	}
	if _, err := zero.Snapshot(ctx); !errors.Is(
		err,
		merkletree.ErrInvalidBuilder,
	) {
		t.Fatalf("zero Snapshot() error = %v, want ErrInvalidBuilder", err)
	}
	if _, err := merkletree.NewBuilder(
		merkletree.Profile{},
		merkletree.DefaultSnapshotLimits(),
	); !errors.Is(err, merkletree.ErrUnsupportedProfile) {
		t.Fatalf("NewBuilder(invalid profile) error = %v", err)
	}
	if _, err := merkletree.NewBuilder(
		merkletree.CanonicalProfile(),
		merkletree.SnapshotLimits{},
	); !errors.Is(err, merkletree.ErrInvalidLimits) {
		t.Fatalf("NewBuilder(invalid limits) error = %v", err)
	}

	limits := merkletree.DefaultSnapshotLimits()
	limits.Construction.MaxLeaves = 2
	limits.Construction.MaxLeafBytes = 3
	limits.Construction.MaxTotalBytes = 4
	limits.MaxRetainedNodes = 3
	builder, err := merkletree.NewBuilder(merkletree.CanonicalProfile(), limits)
	if err != nil {
		t.Fatalf("NewBuilder() error = %v", err)
	}
	if err := builder.Append(ctx, merkletree.NewRawLeaf([]byte("abc"))); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	tests := []struct {
		name string
		leaf merkletree.RawLeaf
		kind merkletree.ResourceKind
	}{
		{
			name: "leaf bytes",
			leaf: merkletree.NewRawLeaf([]byte("four")),
			kind: merkletree.ResourceLeafBytes,
		},
		{
			name: "total bytes",
			leaf: merkletree.NewRawLeaf([]byte("de")),
			kind: merkletree.ResourceTotalBytes,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := builder.Append(ctx, test.leaf)
			var got *merkletree.ResourceError
			if !errors.As(err, &got) || got.Kind != test.kind {
				t.Fatalf("Append() error = %v, want resource %d", err, test.kind)
			}
		})
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := builder.Append(cancelled, merkletree.NewRawLeaf(nil)); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("Append(cancelled) error = %v", err)
	}
	if _, err := builder.Snapshot(cancelled); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("Snapshot(cancelled) error = %v", err)
	}
	var nilContext context.Context
	if err := builder.Append(
		nilContext,
		merkletree.NewRawLeaf(nil),
	); !errors.Is(
		err,
		merkletree.ErrInvalidContext,
	) {
		t.Fatalf("Append(nil context) error = %v", err)
	}
	if _, err := builder.Snapshot(nilContext); !errors.Is(
		err,
		merkletree.ErrInvalidContext,
	) {
		t.Fatalf("Snapshot(nil context) error = %v", err)
	}
}

func assertSameRoot(t *testing.T, got, want merkletree.Snapshot) {
	t.Helper()

	gotRoot, err := got.Root()
	if err != nil {
		t.Fatalf("got.Root() error = %v", err)
	}
	wantRoot, err := want.Root()
	if err != nil {
		t.Fatalf("want.Root() error = %v", err)
	}
	if gotRoot.ProfileID() != wantRoot.ProfileID() ||
		gotRoot.ProfileVersion() != wantRoot.ProfileVersion() ||
		gotRoot.Algorithm() != wantRoot.Algorithm() ||
		gotRoot.TreeSize() != wantRoot.TreeSize() ||
		!bytes.Equal(gotRoot.Digest().Bytes(), wantRoot.Digest().Bytes()) {
		t.Fatalf("root mismatch: got size %d digest %x, want size %d digest %x",
			gotRoot.TreeSize(),
			gotRoot.Digest().Bytes(),
			wantRoot.TreeSize(),
			wantRoot.Digest().Bytes(),
		)
	}
}
