package authstate

import (
	"context"
	"testing"
)

func BenchmarkEncodeStatelessWitness(b *testing.B) {
	witness, _, _ := benchmarkStatelessWitness(b)
	limits := testStatelessWitnessEncodingLimits()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := witness.Bytes(context.Background(), limits); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeStatelessWitness(b *testing.B) {
	_, encoded, _ := benchmarkStatelessWitness(b)
	limits := testStatelessWitnessDecodingLimits()
	b.ReportAllocs()
	b.SetBytes(int64(len(encoded)))
	b.ResetTimer()
	for range b.N {
		if _, err := DecodeStatelessWitness(context.Background(), encoded, limits); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkApplyStatelessWitness(b *testing.B) {
	witness, _, updater := benchmarkStatelessWitness(b)
	verificationLimits := testProofVerificationLimits()
	updateLimits := testStatelessUpdateLimits()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := updater.ApplyWitness(
			context.Background(), witness, verificationLimits, updateLimits,
		); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkStatelessWitness(
	b testing.TB,
) (StatelessWitness, []byte, *StatelessUpdater) {
	b.Helper()

	key := testKey(0x11, 0x22)
	snapshot := newTestSnapshot(b, []Entry{{Key: key, Value: testValue(1)}})
	proof, updater := newStatelessTestProof(b, snapshot, []Key{key})
	updates := []Update{Set(key, testValue(2))}
	postRoot, err := updater.Apply(
		context.Background(), proof, updates,
		testProofVerificationLimits(), testStatelessUpdateLimits(),
	)
	if err != nil {
		b.Fatalf("derive post-state root: %v", err)
	}
	witness, err := NewStatelessWitness(
		context.Background(), proof, updates, postRoot, testStatelessWitnessLimits(),
	)
	if err != nil {
		b.Fatalf("construct witness: %v", err)
	}
	encoded, err := witness.Bytes(
		context.Background(), testStatelessWitnessEncodingLimits(),
	)
	if err != nil {
		b.Fatalf("encode witness: %v", err)
	}

	return witness, encoded, updater
}
