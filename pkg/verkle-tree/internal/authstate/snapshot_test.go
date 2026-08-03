package authstate

import (
	"context"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/committedtree"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/statemodel"
)

func TestApplyMatchesPinnedIndependentPreAndPostRoots(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{Key: testKey(0x00, 0x00), Value: testValue(0x11)},
		{Key: testKey(0x00, 0x01), Value: testValue(0x22)},
		{Key: testKey(0x01, 0xff), Value: testValue(0x33)},
		{Key: testKey(0x01, 0x7f), Value: testValue(0x44)},
	}
	snapshot, err := NewSnapshot(
		context.Background(),
		entries,
		testLimits(),
		testTreeLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}
	assertRootBytes(
		t,
		snapshot,
		"45c94c43252d82b4ee001e956c39b519bb38349dfc3576a11f3ea2f8a4525135",
	)

	next, transition, err := snapshot.Apply(
		context.Background(),
		[]Update{
			Delete(testKey(0x02, 0x00)),
			Set(testKey(0x00, 0x02), testValue(0x66)),
			Set(testKey(0x00, 0x00), testValue(0x55)),
		},
	)
	if err != nil {
		t.Fatalf("apply updates: %v", err)
	}
	assertRootBytes(
		t,
		next,
		"60a128ee3c2aafe2c12ea104e4b07338677445012dc20c2dd3495a216439e077",
	)
	assertTransitionRoots(t, transition, snapshot, next)

	oldValue, present, err := snapshot.Get(context.Background(), testKey(0x00, 0x00))
	if err != nil || !present || oldValue != testValue(0x11) {
		t.Fatalf("old snapshot value = %x, present %t, error %v", oldValue, present, err)
	}
	newValue, present, err := next.Get(context.Background(), testKey(0x00, 0x00))
	if err != nil || !present || newValue != testValue(0x55) {
		t.Fatalf("new snapshot value = %x, present %t, error %v", newValue, present, err)
	}
	unchangedValue, present, err := next.Get(context.Background(), testKey(0x01, 0xff))
	if err != nil || !present || unchangedValue != testValue(0x33) {
		t.Fatalf("unchanged value = %x, present %t, error %v", unchangedValue, present, err)
	}
}

func TestSnapshotAppliesCanonicalImmutableTransitions(t *testing.T) {
	t.Parallel()

	empty := newTestSnapshot(t, nil)
	key1 := testKey(0, 1)
	key2 := testKey(0, 2)
	key3 := testKey(1, 3)
	zero := Value{}
	after, transition, err := empty.Apply(context.Background(), []Update{
		Set(key3, testValue(0x33)),
		Set(key1, zero),
		Set(key2, testValue(0x22)),
	})
	if err != nil {
		t.Fatalf("apply inserts: %v", err)
	}
	assertTransitionRoots(t, transition, empty, after)
	assertValue(t, empty, key1, Value{}, false)
	assertValue(t, after, key1, zero, true)
	assertValue(t, after, key2, testValue(0x22), true)

	updated, transition, err := after.Apply(context.Background(), []Update{
		Delete(key2),
		Set(key3, testValue(0x44)),
	})
	if err != nil {
		t.Fatalf("apply update and delete: %v", err)
	}
	assertTransitionRoots(t, transition, after, updated)
	assertValue(t, updated, key2, Value{}, false)
	assertValue(t, updated, key3, testValue(0x44), true)
	assertValue(t, after, key2, testValue(0x22), true)

	unchanged, transition, err := updated.Apply(context.Background(), nil)
	if err != nil {
		t.Fatalf("apply empty batch: %v", err)
	}
	assertSameSnapshotRoot(t, updated, unchanged)
	assertTransitionRoots(t, transition, updated, unchanged)
}

