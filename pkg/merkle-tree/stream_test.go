package merkletree_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	merkletree "github.com/faustbrian/golib/pkg/merkle-tree"
)

func TestRootBuilderMatchesBatchConstructionForEveryPrefixAndProfile(
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
	leaves := make([]merkletree.RawLeaf, 257)
	for index := range leaves {
		leaves[index] = merkletree.NewRawLeaf([]byte{
			byte(index),
			byte(index * 17),
		})
	}
	for _, profile := range profiles {
		builder, builderErr := merkletree.NewRootBuilder(
			profile,
			merkletree.DefaultLimits(),
		)
		if builderErr != nil {
			t.Fatalf("NewRootBuilder() error = %v", builderErr)
		}
		for size := 0; ; {
			got, rootErr := builder.Root(context.Background())
			if rootErr != nil {
				t.Fatalf(
					"Root(profile %d, size %d) error = %v",
					profile.ID(),
					size,
					rootErr,
				)
			}
			want, wantErr := merkletree.ComputeRoot(
				context.Background(),
				profile,
				leaves[:size],
				merkletree.DefaultLimits(),
			)
			if wantErr != nil {
				t.Fatalf(
					"ComputeRoot(profile %d, size %d) error = %v",
					profile.ID(),
					size,
					wantErr,
				)
			}
			if !sameRoot(got, want) {
				t.Fatalf(
					"root mismatch profile %d size %d: got %x, want %x",
					profile.ID(),
					size,
					got.Digest().Bytes(),
					want.Digest().Bytes(),
				)
			}
			if size == len(leaves) {
				break
			}

			end := min(size+1+(size%7), len(leaves))
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

func TestRootBuilderBatchAppendIsAtomicAndBounded(t *testing.T) {
	t.Parallel()

	limits := merkletree.DefaultLimits()
	limits.MaxLeaves = 2
	limits.MaxLeafBytes = 3
	limits.MaxTotalBytes = 5
	builder, err := merkletree.NewRootBuilder(
		merkletree.CanonicalProfile(),
		limits,
	)
	if err != nil {
		t.Fatalf("NewRootBuilder() error = %v", err)
	}
	if err := builder.Append(
		context.Background(),
		merkletree.NewRawLeaf([]byte("one")),
	); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	before, err := builder.Root(context.Background())
	if err != nil {
		t.Fatalf("Root(before) error = %v", err)
	}
	err = builder.AppendBatch(context.Background(), []merkletree.RawLeaf{
		merkletree.NewRawLeaf([]byte("two")),
		merkletree.NewRawLeaf([]byte("toolong")),
	})
	var resourceErr *merkletree.ResourceError
	if !errors.As(err, &resourceErr) ||
		resourceErr.Kind != merkletree.ResourceLeaves {
		t.Fatalf("AppendBatch() error = %v, want leaves ResourceError", err)
	}
	after, err := builder.Root(context.Background())
	if err != nil {
		t.Fatalf("Root(after) error = %v", err)
	}
	if !sameRoot(after, before) {
		t.Fatal("failed batch changed the streaming root")
	}
	if err := builder.AppendBatch(context.Background(), nil); err != nil {
		t.Fatalf("AppendBatch(nil) error = %v", err)
	}
}

func sameRoot(left, right merkletree.Root) bool {
	return left.ProfileID() == right.ProfileID() &&
		left.ProfileVersion() == right.ProfileVersion() &&
		left.Algorithm() == right.Algorithm() &&
		left.TreeSize() == right.TreeSize() &&
		bytes.Equal(left.Digest().Bytes(), right.Digest().Bytes())
}
