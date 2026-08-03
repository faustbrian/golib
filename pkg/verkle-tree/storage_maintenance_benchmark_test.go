package verkletree

import (
	"context"
	"testing"
)

func BenchmarkMaintainStorageDropRetainedAndPrune(b *testing.B) {
	oldSnapshot := testStorageFacadeSnapshot(b)
	var key Key
	key[0] = 9
	key[31] = 9
	currentSnapshot, _, err := oldSnapshot.Apply(
		context.Background(),
		[]Update{Set(key, Value{9})},
	)
	if err != nil {
		b.Fatalf("Apply() error = %v", err)
	}
	store := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(
			b, currentSnapshot, []Snapshot{oldSnapshot},
		),
	}
	store.capabilities |= StoreCapabilityAtomicMaintenance
	store.view.nodes[NodeID{0xff}] = []byte("unpublished node")
	profile := ExperimentalBandersnatchIPA256V0()
	limits := testInternalStorageAuditLimits()
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := MaintainStorage(
			context.Background(), profile, store, nil, limits,
		); err != nil {
			b.Fatalf("MaintainStorage() error = %v", err)
		}
	}
}

func BenchmarkRecoverStoragePreserveRetainedAndDeleteOrphan(b *testing.B) {
	oldSnapshot := testStorageFacadeSnapshotWithKey(b, 1)
	currentSnapshot := testStorageFacadeSnapshotWithKey(b, 2)
	store := &internalMaintenanceStore{
		internalAuditStore: newInternalAuditStore(
			b, currentSnapshot, []Snapshot{oldSnapshot},
		),
	}
	store.capabilities |= StoreCapabilityAtomicMaintenance
	store.view.nodes[NodeID{0xff}] = []byte("interrupted unpublished node")
	profile := ExperimentalBandersnatchIPA256V0()
	limits := testInternalStorageAuditLimits()
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := RecoverStorage(
			context.Background(), profile, store, limits,
		); err != nil {
			b.Fatalf("RecoverStorage() error = %v", err)
		}
	}
}
