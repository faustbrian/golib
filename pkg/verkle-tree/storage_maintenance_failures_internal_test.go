package verkletree

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestMaintainStoragePreservesPlanningAndCancellationFailures(t *testing.T) {
	t.Parallel()

	snapshot := testStorageFacadeSnapshot(t)
	sentinel := errors.New("publication read failed")

	cancelled := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(t, snapshot, nil),
	}
	cancelled.capabilities |= StoreCapabilityAtomicMaintenance
	ctx, cancel := context.WithCancel(context.Background())
	cancelled.cancelAfterCapabilities = cancel
	_, err := MaintainStorage(
		ctx, BandersnatchIPA256V0(), cancelled, nil,
		testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrCancelled) || cancelled.openCalls != 0 {
		t.Fatalf("post-capability cancellation = %v, open calls = %d", err, cancelled.openCalls)
	}

	readFailure := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(t, snapshot, nil),
	}
	readFailure.capabilities |= StoreCapabilityAtomicMaintenance
	readFailure.view.currentErr = sentinel
	_, err = MaintainStorage(
		context.Background(), BandersnatchIPA256V0(), readFailure, nil,
		testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrStorageMaintenance) || !errors.Is(err, ErrStorageAudit) ||
		!errors.Is(err, sentinel) || readFailure.applyCalls != 0 {
		t.Fatalf("publication read failure = %v, apply calls = %d", err, readFailure.applyCalls)
	}

	joined := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(t, snapshot, nil),
	}
	joined.capabilities |= StoreCapabilityAtomicMaintenance
	joined.view.currentErr = sentinel
	joined.view.closeErr = ErrStaleRoot
	_, err = MaintainStorage(
		context.Background(), BandersnatchIPA256V0(), joined, nil,
		testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, sentinel) || !errors.Is(err, ErrStaleRoot) || joined.applyCalls != 0 {
		t.Fatalf("joined plan/close failure = %v, apply calls = %d", err, joined.applyCalls)
	}

	applyCancelled := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(t, snapshot, nil),
		applyErr:           context.DeadlineExceeded,
	}
	applyCancelled.capabilities |= StoreCapabilityAtomicMaintenance
	_, err = MaintainStorage(
		context.Background(), BandersnatchIPA256V0(), applyCancelled, nil,
		testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrStorageMaintenance) || !errors.Is(err, ErrCancelled) ||
		!errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("apply cancellation error = %v", err)
	}
}

func TestMaintainStorageEnforcesPlanningResourceBudgets(t *testing.T) {
	t.Parallel()

	snapshot := testStorageFacadeSnapshot(t)

	nodeBound := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(t, snapshot, nil),
	}
	nodeBound.capabilities |= StoreCapabilityAtomicMaintenance
	limits := testInternalStorageAuditLimits()
	limits.MaxInventoryNodes = 1
	limits.MaxNodeIDsPerPage = 1
	limits.MaxUnreachableNodes = 1
	_, err := MaintainStorage(
		context.Background(), BandersnatchIPA256V0(), nodeBound, nil, limits,
	)
	assertMaintenanceResourceError(t, err, ResourceInventoryNodes, 1, 2)

	temporary := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(t, snapshot, nil),
	}
	temporary.capabilities |= StoreCapabilityAtomicMaintenance
	limits = testInternalStorageAuditLimits()
	limits.MaxTemporaryBytes = storageAuditPublicationBytes +
		2*storageAuditReachableBytes - 1
	_, err = MaintainStorage(
		context.Background(), BandersnatchIPA256V0(), temporary, nil, limits,
	)
	var resourceErr *ResourceError
	if !errors.As(err, &resourceErr) || resourceErr.Resource != ResourceTemporaryBytes ||
		resourceErr.Actual != storageAuditPublicationBytes+2*storageAuditReachableBytes {
		t.Fatalf("reachable temporary budget error = %v", err)
	}

	hidden := make([]StorePublication, 0, 2)
	requestBound := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(t, snapshot, nil),
	}
	requestBound.capabilities |= StoreCapabilityAtomicMaintenance
	limits = testInternalStorageAuditLimits()
	limits.MaxPublications = 1
	_, err = MaintainStorage(
		context.Background(), BandersnatchIPA256V0(), requestBound,
		hidden, limits,
	)
	assertMaintenanceResourceError(t, err, ResourcePublications, 1, 2)
	if requestBound.openCalls != 0 {
		t.Fatalf("request capacity opened audit %d times", requestBound.openCalls)
	}
}

