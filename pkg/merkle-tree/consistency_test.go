package merkletree_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"testing"

	merkletree "github.com/faustbrian/golib/pkg/merkle-tree"
)

func TestConsistencyProofMatchesRFC9162Examples(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	leaves := consistencyLeaves(7)
	newer, err := merkletree.NewSnapshot(
		ctx,
		merkletree.CanonicalProfile(),
		leaves,
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot(newer) error = %v", err)
	}

	for _, olderSize := range []int{3, 4, 6} {
		older, olderErr := merkletree.NewSnapshot(
			ctx,
			merkletree.CanonicalProfile(),
			leaves[:olderSize],
			merkletree.DefaultSnapshotLimits(),
		)
		if olderErr != nil {
			t.Fatalf("NewSnapshot(%d) error = %v", olderSize, olderErr)
		}
		olderRoot, rootErr := older.Root()
		if rootErr != nil {
			t.Fatalf("older.Root(%d) error = %v", olderSize, rootErr)
		}
		proof, proofErr := newer.ConsistencyProof(
			ctx,
			olderRoot,
			merkletree.DefaultConsistencyProofLimits(),
		)
		if proofErr != nil {
			t.Fatalf("ConsistencyProof(%d, 7) error = %v", olderSize, proofErr)
		}
		want := independentConsistencyPath(
			leaves,
			olderSize,
			len(leaves),
		)
		assertDigestList(t, proof.Nodes(), want)
		if verifyErr := merkletree.VerifyConsistency(
			ctx,
			proof,
			merkletree.DefaultConsistencyProofLimits(),
		); verifyErr != nil {
			t.Fatalf("VerifyConsistency(%d, 7) error = %v", olderSize, verifyErr)
		}
	}
}

func TestConsistencyProofExhaustivelyAuthenticatesSmallPrefixes(
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
	leaves := consistencyLeaves(65)
	ctx := context.Background()
	for _, profile := range profiles {
		for newerSize := 1; newerSize <= len(leaves); newerSize++ {
			newer, newerErr := merkletree.NewSnapshot(
				ctx,
				profile,
				leaves[:newerSize],
				merkletree.DefaultSnapshotLimits(),
			)
			if newerErr != nil {
				t.Fatalf(
					"NewSnapshot(profile %d, newer %d) error = %v",
					profile.ID(),
					newerSize,
					newerErr,
				)
			}
			for olderSize := 1; olderSize <= newerSize; olderSize++ {
				older, olderErr := merkletree.NewSnapshot(
					ctx,
					profile,
					leaves[:olderSize],
					merkletree.DefaultSnapshotLimits(),
				)
				if olderErr != nil {
					t.Fatalf(
						"NewSnapshot(profile %d, older %d) error = %v",
						profile.ID(),
						olderSize,
						olderErr,
					)
				}
				olderRoot, rootErr := older.Root()
				if rootErr != nil {
					t.Fatalf("older.Root() error = %v", rootErr)
				}
				proof, proofErr := newer.ConsistencyProof(
					ctx,
					olderRoot,
					merkletree.DefaultConsistencyProofLimits(),
				)
				if proofErr != nil {
					t.Fatalf(
						"ConsistencyProof(profile %d, %d, %d) error = %v",
						profile.ID(),
						olderSize,
						newerSize,
						proofErr,
					)
				}
				want := independentConsistencyPath(
					leaves,
					olderSize,
					newerSize,
				)
				assertDigestList(t, proof.Nodes(), want)
				if verifyErr := merkletree.VerifyConsistency(
					ctx,
					proof,
					merkletree.DefaultConsistencyProofLimits(),
				); verifyErr != nil {
					t.Fatalf(
						"VerifyConsistency(profile %d, %d, %d) error = %v",
						profile.ID(),
						olderSize,
						newerSize,
						verifyErr,
					)
				}
			}
		}
	}
}

func TestConsistencyProofBindsRootsIdentityAndOwnsNodeSlices(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	leaves := consistencyLeaves(7)
	older, err := merkletree.NewSnapshot(
		ctx,
		merkletree.CanonicalProfile(),
		leaves[:3],
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot(older) error = %v", err)
	}
	newer, err := merkletree.NewSnapshot(
		ctx,
		merkletree.CanonicalProfile(),
		leaves,
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot(newer) error = %v", err)
	}
	olderRoot, err := older.Root()
	if err != nil {
		t.Fatalf("older.Root() error = %v", err)
	}
	newerRoot, err := newer.Root()
	if err != nil {
		t.Fatalf("newer.Root() error = %v", err)
	}
	proof, err := newer.ConsistencyProof(
		ctx,
		olderRoot,
		merkletree.DefaultConsistencyProofLimits(),
	)
	if err != nil {
		t.Fatalf("ConsistencyProof() error = %v", err)
	}

	if proof.ProfileID() != merkletree.ProfileCanonicalBinary ||
		proof.ProfileVersion() != 1 ||
		proof.Algorithm() != merkletree.HashSHA256 ||
		proof.OlderTreeSize() != 3 ||
		proof.NewerTreeSize() != 7 ||
		!bytes.Equal(
			proof.OlderRoot().Digest().Bytes(),
			olderRoot.Digest().Bytes(),
		) ||
		!bytes.Equal(
			proof.NewerRoot().Digest().Bytes(),
			newerRoot.Digest().Bytes(),
		) {
		t.Fatal("proof does not bind the complete consistency identity")
	}

	nodes := proof.Nodes()
	if len(nodes) == 0 {
		t.Fatal("proof has no nodes")
	}
	original := proof.Nodes()[0].Bytes()
	nodeBytes := nodes[0].Bytes()
	nodeBytes[0] ^= 0xff
	nodes[0] = merkletree.Digest{}
	if !bytes.Equal(proof.Nodes()[0].Bytes(), original) {
		t.Fatal("Nodes returned an alias to proof state")
	}
}

