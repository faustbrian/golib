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

func BenchmarkBuilderAppend(b *testing.B) {
	leaf := merkletree.NewRawLeaf(make([]byte, 32))
	limits := merkletree.DefaultSnapshotLimits()
	limits.Construction.MaxLeaves = uint64(b.N)
	limits.MaxRetainedNodes = 2*uint64(b.N) + 1
	builder, err := merkletree.NewBuilder(
		merkletree.CanonicalProfile(),
		limits,
	)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.SetBytes(32)
	b.ResetTimer()
	for range b.N {
		if err := builder.Append(context.Background(), leaf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuilderAppendBatch(b *testing.B) {
	leaves := benchmarkLeaves(256)
	limits := merkletree.DefaultSnapshotLimits()
	limits.Construction.MaxLeaves = uint64(b.N * len(leaves))
	limits.MaxRetainedNodes = 2*limits.Construction.MaxLeaves + 1
	builder, err := merkletree.NewBuilder(
		merkletree.CanonicalProfile(),
		limits,
	)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(leaves) * 32))
	b.ResetTimer()
	for range b.N {
		if err := builder.AppendBatch(
			context.Background(),
			leaves,
		); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConsistencyProof(b *testing.B) {
	leaves := benchmarkLeaves(16_384)
	older, newer := benchmarkConsistencySnapshots(b, leaves, 8_191)
	olderRoot, err := older.Root()
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := newer.ConsistencyProof(
			context.Background(),
			olderRoot,
			merkletree.DefaultConsistencyProofLimits(),
		); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyConsistency(b *testing.B) {
	leaves := benchmarkLeaves(16_384)
	older, newer := benchmarkConsistencySnapshots(b, leaves, 8_191)
	olderRoot, err := older.Root()
	if err != nil {
		b.Fatal(err)
	}
	proof, err := newer.ConsistencyProof(
		context.Background(),
		olderRoot,
		merkletree.DefaultConsistencyProofLimits(),
	)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := merkletree.VerifyConsistency(
			context.Background(),
			proof,
			merkletree.DefaultConsistencyProofLimits(),
		); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkConsistencySnapshots(
	b *testing.B,
	leaves []merkletree.RawLeaf,
	olderSize int,
) (merkletree.Snapshot, merkletree.Snapshot) {
	b.Helper()

	older, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		leaves[:olderSize],
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		b.Fatal(err)
	}
	newer, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		leaves,
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		b.Fatal(err)
	}

	return older, newer
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
