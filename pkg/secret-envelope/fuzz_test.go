package secretenvelope

import (
	"bytes"
	"testing"
)

func FuzzParseEnvelope(f *testing.F) {
	encoded, err := validTestEnvelope().MarshalBinary()
	if err != nil {
		f.Fatalf("MarshalBinary() error = %v", err)
	}
	f.Add(encoded)
	f.Add([]byte("invalid"))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, candidate []byte) {
		envelope, err := ParseEnvelope(candidate)
		if err != nil {
			return
		}
		roundTrip, err := envelope.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary() error = %v", err)
		}
		if !bytes.Equal(roundTrip, candidate) {
			t.Fatalf("round trip changed a valid envelope")
		}
	})
}
