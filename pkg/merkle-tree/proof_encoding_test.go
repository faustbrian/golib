package merkletree_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"testing"

	merkletree "github.com/faustbrian/golib/pkg/merkle-tree"
)

func TestProofCanonicalBinaryEncodingFixtures(t *testing.T) {
	t.Parallel()

	leaf := merkletree.NewRawLeaf([]byte("hello"))
	snapshot, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		[]merkletree.RawLeaf{leaf},
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	root, err := snapshot.Root()
	if err != nil {
		t.Fatalf("Root() error = %v", err)
	}
	inclusion, err := snapshot.InclusionProof(
		context.Background(),
		0,
		merkletree.DefaultProofLimits(),
	)
	if err != nil {
		t.Fatalf("InclusionProof() error = %v", err)
	}
	consistency, err := snapshot.ConsistencyProof(
		context.Background(),
		root,
		merkletree.DefaultConsistencyProofLimits(),
	)
	if err != nil {
		t.Fatalf("ConsistencyProof() error = %v", err)
	}
	multi, err := snapshot.MultiInclusionProof(
		context.Background(),
		[]uint64{0},
		merkletree.DefaultMultiProofLimits(),
	)
	if err != nil {
		t.Fatalf("MultiInclusionProof() error = %v", err)
	}

	const digest = "8a2a5c9b768827de5a9552c38a044c66959c68f6d2f21b5260af54d2f87db827"
	fixtures := []struct {
		name    string
		marshal func() ([]byte, error)
		wantHex string
	}{
		{
			name:    "inclusion",
			marshal: inclusion.MarshalBinary,
			wantHex: "4d545245010201000101" +
				"0000000000000001" + digest +
				"0000000000000000" + digest +
				"0000000000000000",
		},
		{
			name:    "consistency",
			marshal: consistency.MarshalBinary,
			wantHex: "4d545245010301000101" +
				"0000000000000001" + digest +
				"0000000000000001" + digest +
				"0000000000000000",
		},
		{
			name:    "multi",
			marshal: multi.MarshalBinary,
			wantHex: "4d545245010401000101" +
				"0000000000000001" + digest +
				"0000000000000001" +
				"0000000000000000" + digest +
				"0000000000000000",
		},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			got, marshalErr := fixture.marshal()
			if marshalErr != nil {
				t.Fatalf("MarshalBinary() error = %v", marshalErr)
			}
			want, decodeErr := hex.DecodeString(fixture.wantHex)
			if decodeErr != nil {
				t.Fatalf("DecodeString() error = %v", decodeErr)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("MarshalBinary() = %x, want %x", got, want)
			}
		})
	}

	inclusionBytes := mustMarshalBinary(t, inclusion.MarshalBinary)
	decodedInclusion, err := merkletree.ParseInclusionProof(
		context.Background(),
		inclusionBytes,
		merkletree.DefaultEncodingLimits(),
		merkletree.DefaultProofLimits(),
	)
	if err != nil {
		t.Fatalf("ParseInclusionProof() error = %v", err)
	}
	if err := merkletree.VerifyInclusion(
		context.Background(),
		decodedInclusion,
		leaf,
		merkletree.DefaultProofLimits(),
	); err != nil {
		t.Fatalf("VerifyInclusion(decoded) error = %v", err)
	}

	consistencyBytes := mustMarshalBinary(t, consistency.MarshalBinary)
	decodedConsistency, err := merkletree.ParseConsistencyProof(
		context.Background(),
		consistencyBytes,
		merkletree.DefaultEncodingLimits(),
		merkletree.DefaultConsistencyProofLimits(),
	)
	if err != nil {
		t.Fatalf("ParseConsistencyProof() error = %v", err)
	}
	if err := merkletree.VerifyConsistency(
		context.Background(),
		decodedConsistency,
		merkletree.DefaultConsistencyProofLimits(),
	); err != nil {
		t.Fatalf("VerifyConsistency(decoded) error = %v", err)
	}

	multiBytes := mustMarshalBinary(t, multi.MarshalBinary)
	decodedMulti, err := merkletree.ParseMultiInclusionProof(
		context.Background(),
		multiBytes,
		merkletree.DefaultEncodingLimits(),
		merkletree.DefaultMultiProofLimits(),
	)
	if err != nil {
		t.Fatalf("ParseMultiInclusionProof() error = %v", err)
	}
	if err := merkletree.VerifyMultiInclusion(
		context.Background(),
		decodedMulti,
		[]merkletree.RawLeaf{leaf},
		merkletree.DefaultMultiProofLimits(),
	); err != nil {
		t.Fatalf("VerifyMultiInclusion(decoded) error = %v", err)
	}
}

