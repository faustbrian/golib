package merkletree

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestVerifyInclusionRejectsMalformedAndNonVerifyingProofs(t *testing.T) {
	t.Parallel()

	leaves := []RawLeaf{
		NewRawLeaf([]byte("first")),
		NewRawLeaf([]byte("second")),
		NewRawLeaf([]byte("third")),
		NewRawLeaf([]byte("fourth")),
		NewRawLeaf([]byte("fifth")),
	}
	snapshot, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		leaves,
		DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	valid, err := snapshot.InclusionProof(
		context.Background(),
		1,
		DefaultProofLimits(),
	)
	if err != nil {
		t.Fatalf("proof: %v", err)
	}

	tests := map[string]struct {
		mutate func(*InclusionProof)
		want   error
	}{
		"missing sibling": {
			mutate: func(proof *InclusionProof) {
				proof.siblings = proof.siblings[:len(proof.siblings)-1]
			},
			want: ErrMalformedProof,
		},
		"surplus sibling": {
			mutate: func(proof *InclusionProof) {
				proof.siblings = append(proof.siblings, proof.siblings[0])
			},
			want: ErrMalformedProof,
		},
		"out-of-range index": {
			mutate: func(proof *InclusionProof) {
				proof.leafIndex = proof.treeSize
			},
			want: ErrMalformedProof,
		},
		"wrong bound tree size": {
			mutate: func(proof *InclusionProof) {
				proof.treeSize++
			},
			want: ErrMalformedProof,
		},
		"wrong root profile": {
			mutate: func(proof *InclusionProof) {
				proof.root.profileID = ProfileRFC9162
			},
			want: ErrMalformedProof,
		},
		"wrong sibling algorithm": {
			mutate: func(proof *InclusionProof) {
				proof.siblings[0].algorithm = HashAlgorithm(255)
			},
			want: ErrMalformedProof,
		},
		"unsupported proof profile": {
			mutate: func(proof *InclusionProof) {
				proof.profileID = ProfileID(255)
			},
			want: ErrUnsupportedProfile,
		},
		"unsupported proof algorithm": {
			mutate: func(proof *InclusionProof) {
				proof.algorithm = HashAlgorithm(255)
			},
			want: ErrUnsupportedAlgorithm,
		},
		"reordered siblings": {
			mutate: func(proof *InclusionProof) {
				proof.siblings[0], proof.siblings[1] =
					proof.siblings[1], proof.siblings[0]
			},
			want: ErrVerificationFailed,
		},
		"replaced sibling": {
			mutate: func(proof *InclusionProof) {
				proof.siblings[0].value[0] ^= 0xff
			},
			want: ErrVerificationFailed,
		},
		"wrong root": {
			mutate: func(proof *InclusionProof) {
				proof.root.digest.value[0] ^= 0xff
			},
			want: ErrVerificationFailed,
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			proof := cloneInclusionProof(valid)
			test.mutate(&proof)
			verifyErr := VerifyInclusion(
				context.Background(),
				proof,
				leaves[1],
				DefaultProofLimits(),
			)
			if !errors.Is(verifyErr, test.want) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", verifyErr, test.want)
			}
		})
	}
}

func TestSnapshotNodeCountChecksIntegerConversion(t *testing.T) {
	t.Parallel()

	if count := snapshotNodeCount(0); count != 0 {
		t.Fatalf("empty node count = %d", count)
	}
	if count := snapshotNodeCount(3); count != 5 {
		t.Fatalf("three-leaf node count = %d", count)
	}
}

func TestAuditPathLengthCoversMaximumTreeDepth(t *testing.T) {
	t.Parallel()

	if length := auditPathLength(0, ^uint64(0)); length != 64 {
		t.Fatalf("maximum-tree audit path length = %d", length)
	}
}

func TestSnapshotAndProofHonorCancellationAtEveryCheckpoint(t *testing.T) {
	t.Parallel()

	leaves := []RawLeaf{
		NewRawLeaf([]byte("first")),
		NewRawLeaf([]byte("second")),
		NewRawLeaf([]byte("third")),
		NewRawLeaf([]byte("fourth")),
		NewRawLeaf([]byte("fifth")),
	}
	var snapshot Snapshot
	snapshotSucceeded := false
	for allowedCalls := 0; allowedCalls < 100; allowedCalls++ {
		ctx := &checkpointContext{
			remaining: allowedCalls,
			done:      make(chan struct{}),
		}
		result, err := NewSnapshot(
			ctx,
			CanonicalProfile(),
			leaves,
			DefaultSnapshotLimits(),
		)
		if err == nil {
			snapshot = result
			snapshotSucceeded = true

			break
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("snapshot checkpoint %d: error = %v", allowedCalls, err)
		}
	}
	if !snapshotSucceeded {
		t.Fatal("snapshot never passed all cancellation checkpoints")
	}

	var proof InclusionProof
	proofSucceeded := false
	for allowedCalls := 0; allowedCalls < 100; allowedCalls++ {
		ctx := &checkpointContext{
			remaining: allowedCalls,
			done:      make(chan struct{}),
		}
		result, err := snapshot.InclusionProof(
			ctx,
			1,
			DefaultProofLimits(),
		)
		if err == nil {
			proof = result
			proofSucceeded = true

			break
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("proof checkpoint %d: error = %v", allowedCalls, err)
		}
	}
	if !proofSucceeded {
		t.Fatal("proof generation never passed all cancellation checkpoints")
	}

	verificationSucceeded := false
	for allowedCalls := 0; allowedCalls < 100; allowedCalls++ {
		ctx := &checkpointContext{
			remaining: allowedCalls,
			done:      make(chan struct{}),
		}
		err := VerifyInclusion(
			ctx,
			proof,
			leaves[1],
			DefaultProofLimits(),
		)
		if err == nil {
			verificationSucceeded = true

			break
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("verify checkpoint %d: error = %v", allowedCalls, err)
		}
	}
	if !verificationSucceeded {
		t.Fatal("verification never passed all cancellation checkpoints")
	}
}

