package merkletree_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	merkletree "github.com/faustbrian/golib/pkg/merkle-tree"
)

func TestMultiInclusionProofExhaustivelyAuthenticatesSmallSubsets(
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
	allLeaves := consistencyLeaves(10)
	ctx := context.Background()
	for _, profile := range profiles {
		for treeSize := 1; treeSize <= len(allLeaves); treeSize++ {
			leaves := allLeaves[:treeSize]
			snapshot, snapshotErr := merkletree.NewSnapshot(
				ctx,
				profile,
				leaves,
				merkletree.DefaultSnapshotLimits(),
			)
			if snapshotErr != nil {
				t.Fatalf(
					"NewSnapshot(profile %d, size %d) error = %v",
					profile.ID(),
					treeSize,
					snapshotErr,
				)
			}
			for mask := 1; mask < 1<<treeSize; mask++ {
				indexes, selected := selectMultiLeaves(leaves, mask)
				proof, proofErr := snapshot.MultiInclusionProof(
					ctx,
					indexes,
					merkletree.DefaultMultiProofLimits(),
				)
				if proofErr != nil {
					t.Fatalf(
						"MultiInclusionProof(profile %d, size %d, mask %x) error = %v",
						profile.ID(),
						treeSize,
						mask,
						proofErr,
					)
				}
				assertDigestList(
					t,
					proof.Frontier(),
					independentMultiFrontier(leaves, indexes, 0),
				)
				if verifyErr := merkletree.VerifyMultiInclusion(
					ctx,
					proof,
					selected,
					merkletree.DefaultMultiProofLimits(),
				); verifyErr != nil {
					t.Fatalf(
						"VerifyMultiInclusion(profile %d, size %d, mask %x) error = %v",
						profile.ID(),
						treeSize,
						mask,
						verifyErr,
					)
				}
			}
		}
	}
}

func TestMultiInclusionProofCanonicalizesIndexesAndOwnsSlices(t *testing.T) {
	t.Parallel()

	leaves := consistencyLeaves(7)
	snapshot, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		leaves,
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	indexes := []uint64{6, 1, 4}
	proof, err := snapshot.MultiInclusionProof(
		context.Background(),
		indexes,
		merkletree.DefaultMultiProofLimits(),
	)
	if err != nil {
		t.Fatalf("MultiInclusionProof() error = %v", err)
	}
	if indexes[0] != 6 || indexes[1] != 1 || indexes[2] != 4 {
		t.Fatal("MultiInclusionProof modified caller indexes")
	}
	canonicalProof, err := snapshot.MultiInclusionProof(
		context.Background(),
		[]uint64{1, 4, 6},
		merkletree.DefaultMultiProofLimits(),
	)
	if err != nil {
		t.Fatalf("MultiInclusionProof(canonical indexes) error = %v", err)
	}
	if !equalDigestSlices(proof.LeafDigests(), canonicalProof.LeafDigests()) ||
		!equalDigestSlices(proof.Frontier(), canonicalProof.Frontier()) {
		t.Fatal("equivalent index sets produced different canonical proofs")
	}
	if got := proof.LeafIndexes(); !equalUint64s(got, []uint64{1, 4, 6}) {
		t.Fatalf("LeafIndexes() = %v, want [1 4 6]", got)
	}
	if proof.ProfileID() != merkletree.ProfileCanonicalBinary ||
		proof.ProfileVersion() != 1 ||
		proof.Algorithm() != merkletree.HashSHA256 ||
		proof.TreeSize() != 7 ||
		proof.Root().TreeSize() != 7 ||
		len(proof.LeafDigests()) != 3 {
		t.Fatal("proof does not bind the complete multi-inclusion identity")
	}

	indexCopy := proof.LeafIndexes()
	indexCopy[0] = 0
	digestCopy := proof.LeafDigests()
	digestCopy[0] = merkletree.Digest{}
	frontierCopy := proof.Frontier()
	if len(frontierCopy) != 0 {
		frontierCopy[0] = merkletree.Digest{}
	}
	if !equalUint64s(proof.LeafIndexes(), []uint64{1, 4, 6}) ||
		proof.LeafDigests()[0].Algorithm() != merkletree.HashSHA256 {
		t.Fatal("proof getters returned aliases to proof state")
	}

	selected := []merkletree.RawLeaf{leaves[1], leaves[4], leaves[6]}
	if err := merkletree.VerifyMultiInclusion(
		context.Background(),
		proof,
		selected,
		merkletree.DefaultMultiProofLimits(),
	); err != nil {
		t.Fatalf("VerifyMultiInclusion() error = %v", err)
	}
}

func TestMultiInclusionProofRejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	leaves := consistencyLeaves(4)
	snapshot, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		leaves,
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	for _, indexes := range [][]uint64{nil, {}, {1, 1}} {
		if _, proofErr := snapshot.MultiInclusionProof(
			context.Background(),
			indexes,
			merkletree.DefaultMultiProofLimits(),
		); !errors.Is(proofErr, merkletree.ErrInvalidLeafIndexes) {
			t.Fatalf(
				"MultiInclusionProof(%v) error = %v, want ErrInvalidLeafIndexes",
				indexes,
				proofErr,
			)
		}
	}
	if _, err := snapshot.MultiInclusionProof(
		context.Background(),
		[]uint64{0, 4},
		merkletree.DefaultMultiProofLimits(),
	); !errors.Is(err, merkletree.ErrIndexOutOfRange) {
		t.Fatalf("MultiInclusionProof(out of range) error = %v", err)
	}

	limits := merkletree.DefaultMultiProofLimits()
	limits.MaxLeaves = 1
	if _, err := snapshot.MultiInclusionProof(
		context.Background(),
		[]uint64{0, 3},
		limits,
	); !errors.Is(err, merkletree.ErrResourceExhausted) {
		t.Fatalf("MultiInclusionProof(leaf limit) error = %v", err)
	}
}

func selectMultiLeaves(
	leaves []merkletree.RawLeaf,
	mask int,
) ([]uint64, []merkletree.RawLeaf) {
	indexes := make([]uint64, 0, len(leaves))
	selected := make([]merkletree.RawLeaf, 0, len(leaves))
	for index, leaf := range leaves {
		if mask&(1<<index) == 0 {
			continue
		}
		indexes = append(indexes, uint64(index))
		selected = append(selected, leaf)
	}

	return indexes, selected
}

func equalUint64s(left, right []uint64) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}

	return true
}

func equalDigestSlices(left, right []merkletree.Digest) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !bytes.Equal(left[index].Bytes(), right[index].Bytes()) {
			return false
		}
	}

	return true
}

func independentMultiFrontier(
	leaves []merkletree.RawLeaf,
	indexes []uint64,
	start uint64,
) [][32]byte {
	if len(indexes) == 0 {
		return [][32]byte{independentTreeHash(leaves)}
	}
	if len(leaves) == 1 {
		return nil
	}

	split := largestPowerOfTwoBelow(len(leaves))
	splitIndex := 0
	for splitIndex < len(indexes) &&
		indexes[splitIndex] < start+uint64(split) {
		splitIndex++
	}
	frontier := independentMultiFrontier(
		leaves[:split],
		indexes[:splitIndex],
		start,
	)

	return append(
		frontier,
		independentMultiFrontier(
			leaves[split:],
			indexes[splitIndex:],
			start+uint64(split),
		)...,
	)
}