func TestSnapshotCanonicalOrderDoesNotDependOnInputOrder(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{Key: testKey(2, 2), Value: testValue(2)},
		{Key: testKey(1, 1), Value: testValue(1)},
		{Key: testKey(3, 3), Value: testValue(3)},
	}
	forward := newTestSnapshot(t, entries)
	reverse := newTestSnapshot(t, []Entry{entries[2], entries[1], entries[0]})
	assertSameSnapshotRoot(t, forward, reverse)

	key4 := testKey(4, 4)
	key5 := testKey(5, 5)
	forward, _, err := forward.Apply(context.Background(), []Update{
		Set(key4, testValue(4)),
		Set(key5, testValue(5)),
	})
	if err != nil {
		t.Fatalf("apply forward: %v", err)
	}
	reverse, _, err = reverse.Apply(context.Background(), []Update{
		Set(key5, testValue(5)),
		Set(key4, testValue(4)),
	})
	if err != nil {
		t.Fatalf("apply reverse: %v", err)
	}
	assertSameSnapshotRoot(t, forward, reverse)
}

func TestSnapshotMergeCoversOrderedBoundaryCases(t *testing.T) {
	t.Parallel()

	snapshot := newTestSnapshot(t, []Entry{
		{Key: testKey(2, 0), Value: testValue(2)},
		{Key: testKey(3, 0), Value: testValue(3)},
	})

	prepended, _, err := snapshot.Apply(
		context.Background(),
		[]Update{Set(testKey(1, 0), testValue(1))},
	)
	if err != nil {
		t.Fatalf("prepend key: %v", err)
	}
	assertValue(t, prepended, testKey(1, 0), testValue(1), true)

	for _, key := range []Key{testKey(1, 0), testKey(4, 0)} {
		unchanged, _, applyErr := snapshot.Apply(context.Background(), []Update{Delete(key)})
		if applyErr != nil {
			t.Fatalf("delete absent key %x: %v", key, applyErr)
		}
		assertSameSnapshotRoot(t, snapshot, unchanged)
	}
}

func TestSnapshotTransitionsMatchIndependentStateModel(t *testing.T) {
	t.Parallel()

	authenticated := newTestSnapshot(t, nil)
	oracle, err := statemodel.NewSnapshot(statemodel.Limits{
		MaxBatchUpdates:   32,
		MaxEntries:        32,
		MaxTemporaryBytes: 1 << 20,
	})
	if err != nil {
		t.Fatalf("new reference snapshot: %v", err)
	}

	type batch struct {
		auth   []Update
		oracle []statemodel.Update
	}
	batches := []batch{
		{
			auth: []Update{
				Set(testKey(3, 3), testValue(3)),
				Set(testKey(1, 1), Value{}),
				Set(testKey(2, 2), testValue(2)),
			},
			oracle: []statemodel.Update{
				statemodel.Set(statemodel.Key(testKey(3, 3)), statemodel.Value(testValue(3))),
				statemodel.Set(statemodel.Key(testKey(1, 1)), statemodel.Value{}),
				statemodel.Set(statemodel.Key(testKey(2, 2)), statemodel.Value(testValue(2))),
			},
		},
		{
			auth: []Update{
				Delete(testKey(0, 0)),
				Set(testKey(3, 3), testValue(9)),
				Delete(testKey(2, 2)),
			},
			oracle: []statemodel.Update{
				statemodel.Delete(statemodel.Key(testKey(0, 0))),
				statemodel.Set(statemodel.Key(testKey(3, 3)), statemodel.Value(testValue(9))),
				statemodel.Delete(statemodel.Key(testKey(2, 2))),
			},
		},
		{
			auth:   []Update{Set(testKey(4, 4), testValue(4))},
			oracle: []statemodel.Update{statemodel.Set(statemodel.Key(testKey(4, 4)), statemodel.Value(testValue(4)))},
		},
	}

	for batchIndex, updates := range batches {
		authenticated, _, err = authenticated.Apply(context.Background(), updates.auth)
		if err != nil {
			t.Fatalf("apply authenticated batch %d: %v", batchIndex, err)
		}
		oracle, err = oracle.Apply(context.Background(), updates.oracle)
		if err != nil {
			t.Fatalf("apply reference batch %d: %v", batchIndex, err)
		}
		for first := byte(0); first <= 5; first++ {
			key := testKey(first, first)
			got, present, getErr := authenticated.Get(context.Background(), key)
			want, wantPresent, wantErr := oracle.Get(context.Background(), statemodel.Key(key))
			if getErr != nil || wantErr != nil || present != wantPresent || statemodel.Value(got) != want {
				t.Fatalf(
					"batch %d key %x = %x/%t/%v, want %x/%t/%v",
					batchIndex,
					key,
					got,
					present,
					getErr,
					want,
					wantPresent,
					wantErr,
				)
			}
		}
	}
}

