package authstate

import (
	"bytes"
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
)

func FuzzDecodeStatelessWitness(f *testing.F) {
	key := testKey(0x22, 0x44)
	snapshot := newTestSnapshot(f, []Entry{{Key: key, Value: testValue(1)}})
	proof, updater := newStatelessTestProof(f, snapshot, []Key{key})
	updates := []Update{Set(key, testValue(2))}
	postRoot, err := updater.Apply(
		context.Background(), proof, updates,
		testProofVerificationLimits(), testStatelessUpdateLimits(),
	)
	if err != nil {
		f.Fatalf("derive seed post-state root: %v", err)
	}
	witness, err := NewStatelessWitness(
		context.Background(), proof, updates, postRoot, testStatelessWitnessLimits(),
	)
	if err != nil {
		f.Fatalf("construct seed witness: %v", err)
	}
	encoded, err := witness.Bytes(
		context.Background(), testStatelessWitnessEncodingLimits(),
	)
	if err != nil {
		f.Fatalf("encode seed witness: %v", err)
	}
	f.Add(encoded)
	f.Add([]byte(nil))
	f.Add(bytes.Repeat([]byte{0xff}, statelessWitnessHeaderBytes))

	f.Fuzz(func(t *testing.T, candidate []byte) {
		decoded, decodeErr := DecodeStatelessWitness(
			context.Background(), candidate, testStatelessWitnessDecodingLimits(),
		)
		if decodeErr != nil {
			return
		}
		reencoded, encodeErr := decoded.Bytes(
			context.Background(), testStatelessWitnessEncodingLimits(),
		)
		if encodeErr != nil {
			t.Fatalf("re-encode accepted witness: %v", encodeErr)
		}
		if !bytes.Equal(reencoded, candidate) {
			t.Fatal("accepted witness did not round-trip canonically")
		}
	})
}

func FuzzApplyStatelessWitness(f *testing.F) {
	key := testKey(0x33, 0x55)
	snapshot := newTestSnapshot(f, []Entry{{Key: key, Value: testValue(1)}})
	proof, updater := newStatelessTestProof(f, snapshot, []Key{key})
	updates := []Update{Set(key, testValue(2))}
	postRoot, err := updater.Apply(
		context.Background(), proof, updates,
		testProofVerificationLimits(), testStatelessUpdateLimits(),
	)
	if err != nil {
		f.Fatalf("derive seed post-state root: %v", err)
	}
	valid := encodeStatelessWitnessFuzzSeed(f, proof, updates, postRoot)
	preRoot, err := proof.Root()
	if err != nil {
		f.Fatalf("read seed pre-state root: %v", err)
	}
	mismatched := encodeStatelessWitnessFuzzSeed(f, proof, updates, preRoot)
	f.Add(valid)
	f.Add(mismatched)
	f.Add([]byte(nil))

	f.Fuzz(func(t *testing.T, candidate []byte) {
		limits := testStatelessWitnessDecodingLimits()
		if uint64(len(candidate)) > limits.MaxWitnessBytes+1 {
			return
		}
		original := bytes.Clone(candidate)
		witness, decodeErr := DecodeStatelessWitness(
			context.Background(), candidate, limits,
		)
		if decodeErr != nil {
			return
		}
		root, applyErr := updater.ApplyWitness(
			context.Background(), witness,
			testProofVerificationLimits(), testStatelessUpdateLimits(),
		)
		if !bytes.Equal(candidate, original) {
			t.Fatal("witness application mutated caller input")
		}
		if bytes.Equal(candidate, mismatched) {
			if !IsStatelessPostRootMismatchError(applyErr) {
				t.Fatalf("mismatched post-root error = %v", applyErr)
			}
			return
		}
		if applyErr != nil {
			if bytes.Equal(candidate, valid) {
				t.Fatalf("apply valid witness: %v", applyErr)
			}
			return
		}
		got, gotErr := root.Bytes()
		if gotErr != nil {
			t.Fatalf("encode applied root: %v", gotErr)
		}
		wantRoot, wantErr := witness.PostRoot()
		if wantErr != nil {
			t.Fatalf("read accepted post-state root: %v", wantErr)
		}
		want, wantErr := wantRoot.Bytes()
		if wantErr != nil {
			t.Fatalf("encode accepted post-state root: %v", wantErr)
		}
		if got != want {
			t.Fatal("successful witness application returned a different post-state root")
		}
	})
}

func encodeStatelessWitnessFuzzSeed(
	tb testing.TB,
	proof TreeProof,
	updates []Update,
	postRoot backend.Root,
) []byte {
	tb.Helper()
	witness, err := NewStatelessWitness(
		context.Background(), proof, updates, postRoot, testStatelessWitnessLimits(),
	)
	if err != nil {
		tb.Fatalf("construct seed witness: %v", err)
	}
	encoded, err := witness.Bytes(
		context.Background(), testStatelessWitnessEncodingLimits(),
	)
	if err != nil {
		tb.Fatalf("encode seed witness: %v", err)
	}
	return encoded
}
