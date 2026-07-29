package merkletree_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	merkletree "github.com/faustbrian/golib/pkg/merkle-tree"
	transparencyproof "github.com/transparency-dev/merkle/proof"
	"github.com/transparency-dev/merkle/rfc6962"
	"github.com/transparency-dev/merkle/testonly"
)

func TestRFC9162MatchesTransparencyDevMerkle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile, err := merkletree.RFC9162Profile(merkletree.HashSHA256)
	if err != nil {
		t.Fatalf("RFC9162Profile() error = %v", err)
	}

	raw, leaves := differentialLeaves(128)
	reference := testonly.New(rfc6962.DefaultHasher)
	reference.AppendData(raw...)

	snapshots := make([]merkletree.Snapshot, len(leaves)+1)
	for size := range snapshots {
		snapshot, snapshotErr := merkletree.NewSnapshot(
			ctx,
			profile,
			leaves[:size],
			merkletree.DefaultSnapshotLimits(),
		)
		if snapshotErr != nil {
			t.Fatalf("NewSnapshot(size=%d) error = %v", size, snapshotErr)
		}
		snapshots[size] = snapshot

		root, rootErr := snapshot.Root()
		if rootErr != nil {
			t.Fatalf("Root(size=%d) error = %v", size, rootErr)
		}
		if got, want := root.Digest().Bytes(), reference.HashAt(uint64(size)); !bytes.Equal(got, want) {
			t.Fatalf("root(size=%d) = %x, transparency-dev/merkle = %x", size, got, want)
		}
	}

	for size := 1; size <= len(leaves); size++ {
		root, rootErr := snapshots[size].Root()
		if rootErr != nil {
			t.Fatalf("Root(size=%d) error = %v", size, rootErr)
		}
		for index := 0; index < size; index++ {
			proof, proofErr := snapshots[size].InclusionProof(
				ctx,
				uint64(index),
				merkletree.DefaultProofLimits(),
			)
			if proofErr != nil {
				t.Fatalf(
					"InclusionProof(size=%d,index=%d) error = %v",
					size,
					index,
					proofErr,
				)
			}
			referenceProof, referenceErr := reference.InclusionProof(
				uint64(index),
				uint64(size),
			)
			if referenceErr != nil {
				t.Fatalf(
					"transparency-dev InclusionProof(size=%d,index=%d) error = %v",
					size,
					index,
					referenceErr,
				)
			}
			if got := digestBytes(proof.Siblings()); !equalByteSlices(got, referenceProof) {
				t.Fatalf(
					"proof(size=%d,index=%d) = %x, transparency-dev/merkle = %x",
					size,
					index,
					got,
					referenceProof,
				)
			}
			if verifyErr := transparencyproof.VerifyInclusion(
				rfc6962.DefaultHasher,
				uint64(index),
				uint64(size),
				rfc6962.DefaultHasher.HashLeaf(raw[index]),
				digestBytes(proof.Siblings()),
				root.Digest().Bytes(),
			); verifyErr != nil {
				t.Fatalf(
					"transparency-dev VerifyInclusion(size=%d,index=%d) error = %v",
					size,
					index,
					verifyErr,
				)
			}
		}
	}

	for newerSize := 1; newerSize <= len(leaves); newerSize++ {
		newerRoot, rootErr := snapshots[newerSize].Root()
		if rootErr != nil {
			t.Fatalf("Root(size=%d) error = %v", newerSize, rootErr)
		}
		for olderSize := 1; olderSize <= newerSize; olderSize++ {
			olderRoot, olderRootErr := snapshots[olderSize].Root()
			if olderRootErr != nil {
				t.Fatalf("Root(size=%d) error = %v", olderSize, olderRootErr)
			}
			proof, proofErr := snapshots[newerSize].ConsistencyProof(
				ctx,
				olderRoot,
				merkletree.DefaultConsistencyProofLimits(),
			)
			if proofErr != nil {
				t.Fatalf(
					"ConsistencyProof(%d,%d) error = %v",
					olderSize,
					newerSize,
					proofErr,
				)
			}
			referenceProof, referenceErr := reference.ConsistencyProof(
				uint64(olderSize),
				uint64(newerSize),
			)
			if referenceErr != nil {
				t.Fatalf(
					"transparency-dev ConsistencyProof(%d,%d) error = %v",
					olderSize,
					newerSize,
					referenceErr,
				)
			}
			if got := digestBytes(proof.Nodes()); !equalByteSlices(got, referenceProof) {
				t.Fatalf(
					"consistency proof(%d,%d) = %x, transparency-dev/merkle = %x",
					olderSize,
					newerSize,
					got,
					referenceProof,
				)
			}
			if verifyErr := transparencyproof.VerifyConsistency(
				rfc6962.DefaultHasher,
				uint64(olderSize),
				uint64(newerSize),
				digestBytes(proof.Nodes()),
				olderRoot.Digest().Bytes(),
				newerRoot.Digest().Bytes(),
			); verifyErr != nil {
				t.Fatalf(
					"transparency-dev VerifyConsistency(%d,%d) error = %v",
					olderSize,
					newerSize,
					verifyErr,
				)
			}
		}
	}
}

func differentialLeaves(count int) ([][]byte, []merkletree.RawLeaf) {
	raw := make([][]byte, count)
	leaves := make([]merkletree.RawLeaf, count)
	for index := range raw {
		raw[index] = []byte(fmt.Sprintf("leaf-%03d-%x", index, index*index+17))
		leaves[index] = merkletree.NewRawLeaf(raw[index])
	}

	return raw, leaves
}

func digestBytes(digests []merkletree.Digest) [][]byte {
	result := make([][]byte, len(digests))
	for index := range digests {
		result[index] = digests[index].Bytes()
	}

	return result
}

func equalByteSlices(left, right [][]byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !bytes.Equal(left[index], right[index]) {
			return false
		}
	}

	return true
}