func TestSnapshotRejectsInvalidTransitionsAtomically(t *testing.T) {
	t.Parallel()

	snapshot := newTestSnapshot(t, []Entry{{Key: testKey(1, 1), Value: testValue(1)}})
	tests := map[string]struct {
		updates []Update
		want    error
	}{
		"duplicate": {
			updates: []Update{Set(testKey(2, 2), testValue(2)), Delete(testKey(2, 2))},
			want:    errDuplicateKey,
		},
		"invalid kind": {
			updates: []Update{{kind: UpdateKind(0xff), key: testKey(2, 2)}},
			want:    errInvalidUpdate,
		},
		"delete with value": {
			updates: []Update{{kind: UpdateDelete, key: testKey(2, 2), value: Value{1}}},
			want:    errInvalidUpdate,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, transition, err := snapshot.Apply(context.Background(), test.updates)
			if !errors.Is(err, test.want) {
				t.Fatalf("apply error = %v, want %v", err, test.want)
			}
			if _, rootErr := transition.PreRoot(); !errors.Is(rootErr, errInvalidTransition) {
				t.Fatalf("failed transition pre-root error = %v", rootErr)
			}
			assertValue(t, snapshot, testKey(1, 1), testValue(1), true)
		})
	}
}

func TestSnapshotEnforcesLimitsBeforeAmplifiedWork(t *testing.T) {
	t.Parallel()

	initialLimits := testLimits()
	initialLimits.MaxEntries = 1
	_, err := NewSnapshot(
		context.Background(),
		[]Entry{{Key: testKey(1, 1)}, {Key: testKey(2, 2)}},
		initialLimits,
		testTreeLimits(),
		testCommitmentLimits(),
	)
	assertResourceError(t, err, ResourceEntries, 1, 2)

	initialLimits = testLimits()
	initialLimits.MaxTemporaryBytes = 255
	_, err = NewSnapshot(
		context.Background(),
		[]Entry{{Key: testKey(1, 1)}, {Key: testKey(2, 2)}},
		initialLimits,
		testTreeLimits(),
		testCommitmentLimits(),
	)
	assertResourceError(t, err, ResourceTemporaryBytes, 255, 256)

	tests := []struct {
		name     string
		limits   Limits
		updates  []Update
		resource Resource
		limit    uint64
		actual   uint64
	}{
		{
			name: "batch",
			limits: Limits{
				MaxEntries: 2, MaxBatchUpdates: 1, MaxTemporaryBytes: 1024,
			},
			updates:  []Update{Set(testKey(1, 1), Value{}), Set(testKey(2, 2), Value{})},
			resource: ResourceBatchUpdates, limit: 1, actual: 2,
		},
		{
			name: "update scratch",
			limits: Limits{
				MaxEntries: 1, MaxBatchUpdates: 1, MaxTemporaryBytes: 191,
			},
			updates:  []Update{Set(testKey(1, 1), Value{})},
			resource: ResourceTemporaryBytes, limit: 191, actual: 192,
		},
		{
			name: "entries",
			limits: Limits{
				MaxEntries: 1, MaxBatchUpdates: 2, MaxTemporaryBytes: 1024,
			},
			updates:  []Update{Set(testKey(1, 1), Value{}), Set(testKey(2, 2), Value{})},
			resource: ResourceEntries, limit: 1, actual: 2,
		},
		{
			name: "complete scratch",
			limits: Limits{
				MaxEntries: 1, MaxBatchUpdates: 1, MaxTemporaryBytes: 255,
			},
			updates:  []Update{Set(testKey(1, 1), Value{})},
			resource: ResourceTemporaryBytes, limit: 255, actual: 256,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := newSnapshotWithLimits(t, nil, test.limits)
			_, _, applyErr := snapshot.Apply(context.Background(), test.updates)
			assertResourceError(t, applyErr, test.resource, test.limit, test.actual)
		})
	}
}

