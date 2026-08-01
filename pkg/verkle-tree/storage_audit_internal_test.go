package verkletree

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"slices"
	"sort"
	"testing"
)

func TestAuditStorageFindsOnlyNodesOutsideCurrentAndRetainedSnapshots(t *testing.T) {
	t.Parallel()

	oldSnapshot := testStorageFacadeSnapshot(t)
	var addedKey Key
	addedKey[0] = 9
	addedKey[31] = 9
	currentSnapshot, _, err := oldSnapshot.Apply(
		context.Background(),
		[]Update{Set(addedKey, Value{9})},
	)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	store := newInternalAuditStore(t, currentSnapshot, []Snapshot{oldSnapshot})
	orphan := NodeID{0xff}
	store.view.nodes[orphan] = []byte("interrupted unpublished write")

	audit, err := AuditStorage(
		context.Background(),
		ExperimentalBandersnatchIPA256V0(),
		store,
		testInternalStorageAuditLimits(),
	)
	if err != nil {
		t.Fatalf("AuditStorage() error = %v", err)
	}
	if got, want := audit.PublicationCount(), uint32(2); got != want {
		t.Fatalf("PublicationCount() = %d, want %d", got, want)
	}
	if got, want := audit.InventoryNodeCount(), uint32(len(store.view.nodes)); got != want {
		t.Fatalf("InventoryNodeCount() = %d, want %d", got, want)
	}
	if audit.ReachableNodeCount()+audit.UnreachableNodeCount() != audit.InventoryNodeCount() {
		t.Fatalf(
			"node counts reachable=%d unreachable=%d inventory=%d",
			audit.ReachableNodeCount(),
			audit.UnreachableNodeCount(),
			audit.InventoryNodeCount(),
		)
	}
	if got, want := audit.UnreachableNodeCount(), uint32(1); got != want {
		t.Fatalf("UnreachableNodeCount() = %d, want %d", got, want)
	}
	unreachable, err := audit.UnreachableNodes(context.Background())
	if err != nil || !slices.Equal(unreachable, []NodeID{orphan}) {
		t.Fatalf("UnreachableNodes() = (%x, %v), want %x", unreachable, err, orphan)
	}
	unreachable[0] = NodeID{}
	again, err := audit.UnreachableNodes(context.Background())
	if err != nil || again[0] != orphan {
		t.Fatalf("UnreachableNodes() aliases report: (%x, %v)", again, err)
	}
	if store.openCalls != 1 || store.view.closeCalls != 1 {
		t.Fatalf("audit lifecycle open=%d close=%d", store.openCalls, store.view.closeCalls)
	}
}

func TestAuditStorageRejectsInventoryThatOmitsReachableNode(t *testing.T) {
	t.Parallel()

	snapshot := testStorageFacadeSnapshot(t)
	store := newInternalAuditStore(t, snapshot, nil)
	store.view.omitLast = true

	audit, err := AuditStorage(
		context.Background(),
		ExperimentalBandersnatchIPA256V0(),
		store,
		testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrStorageInventory) {
		t.Fatalf("AuditStorage() error = %v, want %v", err, ErrStorageInventory)
	}
	if audit.valid || store.view.closeCalls != 1 {
		t.Fatalf("failed audit = (%+v), close calls = %d", audit, store.view.closeCalls)
	}
}

func TestAuditStorageRejectsInvalidInputsBeforeOpeningStore(t *testing.T) {
	t.Parallel()

	valid := newInternalAuditStore(t, testStorageFacadeSnapshot(t), nil)
	var nilContext context.Context
	var nilStore *internalAuditStore
	tests := map[string]struct {
		ctx     context.Context
		profile Profile
		store   NodeAuditStore
		limits  StorageAuditLimits
		want    error
	}{
		"nil context": {
			ctx: nilContext, profile: ExperimentalBandersnatchIPA256V0(),
			store: valid, limits: testInternalStorageAuditLimits(), want: ErrInvalidContext,
		},
		"invalid profile": {
			ctx: context.Background(), store: valid,
			limits: testInternalStorageAuditLimits(), want: ErrUnsupportedProfile,
		},
		"nil store": {
			ctx: context.Background(), profile: ExperimentalBandersnatchIPA256V0(),
			limits: testInternalStorageAuditLimits(), want: ErrInvalidStore,
		},
		"typed nil store": {
			ctx: context.Background(), profile: ExperimentalBandersnatchIPA256V0(),
			store: nilStore, limits: testInternalStorageAuditLimits(), want: ErrInvalidStore,
		},
		"invalid limits": {
			ctx: context.Background(), profile: ExperimentalBandersnatchIPA256V0(),
			store: valid, want: ErrInvalidLimits,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			before := valid.openCalls
			_, err := AuditStorage(test.ctx, test.profile, test.store, test.limits)
			if !errors.Is(err, test.want) {
				t.Fatalf("AuditStorage() error = %v, want %v", err, test.want)
			}
			if valid.openCalls != before {
				t.Fatalf("open calls changed from %d to %d", before, valid.openCalls)
			}
		})
	}

	for _, omitted := range []StoreCapabilities{
		StoreCapabilityImmutableNodes,
		StoreCapabilitySnapshotReads,
		StoreCapabilityNodeInventory,
	} {
		missing := newInternalAuditStore(t, testStorageFacadeSnapshot(t), nil)
		missing.capabilities &^= omitted
		_, err := AuditStorage(
			context.Background(), ExperimentalBandersnatchIPA256V0(),
			missing, testInternalStorageAuditLimits(),
		)
		var capabilityErr *StoreCapabilityError
		if !errors.As(err, &capabilityErr) ||
			capabilityErr.Missing != omitted || missing.openCalls != 0 {
			t.Fatalf(
				"omitted capability %d rejection = %v, open calls = %d",
				omitted, err, missing.openCalls,
			)
		}
	}

	cancelledAfterCapabilities := newInternalAuditStore(
		t, testStorageFacadeSnapshot(t), nil,
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancelledAfterCapabilities.cancelAfterCapabilities = cancel
	_, err := AuditStorage(
		ctx, ExperimentalBandersnatchIPA256V0(),
		cancelledAfterCapabilities, testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrCancelled) || cancelledAfterCapabilities.openCalls != 0 {
		t.Fatalf(
			"post-capability cancellation = %v, open calls = %d",
			err, cancelledAfterCapabilities.openCalls,
		)
	}
}

