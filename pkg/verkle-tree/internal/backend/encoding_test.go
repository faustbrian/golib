package backend

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
)

func TestCommitmentEncodingRoundTrip(t *testing.T) {
	t.Parallel()

	encoded, err := hex.DecodeString(
		"4a2c7486fd924882bf02c6908de395122843e3e05264d7991e18e7985dad51e9",
	)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	original := bytes.Clone(encoded)

	decoded, err := decodeCommitment(encoded)
	if err != nil {
		t.Fatalf("decode commitment: %v", err)
	}
	if !bytes.Equal(encoded, original) {
		t.Fatalf("decode commitment mutated input to %x", encoded)
	}

	got := encodeCommitment(decoded)
	if !bytes.Equal(got[:], original) {
		t.Fatalf("encode commitment = %x, want %x", got, original)
	}
}

func TestDecodeCommitmentRejectsIdentity(t *testing.T) {
	t.Parallel()

	_, err := decodeCommitment(make([]byte, commitmentSize))
	if !errors.Is(err, errInvalidCommitment) {
		t.Fatalf("decode identity commitment error = %v, want %v", err, errInvalidCommitment)
	}
}

func TestDecodeOpeningProofPointRejectsWrongLength(t *testing.T) {
	t.Parallel()

	_, err := decodeOpeningProofPoint(make([]byte, commitmentSize-1))
	if !errors.Is(err, errInvalidCommitment) {
		t.Fatalf("decode short proof point error = %v, want %v", err, errInvalidCommitment)
	}
}

func TestDecodeCommitmentRejectsMalformedEncodings(t *testing.T) {
	t.Parallel()

	wrongSubgroup, err := hex.DecodeString(
		"280e608d5bbbe84b16aac62aa450e8921840ea563f1c9c266e0240d89cbe6a78",
	)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	tests := map[string][]byte{
		"empty":          nil,
		"short":          make([]byte, commitmentSize-1),
		"trailing byte":  make([]byte, commitmentSize+1),
		"non-canonical":  bytes.Repeat([]byte{0xff}, commitmentSize),
		"wrong subgroup": wrongSubgroup,
	}

	for name, encoded := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := decodeCommitment(encoded)
			if !errors.Is(err, errInvalidCommitment) {
				t.Fatalf("decode commitment error = %v, want %v", err, errInvalidCommitment)
			}
		})
	}
}

func TestScalarEncodingRoundTrip(t *testing.T) {
	t.Parallel()

	encoded := make([]byte, scalarSize)
	encoded[0] = 1
	original := bytes.Clone(encoded)

	decoded, err := decodeScalar(encoded)
	if err != nil {
		t.Fatalf("decode scalar: %v", err)
	}
	if !bytes.Equal(encoded, original) {
		t.Fatalf("decode scalar mutated input to %x", encoded)
	}

	got := encodeScalar(decoded)
	if !bytes.Equal(got[:], original) {
		t.Fatalf("encode scalar = %x, want %x", got, original)
	}
}

func TestDecodeScalarRejectsMalformedEncodings(t *testing.T) {
	t.Parallel()

	modulus, err := hex.DecodeString(
		"e1e77628b506fd747104197400878fff007668020276ce0c525f67cad469fb1c",
	)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	tests := map[string][]byte{
		"empty":         nil,
		"short":         make([]byte, scalarSize-1),
		"trailing byte": make([]byte, scalarSize+1),
		"field modulus": modulus,
	}

	for name, encoded := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := decodeScalar(encoded)
			if !errors.Is(err, errInvalidScalar) {
				t.Fatalf("decode scalar error = %v, want %v", err, errInvalidScalar)
			}
		})
	}
}
