package merkletree_test

import (
	"context"
	"errors"
	"testing"

	merkletree "github.com/faustbrian/golib/pkg/merkle-tree"
)

func TestSnapshotGeneratesIndependentlyVerifiableInclusionProofs(t *testing.T) {
	t.Parallel()

	for size := 1; size <= 65; size++ {
		leaves := make([]merkletree.RawLeaf, size)
		raw := make([][]byte, size)
		for index := range size {
			raw[index] = []byte{byte(index), byte(index >> 8), 0xa5}
			leaves[index] = merkletree.NewRawLeaf(raw[index])
		}

		snapshot, err := merkletree.NewSnapshot(
			context.Background(),
			merkletree.CanonicalProfile(),
			leaves,
			merkletree.DefaultSnapshotLimits(),
		)
		if err != nil {
			t.Fatalf("size %d: snapshot: %v", size, err)
		}

		root, err := snapshot.Root()
		if err != nil {
			t.Fatalf("size %d: root: %v", size, err)
		}
		computed, err := merkletree.ComputeRoot(
			context.Background(),
			merkletree.CanonicalProfile(),
			leaves,
			merkletree.DefaultLimits(),
		)
		if err != nil {
			t.Fatalf("size %d: compute root: %v", size, err)
		}
		if !equalBytes(root.Digest().Bytes(), computed.Digest().Bytes()) {
			t.Fatalf(
				"size %d: snapshot root = %x, computed root = %x",
				size,
				root.Digest().Bytes(),
				computed.Digest().Bytes(),
			)
		}

		for index, leaf := range leaves {
			proof, proofErr := snapshot.InclusionProof(
				context.Background(),
				uint64(index),
				merkletree.DefaultProofLimits(),
			)
			if proofErr != nil {
				t.Fatalf("size %d index %d: proof: %v", size, index, proofErr)
			}
			wantPath := referenceAuditPath(raw, index)
			gotPath := proof.Siblings()
			if len(gotPath) != len(wantPath) {
				t.Fatalf(
					"size %d index %d: path length = %d, want %d",
					size,
					index,
					len(gotPath),
					len(wantPath),
				)
			}
			for pathIndex := range wantPath {
				if !equalBytes(gotPath[pathIndex].Bytes(), wantPath[pathIndex]) {
					t.Fatalf(
						"size %d index %d path %d: digest = %x, want %x",
						size,
						index,
						pathIndex,
						gotPath[pathIndex].Bytes(),
						wantPath[pathIndex],
					)
				}
			}
			if verifyErr := merkletree.VerifyInclusion(
				context.Background(),
				proof,
				leaf,
				merkletree.DefaultProofLimits(),
			); verifyErr != nil {
				t.Fatalf(
					"size %d index %d: verify: %v",
					size,
					index,
					verifyErr,
				)
			}
		}
	}
}

func referenceAuditPath(leaves [][]byte, index int) [][]byte {
	if len(leaves) == 1 {
		return nil
	}

	split := largestPowerOfTwoBelow(len(leaves))
	if index < split {
		path := referenceAuditPath(leaves[:split], index)

		return append(path, referenceTreeHash(leaves[split:]))
	}

	path := referenceAuditPath(leaves[split:], index-split)

	return append(path, referenceTreeHash(leaves[:split]))
}