func TestSnapshotRejectsInvalidStateInputsAndContexts(t *testing.T) {
	t.Parallel()

	validLimits := testLimits()
	invalidLimits := []Limits{{}}
	for _, invalidate := range []func(*Limits){
		func(limits *Limits) { limits.MaxEntries = 0 },
		func(limits *Limits) { limits.MaxBatchUpdates = 0 },
		func(limits *Limits) { limits.MaxTemporaryBytes = 0 },
		func(limits *Limits) { limits.MaxEntries = maxSupportedCount + 1 },
		func(limits *Limits) { limits.MaxBatchUpdates = maxSupportedCount + 1 },
	} {
		candidate := validLimits
		invalidate(&candidate)
		invalidLimits = append(invalidLimits, candidate)
	}
	for _, limits := range invalidLimits {
		if _, err := NewSnapshot(
			context.Background(), nil, limits, testTreeLimits(), testCommitmentLimits(),
		); !errors.Is(err, errInvalidLimits) {
			t.Fatalf("invalid limits error = %v, want %v", err, errInvalidLimits)
		}
	}
	maximum := validLimits
	maximum.MaxEntries = maxSupportedCount
	maximum.MaxBatchUpdates = maxSupportedCount
	if err := maximum.validate(); err != nil {
		t.Fatalf("exact maximum limits: %v", err)
	}

	entry := Entry{Key: testKey(1, 1), Value: testValue(1)}
	if _, err := NewSnapshot(
		context.Background(), []Entry{entry, entry}, validLimits, testTreeLimits(), testCommitmentLimits(),
	); !errors.Is(err, errDuplicateKey) {
		t.Fatalf("duplicate initial key error = %v, want %v", err, errDuplicateKey)
	}
	if _, err := NewSnapshot(
		context.Background(), nil, validLimits, committedtree.Limits{}, testCommitmentLimits(),
	); err == nil {
		t.Fatal("invalid committed-tree limits were accepted")
	}
	if _, err := NewSnapshot(
		context.Background(), nil, validLimits, testTreeLimits(), backend.CommitmentLimits{},
	); err == nil {
		t.Fatal("invalid commitment limits were accepted")
	}

	var nilContext context.Context
	if _, err := NewSnapshot(
		nilContext, nil, validLimits, testTreeLimits(), testCommitmentLimits(),
	); !errors.Is(err, errInvalidContext) {
		t.Fatalf("nil constructor context error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewSnapshot(
		cancelled, nil, validLimits, testTreeLimits(), testCommitmentLimits(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled constructor context error = %v", err)
	}
	if _, err := NewSnapshot(
		&stepContext{successfulChecks: 5},
		nil,
		validLimits,
		testTreeLimits(),
		testCommitmentLimits(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("construction cancellation error = %v", err)
	}

	snapshot := newTestSnapshot(t, nil)
	if _, _, err := snapshot.Get(nilContext, Key{}); !errors.Is(err, errInvalidContext) {
		t.Fatalf("nil get context error = %v", err)
	}
	if _, _, err := snapshot.Get(cancelled, Key{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled get context error = %v", err)
	}
	if _, _, err := snapshot.Apply(nilContext, nil); !errors.Is(err, errInvalidContext) {
		t.Fatalf("nil apply context error = %v", err)
	}
	if _, _, err := snapshot.Apply(cancelled, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled apply context error = %v", err)
	}
	storageLimits := committedtree.StorageEncodingLimits{
		MaxNodes:          8,
		MaxNodeBytes:      1 << 16,
		MaxEncodedBytes:   1 << 16,
		MaxHashes:         8,
		MaxTemporaryBytes: 1 << 17,
	}
	if _, err := snapshot.StorageImage(
		context.Background(),
		storageLimits,
	); err != nil {
		t.Fatalf("valid storage image: %v", err)
	}
	if _, err := snapshot.StorageImage(
		nilContext,
		storageLimits,
	); !errors.Is(err, errInvalidContext) {
		t.Fatalf("nil storage-image context error = %v", err)
	}

	var zero Snapshot
	if _, _, err := zero.Get(context.Background(), Key{}); !errors.Is(err, errInvalidSnapshot) {
		t.Fatalf("zero get error = %v", err)
	}
	if _, err := zero.Root(); !errors.Is(err, errInvalidSnapshot) {
		t.Fatalf("zero root error = %v", err)
	}
	if _, err := zero.StorageImage(
		context.Background(),
		storageLimits,
	); !errors.Is(err, errInvalidSnapshot) {
		t.Fatalf("zero storage-image error = %v", err)
	}
	if _, _, err := zero.Apply(context.Background(), nil); !errors.Is(err, errInvalidSnapshot) {
		t.Fatalf("zero apply error = %v", err)
	}
	corrupt := Snapshot{limits: validLimits, valid: true}
	if _, err := corrupt.Root(); !errors.Is(err, errInvalidSnapshot) {
		t.Fatalf("corrupt snapshot error = %v", err)
	}
	corrupt = Snapshot{builder: &scriptedTreeBuilder{}, valid: true}
	if _, err := corrupt.Root(); !errors.Is(err, errInvalidSnapshot) {
		t.Fatalf("corrupt limits error = %v", err)
	}
	corrupt = newTestSnapshot(t, []Entry{
		{Key: testKey(1, 1), Value: testValue(1)},
		{Key: testKey(2, 2), Value: testValue(2)},
	})
	corrupt.entries[0], corrupt.entries[1] = corrupt.entries[1], corrupt.entries[0]
	if _, _, err := corrupt.Apply(
		context.Background(), []Update{Set(testKey(2, 2), testValue(3))},
	); !errors.Is(err, errInvalidSnapshot) {
		t.Fatalf("corrupt entry order error = %v", err)
	}

	var transition Transition
	if _, err := transition.PreRoot(); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("zero transition pre-root error = %v", err)
	}
	if _, err := transition.PostRoot(); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("zero transition post-root error = %v", err)
	}
	if err := validateResultCount([]Entry{{}}, 0); !errors.Is(err, errInvalidSnapshot) {
		t.Fatalf("result count error = %v, want %v", err, errInvalidSnapshot)
	}
}

func TestSnapshotPropagatesCommittedTreeFailures(t *testing.T) {
	t.Parallel()

	want := errors.New("tree build failed")
	snapshot := newTestSnapshot(t, nil)
	snapshot.builder = &scriptedTreeBuilder{err: want}
	if _, _, err := snapshot.Apply(
		context.Background(), []Update{Set(testKey(1, 1), testValue(1))},
	); !errors.Is(err, want) {
		t.Fatalf("build error = %v, want %v", err, want)
	}

	snapshot = newTestSnapshot(t, nil)
	snapshot.builder = &scriptedTreeBuilder{}
	if _, _, err := snapshot.Apply(
		context.Background(), []Update{Set(testKey(1, 1), testValue(1))},
	); err == nil {
		t.Fatal("invalid built tree was accepted")
	}

	snapshot = newTestSnapshot(t, nil)
	snapshot.tree = committedtree.Tree{}
	if _, _, err := snapshot.Apply(
		context.Background(), []Update{Set(testKey(1, 1), testValue(1))},
	); err == nil {
		t.Fatal("invalid pre-state tree was accepted")
	}
}

func TestSnapshotStopsAtCancellationBoundaries(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{Key: testKey(2, 2), Value: testValue(2)},
		{Key: testKey(1, 1), Value: testValue(1)},
	}
	for cancelAt := 1; cancelAt <= 20; cancelAt++ {
		_, err := prepareInitialEntries(&stepContext{successfulChecks: cancelAt - 1}, entries, testLimits())
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("prepare cancellation at %d = %v", cancelAt, err)
		}
	}
	if err := sortEntries(&stepContext{}, []Entry{{}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("short entry sort cancellation = %v", err)
	}
	if err := sortUpdates(&stepContext{}, []Update{{}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("short update sort cancellation = %v", err)
	}

	snapshot := newTestSnapshot(t, entries)
	updates := []Update{
		Set(testKey(3, 3), testValue(3)),
		Delete(testKey(1, 1)),
	}
	for cancelAt := 1; cancelAt <= 40; cancelAt++ {
		_, _, err := snapshot.Apply(&stepContext{successfulChecks: cancelAt - 1}, updates)
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("apply cancellation at %d = %v", cancelAt, err)
		}
	}
}

func TestCanonicalSortHelpersPreserveExactAndStableOrder(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{Key: testKey(2, 2), Value: testValue(2)},
		{Key: testKey(1, 1), Value: testValue(1)},
	}
	if err := sortEntries(context.Background(), entries); err != nil {
		t.Fatalf("sort two entries: %v", err)
	}
	if entries[0].Key != testKey(1, 1) || entries[1].Key != testKey(2, 2) {
		t.Fatalf("two-entry order = %x", entries)
	}

	equalKey := testKey(9, 9)
	entries = []Entry{
		{Key: equalKey, Value: Value{0: 1}},
		{Key: testKey(8, 8), Value: Value{0: 2}},
		{Key: equalKey, Value: Value{0: 3}},
	}
	if err := sortEntries(context.Background(), entries); err != nil {
		t.Fatalf("stable entry sort: %v", err)
	}
	if entries[0].Key != testKey(8, 8) ||
		entries[1].Value[0] != 1 ||
		entries[2].Value[0] != 3 {
		t.Fatalf("stable entry order = %#v", entries)
	}

	updates := []Update{
		Set(equalKey, Value{0: 1}),
		Set(testKey(8, 8), Value{0: 2}),
		Set(equalKey, Value{0: 3}),
	}
	if err := sortUpdates(context.Background(), updates); err != nil {
		t.Fatalf("stable update sort: %v", err)
	}
	if updates[0].key != testKey(8, 8) ||
		updates[1].value[0] != 1 ||
		updates[2].value[0] != 3 {
		t.Fatalf("stable update order = %#v", updates)
	}
}

