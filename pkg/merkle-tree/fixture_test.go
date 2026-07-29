package merkletree_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	merkletree "github.com/faustbrian/golib/pkg/merkle-tree"
)

const rfc9162FixtureChecksum = "1ee115281c3a1861ef9e92e126e1f2bf7e043bc0ce4ada8b83a9d54d4ab885c7"

type rfc9162Fixture struct {
	Schema      uint64                     `json:"schema"`
	Profile     string                     `json:"profile"`
	Reference   rfc9162FixtureReference    `json:"reference"`
	Leaves      []string                   `json:"leaves_base64"`
	Roots       []string                   `json:"roots_hex"`
	Inclusion   []rfc9162InclusionVector   `json:"inclusion"`
	Consistency []rfc9162ConsistencyVector `json:"consistency"`
}

type rfc9162FixtureReference struct {
	Specification string `json:"specification"`
	Module        string `json:"module"`
	Version       string `json:"version"`
	Revision      string `json:"revision"`
}

type rfc9162InclusionVector struct {
	TreeSize uint64   `json:"tree_size"`
	Index    uint64   `json:"index"`
	Siblings []string `json:"siblings_hex"`
}

type rfc9162ConsistencyVector struct {
	OlderTreeSize uint64   `json:"older_tree_size"`
	NewerTreeSize uint64   `json:"newer_tree_size"`
	Nodes         []string `json:"nodes_hex"`
}

func TestRFC9162PinnedReferenceFixture(t *testing.T) {
	t.Parallel()

	encoded, err := os.ReadFile("testdata/rfc9162-sha256-v1.json")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	checksum := sha256.Sum256(encoded)
	if got := hex.EncodeToString(checksum[:]); got != rfc9162FixtureChecksum {
		t.Fatalf("fixture checksum = %s, want %s", got, rfc9162FixtureChecksum)
	}

	var fixture rfc9162Fixture
	if err := json.Unmarshal(encoded, &fixture); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if fixture.Schema != 1 ||
		fixture.Profile != "rfc9162-sha256-v1" ||
		fixture.Reference.Module != "github.com/transparency-dev/merkle" ||
		fixture.Reference.Version != "v0.0.2" ||
		fixture.Reference.Revision != "036047b5d2f7faf3b1ee643d391e60fe5b1defcf" {
		t.Fatalf("unexpected fixture identity: %#v", fixture)
	}

	raw := make([][]byte, len(fixture.Leaves))
	leaves := make([]merkletree.RawLeaf, len(fixture.Leaves))
	for index, encodedLeaf := range fixture.Leaves {
		leaf, decodeErr := base64.StdEncoding.DecodeString(encodedLeaf)
		if decodeErr != nil {
			t.Fatalf("DecodeString(leaf=%d) error = %v", index, decodeErr)
		}
		raw[index] = leaf
		leaves[index] = merkletree.NewRawLeaf(leaf)
	}

	ctx := context.Background()
	profile, err := merkletree.RFC9162Profile(merkletree.HashSHA256)
	if err != nil {
		t.Fatalf("RFC9162Profile() error = %v", err)
	}
	snapshots := make([]merkletree.Snapshot, len(leaves)+1)
	for size := range snapshots {
		snapshot, snapshotErr := merkletree.NewSnapshot(
			ctx,
			profile,
			leaves[:size],
			merkletree.DefaultSnapshotLimits(),
		)
		if snapshotErr != nil {
			t.Fatalf("NewSnapshot(size=%d) error = %v", size, snapshotErr)
		}
		snapshots[size] = snapshot
		root, rootErr := snapshot.Root()
		if rootErr != nil {
			t.Fatalf("Root(size=%d) error = %v", size, rootErr)
		}
		if got := hex.EncodeToString(root.Digest().Bytes()); got != fixture.Roots[size] {
			t.Fatalf("root(size=%d) = %s, fixture = %s", size, got, fixture.Roots[size])
		}
	}

	for _, vector := range fixture.Inclusion {
		proof, proofErr := snapshots[vector.TreeSize].InclusionProof(
			ctx,
			vector.Index,
			merkletree.DefaultProofLimits(),
		)
		if proofErr != nil {
			t.Fatalf(
				"InclusionProof(size=%d,index=%d) error = %v",
				vector.TreeSize,
				vector.Index,
				proofErr,
			)
		}
		assertEncodedDigests(t, proof.Siblings(), vector.Siblings)
		if verifyErr := merkletree.VerifyInclusion(
			ctx,
			proof,
			merkletree.NewRawLeaf(raw[vector.Index]),
			merkletree.DefaultProofLimits(),
		); verifyErr != nil {
			t.Fatalf(
				"VerifyInclusion(size=%d,index=%d) error = %v",
				vector.TreeSize,
				vector.Index,
				verifyErr,
			)
		}
	}

	for _, vector := range fixture.Consistency {
		olderRoot, olderErr := snapshots[vector.OlderTreeSize].Root()
		if olderErr != nil {
			t.Fatalf("Root(size=%d) error = %v", vector.OlderTreeSize, olderErr)
		}
		proof, proofErr := snapshots[vector.NewerTreeSize].ConsistencyProof(
			ctx,
			olderRoot,
			merkletree.DefaultConsistencyProofLimits(),
		)
		if proofErr != nil {
			t.Fatalf(
				"ConsistencyProof(%d,%d) error = %v",
				vector.OlderTreeSize,
				vector.NewerTreeSize,
				proofErr,
			)
		}
		assertEncodedDigests(t, proof.Nodes(), vector.Nodes)
		if verifyErr := merkletree.VerifyConsistency(
			ctx,
			proof,
			merkletree.DefaultConsistencyProofLimits(),
		); verifyErr != nil {
			t.Fatalf(
				"VerifyConsistency(%d,%d) error = %v",
				vector.OlderTreeSize,
				vector.NewerTreeSize,
				verifyErr,
			)
		}
	}
}

func assertEncodedDigests(t *testing.T, got []merkletree.Digest, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("digest count = %d, want %d", len(got), len(want))
	}
	for index := range got {
		expected, err := hex.DecodeString(want[index])
		if err != nil {
			t.Fatalf("DecodeString(digest=%d) error = %v", index, err)
		}
		if !bytes.Equal(got[index].Bytes(), expected) {
			t.Fatalf("digest[%d] = %x, want %x", index, got[index].Bytes(), expected)
		}
	}
}
