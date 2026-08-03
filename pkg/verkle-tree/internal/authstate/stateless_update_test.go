package authstate

import (
	"context"
	"encoding/hex"
	"errors"
	"sync"
	"testing"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/leafvector"
)

func TestStatelessUpdaterDerivesPinnedPostStateRoot(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{Key: testKey(0x00, 0x00), Value: testValue(0x11)},
		{Key: testKey(0x00, 0x01), Value: testValue(0x22)},
		{Key: testKey(0x01, 0xff), Value: testValue(0x33)},
		{Key: testKey(0x01, 0x7f), Value: testValue(0x44)},
	}
	snapshot := newTestSnapshot(t, entries)
	proofEngine := newTestProofEngine(t)
	proof, err := proofEngine.Prove(
		context.Background(),
		snapshot,
		[]Key{
			testKey(0x00, 0x00),
			testKey(0x00, 0x02),
			testKey(0x01, 0xff),
			testKey(0x02, 0x00),
		},
		testProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("generate witness proof: %v", err)
	}
	updater, err := NewStatelessUpdater(
		context.Background(),
		testAuthstateAggregateOpeningLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("new stateless updater: %v", err)
	}
	updates := []Update{
		Set(testKey(0x00, 0x02), testValue(0x66)),
		Set(testKey(0x00, 0x00), testValue(0x55)),
	}

	got, err := updater.Apply(
		context.Background(),
		proof,
		updates,
		testProofVerificationLimits(),
		testStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("apply stateless updates: %v", err)
	}
	wantSnapshot, _, err := snapshot.Apply(context.Background(), updates)
	if err != nil {
		t.Fatalf("apply stateful oracle updates: %v", err)
	}
	want, err := wantSnapshot.RootContainer(context.Background())
	if err != nil {
		t.Fatalf("stateful oracle root: %v", err)
	}
	assertSameBackendRoot(t, got, want)
	assertStatelessRootCommitment(
		t,
		got,
		"60a128ee3c2aafe2c12ea104e4b07338677445012dc20c2dd3495a216439e077",
	)

	reordered, err := updater.Apply(
		context.Background(),
		proof,
		[]Update{updates[1], updates[0]},
		testProofVerificationLimits(),
		testStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("apply reordered stateless updates: %v", err)
	}
	assertSameBackendRoot(t, reordered, want)
}

func TestStatelessUpdaterInsertsOneAuthenticatedMissingStem(t *testing.T) {
	t.Parallel()

	existing := testKey(0x00, 0x00)
	neighbor := testKey(0x02, 0xff)
	insertedLow := testKey(0x01, 0x01)
	insertedHigh := testKey(0x01, 0x80)
	snapshot := newTestSnapshot(t, []Entry{
		{Key: existing, Value: testValue(0x11)},
		{Key: neighbor, Value: testValue(0x33)},
	})
	proof, updater := newStatelessTestProof(
		t, snapshot, []Key{insertedLow, insertedHigh},
	)
	updates := []Update{
		Set(insertedHigh, testValue(0x22)),
		Set(insertedLow, Value{}),
	}

	got, err := updater.Apply(
		context.Background(), proof, updates,
		testProofVerificationLimits(), testStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("insert authenticated missing stem: %v", err)
	}
	wantSnapshot, _, err := snapshot.Apply(context.Background(), updates)
	if err != nil {
		t.Fatalf("apply stateful missing-stem insertion: %v", err)
	}
	want, err := wantSnapshot.RootContainer(context.Background())
	if err != nil {
		t.Fatalf("stateful missing-stem root: %v", err)
	}
	assertSameBackendRoot(t, got, want)
}

func TestStatelessUpdaterBuildsCanonicalSubtreeForMissingSiblingStems(t *testing.T) {
	t.Parallel()

	left := testKey(0x00, 0x00)
	right := testKey(0x02, 0xff)
	first := testKey(0x01, 0x01)
	first[1] = 0x10
	second := testKey(0x01, 0x80)
	second[1] = 0x20
	snapshot := newTestSnapshot(t, []Entry{
		{Key: left, Value: testValue(0x11)},
		{Key: right, Value: testValue(0x22)},
	})
	proof, updater := newStatelessTestProof(t, snapshot, []Key{first, second})
	updates := []Update{
		Set(second, testValue(0x44)),
		Set(first, testValue(0x33)),
	}

	got, err := updater.Apply(
		context.Background(), proof, updates,
		testProofVerificationLimits(), testStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("insert sibling stems below missing edge: %v", err)
	}
	wantSnapshot, _, err := snapshot.Apply(context.Background(), updates)
	if err != nil {
		t.Fatalf("apply stateful sibling-stem insertion: %v", err)
	}
	want, err := wantSnapshot.RootContainer(context.Background())
	if err != nil {
		t.Fatalf("stateful sibling-stem root: %v", err)
	}
	assertSameBackendRoot(t, got, want)
}

func TestStatelessUpdaterSplitsAuthenticatedDifferentStem(t *testing.T) {
	t.Parallel()

	existing := testKey(0x10, 0x00)
	existing[1] = 0x20
	neighbor := testKey(0x30, 0xff)
	insertedBefore := testKey(0x10, 0x01)
	insertedBefore[1] = 0x10
	insertedAfter := testKey(0x10, 0x80)
	insertedAfter[1] = 0x30
	snapshot := newTestSnapshot(t, []Entry{
		{Key: existing, Value: testValue(0x11)},
		{Key: neighbor, Value: testValue(0x22)},
	})
	proof, updater := newStatelessTestProof(
		t, snapshot, []Key{insertedAfter, insertedBefore},
	)
	updates := []Update{
		Set(insertedAfter, testValue(0x33)),
		Set(insertedBefore, Value{}),
	}

	got, err := updater.Apply(
		context.Background(), proof, updates,
		testProofVerificationLimits(), testStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("split authenticated different stem: %v", err)
	}
	wantSnapshot, _, err := snapshot.Apply(context.Background(), updates)
	if err != nil {
		t.Fatalf("apply stateful different-stem insertion: %v", err)
	}
	want, err := wantSnapshot.RootContainer(context.Background())
	if err != nil {
		t.Fatalf("stateful different-stem root: %v", err)
	}
	assertSameBackendRoot(t, got, want)
}

