package backend

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func FuzzDecodeCommitment(f *testing.F) {
	f.Add([]byte(nil))
	f.Add(make([]byte, commitmentSize))
	f.Add(mustDecodeHex(
		f,
		"4a2c7486fd924882bf02c6908de395122843e3e05264d7991e18e7985dad51e9",
	))
	f.Add(mustDecodeHex(
		f,
		"280e608d5bbbe84b16aac62aa450e8921840ea563f1c9c266e0240d89cbe6a78",
	))

	f.Fuzz(func(t *testing.T, encoded []byte) {
		original := bytes.Clone(encoded)

		decoded, err := decodeCommitment(encoded)
		if !bytes.Equal(encoded, original) {
			t.Fatalf("decode commitment mutated input to %x", encoded)
		}
		if err != nil {
			return
		}

		canonical := encodeCommitment(decoded)
		roundTrip, err := decodeCommitment(canonical[:])
		if err != nil {
			t.Fatalf("decode canonical commitment: %v", err)
		}
		if got := encodeCommitment(roundTrip); got != canonical {
			t.Fatalf("commitment round trip = %x, want %x", got, canonical)
		}
	})
}

func FuzzDecodeScalar(f *testing.F) {
	f.Add([]byte(nil))
	f.Add(make([]byte, scalarSize))
	f.Add(mustDecodeHex(
		f,
		"e1e77628b506fd747104197400878fff007668020276ce0c525f67cad469fb1c",
	))

	f.Fuzz(func(t *testing.T, encoded []byte) {
		original := bytes.Clone(encoded)

		decoded, err := decodeScalar(encoded)
		if !bytes.Equal(encoded, original) {
			t.Fatalf("decode scalar mutated input to %x", encoded)
		}
		if err != nil {
			return
		}

		canonical := encodeScalar(decoded)
		roundTrip, err := decodeScalar(canonical[:])
		if err != nil {
			t.Fatalf("decode canonical scalar: %v", err)
		}
		if got := encodeScalar(roundTrip); got != canonical {
			t.Fatalf("scalar round trip = %x, want %x", got, canonical)
		}
	})
}

func mustDecodeHex(tb testing.TB, value string) []byte {
	tb.Helper()

	decoded, err := hex.DecodeString(value)
	if err != nil {
		tb.Fatalf("decode fixture: %v", err)
	}

	return decoded
}
