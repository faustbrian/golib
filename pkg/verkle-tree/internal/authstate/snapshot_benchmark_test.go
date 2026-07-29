package authstate

import (
	"context"
	"testing"
)

func BenchmarkSnapshotGet(b *testing.B) {
	snapshot := newTestSnapshot(b, []Entry{{Key: testKey(1, 1), Value: testValue(1)}})
	key := testKey(1, 1)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, _, err := snapshot.Get(context.Background(), key); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSnapshotApply(b *testing.B) {
	snapshot := newTestSnapshot(b, []Entry{{Key: testKey(1, 1), Value: testValue(1)}})
	updates := []Update{Set(testKey(1, 1), testValue(2))}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, _, err := snapshot.Apply(context.Background(), updates); err != nil {
			b.Fatal(err)
		}
	}
}
