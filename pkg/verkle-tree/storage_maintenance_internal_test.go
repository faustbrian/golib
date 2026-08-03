package verkletree

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sort"
	"testing"
)

func TestRecoverStoragePreservesPublicationsAndRemovesInterruptedWrites(
	t *testing.T,
) {
	t.Parallel()

	oldSnapshot := testStorageFacadeSnapshotWithKey(t, 1)
	middleSnapshot := testStorageFacadeSnapshotWithKey(t, 2)
	currentSnapshot := testStorageFacadeSnapshotWithKey(t, 3)
	auditStore := newInternalAuditStore(
		t, currentSnapshot, []Snapshot{oldSnapshot, middleSnapshot},
	)
	wantRetained := slices.Clone(auditStore.view.retained)
	orphan := NodeID{0xff}
	auditStore.view.nodes[orphan] = []byte("interrupted unpublished write")
	store := &internalMaintenanceStore{
		internalAuditStore: auditStore,
		applyToView:        true,
	}
	store.capabilities |= StoreCapabilityAtomicMaintenance

	result, err := RecoverStorage(
		context.Background(),
		ExperimentalBandersnatchIPA256V0(),
		store,
		testInternalStorageAuditLimits(),
	)
	if err != nil {
		t.Fatalf("RecoverStorage() error = %v", err)
	}
	if result.PreviousRetainedCount() != 2 || result.RetainedCount() != 2 ||
		result.DeletedNodeCount() != 1 {
		t.Fatalf(
			"recovery counts = (%d, %d, %d), want (2, 2, 1)",
			result.PreviousRetainedCount(),
			result.RetainedCount(),
			result.DeletedNodeCount(),
		)
	}
	retained, err := store.request.RetainedPublications(context.Background())
	if err != nil || !slices.Equal(retained, wantRetained) {
		t.Fatalf("recovery retained publications = (%+v, %v)", retained, err)
	}
	deleted, err := result.DeletedNodes(context.Background())
	if err != nil || !slices.Equal(deleted, []NodeID{orphan}) {
		t.Fatalf("recovery deleted nodes = (%x, %v), want %x", deleted, err, orphan)
	}
	if store.applyCalls != 1 || !store.closedBeforeApply {
		t.Fatalf(
			"recovery apply calls = %d, closed before apply = %t",
			store.applyCalls,
			store.closedBeforeApply,
		)
	}
	if _, present := store.view.nodes[orphan]; present {
		t.Fatal("interrupted unpublished node survived recovery")
	}
	for id := range internalReaderFromSnapshot(t, currentSnapshot).view.nodes {
		if _, present := store.view.nodes[id]; !present {
			t.Fatalf("current publication node %x was removed", id)
		}
	}
	for _, snapshot := range []Snapshot{oldSnapshot, middleSnapshot} {
		for id := range internalReaderFromSnapshot(t, snapshot).view.nodes {
			if _, present := store.view.nodes[id]; !present {
				t.Fatalf("retained publication node %x was removed", id)
			}
		}
	}
}

func TestRecoverStorageSupportsAnUnpublishedNodeOnlyStore(t *testing.T) {
	t.Parallel()

	auditStore := newInternalAuditStore(t, testStorageFacadeSnapshot(t), nil)
	auditStore.view.current = StorePublication{}
	auditStore.view.currentOK = false
	auditStore.view.retained = nil
	auditStore.view.nodes = map[NodeID][]byte{
		{1}: []byte("first interrupted node"),
		{2}: []byte("second interrupted node"),
	}
	store := &internalMaintenanceStore{
		internalAuditStore: auditStore,
		applyToView:        true,
	}
	store.capabilities |= StoreCapabilityAtomicMaintenance

	result, err := RecoverStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(), store,
		testInternalStorageAuditLimits(),
	)
	if err != nil || result.PreviousRetainedCount() != 0 ||
		result.RetainedCount() != 0 || result.DeletedNodeCount() != 2 {
		t.Fatalf("unpublished-only recovery = (%+v, %v)", result, err)
	}
	current, present, currentErr := store.request.CurrentPublication()
	if currentErr != nil || present || current != (StorePublication{}) {
		t.Fatalf(
			"recovery current expectation = (%+v, %t, %v)",
			current,
			present,
			currentErr,
		)
	}
	if len(store.view.nodes) != 0 {
		t.Fatalf("recovery left %d unpublished nodes", len(store.view.nodes))
	}
}

