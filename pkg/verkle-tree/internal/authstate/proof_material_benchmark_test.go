package authstate

import (
	"context"
	"testing"
)

const benchmarkProofMaterialBytes = int64(16 * len(Key{}))

func BenchmarkSnapshotProofMaterialSixteen(b *testing.B) {
	entries := make([]Entry, 0, 8)
	keys := make([]Key, 16)
	for index := range keys {
		key := testKey(byte(15-index), byte(index))
		keys[index] = key
		if index%2 == 0 {
			entries = append(entries, Entry{Key: key, Value: testValue(byte(index))})
		}
	}
	snapshot := newTestSnapshot(b, entries)
	limits := testProofMaterialLimits()
	b.ReportAllocs()
	b.SetBytes(benchmarkProofMaterialBytes)
	b.ResetTimer()
	for range b.N {
		if _, err := snapshot.ProofMaterial(
			context.Background(),
			keys,
			limits,
		); err != nil {
			b.Fatal(err)
		}
	}
}
