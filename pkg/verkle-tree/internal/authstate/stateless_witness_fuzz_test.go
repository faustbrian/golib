package authstate

import (
	"bytes"
	"context"
	"testing"
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