func TestRecoverStorageFailsAtomicallyForInvalidOrChangingState(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("adapter failure")
	publicationFailure := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(
			t, testStorageFacadeSnapshot(t), nil,
		),
	}
	publicationFailure.capabilities |= StoreCapabilityAtomicMaintenance
	publicationFailure.view.currentErr = sentinel
	result, err := RecoverStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(),
		publicationFailure, testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrStorageMaintenance) || !errors.Is(err, sentinel) ||
		result.valid || publicationFailure.applyCalls != 0 ||
		publicationFailure.view.closeCalls != 1 {
		t.Fatalf(
			"publication-failed recovery = (%+v, %v), apply=%d close=%d",
			result,
			err,
			publicationFailure.applyCalls,
			publicationFailure.view.closeCalls,
		)
	}

	corrupt := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(
			t, testStorageFacadeSnapshot(t), nil,
		),
	}
	corrupt.capabilities |= StoreCapabilityAtomicMaintenance
	corrupt.view.readErr = sentinel
	result, err = RecoverStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(), corrupt,
		testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrStorageMaintenance) || !errors.Is(err, sentinel) ||
		result.valid || corrupt.applyCalls != 0 || corrupt.view.closeCalls != 1 {
		t.Fatalf(
			"corrupt recovery = (%+v, %v), apply=%d close=%d",
			result,
			err,
			corrupt.applyCalls,
			corrupt.view.closeCalls,
		)
	}

	closeFailure := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(
			t, testStorageFacadeSnapshot(t), nil,
		),
	}
	closeFailure.capabilities |= StoreCapabilityAtomicMaintenance
	closeFailure.view.closeErr = sentinel
	result, err = RecoverStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(), closeFailure,
		testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrStorageMaintenance) || !errors.Is(err, sentinel) ||
		result.valid || closeFailure.applyCalls != 0 ||
		closeFailure.view.closeCalls != 1 {
		t.Fatalf(
			"close-failed recovery = (%+v, %v), apply=%d close=%d",
			result,
			err,
			closeFailure.applyCalls,
			closeFailure.view.closeCalls,
		)
	}

	stale := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(
			t, testStorageFacadeSnapshot(t), nil,
		),
		applyErr: ErrStaleRoot,
	}
	stale.capabilities |= StoreCapabilityAtomicMaintenance
	stale.view.nodes[NodeID{0xfe}] = []byte("interrupted node")
	result, err = RecoverStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(), stale,
		testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrStorageMaintenance) || !errors.Is(err, ErrStaleRoot) ||
		result.valid || stale.applyCalls != 1 || !stale.closedBeforeApply {
		t.Fatalf(
			"stale recovery = (%+v, %v), apply=%d closed=%t",
			result,
			err,
			stale.applyCalls,
			stale.closedBeforeApply,
		)
	}
}

func TestRecoverStorageRejectsInvalidInputsBeforeOpening(t *testing.T) {
	t.Parallel()

	valid := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(
			t, testStorageFacadeSnapshot(t), nil,
		),
	}
	valid.capabilities |= StoreCapabilityAtomicMaintenance
	var nilContext context.Context
	var nilStore *internalMaintenanceStore
	tests := map[string]struct {
		ctx     context.Context
		profile Profile
		store   NodeMaintenanceStore
		limits  StorageAuditLimits
		want    error
	}{
		"nil context": {
			ctx: nilContext, profile: ExperimentalBandersnatchIPA256V0(),
			store: valid, limits: testInternalStorageAuditLimits(),
			want: ErrInvalidContext,
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
			store: nilStore, limits: testInternalStorageAuditLimits(),
			want: ErrInvalidStore,
		},
		"invalid limits": {
			ctx: context.Background(), profile: ExperimentalBandersnatchIPA256V0(),
			store: valid, want: ErrInvalidLimits,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			before := valid.openCalls
			_, recoverErr := RecoverStorage(
				test.ctx, test.profile, test.store, test.limits,
			)
			if !errors.Is(recoverErr, test.want) {
				t.Fatalf("RecoverStorage() error = %v, want %v", recoverErr, test.want)
			}
			if valid.openCalls != before {
				t.Fatalf("open calls changed from %d to %d", before, valid.openCalls)
			}
		})
	}

	missing := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(
			t, testStorageFacadeSnapshot(t), nil,
		),
	}
	missing.capabilities = RequiredMaintenanceStoreCapabilities &^
		StoreCapabilityAtomicMaintenance
	_, err := RecoverStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(), missing,
		testInternalStorageAuditLimits(),
	)
	var capabilityErr *StoreCapabilityError
	if !errors.As(err, &capabilityErr) ||
		capabilityErr.Missing != StoreCapabilityAtomicMaintenance ||
		missing.openCalls != 0 {
		t.Fatalf("missing recovery capability = %v, open calls = %d", err, missing.openCalls)
	}
}

