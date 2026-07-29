package backend

import (
	"context"
	"testing"
)

func BenchmarkDecodeOpeningProofCanonical(b *testing.B) {
	_, fixture := readMultiProofFixture(b)
	limits := testOpeningProofLimits()
	b.ReportAllocs()
	b.SetBytes(OpeningProofSize)
	b.ResetTimer()
	for range b.N {
		if _, err := DecodeOpeningProof(context.Background(), fixture, limits); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeOpeningProofWrongLength(b *testing.B) {
	_, fixture := readMultiProofFixture(b)
	fixture = fixture[:len(fixture)-1]
	limits := testOpeningProofLimits()
	b.ReportAllocs()
	b.SetBytes(int64(len(fixture)))
	b.ResetTimer()
	for range b.N {
		if _, err := DecodeOpeningProof(context.Background(), fixture, limits); err == nil {
			b.Fatal("accepted short proof")
		}
	}
}
