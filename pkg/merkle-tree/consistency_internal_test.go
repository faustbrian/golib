package merkletree

import (
	"context"
	"errors"
	"math/bits"
	"testing"
)

func TestVerifyConsistencyRejectsMalformedAndNonVerifyingProofs(
	t *testing.T,
) {
	t.Parallel()

	proof := testConsistencyProof(t, 3, 7)
	missing := cloneConsistencyProof(proof)
	missing.nodes = missing.nodes[:len(missing.nodes)-1]
	surplus := cloneConsistencyProof(proof)
	surplus.nodes = append(surplus.nodes, surplus.nodes[0])
	reordered := cloneConsistencyProof(proof)
	reordered.nodes[0], reordered.nodes[1] =
		reordered.nodes[1], reordered.nodes[0]
	changedNode := cloneConsistencyProof(proof)
	changedNode.nodes[0].value[0] ^= 0xff
	changedOlderRoot := cloneConsistencyProof(proof)
	changedOlderRoot.olderRoot.digest.value[0] ^= 0xff
	changedNewerRoot := cloneConsistencyProof(proof)
	changedNewerRoot.newerRoot.digest.value[0] ^= 0xff
	unsupportedAlgorithm := cloneConsistencyProof(proof)
	unsupportedAlgorithm.algorithm = HashAlgorithm(255)
	unsupportedProfile := cloneConsistencyProof(proof)
	unsupportedProfile.profileID = ProfileID(255)
	unsupportedVersion := cloneConsistencyProof(proof)
	unsupportedVersion.profileVersion++
	zeroOlderSize := cloneConsistencyProof(proof)
	zeroOlderSize.olderTreeSize = 0
	zeroOlderSize.olderRoot.treeSize = 0
	olderAfterNewer := cloneConsistencyProof(proof)
	olderAfterNewer.olderTreeSize = olderAfterNewer.newerTreeSize + 1
	olderAfterNewer.olderRoot.treeSize = olderAfterNewer.olderTreeSize
	wrongOlderSize := cloneConsistencyProof(proof)
	wrongOlderSize.olderRoot.treeSize++
	wrongNewerSize := cloneConsistencyProof(proof)
	wrongNewerSize.newerRoot.treeSize++
	wrongOlderProfile := cloneConsistencyProof(proof)
	wrongOlderProfile.olderRoot.profileID = ProfileRFC9162
	wrongNewerVersion := cloneConsistencyProof(proof)
	wrongNewerVersion.newerRoot.profileVersion++
	wrongOlderAlgorithm := cloneConsistencyProof(proof)
	wrongOlderAlgorithm.olderRoot.algorithm = HashAlgorithm(255)
	wrongNewerDigestAlgorithm := cloneConsistencyProof(proof)
	wrongNewerDigestAlgorithm.newerRoot.digest.algorithm = HashAlgorithm(255)
	wrongNodeAlgorithm := cloneConsistencyProof(proof)
	wrongNodeAlgorithm.nodes[0].algorithm = HashAlgorithm(255)

	tests := []struct {
		name  string
		proof ConsistencyProof
		want  error
	}{
		{name: "missing", proof: missing, want: ErrMalformedProof},
		{name: "surplus", proof: surplus, want: ErrMalformedProof},
		{name: "reordered", proof: reordered, want: ErrVerificationFailed},
		{name: "changed node", proof: changedNode, want: ErrVerificationFailed},
		{
			name:  "changed older root",
			proof: changedOlderRoot,
			want:  ErrVerificationFailed,
		},
		{
			name:  "changed newer root",
			proof: changedNewerRoot,
			want:  ErrVerificationFailed,
		},
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
		{name: "zero older size", proof: zeroOlderSize, want: ErrMalformedProof},
		{
			name:  "older after newer",
			proof: olderAfterNewer,
			want:  ErrMalformedProof,
		},
		{
			name:  "wrong older root size",
			proof: wrongOlderSize,
			want:  ErrMalformedProof,
		},
		{
			name:  "wrong newer root size",
			proof: wrongNewerSize,
			want:  ErrMalformedProof,
		},
		{
			name:  "wrong older root profile",
			proof: wrongOlderProfile,
			want:  ErrMalformedProof,
		},
		{
			name:  "wrong newer root version",
			proof: wrongNewerVersion,
			want:  ErrMalformedProof,
		},
		{
			name:  "wrong older root algorithm",
			proof: wrongOlderAlgorithm,
			want:  ErrMalformedProof,
		},
		{
			name:  "wrong newer digest algorithm",
			proof: wrongNewerDigestAlgorithm,
			want:  ErrMalformedProof,
		},
		{
			name:  "wrong node algorithm",
			proof: wrongNodeAlgorithm,
			want:  ErrMalformedProof,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.name == "zero older size" ||
				test.name == "older after newer" {
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
			err := VerifyConsistency(
				context.Background(),
				test.proof,
				DefaultConsistencyProofLimits(),
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("VerifyConsistency() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestConsistencyProofEqualSizeRequiresIdenticalRoots(t *testing.T) {
	t.Parallel()

	proof := testConsistencyProof(t, 4, 4)
	if len(proof.nodes) != 0 {
		t.Fatalf("equal-size proof has %d nodes, want 0", len(proof.nodes))
	}
	if err := VerifyConsistency(
		context.Background(),
		proof,
		DefaultConsistencyProofLimits(),
	); err != nil {
		t.Fatalf("VerifyConsistency(equal roots) error = %v", err)
	}

	changed := cloneConsistencyProof(proof)
	changed.olderRoot.digest.value[0] ^= 0xff
	if err := VerifyConsistency(
		context.Background(),
		changed,
		DefaultConsistencyProofLimits(),
	); !errors.Is(err, ErrVerificationFailed) {
		t.Fatalf("VerifyConsistency(changed equal root) error = %v", err)
	}

	surplus := cloneConsistencyProof(proof)
	surplus.nodes = []Digest{proof.olderRoot.digest}
	if err := VerifyConsistency(
		context.Background(),
		surplus,
		DefaultConsistencyProofLimits(),
	); !errors.Is(err, ErrMalformedProof) {
		t.Fatalf("VerifyConsistency(surplus equal proof) error = %v", err)
	}
}

func TestConsistencyProofSupportsEqualEmptyTrees(t *testing.T) {
	t.Parallel()

	snapshot, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		nil,
		DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	root, err := snapshot.Root()
	if err != nil {
		t.Fatalf("Root() error = %v", err)
	}
	proof, err := snapshot.ConsistencyProof(
		context.Background(),
		root,
		DefaultConsistencyProofLimits(),
	)
	if err != nil {
		t.Fatalf("ConsistencyProof() error = %v", err)
	}
	if len(proof.nodes) != 0 {
		t.Fatalf("equal empty proof has %d nodes, want 0", len(proof.nodes))
	}
	if err := VerifyConsistency(
		context.Background(),
		proof,
		DefaultConsistencyProofLimits(),
	); err != nil {
		t.Fatalf("VerifyConsistency() error = %v", err)
	}
}

func TestConsistencyProofGenerationRejectsUnrelatedOlderRoot(t *testing.T) {
	t.Parallel()

	leaves := []RawLeaf{
		NewRawLeaf([]byte("first")),
		NewRawLeaf([]byte("second")),
		NewRawLeaf([]byte("third")),
	}
	older, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		leaves[:2],
		DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot(older) error = %v", err)
	}
	newer, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		leaves,
		DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot(newer) error = %v", err)
	}
	olderRoot, err := older.Root()
	if err != nil {
		t.Fatalf("older.Root() error = %v", err)
	}
	olderRoot.digest.value[0] ^= 0xff
	if _, err := newer.ConsistencyProof(
		context.Background(),
		olderRoot,
		DefaultConsistencyProofLimits(),
	); !errors.Is(err, ErrVerificationFailed) {
		t.Fatalf("ConsistencyProof(unrelated older root) error = %v", err)
	}

	sameRoot, err := newer.Root()
	if err != nil {
		t.Fatalf("newer.Root() error = %v", err)
	}
	sameRoot.digest.value[0] ^= 0xff
	if _, err := newer.ConsistencyProof(
		context.Background(),
		sameRoot,
		DefaultConsistencyProofLimits(),
	); !errors.Is(err, ErrVerificationFailed) {
		t.Fatalf("ConsistencyProof(changed equal root) error = %v", err)
	}
}

func TestConsistencyProofLimitsContextsAndCancellation(t *testing.T) {
	t.Parallel()

	if got := DefaultConsistencyProofLimits(); got != (ConsistencyProofLimits{
		MaxElements:       65,
		MaxTraversalDepth: 64,
	}) {
		t.Fatalf("DefaultConsistencyProofLimits() = %+v", got)
	}
	for _, limits := range []ConsistencyProofLimits{
		{},
		{MaxElements: 1},
		{MaxTraversalDepth: 1},
	} {
		if err := limits.validate(); !errors.Is(err, ErrInvalidLimits) {
			t.Fatalf("limits.validate(%+v) error = %v", limits, err)
		}
	}

	proof := testConsistencyProof(t, 3, 7)
	leaves := []RawLeaf{
		NewRawLeaf([]byte("first")),
		NewRawLeaf([]byte("second")),
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
	older, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		leaves[:1],
		DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot(older) error = %v", err)
	}
	olderRoot, err := older.Root()
	if err != nil {
		t.Fatalf("older.Root() error = %v", err)
	}
	var nilContext context.Context
	if _, err := snapshot.ConsistencyProof(
		nilContext,
		olderRoot,
		DefaultConsistencyProofLimits(),
	); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("ConsistencyProof(nil context) error = %v", err)
	}
	var zero Snapshot
	if _, err := zero.ConsistencyProof(
		context.Background(),
		olderRoot,
		DefaultConsistencyProofLimits(),
	); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("zero ConsistencyProof() error = %v", err)
	}
	if _, err := snapshot.ConsistencyProof(
		context.Background(),
		olderRoot,
		ConsistencyProofLimits{},
	); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("ConsistencyProof(invalid limits) error = %v", err)
	}
	exact := DefaultConsistencyProofLimits()
	exact.MaxElements = uint64(len(proof.nodes))
	exact.MaxTraversalDepth = uint64(bits.Len64(proof.newerTreeSize))
	if err := VerifyConsistency(
		context.Background(),
		proof,
		exact,
	); err != nil {
		t.Fatalf("VerifyConsistency(exact limits) error = %v", err)
	}
	sevenLeaves := make([]RawLeaf, 7)
	for index := range sevenLeaves {
		sevenLeaves[index] = NewRawLeaf([]byte{byte(index)})
	}
	seven, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		sevenLeaves,
		DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot(seven) error = %v", err)
	}
	three, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		sevenLeaves[:3],
		DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot(three) error = %v", err)
	}
	threeRoot, err := three.Root()
	if err != nil {
		t.Fatalf("three.Root() error = %v", err)
	}
	if _, err := seven.ConsistencyProof(
		context.Background(),
		threeRoot,
		exact,
	); err != nil {
		t.Fatalf("ConsistencyProof(exact limits) error = %v", err)
	}
	if err := VerifyConsistency(
		nilContext,
		proof,
		DefaultConsistencyProofLimits(),
	); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("VerifyConsistency(nil context) error = %v", err)
	}
	if err := VerifyConsistency(
		context.Background(),
		proof,
		ConsistencyProofLimits{},
	); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("VerifyConsistency(invalid limits) error = %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := VerifyConsistency(
		cancelled,
		proof,
		DefaultConsistencyProofLimits(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("VerifyConsistency(cancelled) error = %v", err)
	}

	resourceProof := cloneConsistencyProof(proof)
	resourceProof.nodes = append(
		resourceProof.nodes,
		make([]Digest, 64)...,
	)
	limits := DefaultConsistencyProofLimits()
	limits.MaxElements = 64
	var resourceErr *ResourceError
	if err := VerifyConsistency(
		context.Background(),
		resourceProof,
		limits,
	); !errors.As(err, &resourceErr) ||
		resourceErr.Kind != ResourceProofElements ||
		resourceErr.Actual != 68 {
		t.Fatalf("VerifyConsistency(element limit) error = %v", err)
	}

	deep := cloneConsistencyProof(proof)
	deep.newerTreeSize = 1 << 63
	limits = DefaultConsistencyProofLimits()
	limits.MaxTraversalDepth = 63
	if err := VerifyConsistency(
		context.Background(),
		deep,
		limits,
	); !errors.As(err, &resourceErr) ||
		resourceErr.Kind != ResourceTraversalDepth ||
		resourceErr.Actual != 64 {
		t.Fatalf("VerifyConsistency(depth limit) error = %v", err)
	}
}