func TestStatelessUpdaterBuildsDeepestDifferentStemChain(t *testing.T) {
	t.Parallel()

	var existing Key
	for index := 0; index < 30; index++ {
		existing[index] = 0x40
	}
	existing[30] = 0x10
	inserted := existing
	inserted[30] = 0x20
	inserted[31] = 0xff
	neighbor := testKey(0x80, 0x00)
	snapshot := newTestSnapshot(t, []Entry{
		{Key: existing, Value: testValue(0x11)},
		{Key: neighbor, Value: testValue(0x22)},
	})
	proof, updater := newStatelessTestProof(t, snapshot, []Key{inserted})
	updates := []Update{Set(inserted, testValue(0x33))}

	got, err := updater.Apply(
		context.Background(), proof, updates,
		testProofVerificationLimits(), testStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("insert deepest different stem: %v", err)
	}
	wantSnapshot, _, err := snapshot.Apply(context.Background(), updates)
	if err != nil {
		t.Fatalf("apply stateful deepest insertion: %v", err)
	}
	want, err := wantSnapshot.RootContainer(context.Background())
	if err != nil {
		t.Fatalf("stateful deepest insertion root: %v", err)
	}
	assertSameBackendRoot(t, got, want)
}

func TestStatelessUpdaterHandlesDeepSharedPathsAndBothSuffixHalves(t *testing.T) {
	t.Parallel()

	first := testKey(0x40, 0x01)
	first[1] = 0x10
	second := testKey(0x40, 0x81)
	second[1] = 0x20
	third := testKey(0x41, 0xff)
	entries := []Entry{
		{Key: first, Value: testValue(0x11)},
		{Key: second, Value: testValue(0x22)},
		{Key: third, Value: testValue(0x33)},
	}
	snapshot := newTestSnapshot(t, entries)
	proof, updater := newStatelessTestProof(t, snapshot, []Key{first, second, third})
	updates := []Update{
		Set(third, testValue(0x66)),
		Set(first, testValue(0x44)),
		Set(second, testValue(0x55)),
	}
	for index := range updates {
		single := []Update{updates[index]}
		gotSingle, singleErr := updater.Apply(
			context.Background(), proof, single,
			testProofVerificationLimits(), testStatelessUpdateLimits(),
		)
		if singleErr != nil {
			t.Fatalf("apply deep stateless update %d: %v", index, singleErr)
		}
		wantSingleSnapshot, _, singleErr := snapshot.Apply(context.Background(), single)
		if singleErr != nil {
			t.Fatalf("apply deep stateful update %d: %v", index, singleErr)
		}
		wantSingle, singleErr := wantSingleSnapshot.RootContainer(context.Background())
		if singleErr != nil {
			t.Fatalf("deep stateful root %d: %v", index, singleErr)
		}
		assertSameBackendRoot(t, gotSingle, wantSingle)
	}

	got, err := updater.Apply(
		context.Background(), proof, updates,
		testProofVerificationLimits(), testStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("apply deep stateless updates: %v", err)
	}
	wantSnapshot, _, err := snapshot.Apply(context.Background(), updates)
	if err != nil {
		t.Fatalf("apply deep stateful updates: %v", err)
	}
	want, err := wantSnapshot.RootContainer(context.Background())
	if err != nil {
		t.Fatalf("deep stateful root: %v", err)
	}
	assertSameBackendRoot(t, got, want)
}

func TestStatelessUpdaterSupportsPresentZeroAndNoOpValues(t *testing.T) {
	t.Parallel()

	present := testKey(0, 0)
	absent := testKey(0, 1)
	snapshot := newTestSnapshot(t, []Entry{{Key: present, Value: testValue(1)}})
	proof, updater := newStatelessTestProof(t, snapshot, []Key{present, absent})
	updates := []Update{Set(present, testValue(1)), Set(absent, Value{})}

	got, err := updater.Apply(
		context.Background(), proof, updates,
		testProofVerificationLimits(), testStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("apply zero and no-op values: %v", err)
	}
	wantSnapshot, _, err := snapshot.Apply(context.Background(), updates)
	if err != nil {
		t.Fatalf("apply stateful zero and no-op values: %v", err)
	}
	want, err := wantSnapshot.RootContainer(context.Background())
	if err != nil {
		t.Fatalf("stateful zero-value root: %v", err)
	}
	assertSameBackendRoot(t, got, want)
}