func TestMaintainStorageAtomicallyAppliesRetentionAndPruning(t *testing.T) {
	t.Parallel()

	oldSnapshot := testStorageFacadeSnapshot(t)
	var middleKey Key
	middleKey[0] = 7
	middleKey[31] = 7
	middleSnapshot, _, err := oldSnapshot.Apply(
		context.Background(), []Update{Set(middleKey, Value{7})},
	)
	if err != nil {
		t.Fatalf("first Apply() error = %v", err)
	}
	var currentKey Key
	currentKey[0] = 8
	currentKey[31] = 8
	currentSnapshot, _, err := middleSnapshot.Apply(
		context.Background(), []Update{Set(currentKey, Value{8})},
	)
	if err != nil {
		t.Fatalf("second Apply() error = %v", err)
	}
	auditStore := newInternalAuditStore(
		t, currentSnapshot, []Snapshot{oldSnapshot, middleSnapshot},
	)
	orphan := NodeID{0xff}
	auditStore.view.nodes[orphan] = []byte("interrupted unpublished write")
	store := &internalMaintenanceStore{internalAuditStore: auditStore}
	store.capabilities |= StoreCapabilityAtomicMaintenance
	desired := []StorePublication{
		internalReaderFromSnapshot(t, oldSnapshot).view.publication,
	}
	wantDeleted := internalNodesOutsideSnapshots(
		t, auditStore.view.nodes, currentSnapshot, oldSnapshot,
	)

	result, err := MaintainStorage(
		context.Background(),
		ExperimentalBandersnatchIPA256V0(),
		store,
		desired,
		testInternalStorageAuditLimits(),
	)
	if err != nil {
		t.Fatalf("MaintainStorage() error = %v", err)
	}
	if store.applyCalls != 1 || !store.closedBeforeApply {
		t.Fatalf(
			"maintenance calls = %d, audit closed before apply = %t",
			store.applyCalls,
			store.closedBeforeApply,
		)
	}
	if result.PreviousRetainedCount() != 2 || result.RetainedCount() != 1 ||
		result.DeletedNodeCount() != uint32(len(wantDeleted)) {
		t.Fatalf(
			"result counts = (%d, %d, %d), want (2, 1, %d)",
			result.PreviousRetainedCount(),
			result.RetainedCount(),
			result.DeletedNodeCount(),
			len(wantDeleted),
		)
	}
	requestProfile, err := store.request.Profile()
	if err != nil || requestProfile != ExperimentalBandersnatchIPA256V0() {
		t.Fatalf("request Profile() = (%+v, %v)", requestProfile, err)
	}
	deleted, err := result.DeletedNodes(context.Background())
	if err != nil || !slices.Equal(deleted, wantDeleted) {
		t.Fatalf("DeletedNodes() = (%x, %v), want %x", deleted, err, wantDeleted)
	}
	deleted[0] = NodeID{}
	again, err := result.DeletedNodes(context.Background())
	if err != nil || !slices.Equal(again, wantDeleted) {
		t.Fatalf("DeletedNodes() aliases result: (%x, %v)", again, err)
	}

	current, present, err := store.request.CurrentPublication()
	if err != nil || !present || compareStorePublication(current, auditStore.view.current) != 0 {
		t.Fatalf("CurrentPublication() = (%+v, %t, %v)", current, present, err)
	}
	previous, err := store.request.PreviousRetainedPublications(context.Background())
	if err != nil || !slices.Equal(previous, auditStore.view.retained) {
		t.Fatalf("PreviousRetainedPublications() = (%+v, %v)", previous, err)
	}
	retained, err := store.request.RetainedPublications(context.Background())
	if err != nil || !slices.Equal(retained, desired) {
		t.Fatalf("RetainedPublications() = (%+v, %v), want %+v", retained, err, desired)
	}
	requestDeleted, err := store.request.DeletedNodes(context.Background())
	if err != nil || !slices.Equal(requestDeleted, wantDeleted) {
		t.Fatalf("request DeletedNodes() = (%x, %v), want %x", requestDeleted, err, wantDeleted)
	}
}

