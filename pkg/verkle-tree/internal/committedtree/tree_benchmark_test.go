package committedtree

import (
	"context"
	"testing"
)

var benchmarkTree Tree

func BenchmarkBuildFourEntries(b *testing.B) {
	builder, err := NewBuilder(
		context.Background(),
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		b.Fatalf("new builder: %v", err)
	}
	entries := []Entry{
		{Key: testKey(0x00, 0x00), Value: testValue(0x11)},
		{Key: testKey(0x00, 0x01), Value: testValue(0x22)},
		{Key: testKey(0x01, 0xff), Value: testValue(0x33)},
		{Key: testKey(0x01, 0x7f), Value: testValue(0x44)},
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(entries) * 64))
	b.ResetTimer()
	for range b.N {
		benchmarkTree, err = builder.Build(context.Background(), entries)
		if err != nil {
			b.Fatalf("build tree: %v", err)
		}
	}
}
