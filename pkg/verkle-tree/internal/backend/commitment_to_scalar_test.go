package backend

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestCommitmentToScalarMatchesPinnedGeneratorVector(t *testing.T) {
	t.Parallel()

	encodedCommitment, err := hex.DecodeString(
		"4a2c7486fd924882bf02c6908de395122843e3e05264d7991e18e7985dad51e9",
	)
	if err != nil {
		t.Fatalf("decode commitment fixture: %v", err)
	}
	commitment, err := decodeCommitment(encodedCommitment)
	if err != nil {
		t.Fatalf("decode commitment: %v", err)
	}
	want, err := hex.DecodeString(
		"d1e7de2aaea9603d5bc6c208d319596376556ecd8336671ba7670c2139772d14",
	)
	if err != nil {
		t.Fatalf("decode scalar fixture: %v", err)
	}

	got := encodeScalar(commitmentToScalar(commitment))

	if !bytes.Equal(got[:], want) {
		t.Fatalf("commitment scalar = %x, want %x", got, want)
	}
}