func TestProofDecodersRejectTruncatedTrailingAndWrongOperation(t *testing.T) {
	t.Parallel()

	proofs := encodedProofFixtures(t)
	for _, proof := range proofs {
		t.Run(proof.name, func(t *testing.T) {
			for _, data := range [][]byte{
				nil,
				proof.data[:len(proof.data)-1],
				append(append([]byte(nil), proof.data...), 0),
				mutateEncodingByte(proof.data, 5, 0xff),
			} {
				if err := proof.parse(data); err == nil {
					t.Fatalf("parser accepted %x", data)
				}
			}
		})
	}
}

type encodedProofFixture struct {
	name  string
	data  []byte
	parse func([]byte) error
}

func encodedProofFixtures(t testing.TB) []encodedProofFixture {
	t.Helper()

	leaves := consistencyLeaves(7)
	snapshot, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		leaves,
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	inclusion, err := snapshot.InclusionProof(
		context.Background(),
		3,
		merkletree.DefaultProofLimits(),
	)
	if err != nil {
		t.Fatalf("InclusionProof() error = %v", err)
	}
	older, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		leaves[:3],
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot(older) error = %v", err)
	}
	olderRoot, err := older.Root()
	if err != nil {
		t.Fatalf("older.Root() error = %v", err)
	}
	consistency, err := snapshot.ConsistencyProof(
		context.Background(),
		olderRoot,
		merkletree.DefaultConsistencyProofLimits(),
	)
	if err != nil {
		t.Fatalf("ConsistencyProof() error = %v", err)
	}
	multi, err := snapshot.MultiInclusionProof(
		context.Background(),
		[]uint64{1, 5},
		merkletree.DefaultMultiProofLimits(),
	)
	if err != nil {
		t.Fatalf("MultiInclusionProof() error = %v", err)
	}

	inclusionBytes := mustMarshalBinary(t, inclusion.MarshalBinary)
	consistencyBytes := mustMarshalBinary(t, consistency.MarshalBinary)
	multiBytes := mustMarshalBinary(t, multi.MarshalBinary)

	return []encodedProofFixture{
		{
			name: "inclusion",
			data: inclusionBytes,
			parse: func(data []byte) error {
				_, parseErr := merkletree.ParseInclusionProof(
					context.Background(),
					data,
					merkletree.DefaultEncodingLimits(),
					merkletree.DefaultProofLimits(),
				)

				return parseErr
			},
		},
		{
			name: "consistency",
			data: consistencyBytes,
			parse: func(data []byte) error {
				_, parseErr := merkletree.ParseConsistencyProof(
					context.Background(),
					data,
					merkletree.DefaultEncodingLimits(),
					merkletree.DefaultConsistencyProofLimits(),
				)

				return parseErr
			},
		},
		{
			name: "multi",
			data: multiBytes,
			parse: func(data []byte) error {
				_, parseErr := merkletree.ParseMultiInclusionProof(
					context.Background(),
					data,
					merkletree.DefaultEncodingLimits(),
					merkletree.DefaultMultiProofLimits(),
				)

				return parseErr
			},
		},
	}
}

func mustMarshalBinary(
	t testing.TB,
	marshal func() ([]byte, error),
) []byte {
	t.Helper()

	data, err := marshal()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}

	return data
}
