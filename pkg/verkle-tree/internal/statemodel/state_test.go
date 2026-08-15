package statemodel

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestSnapshotAppliesCanonicalImmutableTransitions(t *testing.T) {
	t.Parallel()

	limits := Limits{
		MaxBatchUpdates:   8,
		MaxEntries:        8,
		MaxTemporaryBytes: 4096,
	}
	before, err := NewSnapshot(limits)
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}

	key1 := Key{0x01}
	key2 := Key{0x02}
	key3 := Key{0x03}
	zero := Value{}
	value2 := Value{0x22}
	value3 := Value{0x33}

	after, err := before.Apply(context.Background(), []Update{
		Set(key3, value3),
		Set(key1, zero),
		Set(key2, value2),
	})
	if err != nil {
		t.Fatalf("apply inserts: %v", err)
	}

	if _, present, err := before.Get(context.Background(), key1); err != nil || present {
		t.Fatalf("old snapshot get = present %t, error %v; want absent", present, err)
	}
	if got, present, err := after.Get(context.Background(), key1); err != nil ||
		!present || got != zero {
		t.Fatalf("zero value get = %x, present %t, error %v", got, present, err)
	}
	if got, present, err := after.Get(context.Background(), key2); err != nil ||
		!present || got != value2 {
		t.Fatalf("value get = %x, present %t, error %v", got, present, err)
	}

	keys, err := after.Keys(context.Background())
	if err != nil {
		t.Fatalf("keys: %v", err)
	}
	if want := []Key{key1, key2, key3}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys = %x, want %x", keys, want)
	}

	updated, err := after.Apply(context.Background(), []Update{
		Delete(key2),
		Set(key3, Value{0x44}),
	})
	if err != nil {
		t.Fatalf("apply update and delete: %v", err)
	}
	if _, present, err := updated.Get(context.Background(), key2); err != nil || present {
		t.Fatalf("deleted get = present %t, error %v; want absent", present, err)
	}
	if got, present, err := updated.Get(context.Background(), key3); err != nil ||
		!present || got != (Value{0x44}) {
		t.Fatalf("updated get = %x, present %t, error %v", got, present, err)
	}
	if got, present, err := after.Get(context.Background(), key2); err != nil ||
		!present || got != value2 {
		t.Fatalf("prior snapshot changed: value %x, present %t, error %v", got, present, err)
	}
}

func TestSnapshotCanonicalOrderDoesNotDependOnBatchOrder(t *testing.T) {
	t.Parallel()

	limits := Limits{
		MaxBatchUpdates:   4,
		MaxEntries:        4,
		MaxTemporaryBytes: 2048,
	}
	empty, err := NewSnapshot(limits)
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}

	key1 := Key{0x01}
	key2 := Key{0x02}
	forward, err := empty.Apply(context.Background(), []Update{
		Set(key1, Value{0x11}),
		Set(key2, Value{0x22}),
	})
	if err != nil {
		t.Fatalf("apply forward: %v", err)
	}
	reverse, err := empty.Apply(context.Background(), []Update{
		Set(key2, Value{0x22}),
		Set(key1, Value{0x11}),
	})
	if err != nil {
		t.Fatalf("apply reverse: %v", err)
	}

	forwardKeys, err := forward.Keys(context.Background())
	if err != nil {
		t.Fatalf("forward keys: %v", err)
	}
	reverseKeys, err := reverse.Keys(context.Background())
	if err != nil {
		t.Fatalf("reverse keys: %v", err)
	}
	if !reflect.DeepEqual(forwardKeys, reverseKeys) {
		t.Fatalf("ordered keys differ: %x != %x", forwardKeys, reverseKeys)
	}
}

