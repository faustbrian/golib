package merkletree_test

import (
	"context"
	"fmt"
	"testing"

	merkletree "github.com/faustbrian/golib/pkg/merkle-tree"
)

func BenchmarkComputeRoot(b *testing.B) {
	for _, size := range []int{1, 256, 16_384} {
		leaves := make([]merkletree.RawLeaf, size)
		for index := range leaves {
			value := make([]byte, 32)
			value[0] = byte(index)
			value[1] = byte(index >> 8)
			leaves[index] = merkletree.NewRawLeaf(value)
		}

		b.Run(fmt.Sprintf("leaves_%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(size * 32))

			for range b.N {
				if _, err := merkletree.ComputeRoot(
					context.Background(),
					merkletree.CanonicalProfile(),
					leaves,
					merkletree.DefaultLimits(),
				); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkInclusionProof(b *testing.B) {
	leaves := benchmarkLeaves(16_384)
	snapshot, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		leaves,
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := snapshot.InclusionProof(
			context.Background(),
			8_192,
			merkletree.DefaultProofLimits(),
		); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyInclusion(b *testing.B) {
	leaves := benchmarkLeaves(16_384)
	snapshot, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		leaves,
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		b.Fatal(err)
	}
	proof, err := snapshot.InclusionProof(
		context.Background(),
		8_192,
		merkletree.DefaultProofLimits(),
	)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := merkletree.VerifyInclusion(
			context.Background(),
			proof,
			leaves[8_192],
			merkletree.DefaultProofLimits(),
		); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkLeaves(size int) []merkletree.RawLeaf {
	leaves := make([]merkletree.RawLeaf, size)
	for index := range leaves {
		value := make([]byte, 32)
		value[0] = byte(index)
		value[1] = byte(index >> 8)
		leaves[index] = merkletree.NewRawLeaf(value)
	}

	return leaves
}
