package rlp

import (
	"bytes"
	"testing"
)

func FuzzCanonicalDecodeRoundTrip(f *testing.F) {
	f.Add([]byte{0x80})
	f.Add([]byte{0x01})
	f.Add([]byte{0xc2, 0x01, 0x02})
	f.Add([]byte{0x81, 0x01})

	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > 4096 {
			return
		}
		limits := Limits{
			MaxEncodedBytes: 4096,
			MaxDepth:        64,
			MaxItems:        1024,
		}
		decoded, err := Decode(encoded, limits)
		if err != nil {
			return
		}
		roundTrip, err := Encode(decoded, limits)
		if err != nil {
			t.Fatalf("Encode(decoded) error = %v", err)
		}
		if !bytes.Equal(roundTrip, encoded) {
			t.Fatalf("round trip = %x, want %x", roundTrip, encoded)
		}
	})
}
