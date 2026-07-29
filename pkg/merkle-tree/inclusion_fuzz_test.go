package merkletree

import (
	"context"
	"errors"
	"testing"
)

func FuzzVerifyInclusion(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0})
	f.Add([]byte{1, 2, 3, 4})

	leaves := []RawLeaf{
		NewRawLeaf([]byte("first")),
		NewRawLeaf([]byte("second")),
		NewRawLeaf([]byte("third")),
		NewRawLeaf([]byte("fourth")),
	}
	snapshot, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		leaves,
		DefaultSnapshotLimits(),
	)
	if err != nil {
		f.Fatalf("snapshot: %v", err)
	}
	base, err := snapshot.InclusionProof(
		context.Background(),
		1,
		DefaultProofLimits(),
	)
	if err != nil {
		f.Fatalf("proof: %v", err)
	}

	f.Fuzz(func(t *testing.T, mutation []byte) {
		proof := cloneInclusionProof(base)
		leaf := leaves[1]
		for offset, value := range mutation {
			switch offset % 5 {
			case 0:
				proof.leafIndex ^= uint64(value)
			case 1:
				proof.treeSize ^= uint64(value)
			case 2:
				if len(proof.siblings) != 0 {
					proof.siblings[offset%len(proof.siblings)].value[offset%32] ^= value
				}
			case 3:
				leaf = NewRawLeaf(mutation)
			case 4:
				proof.root.digest.value[offset%32] ^= value
			}
		}

		err := VerifyInclusion(
			context.Background(),
			proof,
			leaf,
			DefaultProofLimits(),
		)
		if err != nil &&
			!errors.Is(err, ErrMalformedProof) &&
			!errors.Is(err, ErrVerificationFailed) {
			t.Fatalf("unexpected verification error: %v", err)
		}
	})
}
