package authstate

import (
	"bytes"
	"context"
	"testing"
)

func FuzzDecodeTreeProof(f *testing.F) {
	_, canonical := testCanonicalEncodedTreeProof(f)
	f.Add(canonical)
	f.Add([]byte{})
	f.Add(bytes.Repeat([]byte{0xff}, treeProofFixedBytes))

	f.Fuzz(func(t *testing.T, encoded []byte) {
		limits := testTreeProofDecodingLimits()
		if uint64(len(encoded)) > limits.MaxProofBytes+1 {
			return
		}
		original := bytes.Clone(encoded)

		proof, err := DecodeTreeProof(
			context.Background(),
			encoded,
			limits,
		)
		if err != nil {
			return
		}
		if !bytes.Equal(encoded, original) {
			t.Fatal("decoder mutated caller input")
		}
		reencoded, err := proof.Bytes(
			context.Background(),
			testTreeProofEncodingLimits(),
		)
		if err != nil {
			t.Fatalf("encode accepted proof: %v", err)
		}
		if !bytes.Equal(reencoded, original) {
			t.Fatal("accepted proof did not preserve canonical bytes")
		}
		if _, err := DecodeTreeProof(
			context.Background(),
			reencoded,
			limits,
		); err != nil {
			t.Fatalf("decode repeated canonical proof: %v", err)
		}
	})
}