func TestStatelessUpdaterRejectsInvalidAndIncompleteRequests(t *testing.T) {
	t.Parallel()

	var nilContext context.Context
	if _, err := NewStatelessUpdater(
		nilContext, testAuthstateAggregateOpeningLimits(), testCommitmentLimits(),
	); !errors.Is(err, errInvalidTreeProofContext) {
		t.Fatalf("nil constructor context error = %v", err)
	}
	if _, err := NewStatelessUpdater(
		context.Background(), backend.AggregateOpeningLimits{}, testCommitmentLimits(),
	); err == nil {
		t.Fatal("invalid opening limits were accepted")
	}
	if _, err := NewStatelessUpdater(
		context.Background(), testAuthstateAggregateOpeningLimits(), backend.CommitmentLimits{},
	); err == nil {
		t.Fatal("invalid commitment limits were accepted")
	}

	present := testKey(0, 0)
	absentSuffix := testKey(0, 1)
	absentStem := testKey(1, 0)
	snapshot := newTestSnapshot(t, []Entry{{Key: present, Value: testValue(1)}})
	proof, updater := newStatelessTestProof(t, snapshot, []Key{present, absentSuffix, absentStem})
	apply := func(target *StatelessUpdater, ctx context.Context, candidate TreeProof, updates []Update, limits StatelessUpdateLimits) error {
		_, err := target.Apply(ctx, candidate, updates, testProofVerificationLimits(), limits)
		return err
	}
	var nilUpdater *StatelessUpdater
	if err := apply(nilUpdater, context.Background(), proof, []Update{Set(present, testValue(2))}, testStatelessUpdateLimits()); !errors.Is(err, errInvalidStatelessUpdater) {
		t.Fatalf("nil updater error = %v", err)
	}
	if err := apply(&StatelessUpdater{}, context.Background(), proof, []Update{Set(present, testValue(2))}, testStatelessUpdateLimits()); !errors.Is(err, errInvalidStatelessUpdater) {
		t.Fatalf("zero updater error = %v", err)
	}
	if err := apply(updater, nilContext, proof, []Update{Set(present, testValue(2))}, testStatelessUpdateLimits()); !errors.Is(err, errInvalidTreeProofContext) {
		t.Fatalf("nil apply context error = %v", err)
	}
	if err := apply(updater, context.Background(), TreeProof{}, []Update{Set(present, testValue(2))}, testStatelessUpdateLimits()); !errors.Is(err, errInvalidTreeProof) {
		t.Fatalf("invalid proof error = %v", err)
	}
	oversized := proof
	oversized.commitments = make([]PathCommitment, int(maxTreeProofPathCommitments)+1)
	if err := apply(updater, context.Background(), oversized, []Update{Set(present, testValue(2))}, testStatelessUpdateLimits()); !errors.Is(err, errInvalidTreeProof) {
		t.Fatalf("oversized commitment set error = %v", err)
	}
	oversized = proof
	oversized.stemPaths = make([]StemPath, int(maxTreeProofStemPaths)+1)
	if err := apply(updater, context.Background(), oversized, []Update{Set(present, testValue(2))}, testStatelessUpdateLimits()); !errors.Is(err, errInvalidTreeProof) {
		t.Fatalf("oversized stem-path set error = %v", err)
	}
	if err := apply(updater, context.Background(), proof, nil, testStatelessUpdateLimits()); !errors.Is(err, errInvalidStatelessUpdate) {
		t.Fatalf("empty update error = %v", err)
	}
	if err := apply(updater, context.Background(), proof, []Update{{}}, testStatelessUpdateLimits()); !errors.Is(err, errInvalidStatelessUpdate) {
		t.Fatalf("invalid update error = %v", err)
	}
	if err := apply(updater, context.Background(), proof, []Update{Delete(present)}, testStatelessUpdateLimits()); !errors.Is(err, errUnsupportedStatelessUpdate) {
		t.Fatalf("delete error = %v", err)
	}
	if err := apply(updater, context.Background(), proof, []Update{Set(present, testValue(2)), Set(present, testValue(3))}, testStatelessUpdateLimits()); !errors.Is(err, errDuplicateKey) {
		t.Fatalf("duplicate update error = %v", err)
	}
	if err := apply(updater, context.Background(), proof, []Update{Set(testKey(0, 2), testValue(2))}, testStatelessUpdateLimits()); !errors.Is(err, errIncompleteStatelessWitness) {
		t.Fatalf("missing claim error = %v", err)
	}
	tampered := proof
	tampered.claims.claims = append([]Claim(nil), proof.claims.claims...)
	tampered.claims.claims[0].value[0]++
	if err := apply(updater, context.Background(), tampered, []Update{Set(present, testValue(2))}, testStatelessUpdateLimits()); !IsProofVerificationError(err) {
		t.Fatalf("tampered witness error = %v", err)
	}

	invalidLimits := map[string]func(*StatelessUpdateLimits){
		"updates zero":         func(value *StatelessUpdateLimits) { value.MaxUpdates = 0 },
		"updates maximum":      func(value *StatelessUpdateLimits) { value.MaxUpdates = maxStatelessUpdates + 1 },
		"commitments zero":     func(value *StatelessUpdateLimits) { value.MaxCommitmentUpdates = 0 },
		"field mappings zero":  func(value *StatelessUpdateLimits) { value.MaxFieldMappings = 0 },
		"path lookups zero":    func(value *StatelessUpdateLimits) { value.MaxPathLookups = 0 },
		"temporary bytes zero": func(value *StatelessUpdateLimits) { value.MaxTemporaryBytes = 0 },
	}
	for name, invalidate := range invalidLimits {
		t.Run(name, func(t *testing.T) {
			limits := testStatelessUpdateLimits()
			invalidate(&limits)
			if err := apply(updater, context.Background(), proof, []Update{Set(present, testValue(2))}, limits); !errors.Is(err, errInvalidStatelessUpdateLimits) {
				t.Fatalf("invalid limits error = %v", err)
			}
		})
	}
}