func TestStorageAuditLimitsRejectEveryInvalidField(t *testing.T) {
	t.Parallel()

	invalid := map[string]func(*StorageAuditLimits){
		"publications zero":      func(value *StorageAuditLimits) { value.MaxPublications = 0 },
		"publications excessive": func(value *StorageAuditLimits) { value.MaxPublications = maxPublicCount + 1 },
		"inventory zero":         func(value *StorageAuditLimits) { value.MaxInventoryNodes = 0 },
		"inventory excessive":    func(value *StorageAuditLimits) { value.MaxInventoryNodes = maxPublicCount + 1 },
		"page zero":              func(value *StorageAuditLimits) { value.MaxNodeIDsPerPage = 0 },
		"page over inventory":    func(value *StorageAuditLimits) { value.MaxNodeIDsPerPage = value.MaxInventoryNodes + 1 },
		"pages zero":             func(value *StorageAuditLimits) { value.MaxInventoryPages = 0 },
		"pages excessive":        func(value *StorageAuditLimits) { value.MaxInventoryPages = maxPublicCount + 1 },
		"unreachable excessive":  func(value *StorageAuditLimits) { value.MaxUnreachableNodes = value.MaxInventoryNodes + 1 },
		"temporary zero":         func(value *StorageAuditLimits) { value.MaxTemporaryBytes = 0 },
		"read invalid":           func(value *StorageAuditLimits) { value.Read = StorageReadLimits{} },
	}
	for name, mutate := range invalid {
		t.Run(name, func(t *testing.T) {
			limits := testInternalStorageAuditLimits()
			mutate(&limits)
			if err := limits.validate(); !errors.Is(err, ErrInvalidLimits) {
				t.Fatalf("validate() error = %v", err)
			}
		})
	}
	boundary := testInternalStorageAuditLimits()
	boundary.MaxPublications = maxPublicCount
	boundary.MaxInventoryNodes = maxPublicCount
	boundary.MaxInventoryPages = maxPublicCount
	boundary.MaxUnreachableNodes = 0
	if err := boundary.validate(); err != nil {
		t.Fatalf("zero unreachable limit error = %v", err)
	}
}

func TestAuditStoragePreservesLifecycleAndReachableReadFailures(t *testing.T) {
	t.Parallel()

	snapshot := testStorageFacadeSnapshot(t)
	sentinel := errors.New("adapter failure")

	openFailure := newInternalAuditStore(t, snapshot, nil)
	openFailure.openErr = context.DeadlineExceeded
	_, err := AuditStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(),
		openFailure, testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrStorageAudit) || !errors.Is(err, ErrCancelled) {
		t.Fatalf("open failure = %v", err)
	}

	nilView := newInternalAuditStore(t, snapshot, nil)
	nilView.returnNil = true
	_, err = AuditStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(),
		nilView, testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrStorageAudit) {
		t.Fatalf("nil view error = %v", err)
	}

	publicationFailure := newInternalAuditStore(t, snapshot, nil)
	publicationFailure.view.currentErr = sentinel
	publicationFailure.view.closeErr = context.Canceled
	_, err = AuditStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(),
		publicationFailure, testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrStorageAudit) ||
		!errors.Is(err, sentinel) ||
		!errors.Is(err, ErrCancelled) ||
		publicationFailure.view.closeCalls != 1 {
		t.Fatalf("publication/close failure = %v", err)
	}

	readFailure := newInternalAuditStore(t, snapshot, nil)
	readFailure.view.readErr = sentinel
	_, err = AuditStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(),
		readFailure, testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrStorageAudit) || !errors.Is(err, sentinel) {
		t.Fatalf("reachable read failure = %v", err)
	}

	closeFailure := newInternalAuditStore(t, snapshot, nil)
	closeFailure.view.closeErr = sentinel
	audit, err := AuditStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(),
		closeFailure, testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrStorageAudit) || !errors.Is(err, sentinel) || audit.valid {
		t.Fatalf("close failure = (%+v, %v)", audit, err)
	}
}