func TestSnapshotRejectsInvalidConstructionAndInternalIdentity(t *testing.T) {
	t.Parallel()

	var nilContext context.Context
	if _, err := NewSnapshot(
		nilContext,
		CanonicalProfile(),
		nil,
		DefaultSnapshotLimits(),
	); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("invalid construction error = %v", err)
	}

	snapshot, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		[]RawLeaf{NewRawLeaf([]byte("leaf"))},
		DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	tests := map[string]func(*Snapshot){
		"root identity": func(candidate *Snapshot) {
			candidate.root.algorithm = HashAlgorithm(255)
		},
		"root profile": func(candidate *Snapshot) {
			candidate.root.profileID = ProfileRFC9162
		},
		"root profile version": func(candidate *Snapshot) {
			candidate.root.profileVersion++
		},
		"root digest algorithm": func(candidate *Snapshot) {
			candidate.root.digest.algorithm = HashAlgorithm(255)
		},
		"root node index": func(candidate *Snapshot) {
			candidate.rootNode = uint64(len(candidate.nodes))
		},
		"root node size": func(candidate *Snapshot) {
			candidate.root.treeSize++
		},
	}
	for name, mutate := range tests {
		candidate := snapshot
		mutate(&candidate)
		if _, rootErr := candidate.Root(); !errors.Is(rootErr, ErrInvalidSnapshot) {
			t.Fatalf("%s error = %v", name, rootErr)
		}
	}

	empty, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		nil,
		DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("empty snapshot: %v", err)
	}
	empty.nodes = []snapshotNode{{}}
	if _, rootErr := empty.Root(); !errors.Is(rootErr, ErrInvalidSnapshot) {
		t.Fatalf("invalid empty snapshot error = %v", rootErr)
	}
}

func TestVerifyInclusionRejectsSingleLeafIndexAtTreeSize(t *testing.T) {
	t.Parallel()

	leaf := NewRawLeaf([]byte("leaf"))
	snapshot, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		[]RawLeaf{leaf},
		DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	proof, err := snapshot.InclusionProof(
		context.Background(),
		0,
		DefaultProofLimits(),
	)
	if err != nil {
		t.Fatalf("proof: %v", err)
	}
	proof.leafIndex = proof.treeSize
	if err := VerifyInclusion(
		context.Background(),
		proof,
		leaf,
		DefaultProofLimits(),
	); !errors.Is(err, ErrMalformedProof) {
		t.Fatalf("out-of-range proof error = %v", err)
	}
}

func TestVerifyInclusionRejectsResourceClaimsBeforeScanningElements(t *testing.T) {
	t.Parallel()

	proof := InclusionProof{
		profileID:      ProfileCanonicalBinary,
		profileVersion: 1,
		algorithm:      HashSHA256,
		root: newRoot(
			CanonicalProfile(),
			1,
			hashLeaf([]byte("leaf")),
		),
		treeSize:   1,
		leafDigest: newDigest(HashSHA256, hashLeaf([]byte("leaf"))),
		siblings: []Digest{
			newDigest(HashSHA256, hashLeaf([]byte("first"))),
			{algorithm: HashAlgorithm(255)},
		},
	}

	verifyErr := VerifyInclusion(
		context.Background(),
		proof,
		NewRawLeaf([]byte("leaf")),
		ProofLimits{
			MaxElements:       1,
			MaxTraversalDepth: 64,
			MaxLeafBytes:      64,
		},
	)
	var resourceError *ResourceError
	if !errors.As(verifyErr, &resourceError) {
		t.Fatalf("error = %v, want ResourceError", verifyErr)
	}
	if resourceError.Kind != ResourceProofElements {
		t.Fatalf("resource kind = %d", resourceError.Kind)
	}
}

func TestSnapshotInclusionProofHasLogarithmicAllocation(t *testing.T) {
	const leafCount = 16_384
	leaves := make([]RawLeaf, leafCount)
	for index := range leaves {
		leaves[index] = NewRawLeaf([]byte{byte(index), byte(index >> 8)})
	}
	snapshot, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		leaves,
		DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	allocations := testing.AllocsPerRun(20, func() {
		if _, proofErr := snapshot.InclusionProof(
			context.Background(),
			leafCount/2,
			DefaultProofLimits(),
		); proofErr != nil {
			panic(proofErr)
		}
	})
	const maxProofAllocations = 4
	if allocations > maxProofAllocations {
		t.Fatalf(
			"proof allocations = %.0f, want at most %d for %d leaves",
			allocations,
			maxProofAllocations,
			leafCount,
		)
	}
}

func cloneInclusionProof(proof InclusionProof) InclusionProof {
	proof.siblings = append([]Digest(nil), proof.siblings...)

	return proof
}

type checkpointContext struct {
	remaining int
	done      chan struct{}
}

func (ctx *checkpointContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (ctx *checkpointContext) Done() <-chan struct{} {
	return ctx.done
}

func (ctx *checkpointContext) Err() error {
	if ctx.remaining == 0 {
		select {
		case <-ctx.done:
		default:
			close(ctx.done)
		}

		return context.Canceled
	}

	ctx.remaining--

	return nil
}

func (ctx *checkpointContext) Value(any) any {
	return nil
}