func TestStatelessUpdaterEnforcesEveryResource(t *testing.T) {
	t.Parallel()

	key := testKey(0, 0)
	snapshot := newTestSnapshot(t, []Entry{{Key: key, Value: testValue(1)}})
	proof, updater := newStatelessTestProof(t, snapshot, []Key{key})
	update := []Update{Set(key, testValue(2))}
	tests := map[string]struct {
		mutate   func(*StatelessUpdateLimits)
		resource StatelessUpdateResource
		limit    uint64
		actual   uint64
	}{
		"updates": {
			mutate:   func(value *StatelessUpdateLimits) { value.MaxUpdates = 1 },
			resource: StatelessUpdateResourceUpdates, limit: 1, actual: 2,
		},
		"temporary bytes": {
			mutate: func(value *StatelessUpdateLimits) {
				value.MaxTemporaryBytes = statelessTemporaryBytes(proof, 1) - 1
			},
			resource: StatelessUpdateResourceTemporaryBytes,
			limit:    statelessTemporaryBytes(proof, 1) - 1,
			actual:   statelessTemporaryBytes(proof, 1),
		},
		"path lookups": {
			mutate:   func(value *StatelessUpdateLimits) { value.MaxPathLookups = 1 },
			resource: StatelessUpdateResourcePathLookups, limit: 1, actual: 2,
		},
		"commitment updates": {
			mutate:   func(value *StatelessUpdateLimits) { value.MaxCommitmentUpdates = 1 },
			resource: StatelessUpdateResourceCommitmentUpdates, limit: 1, actual: 2,
		},
		"field mappings": {
			mutate:   func(value *StatelessUpdateLimits) { value.MaxFieldMappings = 1 },
			resource: StatelessUpdateResourceFieldMappings, limit: 1, actual: 2,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			limits := testStatelessUpdateLimits()
			test.mutate(&limits)
			candidate := update
			if name == "updates" {
				candidate = append(candidate, Set(testKey(0, 1), testValue(3)))
			}
			_, err := updater.Apply(context.Background(), proof, candidate, testProofVerificationLimits(), limits)
			assertStatelessResourceError(t, err, test.resource, test.limit, test.actual)
		})
	}
}

func TestStatelessUpdaterAccountsForProofAndPropagationScratch(t *testing.T) {
	t.Parallel()

	key := testKey(0, 0)
	snapshot := newTestSnapshot(t, []Entry{{Key: key, Value: testValue(1)}})
	proof, updater := newStatelessTestProof(t, snapshot, []Key{key})
	limits := testStatelessUpdateLimits()
	limits.MaxTemporaryBytes = statelessUpdateWorkingBytes

	_, err := updater.Apply(
		context.Background(), proof, []Update{Set(key, testValue(2))},
		testProofVerificationLimits(), limits,
	)
	var resourceErr *StatelessUpdateResourceError
	if !errors.As(err, &resourceErr) ||
		resourceErr.Resource != StatelessUpdateResourceTemporaryBytes ||
		resourceErr.Limit != statelessUpdateWorkingBytes ||
		resourceErr.Actual != statelessTemporaryBytes(proof, 1) {
		t.Fatalf("proof scratch error = %v", err)
	}
}

func TestStatelessUpdateProofCountAndScratchBoundaries(t *testing.T) {
	t.Parallel()
	limits := testStatelessUpdateLimits()
	limits.MaxUpdates = maxStatelessUpdates
	if err := limits.validate(); err != nil {
		t.Fatalf("exact maximum update limit: %v", err)
	}

	proof := TreeProof{
		commitments: make([]PathCommitment, int(maxTreeProofPathCommitments)),
		stemPaths:   make([]StemPath, int(maxTreeProofStemPaths)),
	}
	if !statelessProofCountsWithinLimits(proof) {
		t.Fatal("exact proof component maxima were rejected")
	}
	proof.commitments = append(proof.commitments, PathCommitment{})
	if statelessProofCountsWithinLimits(proof) {
		t.Fatal("excessive proof commitment count was accepted")
	}
	proof.commitments = proof.commitments[:maxTreeProofPathCommitments]
	proof.stemPaths = append(proof.stemPaths, StemPath{})
	if statelessProofCountsWithinLimits(proof) {
		t.Fatal("excessive proof stem-path count was accepted")
	}

	proof.commitments = make([]PathCommitment, 2)
	proof.stemPaths = make([]StemPath, 3)
	const updateCount = uint64(4)
	want := updateCount*statelessUpdateWorkingBytes +
		2*statelessCommitmentPathWorkingBytes +
		3*statelessStemPathWorkingBytes +
		updateCount*uint64(maxProofPathLength)*statelessPropagationLevelWorkingBytes
	if got := statelessTemporaryBytes(proof, updateCount); got != want {
		t.Fatalf("stateless scratch bytes = %d, want %d", got, want)
	}

	present := TreeProof{stemPaths: []StemPath{{kind: StemPathPresent}}}
	missing := TreeProof{stemPaths: []StemPath{{kind: StemPathMissing}}}
	presentBytes := statelessTemporaryBytes(present, 1)
	missingBytes := statelessTemporaryBytes(missing, 1)
	const insertionVectors = uint64(maxProofPathLength * len(backend.Vector{}) * len(backend.Vector{}[0]))
	if missingBytes != presentBytes+insertionVectors {
		t.Fatalf(
			"topology insertion scratch = %d, want present %d plus vectors %d",
			missingBytes,
			presentBytes,
			insertionVectors,
		)
	}
}