func TestAuditStorageRejectsMalformedPublicationSets(t *testing.T) {
	t.Parallel()

	snapshot := testStorageFacadeSnapshot(t)
	tests := map[string]func(*internalAuditStore){
		"invalid current": func(store *internalAuditStore) {
			store.view.current = StorePublication{}
		},
		"retained failure": func(store *internalAuditStore) {
			store.view.retainedErr = errors.New("retention unavailable")
		},
		"invalid retained": func(store *internalAuditStore) {
			store.view.retained = []StorePublication{{}}
		},
		"duplicate current": func(store *internalAuditStore) {
			store.view.retained = []StorePublication{store.view.current}
		},
		"too many": func(store *internalAuditStore) {
			store.view.retained = make([]StorePublication, 8)
			for index := range store.view.retained {
				store.view.retained[index] = store.view.current
			}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			store := newInternalAuditStore(t, snapshot, nil)
			mutate(store)
			limits := testInternalStorageAuditLimits()
			if name == "too many" {
				limits.MaxPublications = 1
			}
			_, err := AuditStorage(
				context.Background(), ExperimentalBandersnatchIPA256V0(), store, limits,
			)
			if name == "retained failure" {
				if !errors.Is(err, ErrStorageAudit) {
					t.Fatalf("AuditStorage() error = %v", err)
				}
			} else if name == "too many" {
				assertAuditResourceError(t, err, ResourcePublications, 1, 9)
			} else if !errors.Is(err, ErrStorageInventory) {
				t.Fatalf("AuditStorage() error = %v, want inventory failure", err)
			}
		})
	}
}

func TestAuditStorageRejectsMalformedAndExhaustedInventoryPages(t *testing.T) {
	t.Parallel()

	snapshot := testStorageFacadeSnapshot(t)
	tests := map[string]struct {
		configure func(*internalAuditStore, *StorageAuditLimits)
		want      error
		resource  Resource
	}{
		"adapter failure": {
			configure: func(store *internalAuditStore, _ *StorageAuditLimits) {
				store.view.nodeIDsErr = context.Canceled
			},
			want: ErrStorageAudit,
		},
		"oversized page": {
			configure: func(store *internalAuditStore, _ *StorageAuditLimits) {
				store.view.nodeIDsFn = func(_ *NodeID, max uint32) ([]NodeID, bool, error) {
					return make([]NodeID, max+1), false, nil
				}
			},
			want: ErrStorageInventory,
		},
		"empty continuation": {
			configure: func(store *internalAuditStore, _ *StorageAuditLimits) {
				store.view.nodeIDsFn = func(*NodeID, uint32) ([]NodeID, bool, error) {
					return nil, true, nil
				}
			},
			want: ErrStorageInventory,
		},
		"unordered page": {
			configure: func(store *internalAuditStore, _ *StorageAuditLimits) {
				store.view.nodeIDsFn = func(*NodeID, uint32) ([]NodeID, bool, error) {
					return []NodeID{{2}, {1}}, false, nil
				}
			},
			want: ErrStorageInventory,
		},
		"repeated cursor": {
			configure: func(store *internalAuditStore, _ *StorageAuditLimits) {
				calls := 0
				store.view.nodeIDsFn = func(*NodeID, uint32) ([]NodeID, bool, error) {
					calls++
					return []NodeID{{1}}, calls == 1, nil
				}
			},
			want: ErrStorageInventory,
		},
		"page budget": {
			configure: func(store *internalAuditStore, limits *StorageAuditLimits) {
				limits.MaxInventoryPages = 1
				store.view.nodeIDsFn = func(*NodeID, uint32) ([]NodeID, bool, error) {
					return []NodeID{{1}}, true, nil
				}
			},
			resource: ResourceInventoryPages,
		},
		"node budget": {
			configure: func(store *internalAuditStore, limits *StorageAuditLimits) {
				limits.MaxInventoryNodes = 1
				limits.MaxNodeIDsPerPage = 1
				limits.MaxUnreachableNodes = 1
				store.view.nodeIDsFn = func(*NodeID, uint32) ([]NodeID, bool, error) {
					return []NodeID{{1}}, true, nil
				}
			},
			resource: ResourceInventoryNodes,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			store := newInternalAuditStore(t, snapshot, nil)
			limits := testInternalStorageAuditLimits()
			test.configure(store, &limits)
			_, err := AuditStorage(
				context.Background(), ExperimentalBandersnatchIPA256V0(), store, limits,
			)
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("AuditStorage() error = %v, want %v", err, test.want)
			}
			if test.resource != 0 {
				var resourceErr *ResourceError
				if !errors.As(err, &resourceErr) || resourceErr.Resource != test.resource {
					t.Fatalf("AuditStorage() error = %v, want resource %d", err, test.resource)
				}
			}
		})
	}
}

func TestAuditStorageSupportsEmptyPublicationSetAndBoundsUnreachableNodes(t *testing.T) {
	t.Parallel()

	store := newInternalAuditStore(t, testStorageFacadeSnapshot(t), nil)
	store.view.currentOK = false
	store.view.current = StorePublication{}
	store.view.retained = nil
	store.view.nodes = map[NodeID][]byte{{1}: {1}, {2}: {2}}

	audit, err := AuditStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(),
		store, testInternalStorageAuditLimits(),
	)
	if err != nil || audit.PublicationCount() != 0 ||
		audit.ReachableNodeCount() != 0 || audit.UnreachableNodeCount() != 2 {
		t.Fatalf("empty publication audit = (%+v, %v)", audit, err)
	}

	limited := newInternalAuditStore(t, testStorageFacadeSnapshot(t), nil)
	limited.view.nodes[NodeID{0xff}] = []byte{1}
	limits := testInternalStorageAuditLimits()
	limits.MaxUnreachableNodes = 0
	_, err = AuditStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(), limited, limits,
	)
	assertAuditResourceError(t, err, ResourceUnreachableNodes, 0, 1)
}