func TestCopyRequestedRetainedChecksBeforeAllocatingAndEachElement(t *testing.T) {
	t.Parallel()

	publication := internalReaderFromSnapshot(t, testStorageFacadeSnapshot(t)).view.publication
	limits := testInternalStorageAuditLimits()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := copyRequestedRetained(
		cancelled, []StorePublication{publication}, limits,
	)
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("initial copy cancellation error = %v", err)
	}

	limits.MaxTemporaryBytes = 2*storageAuditPublicationBytes - 1
	_, _, err = copyRequestedRetained(
		context.Background(), []StorePublication{publication}, limits,
	)
	var resourceErr *ResourceError
	if !errors.As(err, &resourceErr) || resourceErr.Resource != ResourceTemporaryBytes ||
		resourceErr.Actual != 2*storageAuditPublicationBytes {
		t.Fatalf("copy temporary budget error = %v", err)
	}

	copyContext := &auditCancelContext{Context: context.Background(), failAt: 2}
	_, _, err = copyRequestedRetained(
		copyContext, []StorePublication{publication}, testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("element copy cancellation error = %v", err)
	}

	limits = testInternalStorageAuditLimits()
	limits.MaxPublications = 1
	owned, requestedCapacity, err := copyRequestedRetained(
		context.Background(), []StorePublication{publication}, limits,
	)
	if err != nil || len(owned) != 1 || requestedCapacity != 1 {
		t.Fatalf(
			"exact copy boundary = (len %d, capacity %d, %v)",
			len(owned), requestedCapacity, err,
		)
	}
}

func TestMaintainStorageChargesEveryLivePublicationAndReachabilitySlot(t *testing.T) {
	t.Parallel()

	retainedSnapshot, err := NewSnapshot(
		context.Background(), BandersnatchIPA256V0(), nil,
		testFacadeSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	store := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(
			t, retainedSnapshot, []Snapshot{retainedSnapshot},
		),
	}
	store.capabilities |= StoreCapabilityAtomicMaintenance
	if got := len(store.view.nodes); got != 1 {
		t.Fatalf("empty snapshot nodes = %d, want 1", got)
	}
	store.view.current = StorePublication{}
	store.view.currentOK = false
	requested := make([]StorePublication, 1, 2)
	requested[0] = store.view.retained[0]
	limits := testInternalStorageAuditLimits()
	wantActual := uint64(4)*storageAuditPublicationBytes +
		2*storageAuditReachableBytes
	limits.MaxTemporaryBytes = wantActual - 1
	_, err = MaintainStorage(
		context.Background(), BandersnatchIPA256V0(), store,
		requested, limits,
	)
	var resourceErr *ResourceError
	if !errors.As(err, &resourceErr) || resourceErr.Resource != ResourceTemporaryBytes ||
		resourceErr.Actual != wantActual || store.view.nodeIDsCalls != 0 {
		t.Fatalf(
			"publication/reachability budget = %v, inventory calls = %d, want actual %d",
			err, store.view.nodeIDsCalls, wantActual,
		)
	}
}

func TestMaintainStorageChargesDroppedReachabilityUntilFinalRequest(t *testing.T) {
	t.Parallel()

	retainedSnapshot, err := NewSnapshot(
		context.Background(), BandersnatchIPA256V0(), nil,
		testFacadeSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	store := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(
			t, retainedSnapshot, []Snapshot{retainedSnapshot},
		),
	}
	store.capabilities |= StoreCapabilityAtomicMaintenance
	store.view.current = StorePublication{}
	store.view.currentOK = false
	limits := testInternalStorageAuditLimits()
	wantActual := 2*storageAuditPublicationBytes + storageAuditReachableBytes +
		storageAuditNodeIDBytes
	limits.MaxTemporaryBytes = 1100
	_, err = MaintainStorage(
		context.Background(), BandersnatchIPA256V0(), store, nil, limits,
	)
	var resourceErr *ResourceError
	if !errors.As(err, &resourceErr) || resourceErr.Resource != ResourceTemporaryBytes ||
		resourceErr.Actual != wantActual || store.view.nodeIDsCalls != 1 {
		t.Fatalf(
			"dropped reachability budget = %v, inventory calls = %d, want actual %d",
			err, store.view.nodeIDsCalls, wantActual,
		)
	}
}