func TestSnapshotSupportsConcurrentImmutableUse(t *testing.T) {
	t.Parallel()

	snapshot := newTestSnapshot(t, []Entry{{Key: testKey(1, 1), Value: testValue(1)}})
	const workers = 8
	errorsByWorker := make(chan error, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for worker := range workers {
		go func(worker int) {
			defer group.Done()
			if worker%2 == 0 {
				value, present, err := snapshot.Get(context.Background(), testKey(1, 1))
				if err != nil || !present || value != testValue(1) {
					errorsByWorker <- errors.New("concurrent read differs")
				}
				return
			}
			next, transition, err := snapshot.Apply(
				context.Background(),
				[]Update{Set(testKey(byte(worker+1), byte(worker+1)), testValue(byte(worker+1)))},
			)
			if err != nil {
				errorsByWorker <- err
				return
			}
			if _, err := next.Root(); err != nil {
				errorsByWorker <- err
				return
			}
			if _, err := transition.PostRoot(); err != nil {
				errorsByWorker <- err
			}
		}(worker)
	}
	group.Wait()
	close(errorsByWorker)
	for workerErr := range errorsByWorker {
		t.Error(workerErr)
	}
}

func TestSnapshotCopiesCanonicalEntriesWithinBounds(t *testing.T) {
	t.Parallel()

	first := testKey(0x10, 0x01)
	snapshot := newTestSnapshot(t, []Entry{
		{Key: testKey(0x20, 0x02), Value: testValue(2)},
		{Key: first, Value: testValue(1)},
	})
	count, err := snapshot.EntryCount()
	if err != nil || count != 2 {
		t.Fatalf("entry count = %d, error %v", count, err)
	}
	entries, err := snapshot.CopyEntries(context.Background(), 2, 128)
	if err != nil {
		t.Fatalf("copy entries: %v", err)
	}
	if entries[0].Key != first {
		t.Fatalf("first copied key = %x", entries[0].Key)
	}
	entries[0].Value = testValue(0xff)
	value, present, err := snapshot.Get(context.Background(), first)
	if err != nil || !present || value != testValue(1) {
		t.Fatalf("snapshot aliases entry copy = %x/%t, error %v", value, present, err)
	}

	if _, err := snapshot.CopyEntries(
		context.Background(), 1, 128,
	); !errors.Is(err, errResource) {
		t.Fatalf("entry-count resource error = %v", err)
	}
	if _, err := snapshot.CopyEntries(
		context.Background(), 2, 127,
	); !errors.Is(err, errResource) {
		t.Fatalf("entry-copy resource error = %v", err)
	}
	if _, err := snapshot.CopyEntries(
		&stepContext{successfulChecks: 1}, 2, 128,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("entry-copy cancellation error = %v", err)
	}
	if _, err := snapshot.CopyEntries(
		context.Background(), 0, 128,
	); !errors.Is(err, errInvalidLimits) {
		t.Fatalf("invalid copy limits error = %v", err)
	}
	var nilContext context.Context
	if _, err := snapshot.CopyEntries(
		nilContext, 2, 128,
	); !errors.Is(err, errInvalidContext) {
		t.Fatalf("nil-context copy error = %v", err)
	}
	var zero Snapshot
	if _, err := zero.EntryCount(); !errors.Is(err, errInvalidSnapshot) {
		t.Fatalf("zero entry count error = %v", err)
	}
	if _, err := zero.CopyEntries(
		context.Background(), 2, 128,
	); !errors.Is(err, errInvalidSnapshot) {
		t.Fatalf("zero entry copy error = %v", err)
	}
}

func assertRootBytes(t testing.TB, snapshot Snapshot, encoded string) {
	t.Helper()

	root, err := snapshot.Root()
	if err != nil {
		t.Fatalf("get root: %v", err)
	}
	got, err := root.Bytes()
	if err != nil {
		t.Fatalf("encode root: %v", err)
	}
	want, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode expected root: %v", err)
	}
	if string(got[:]) != string(want) {
		t.Fatalf("root = %x, want %x", got, want)
	}
}