func TestSnapshotMergeCoversOrderedBoundaryCases(t *testing.T) {
	t.Parallel()

	snapshot, err := NewSnapshot(Limits{
		MaxBatchUpdates:   4,
		MaxEntries:        4,
		MaxTemporaryBytes: 1024,
	})
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}
	snapshot, err = snapshot.Apply(context.Background(), []Update{
		Set(Key{0x02}, Value{0x22}),
		Set(Key{0x03}, Value{0x33}),
	})
	if err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}

	prepended, err := snapshot.Apply(
		context.Background(),
		[]Update{Set(Key{0x01}, Value{0x11})},
	)
	if err != nil {
		t.Fatalf("prepend key: %v", err)
	}
	keys, err := prepended.Keys(context.Background())
	if err != nil {
		t.Fatalf("prepend keys: %v", err)
	}
	if want := []Key{{0x01}, {0x02}, {0x03}}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("prepended keys = %x, want %x", keys, want)
	}

	unchanged, err := snapshot.Apply(
		context.Background(),
		[]Update{Delete(Key{0x01})},
	)
	if err != nil {
		t.Fatalf("delete missing prefix: %v", err)
	}
	keys, err = unchanged.Keys(context.Background())
	if err != nil {
		t.Fatalf("unchanged keys: %v", err)
	}
	if want := []Key{{0x02}, {0x03}}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("unchanged keys = %x, want %x", keys, want)
	}

	unchanged, err = snapshot.Apply(
		context.Background(),
		[]Update{Delete(Key{0x04})},
	)
	if err != nil {
		t.Fatalf("delete missing suffix: %v", err)
	}
	keys, err = unchanged.Keys(context.Background())
	if err != nil {
		t.Fatalf("suffix keys: %v", err)
	}
	if want := []Key{{0x02}, {0x03}}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("suffix keys = %x, want %x", keys, want)
	}
}

func TestSnapshotRejectsInvalidTransitionsAtomically(t *testing.T) {
	t.Parallel()

	limits := Limits{
		MaxBatchUpdates:   2,
		MaxEntries:        1,
		MaxTemporaryBytes: 512,
	}
	snapshot, err := NewSnapshot(limits)
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}

	key1 := Key{0x01}
	key2 := Key{0x02}
	tests := map[string]struct {
		updates []Update
		want    error
	}{
		"duplicate": {
			updates: []Update{Set(key1, Value{0x11}), Delete(key1)},
			want:    errDuplicateKey,
		},
		"entries": {
			updates: []Update{Set(key1, Value{0x11}), Set(key2, Value{0x22})},
			want:    errResourceExhausted,
		},
		"invalid kind": {
			updates: []Update{{kind: UpdateKind(0xff), key: key1}},
			want:    errInvalidUpdate,
		},
		"delete with value": {
			updates: []Update{{kind: UpdateDelete, key: key1, value: Value{1}}},
			want:    errInvalidUpdate,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := snapshot.Apply(context.Background(), test.updates)
			if !errors.Is(err, test.want) {
				t.Fatalf("apply error = %v, want %v", err, test.want)
			}
			if name == "entries" {
				var resourceErr *ResourceError
				if !errors.As(err, &resourceErr) ||
					resourceErr.Kind != ResourceEntries {
					t.Fatalf("entries error = %v, want ResourceEntries", err)
				}
			}
			if keys, keysErr := snapshot.Keys(context.Background()); keysErr != nil ||
				len(keys) != 0 {
				t.Fatalf("snapshot changed: keys %x, error %v", keys, keysErr)
			}
		})
	}
}