func TestInclusionProofBindsOperationIdentityAndOwnsReturnedSlices(t *testing.T) {
	t.Parallel()

	profile, err := merkletree.RFC9162Profile(merkletree.HashSHA256)
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	leaves := []merkletree.RawLeaf{
		merkletree.NewRawLeaf([]byte("first")),
		merkletree.NewRawLeaf([]byte("second")),
		merkletree.NewRawLeaf([]byte("third")),
	}
	snapshot, err := merkletree.NewSnapshot(
		context.Background(),
		profile,
		leaves,
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	proof, err := snapshot.InclusionProof(
		context.Background(),
		1,
		merkletree.DefaultProofLimits(),
	)
	if err != nil {
		t.Fatalf("proof: %v", err)
	}

	if proof.ProfileID() != merkletree.ProfileRFC9162 ||
		proof.ProfileVersion() != 1 ||
		proof.Algorithm() != merkletree.HashSHA256 ||
		proof.TreeSize() != 3 ||
		proof.LeafIndex() != 1 {
		t.Fatalf(
			"proof identity = profile %d version %d algorithm %d size %d index %d",
			proof.ProfileID(),
			proof.ProfileVersion(),
			proof.Algorithm(),
			proof.TreeSize(),
			proof.LeafIndex(),
		)
	}
	if !equalBytes(
		proof.Root().Digest().Bytes(),
		mustSnapshotRoot(t, snapshot).Digest().Bytes(),
	) {
		t.Fatal("proof root differs from snapshot root")
	}
	if proof.LeafDigest().Algorithm() != merkletree.HashSHA256 {
		t.Fatalf("leaf digest algorithm = %d", proof.LeafDigest().Algorithm())
	}

	siblings := proof.Siblings()
	if len(siblings) == 0 {
		t.Fatal("proof has no siblings")
	}
	siblings[0] = merkletree.Digest{}
	if err := merkletree.VerifyInclusion(
		context.Background(),
		proof,
		leaves[1],
		merkletree.DefaultProofLimits(),
	); err != nil {
		t.Fatalf("mutating returned siblings changed proof: %v", err)
	}
}

func TestInclusionProofRejectsWrongLeafAndInvalidRequests(t *testing.T) {
	t.Parallel()

	snapshot, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		[]merkletree.RawLeaf{merkletree.NewRawLeaf([]byte("expected"))},
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	proof, err := snapshot.InclusionProof(
		context.Background(),
		0,
		merkletree.DefaultProofLimits(),
	)
	if err != nil {
		t.Fatalf("proof: %v", err)
	}

	if verifyErr := merkletree.VerifyInclusion(
		context.Background(),
		proof,
		merkletree.NewRawLeaf([]byte("different")),
		merkletree.DefaultProofLimits(),
	); !errors.Is(verifyErr, merkletree.ErrVerificationFailed) {
		t.Fatalf("wrong leaf error = %v", verifyErr)
	}
	if _, proofErr := snapshot.InclusionProof(
		context.Background(),
		1,
		merkletree.DefaultProofLimits(),
	); !errors.Is(proofErr, merkletree.ErrIndexOutOfRange) {
		t.Fatalf("out-of-range error = %v", proofErr)
	}

	empty, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		nil,
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("empty snapshot: %v", err)
	}
	if _, proofErr := empty.InclusionProof(
		context.Background(),
		0,
		merkletree.DefaultProofLimits(),
	); !errors.Is(proofErr, merkletree.ErrIndexOutOfRange) {
		t.Fatalf("empty snapshot proof error = %v", proofErr)
	}

	var zero merkletree.Snapshot
	if _, rootErr := zero.Root(); !errors.Is(rootErr, merkletree.ErrInvalidSnapshot) {
		t.Fatalf("zero snapshot root error = %v", rootErr)
	}
	if _, proofErr := zero.InclusionProof(
		context.Background(),
		0,
		merkletree.DefaultProofLimits(),
	); !errors.Is(proofErr, merkletree.ErrInvalidSnapshot) {
		t.Fatalf("zero snapshot proof error = %v", proofErr)
	}
}

