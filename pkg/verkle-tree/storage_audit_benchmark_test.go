package verkletree

import (
	"context"
	"testing"
)

func BenchmarkAuditStorageCurrentAndRetainedSnapshots(b *testing.B) {
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
	store := newInternalAuditStore(b, currentSnapshot, []Snapshot{oldSnapshot})
	store.view.nodes[NodeID{0xff}] = []byte("unpublished node")
	profile := BandersnatchIPA256V0()
	limits := testInternalStorageAuditLimits()
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := AuditStorage(context.Background(), profile, store, limits); err != nil {
			b.Fatalf("AuditStorage() error = %v", err)
		}
	}
}