func TestSnapshotEnforcesOperationBudgetsBeforeAllocation(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		limits  Limits
		updates []Update
		kind    ResourceKind
	}{
		"batch": {
			limits: Limits{
				MaxBatchUpdates:   1,
				MaxEntries:        2,
				MaxTemporaryBytes: 512,
			},
			updates: []Update{Set(Key{1}, Value{1}), Set(Key{2}, Value{2})},
			kind:    ResourceBatchUpdates,
		},
		"temporary": {
			limits: Limits{
				MaxBatchUpdates:   1,
				MaxEntries:        1,
				MaxTemporaryBytes: 1,
			},
			updates: []Update{Set(Key{1}, Value{1})},
			kind:    ResourceTemporaryBytes,
		},
		"result temporary": {
			limits: Limits{
				MaxBatchUpdates:   1,
				MaxEntries:        1,
				MaxTemporaryBytes: workingUpdateBytes,
			},
			updates: []Update{Set(Key{1}, Value{1})},
			kind:    ResourceTemporaryBytes,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			snapshot, err := NewSnapshot(test.limits)
			if err != nil {
				t.Fatalf("new snapshot: %v", err)
			}
			_, err = snapshot.Apply(context.Background(), test.updates)
			var resourceErr *ResourceError
			if !errors.As(err, &resourceErr) {
				t.Fatalf("apply error = %v, want ResourceError", err)
			}
			if resourceErr.Kind != test.kind {
				t.Fatalf("resource kind = %d, want %d", resourceErr.Kind, test.kind)
			}
			if name == "result temporary" &&
				resourceErr.Actual != workingUpdateBytes+workingEntryBytes {
				t.Fatalf(
					"temporary bytes = %d, want %d",
					resourceErr.Actual,
					workingUpdateBytes+workingEntryBytes,
				)
			}
			if !errors.Is(err, errResourceExhausted) {
				t.Fatalf("apply error = %v, want errResourceExhausted", err)
			}
			if resourceErr.Error() == "" {
				t.Fatal("resource error must have a message")
			}
		})
	}
}

func TestSnapshotRejectsInvalidStateAndContext(t *testing.T) {
	t.Parallel()

	validLimits := Limits{
		MaxBatchUpdates:   1,
		MaxEntries:        1,
		MaxTemporaryBytes: 128,
	}
	invalidLimits := []Limits{
		{},
		{MaxBatchUpdates: 1, MaxEntries: 1},
		{MaxBatchUpdates: 1, MaxTemporaryBytes: 1},
		{MaxEntries: 1, MaxTemporaryBytes: 1},
		{
			MaxBatchUpdates:   maxSupportedCount + 1,
			MaxEntries:        1,
			MaxTemporaryBytes: 1,
		},
		{
			MaxBatchUpdates:   1,
			MaxEntries:        maxSupportedCount + 1,
			MaxTemporaryBytes: 1,
		},
	}
	for _, limits := range invalidLimits {
		if _, err := NewSnapshot(limits); !errors.Is(err, errInvalidLimits) {
			t.Fatalf("new snapshot error = %v, want errInvalidLimits", err)
		}
	}
	if _, err := NewSnapshot(Limits{
		MaxBatchUpdates:   maxSupportedCount,
		MaxEntries:        maxSupportedCount,
		MaxTemporaryBytes: 1,
	}); err != nil {
		t.Fatalf("new maximum-count snapshot: %v", err)
	}

	var zero Snapshot
	if _, _, err := zero.Get(context.Background(), Key{}); !errors.Is(err, errInvalidSnapshot) {
		t.Fatalf("zero get error = %v, want errInvalidSnapshot", err)
	}
	if _, err := zero.Keys(context.Background()); !errors.Is(err, errInvalidSnapshot) {
		t.Fatalf("zero keys error = %v, want errInvalidSnapshot", err)
	}
	if _, err := zero.Apply(context.Background(), nil); !errors.Is(err, errInvalidSnapshot) {
		t.Fatalf("zero apply error = %v, want errInvalidSnapshot", err)
	}
	corrupt := Snapshot{valid: true}
	if _, _, err := corrupt.Get(context.Background(), Key{}); !errors.Is(
		err,
		errInvalidSnapshot,
	) {
		t.Fatalf("corrupt get error = %v, want errInvalidSnapshot", err)
	}
	if err := validateResultCount([]entry{{}}, 0); !errors.Is(
		err,
		errInvalidSnapshot,
	) {
		t.Fatalf("result count error = %v, want errInvalidSnapshot", err)
	}
	corruptOrder := Snapshot{
		limits: Limits{
			MaxBatchUpdates:   1,
			MaxEntries:        2,
			MaxTemporaryBytes: 512,
		},
		entries: []entry{{key: Key{2}}, {key: Key{1}}},
		valid:   true,
	}
	if _, err := corruptOrder.Apply(
		context.Background(),
		[]Update{Delete(Key{2})},
	); !errors.Is(err, errInvalidSnapshot) {
		t.Fatalf("corrupt order error = %v, want errInvalidSnapshot", err)
	}

	snapshot, err := NewSnapshot(validLimits)
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}
	var nilContext context.Context
	if _, _, err := snapshot.Get(nilContext, Key{}); !errors.Is(err, errInvalidContext) {
		t.Fatalf("nil get error = %v, want errInvalidContext", err)
	}
	if _, err := snapshot.Keys(nilContext); !errors.Is(err, errInvalidContext) {
		t.Fatalf("nil keys error = %v, want errInvalidContext", err)
	}
	if _, err := snapshot.Apply(nilContext, nil); !errors.Is(err, errInvalidContext) {
		t.Fatalf("nil apply error = %v, want errInvalidContext", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := snapshot.Get(cancelled, Key{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled get error = %v, want context.Canceled", err)
	}
	if _, err := snapshot.Keys(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled keys error = %v, want context.Canceled", err)
	}
	if _, err := snapshot.Apply(cancelled, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled apply error = %v, want context.Canceled", err)
	}
}

func TestSnapshotAcceptsExactTemporaryBudget(t *testing.T) {
	t.Parallel()

	snapshot, err := NewSnapshot(Limits{
		MaxBatchUpdates:   1,
		MaxEntries:        1,
		MaxTemporaryBytes: workingUpdateBytes + workingEntryBytes,
	})
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}

	snapshot, err = snapshot.Apply(
		context.Background(),
		[]Update{Set(Key{1}, Value{1})},
	)
	if err != nil {
		t.Fatalf("apply at exact temporary budget: %v", err)
	}
	if got, present, err := snapshot.Get(context.Background(), Key{1}); err != nil ||
		!present || got != (Value{1}) {
		t.Fatalf("get = %x, present %t, error %v", got, present, err)
	}
}

func TestSnapshotAccountsForOwnedUpdateAndSortScratch(t *testing.T) {
	t.Parallel()

	snapshot, err := NewSnapshot(Limits{
		MaxBatchUpdates:   1,
		MaxEntries:        1,
		MaxTemporaryBytes: 129,
	})
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}

	_, err = snapshot.Apply(
		context.Background(),
		[]Update{Set(Key{1}, Value{1})},
	)
	var resourceErr *ResourceError
	if !errors.As(err, &resourceErr) ||
		resourceErr.Kind != ResourceTemporaryBytes ||
		resourceErr.Actual != 130 {
		t.Fatalf("apply error = %v, want temporary-byte actual 130", err)
	}
}

