package mpt

import (
	"bytes"
	"context"
	"testing"
)

func FuzzCanonicalNodeDecodeRoundTrip(f *testing.F) {
	f.Add([]byte{0xc2, 0x20, 0x01})
	f.Add([]byte{0xd1, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01})
	f.Add([]byte{0x80})

	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > 4096 {
			return
		}
		decoded, err := decodeNode(encoded)
		if err != nil || decoded == nil {
			return
		}
		budget := workBudget{hashesLeft: 4096}
		roundTrip, _, err := encodeNodeBounded(
			context.Background(), decoded, 4096, &budget,
		)
		if err != nil {
			t.Fatalf("encodeNodeBounded(decoded) error = %v", err)
		}
		if !bytes.Equal(roundTrip, encoded) {
			t.Fatalf("round trip = %x, want %x", roundTrip, encoded)
		}
	})
}
