package authstate

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestFailFastAggregateVerifierQueryCapacityBoundsTopology(t *testing.T) {
	paths := []StemPath{
		PresentStemPath(Stem{1}, 3),
		MissingStemPath(Stem{2}, 2),
		DifferentStemPath(Stem{3}, 2, Stem{4}),
	}
	got, err := aggregateVerifierQueryCapacity(context.Background(), 2, paths, 100)
	if err != nil || got != 17 {
		t.Fatalf("topology query capacity = %d, want 17: %v", got, err)
	}
}

func TestFailFastProofMaterialRemainingBoundary(t *testing.T) {
	t.Parallel()

	remaining, err := remainingProofMaterialResource(
		ProofMaterialResourceNodeReads,
		2,
		1,
	)
	if err != nil || remaining != 1 {
		t.Fatalf("remaining resource = %d, error = %v", remaining, err)
	}
	_, err = remainingProofMaterialResource(
		ProofMaterialResourceNodeReads,
		2,
		2,
	)
	var resourceErr *ProofMaterialResourceError
	if !errors.As(err, &resourceErr) ||
		resourceErr.Resource != ProofMaterialResourceNodeReads ||
		resourceErr.Limit != 2 ||
		resourceErr.Actual != 3 {
		t.Fatalf("exhausted resource error = %v", err)
	}
}

func TestFailFastSnapshotLimitDisjunction(t *testing.T) {
	t.Parallel()

	limits := testLimits()
	limits.MaxEntries = maxSupportedCount + 1
	limits.MaxBatchUpdates = 1
	if err := limits.validate(); !errors.Is(err, errInvalidLimits) {
		t.Fatalf("validate() error = %v, want errInvalidLimits", err)
	}
}

func TestFailFastClaimMergeObservesCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	claims := []Claim{
		Absence(testKey(2, 2)),
		Absence(testKey(1, 1)),
	}
	if err := sortClaims(ctx, claims); !errors.Is(err, context.Canceled) ||
		!errors.Is(err, errClaimCancelled) {
		t.Fatalf("sortClaims() error = %v, want cancellation", err)
	}
}

func TestFailFastClaimMergeSortsTwoValues(t *testing.T) {
	t.Parallel()

	claims := []Claim{
		Absence(testKey(2, 2)),
		Absence(testKey(1, 1)),
	}
	if err := sortClaims(context.Background(), claims); err != nil {
		t.Fatalf("sortClaims() error = %v", err)
	}
	if claims[0].key != testKey(1, 1) || claims[1].key != testKey(2, 2) {
		t.Fatalf("sorted claims = %x, %x", claims[0].key, claims[1].key)
	}
}

func TestFailFastUpdateProofAccountsForEveryWorkingKey(t *testing.T) {
	t.Parallel()

	first := testKey(3, 1)
	second := testKey(4, 1)
	limits := testProofGenerationLimits()
	limits.Material.MaxTemporaryBytes = 4*proofMaterialKeyWorkingBytes - 1
	_, err := newTestProofEngine(t).ProveUpdates(
		context.Background(),
		newTestSnapshot(t, nil),
		[]Update{Set(first, testValue(1)), Set(second, testValue(2))},
		limits,
	)
	var resourceErr *ProofMaterialResourceError
	if !errors.As(err, &resourceErr) ||
		resourceErr.Resource != ProofMaterialResourceTemporaryBytes ||
		resourceErr.Limit != 4*proofMaterialKeyWorkingBytes-1 ||
		resourceErr.Actual != 4*proofMaterialKeyWorkingBytes {
		t.Fatalf("update proof temporary resource error = %v", err)
	}
}

func TestFailFastStatelessProofKeyAccountsForNextSlot(t *testing.T) {
	t.Parallel()

	first := testKey(4, 1)
	second := testKey(4, 2)
	limit := 4*proofMaterialKeyWorkingBytes - 1
	err := addStatelessProofKey(
		map[Key]struct{}{first: {}}, second, 2, limit,
	)
	var resourceErr *ProofMaterialResourceError
	if !errors.As(err, &resourceErr) ||
		resourceErr.Resource != ProofMaterialResourceTemporaryBytes ||
		resourceErr.Limit != limit ||
		resourceErr.Actual != 4*proofMaterialKeyWorkingBytes {
		t.Fatalf("proof-key temporary resource error = %v", err)
	}
}