func assertTransitionRoots(
	t testing.TB,
	transition Transition,
	before Snapshot,
	after Snapshot,
) {
	t.Helper()

	transitionBefore, err := transition.PreRoot()
	if err != nil {
		t.Fatalf("get transition pre-root: %v", err)
	}
	transitionAfter, err := transition.PostRoot()
	if err != nil {
		t.Fatalf("get transition post-root: %v", err)
	}
	beforeRoot, err := before.Root()
	if err != nil {
		t.Fatalf("get snapshot pre-root: %v", err)
	}
	afterRoot, err := after.Root()
	if err != nil {
		t.Fatalf("get snapshot post-root: %v", err)
	}
	assertSameCommitment(t, transitionBefore, beforeRoot)
	assertSameCommitment(t, transitionAfter, afterRoot)
}

func assertSameSnapshotRoot(t testing.TB, left Snapshot, right Snapshot) {
	t.Helper()

	leftRoot, err := left.Root()
	if err != nil {
		t.Fatalf("get left root: %v", err)
	}
	rightRoot, err := right.Root()
	if err != nil {
		t.Fatalf("get right root: %v", err)
	}
	assertSameCommitment(t, leftRoot, rightRoot)
}

func assertSameCommitment(
	t testing.TB,
	left backend.VectorCommitment,
	right backend.VectorCommitment,
) {
	t.Helper()

	leftIdentity, err := left.IsIdentity()
	if err != nil {
		t.Fatalf("classify left root: %v", err)
	}
	rightIdentity, err := right.IsIdentity()
	if err != nil {
		t.Fatalf("classify right root: %v", err)
	}
	if leftIdentity || rightIdentity {
		if leftIdentity != rightIdentity {
			t.Fatalf("root identity differs: %t / %t", leftIdentity, rightIdentity)
		}
		return
	}
	leftBytes, err := left.Bytes()
	if err != nil {
		t.Fatalf("encode left root: %v", err)
	}
	rightBytes, err := right.Bytes()
	if err != nil {
		t.Fatalf("encode right root: %v", err)
	}
	if leftBytes != rightBytes {
		t.Fatalf("roots differ: %x / %x", leftBytes, rightBytes)
	}
}

