package authstate

import (
	"context"
	"testing"
)

func BenchmarkEncodeTreeProof(b *testing.B) {
	proof, encoded := testCanonicalEncodedTreeProof(b)
	limits := testTreeProofEncodingLimits()
	b.SetBytes(int64(len(encoded)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := proof.Bytes(context.Background(), limits); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeTreeProof(b *testing.B) {
	_, encoded := testCanonicalEncodedTreeProof(b)
	limits := testTreeProofDecodingLimits()
	b.SetBytes(int64(len(encoded)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := DecodeTreeProof(
			context.Background(),
			encoded,
			limits,
		); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeTreeProofWrongLength(b *testing.B) {
	_, encoded := testCanonicalEncodedTreeProof(b)
	encoded = encoded[:len(encoded)-1]
	limits := testTreeProofDecodingLimits()
	b.SetBytes(int64(len(encoded)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := DecodeTreeProof(
			context.Background(),
			encoded,
			limits,
		); err == nil {
			b.Fatal("wrong-length proof accepted")
		}
	}
}