func TestInclusionProofEnforcesResourceAndCancellationLimits(t *testing.T) {
	t.Parallel()

	leaves := []merkletree.RawLeaf{
		merkletree.NewRawLeaf([]byte("first")),
		merkletree.NewRawLeaf([]byte("second")),
		merkletree.NewRawLeaf([]byte("third")),
	}
	snapshot, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		leaves,
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	proof, err := snapshot.InclusionProof(
		context.Background(),
		0,
		merkletree.DefaultProofLimits(),
	)
	if err != nil {
		t.Fatalf("proof: %v", err)
	}

	exactProofLimits := merkletree.ProofLimits{
		MaxElements:       uint64(len(proof.Siblings())),
		MaxTraversalDepth: 2,
		MaxLeafBytes:      uint64(len(leaves[0].Bytes())),
	}
	exactProof, err := snapshot.InclusionProof(
		context.Background(),
		0,
		exactProofLimits,
	)
	if err != nil {
		t.Fatalf("exact generation limits: %v", err)
	}
	if err := merkletree.VerifyInclusion(
		context.Background(),
		exactProof,
		leaves[0],
		exactProofLimits,
	); err != nil {
		t.Fatalf("exact verification limits: %v", err)
	}

	tests := map[string]struct {
		limits merkletree.ProofLimits
		leaf   merkletree.RawLeaf
		kind   merkletree.ResourceKind
	}{
		"elements": {
			limits: merkletree.ProofLimits{
				MaxElements:       1,
				MaxTraversalDepth: 64,
				MaxLeafBytes:      64,
			},
			leaf: leaves[0],
			kind: merkletree.ResourceProofElements,
		},
		"depth": {
			limits: merkletree.ProofLimits{
				MaxElements:       64,
				MaxTraversalDepth: 1,
				MaxLeafBytes:      64,
			},
			leaf: leaves[0],
			kind: merkletree.ResourceTraversalDepth,
		},
		"leaf bytes": {
			limits: merkletree.ProofLimits{
				MaxElements:       64,
				MaxTraversalDepth: 64,
				MaxLeafBytes:      2,
			},
			leaf: leaves[0],
			kind: merkletree.ResourceLeafBytes,
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			verifyErr := merkletree.VerifyInclusion(
				context.Background(),
				proof,
				test.leaf,
				test.limits,
			)
			var resourceError *merkletree.ResourceError
			if !errors.As(verifyErr, &resourceError) {
				t.Fatalf("error = %v, want ResourceError", verifyErr)
			}
			if resourceError.Kind != test.kind {
				t.Fatalf("resource kind = %d, want %d", resourceError.Kind, test.kind)
			}
		})
	}

	generationLimits := merkletree.ProofLimits{
		MaxElements:       1,
		MaxTraversalDepth: 64,
		MaxLeafBytes:      64,
	}
	if _, proofErr := snapshot.InclusionProof(
		context.Background(),
		0,
		generationLimits,
	); !resourceErrorHasKind(proofErr, merkletree.ResourceProofElements) {
		t.Fatalf("generation element limit error = %v", proofErr)
	}
	generationLimits = merkletree.ProofLimits{
		MaxElements:       64,
		MaxTraversalDepth: 1,
		MaxLeafBytes:      64,
	}
	if _, proofErr := snapshot.InclusionProof(
		context.Background(),
		0,
		generationLimits,
	); !resourceErrorHasKind(proofErr, merkletree.ResourceTraversalDepth) {
		t.Fatalf("generation depth limit error = %v", proofErr)
	}

	for name, mutate := range map[string]func(*merkletree.ProofLimits){
		"elements": func(limits *merkletree.ProofLimits) {
			limits.MaxElements = 0
		},
		"depth": func(limits *merkletree.ProofLimits) {
			limits.MaxTraversalDepth = 0
		},
		"leaf bytes": func(limits *merkletree.ProofLimits) {
			limits.MaxLeafBytes = 0
		},
	} {
		invalidLimits := merkletree.DefaultProofLimits()
		mutate(&invalidLimits)
		if _, proofErr := snapshot.InclusionProof(
			context.Background(),
			0,
			invalidLimits,
		); !errors.Is(proofErr, merkletree.ErrInvalidLimits) {
			t.Fatalf("%s zero generation limit error = %v", name, proofErr)
		}
		if verifyErr := merkletree.VerifyInclusion(
			context.Background(),
			proof,
			leaves[0],
			invalidLimits,
		); !errors.Is(verifyErr, merkletree.ErrInvalidLimits) {
			t.Fatalf("%s zero verification limit error = %v", name, verifyErr)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, proofErr := snapshot.InclusionProof(
		ctx,
		0,
		merkletree.DefaultProofLimits(),
	); !errors.Is(proofErr, context.Canceled) {
		t.Fatalf("canceled generation error = %v", proofErr)
	}
	if verifyErr := merkletree.VerifyInclusion(
		ctx,
		proof,
		leaves[0],
		merkletree.DefaultProofLimits(),
	); !errors.Is(verifyErr, context.Canceled) {
		t.Fatalf("canceled verification error = %v", verifyErr)
	}

	var nilContext context.Context
	if _, proofErr := snapshot.InclusionProof(
		nilContext,
		0,
		merkletree.DefaultProofLimits(),
	); !errors.Is(proofErr, merkletree.ErrInvalidContext) {
		t.Fatalf("nil generation context error = %v", proofErr)
	}
	if verifyErr := merkletree.VerifyInclusion(
		nilContext,
		proof,
		leaves[0],
		merkletree.DefaultProofLimits(),
	); !errors.Is(verifyErr, merkletree.ErrInvalidContext) {
		t.Fatalf("nil verification context error = %v", verifyErr)
	}
}

func TestSnapshotRejectsRetainedNodeClaimsBeforeAllocation(t *testing.T) {
	t.Parallel()

	if _, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		nil,
		merkletree.SnapshotLimits{},
	); !errors.Is(err, merkletree.ErrInvalidLimits) {
		t.Fatalf("zero snapshot limits error = %v", err)
	}
	invalidNodeLimit := merkletree.DefaultSnapshotLimits()
	invalidNodeLimit.MaxRetainedNodes = 0
	if _, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		nil,
		invalidNodeLimit,
	); !errors.Is(err, merkletree.ErrInvalidLimits) {
		t.Fatalf("zero retained-node limit error = %v", err)
	}
	invalidNodeLimit.MaxRetainedNodes = uint64(^uint(0)>>1) + 1
	if _, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		nil,
		invalidNodeLimit,
	); !errors.Is(err, merkletree.ErrInvalidLimits) {
		t.Fatalf("unrepresentable retained-node limit error = %v", err)
	}
	invalidNodeLimit.MaxRetainedNodes = uint64(^uint(0) >> 1)
	if _, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		nil,
		invalidNodeLimit,
	); err != nil {
		t.Fatalf("maximum representable retained-node limit: %v", err)
	}

	limits := merkletree.DefaultSnapshotLimits()
	limits.MaxRetainedNodes = 2
	_, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		[]merkletree.RawLeaf{
			merkletree.NewRawLeaf(nil),
			merkletree.NewRawLeaf(nil),
		},
		limits,
	)
	var resourceError *merkletree.ResourceError
	if !errors.As(err, &resourceError) {
		t.Fatalf("error = %v, want ResourceError", err)
	}
	if resourceError.Kind != merkletree.ResourceRetainedNodes ||
		resourceError.Limit != 2 ||
		resourceError.Actual != 3 {
		t.Fatalf("resource error = %#v", resourceError)
	}
	limits.MaxRetainedNodes = 3
	if _, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		[]merkletree.RawLeaf{
			merkletree.NewRawLeaf(nil),
			merkletree.NewRawLeaf(nil),
		},
		limits,
	); err != nil {
		t.Fatalf("exact retained-node limit: %v", err)
	}

	leafLimits := merkletree.DefaultSnapshotLimits()
	leafLimits.Construction.MaxLeaves = 1
	leafLimits.MaxRetainedNodes = 1
	if _, err = merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		[]merkletree.RawLeaf{merkletree.NewRawLeaf(nil)},
		leafLimits,
	); err != nil {
		t.Fatalf("exact snapshot leaf limit: %v", err)
	}
	_, err = merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		[]merkletree.RawLeaf{
			merkletree.NewRawLeaf(nil),
			merkletree.NewRawLeaf(nil),
		},
		leafLimits,
	)
	if !resourceErrorHasKind(err, merkletree.ResourceLeaves) {
		t.Fatalf("snapshot leaf limit error = %v", err)
	}
}

