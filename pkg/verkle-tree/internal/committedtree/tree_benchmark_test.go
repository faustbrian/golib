package committedtree

import (
	"context"
	"testing"
)

var benchmarkTree Tree
var benchmarkProofPath ProofPath
var benchmarkStorageImage StorageImage

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

func BenchmarkProofPath(b *testing.B) {
	key := testKey(0, 0)
	other := testKey(0, 1)
	other[1] = 1
	tree, err := Build(
		context.Background(),
		[]Entry{
			{Key: key, Value: testValue(1)},
			{Key: other, Value: testValue(2)},
		},
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		b.Fatalf("build proof tree: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchmarkProofPath, err = tree.ProofPath(
			context.Background(),
			key,
			testProofPathLimits(),
		)
		if err != nil {
			b.Fatalf("extract proof path: %v", err)
		}
	}
}

func BenchmarkStorageImage(b *testing.B) {
	entries := []Entry{
		{Key: testKey(0x00, 0x00), Value: testValue(0x11)},
		{Key: testKey(0x00, 0x80), Value: testValue(0x22)},
		{Key: testKey(0x01, 0xff), Value: testValue(0x33)},
		{Key: testKey(0xff, 0x7f), Value: testValue(0x44)},
	}
	tree, err := Build(
		context.Background(),
		entries,
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		b.Fatalf("build storage tree: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchmarkStorageImage, err = tree.StorageImage(
			context.Background(),
			testStorageEncodingLimits(),
		)
		if err != nil {
			b.Fatalf("encode storage image: %v", err)
		}
	}
}