func TestStatelessUpdaterHonorsCancellationAndConcurrentUse(t *testing.T) {
	t.Parallel()

	key := testKey(0, 0)
	snapshot := newTestSnapshot(t, []Entry{{Key: key, Value: testValue(1)}})
	proof, updater := newStatelessTestProof(t, snapshot, []Key{key})
	updates := []Update{Set(key, testValue(2))}
	observed := false
	completed := false
	for successful := 0; successful < 2_000; successful++ {
		_, err := updater.Apply(
			&stepContext{successfulChecks: successful}, proof, updates,
			testProofVerificationLimits(), testStatelessUpdateLimits(),
		)
		if err == nil {
			completed = true
			break
		}
		observed = true
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation after %d checks = %v", successful, err)
		}
	}
	if !observed {
		t.Fatal("no cancellation boundary was exercised")
	}
	if !completed {
		t.Fatal("cancellation sweep did not reach a successful operation")
	}

	want, err := updater.Apply(
		context.Background(), proof, updates,
		testProofVerificationLimits(), testStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("reference stateless update: %v", err)
	}
	const workers = 8
	var group sync.WaitGroup
	errorsByWorker := make(chan error, workers)
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			got, applyErr := updater.Apply(
				context.Background(), proof, updates,
				testProofVerificationLimits(), testStatelessUpdateLimits(),
			)
			if applyErr != nil {
				errorsByWorker <- applyErr
				return
			}
			gotBytes, gotErr := got.Bytes()
			wantBytes, wantErr := want.Bytes()
			if gotErr != nil || wantErr != nil || gotBytes != wantBytes {
				errorsByWorker <- errors.New("concurrent stateless root differs")
			}
		}()
	}
	group.Wait()
	close(errorsByWorker)
	for workerErr := range errorsByWorker {
		t.Error(workerErr)
	}
}

func TestStatelessUpdaterHonorsTopologyInsertionCancellation(t *testing.T) {
	t.Parallel()

	missingExisting := testKey(0x00, 0x00)
	missingNeighbor := testKey(0x02, 0xff)
	missingInserted := testKey(0x01, 0x01)
	missingSnapshot := newTestSnapshot(t, []Entry{
		{Key: missingExisting, Value: testValue(0x11)},
		{Key: missingNeighbor, Value: testValue(0x22)},
	})
	missingProof, missingUpdater := newStatelessTestProof(
		t, missingSnapshot, []Key{missingInserted},
	)
	assertStatelessTopologyCancellationSweep(
		t,
		missingUpdater,
		missingProof,
		[]Update{Set(missingInserted, testValue(0x33))},
	)

	differentExisting := testKey(0x10, 0x00)
	differentExisting[1] = 0x20
	differentNeighbor := testKey(0x30, 0xff)
	differentInserted := testKey(0x10, 0x01)
	differentInserted[1] = 0x10
	differentSnapshot := newTestSnapshot(t, []Entry{
		{Key: differentExisting, Value: testValue(0x44)},
		{Key: differentNeighbor, Value: testValue(0x55)},
	})
	differentProof, differentUpdater := newStatelessTestProof(
		t, differentSnapshot, []Key{differentInserted},
	)
	assertStatelessTopologyCancellationSweep(
		t,
		differentUpdater,
		differentProof,
		[]Update{Set(differentInserted, testValue(0x66))},
	)
}

