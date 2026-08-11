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

func TestFailFastStatelessWitnessStemPathLookup(t *testing.T) {
	paths := []StemPath{
		PresentStemPath(Stem{1}, 1),
		MissingStemPath(Stem{2}, 1),
		DifferentStemPath(Stem{3}, 1, Stem{4}),
	}
	for index, stem := range []Stem{{1}, {2}, {3}} {
		got, found := statelessWitnessStemPath(paths, stem)
		if !found || got != paths[index] {
			t.Fatalf("stem %x lookup = %#v/%t, want %#v/true", stem, got, found, paths[index])
		}
	}
	for _, stem := range []Stem{{0}, {2, 1}, {4}} {
		if got, found := statelessWitnessStemPath(paths, stem); found || got != (StemPath{}) {
			t.Fatalf("missing stem %x lookup = %#v/%t", stem, got, found)
		}
	}
}

func TestFailFastCanonicalEmptyRootStemPath(t *testing.T) {
	stem := Stem{1}
	for name, test := range map[string]struct {
		path StemPath
		want bool
	}{
		"canonical":    {path: MissingStemPath(stem, 1), want: true},
		"present":      {path: PresentStemPath(stem, 1), want: false},
		"different":    {path: DifferentStemPath(stem, 1, Stem{2}), want: false},
		"deep missing": {path: MissingStemPath(stem, 2), want: false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := canonicalEmptyRootStemPath(test.path); got != test.want {
				t.Fatalf("canonical empty-root stem path = %t, want %t", got, test.want)
			}
		})
	}
}

func TestFailFastEmptyRootTreeProofShape(t *testing.T) {
	firstKey := testKey(1, 1)
	secondKey := testKey(2, 2)
	firstClaim := Absence(firstKey)
	secondClaim := Absence(secondKey)
	firstPath := MissingStemPath(stemFromKey(firstKey), 1)
	secondPath := MissingStemPath(stemFromKey(secondKey), 1)

	for name, test := range map[string]struct {
		claims      []Claim
		paths       []StemPath
		commitments []PathCommitment
		want        bool
	}{
		"canonical": {
			claims: []Claim{firstClaim, secondClaim},
			paths:  []StemPath{firstPath, secondPath},
			want:   true,
		},
		"empty claims": {paths: []StemPath{firstPath}},
		"empty paths":  {claims: []Claim{firstClaim}},
		"commitment": {
			claims: []Claim{firstClaim}, paths: []StemPath{firstPath},
			commitments: []PathCommitment{{}},
		},
		"present path": {
			claims: []Claim{firstClaim},
			paths:  []StemPath{PresentStemPath(stemFromKey(firstKey), 1)},
		},
		"path without claim": {
			claims: []Claim{firstClaim},
			paths:  []StemPath{firstPath, secondPath},
		},
		"mismatched stem": {
			claims: []Claim{firstClaim},
			paths:  []StemPath{secondPath},
		},
		"membership": {
			claims: []Claim{Membership(firstKey, testValue(1))},
			paths:  []StemPath{firstPath},
		},
		"claim without path": {
			claims: []Claim{firstClaim, secondClaim},
			paths:  []StemPath{firstPath},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := validEmptyRootTreeProofShape(
				test.claims, test.paths, test.commitments,
			); got != test.want {
				t.Fatalf("valid empty-root tree-proof shape = %t, want %t", got, test.want)
			}
		})
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

func TestFailFastStatelessScratchAccountsForEveryComponent(t *testing.T) {
	proof := TreeProof{
		commitments: make([]PathCommitment, 2),
		stemPaths: []StemPath{
			{kind: StemPathPresent},
			{kind: StemPathMissing},
			{kind: StemPathDifferent},
		},
	}
	const updateCount = uint64(4)
	want := updateCount*statelessUpdateWorkingBytes +
		2*statelessCommitmentPathWorkingBytes +
		3*statelessStemPathWorkingBytes +
		updateCount*uint64(maxProofPathLength)*statelessPropagationLevelWorkingBytes +
		uint64(maxProofPathLength*len(backend.Vector{})*len(backend.Vector{}[0]))
	if got := statelessTemporaryBytes(proof, updateCount, false); got != want {
		t.Fatalf("stateless scratch bytes = %d, want %d", got, want)
	}
}

func TestFailFastStatelessAbsentDeleteDoesNotStopLaterStemUpdate(t *testing.T) {
	later := testKey(0x40, 1)
	differentExisting := testKey(0x20, 1)
	differentExisting[1] = 1
	for _, test := range []struct {
		name     string
		entries  []Entry
		absent   Key
		wantKind StemPathKind
	}{
		{
			name:     "missing stem",
			entries:  []Entry{{Key: later, Value: testValue(1)}},
			absent:   testKey(0x20, 1),
			wantKind: StemPathMissing,
		},
		{
			name: "different stem",
			entries: []Entry{
				{Key: differentExisting, Value: testValue(2)},
				{Key: later, Value: testValue(1)},
			},
			absent:   testKey(0x20, 2),
			wantKind: StemPathDifferent,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := newTestSnapshot(t, test.entries)
			proof, updater := newStatelessTestProof(t, snapshot, []Key{test.absent, later})
			if proof.stemPaths[0].kind != test.wantKind {
				t.Fatalf("absent stem path kind = %d, want %d", proof.stemPaths[0].kind, test.wantKind)
			}
			updates := []Update{Delete(test.absent), Set(later, testValue(3))}
			got, err := updater.Apply(
				context.Background(), proof, updates,
				testProofVerificationLimits(), testStatelessUpdateLimits(),
			)
			if err != nil {
				t.Fatalf("apply absent delete before later update: %v", err)
			}
			wantSnapshot, _, err := snapshot.Apply(context.Background(), updates)
			if err != nil {
				t.Fatalf("apply stateful comparison updates: %v", err)
			}
			want, err := wantSnapshot.RootContainer(context.Background())
			if err != nil {
				t.Fatalf("stateful comparison root: %v", err)
			}
			assertSameBackendRoot(t, got, want)
		})
	}
}