func TestMaintainStorageLeavesACompleteAuditablePostMaintenanceStore(t *testing.T) {
	t.Parallel()

	oldSnapshot := testStorageFacadeSnapshot(t)
	var key Key
	key[0] = 61
	currentSnapshot, _, err := oldSnapshot.Apply(
		context.Background(), []Update{Set(key, Value{61})},
	)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	store := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(
			t, currentSnapshot, []Snapshot{oldSnapshot},
		),
		applyToView: true,
	}
	store.capabilities |= StoreCapabilityAtomicMaintenance
	store.view.nodes[NodeID{0xfc}] = []byte("unpublished")

	result, err := MaintainStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(), store, nil,
		testInternalStorageAuditLimits(),
	)
	if err != nil {
		t.Fatalf("MaintainStorage() error = %v", err)
	}
	currentNodes := internalReaderFromSnapshot(t, currentSnapshot).view.nodes
	if result.RetainedCount() != 0 || len(store.view.retained) != 0 ||
		len(store.view.nodes) != len(currentNodes) {
		t.Fatalf(
			"post-maintenance retained=%d/%d nodes=%d, want %d",
			result.RetainedCount(), len(store.view.retained),
			len(store.view.nodes), len(currentNodes),
		)
	}
	for id := range currentNodes {
		if _, present := store.view.nodes[id]; !present {
			t.Fatalf("current node %x was pruned", id)
		}
	}
	audit, err := AuditStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(),
		store, testInternalStorageAuditLimits(),
	)
	if err != nil || audit.UnreachableNodeCount() != 0 {
		t.Fatalf("post-maintenance AuditStorage() = (%+v, %v)", audit, err)
	}
}

func TestMaintainStorageReadsOneIsolatedPublicationSet(t *testing.T) {
	t.Parallel()

	snapshot := testStorageFacadeSnapshot(t)
	store := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(t, snapshot, nil),
	}
	store.capabilities |= StoreCapabilityAtomicMaintenance
	result, err := MaintainStorage(
		context.Background(),
		ExperimentalBandersnatchIPA256V0(),
		store,
		nil,
		testInternalStorageAuditLimits(),
	)
	if err != nil || !result.valid {
		t.Fatalf("MaintainStorage() = (%+v, %v), want success", result, err)
	}
	if store.view.currentCalls != 1 {
		t.Fatalf("CurrentPublication() calls = %d, want 1", store.view.currentCalls)
	}
	if store.applyCalls != 1 || store.view.closeCalls != 1 {
		t.Fatalf("apply calls = %d, close calls = %d", store.applyCalls, store.view.closeCalls)
	}
}