func TestStorageAuditZeroValueAndCancellationFailClosed(t *testing.T) {
	t.Parallel()

	var audit StorageAudit
	if audit.PublicationCount() != 0 || audit.ReachableNodeCount() != 0 ||
		audit.InventoryNodeCount() != 0 || audit.UnreachableNodeCount() != 0 {
		t.Fatal("zero audit exposed counts")
	}
	if _, err := audit.UnreachableNodes(context.Background()); !errors.Is(err, ErrStorageAudit) {
		t.Fatalf("zero UnreachableNodes() error = %v", err)
	}
	valid := StorageAudit{valid: true, unreachable: []NodeID{{1}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := valid.UnreachableNodes(ctx); !errors.Is(err, ErrCancelled) {
		t.Fatalf("cancelled UnreachableNodes() error = %v", err)
	}
	midCopy := StorageAudit{valid: true, unreachable: []NodeID{{1}, {2}}}
	if _, err := midCopy.UnreachableNodes(&auditCancelContext{
		Context: context.Background(), failAt: 2,
	}); !errors.Is(err, ErrCancelled) {
		t.Fatalf("mid-copy cancellation error = %v", err)
	}
}

func TestAuditStorageRejectsCancellationAfterEmptyInventoryRead(t *testing.T) {
	t.Parallel()

	store := newInternalAuditStore(t, testStorageFacadeSnapshot(t), nil)
	store.view.current = StorePublication{}
	store.view.currentOK = false
	store.view.nodes = map[NodeID][]byte{}
	ctx, cancel := context.WithCancel(context.Background())
	store.view.nodeIDsFn = func(*NodeID, uint32) ([]NodeID, bool, error) {
		cancel()

		return nil, false, nil
	}

	audit, err := AuditStorage(
		ctx,
		ExperimentalBandersnatchIPA256V0(),
		store,
		testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrCancelled) || audit.valid || store.view.closeCalls != 1 {
		t.Fatalf(
			"post-inventory cancellation = (%+v, %v), close calls = %d",
			audit,
			err,
			store.view.closeCalls,
		)
	}
}

func TestStorageAuditInternalBoundariesFailClosed(t *testing.T) {
	t.Parallel()

	snapshot := testStorageFacadeSnapshot(t)
	store := newInternalAuditStore(t, snapshot, nil)
	cancelledContext, cancelBeforePublication := context.WithCancel(context.Background())
	cancelBeforePublication()
	if _, err := auditPublications(
		cancelledContext, store.view, testInternalStorageAuditLimits(),
	); !errors.Is(err, ErrCancelled) || store.view.currentCalls != 0 {
		t.Fatalf(
			"pre-publication cancellation = %v, current calls = %d",
			err,
			store.view.currentCalls,
		)
	}

	store = newInternalAuditStore(t, snapshot, nil)
	publicationContextAfterCurrent, cancelAfterCurrent := context.WithCancel(context.Background())
	store.view.cancelAfterCurrent = cancelAfterCurrent
	if _, err := auditPublications(
		publicationContextAfterCurrent, store.view, testInternalStorageAuditLimits(),
	); !errors.Is(err, ErrCancelled) || store.view.retainedCalls != 0 {
		t.Fatalf(
			"post-current cancellation = %v, retained calls = %d",
			err,
			store.view.retainedCalls,
		)
	}

	store = newInternalAuditStore(t, snapshot, nil)
	hiddenRetained := make([]StorePublication, 1, 4)
	hiddenRetained[0] = store.view.current
	store.view.currentOK = false
	store.view.retained = hiddenRetained
	hiddenRetainedLimits := testInternalStorageAuditLimits()
	hiddenRetainedLimits.MaxPublications = 3
	if _, err := auditPublications(
		context.Background(), store.view, hiddenRetainedLimits,
	); err == nil {
		t.Fatal("hidden retained capacity accepted")
	} else {
		assertAuditResourceError(t, err, ResourcePublications, 3, 4)
	}
	if publicationBytes := uint64(reflect.TypeOf(StorePublication{}).Size()); 2*publicationBytes > storageAuditPublicationBytes {
		t.Fatalf(
			"publication allowance = %d, want at least two %d-byte slots",
			storageAuditPublicationBytes, publicationBytes,
		)
	}

	store = newInternalAuditStore(t, snapshot, nil)
	store.view.currentOK = false
	store.view.retained = make([]StorePublication, 1, 4)
	store.view.retained[0] = store.view.current
	hiddenRetainedLimits = testInternalStorageAuditLimits()
	hiddenRetainedLimits.MaxPublications = 4
	hiddenRetainedLimits.MaxTemporaryBytes = 4 * storageAuditPublicationBytes
	if _, err := auditPublications(
		context.Background(), store.view, hiddenRetainedLimits,
	); err == nil {
		t.Fatal("within-count retained capacity escaped temporary budget")
	} else {
		assertAuditResourceError(
			t, err, ResourceTemporaryBytes,
			4*storageAuditPublicationBytes, 5*storageAuditPublicationBytes,
		)
	}

	store = newInternalAuditStore(t, snapshot, nil)
	exact := testInternalStorageAuditLimits()
	exact.MaxPublications = 1
	exact.MaxInventoryNodes = uint32(len(store.view.nodes))
	exact.MaxNodeIDsPerPage = 1
	exact.MaxInventoryPages = uint32(len(store.view.nodes))
	exact.MaxUnreachableNodes = 0
	if _, err := AuditStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(), store, exact,
	); err != nil {
		t.Fatalf("exact audit boundaries error = %v", err)
	}
	if store.view.retainedMax != 0 {
		t.Fatalf("retained publication bound = %d, want 0", store.view.retainedMax)
	}

	store = newInternalAuditStore(t, snapshot, nil)
	if _, err := auditPublications(
		context.Background(), store.view, testInternalStorageAuditLimits(),
	); err != nil {
		t.Fatalf("auditPublications() error = %v", err)
	}
	if got, want := store.view.retainedMax, uint32(7); got != want {
		t.Fatalf("retained publication bound = %d, want %d", got, want)
	}

	publicationContext := &auditCancelContext{Context: context.Background(), failAt: 3}
	store.view.retained = []StorePublication{store.view.current}
	store.view.currentOK = false
	if _, err := auditPublications(
		publicationContext, store.view, testInternalStorageAuditLimits(),
	); !errors.Is(err, ErrCancelled) {
		t.Fatalf("publication cancellation error = %v", err)
	}

	store = newInternalAuditStore(t, snapshot, nil)
	store.view.currentOK = false
	store.view.retained = []StorePublication{store.view.current, store.view.current}
	if _, err := auditPublications(
		context.Background(), store.view, testInternalStorageAuditLimits(),
	); !errors.Is(err, ErrStorageInventory) {
		t.Fatalf("retained ordering error = %v", err)
	}

	store = newInternalAuditStore(t, snapshot, nil)
	limits := testInternalStorageAuditLimits()
	limits.MaxTemporaryBytes = storageAuditPublicationBytes - 1
	if _, err := auditPublications(context.Background(), store.view, limits); err == nil {
		t.Fatal("publication temporary limit accepted")
	} else {
		assertAuditResourceError(
			t, err, ResourceTemporaryBytes,
			storageAuditPublicationBytes-1, storageAuditPublicationBytes,
		)
	}

	store = newInternalAuditStore(t, snapshot, nil)
	limits = testInternalStorageAuditLimits()
	limits.MaxTemporaryBytes = storageAuditPublicationBytes + storageAuditReachableBytes - 1
	_, err := AuditStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(), store, limits,
	)
	if err == nil {
		t.Fatal("reachable-set temporary limit accepted")
	}
	var resourceErr *ResourceError
	if !errors.As(err, &resourceErr) || resourceErr.Resource != ResourceTemporaryBytes {
		t.Fatalf("reachable temporary error = %v", err)
	}

	emptySnapshot, emptyErr := NewSnapshot(
		context.Background(), ExperimentalBandersnatchIPA256V0(), nil,
		testFacadeSnapshotLimits(),
	)
	if emptyErr != nil {
		t.Fatalf("NewSnapshot() error = %v", emptyErr)
	}
	store = newInternalAuditStore(t, emptySnapshot, nil)
	limits = testInternalStorageAuditLimits()
	limits.MaxTemporaryBytes = storageAuditPublicationBytes + 319
	_, err = AuditStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(), store, limits,
	)
	if !errors.As(err, &resourceErr) || resourceErr.Resource != ResourceTemporaryBytes {
		t.Fatalf("first reachable map allocation error = %v", err)
	}

	view := &internalAuditSnapshot{
		nodeIDsFn: func(*NodeID, uint32) ([]NodeID, bool, error) {
			return []NodeID{{1}}, false, nil
		},
	}
	limits = testInternalStorageAuditLimits()
	limits.MaxTemporaryBytes = 2*storageAuditNodeIDBytes - 1
	_, _, err = auditInventory(
		context.Background(), view, limits, 0, 0, map[NodeID]struct{}{},
	)
	assertAuditResourceError(
		t, err, ResourceTemporaryBytes,
		2*storageAuditNodeIDBytes-1, 2*storageAuditNodeIDBytes,
	)

	limits = testInternalStorageAuditLimits()
	limits.MaxInventoryNodes = 1
	limits.MaxNodeIDsPerPage = 1
	limits.MaxUnreachableNodes = 1
	view.nodeIDsFn = func(*NodeID, uint32) ([]NodeID, bool, error) {
		return []NodeID{{1}}, true, nil
	}
	_, _, err = auditInventory(
		context.Background(), view, limits, 0, 0, map[NodeID]struct{}{},
	)
	assertAuditResourceError(t, err, ResourceInventoryNodes, 1, 2)

	_, _, err = auditInventory(
		&auditCancelContext{Context: context.Background(), failAt: 1},
		view,
		testInternalStorageAuditLimits(),
		0,
		0,
		map[NodeID]struct{}{},
	)
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("inventory pre-page cancellation = %v", err)
	}
	view.nodeIDsFn = func(*NodeID, uint32) ([]NodeID, bool, error) {
		return []NodeID{{1}}, false, nil
	}
	_, _, err = auditInventory(
		&auditCancelContext{Context: context.Background(), failAt: 3},
		view,
		testInternalStorageAuditLimits(),
		0,
		0,
		map[NodeID]struct{}{},
	)
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("inventory page cancellation = %v", err)
	}

	duplicatePage := &internalAuditSnapshot{
		nodeIDsFn: func(*NodeID, uint32) ([]NodeID, bool, error) {
			return []NodeID{{1}, {1}}, false, nil
		},
	}
	_, _, err = auditInventory(
		context.Background(), duplicatePage, testInternalStorageAuditLimits(),
		0, 0, map[NodeID]struct{}{},
	)
	if !errors.Is(err, ErrStorageInventory) {
		t.Fatalf("duplicate page error = %v", err)
	}

	cursorCalls := 0
	duplicateCursor := &internalAuditSnapshot{
		nodeIDsFn: func(*NodeID, uint32) ([]NodeID, bool, error) {
			cursorCalls++

			return []NodeID{{1}}, cursorCalls == 1, nil
		},
	}
	_, _, err = auditInventory(
		context.Background(), duplicateCursor, testInternalStorageAuditLimits(),
		0, 0, map[NodeID]struct{}{},
	)
	if !errors.Is(err, ErrStorageInventory) {
		t.Fatalf("duplicate cursor error = %v", err)
	}

	var differentKey Key
	differentKey[0] = 8
	differentKey[31] = 8
	newer, _, applyErr := snapshot.Apply(
		context.Background(), []Update{Set(differentKey, Value{8})},
	)
	if applyErr != nil {
		t.Fatalf("Apply() error = %v", applyErr)
	}
	oldRoot, _ := snapshot.Root()
	newerRoot, _ := newer.Root()
	oldBytes, _ := oldRoot.Bytes()
	newerBytes, _ := newerRoot.Bytes()
	wantComparison := bytes.Compare(oldBytes[:], newerBytes[:])
	var oldNode NodeID
	var newerNode NodeID
	if wantComparison < 0 {
		oldNode[0] = 0xff
	} else {
		newerNode[0] = 0xff
	}
	oldPublication, oldErr := NewStorePublication(oldRoot, oldNode)
	newerPublication, newerErr := NewStorePublication(newerRoot, newerNode)
	if oldErr != nil || newerErr != nil || wantComparison == 0 {
		t.Fatalf("publication setup errors = (%v, %v), comparison = %d", oldErr, newerErr, wantComparison)
	}
	if got := compareStorePublication(oldPublication, newerPublication); got != wantComparison {
		t.Fatalf("publication comparison = %d, want %d", got, wantComparison)
	}
	if got := compareStorePublication(newerPublication, oldPublication); got != -wantComparison {
		t.Fatalf("reverse publication comparison = %d, want %d", got, -wantComparison)
	}

	temporary := testInternalStorageAuditLimits()
	temporary.MaxTemporaryBytes = storageAuditPublicationBytes
	if err := checkAuditTemporary(temporary, 1, 0, 0, 0); err != nil {
		t.Fatalf("exact temporary boundary error = %v", err)
	}
}

