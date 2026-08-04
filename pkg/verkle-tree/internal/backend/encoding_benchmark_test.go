package backend

import (
	"context"
	"encoding/hex"
	"strconv"
	"testing"
)

var (
	benchmarkCommitment        commitment
	benchmarkEncodedCommitment [commitmentSize]byte
	benchmarkScalar            scalar
	benchmarkEncodedScalar     [scalarSize]byte
	benchmarkVectorCommitment  VectorCommitment
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

func BenchmarkCommitVectorSparse(b *testing.B) {
	benchmarkCommitVector(b, "sparse-boundaries")
}

func BenchmarkCommitVectorDense(b *testing.B) {
	benchmarkCommitVector(b, "dense-incrementing")
}

func BenchmarkUpdateCommitmentSparse(b *testing.B) {
	limits := testCommitmentLimits()
	limits.MaxScalarDecodes = 2 * VectorWidth
	engine, err := NewCommitmentEngine(context.Background(), limits)
	if err != nil {
		b.Fatal(err)
	}
	committed := EmptyVectorCommitment()
	for _, terms := range []int{1, 4, 16, 64, VectorWidth} {
		updates := make([]VectorUpdate, terms)
		for index := range updates {
			var vector Vector
			setVectorUint64(&vector, index, uint64(index+1))
			updates[index] = VectorUpdate{Index: uint8(index), New: vector[index]}
		}
		b.Run(strconv.Itoa(terms), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(terms * 2 * scalarSize))
			for b.Loop() {
				benchmarkVectorCommitment, err = engine.UpdateCommitment(
					context.Background(), committed, updates,
				)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func benchmarkCommitVector(b *testing.B, fixture string) {
	b.Helper()

	engine, err := NewCommitmentEngine(context.Background(), testCommitmentLimits())
	if err != nil {
		b.Fatal(err)
	}
	vector, _ := commitmentFixtureVector(b, fixture)
	b.ReportAllocs()
	b.SetBytes(VectorWidth * scalarSize)
	b.ResetTimer()

	for range b.N {
		benchmarkVectorCommitment, err = engine.Commit(context.Background(), vector)
		if err != nil {
			b.Fatal(err)
		}
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

func BenchmarkCommitmentToScalar(b *testing.B) {
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
		benchmarkScalar = commitmentToScalar(value)
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