func TestMaintainStorageRejectsInvalidInputsAndCapabilitiesBeforeOpening(t *testing.T) {
	t.Parallel()

	valid := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(t, testStorageFacadeSnapshot(t), nil),
	}
	valid.capabilities |= StoreCapabilityAtomicMaintenance
	var nilContext context.Context
	var nilStore *internalMaintenanceStore
	tests := map[string]struct {
		ctx     context.Context
		profile Profile
		store   NodeMaintenanceStore
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
			_, err := MaintainStorage(test.ctx, test.profile, test.store, nil, test.limits)
			if !errors.Is(err, test.want) {
				t.Fatalf("MaintainStorage() error = %v, want %v", err, test.want)
			}
			if valid.openCalls != before {
				t.Fatalf("open calls changed from %d to %d", before, valid.openCalls)
			}
		})
	}

	for _, missing := range []StoreCapabilities{
		StoreCapabilityImmutableNodes,
		StoreCapabilitySnapshotReads,
		StoreCapabilityNodeInventory,
		StoreCapabilityAtomicMaintenance,
	} {
		store := &internalMaintenanceStore{
			internalAuditStore: newInternalAuditStore(t, testStorageFacadeSnapshot(t), nil),
		}
		store.capabilities = RequiredMaintenanceStoreCapabilities &^ missing
		_, err := MaintainStorage(
			context.Background(), ExperimentalBandersnatchIPA256V0(), store, nil,
			testInternalStorageAuditLimits(),
		)
		var capabilityErr *StoreCapabilityError
		if !errors.As(err, &capabilityErr) || capabilityErr.Missing != missing || store.openCalls != 0 {
			t.Fatalf("missing %d = (%v, open calls %d)", missing, err, store.openCalls)
		}
	}
}

func TestMaintainStorageRejectsUnboundProfileNamespaceBeforeOpening(t *testing.T) {
	t.Parallel()

	store := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(t, testStorageFacadeSnapshot(t), nil),
		invalidProfile:     true,
	}
	store.capabilities |= StoreCapabilityAtomicMaintenance
	result, err := MaintainStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(), store, nil,
		testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrUnsupportedProfile) || result.valid ||
		store.openCalls != 0 || store.applyCalls != 0 {
		t.Fatalf(
			"unbound namespace = (%+v, %v), open=%d apply=%d",
			result, err, store.openCalls, store.applyCalls,
		)
	}
}

func TestMaintainStorageRejectsInvalidRetentionSetsWithoutApplying(t *testing.T) {
	t.Parallel()

	oldSnapshot := testStorageFacadeSnapshot(t)
	var key Key
	key[0] = 11
	currentSnapshot, _, err := oldSnapshot.Apply(
		context.Background(), []Update{Set(key, Value{11})},
	)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	current := internalReaderFromSnapshot(t, currentSnapshot).view.publication
	old := internalReaderFromSnapshot(t, oldSnapshot).view.publication
	unknown := internalReaderFromSnapshot(t, testStorageFacadeSnapshotWithKey(t, 12)).view.publication
	tests := map[string][]StorePublication{
		"current":            {current},
		"unknown":            {unknown},
		"duplicate retained": {old, old},
	}
	for name, retained := range tests {
		t.Run(name, func(t *testing.T) {
			store := &internalMaintenanceStore{
				internalAuditStore: newInternalAuditStore(t, currentSnapshot, []Snapshot{oldSnapshot}),
			}
			store.capabilities |= StoreCapabilityAtomicMaintenance
			result, err := MaintainStorage(
				context.Background(), ExperimentalBandersnatchIPA256V0(), store,
				retained, testInternalStorageAuditLimits(),
			)
			if !errors.Is(err, ErrInvalidRetention) || result.valid || store.applyCalls != 0 {
				t.Fatalf("MaintainStorage() = (%+v, %v), apply calls = %d", result, err, store.applyCalls)
			}
			if store.view.closeCalls != 1 {
				t.Fatalf("close calls = %d, want 1", store.view.closeCalls)
			}
		})
	}
}

func TestMaintainStorageValidatesAndCopiesRequestedRetentionBeforeOpening(t *testing.T) {
	t.Parallel()

	snapshot := testStorageFacadeSnapshot(t)
	invalid := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(t, snapshot, nil),
	}
	invalid.capabilities |= StoreCapabilityAtomicMaintenance
	_, err := MaintainStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(), invalid,
		[]StorePublication{{}}, testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrInvalidRetention) || invalid.openCalls != 0 {
		t.Fatalf("malformed request error = %v, open calls = %d", err, invalid.openCalls)
	}

	var key Key
	key[0] = 62
	current, _, err := snapshot.Apply(
		context.Background(), []Update{Set(key, Value{62})},
	)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	store := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(t, current, []Snapshot{snapshot}),
	}
	store.capabilities |= StoreCapabilityAtomicMaintenance
	requested := []StorePublication{store.view.retained[0]}
	store.beforeOpen = func() {
		requested[0] = StorePublication{}
	}
	result, err := MaintainStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(), store,
		requested, testInternalStorageAuditLimits(),
	)
	if err != nil || result.RetainedCount() != 1 || store.applyCalls != 1 {
		t.Fatalf(
			"caller mutation during open = (%+v, %v), apply calls = %d",
			result, err, store.applyCalls,
		)
	}
}