func TestConsistencyProofRejectsInvalidRequestsAndResourceClaims(
	t *testing.T,
) {
	t.Parallel()

	ctx := context.Background()
	leaves := consistencyLeaves(8)
	canonical := merkletree.CanonicalProfile()
	newer, err := merkletree.NewSnapshot(
		ctx,
		canonical,
		leaves,
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot(newer) error = %v", err)
	}
	empty, err := merkletree.NewSnapshot(
		ctx,
		canonical,
		nil,
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot(empty) error = %v", err)
	}
	emptyRoot, err := empty.Root()
	if err != nil {
		t.Fatalf("empty.Root() error = %v", err)
	}
	if _, err := newer.ConsistencyProof(
		ctx,
		emptyRoot,
		merkletree.DefaultConsistencyProofLimits(),
	); !errors.Is(err, merkletree.ErrInvalidTreeSize) {
		t.Fatalf("ConsistencyProof(empty, newer) error = %v", err)
	}

	larger, err := merkletree.NewSnapshot(
		ctx,
		canonical,
		append(leaves, merkletree.NewRawLeaf([]byte("extra"))),
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot(larger) error = %v", err)
	}
	largerRoot, err := larger.Root()
	if err != nil {
		t.Fatalf("larger.Root() error = %v", err)
	}
	if _, err := newer.ConsistencyProof(
		ctx,
		largerRoot,
		merkletree.DefaultConsistencyProofLimits(),
	); !errors.Is(err, merkletree.ErrInvalidTreeSize) {
		t.Fatalf("ConsistencyProof(larger, newer) error = %v", err)
	}

	rfc, err := merkletree.RFC9162Profile(merkletree.HashSHA256)
	if err != nil {
		t.Fatalf("RFC9162Profile() error = %v", err)
	}
	rfcOlder, err := merkletree.NewSnapshot(
		ctx,
		rfc,
		leaves[:3],
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot(rfc) error = %v", err)
	}
	rfcRoot, err := rfcOlder.Root()
	if err != nil {
		t.Fatalf("rfcOlder.Root() error = %v", err)
	}
	if _, err := newer.ConsistencyProof(
		ctx,
		rfcRoot,
		merkletree.DefaultConsistencyProofLimits(),
	); !errors.Is(err, merkletree.ErrIncompatibleRoot) {
		t.Fatalf("ConsistencyProof(incompatible profile) error = %v", err)
	}

	older, err := merkletree.NewSnapshot(
		ctx,
		canonical,
		leaves[:3],
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot(older) error = %v", err)
	}
	olderRoot, err := older.Root()
	if err != nil {
		t.Fatalf("older.Root() error = %v", err)
	}
	limits := merkletree.DefaultConsistencyProofLimits()
	limits.MaxElements = 1
	if _, err := newer.ConsistencyProof(
		ctx,
		olderRoot,
		limits,
	); !errors.Is(err, merkletree.ErrResourceExhausted) {
		t.Fatalf("ConsistencyProof(element limit) error = %v", err)
	}
	limits = merkletree.DefaultConsistencyProofLimits()
	limits.MaxTraversalDepth = 1
	if _, err := newer.ConsistencyProof(
		ctx,
		olderRoot,
		limits,
	); !errors.Is(err, merkletree.ErrResourceExhausted) {
		t.Fatalf("ConsistencyProof(depth limit) error = %v", err)
	}
}

func consistencyLeaves(size int) []merkletree.RawLeaf {
	leaves := make([]merkletree.RawLeaf, size)
	for index := range leaves {
		leaves[index] = merkletree.NewRawLeaf([]byte{
			byte(index),
			byte(index * 31),
		})
	}

	return leaves
}

func independentConsistencyPath(
	leaves []merkletree.RawLeaf,
	olderSize int,
	newerSize int,
) [][sha256.Size]byte {
	if olderSize == newerSize {
		return nil
	}

	return independentSubproof(
		leaves[:newerSize],
		olderSize,
		true,
	)
}

func independentSubproof(
	leaves []merkletree.RawLeaf,
	olderSize int,
	complete bool,
) [][sha256.Size]byte {
	if olderSize == len(leaves) {
		if complete {
			return nil
		}

		return [][sha256.Size]byte{independentTreeHash(leaves)}
	}

	split := largestPowerOfTwoBelow(len(leaves))
	if olderSize <= split {
		path := independentSubproof(leaves[:split], olderSize, complete)

		return append(path, independentTreeHash(leaves[split:]))
	}
	path := independentSubproof(
		leaves[split:],
		olderSize-split,
		false,
	)

	return append(path, independentTreeHash(leaves[:split]))
}

func independentTreeHash(
	leaves []merkletree.RawLeaf,
) [sha256.Size]byte {
	values := make([][]byte, len(leaves))
	for index, leaf := range leaves {
		values[index] = leaf.Bytes()
	}
	value := referenceTreeHash(values)
	var result [sha256.Size]byte
	copy(result[:], value)

	return result
}

func assertDigestList(
	t *testing.T,
	got []merkletree.Digest,
	want [][sha256.Size]byte,
) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("node count = %d, want %d", len(got), len(want))
	}
	for index := range want {
		if !bytes.Equal(got[index].Bytes(), want[index][:]) {
			t.Fatalf(
				"node %d = %x, want %x",
				index,
				got[index].Bytes(),
				want[index],
			)
		}
	}
}