func assertValue(t testing.TB, snapshot Snapshot, key Key, want Value, wantPresent bool) {
	t.Helper()

	got, present, err := snapshot.Get(context.Background(), key)
	if err != nil || present != wantPresent || (present && got != want) {
		t.Fatalf("get %x = %x, present %t, error %v; want %x/%t", key, got, present, err, want, wantPresent)
	}
}

func assertResourceError(
	t testing.TB,
	err error,
	resource Resource,
	limit uint64,
	actual uint64,
) {
	t.Helper()

	var resourceErr *ResourceError
	if !errors.As(err, &resourceErr) {
		t.Fatalf("error = %v, want ResourceError", err)
	}
	if resourceErr.Resource != resource || resourceErr.Limit != limit || resourceErr.Actual != actual {
		t.Fatalf(
			"resource error = (%d, %d, %d), want (%d, %d, %d)",
			resourceErr.Resource,
			resourceErr.Limit,
			resourceErr.Actual,
			resource,
			limit,
			actual,
		)
	}
	if !errors.Is(err, errResource) || resourceErr.Error() == "" {
		t.Fatalf("resource error does not preserve sentinel: %v", err)
	}
}

func newTestSnapshot(t testing.TB, entries []Entry) Snapshot {
	t.Helper()

	return newSnapshotWithLimits(t, entries, testLimits())
}