func TestMaintainStorageChecksFinalRequestAllocationBudget(t *testing.T) {
	t.Parallel()

	snapshotLimits := testFacadeSnapshotLimits()
	snapshotLimits.State.MaxEntries = 32
	snapshotLimits.State.MaxBatchUpdates = 32
	snapshotLimits.Tree.MaxEntries = 32
	snapshotLimits.Tree.MaxStems = 32
	snapshotLimits.Tree.MaxNodes = 128
	snapshotLimits.Tree.MaxEdges = 128
	snapshotLimits.Tree.MaxCommitments = 256
	snapshotLimits.Tree.MaxFieldMappings = 256
	base, err := NewSnapshot(
		context.Background(), BandersnatchIPA256V0(), nil, snapshotLimits,
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	snapshots := []Snapshot{base}
	for index := byte(1); index <= 15; index++ {
		var key Key
		key[0] = 30 + index
		next, _, err := snapshots[len(snapshots)-1].Apply(
			context.Background(), []Update{Set(key, Value{index})},
		)
		if err != nil {
			t.Fatalf("Apply(%d) error = %v", index, err)
		}
		snapshots = append(snapshots, next)
	}
	current := snapshots[len(snapshots)-1]
	retained := snapshots[:len(snapshots)-1]
	limits := testInternalStorageAuditLimits()
	limits.MaxPublications = 16
	limits.MaxInventoryNodes = 512
	limits.MaxUnreachableNodes = 256
	limits.MaxNodeIDsPerPage = 1
	limits.Read.Snapshot = snapshotLimits
	store := newInternalAuditStore(t, current, retained)
	maintenance, err := planStorageMaintenance(
		context.Background(), BandersnatchIPA256V0(), store.view, nil, 0, limits,
	)
	if err != nil {
		t.Fatalf("initial planStorageMaintenance() error = %v", err)
	}
	keptNodes := len(internalReaderFromSnapshot(t, current).view.nodes)
	allReachableNodes := len(store.view.nodes)
	publicationSlots := len(snapshots)
	finalBytes := storageAuditTemporaryBytes(
		uint64(publicationSlots)+uint64(len(retained)),
		uint64(allReachableNodes)+uint64(keptNodes),
		uint64(cap(maintenance.deleted)),
		0,
	)
	if finalBytes == 0 {
		t.Fatal("final request accounting is zero")
	}

	bounded := newInternalAuditStore(t, current, retained)
	limits.MaxTemporaryBytes = finalBytes - 1
	_, err = planStorageMaintenance(
		context.Background(), BandersnatchIPA256V0(), bounded.view, nil, 0, limits,
	)
	var resourceErr *ResourceError
	if !errors.As(err, &resourceErr) || resourceErr.Resource != ResourceTemporaryBytes ||
		resourceErr.Actual != finalBytes || bounded.view.nodeIDsCalls != len(bounded.view.nodes) {
		t.Fatalf(
			"final request budget = %v, inventory calls = %d/%d, want actual %d",
			err, bounded.view.nodeIDsCalls, len(bounded.view.nodes), finalBytes,
		)
	}
}

func TestNormalizeRetainedPublicationsChecksBeforeCopying(t *testing.T) {
	t.Parallel()

	publication := internalReaderFromSnapshot(t, testStorageFacadeSnapshot(t)).view.publication
	limits := testInternalStorageAuditLimits()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := normalizeRetainedPublications(
		ctx, []StorePublication{publication}, 1, StorePublication{}, false,
		[]StorePublication{publication}, limits,
	)
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("normalization cancellation error = %v", err)
	}

	limits.MaxPublications = 1
	_, err = normalizeRetainedPublications(
		context.Background(), nil, 2, StorePublication{}, false, nil, limits,
	)
	var publicationErr *ResourceError
	if !errors.As(err, &publicationErr) || publicationErr.Resource != ResourcePublications ||
		publicationErr.Actual != 2 {
		t.Fatalf("normalization publication bound error = %v", err)
	}
	limits = testInternalStorageAuditLimits()
	_, err = normalizeRetainedPublications(
		context.Background(), []StorePublication{{}}, 1, StorePublication{}, false,
		nil, limits,
	)
	if !errors.Is(err, ErrInvalidRetention) {
		t.Fatalf("normalization malformed publication error = %v", err)
	}

	limits.MaxTemporaryBytes = 4*storageAuditPublicationBytes - 1
	_, err = normalizeRetainedPublications(
		context.Background(), []StorePublication{publication}, 1, StorePublication{}, false,
		[]StorePublication{publication}, limits,
	)
	var resourceErr *ResourceError
	if !errors.As(err, &resourceErr) || resourceErr.Resource != ResourceTemporaryBytes ||
		resourceErr.Actual != 4*storageAuditPublicationBytes {
		t.Fatalf("normalization temporary error = %v", err)
	}

	oldSnapshot := testStorageFacadeSnapshot(t)
	middleSnapshot := testStorageFacadeSnapshotWithKey(t, 44)
	observedStore := newInternalAuditStore(
		t, testStorageFacadeSnapshotWithKey(t, 45),
		[]Snapshot{oldSnapshot, middleSnapshot},
	)
	observed := observedStore.view.retained
	sortCtx := &auditCancelContext{Context: context.Background(), failAt: 3}
	_, err = normalizeRetainedPublications(
		sortCtx,
		[]StorePublication{observed[1], observed[0]},
		2,
		StorePublication{},
		false,
		observed,
		testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("normalization sort cancellation error = %v", err)
	}
	postSortCtx := &auditCancelContext{Context: context.Background(), failAt: 4}
	_, err = normalizeRetainedPublications(
		postSortCtx,
		[]StorePublication{publication},
		1,
		StorePublication{},
		false,
		[]StorePublication{publication},
		testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("post-sort cancellation error = %v", err)
	}
}