func TestConsistencyProofHonorsEveryCancellationCheckpoint(t *testing.T) {
	t.Parallel()

	leaves := make([]RawLeaf, 65)
	for index := range leaves {
		leaves[index] = NewRawLeaf([]byte{byte(index)})
	}
	older, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		leaves[:31],
		DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot(older) error = %v", err)
	}
	newer, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		leaves,
		DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot(newer) error = %v", err)
	}
	olderRoot, err := older.Root()
	if err != nil {
		t.Fatalf("older.Root() error = %v", err)
	}

	var proof ConsistencyProof
	generationSucceeded := false
	for allowedCalls := 0; allowedCalls < 100; allowedCalls++ {
		ctx := &checkpointContext{
			remaining: allowedCalls,
			done:      make(chan struct{}),
		}
		result, proofErr := newer.ConsistencyProof(
			ctx,
			olderRoot,
			DefaultConsistencyProofLimits(),
		)
		if proofErr == nil {
			proof = result
			generationSucceeded = true

			break
		}
		if !errors.Is(proofErr, context.Canceled) {
			t.Fatalf("generation checkpoint %d: error = %v", allowedCalls, proofErr)
		}
	}
	if !generationSucceeded {
		t.Fatal("generation never passed all cancellation checkpoints")
	}

	verificationSucceeded := false
	for allowedCalls := 0; allowedCalls < 100; allowedCalls++ {
		ctx := &checkpointContext{
			remaining: allowedCalls,
			done:      make(chan struct{}),
		}
		verifyErr := VerifyConsistency(
			ctx,
			proof,
			DefaultConsistencyProofLimits(),
		)
		if verifyErr == nil {
			verificationSucceeded = true

			break
		}
		if !errors.Is(verifyErr, context.Canceled) {
			t.Fatalf(
				"verification checkpoint %d: error = %v",
				allowedCalls,
				verifyErr,
			)
		}
	}
	if !verificationSucceeded {
		t.Fatal("verification never passed all cancellation checkpoints")
	}
}

