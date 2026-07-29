package backend

import (
	"encoding/hex"
	"testing"
)

var (
	benchmarkCommitment        commitment
	benchmarkEncodedCommitment [commitmentSize]byte
	benchmarkScalar            scalar
	benchmarkEncodedScalar     [scalarSize]byte
)

func BenchmarkDecodeCommitmentCanonical(b *testing.B) {
	encoded := mustDecodeBenchmarkHex(
		b,
		"4a2c7486fd924882bf02c6908de395122843e3e05264d7991e18e7985dad51e9",
	)

	b.ReportAllocs()
	b.SetBytes(int64(len(encoded)))
	b.ResetTimer()

	for range b.N {
		value, err := decodeCommitment(encoded)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkCommitment = value
	}
}

func BenchmarkDecodeCommitmentIdentity(b *testing.B) {
	encoded := make([]byte, commitmentSize)

	b.ReportAllocs()
	b.SetBytes(int64(len(encoded)))
	b.ResetTimer()

	for range b.N {
		if _, err := decodeCommitment(encoded); err == nil {
			b.Fatal("identity commitment was accepted")
		}
	}
}

func BenchmarkEncodeCommitment(b *testing.B) {
	encoded := mustDecodeBenchmarkHex(
		b,
		"4a2c7486fd924882bf02c6908de395122843e3e05264d7991e18e7985dad51e9",
	)
	value, err := decodeCommitment(encoded)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.SetBytes(commitmentSize)
	b.ResetTimer()

	for range b.N {
		benchmarkEncodedCommitment = encodeCommitment(value)
	}
}

func BenchmarkDecodeScalarCanonical(b *testing.B) {
	encoded := make([]byte, scalarSize)
	encoded[0] = 1

	b.ReportAllocs()
	b.SetBytes(int64(len(encoded)))
	b.ResetTimer()

	for range b.N {
		value, err := decodeScalar(encoded)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkScalar = value
	}
}

func BenchmarkDecodeScalarNonCanonical(b *testing.B) {
	encoded := mustDecodeBenchmarkHex(
		b,
		"e1e77628b506fd747104197400878fff007668020276ce0c525f67cad469fb1c",
	)

	b.ReportAllocs()
	b.SetBytes(int64(len(encoded)))
	b.ResetTimer()

	for range b.N {
		if _, err := decodeScalar(encoded); err == nil {
			b.Fatal("non-canonical scalar was accepted")
		}
	}
}

func BenchmarkEncodeScalar(b *testing.B) {
	encoded := make([]byte, scalarSize)
	encoded[0] = 1
	value, err := decodeScalar(encoded)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.SetBytes(scalarSize)
	b.ResetTimer()

	for range b.N {
		benchmarkEncodedScalar = encodeScalar(value)
	}
}

func mustDecodeBenchmarkHex(b *testing.B, encoded string) []byte {
	b.Helper()

	decoded, err := hex.DecodeString(encoded)
	if err != nil {
		b.Fatal(err)
	}

	return decoded
}