func TestFailFastStatelessAbsentDeleteDoesNotStopSameStemInsertion(t *testing.T) {
	existing := testKey(0x40, 1)
	absent := testKey(0x20, 1)
	inserted := testKey(0x20, 2)
	snapshot := newTestSnapshot(t, []Entry{{Key: existing, Value: testValue(1)}})
	proof, updater := newStatelessTestProof(t, snapshot, []Key{absent, inserted})
	updates := []Update{Delete(absent), Set(inserted, testValue(2))}
	got, err := updater.Apply(
		context.Background(), proof, updates,
		testProofVerificationLimits(), testStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("apply absent delete before same-stem insertion: %v", err)
	}
	wantSnapshot, _, err := snapshot.Apply(context.Background(), updates)
	if err != nil {
		t.Fatalf("apply stateful comparison updates: %v", err)
	}
	want, err := wantSnapshot.RootContainer(context.Background())
	if err != nil {
		t.Fatalf("stateful comparison root: %v", err)
	}
	assertSameBackendRoot(t, got, want)
}

func TestFailFastStatelessNoOpDoesNotStopLaterStemUpdate(t *testing.T) {
	first := testKey(0x20, 1)
	later := testKey(0x40, 1)
	firstValue := testValue(1)
	snapshot := newTestSnapshot(t, []Entry{
		{Key: first, Value: firstValue},
		{Key: later, Value: testValue(2)},
	})
	proof, updater := newStatelessTestProof(t, snapshot, []Key{first, later})
	updates := []Update{Set(first, firstValue), Set(later, testValue(3))}
	got, err := updater.Apply(
		context.Background(), proof, updates,
		testProofVerificationLimits(), testStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("apply no-op before later update: %v", err)
	}
	wantSnapshot, _, err := snapshot.Apply(context.Background(), updates)
	if err != nil {
		t.Fatalf("apply stateful comparison updates: %v", err)
	}
	want, err := wantSnapshot.RootContainer(context.Background())
	if err != nil {
		t.Fatalf("stateful comparison root: %v", err)
	}
	assertSameBackendRoot(t, got, want)
}