func TestNormalizeRetainedPublicationsAcceptsExactCountAndCapacityLimit(t *testing.T) {
	t.Parallel()

	publication := internalReaderFromSnapshot(t, testStorageFacadeSnapshot(t)).view.publication
	requested := make([]StorePublication, 1)
	requested[0] = publication
	limits := testInternalStorageAuditLimits()
	limits.MaxPublications = 1
	retained, err := normalizeRetainedPublications(
		context.Background(), requested, 1, StorePublication{}, false,
		[]StorePublication{publication}, limits,
	)
	if err != nil || !slices.Equal(retained, requested) {
		t.Fatalf("exact publication boundary = (%+v, %v)", retained, err)
	}
}

func TestNormalizeRetainedPublicationsChargesCurrentPublication(t *testing.T) {
	t.Parallel()

	current := internalReaderFromSnapshot(
		t, testStorageFacadeSnapshotWithKey(t, 54),
	).view.publication
	retained := internalReaderFromSnapshot(
		t, testStorageFacadeSnapshot(t),
	).view.publication
	limits := testInternalStorageAuditLimits()
	wantActual := uint64(5) * storageAuditPublicationBytes
	limits.MaxTemporaryBytes = wantActual - 1
	_, err := normalizeRetainedPublications(
		context.Background(), []StorePublication{retained}, 1, current, true,
		[]StorePublication{retained}, limits,
	)
	var resourceErr *ResourceError
	if !errors.As(err, &resourceErr) || resourceErr.Resource != ResourceTemporaryBytes ||
		resourceErr.Actual != wantActual {
		t.Fatalf("current-publication temporary budget = %v, want actual %d", err, wantActual)
	}
}

