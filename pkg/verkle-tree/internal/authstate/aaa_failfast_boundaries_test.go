package authstate

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/leafvector"
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

func TestFailFastClaimAccessorsPreservePresenceAndAbsence(t *testing.T) {
	key := testKey(1, 2)
	value := testValue(3)
	membership := Membership(key, value)

	kind, err := membership.Kind()
	if err != nil || kind != ClaimMembership {
		t.Fatalf("membership kind = %d, error = %v", kind, err)
	}
	gotKey, err := membership.Key()
	if err != nil || gotKey != key {
		t.Fatalf("membership key = %x, error = %v", gotKey, err)
	}
	gotValue, present, err := membership.Value()
	if err != nil || !present || gotValue != value {
		t.Fatalf("membership value = %x/%t, error = %v", gotValue, present, err)
	}

	absence := Absence(key)
	gotValue, present, err = absence.Value()
	if err != nil || present || gotValue != (Value{}) {
		t.Fatalf("absence value = %x/%t, error = %v", gotValue, present, err)
	}
	if _, err = (Claim{}).Kind(); !errors.Is(err, errInvalidClaim) {
		t.Fatalf("zero claim kind error = %v, want errInvalidClaim", err)
	}
}

func TestFailFastClaimLimitsRejectEachInvalidField(t *testing.T) {
	valid := testClaimLimits()
	if err := valid.validate(); err != nil {
		t.Fatalf("valid claim limits: %v", err)
	}

	for _, limits := range []ClaimLimits{
		{MaxTemporaryBytes: valid.MaxTemporaryBytes},
		{MaxClaims: maxClaimCount + 1, MaxTemporaryBytes: valid.MaxTemporaryBytes},
		{MaxClaims: valid.MaxClaims},
	} {
		if err := limits.validate(); !errors.Is(err, errInvalidClaimLimits) {
			t.Fatalf("invalid claim limits %#v error = %v", limits, err)
		}
	}
}

func TestFailFastAggregateVerifierQueriesEmptyRoot(t *testing.T) {
	key := testKey(1, 3)
	sameStem := key
	sameStem[31] = 4
	otherStem := testKey(2, 5)
	material, err := newTestSnapshot(t, nil).ProofMaterial(
		context.Background(),
		[]Key{otherStem, key, sameStem},
		testProofMaterialLimits(),
	)
	if err != nil {
		t.Fatalf("empty-root proof material: %v", err)
	}
	assertQueries := func(limits AggregateVerifierQueryLimits) {
		t.Helper()

		queries, queryErr := material.AggregateVerifierQueries(
			context.Background(),
			limits,
		)
		if queryErr != nil {
			t.Fatalf("empty-root verifier queries: %v", queryErr)
		}
		if len(queries) != 2 || queries[0].Length != 0 ||
			queries[0].Opening.Index != key[0] || queries[1].Length != 0 ||
			queries[1].Opening.Index != otherStem[0] {
			t.Fatalf("empty-root verifier queries = %#v", queries)
		}
	}
	limits := testAggregateVerifierQueryLimits()
	assertQueries(limits)
	limits.MaxQueries = maxAggregateVerifierQueries
	assertQueries(limits)
}