func TestFailFastStatelessMissingInsertionClassifiesChildShape(t *testing.T) {
	existing := testKey(0x00, 0x00)
	neighbor := testKey(0x02, 0xff)
	inserted := testKey(0x01, 0x01)
	snapshot := newTestSnapshot(t, []Entry{
		{Key: existing, Value: testValue(1)},
		{Key: neighbor, Value: testValue(2)},
	})
	proof, updater := newStatelessTestProof(t, snapshot, []Key{inserted})
	paths, commitments := statelessTestMaterial(proof)
	stem := Stem(inserted[:31])
	path := paths[stem]
	if path.kind != StemPathMissing {
		t.Fatalf("inserted stem path kind = %d, want missing", path.kind)
	}
	changed, err := updater.updateStems(
		context.Background(),
		proof.claims,
		paths,
		commitments,
		[]Update{Set(inserted, testValue(3))},
		&statelessUpdateBudget{limits: testStatelessUpdateLimits()},
	)
	if err != nil {
		t.Fatalf("classify missing-stem insertion: %v", err)
	}
	stemPath := makeStatelessPath(stem[:path.depth])
	change, found := changed[stemPath]
	if !found || len(changed) != 1 || change.kind != statelessChangedStem {
		t.Fatalf("missing-stem change = (%#v, %t), changes = %d", change, found, len(changed))
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

func TestFailFastMergeExistingStemIntoEmptyInsertion(t *testing.T) {
	existing := statelessInsertedStem{stem: Stem{1}}
	merged, err := mergeStatelessExistingStem(
		context.Background(),
		existing,
		nil,
	)
	if err != nil {
		t.Fatalf("merge existing stem into empty insertion: %v", err)
	}
	if len(merged) != 1 || merged[0] != existing {
		t.Fatalf("merged stems = %#v, want only %#v", merged, existing)
	}
}

func TestFailFastParentChangedChildReturnsExactIndex(t *testing.T) {
	matching := statelessChangedCommitment{kind: statelessChangedStem}
	later := statelessChangedCommitment{kind: statelessChangedInternal}
	changes := []statelessParentChange{
		{opening: backend.VectorUpdate{Index: 7}, child: matching},
		{opening: backend.VectorUpdate{Index: 8}, child: later},
	}
	changed, found := statelessParentChangedChild(changes, 7)
	if !found || changed != matching {
		t.Fatalf("matching changed child = %#v, found %v", changed, found)
	}
	if _, found := statelessParentChangedChild(changes, 9); found {
		t.Fatal("absent changed child reported present")
	}
}

func TestFailFastDisclosedChildClassifiesEveryPathKind(t *testing.T) {
	parent := makeStatelessPath([]byte{1})
	child := byte(2)
	probe := statelessTopologyProbe(parent, child)
	stem := Stem(probe[:31])
	for _, test := range []struct {
		name string
		path StemPath
		want bool
	}{
		{name: "present", path: PresentStemPath(stem, 2), want: true},
		{name: "different", path: DifferentStemPath(stem, 2, Stem{3}), want: true},
		{name: "missing", path: MissingStemPath(stem, 2)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := statelessDisclosedChildIsStem(
				context.Background(),
				map[Stem]StemPath{stem: test.path},
				parent,
				child,
				&statelessUpdateBudget{limits: topologyStatelessUpdateLimits()},
			)
			if err != nil || got != test.want {
				t.Fatalf("disclosed child stem = %v, want %v: %v", got, test.want, err)
			}
		})
	}
}