func TestMaintainStoragePreservesLifecycleAndAtomicFailureSemantics(t *testing.T) {
	t.Parallel()

	snapshot := testStorageFacadeSnapshot(t)
	sentinel := errors.New("maintenance unavailable")

	openFailure := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(t, snapshot, nil),
	}
	openFailure.capabilities |= StoreCapabilityAtomicMaintenance
	openFailure.openErr = sentinel
	_, err := MaintainStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(), openFailure, nil,
		testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrStorageMaintenance) || !errors.Is(err, sentinel) || openFailure.applyCalls != 0 {
		t.Fatalf("open failure = %v, apply calls = %d", err, openFailure.applyCalls)
	}

	nilView := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(t, snapshot, nil),
	}
	nilView.capabilities |= StoreCapabilityAtomicMaintenance
	nilView.returnNil = true
	_, err = MaintainStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(), nilView, nil,
		testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrStorageMaintenance) || nilView.applyCalls != 0 {
		t.Fatalf("nil view failure = %v, apply calls = %d", err, nilView.applyCalls)
	}

	closeFailure := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(t, snapshot, nil),
	}
	closeFailure.capabilities |= StoreCapabilityAtomicMaintenance
	closeFailure.view.closeErr = sentinel
	_, err = MaintainStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(), closeFailure, nil,
		testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrStorageMaintenance) || !errors.Is(err, sentinel) || closeFailure.applyCalls != 0 {
		t.Fatalf("close failure = %v, apply calls = %d", err, closeFailure.applyCalls)
	}

	applyFailure := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(t, snapshot, nil), applyErr: ErrStaleRoot,
	}
	applyFailure.capabilities |= StoreCapabilityAtomicMaintenance
	result, err := MaintainStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(), applyFailure, nil,
		testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrStorageMaintenance) || !errors.Is(err, ErrStaleRoot) || result.valid ||
		applyFailure.applyCalls != 1 || !applyFailure.closedBeforeApply {
		t.Fatalf("apply failure = (%+v, %v), calls = %d", result, err, applyFailure.applyCalls)
	}
}

func TestMaintainStorageRejectsIncompleteInventoryAndDeletionBudget(t *testing.T) {
	t.Parallel()

	snapshot := testStorageFacadeSnapshot(t)
	omitted := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(t, snapshot, nil),
	}
	omitted.capabilities |= StoreCapabilityAtomicMaintenance
	omitted.view.omitLast = true
	_, err := MaintainStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(), omitted, nil,
		testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrStorageInventory) || omitted.applyCalls != 0 {
		t.Fatalf("omitted inventory error = %v, apply calls = %d", err, omitted.applyCalls)
	}

	orphaned := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(t, snapshot, nil),
	}
	orphaned.capabilities |= StoreCapabilityAtomicMaintenance
	orphaned.view.nodes[NodeID{0xfe}] = []byte("orphan")
	limits := testInternalStorageAuditLimits()
	limits.MaxUnreachableNodes = 0
	_, err = MaintainStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(), orphaned, nil, limits,
	)
	var resourceErr *ResourceError
	if !errors.As(err, &resourceErr) || resourceErr.Resource != ResourceUnreachableNodes ||
		resourceErr.Limit != 0 || resourceErr.Actual != 1 || orphaned.applyCalls != 0 {
		t.Fatalf("deletion budget error = %v, apply calls = %d", err, orphaned.applyCalls)
	}
}