func TestFailFastAggregateVerifierCollectorStemQueries(t *testing.T) {
	key := testKey(1, 3)
	key[1] = 2
	stem := Stem(key[:31])
	rootPath := aggregateVerifierPath{}
	prefixPath := aggregateVerifierPath{path: [32]byte{1}, length: 1}
	stemPath := aggregateVerifierPath{path: [32]byte{1, 2}, length: 2}
	c1Path := aggregateVerifierPath{
		path:   [32]byte{1, 2, leafvector.C1HashIndex},
		length: 3,
	}
	c2Path := aggregateVerifierPath{
		path:   [32]byte{1, 2, leafvector.C2HashIndex},
		length: 3,
	}
	empty := backend.EmptyVectorCommitment()
	newCollector := func(paths ...aggregateVerifierPath) *aggregateVerifierCollector {
		commitments := make(map[aggregateVerifierPath]backend.VectorCommitment, len(paths))
		for _, path := range paths {
			commitments[path] = empty
		}

		return &aggregateVerifierCollector{
			ctx: context.Background(),
			limits: AggregateVerifierQueryLimits{
				MaxQueries:        16,
				MaxTemporaryBytes: 16 * aggregateVerifierQueryWorkingByte,
			},
			commitments:   commitments,
			queryCapacity: 16,
			queryByID:     make(map[aggregateVerifierIdentity]int),
		}
	}

	value := testValue(7)
	secondKey := key
	secondKey[31] = 4
	secondValue := testValue(8)
	thirdKey := key
	thirdKey[31] = 128
	thirdValue := testValue(9)
	present := newCollector(rootPath, prefixPath, stemPath, c1Path, c2Path)
	if err := present.collectStem(
		PresentStemPath(stem, 2),
		[]Claim{
			Membership(key, value),
			Membership(secondKey, secondValue),
			Membership(thirdKey, thirdValue),
		},
	); err != nil {
		t.Fatalf("collect present stem: %v", err)
	}
	if len(present.queries) != 12 {
		t.Fatalf("present stem query count = %d, want 12", len(present.queries))
	}
	opening := leafvector.EncodePresent(key[31], [32]byte(value))
	secondOpening := leafvector.EncodePresent(secondKey[31], [32]byte(secondValue))
	thirdOpening := leafvector.EncodePresent(thirdKey[31], [32]byte(thirdValue))
	wantPresent := []struct {
		path  aggregateVerifierPath
		index uint8
		value [32]byte
	}{
		{path: rootPath, index: 1},
		{path: prefixPath, index: 2},
		{path: stemPath, index: leafvector.ExtensionMarkerIndex, value: extensionMarkerScalar()},
		{path: stemPath, index: leafvector.StemIndex, value: [32]byte(leafvector.EncodeStem(stem))},
		{path: stemPath, index: leafvector.C1HashIndex},
		{path: c1Path, index: opening.LowIndex, value: [32]byte(opening.Low)},
		{path: c1Path, index: opening.HighIndex, value: [32]byte(opening.High)},
		{path: c1Path, index: secondOpening.LowIndex, value: [32]byte(secondOpening.Low)},
		{path: c1Path, index: secondOpening.HighIndex, value: [32]byte(secondOpening.High)},
		{path: stemPath, index: leafvector.C2HashIndex},
		{path: c2Path, index: thirdOpening.LowIndex, value: [32]byte(thirdOpening.Low)},
		{path: c2Path, index: thirdOpening.HighIndex, value: [32]byte(thirdOpening.High)},
	}
	for index, want := range wantPresent {
		got := present.queries[index]
		if got.Path != want.path.path || got.Length != want.path.length ||
			got.Opening.Index != want.index || got.Opening.Value != want.value {
			t.Fatalf("present query %d = %#v, want %#v", index, got, want)
		}
	}

	existing := stem
	existing[1] = 9
	different := newCollector(rootPath, prefixPath, stemPath)
	if err := different.collectStem(
		DifferentStemPath(stem, 2, existing),
		nil,
	); err != nil {
		t.Fatalf("collect different stem: %v", err)
	}
	if len(different.queries) != 4 ||
		different.queries[3].Opening.Index != leafvector.StemIndex ||
		different.queries[3].Opening.Value != [32]byte(leafvector.EncodeStem(existing)) {
		t.Fatalf("different stem queries = %#v", different.queries)
	}
}

func TestFailFastProofMaterialRemainingBoundary(t *testing.T) {
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
	limits := testLimits()
	limits.MaxEntries = maxSupportedCount + 1
	limits.MaxBatchUpdates = 1
	if err := limits.validate(); !errors.Is(err, errInvalidLimits) {
		t.Fatalf("validate() error = %v, want errInvalidLimits", err)
	}
}

func TestFailFastCopyEntriesAcceptsExactMaximumCount(t *testing.T) {
	entries, err := newTestSnapshot(t, nil).CopyEntries(
		context.Background(),
		maxSupportedCount,
		1,
	)
	if err != nil || len(entries) != 0 {
		t.Fatalf("CopyEntries() = %v, %v, want empty entries", entries, err)
	}
}

func TestFailFastClaimMergeObservesCancellation(t *testing.T) {
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

func TestFailFastStatelessDeleteScratchAccountsForWholeVector(t *testing.T) {
	proof := TreeProof{stemPaths: []StemPath{{kind: StemPathPresent}}}
	presentBytes := statelessTemporaryBytes(proof, 1, false)
	deletionBytes := statelessTemporaryBytes(proof, 1, true)
	vectorBytes := uint64(len(backend.Vector{}) * len(backend.Vector{}[0]))
	if deletionBytes != presentBytes+vectorBytes {
		t.Fatalf(
			"deletion scratch = %d, want present %d plus vector %d",
			deletionBytes,
			presentBytes,
			vectorBytes,
		)
	}
}

func TestFailFastUpdateProofClassifiesWholeStemTransitions(t *testing.T) {
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