func TestConsistencyPathLengthCoversUint64Boundary(t *testing.T) {
	t.Parallel()

	if got := consistencyPathLength(1, ^uint64(0)); got != 64 {
		t.Fatalf("consistencyPathLength(1, MaxUint64) = %d, want 64", got)
	}
	if got := consistencyPathLength(1<<63+1, ^uint64(0)); got != 65 {
		t.Fatalf(
			"consistencyPathLength(2^63+1, MaxUint64) = %d, want 65",
			got,
		)
	}
	if got := consistencyPathLength(3, 7); got != 4 {
		t.Fatalf("consistencyPathLength(3, 7) = %d, want 4", got)
	}
	if got := consistencyPathLength(6, 7); got != 3 {
		t.Fatalf("consistencyPathLength(6, 7) = %d, want 3", got)
	}
	if got := consistencyPathLength(5, 6); got != 3 {
		t.Fatalf("consistencyPathLength(5, 6) = %d, want 3", got)
	}
}

func testConsistencyProof(
	t *testing.T,
	olderSize int,
	newerSize int,
) ConsistencyProof {
	t.Helper()

	leaves := make([]RawLeaf, newerSize)
	for index := range leaves {
		leaves[index] = NewRawLeaf([]byte{byte(index)})
	}
	newer, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		leaves,
		DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot(newer) error = %v", err)
	}
	older, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		leaves[:olderSize],
		DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot(older) error = %v", err)
	}
	olderRoot, err := older.Root()
	if err != nil {
		t.Fatalf("older.Root() error = %v", err)
	}
	proof, err := newer.ConsistencyProof(
		context.Background(),
		olderRoot,
		DefaultConsistencyProofLimits(),
	)
	if err != nil {
		t.Fatalf("ConsistencyProof() error = %v", err)
	}

	return proof
}

func cloneConsistencyProof(proof ConsistencyProof) ConsistencyProof {
	proof.nodes = append([]Digest(nil), proof.nodes...)

	return proof
}