func TestMaintainStorageCanonicalizesRetentionAndVerifiesDroppedSnapshots(t *testing.T) {
	t.Parallel()

	oldSnapshot := testStorageFacadeSnapshot(t)
	var middleKey Key
	middleKey[0] = 21
	middleSnapshot, _, err := oldSnapshot.Apply(
		context.Background(), []Update{Set(middleKey, Value{21})},
	)
	if err != nil {
		t.Fatalf("first Apply() error = %v", err)
	}
	var currentKey Key
	currentKey[0] = 22
	currentSnapshot, _, err := middleSnapshot.Apply(
		context.Background(), []Update{Set(currentKey, Value{22})},
	)
	if err != nil {
		t.Fatalf("second Apply() error = %v", err)
	}
	store := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(
			t, currentSnapshot, []Snapshot{oldSnapshot, middleSnapshot},
		),
	}
	store.capabilities |= StoreCapabilityAtomicMaintenance
	requested := []StorePublication{store.view.retained[1], store.view.retained[0]}
	if _, err := MaintainStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(), store,
		requested, testInternalStorageAuditLimits(),
	); err != nil {
		t.Fatalf("MaintainStorage() error = %v", err)
	}
	retained, err := store.request.RetainedPublications(context.Background())
	if err != nil || !slices.Equal(retained, store.view.retained) {
		t.Fatalf("canonical retained = (%+v, %v), want %+v", retained, err, store.view.retained)
	}

	corrupt := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(
			t, currentSnapshot, []Snapshot{oldSnapshot},
		),
	}
	corrupt.capabilities |= StoreCapabilityAtomicMaintenance
	droppedRootNode, _ := corrupt.view.retained[0].RootNode()
	corrupt.view.nodes[droppedRootNode][0] ^= 0xff
	_, err = MaintainStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(), corrupt, nil,
		testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrStorageNodeCorrupt) || corrupt.applyCalls != 0 {
		t.Fatalf("dropped corrupt snapshot error = %v, apply calls = %d", err, corrupt.applyCalls)
	}
}

func TestMaintainStorageSupportsNoCurrentPublicationAndRejectsMalformedPages(t *testing.T) {
	t.Parallel()

	noCurrent := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(t, testStorageFacadeSnapshot(t), nil),
	}
	noCurrent.capabilities |= StoreCapabilityAtomicMaintenance
	noCurrent.view.current = StorePublication{}
	noCurrent.view.currentOK = false
	noCurrent.view.nodes = map[NodeID][]byte{{0xfd}: []byte("abandoned")}
	result, err := MaintainStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(), noCurrent, nil,
		testInternalStorageAuditLimits(),
	)
	if err != nil || result.DeletedNodeCount() != 1 {
		t.Fatalf("no-current maintenance = (%+v, %v)", result, err)
	}
	if _, present, currentErr := noCurrent.request.CurrentPublication(); currentErr != nil || present {
		t.Fatalf("current expectation = (present %t, error %v)", present, currentErr)
	}

	malformed := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(t, testStorageFacadeSnapshot(t), nil),
	}
	malformed.capabilities |= StoreCapabilityAtomicMaintenance
	malformed.view.nodeIDsFn = func(*NodeID, uint32) ([]NodeID, bool, error) {
		return nil, true, nil
	}
	_, err = MaintainStorage(
		context.Background(), ExperimentalBandersnatchIPA256V0(), malformed, nil,
		testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrStorageInventory) || malformed.applyCalls != 0 {
		t.Fatalf("malformed page error = %v, apply calls = %d", err, malformed.applyCalls)
	}
}

func TestMaintainStorageStopsAfterCloseCancellation(t *testing.T) {
	t.Parallel()

	store := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(t, testStorageFacadeSnapshot(t), nil),
	}
	store.capabilities |= StoreCapabilityAtomicMaintenance
	ctx, cancel := context.WithCancel(context.Background())
	store.view.cancelAfterClose = cancel
	result, err := MaintainStorage(
		ctx, ExperimentalBandersnatchIPA256V0(), store, nil,
		testInternalStorageAuditLimits(),
	)
	if !errors.Is(err, ErrCancelled) || result.valid || store.applyCalls != 0 || store.view.closeCalls != 1 {
		t.Fatalf(
			"post-close cancellation = (%+v, %v), apply=%d close=%d",
			result, err, store.applyCalls, store.view.closeCalls,
		)
	}
}

