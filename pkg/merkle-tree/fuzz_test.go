package merkletree_test

import (
	"context"
	"testing"

	merkletree "github.com/faustbrian/golib/pkg/merkle-tree"
)

func FuzzComputeRoot(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0})
	f.Add([]byte{3, 'a', 'b', 'c'})
	f.Add([]byte{8, 0, 1, 2, 3, 4, 5, 6, 7})

	f.Fuzz(func(t *testing.T, data []byte) {
		const maxFuzzBytes = 4096
		if len(data) > maxFuzzBytes {
			data = data[:maxFuzzBytes]
		}

		leafCount := 0
		if len(data) != 0 {
			leafCount = int(data[0] % 33)
			data = data[1:]
		}
		leaves := splitFuzzLeaves(data, leafCount)
		limits := merkletree.Limits{
			MaxLeaves:     32,
			MaxLeafBytes:  maxFuzzBytes,
			MaxTotalBytes: maxFuzzBytes,
		}

		canonical, err := merkletree.ComputeRoot(
			context.Background(),
			merkletree.CanonicalProfile(),
			leaves,
			limits,
		)
		if err != nil {
			t.Fatalf("canonical root: %v", err)
		}
		repeated, err := merkletree.ComputeRoot(
			context.Background(),
			merkletree.CanonicalProfile(),
			leaves,
			limits,
		)
		if err != nil {
			t.Fatalf("repeated root: %v", err)
		}
		rfcProfile, err := merkletree.RFC9162Profile(merkletree.HashSHA256)
		if err != nil {
			t.Fatalf("RFC 9162 profile: %v", err)
		}
		rfc, err := merkletree.ComputeRoot(
			context.Background(),
			rfcProfile,
			leaves,
			limits,
		)
		if err != nil {
			t.Fatalf("RFC 9162 root: %v", err)
		}

		if !equalBytes(canonical.Digest().Bytes(), repeated.Digest().Bytes()) {
			t.Fatal("identical construction was not deterministic")
		}
		if !equalBytes(canonical.Digest().Bytes(), rfc.Digest().Bytes()) {
			t.Fatal("canonical and RFC 9162 version 1 roots diverged")
		}
		if canonical.TreeSize() != uint64(len(leaves)) {
			t.Fatalf("tree size = %d, want %d", canonical.TreeSize(), len(leaves))
		}
	})
}

func splitFuzzLeaves(data []byte, count int) []merkletree.RawLeaf {
	if count == 0 {
		return nil
	}

	result := make([]merkletree.RawLeaf, count)
	for index := range count {
		start := len(data) * index / count
		end := len(data) * (index + 1) / count
		result[index] = merkletree.NewRawLeaf(data[start:end])
	}

	return result
}