func newSnapshotWithLimits(t testing.TB, entries []Entry, limits Limits) Snapshot {
	t.Helper()

	snapshot, err := NewSnapshot(
		context.Background(), entries, limits, testTreeLimits(), testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}

	return snapshot
}

type scriptedTreeBuilder struct {
	tree committedtree.Tree
	err  error
}

func (builder *scriptedTreeBuilder) Build(
	context.Context,
	[]committedtree.Entry,
) (committedtree.Tree, error) {
	return builder.tree, builder.err
}

type stepContext struct {
	successfulChecks int
}

func (*stepContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (*stepContext) Done() <-chan struct{} {
	return nil
}

func (ctx *stepContext) Err() error {
	if ctx.successfulChecks == 0 {
		return context.Canceled
	}
	ctx.successfulChecks--

	return nil
}

func (*stepContext) Value(any) any {
	return nil
}

func testKey(first, suffix byte) Key {
	var key Key
	key[0] = first
	key[31] = suffix

	return key
}

func testValue(seed byte) Value {
	var value Value
	for index := range value {
		value[index] = seed + byte(index)
	}

	return value
}

func testLimits() Limits {
	return Limits{
		MaxEntries:        32,
		MaxBatchUpdates:   32,
		MaxTemporaryBytes: 1 << 20,
	}
}

func testTreeLimits() committedtree.Limits {
	return committedtree.Limits{
		MaxEntries:         32,
		MaxStems:           32,
		MaxNodes:           128,
		MaxEdges:           128,
		MaxCommitments:     128,
		MaxFieldMappings:   128,
		MaxCommitmentTerms: 2048,
		MaxTemporaryBytes:  1 << 20,
	}
}

func testCommitmentLimits() backend.CommitmentLimits {
	return backend.CommitmentLimits{
		MaxGeneratorDerivations: backend.VectorWidth,
		MaxScalarDecodes:        backend.VectorWidth,
		MaxMSMTerms:             backend.VectorWidth,
		MaxTemporaryBytes:       1 << 20,
	}
}
