package merkletree

import (
	"context"
	"testing"
)

func FuzzVerifyMultiInclusion(f *testing.F) {
	snapshot, leaves := fuzzMultiSnapshot(f)
	base, err := snapshot.MultiInclusionProof(
		context.Background(),
		[]uint64{1, 6},
		DefaultMultiProofLimits(),
	)
	if err != nil {
		f.Fatalf("MultiInclusionProof() error = %v", err)
	}

	f.Add(byte(0), uint64(1), uint64(8), byte(0), byte(0))
	f.Add(byte(7), ^uint64(0), uint64(0), byte(3), byte(0xff))
	f.Add(byte(15), uint64(6), uint64(8), byte(1), byte(1))
	f.Fuzz(func(
		t *testing.T,
		selector byte,
		indexValue uint64,
		treeSize uint64,
		elementIndex byte,
		delta byte,
	) {
		proof := cloneMultiProof(base)
		selected := []RawLeaf{leaves[1], leaves[6]}
		switch selector % 18 {
		case 0:
			proof.treeSize = treeSize
		case 1:
			proof.root.treeSize = treeSize
		case 2:
			proof.profileID = ProfileID(delta)
		case 3:
			proof.profileVersion = uint16(indexValue)
		case 4:
			proof.algorithm = HashAlgorithm(delta)
		case 5:
			if len(proof.leafIndexes) != 0 {
				index := int(elementIndex) % len(proof.leafIndexes)
				proof.leafIndexes[index] = indexValue
			}
		case 6:
			if len(proof.leafDigests) != 0 {
				index := int(elementIndex) % len(proof.leafDigests)
				proof.leafDigests[index].value[0] ^= delta
			}
		case 7:
			if len(proof.frontier) != 0 {
				index := int(elementIndex) % len(proof.frontier)
				proof.frontier[index].value[0] ^= delta
			}
		case 8:
			if len(proof.frontier) != 0 {
				proof.frontier = proof.frontier[:len(proof.frontier)-1]
			}
		case 9:
			proof.frontier = append(proof.frontier, Digest{})
		case 10:
			proof.root.digest.value[0] ^= delta
		case 11:
			proof.root.profileID = ProfileID(delta)
		case 12:
			proof.root.algorithm = HashAlgorithm(delta)
		case 13:
			if len(proof.leafDigests) != 0 {
				index := int(elementIndex) % len(proof.leafDigests)
				proof.leafDigests[index].algorithm = HashAlgorithm(delta)
			}
		case 14:
			if len(proof.frontier) > 1 {
				proof.frontier[0], proof.frontier[1] =
					proof.frontier[1], proof.frontier[0]
			}
		case 15:
			if len(proof.leafIndexes) > 1 {
				proof.leafIndexes[0], proof.leafIndexes[1] =
					proof.leafIndexes[1], proof.leafIndexes[0]
			}
		case 16:
			selected[0] = NewRawLeaf([]byte{delta})
		case 17:
			selected = selected[:int(elementIndex)%3]
		}

		_ = VerifyMultiInclusion(
			context.Background(),
			proof,
			selected,
			DefaultMultiProofLimits(),
		)
	})
}

func fuzzMultiSnapshot(f *testing.F) (Snapshot, []RawLeaf) {
	f.Helper()

	leaves := make([]RawLeaf, 8)
	for index := range leaves {
		leaves[index] = NewRawLeaf([]byte{byte(index), byte(index * 31)})
	}
	snapshot, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		leaves,
		DefaultSnapshotLimits(),
	)
	if err != nil {
		f.Fatalf("NewSnapshot() error = %v", err)
	}

	return snapshot, leaves
}