func TestStatelessUpdateInternalFailureBoundaries(t *testing.T) {
	t.Parallel()

	key := testKey(0, 0)
	snapshot := newTestSnapshot(t, []Entry{{Key: key, Value: testValue(1)}})
	proof, updater := newStatelessTestProof(t, snapshot, []Key{key})
	paths, commitments := statelessTestMaterial(proof)
	updates := []Update{Set(key, testValue(2))}
	newBudget := func() *statelessUpdateBudget {
		return &statelessUpdateBudget{limits: testStatelessUpdateLimits()}
	}
	cloneCommitments := func() map[statelessPath]backend.VectorCommitment {
		cloned := make(map[statelessPath]backend.VectorCommitment, len(commitments))
		for path, commitment := range commitments {
			cloned[path] = commitment
		}

		return cloned
	}

	budget := newBudget()
	budget.limits.MaxPathLookups = 0
	if _, err := updater.updateStems(context.Background(), proof.claims, paths, commitments, updates, budget); !errors.Is(err, errStatelessUpdateResource) {
		t.Fatalf("initial stem-path lookup budget error = %v", err)
	}

	budget = newBudget()
	budget.limits.MaxPathLookups = 1
	if _, err := updater.updateStems(context.Background(), proof.claims, paths, commitments, updates, budget); !errors.Is(err, errStatelessUpdateResource) {
		t.Fatalf("stem-path lookup budget error = %v", err)
	}

	missingStem := cloneCommitments()
	stemPath := makeStatelessPath(key[:1])
	delete(missingStem, stemPath)
	if _, err := updater.updateStems(context.Background(), proof.claims, paths, missingStem, updates, newBudget()); !errors.Is(err, errIncompleteStatelessWitness) {
		t.Fatalf("missing stem commitment error = %v", err)
	}

	if _, err := updater.updateStems(context.Background(), ClaimSet{}, paths, commitments, updates, newBudget()); !errors.Is(err, errInvalidClaimSet) {
		t.Fatalf("invalid claims error = %v", err)
	}

	budget = newBudget()
	budget.limits.MaxPathLookups = 2
	if _, err := updater.updateStems(context.Background(), proof.claims, paths, commitments, updates, budget); !errors.Is(err, errStatelessUpdateResource) {
		t.Fatalf("claim lookup budget error = %v", err)
	}

	budget = newBudget()
	budget.limits.MaxPathLookups = 3
	if _, err := updater.updateStems(context.Background(), proof.claims, paths, commitments, updates, budget); !errors.Is(err, errStatelessUpdateResource) {
		t.Fatalf("suffix lookup budget error = %v", err)
	}

	missingHalf := cloneCommitments()
	halfPath := stemPath
	halfPath.path[halfPath.length] = leafvector.C1HashIndex
	halfPath.length++
	delete(missingHalf, halfPath)
	if _, err := updater.updateStems(context.Background(), proof.claims, paths, missingHalf, updates, newBudget()); !errors.Is(err, errIncompleteStatelessWitness) {
		t.Fatalf("missing suffix commitment error = %v", err)
	}

	budget = newBudget()
	budget.limits.MaxCommitmentUpdates = 1
	budget.commitmentUpdates = 1
	if _, err := updater.updateStems(context.Background(), proof.claims, paths, commitments, updates, budget); !errors.Is(err, errStatelessUpdateResource) {
		t.Fatalf("suffix commitment-update budget error = %v", err)
	}

	budget = newBudget()
	budget.limits.MaxFieldMappings = 1
	budget.fieldMappings = 1
	if _, err := updater.updateStems(context.Background(), proof.claims, paths, commitments, updates, budget); !errors.Is(err, errStatelessUpdateResource) {
		t.Fatalf("suffix field-mapping budget error = %v", err)
	}

	root, err := proof.root.Commitment()
	if err != nil {
		t.Fatalf("proof root commitment: %v", err)
	}
	changed := map[statelessPath]statelessChangedCommitment{
		stemPath: {old: commitments[stemPath], new: commitments[stemPath]},
	}
	budget = newBudget()
	budget.limits.MaxFieldMappings = 1
	if _, err := updater.updateAncestors(context.Background(), commitments, changed, budget); !errors.Is(err, errStatelessUpdateResource) {
		t.Fatalf("ancestor new-field mapping budget error = %v", err)
	}

	budget = newBudget()
	budget.limits.MaxFieldMappings = 1
	budget.fieldMappings = 1
	if _, err := updater.updateAncestors(context.Background(), commitments, changed, budget); !errors.Is(err, errStatelessUpdateResource) {
		t.Fatalf("ancestor old-field mapping budget error = %v", err)
	}

	budget = newBudget()
	budget.limits.MaxPathLookups = 1
	budget.pathLookups = 1
	if _, err := updater.updateAncestors(context.Background(), commitments, changed, budget); !errors.Is(err, errStatelessUpdateResource) {
		t.Fatalf("ancestor lookup budget error = %v", err)
	}

	missingRoot := cloneCommitments()
	delete(missingRoot, statelessPath{})
	if _, err := updater.updateAncestors(context.Background(), missingRoot, changed, newBudget()); !errors.Is(err, errIncompleteStatelessWitness) {
		t.Fatalf("missing ancestor commitment error = %v", err)
	}

	budget = newBudget()
	budget.limits.MaxCommitmentUpdates = 1
	budget.commitmentUpdates = 1
	if _, err := updater.updateAncestors(context.Background(), commitments, changed, budget); !errors.Is(err, errStatelessUpdateResource) {
		t.Fatalf("ancestor commitment-update budget error = %v", err)
	}

	invalidRootPath := statelessPath{path: [maxProofPathLength]byte{1}}
	if _, err := updater.updateAncestors(
		context.Background(),
		commitments,
		map[statelessPath]statelessChangedCommitment{
			invalidRootPath: {old: root, new: root},
		},
		newBudget(),
	); !errors.Is(err, errInvalidStatelessUpdate) {
		t.Fatalf("invalid root path error = %v", err)
	}
	if _, err := updater.updateAncestors(context.Background(), commitments, nil, newBudget()); !errors.Is(err, errInvalidStatelessUpdate) {
		t.Fatalf("empty ancestor changes error = %v", err)
	}
}

