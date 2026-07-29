package merkletree_test

import (
	"bytes"
	"context"
	"testing"

	merkletree "github.com/faustbrian/golib/pkg/merkle-tree"
)

func FuzzParseRoot(f *testing.F) {
	f.Add(canonicalRootFixture(f))
	f.Add([]byte("not a root"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		root, err := merkletree.ParseRoot(
			data,
			merkletree.EncodingLimits{MaxBytes: 1 << 20},
		)
		if err != nil {
			return
		}
		encoded, err := root.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary() error = %v", err)
		}
		if !bytes.Equal(encoded, data) {
			t.Fatalf("non-canonical successful decode: %x != %x", encoded, data)
		}
	})
}

func FuzzParseInclusionProof(f *testing.F) {
	fixtures := encodedProofFixtures(f)
	f.Add(fixtures[0].data)
	f.Add([]byte("not an inclusion proof"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		proof, err := merkletree.ParseInclusionProof(
			context.Background(),
			data,
			merkletree.EncodingLimits{MaxBytes: 1 << 20},
			merkletree.DefaultProofLimits(),
		)
		assertCanonicalProofEncoding(t, data, proof.MarshalBinary, err)
	})
}

func FuzzParseConsistencyProof(f *testing.F) {
	fixtures := encodedProofFixtures(f)
	f.Add(fixtures[1].data)
	f.Add([]byte("not a consistency proof"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		proof, err := merkletree.ParseConsistencyProof(
			context.Background(),
			data,
			merkletree.EncodingLimits{MaxBytes: 1 << 20},
			merkletree.DefaultConsistencyProofLimits(),
		)
		assertCanonicalProofEncoding(t, data, proof.MarshalBinary, err)
	})
}

func FuzzParseMultiInclusionProof(f *testing.F) {
	fixtures := encodedProofFixtures(f)
	f.Add(fixtures[2].data)
	f.Add([]byte("not a multi-inclusion proof"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		proof, err := merkletree.ParseMultiInclusionProof(
			context.Background(),
			data,
			merkletree.EncodingLimits{MaxBytes: 1 << 20},
			merkletree.DefaultMultiProofLimits(),
		)
		assertCanonicalProofEncoding(t, data, proof.MarshalBinary, err)
	})
}

func FuzzParseSnapshot(f *testing.F) {
	snapshot, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		[]merkletree.RawLeaf{
			merkletree.NewRawLeaf([]byte("first")),
			merkletree.NewRawLeaf([]byte("second")),
		},
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		f.Fatalf("NewSnapshot() error = %v", err)
	}
	encoded, err := snapshot.MarshalBinary()
	if err != nil {
		f.Fatalf("MarshalBinary() error = %v", err)
	}
	f.Add(encoded)
	f.Add([]byte("not a snapshot"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		limits := merkletree.DefaultSnapshotPersistenceLimits()
		limits.MaxEncodedBytes = 1 << 20
		limits.MaxTemporaryBytes = 1 << 20
		decoded, err := merkletree.ParseSnapshot(
			context.Background(),
			data,
			limits,
		)
		assertCanonicalProofEncoding(t, data, decoded.MarshalBinary, err)
	})
}

func assertCanonicalProofEncoding(
	t *testing.T,
	data []byte,
	marshal func() ([]byte, error),
	parseErr error,
) {
	t.Helper()

	if parseErr != nil {
		return
	}
	encoded, err := marshal()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	if !bytes.Equal(encoded, data) {
		t.Fatalf("non-canonical successful decode: %x != %x", encoded, data)
	}
}

func canonicalRootFixture(t testing.TB) []byte {
	t.Helper()

	root, err := merkletree.ComputeRoot(
		context.Background(),
		merkletree.CanonicalProfile(),
		[]merkletree.RawLeaf{merkletree.NewRawLeaf([]byte("fuzz"))},
		merkletree.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("ComputeRoot() error = %v", err)
	}
	data, err := root.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}

	return data
}