func TestStorageAuditRejectsHiddenPageCapacityAndPreflightsResultGrowth(t *testing.T) {
	t.Parallel()

	hiddenCapacity := &internalAuditSnapshot{
		nodeIDsFn: func(*NodeID, uint32) ([]NodeID, bool, error) {
			ids := make([]NodeID, 1, 4)
			ids[0] = NodeID{1}

			return ids, false, nil
		},
	}
	limits := testInternalStorageAuditLimits()
	limits.MaxNodeIDsPerPage = 3
	_, _, err := auditInventory(
		context.Background(), hiddenCapacity, limits,
		0, 0, map[NodeID]struct{}{},
	)
	if !errors.Is(err, ErrStorageInventory) {
		t.Fatalf("hidden page capacity error = %v", err)
	}

	threeIDs := []NodeID{{1}, {2}, {3}}
	threeCursor := 0
	threeCalls := 0
	threeUnreachable := &internalAuditSnapshot{
		nodeIDsFn: func(_ *NodeID, maxIDs uint32) ([]NodeID, bool, error) {
			threeCalls++
			end := min(threeCursor+int(maxIDs), len(threeIDs))
			page := append([]NodeID(nil), threeIDs[threeCursor:end]...)
			threeCursor = end

			return page, end < len(threeIDs), nil
		},
	}
	limits = testInternalStorageAuditLimits()
	limits.MaxTemporaryBytes = 7*storageAuditNodeIDBytes - 1
	_, _, err = auditInventory(
		context.Background(), threeUnreachable, limits,
		0, 0, map[NodeID]struct{}{},
	)
	assertAuditResourceError(
		t, err, ResourceTemporaryBytes,
		7*storageAuditNodeIDBytes-1, 7*storageAuditNodeIDBytes,
	)
	if threeCalls != 1 {
		t.Fatalf("preflight page calls = %d, want 1", threeCalls)
	}

	threeCursor = 0
	threeCalls = 0
	limits.MaxTemporaryBytes = 7 * storageAuditNodeIDBytes
	unreachable, count, err := auditInventory(
		context.Background(), threeUnreachable, limits,
		0, 0, map[NodeID]struct{}{},
	)
	if err != nil || count != 3 || !slices.Equal(unreachable, threeIDs) || threeCalls != 2 {
		t.Fatalf(
			"bounded pagination = (%x, %d, %d, %v)",
			unreachable, count, threeCalls, err,
		)
	}

	pageCalls := 0
	maxPage := uint32(0)
	emptyInventory := &internalAuditSnapshot{
		nodeIDsFn: func(_ *NodeID, maxIDs uint32) ([]NodeID, bool, error) {
			pageCalls++
			maxPage = maxIDs

			return nil, false, nil
		},
	}
	limits = testInternalStorageAuditLimits()
	limits.MaxTemporaryBytes = 2*storageAuditNodeIDBytes - 1
	_, _, err = auditInventory(
		context.Background(), emptyInventory, limits,
		0, 0, map[NodeID]struct{}{},
	)
	assertAuditResourceError(
		t, err, ResourceTemporaryBytes,
		2*storageAuditNodeIDBytes-1, 2*storageAuditNodeIDBytes,
	)
	if pageCalls != 0 {
		t.Fatalf("page calls = %d, want 0", pageCalls)
	}

	limits.MaxTemporaryBytes = 2 * storageAuditNodeIDBytes
	if _, _, err := auditInventory(
		context.Background(), emptyInventory, limits,
		0, 0, map[NodeID]struct{}{},
	); err != nil {
		t.Fatalf("exact page budget error = %v", err)
	}
	if pageCalls != 1 || maxPage != 1 {
		t.Fatalf("page calls = %d, max page = %d, want (1, 1)", pageCalls, maxPage)
	}
}