func TestFailFastUpdateProofClassifiesWholeStemTransitions(t *testing.T) {
	t.Parallel()

	first := testKey(5, 1)
	second := testKey(5, 2)
	for _, test := range []struct {
		name             string
		entries          []Entry
		updates          []Update
		wantTopologyRead bool
	}{
		{
			name:    "set keeps stem present",
			entries: []Entry{{Key: first, Value: testValue(1)}},
			updates: []Update{Delete(first), Set(second, testValue(2))},
		},
		{
			name:             "later member deletion empties stem",
			entries:          []Entry{{Key: second, Value: testValue(2)}},
			updates:          []Update{Delete(first), Delete(second)},
			wantTopologyRead: true,
		},
		{
			name:    "absent deletions do not empty stem",
			updates: []Update{Delete(first), Delete(second)},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			limits := topologyProofGenerationLimits()
			limits.Material.MaxKeys = 2
			limits.ProverQueries.MaxKeys = 2
			proof, err := newTestProofEngine(t).ProveUpdates(
				context.Background(),
				newTestSnapshot(t, test.entries),
				test.updates,
				limits,
			)
			if test.wantTopologyRead {
				var resourceErr *ProofMaterialResourceError
				if !errors.As(err, &resourceErr) ||
					resourceErr.Resource != ProofMaterialResourceKeys ||
					resourceErr.Limit != 2 || resourceErr.Actual != 3 {
					t.Fatalf("topology key resource error = %v", err)
				}

				return
			}
			if err != nil {
				t.Fatalf("prove bounded update keys: %v", err)
			}
			if len(proof.claims.claims) != 2 ||
				proof.claims.claims[0].key != first ||
				proof.claims.claims[1].key != second {
				t.Fatal("update proof did not contain the exact two canonical keys")
			}
		})
	}
}

func TestFailFastUpdateProofContinuesAcrossStemGroups(t *testing.T) {
	t.Parallel()

	firstDeleted := testKey(6, 1)
	firstRetained := testKey(6, 2)
	secondDeleted := testKey(7, 1)
	for _, test := range []struct {
		name    string
		entries []Entry
		limit   uint32
	}{
		{
			name: "retained member",
			entries: []Entry{
				{Key: firstDeleted, Value: testValue(1)},
				{Key: firstRetained, Value: testValue(2)},
				{Key: secondDeleted, Value: testValue(3)},
			},
			limit: 3,
		},
		{
			name:    "absent deletion",
			entries: []Entry{{Key: secondDeleted, Value: testValue(3)}},
			limit:   2,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			limits := topologyProofGenerationLimits()
			limits.Material.MaxKeys = test.limit
			limits.ProverQueries.MaxKeys = test.limit
			_, err := newTestProofEngine(t).ProveUpdates(
				context.Background(),
				newTestSnapshot(t, test.entries),
				[]Update{Delete(firstDeleted), Delete(secondDeleted)},
				limits,
			)
			var resourceErr *ProofMaterialResourceError
			if !errors.As(err, &resourceErr) ||
				resourceErr.Resource != ProofMaterialResourceKeys ||
				resourceErr.Limit != uint64(test.limit) ||
				resourceErr.Actual != uint64(test.limit)+1 {
				t.Fatalf("later topology key resource error = %v", err)
			}
		})
	}
}

func TestFailFastStemDepthUsesNeighborAfterMultiSuffixStem(t *testing.T) {
	t.Parallel()

	previous := testKey(8, 1)
	previous[1] = 0x10
	target := testKey(8, 1)
	target[1] = 0x20
	target[2] = 0x20
	targetSuffix := target
	targetSuffix[31]++
	next := target
	next[2]++
	depth, found, err := statelessSnapshotStemDepth(
		context.Background(),
		[]Entry{
			{Key: previous, Value: testValue(1)},
			{Key: target, Value: testValue(2)},
			{Key: targetSuffix, Value: testValue(3)},
			{Key: next, Value: testValue(4)},
		},
		Stem(target[:31]),
	)
	if err != nil || !found || depth != 3 {
		t.Fatalf("multi-suffix stem depth = %d, found %v, error %v", depth, found, err)
	}
}

func TestFailFastTreeProofMergeCopiesSortedValues(t *testing.T) {
	t.Parallel()

	values := []int{2, 1}
	if err := sortTreeProofValues(
		context.Background(),
		values,
		func(left int, right int) int {
			return left - right
		},
	); err != nil {
		t.Fatalf("sortTreeProofValues() error = %v", err)
	}
	if !slices.Equal(values, []int{1, 2}) {
		t.Fatalf("sorted values = %v, want [1 2]", values)
	}
}
