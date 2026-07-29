package merkletree

import (
	"context"
	"errors"
	"math/bits"
	"testing"
)

func TestMultiProofLimitsContextsAndResources(t *testing.T) {
	t.Parallel()

	invalidLimits := []MultiProofLimits{{}}
	for field := 0; field < 5; field++ {
		limits := DefaultMultiProofLimits()
		switch field {
		case 0:
			limits.MaxLeaves = 0
		case 1:
			limits.MaxElements = 0
		case 2:
			limits.MaxTraversalDepth = 0
		case 3:
			limits.MaxLeafBytes = 0
		case 4:
			limits.MaxTotalLeafBytes = 0
		}
		invalidLimits = append(invalidLimits, limits)
	}
	for _, limits := range invalidLimits {
		if err := limits.validate(); !errors.Is(err, ErrInvalidLimits) {
			t.Fatalf("limits.validate(%+v) error = %v", limits, err)
		}
	}

	snapshot, leaves := testMultiSnapshot(t, 8)
	var nilContext context.Context
	if _, err := snapshot.MultiInclusionProof(
		nilContext,
		[]uint64{1},
		DefaultMultiProofLimits(),
	); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("MultiInclusionProof(nil context) error = %v", err)
	}
	var zero Snapshot
	if _, err := zero.MultiInclusionProof(
		context.Background(),
		[]uint64{1},
		DefaultMultiProofLimits(),
	); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("zero MultiInclusionProof() error = %v", err)
	}
	if _, err := snapshot.MultiInclusionProof(
		context.Background(),
		[]uint64{1},
		MultiProofLimits{},
	); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("MultiInclusionProof(invalid limits) error = %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := snapshot.MultiInclusionProof(
		cancelled,
		[]uint64{1},
		DefaultMultiProofLimits(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("MultiInclusionProof(cancelled) error = %v", err)
	}

	limits := DefaultMultiProofLimits()
	limits.MaxTraversalDepth = 3
	if _, err := snapshot.MultiInclusionProof(
		context.Background(),
		[]uint64{1},
		limits,
	); !errors.Is(err, ErrResourceExhausted) {
		t.Fatalf("MultiInclusionProof(depth limit) error = %v", err)
	}
	limits = DefaultMultiProofLimits()
	limits.MaxElements = 1
	_, err := snapshot.MultiInclusionProof(
		context.Background(),
		[]uint64{1, 6},
		limits,
	)
	var elementErr *ResourceError
	if !errors.As(err, &elementErr) ||
		elementErr.Kind != ResourceProofElements ||
		elementErr.Actual != 2 {
		t.Fatalf("MultiInclusionProof(element limit) error = %v", err)
	}

	proof := testMultiProof(t, []uint64{1, 6})
	if err := VerifyMultiInclusion(
		nilContext,
		proof,
		[]RawLeaf{leaves[1], leaves[6]},
		DefaultMultiProofLimits(),
	); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("VerifyMultiInclusion(nil context) error = %v", err)
	}
	if err := VerifyMultiInclusion(
		context.Background(),
		proof,
		[]RawLeaf{leaves[1], leaves[6]},
		MultiProofLimits{},
	); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("VerifyMultiInclusion(invalid limits) error = %v", err)
	}
	if err := VerifyMultiInclusion(
		cancelled,
		proof,
		[]RawLeaf{leaves[1], leaves[6]},
		DefaultMultiProofLimits(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("VerifyMultiInclusion(cancelled) error = %v", err)
	}

	limits = DefaultMultiProofLimits()
	limits.MaxLeaves = 1
	assertResourceError(
		t,
		VerifyMultiInclusion(
			context.Background(),
			proof,
			[]RawLeaf{leaves[1], leaves[6]},
			limits,
		),
		ResourceLeaves,
	)
	limits = DefaultMultiProofLimits()
	limits.MaxElements = 1
	assertResourceError(
		t,
		VerifyMultiInclusion(
			context.Background(),
			proof,
			[]RawLeaf{leaves[1], leaves[6]},
			limits,
		),
		ResourceProofElements,
	)
	deep := cloneMultiProof(proof)
	deep.treeSize = 1 << 63
	limits = DefaultMultiProofLimits()
	limits.MaxTraversalDepth = 63
	assertResourceError(
		t,
		VerifyMultiInclusion(
			context.Background(),
			deep,
			[]RawLeaf{leaves[1], leaves[6]},
			limits,
		),
		ResourceTraversalDepth,
	)

	if err := VerifyMultiInclusion(
		context.Background(),
		proof,
		leaves[1:2],
		DefaultMultiProofLimits(),
	); !errors.Is(err, ErrVerificationFailed) {
		t.Fatalf("VerifyMultiInclusion(missing leaf) error = %v", err)
	}
	oversized := []RawLeaf{
		NewRawLeaf([]byte("oversized")),
		leaves[6],
	}
	limits = DefaultMultiProofLimits()
	limits.MaxLeafBytes = 1
	assertResourceError(
		t,
		VerifyMultiInclusion(
			context.Background(),
			proof,
			oversized,
			limits,
		),
		ResourceLeafBytes,
	)
	limits = DefaultMultiProofLimits()
	limits.MaxTotalLeafBytes = 3
	assertResourceError(
		t,
		VerifyMultiInclusion(
			context.Background(),
			proof,
			[]RawLeaf{leaves[1], leaves[6]},
			limits,
		),
		ResourceTotalBytes,
	)
	changedLeaves := []RawLeaf{
		NewRawLeaf([]byte("changed")),
		leaves[6],
	}
	if err := VerifyMultiInclusion(
		context.Background(),
		proof,
		changedLeaves,
		DefaultMultiProofLimits(),
	); !errors.Is(err, ErrVerificationFailed) {
		t.Fatalf("VerifyMultiInclusion(changed leaf) error = %v", err)
	}

	variedLeaves := []RawLeaf{
		NewRawLeaf([]byte("a")),
		NewRawLeaf([]byte("bb")),
		NewRawLeaf([]byte("ccc")),
	}
	variedSnapshot, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		variedLeaves,
		DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot(varied leaves) error = %v", err)
	}
	variedProof, err := variedSnapshot.MultiInclusionProof(
		context.Background(),
		[]uint64{0, 1, 2},
		DefaultMultiProofLimits(),
	)
	if err != nil {
		t.Fatalf("MultiInclusionProof(varied leaves) error = %v", err)
	}
	limits = DefaultMultiProofLimits()
	limits.MaxTotalLeafBytes = 5
	assertResourceError(
		t,
		VerifyMultiInclusion(
			context.Background(),
			variedProof,
			variedLeaves,
			limits,
		),
		ResourceTotalBytes,
	)
}