func TestStorageAuditAccountingFunctionsAreExact(t *testing.T) {
	t.Parallel()

	limits := testInternalStorageAuditLimits()
	limits.MaxTemporaryBytes = 8*storageAuditNodeIDBytes - 1
	if _, err := storageAuditPageLimit(limits, 0, 0, 3, 4, 1); err == nil {
		t.Fatal("defensive-copy peak escaped temporary budget")
	} else {
		assertAuditResourceError(
			t, err, ResourceTemporaryBytes,
			8*storageAuditNodeIDBytes-1, 8*storageAuditNodeIDBytes,
		)
	}
	if got := storageAuditPreviousCapacity(4, 4); got != 0 {
		t.Fatalf("unchanged previous capacity = %d, want 0", got)
	}
	if got := storageAuditPreviousCapacity(0, 1); got != 0 {
		t.Fatalf("unit previous capacity = %d, want 0", got)
	}
	if got := storageAuditPreviousCapacity(3, 12); got != 6 {
		t.Fatalf("non-power previous capacity = %d, want 6", got)
	}
	if got := storageAuditPreviousCapacity(4, 10); got != 8 {
		t.Fatalf("truncated previous capacity = %d, want 8", got)
	}
	if got := storageAuditResultCapacity(0, 0, 0, 8); got != 0 {
		t.Fatalf("empty result capacity = %d, want 0", got)
	}
	if got := storageAuditResultCapacity(3, 3, 7, 20); got != 12 {
		t.Fatalf("non-power result capacity = %d, want 12", got)
	}
	if got := storageAuditResultCapacity(4, 4, 9, 10); got != 10 {
		t.Fatalf("truncated result capacity = %d, want 10", got)
	}
	wantTemporary := storageAuditPublicationBytes + storageAuditReachableBytes +
		5*storageAuditNodeIDBytes
	if got := storageAuditTemporaryBytes(1, 1, 2, 3); got != wantTemporary {
		t.Fatalf("mixed temporary bytes = %d, want %d", got, wantTemporary)
	}
	spare := make([]NodeID, 513, 832)
	spare = appendStorageAuditNode(spare, NodeID{1}, 1000)
	if len(spare) != 514 || cap(spare) != 832 {
		t.Fatalf("spare result growth = (len %d, cap %d)", len(spare), cap(spare))
	}
	empty := appendStorageAuditNode(nil, NodeID{1}, 1)
	if len(empty) != 1 || cap(empty) != 1 {
		t.Fatalf("empty result growth = (len %d, cap %d)", len(empty), cap(empty))
	}
	full := appendStorageAuditNode(make([]NodeID, 4), NodeID{1}, 8)
	if len(full) != 5 || cap(full) != 8 {
		t.Fatalf("full result growth = (len %d, cap %d)", len(full), cap(full))
	}

	const unreachableCount = 600
	ids := make([]NodeID, unreachableCount)
	for index := range ids {
		value := index + 1
		ids[index][NodeIDSize-2] = byte(value >> 8)
		ids[index][NodeIDSize-1] = byte(value)
	}
	view := &internalAuditSnapshot{
		nodeIDsFn: func(*NodeID, uint32) ([]NodeID, bool, error) {
			result := make([]NodeID, len(ids))
			copy(result, ids)

			return result, false, nil
		},
	}
	limits = testInternalStorageAuditLimits()
	limits.MaxInventoryNodes = unreachableCount
	limits.MaxNodeIDsPerPage = unreachableCount
	limits.MaxInventoryPages = 1
	limits.MaxUnreachableNodes = unreachableCount
	unreachable, count, err := auditInventory(
		context.Background(), view, limits, 0, 0, map[NodeID]struct{}{},
	)
	if err != nil || count != unreachableCount || len(unreachable) != unreachableCount ||
		cap(unreachable) != unreachableCount {
		t.Fatalf(
			"deterministic result growth = (count %d, len %d, cap %d, error %v)",
			count, len(unreachable), cap(unreachable), err,
		)
	}
}

