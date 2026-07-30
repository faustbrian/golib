package authstate

import (
	"context"
	"testing"
)

func BenchmarkProofEngine(b *testing.B) {
	entries := make([]Entry, 32)
	keys := make([]Key, 16)
	for index := range entries {
		entries[index] = Entry{
			Key:   testKey(byte(index), byte(index)),
			Value: testValue(byte(index + 1)),
		}
		if index < len(keys) {
			keys[index] = testKey(byte(index), byte(index))
		}
	}
	snapshot := newTestSnapshot(b, entries)
	engine := newTestProofEngine(b)
	limits := testProofGenerationLimits()
	limits.TreeProof.MaxTemporaryBytes = 1 << 20
	proof, err := engine.Prove(
		context.Background(),
		snapshot,
		keys,
		limits,
	)
	if err != nil {
		b.Fatalf("prepare proof: %v", err)
	}

	b.Run("generate-16", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := engine.Prove(
				context.Background(),
				snapshot,
				keys,
				limits,
			); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("verify-16", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := engine.Verify(
				context.Background(),
				proof,
				testProofVerificationLimits(),
			); err != nil {
				b.Fatal(err)
			}
		}
	})
}