func TestSnapshotCountsReplacementAndDeletionIndependently(t *testing.T) {
	t.Parallel()

	limits := Limits{
		MaxBatchUpdates:   2,
		MaxEntries:        2,
		MaxTemporaryBytes: 512,
	}
	snapshot, err := NewSnapshot(limits)
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}
	snapshot, err = snapshot.Apply(context.Background(), []Update{
		Set(Key{1}, Value{1}),
		Set(Key{2}, Value{2}),
	})
	if err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}

	replaced, err := snapshot.Apply(
		context.Background(),
		[]Update{Set(Key{2}, Value{3})},
	)
	if err != nil {
		t.Fatalf("replace existing key: %v", err)
	}
	if got, present, err := replaced.Get(
		context.Background(),
		Key{2},
	); err != nil || !present || got != (Value{3}) {
		t.Fatalf("replacement = %x, present %t, error %v", got, present, err)
	}

	deleted, err := snapshot.Apply(
		context.Background(),
		[]Update{Delete(Key{2})},
	)
	if err != nil {
		t.Fatalf("delete existing key: %v", err)
	}
	if _, present, err := deleted.Get(
		context.Background(),
		Key{2},
	); err != nil || present {
		t.Fatalf("deletion present %t, error %v", present, err)
	}
}

func TestSnapshotStopsDuringBoundedLoops(t *testing.T) {
	t.Parallel()

	snapshot, err := NewSnapshot(Limits{
		MaxBatchUpdates:   2,
		MaxEntries:        2,
		MaxTemporaryBytes: 512,
	})
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}
	snapshot, err = snapshot.Apply(
		context.Background(),
		[]Update{Set(Key{1}, Value{1})},
	)
	if err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}

	keysContext := &stepContext{successfulChecks: 1}
	if _, err := snapshot.Keys(keysContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("keys error = %v, want context.Canceled", err)
	}

	validateContext := &stepContext{successfulChecks: 1}
	if _, err := snapshot.Apply(
		validateContext,
		[]Update{Set(Key{2}, Value{2})},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("validation error = %v, want context.Canceled", err)
	}

	mergeContext := &stepContext{successfulChecks: 2}
	if _, err := snapshot.Apply(
		mergeContext,
		[]Update{Set(Key{2}, Value{2})},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("merge error = %v, want context.Canceled", err)
	}

	sortContext := &stepContext{successfulChecks: 3}
	if _, err := snapshot.Apply(sortContext, []Update{
		Set(Key{2}, Value{2}),
		Set(Key{2}, Value{3}),
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("sort error = %v, want context.Canceled", err)
	}

	completed := false
	for successfulChecks := 0; successfulChecks < 1_000; successfulChecks++ {
		_, err := snapshot.Apply(
			&stepContext{successfulChecks: successfulChecks},
			[]Update{Set(Key{2}, Value{2})},
		)
		if err == nil {
			completed = true
			break
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation after %d checks = %v", successfulChecks, err)
		}
	}
	if !completed {
		t.Fatal("cancellation sweep did not reach a completed apply")
	}
}