func TestVerifyMultiInclusionRejectsMalformedAndNonVerifyingProofs(
	t *testing.T,
) {
	t.Parallel()

	proof := testMultiProof(t, []uint64{1, 6})
	_, leaves := testMultiSnapshot(t, 8)
	selected := []RawLeaf{leaves[1], leaves[6]}

	missing := cloneMultiProof(proof)
	missing.frontier = missing.frontier[:len(missing.frontier)-1]
	surplus := cloneMultiProof(proof)
	surplus.frontier = append(surplus.frontier, proof.root.digest)
	reorderedFrontier := cloneMultiProof(proof)
	reorderedFrontier.frontier[0], reorderedFrontier.frontier[1] =
		reorderedFrontier.frontier[1], reorderedFrontier.frontier[0]
	changedFrontier := cloneMultiProof(proof)
	changedFrontier.frontier[0].value[0] ^= 0xff
	changedRoot := cloneMultiProof(proof)
	changedRoot.root.digest.value[0] ^= 0xff
	unsupportedAlgorithm := cloneMultiProof(proof)
	unsupportedAlgorithm.algorithm = HashAlgorithm(0xff)
	unsupportedProfile := cloneMultiProof(proof)
	unsupportedProfile.profileID = ProfileID(0xff)
	unsupportedVersion := cloneMultiProof(proof)
	unsupportedVersion.profileVersion++
	wrongTreeSize := cloneMultiProof(proof)
	wrongTreeSize.root.treeSize++
	wrongRootProfile := cloneMultiProof(proof)
	wrongRootProfile.root.profileID = ProfileRFC9162
	wrongRootVersion := cloneMultiProof(proof)
	wrongRootVersion.root.profileVersion++
	wrongRootAlgorithm := cloneMultiProof(proof)
	wrongRootAlgorithm.root.algorithm = HashAlgorithm(0xff)
	wrongRootDigestAlgorithm := cloneMultiProof(proof)
	wrongRootDigestAlgorithm.root.digest.algorithm = HashAlgorithm(0xff)
	emptyIndexes := cloneMultiProof(proof)
	emptyIndexes.leafIndexes = nil
	emptyIndexes.leafDigests = nil
	missingDigest := cloneMultiProof(proof)
	missingDigest.leafDigests = missingDigest.leafDigests[:1]
	duplicateIndex := cloneMultiProof(proof)
	duplicateIndex.leafIndexes[1] = duplicateIndex.leafIndexes[0]
	reorderedIndexes := cloneMultiProof(proof)
	reorderedIndexes.leafIndexes[0], reorderedIndexes.leafIndexes[1] =
		reorderedIndexes.leafIndexes[1], reorderedIndexes.leafIndexes[0]
	outOfRange := cloneMultiProof(proof)
	outOfRange.leafIndexes[1] = outOfRange.treeSize
	wrongLeafAlgorithm := cloneMultiProof(proof)
	wrongLeafAlgorithm.leafDigests[0].algorithm = HashAlgorithm(0xff)
	wrongFrontierAlgorithm := cloneMultiProof(proof)
	wrongFrontierAlgorithm.frontier[0].algorithm = HashAlgorithm(0xff)
	zero := MultiInclusionProof{}

	tests := []struct {
		name  string
		proof MultiInclusionProof
		want  error
	}{
		{name: "missing frontier", proof: missing, want: ErrMalformedProof},
		{name: "surplus frontier", proof: surplus, want: ErrMalformedProof},
		{
			name:  "reordered frontier",
			proof: reorderedFrontier,
			want:  ErrVerificationFailed,
		},
		{
			name:  "changed frontier",
			proof: changedFrontier,
			want:  ErrVerificationFailed,
		},
		{name: "changed root", proof: changedRoot, want: ErrVerificationFailed},
		{
			name:  "unsupported algorithm",
			proof: unsupportedAlgorithm,
			want:  ErrUnsupportedAlgorithm,
		},
		{
			name:  "unsupported profile",
			proof: unsupportedProfile,
			want:  ErrUnsupportedProfile,
		},
		{
			name:  "unsupported version",
			proof: unsupportedVersion,
			want:  ErrUnsupportedProfile,
		},
		{name: "wrong tree size", proof: wrongTreeSize, want: ErrMalformedProof},
		{
			name:  "wrong root profile",
			proof: wrongRootProfile,
			want:  ErrMalformedProof,
		},
		{
			name:  "wrong root version",
			proof: wrongRootVersion,
			want:  ErrMalformedProof,
		},
		{
			name:  "wrong root algorithm",
			proof: wrongRootAlgorithm,
			want:  ErrMalformedProof,
		},
		{
			name:  "wrong root digest algorithm",
			proof: wrongRootDigestAlgorithm,
			want:  ErrMalformedProof,
		},
		{name: "empty indexes", proof: emptyIndexes, want: ErrMalformedProof},
		{name: "missing digest", proof: missingDigest, want: ErrMalformedProof},
		{name: "duplicate index", proof: duplicateIndex, want: ErrMalformedProof},
		{
			name:  "reordered indexes",
			proof: reorderedIndexes,
			want:  ErrMalformedProof,
		},
		{name: "out of range", proof: outOfRange, want: ErrMalformedProof},
		{
			name:  "wrong leaf algorithm",
			proof: wrongLeafAlgorithm,
			want:  ErrMalformedProof,
		},
		{
			name:  "wrong frontier algorithm",
			proof: wrongFrontierAlgorithm,
			want:  ErrMalformedProof,
		},
		{name: "zero value", proof: zero, want: ErrUnsupportedAlgorithm},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.name == "duplicate index" ||
				test.name == "reordered indexes" {
				if err := test.proof.validate(); !errors.Is(
					err,
					ErrMalformedProof,
				) {
					t.Fatalf(
						"proof.validate() error = %v, want ErrMalformedProof",
						err,
					)
				}
			}
			err := VerifyMultiInclusion(
				context.Background(),
				test.proof,
				selected,
				DefaultMultiProofLimits(),
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("VerifyMultiInclusion() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestMultiInclusionHonorsEveryCancellationCheckpoint(t *testing.T) {
	t.Parallel()

	snapshot, leaves := testMultiSnapshot(t, 65)
	indexes := []uint64{0, 7, 31, 32, 64}
	var proof MultiInclusionProof
	generationSucceeded := false
	for allowedCalls := 0; allowedCalls < 200; allowedCalls++ {
		ctx := &checkpointContext{
			remaining: allowedCalls,
			done:      make(chan struct{}),
		}
		result, err := snapshot.MultiInclusionProof(
			ctx,
			indexes,
			DefaultMultiProofLimits(),
		)
		if err == nil {
			proof = result
			generationSucceeded = true

			break
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("generation checkpoint %d: error = %v", allowedCalls, err)
		}
	}
	if !generationSucceeded {
		t.Fatal("generation never passed all cancellation checkpoints")
	}

	selected := []RawLeaf{
		leaves[0],
		leaves[7],
		leaves[31],
		leaves[32],
		leaves[64],
	}
	verificationSucceeded := false
	for allowedCalls := 0; allowedCalls < 200; allowedCalls++ {
		ctx := &checkpointContext{
			remaining: allowedCalls,
			done:      make(chan struct{}),
		}
		err := VerifyMultiInclusion(
			ctx,
			proof,
			selected,
			DefaultMultiProofLimits(),
		)
		if err == nil {
			verificationSucceeded = true

			break
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("verification checkpoint %d: error = %v", allowedCalls, err)
		}
	}
	if !verificationSucceeded {
		t.Fatal("verification never passed all cancellation checkpoints")
	}
}

func TestMultiProofExactLimits(t *testing.T) {
	t.Parallel()

	proof := testMultiProof(t, []uint64{1, 6})
	snapshot, leaves := testMultiSnapshot(t, 8)
	selected := []RawLeaf{leaves[1], leaves[6]}
	limits := DefaultMultiProofLimits()
	limits.MaxLeaves = uint64(len(proof.leafIndexes))
	limits.MaxElements = uint64(len(proof.frontier))
	limits.MaxTraversalDepth = uint64(bits.Len64(proof.treeSize))
	limits.MaxLeafBytes = uint64(len(leaves[0].value))
	var total uint64
	for _, leaf := range selected {
		size := uint64(len(leaf.value))
		total += size
	}
	limits.MaxTotalLeafBytes = total
	if _, err := snapshot.MultiInclusionProof(
		context.Background(),
		proof.leafIndexes,
		limits,
	); err != nil {
		t.Fatalf("MultiInclusionProof(exact limits) error = %v", err)
	}
	if err := VerifyMultiInclusion(
		context.Background(),
		proof,
		selected,
		limits,
	); err != nil {
		t.Fatalf("VerifyMultiInclusion(exact limits) error = %v", err)
	}
}

func assertResourceError(t *testing.T, err error, kind ResourceKind) {
	t.Helper()

	var resourceErr *ResourceError
	if !errors.As(err, &resourceErr) || resourceErr.Kind != kind {
		t.Fatalf("error = %v, want ResourceError kind %d", err, kind)
	}
}

func testMultiSnapshot(
	t *testing.T,
	size int,
) (Snapshot, []RawLeaf) {
	t.Helper()

	leaves := make([]RawLeaf, size)
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
		t.Fatalf("NewSnapshot() error = %v", err)
	}

	return snapshot, leaves
}

func testMultiProof(t *testing.T, indexes []uint64) MultiInclusionProof {
	t.Helper()

	snapshot, _ := testMultiSnapshot(t, 8)
	proof, err := snapshot.MultiInclusionProof(
		context.Background(),
		indexes,
		DefaultMultiProofLimits(),
	)
	if err != nil {
		t.Fatalf("MultiInclusionProof() error = %v", err)
	}

	return proof
}

func cloneMultiProof(proof MultiInclusionProof) MultiInclusionProof {
	proof.leafIndexes = append([]uint64(nil), proof.leafIndexes...)
	proof.leafDigests = append([]Digest(nil), proof.leafDigests...)
	proof.frontier = append([]Digest(nil), proof.frontier...)

	return proof
}