type internalAuditStore struct {
	capabilities            StoreCapabilities
	view                    *internalAuditSnapshot
	openErr                 error
	returnNil               bool
	openCalls               int
	cancelAfterCapabilities context.CancelFunc
}

type auditCancelContext struct {
	context.Context
	calls  int
	failAt int
}

func (ctx *auditCancelContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.failAt {
		return context.Canceled
	}

	return nil
}

func (store *internalAuditStore) Capabilities() StoreCapabilities {
	if store.cancelAfterCapabilities != nil {
		store.cancelAfterCapabilities()
	}

	return store.capabilities
}

func (store *internalAuditStore) OpenAudit(context.Context) (NodeAuditSnapshot, error) {
	store.openCalls++
	if store.openErr != nil {
		return nil, store.openErr
	}
	if store.returnNil {
		var snapshot *internalAuditSnapshot

		return snapshot, nil
	}

	return store.view, nil
}

type internalAuditSnapshot struct {
	current            StorePublication
	currentOK          bool
	retained           []StorePublication
	retainedMax        uint32
	nodes              map[NodeID][]byte
	omitLast           bool
	currentErr         error
	retainedErr        error
	readErr            error
	nodeIDsErr         error
	closeErr           error
	nodeIDsFn          func(*NodeID, uint32) ([]NodeID, bool, error)
	closeCalls         int
	currentCalls       int
	retainedCalls      int
	cancelAfterCurrent context.CancelFunc
}

