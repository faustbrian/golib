package backend

import (
	"bytes"
	"context"
	"testing"
)

func FuzzDecodeOpeningProof(f *testing.F) {
	_, fixture := readMultiProofFixture(f)
	f.Add(fixture)
	f.Add([]byte(nil))
	f.Add(make([]byte, OpeningProofSize))
	f.Add(bytes.Repeat([]byte{0xff}, OpeningProofSize))

	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > OpeningProofSize+1 {
			return
		}
		limits := testOpeningProofLimits()
		limits.MaxProofBytes = OpeningProofSize + 1
		proof, err := DecodeOpeningProof(context.Background(), encoded, limits)
		if err != nil {
			return
		}
		canonical, err := proof.Bytes()
		if err != nil {
			t.Fatalf("encode accepted proof: %v", err)
		}
		if !bytes.Equal(canonical[:], encoded) {
			t.Fatalf("accepted proof changed canonical bytes")
		}
		repeated, err := DecodeOpeningProof(context.Background(), encoded, limits)
		if err != nil {
			t.Fatalf("repeat accepted proof: %v", err)
		}
		repeatedBytes, err := repeated.Bytes()
		if err != nil {
			t.Fatalf("encode repeated proof: %v", err)
		}
		if canonical != repeatedBytes {
			t.Fatal("proof decoding is nondeterministic")
		}
	})
}
