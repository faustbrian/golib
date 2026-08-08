package verkletree

import (
	"context"
	"testing"
)

func BenchmarkLoadSnapshotFourEntries(b *testing.B) {
	var entries [4]Entry
	entries[0] = Entry{Key: Key{0: 1, 31: 0}, Value: Value{0: 1}}
	entries[1] = Entry{Key: Key{0: 1, 31: 128}, Value: Value{0: 2}}
	entries[2] = Entry{Key: Key{0: 2, 31: 1}, Value: Value{0: 3}}
	entries[3] = Entry{Key: Key{0: 2, 31: 129}, Value: Value{0: 4}}
	snapshot, err := NewSnapshot(
		context.Background(),
		BandersnatchIPA256V0(),
		entries[:],
		testFacadeSnapshotLimits(),
	)
	if err != nil {
		b.Fatalf("NewSnapshot() error = %v", err)
	}
	reader := internalReaderFromSnapshot(b, snapshot)
	totalBytes := 0
	for _, encoded := range reader.view.nodes {
		totalBytes += len(encoded)
	}
	warm := concurrentInternalStorageReader{
		publication: reader.view.publication,
		nodes:       reader.view.nodes,
	}

	b.Run("warm-caller-store", func(b *testing.B) {
		b.SetBytes(int64(totalBytes))
		b.ReportAllocs()
		for b.Loop() {
			if _, err := LoadSnapshot(
				context.Background(),
				BandersnatchIPA256V0(),
				warm,
				testInternalStorageReadLimits(),
			); err != nil {
				b.Fatalf("LoadSnapshot() error = %v", err)
			}
		}
	})

	b.Run("cold-caller-store", func(b *testing.B) {
		b.SetBytes(int64(totalBytes))
		b.ReportAllocs()
		for b.Loop() {
			owned := cloneInternalStorageReader(reader)
			cold := concurrentInternalStorageReader{
				publication: owned.view.publication,
				nodes:       owned.view.nodes,
			}
			if _, err := LoadSnapshot(
				context.Background(),
				BandersnatchIPA256V0(),
				cold,
				testInternalStorageReadLimits(),
			); err != nil {
				b.Fatalf("LoadSnapshot() error = %v", err)
			}
		}
	})
}
