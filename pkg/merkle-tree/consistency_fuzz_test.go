package merkletree

import (
	"context"
	"testing"
)

func FuzzVerifyConsistency(f *testing.F) {
	leaves := make([]RawLeaf, 7)
	for index := range leaves {
		leaves[index] = NewRawLeaf([]byte{byte(index)})
	}
	older, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		leaves[:3],
		DefaultSnapshotLimits(),
	)
	if err != nil {
		f.Fatalf("NewSnapshot(older) error = %v", err)
	}
	newer, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		leaves,
		DefaultSnapshotLimits(),
	)
	if err != nil {
		f.Fatalf("NewSnapshot(newer) error = %v", err)
	}
	olderRoot, err := older.Root()
	if err != nil {
		f.Fatalf("older.Root() error = %v", err)
	}
	base, err := newer.ConsistencyProof(
		context.Background(),
		olderRoot,
		DefaultConsistencyProofLimits(),
	)
	if err != nil {
		f.Fatalf("ConsistencyProof() error = %v", err)
	}

	f.Add(byte(0), uint64(3), uint64(7), byte(0), byte(0))
	f.Add(byte(5), uint64(0), ^uint64(0), byte(3), byte(0xff))
	f.Add(byte(12), uint64(4), uint64(4), byte(1), byte(1))
	f.Fuzz(func(
		t *testing.T,
		selector byte,
		olderSize uint64,
		newerSize uint64,
		nodeIndex byte,
		delta byte,
	) {
		proof := cloneConsistencyProof(base)
		switch selector % 16 {
		case 0:
			proof.olderTreeSize = olderSize
		case 1:
			proof.newerTreeSize = newerSize
		case 2:
			proof.olderRoot.treeSize = olderSize
		case 3:
			proof.newerRoot.treeSize = newerSize
		case 4:
			proof.profileID = ProfileID(delta)
		case 5:
			proof.profileVersion = uint16(olderSize)
		case 6:
			proof.algorithm = HashAlgorithm(delta)
		case 7:
			proof.olderRoot.digest.value[0] ^= delta
		case 8:
			proof.newerRoot.digest.value[0] ^= delta
		case 9:
			if len(proof.nodes) != 0 {
				index := int(nodeIndex) % len(proof.nodes)
				proof.nodes[index].value[0] ^= delta
			}
		case 10:
			if len(proof.nodes) != 0 {
				proof.nodes = proof.nodes[:len(proof.nodes)-1]
			}
		case 11:
			proof.nodes = append(proof.nodes, Digest{})
		case 12:
			proof.olderRoot.profileID = ProfileID(delta)
		case 13:
			proof.newerRoot.algorithm = HashAlgorithm(delta)
		case 14:
			if len(proof.nodes) > 1 {
				proof.nodes[0], proof.nodes[1] =
					proof.nodes[1], proof.nodes[0]
			}
		case 15:
			if len(proof.nodes) != 0 {
				index := int(nodeIndex) % len(proof.nodes)
				proof.nodes[index].algorithm = HashAlgorithm(delta)
			}
		}

		_ = VerifyConsistency(
			context.Background(),
			proof,
			DefaultConsistencyProofLimits(),
		)
	})
}