func TestStoreMaintenanceAndResultZeroValuesRejectUse(t *testing.T) {
	t.Parallel()

	var maintenance StoreMaintenance
	if _, err := maintenance.Profile(); !errors.Is(err, ErrStorageMaintenance) {
		t.Fatalf("Profile() error = %v", err)
	}
	if _, _, err := maintenance.CurrentPublication(); !errors.Is(err, ErrStorageMaintenance) {
		t.Fatalf("CurrentPublication() error = %v", err)
	}
	for name, call := range map[string]func(context.Context) error{
		"previous": func(ctx context.Context) error {
			_, err := maintenance.PreviousRetainedPublications(ctx)
			return err
		},
		"retained": func(ctx context.Context) error {
			_, err := maintenance.RetainedPublications(ctx)
			return err
		},
		"deleted": func(ctx context.Context) error {
			_, err := maintenance.DeletedNodes(ctx)
			return err
		},
	} {
		if err := call(context.Background()); !errors.Is(err, ErrStorageMaintenance) {
			t.Fatalf("%s accessor error = %v", name, err)
		}
	}
	var result StorageMaintenanceResult
	if result.PreviousRetainedCount() != 0 || result.RetainedCount() != 0 || result.DeletedNodeCount() != 0 {
		t.Fatal("zero result reported nonzero counts")
	}
	if _, err := result.DeletedNodes(context.Background()); !errors.Is(err, ErrStorageMaintenance) {
		t.Fatalf("zero DeletedNodes() error = %v", err)
	}
}

type internalMaintenanceStore struct {
	*internalAuditStore
	request           StoreMaintenance
	applyCalls        int
	closedBeforeApply bool
	applyErr          error
	applyToView       bool
	invalidProfile    bool
}

func (store *internalMaintenanceStore) MaintenanceProfile() Profile {
	if store.invalidProfile {
		return Profile{}
	}

	return ExperimentalBandersnatchIPA256V0()
}

func (store *internalMaintenanceStore) ApplyMaintenance(
	ctx context.Context,
	request StoreMaintenance,
) error {
	store.applyCalls++
	store.closedBeforeApply = store.view.closeCalls == 1
	store.request = request
	if store.applyErr != nil || !store.applyToView {
		return store.applyErr
	}
	current, present, err := request.CurrentPublication()
	if err != nil || present != store.view.currentOK ||
		present && compareStorePublication(current, store.view.current) != 0 {
		return ErrStaleRoot
	}
	previous, err := request.PreviousRetainedPublications(ctx)
	if err != nil || !slices.Equal(previous, store.view.retained) {
		return ErrStaleRoot
	}
	retained, err := request.RetainedPublications(ctx)
	if err != nil {
		return err
	}
	deleted, err := request.DeletedNodes(ctx)
	if err != nil {
		return err
	}
	for _, id := range deleted {
		delete(store.view.nodes, id)
	}
	store.view.retained = retained

	return nil
}

func internalNodesOutsideSnapshots(
	t testing.TB,
	all map[NodeID][]byte,
	retained ...Snapshot,
) []NodeID {
	t.Helper()

	reachable := make(map[NodeID]struct{})
	for _, snapshot := range retained {
		reader := internalReaderFromSnapshot(t, snapshot)
		for id := range reader.view.nodes {
			reachable[id] = struct{}{}
		}
	}
	result := make([]NodeID, 0)
	for id := range all {
		if _, present := reachable[id]; !present {
			result = append(result, id)
		}
	}
	sort.Slice(result, func(left, right int) bool {
		return bytes.Compare(result[left][:], result[right][:]) < 0
	})

	return result
}

func testStorageFacadeSnapshotWithKey(t testing.TB, first byte) Snapshot {
	t.Helper()

	var key Key
	key[0] = first
	snapshot, err := NewSnapshot(
		context.Background(), ExperimentalBandersnatchIPA256V0(),
		[]Entry{{Key: key, Value: Value{first}}}, testFacadeSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}

	return snapshot
}