func TestStatelessInsertedTopologyInternalFailureBoundaries(t *testing.T) {
	t.Parallel()

	existingKey := testKey(0x00, 0x00)
	neighborKey := testKey(0x02, 0xff)
	insertedKey := testKey(0x01, 0x01)
	snapshot := newTestSnapshot(t, []Entry{
		{Key: existingKey, Value: testValue(0x11)},
		{Key: neighborKey, Value: testValue(0x22)},
	})
	proof, updater := newStatelessTestProof(t, snapshot, []Key{insertedKey})
	paths, commitments := statelessTestMaterial(proof)
	updates := []Update{Set(insertedKey, testValue(0x33))}
	stem := Stem(insertedKey[:31])
	path := paths[stem]
	stemPath := makeStatelessPath(stem[:path.depth])
	newBudget := func() *statelessUpdateBudget {
		return &statelessUpdateBudget{limits: testStatelessUpdateLimits()}
	}
	clonePaths := func() map[Stem]StemPath {
		cloned := make(map[Stem]StemPath, len(paths))
		for candidate, candidatePath := range paths {
			cloned[candidate] = candidatePath
		}

		return cloned
	}
	cloneCommitments := func() map[statelessPath]backend.VectorCommitment {
		cloned := make(map[statelessPath]backend.VectorCommitment, len(commitments))
		for candidatePath, commitment := range commitments {
			cloned[candidatePath] = commitment
		}

		return cloned
	}

	if _, err := updater.updateStems(
		context.Background(), proof.claims, map[Stem]StemPath{}, commitments, updates, newBudget(),
	); !errors.Is(err, errUnsupportedStatelessUpdate) {
		t.Fatalf("missing inserted-stem path error = %v", err)
	}

	invalidPaths := clonePaths()
	invalidPath := invalidPaths[stem]
	invalidPath.kind = StemPathKind(0xff)
	invalidPaths[stem] = invalidPath
	if _, err := updater.updateStems(
		context.Background(), proof.claims, invalidPaths, commitments, updates, newBudget(),
	); !errors.Is(err, errUnsupportedStatelessUpdate) {
		t.Fatalf("unsupported inserted-stem path error = %v", err)
	}

	invalidClaims := proof.claims
	invalidClaims.claims = append([]Claim(nil), proof.claims.claims...)
	invalidClaims.claims[0].kind = ClaimMembership
	if _, err := updater.commitInsertedStem(
		context.Background(), invalidClaims, stem, updates, newBudget(),
	); !errors.Is(err, errIncompleteStatelessWitness) {
		t.Fatalf("non-absence insertion claim error = %v", err)
	}
	if _, err := updater.commitInsertedStem(
		context.Background(), ClaimSet{}, stem, updates, newBudget(),
	); !errors.Is(err, errInvalidClaimSet) {
		t.Fatalf("invalid insertion claim-set error = %v", err)
	}

	for maximum := uint32(0); maximum < 3; maximum++ {
		budget := newBudget()
		budget.limits.MaxCommitmentUpdates = maximum
		if _, err := updater.commitInsertedStem(
			context.Background(), proof.claims, stem, updates, budget,
		); !errors.Is(err, errStatelessUpdateResource) {
			t.Fatalf("inserted-stem commitment limit %d error = %v", maximum, err)
		}
	}
	for maximum := uint32(0); maximum < 2; maximum++ {
		budget := newBudget()
		budget.limits.MaxFieldMappings = maximum
		if _, err := updater.commitInsertedStem(
			context.Background(), proof.claims, stem, updates, budget,
		); !errors.Is(err, errStatelessUpdateResource) {
			t.Fatalf("inserted-stem field limit %d error = %v", maximum, err)
		}
	}
	budget := newBudget()
	budget.limits.MaxPathLookups = 0
	if _, err := updater.commitInsertedStem(
		context.Background(), proof.claims, stem, updates, budget,
	); !errors.Is(err, errStatelessUpdateResource) {
		t.Fatalf("inserted-stem lookup limit error = %v", err)
	}

	insertedCommitment, err := updater.commitInsertedStem(
		context.Background(), proof.claims, stem, updates, newBudget(),
	)
	if err != nil {
		t.Fatalf("build inserted stem fixture: %v", err)
	}
	inserted := statelessInsertedStem{stem: stem, commitment: insertedCommitment}
	if _, err := mergeStatelessExistingStem(
		&stepContext{}, inserted, []statelessInsertedStem{inserted},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled existing-stem merge error = %v", err)
	}
	if _, err := mergeStatelessExistingStem(
		context.Background(), inserted, []statelessInsertedStem{inserted},
	); !errors.Is(err, errInvalidStatelessUpdate) {
		t.Fatalf("duplicate existing-stem merge error = %v", err)
	}
	before := inserted
	before.stem[0]--
	merged, err := mergeStatelessExistingStem(
		context.Background(), inserted, []statelessInsertedStem{before},
	)
	if err != nil || len(merged) != 2 || merged[1].stem != inserted.stem {
		t.Fatalf("append existing stem result = %#v, error = %v", merged, err)
	}

	if _, err := updater.commitInsertedSubtree(
		context.Background(), nil, 0, newBudget(),
	); !errors.Is(err, errInvalidStatelessUpdate) {
		t.Fatalf("empty inserted subtree error = %v", err)
	}
	if got, err := updater.commitInsertedSubtree(
		context.Background(), []statelessInsertedStem{inserted}, 0, newBudget(),
	); err != nil || got != inserted.commitment {
		t.Fatalf("single inserted subtree = %#v, error = %v", got, err)
	}
	if _, err := updater.commitInsertedSubtree(
		context.Background(), []statelessInsertedStem{inserted, inserted}, uint8(len(Stem{})), newBudget(),
	); !errors.Is(err, errInvalidStatelessUpdate) {
		t.Fatalf("excessive inserted subtree depth error = %v", err)
	}
	if _, err := updater.commitInsertedSubtree(
		&stepContext{}, []statelessInsertedStem{inserted}, 0, newBudget(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled inserted subtree error = %v", err)
	}

	second := inserted
	second.stem[1]++
	stems := []statelessInsertedStem{inserted, second}
	if _, err := updater.commitInsertedSubtree(
		&stepContext{successfulChecks: 1}, stems, 0, newBudget(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled inserted subtree loop error = %v", err)
	}
	if _, err := updater.commitInsertedSubtree(
		&stepContext{successfulChecks: 2}, stems, 0, newBudget(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled inserted subtree child error = %v", err)
	}
	budget = newBudget()
	budget.limits.MaxFieldMappings = 0
	if _, err := updater.commitInsertedSubtree(
		context.Background(), stems, 0, budget,
	); !errors.Is(err, errStatelessUpdateResource) {
		t.Fatalf("inserted subtree field limit error = %v", err)
	}
	budget = newBudget()
	budget.limits.MaxCommitmentUpdates = 0
	if _, err := updater.commitInsertedSubtree(
		context.Background(), stems, 0, budget,
	); !errors.Is(err, errStatelessUpdateResource) {
		t.Fatalf("inserted subtree commitment limit error = %v", err)
	}
	if _, err := updater.commitInsertedSubtree(
		context.Background(), []statelessInsertedStem{inserted, inserted}, 30, newBudget(),
	); !errors.Is(err, errInvalidStatelessUpdate) {
		t.Fatalf("inserted subtree child error = %v", err)
	}

	missingBudget := newBudget()
	missingBudget.limits.MaxPathLookups = 1
	if _, err := updater.updateStems(
		context.Background(), proof.claims, paths, commitments, updates, missingBudget,
	); !errors.Is(err, errStatelessUpdateResource) {
		t.Fatalf("missing-stem insertion propagation error = %v", err)
	}

	differentPaths := clonePaths()
	differentPath := differentPaths[stem]
	differentPath.kind = StemPathDifferent
	differentPath.existing = Stem(existingKey[:31])
	differentPaths[stem] = differentPath
	differentCommitments := cloneCommitments()
	differentCommitments[stemPath] = commitments[makeStatelessPath(existingKey[:differentPath.depth])]
	missingDifferentCommitment := cloneCommitments()
	delete(missingDifferentCommitment, stemPath)
	if _, err := updater.updateStems(
		context.Background(), proof.claims, differentPaths, missingDifferentCommitment, updates, newBudget(),
	); !errors.Is(err, errIncompleteStatelessWitness) {
		t.Fatalf("missing different-stem commitment error = %v", err)
	}
	differentBudget := newBudget()
	differentBudget.limits.MaxPathLookups = 1
	if _, err := updater.updateStems(
		context.Background(), proof.claims, differentPaths, differentCommitments, updates, differentBudget,
	); !errors.Is(err, errStatelessUpdateResource) {
		t.Fatalf("different-stem lookup propagation error = %v", err)
	}
	differentBudget = newBudget()
	differentBudget.limits.MaxPathLookups = 2
	if _, err := updater.updateStems(
		context.Background(), proof.claims, differentPaths, differentCommitments, updates, differentBudget,
	); !errors.Is(err, errStatelessUpdateResource) {
		t.Fatalf("different-stem insertion propagation error = %v", err)
	}

	duplicatePaths := clonePaths()
	duplicatePath := duplicatePaths[stem]
	duplicatePath.kind = StemPathDifferent
	duplicatePath.existing = stem
	duplicatePaths[stem] = duplicatePath
	duplicateCommitments := cloneCommitments()
	duplicateCommitments[stemPath] = insertedCommitment
	if _, err := updater.updateStems(
		context.Background(), proof.claims, duplicatePaths, duplicateCommitments, updates, newBudget(),
	); !errors.Is(err, errInvalidStatelessUpdate) {
		t.Fatalf("different-stem merge propagation error = %v", err)
	}
}

func assertSameBackendRoot(t testing.TB, got backend.Root, want backend.Root) {
	t.Helper()

	gotBytes, err := got.Bytes()
	if err != nil {
		t.Fatalf("stateless root bytes: %v", err)
	}
	wantBytes, err := want.Bytes()
	if err != nil {
		t.Fatalf("stateful root bytes: %v", err)
	}
	if gotBytes != wantBytes {
		t.Fatalf("stateless root = %x, want %x", gotBytes, wantBytes)
	}
}

func assertStatelessTopologyCancellationSweep(
	t testing.TB,
	updater *StatelessUpdater,
	proof TreeProof,
	updates []Update,
) {
	t.Helper()

	observed := false
	for successful := 0; successful < 4_000; successful++ {
		_, err := updater.Apply(
			&stepContext{successfulChecks: successful},
			proof,
			updates,
			testProofVerificationLimits(),
			testStatelessUpdateLimits(),
		)
		if err == nil {
			if !observed {
				t.Fatal("topology cancellation sweep exercised no boundary")
			}

			return
		}
		observed = true
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("topology cancellation after %d checks = %v", successful, err)
		}
	}

	t.Fatal("topology cancellation sweep did not reach success")
}

func assertStatelessRootCommitment(t testing.TB, root backend.Root, encoded string) {
	t.Helper()

	commitment, err := root.Commitment()
	if err != nil {
		t.Fatalf("stateless root commitment: %v", err)
	}
	got, err := commitment.Bytes()
	if err != nil {
		t.Fatalf("stateless root commitment bytes: %v", err)
	}
	want, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode expected stateless root: %v", err)
	}
	if string(got[:]) != string(want) {
		t.Fatalf("stateless commitment = %x, want %x", got, want)
	}
}

func newStatelessTestProof(
	t testing.TB,
	snapshot Snapshot,
	keys []Key,
) (TreeProof, *StatelessUpdater) {
	t.Helper()

	proofEngine := newTestProofEngine(t)
	proof, err := proofEngine.Prove(
		context.Background(), snapshot, keys, testProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("generate stateless proof: %v", err)
	}
	updater, err := NewStatelessUpdater(
		context.Background(),
		testAuthstateAggregateOpeningLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("new stateless updater: %v", err)
	}

	return proof, updater
}

func statelessTestMaterial(
	proof TreeProof,
) (map[Stem]StemPath, map[statelessPath]backend.VectorCommitment) {
	paths := make(map[Stem]StemPath, len(proof.stemPaths))
	for index := range proof.stemPaths {
		paths[proof.stemPaths[index].stem] = proof.stemPaths[index]
	}
	commitments := make(map[statelessPath]backend.VectorCommitment, len(proof.commitments)+1)
	root, _ := proof.root.Commitment()
	commitments[statelessPath{}] = root
	for index := range proof.commitments {
		commitments[statelessPath{
			path: proof.commitments[index].path, length: proof.commitments[index].length,
		}] = proof.commitments[index].commitment
	}

	return paths, commitments
}

func assertStatelessResourceError(
	t testing.TB,
	err error,
	resource StatelessUpdateResource,
	limit uint64,
	actual uint64,
) {
	t.Helper()

	var resourceErr *StatelessUpdateResourceError
	if !errors.As(err, &resourceErr) ||
		resourceErr.Resource != resource ||
		resourceErr.Limit != limit ||
		resourceErr.Actual != actual ||
		!errors.Is(err, errStatelessUpdateResource) ||
		resourceErr.Error() == "" {
		t.Fatalf("resource error = %v, want (%d, %d, %d)", err, resource, limit, actual)
	}
}

func testStatelessUpdateLimits() StatelessUpdateLimits {
	return StatelessUpdateLimits{
		MaxUpdates:           16,
		MaxCommitmentUpdates: 64,
		MaxFieldMappings:     128,
		MaxPathLookups:       128,
		MaxTemporaryBytes:    1 << 20,
	}
}