func TestStorePublicationMergeSortCanonicalizesMultipleValuesAndRejectsDuplicates(t *testing.T) {
	t.Parallel()

	retainedSnapshots := []Snapshot{
		testStorageFacadeSnapshot(t),
		testStorageFacadeSnapshotWithKey(t, 50),
		testStorageFacadeSnapshotWithKey(t, 51),
		testStorageFacadeSnapshotWithKey(t, 52),
	}
	store := newInternalAuditStore(
		t, testStorageFacadeSnapshotWithKey(t, 53), retainedSnapshots,
	)
	want := store.view.retained
	values := []StorePublication{want[3], want[2], want[1], want[0]}
	if err := sortStorePublications(context.Background(), values); err != nil ||
		!slices.Equal(values, want) {
		t.Fatalf("canonical sort = (%+v, %v), want %+v", values, err, want)
	}
	already := cloneStorePublications(want)
	if err := sortStorePublications(context.Background(), already); err != nil ||
		!slices.Equal(already, want) {
		t.Fatalf("already canonical sort = (%+v, %v), want %+v", already, err, want)
	}
	duplicates := []StorePublication{want[0], want[0]}
	if err := sortStorePublications(
		context.Background(), duplicates,
	); !errors.Is(err, ErrInvalidRetention) {
		t.Fatalf("duplicate sort error = %v", err)
	}
}

func TestStorePublicationMergeSortObservesEveryCancellationPhase(t *testing.T) {
	t.Parallel()

	store := newInternalAuditStore(
		t,
		testStorageFacadeSnapshotWithKey(t, 47),
		[]Snapshot{testStorageFacadeSnapshot(t), testStorageFacadeSnapshotWithKey(t, 46)},
	)
	for _, failAt := range []int{1, 2, 3, 4, 6} {
		publications := []StorePublication{store.view.retained[1], store.view.retained[0]}
		scratch := make([]StorePublication, len(publications))
		ctx := &auditCancelContext{Context: context.Background(), failAt: failAt}
		if err := mergeSortStorePublications(
			ctx, publications, scratch, 0, len(publications),
		); !errors.Is(err, ErrCancelled) {
			t.Fatalf("failAt %d error = %v", failAt, err)
		}
	}
}