func TestDefaultSnapshotAndProofLimitsAreStable(t *testing.T) {
	t.Parallel()

	snapshot := merkletree.DefaultSnapshotLimits()
	if snapshot.Construction.MaxLeaves != 1<<19 ||
		snapshot.Construction.MaxLeafBytes != 16<<20 ||
		snapshot.Construction.MaxTotalBytes != 1<<30 ||
		snapshot.MaxRetainedNodes != 1<<20-1 {
		t.Fatalf("snapshot defaults = %#v", snapshot)
	}
	proof := merkletree.DefaultProofLimits()
	if proof.MaxElements != 64 ||
		proof.MaxTraversalDepth != 64 ||
		proof.MaxLeafBytes != 16<<20 {
		t.Fatalf("proof defaults = %#v", proof)
	}
}

func TestRFC9162InclusionProofMatchesIndependentAuditPaths(t *testing.T) {
	t.Parallel()

	profile, err := merkletree.RFC9162Profile(merkletree.HashSHA256)
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	for _, size := range []int{1, 2, 3, 5, 8, 65} {
		raw := make([][]byte, size)
		leaves := make([]merkletree.RawLeaf, size)
		for index := range size {
			raw[index] = []byte{byte(index), byte(index >> 8), 0x5a}
			leaves[index] = merkletree.NewRawLeaf(raw[index])
		}
		snapshot, snapshotErr := merkletree.NewSnapshot(
			context.Background(),
			profile,
			leaves,
			merkletree.DefaultSnapshotLimits(),
		)
		if snapshotErr != nil {
			t.Fatalf("size %d: snapshot: %v", size, snapshotErr)
		}

		for index, leaf := range leaves {
			proof, proofErr := snapshot.InclusionProof(
				context.Background(),
				uint64(index),
				merkletree.DefaultProofLimits(),
			)
			if proofErr != nil {
				t.Fatalf("size %d index %d: proof: %v", size, index, proofErr)
			}
			got := proof.Siblings()
			want := referenceAuditPath(raw, index)
			if len(got) != len(want) {
				t.Fatalf(
					"size %d index %d: path length = %d, want %d",
					size,
					index,
					len(got),
					len(want),
				)
			}
			for pathIndex := range want {
				if !equalBytes(got[pathIndex].Bytes(), want[pathIndex]) {
					t.Fatalf(
						"size %d index %d path %d differs",
						size,
						index,
						pathIndex,
					)
				}
			}
			if verifyErr := merkletree.VerifyInclusion(
				context.Background(),
				proof,
				leaf,
				merkletree.DefaultProofLimits(),
			); verifyErr != nil {
				t.Fatalf(
					"size %d index %d: verify: %v",
					size,
					index,
					verifyErr,
				)
			}
		}
	}
}

func mustSnapshotRoot(t *testing.T, snapshot merkletree.Snapshot) merkletree.Root {
	t.Helper()

	root, err := snapshot.Root()
	if err != nil {
		t.Fatalf("snapshot root: %v", err)
	}

	return root
}

func resourceErrorHasKind(err error, kind merkletree.ResourceKind) bool {
	var resourceError *merkletree.ResourceError

	return errors.As(err, &resourceError) && resourceError.Kind == kind
}