func (snapshot *internalAuditSnapshot) CurrentPublication(
	context.Context,
) (StorePublication, bool, error) {
	snapshot.currentCalls++
	if snapshot.cancelAfterCurrent != nil {
		snapshot.cancelAfterCurrent()
	}
	if snapshot.currentErr != nil {
		return StorePublication{}, false, snapshot.currentErr
	}

	return snapshot.current, snapshot.currentOK, nil
}

func (snapshot *internalAuditSnapshot) RetainedPublications(
	_ context.Context,
	maxPublications uint32,
) ([]StorePublication, error) {
	snapshot.retainedCalls++
	snapshot.retainedMax = maxPublications
	if snapshot.retainedErr != nil {
		return nil, snapshot.retainedErr
	}

	return snapshot.retained, nil
}

func (snapshot *internalAuditSnapshot) ReadNode(
	_ context.Context,
	id NodeID,
	_ uint64,
) ([]byte, error) {
	if snapshot.readErr != nil {
		return nil, snapshot.readErr
	}
	encoded, ok := snapshot.nodes[id]
	if !ok {
		return nil, ErrStorageNodeMissing
	}

	return append([]byte(nil), encoded...), nil
}

func (snapshot *internalAuditSnapshot) NodeIDs(
	_ context.Context,
	after *NodeID,
	maxIDs uint32,
) ([]NodeID, bool, error) {
	if snapshot.nodeIDsErr != nil {
		return nil, false, snapshot.nodeIDsErr
	}
	if snapshot.nodeIDsFn != nil {
		return snapshot.nodeIDsFn(after, maxIDs)
	}
	ids := make([]NodeID, 0, len(snapshot.nodes))
	for id := range snapshot.nodes {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return bytes.Compare(ids[i][:], ids[j][:]) < 0
	})
	if snapshot.omitLast && len(ids) != 0 {
		ids = ids[:len(ids)-1]
	}
	start := 0
	if after != nil {
		start = sort.Search(len(ids), func(index int) bool {
			return bytes.Compare(ids[index][:], after[:]) > 0
		})
	}
	end := min(start+int(maxIDs), len(ids))

	return append([]NodeID(nil), ids[start:end]...), end < len(ids), nil
}

func (snapshot *internalAuditSnapshot) Close(context.Context) error {
	snapshot.closeCalls++

	return snapshot.closeErr
}

func newInternalAuditStore(
	t testing.TB,
	current Snapshot,
	retained []Snapshot,
) *internalAuditStore {
	t.Helper()

	currentReader := internalReaderFromSnapshot(t, current)
	nodes := make(map[NodeID][]byte, len(currentReader.view.nodes))
	for id, encoded := range currentReader.view.nodes {
		nodes[id] = append([]byte(nil), encoded...)
	}
	publications := make([]StorePublication, len(retained))
	for index, retainedSnapshot := range retained {
		reader := internalReaderFromSnapshot(t, retainedSnapshot)
		publications[index] = reader.view.publication
		for id, encoded := range reader.view.nodes {
			nodes[id] = append([]byte(nil), encoded...)
		}
	}
	sort.Slice(publications, func(i, j int) bool {
		left, _ := publications[i].Root()
		right, _ := publications[j].Root()
		leftBytes, _ := left.Bytes()
		rightBytes, _ := right.Bytes()

		return bytes.Compare(leftBytes[:], rightBytes[:]) < 0
	})

	return &internalAuditStore{
		capabilities: RequiredAuditStoreCapabilities,
		view: &internalAuditSnapshot{
			current:   currentReader.view.publication,
			currentOK: true,
			retained:  publications,
			nodes:     nodes,
		},
	}
}

func testInternalStorageAuditLimits() StorageAuditLimits {
	return StorageAuditLimits{
		MaxPublications:     8,
		MaxInventoryNodes:   256,
		MaxNodeIDsPerPage:   3,
		MaxInventoryPages:   128,
		MaxUnreachableNodes: 64,
		MaxTemporaryBytes:   8 << 20,
		Read:                testInternalStorageReadLimits(),
	}
}

func assertAuditResourceError(
	t testing.TB,
	err error,
	resource Resource,
	limit uint64,
	actual uint64,
) {
	t.Helper()
	var resourceErr *ResourceError
	if !errors.As(err, &resourceErr) ||
		resourceErr.Resource != resource ||
		resourceErr.Limit != limit ||
		resourceErr.Actual != actual {
		t.Fatalf(
			"error = %v, want resource=%d limit=%d actual=%d",
			err,
			resource,
			limit,
			actual,
		)
	}
}