func TestEmptyApplyReturnsEquivalentSnapshot(t *testing.T) {
	t.Parallel()

	snapshot, err := NewSnapshot(Limits{
		MaxBatchUpdates:   1,
		MaxEntries:        1,
		MaxTemporaryBytes: 64,
	})
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}

	got, err := snapshot.Apply(context.Background(), nil)
	if err != nil {
		t.Fatalf("empty apply: %v", err)
	}
	keys, err := got.Keys(context.Background())
	if err != nil {
		t.Fatalf("keys: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("keys = %x, want empty", keys)
	}
}

func TestSortUpdatesPreservesEqualKeyOrder(t *testing.T) {
	t.Parallel()

	key := Key{1}
	updates := []Update{
		Set(key, Value{1}),
		Set(key, Value{2}),
	}
	if err := sortUpdates(context.Background(), updates); err != nil {
		t.Fatalf("sort equal-key updates: %v", err)
	}
	if updates[0].value != (Value{1}) || updates[1].value != (Value{2}) {
		t.Fatalf("equal-key update order = %#v, want input order", updates)
	}
}

func TestSortUpdatesHonorsEveryCancellationBoundary(t *testing.T) {
	t.Parallel()

	original := []Update{
		Set(Key{8}, Value{8}),
		Set(Key{7}, Value{7}),
		Set(Key{6}, Value{6}),
		Set(Key{5}, Value{5}),
		Set(Key{4}, Value{4}),
		Set(Key{3}, Value{3}),
		Set(Key{2}, Value{2}),
		Set(Key{1}, Value{1}),
	}
	completed := false
	for successfulChecks := 0; successfulChecks < 1_000; successfulChecks++ {
		got := append([]Update(nil), original...)
		err := sortUpdates(&stepContext{successfulChecks: successfulChecks}, got)
		if err == nil {
			for index := range got {
				if got[index].key != (Key{byte(index + 1)}) {
					t.Fatalf("sorted updates = %#v", got)
				}
			}
			completed = true
			break
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation after %d checks = %v", successfulChecks, err)
		}
	}
	if !completed {
		t.Fatal("cancellation sweep did not reach a completed sort")
	}
}

type stepContext struct {
	successfulChecks int
}

func (ctx *stepContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (ctx *stepContext) Done() <-chan struct{} {
	return nil
}

func (ctx *stepContext) Err() error {
	if ctx.successfulChecks == 0 {
		return context.Canceled
	}

	ctx.successfulChecks--

	return nil
}

func (ctx *stepContext) Value(any) any {
	return nil
}
