package backend

import (
	"context"
	"testing"
)

func BenchmarkDecodeRootCommitment(b *testing.B) {
	encoded := testEncodedRoot(b)
	limits := testRootLimits()
	b.ReportAllocs()
	b.SetBytes(RootSize)
	b.ResetTimer()
	for range b.N {
		if _, err := DecodeRoot(context.Background(), encoded, limits); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeRootEmpty(b *testing.B) {
	root, err := NewRoot(
		context.Background(),
		testProfile(),
		testIdentityCommitment(),
	)
	if err != nil {
		b.Fatal(err)
	}
	encoded, err := root.Bytes()
	if err != nil {
		b.Fatal(err)
	}
	limits := testRootLimits()
	limits.MaxPointDecodes = 0
	b.ReportAllocs()
	b.SetBytes(RootSize)
	b.ResetTimer()
	for range b.N {
		if _, decodeErr := DecodeRoot(
			context.Background(),
			encoded[:],
			limits,
		); decodeErr != nil {
			b.Fatal(decodeErr)
		}
	}
}

func BenchmarkDecodeRootWrongLength(b *testing.B) {
	encoded := testEncodedRoot(b)
	encoded = encoded[:len(encoded)-1]
	limits := testRootLimits()
	b.ReportAllocs()
	b.SetBytes(int64(len(encoded)))
	b.ResetTimer()
	for range b.N {
		if _, err := DecodeRoot(context.Background(), encoded, limits); err == nil {
			b.Fatal("accepted short root")
		}
	}
}