func TestMaintenanceInventoryRejectsEveryMalformedBoundary(t *testing.T) {
	t.Parallel()

	baseLimits := testInternalStorageAuditLimits()
	first := NodeID{1}
	second := NodeID{2}
	sentinel := errors.New("inventory unavailable")
	afterPageCtx, afterPageCancel := context.WithCancel(context.Background())

	tests := map[string]struct {
		ctx    context.Context
		view   *internalAuditSnapshot
		limits StorageAuditLimits
		all    map[NodeID]struct{}
		want   error
	}{
		"cancelled before page": {
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			}(),
			view: &internalAuditSnapshot{}, limits: baseLimits,
			all: map[NodeID]struct{}{}, want: ErrCancelled,
		},
		"page count": {
			ctx: context.Background(),
			view: &internalAuditSnapshot{nodeIDsFn: func(*NodeID, uint32) ([]NodeID, bool, error) {
				return []NodeID{first}, true, nil
			}},
			limits: func() StorageAuditLimits {
				limits := baseLimits
				limits.MaxInventoryPages = 1
				return limits
			}(),
			all: map[NodeID]struct{}{}, want: ErrResourceExhausted,
		},
		"node count": {
			ctx: context.Background(),
			view: &internalAuditSnapshot{nodeIDsFn: func(*NodeID, uint32) ([]NodeID, bool, error) {
				return []NodeID{first}, true, nil
			}},
			limits: func() StorageAuditLimits {
				limits := baseLimits
				limits.MaxInventoryNodes = 1
				limits.MaxNodeIDsPerPage = 1
				return limits
			}(),
			all: map[NodeID]struct{}{}, want: ErrResourceExhausted,
		},
		"page memory": {
			ctx: context.Background(), view: &internalAuditSnapshot{},
			limits: func() StorageAuditLimits {
				limits := baseLimits
				limits.MaxTemporaryBytes = 1
				return limits
			}(),
			all: map[NodeID]struct{}{}, want: ErrResourceExhausted,
		},
		"adapter failure": {
			ctx: context.Background(), view: &internalAuditSnapshot{nodeIDsErr: sentinel},
			limits: baseLimits, all: map[NodeID]struct{}{}, want: sentinel,
		},
		"cancelled after page": {
			ctx: afterPageCtx,
			view: &internalAuditSnapshot{nodeIDsFn: func(*NodeID, uint32) ([]NodeID, bool, error) {
				afterPageCancel()
				return nil, false, nil
			}},
			limits: baseLimits, all: map[NodeID]struct{}{}, want: ErrCancelled,
		},
		"hidden page capacity": {
			ctx: context.Background(), view: &internalAuditSnapshot{nodeIDsFn: func(*NodeID, uint32) ([]NodeID, bool, error) {
				ids := make([]NodeID, 1, 2)
				ids[0] = first
				return ids, false, nil
			}},
			limits: func() StorageAuditLimits {
				limits := baseLimits
				limits.MaxNodeIDsPerPage = 1
				return limits
			}(),
			all: map[NodeID]struct{}{}, want: ErrStorageInventory,
		},
		"cancelled within page": {
			ctx: &auditCancelContext{Context: context.Background(), failAt: 3},
			view: &internalAuditSnapshot{nodeIDsFn: func(*NodeID, uint32) ([]NodeID, bool, error) {
				return []NodeID{first}, false, nil
			}},
			limits: baseLimits, all: map[NodeID]struct{}{}, want: ErrCancelled,
		},
		"reordered page": {
			ctx: context.Background(), view: &internalAuditSnapshot{nodeIDsFn: func(*NodeID, uint32) ([]NodeID, bool, error) {
				return []NodeID{second, first}, false, nil
			}},
			limits: baseLimits, all: map[NodeID]struct{}{}, want: ErrStorageInventory,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, err := maintenanceInventory(
				test.ctx, test.view, test.limits, 0, test.all, map[NodeID]struct{}{},
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("maintenanceInventory() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestMaintenanceInventoryEnforcesExactPageMemoryAndDeletionBoundaries(t *testing.T) {
	t.Parallel()

	first := NodeID{1}
	limits := testInternalStorageAuditLimits()
	limits.MaxInventoryPages = 1
	deleted, count, err := maintenanceInventory(
		context.Background(),
		&internalAuditSnapshot{nodeIDsFn: func(*NodeID, uint32) ([]NodeID, bool, error) {
			return nil, false, nil
		}},
		limits,
		0,
		map[NodeID]struct{}{},
		map[NodeID]struct{}{},
	)
	if err != nil || count != 0 || len(deleted) != 0 {
		t.Fatalf("exact page limit = (%x, %d, %v)", deleted, count, err)
	}

	limits = testInternalStorageAuditLimits()
	limits.MaxInventoryNodes = 1
	limits.MaxNodeIDsPerPage = 1
	view := &internalAuditSnapshot{nodeIDsFn: func(*NodeID, uint32) ([]NodeID, bool, error) {
		return []NodeID{first}, true, nil
	}}
	_, _, err = maintenanceInventory(
		context.Background(), view, limits, 0,
		map[NodeID]struct{}{}, map[NodeID]struct{}{},
	)
	assertMaintenanceResourceError(t, err, ResourceInventoryNodes, 1, 2)

	limits = testInternalStorageAuditLimits()
	wantTemporary := 2*storageAuditReachableBytes + 2*storageAuditNodeIDBytes
	limits.MaxTemporaryBytes = wantTemporary - 1
	view = &internalAuditSnapshot{}
	_, _, err = maintenanceInventory(
		context.Background(), view, limits, 0,
		map[NodeID]struct{}{first: {}}, map[NodeID]struct{}{first: {}},
	)
	var resourceErr *ResourceError
	if !errors.As(err, &resourceErr) || resourceErr.Resource != ResourceTemporaryBytes ||
		resourceErr.Actual != wantTemporary || view.nodeIDsCalls != 0 {
		t.Fatalf("inventory page memory = %v, calls = %d", err, view.nodeIDsCalls)
	}

	limits = testInternalStorageAuditLimits()
	limits.MaxUnreachableNodes = 1
	deleted, count, err = maintenanceInventory(
		context.Background(),
		&internalAuditSnapshot{nodeIDsFn: func(*NodeID, uint32) ([]NodeID, bool, error) {
			return []NodeID{first}, false, nil
		}},
		limits,
		0,
		map[NodeID]struct{}{},
		map[NodeID]struct{}{},
	)
	if err != nil || count != 1 || !slices.Equal(deleted, []NodeID{first}) {
		t.Fatalf("exact deletion limit = (%x, %d, %v)", deleted, count, err)
	}
}

func TestMaintenanceInventoryRejectsDuplicateIDsWithinAndAcrossPages(t *testing.T) {
	t.Parallel()

	first := NodeID{1}
	limits := testInternalStorageAuditLimits()
	for name, view := range map[string]*internalAuditSnapshot{
		"within page": {nodeIDsFn: func(*NodeID, uint32) ([]NodeID, bool, error) {
			return []NodeID{first, first}, false, nil
		}},
		"across pages": {nodeIDsFn: func(after *NodeID, _ uint32) ([]NodeID, bool, error) {
			if after == nil {
				return []NodeID{first}, true, nil
			}
			return []NodeID{first}, false, nil
		}},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := maintenanceInventory(
				context.Background(), view, limits, 0,
				map[NodeID]struct{}{}, map[NodeID]struct{}{},
			)
			if !errors.Is(err, ErrStorageInventory) {
				t.Fatalf("duplicate inventory error = %v", err)
			}
		})
	}
}

func TestStoreMaintenanceAccessorsPreserveValidationCancellationAndOwnership(t *testing.T) {
	t.Parallel()

	publication := internalReaderFromSnapshot(t, testStorageFacadeSnapshot(t)).view.publication
	profile := BandersnatchIPA256V0()
	invalidFlag := StoreMaintenance{profile: profile}
	if _, err := invalidFlag.Profile(); !errors.Is(err, ErrStorageMaintenance) {
		t.Fatalf("invalid flag error = %v", err)
	}
	invalidProfile := StoreMaintenance{valid: true}
	if _, err := invalidProfile.Profile(); !errors.Is(err, ErrStorageMaintenance) {
		t.Fatalf("invalid profile error = %v", err)
	}
	invalidCurrent := StoreMaintenance{profile: profile, hasCurrent: true, valid: true}
	if _, _, err := invalidCurrent.CurrentPublication(); !errors.Is(err, ErrStorageMaintenance) {
		t.Fatalf("invalid current error = %v", err)
	}

	maintenance := StoreMaintenance{
		profile:          profile,
		previousRetained: []StorePublication{publication, publication},
		retained:         []StorePublication{publication, publication},
		deleted:          []NodeID{{1}, {2}},
		valid:            true,
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := maintenance.PreviousRetainedPublications(cancelled); !errors.Is(err, ErrCancelled) {
		t.Fatalf("initial publication cancellation = %v", err)
	}
	if _, err := maintenance.DeletedNodes(cancelled); !errors.Is(err, ErrCancelled) {
		t.Fatalf("initial node cancellation = %v", err)
	}

	publicationCtx := &auditCancelContext{Context: context.Background(), failAt: 2}
	if _, err := maintenance.RetainedPublications(publicationCtx); !errors.Is(err, ErrCancelled) {
		t.Fatalf("publication-copy cancellation = %v", err)
	}
	nodeCtx := &auditCancelContext{Context: context.Background(), failAt: 2}
	if _, err := maintenance.DeletedNodes(nodeCtx); !errors.Is(err, ErrCancelled) {
		t.Fatalf("node-copy cancellation = %v", err)
	}

	previous, err := maintenance.PreviousRetainedPublications(context.Background())
	if err != nil || !slices.Equal(previous, maintenance.previousRetained) {
		t.Fatalf("owned previous publications = (%+v, %v)", previous, err)
	}
}

func TestMaintenanceOrderingHelpersCoverEqualAndGreaterNodeIDs(t *testing.T) {
	t.Parallel()

	first := NodeID{1}
	second := NodeID{2}
	if compareNodeID(second, first) != 1 || compareNodeID(first, first) != 0 {
		t.Fatal("node identifier comparison is not total")
	}
}

func assertMaintenanceResourceError(
	t testing.TB,
	err error,
	resource Resource,
	limit uint64,
	actual uint64,
) {
	t.Helper()

	var resourceErr *ResourceError
	if !errors.As(err, &resourceErr) || resourceErr.Resource != resource ||
		resourceErr.Limit != limit || resourceErr.Actual != actual {
		t.Fatalf("resource error = %v, want resource=%d limit=%d actual=%d", err, resource, limit, actual)
	}
}
