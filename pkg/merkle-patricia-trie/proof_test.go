package mpt_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

func TestRawMembershipAndAbsenceProofs(t *testing.T) {
	t.Parallel()

	trie := mustRawTrie(t, map[string]string{
		"do":    "verb",
		"dog":   "puppy",
		"horse": "stallion",
	})
	root := mustTrieRoot(t, trie)

	membership, err := trie.Prove(context.Background(), []byte("dog"))
	if err != nil {
		t.Fatalf("Prove(membership) error = %v", err)
	}
	if err := mpt.VerifyRawMembership(
		context.Background(),
		root,
		[]byte("dog"),
		[]byte("puppy"),
		membership,
		mpt.DefaultLimits(),
	); err != nil {
		t.Fatalf("VerifyRawMembership() error = %v", err)
	}

	absence, err := trie.Prove(context.Background(), []byte("cat"))
	if err != nil {
		t.Fatalf("Prove(absence) error = %v", err)
	}
	if err := mpt.VerifyRawAbsence(
		context.Background(),
		root,
		[]byte("cat"),
		absence,
		mpt.DefaultLimits(),
	); err != nil {
		t.Fatalf("VerifyRawAbsence() error = %v", err)
	}
}

func TestProofVerificationBindsClaimRootKeyValueAndProfile(t *testing.T) {
	t.Parallel()

	trie := mustRawTrie(t, map[string]string{"key": "value"})
	root := mustTrieRoot(t, trie)
	proof, err := trie.Prove(context.Background(), []byte("key"))
	if err != nil {
		t.Fatalf("Prove() error = %v", err)
	}
	otherRoot := root
	otherRoot[0] ^= 0xff

	tests := []struct {
		name string
		run  func() error
		want error
	}{
		{
			name: "wrong root",
			run: func() error {
				return mpt.VerifyRawMembership(
					context.Background(), otherRoot, []byte("key"),
					[]byte("value"), proof, mpt.DefaultLimits(),
				)
			},
			want: mpt.ErrWrongRoot,
		},
		{
			name: "wrong key",
			run: func() error {
				return mpt.VerifyRawMembership(
					context.Background(), root, []byte("other"),
					[]byte("value"), proof, mpt.DefaultLimits(),
				)
			},
			want: mpt.ErrFailedProof,
		},
		{
			name: "wrong value",
			run: func() error {
				return mpt.VerifyRawMembership(
					context.Background(), root, []byte("key"),
					[]byte("other"), proof, mpt.DefaultLimits(),
				)
			},
			want: mpt.ErrFailedProof,
		},
		{
			name: "membership as absence",
			run: func() error {
				return mpt.VerifyRawAbsence(
					context.Background(), root, []byte("key"),
					proof, mpt.DefaultLimits(),
				)
			},
			want: mpt.ErrFailedProof,
		},
		{
			name: "wrong secure profile",
			run: func() error {
				return mpt.VerifySecureMembership(
					context.Background(), root, []byte("key"),
					[]byte("value"), proof, mpt.DefaultLimits(),
				)
			},
			want: mpt.ErrFailedProof,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.run(); !errors.Is(err, test.want) {
				t.Fatalf("verification error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestProofRejectsMissingMutatedReorderedAndSurplusNodes(t *testing.T) {
	t.Parallel()

	trie := mustRawTrie(t, map[string]string{
		"alpha":  "a long value that forces hashed child references 1",
		"alpine": "a long value that forces hashed child references 2",
		"beta":   "a long value that forces hashed child references 3",
	})
	root := mustTrieRoot(t, trie)
	proof, err := trie.Prove(context.Background(), []byte("alpha"))
	if err != nil {
		t.Fatalf("Prove() error = %v", err)
	}
	nodes := proof.Nodes()
	if len(nodes) < 2 {
		t.Fatalf("proof has %d nodes, need a multi-node vector", len(nodes))
	}

	missing, err := mpt.ProofFromNodes(nodes[:len(nodes)-1], mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("ProofFromNodes(missing) error = %v", err)
	}
	if err := mpt.VerifyRawMembership(
		context.Background(), root, []byte("alpha"),
		[]byte("a long value that forces hashed child references 1"),
		missing, mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrIncompleteProof) {
		t.Fatalf("missing proof error = %v, want ErrIncompleteProof", err)
	}

	mutatedNodes := proof.Nodes()
	mutatedNodes[0][len(mutatedNodes[0])-1] ^= 0xff
	mutated, err := mpt.ProofFromNodes(mutatedNodes, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("ProofFromNodes(mutated) error = %v", err)
	}
	if err := mpt.VerifyRawMembership(
		context.Background(), root, []byte("alpha"),
		[]byte("a long value that forces hashed child references 1"),
		mutated, mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrWrongRoot) {
		t.Fatalf("mutated proof error = %v, want ErrWrongRoot", err)
	}

	reorderedNodes := proof.Nodes()
	reorderedNodes[0], reorderedNodes[1] = reorderedNodes[1], reorderedNodes[0]
	reordered, err := mpt.ProofFromNodes(reorderedNodes, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("ProofFromNodes(reordered) error = %v", err)
	}
	if err := mpt.VerifyRawMembership(
		context.Background(), root, []byte("alpha"),
		[]byte("a long value that forces hashed child references 1"),
		reordered, mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrWrongRoot) {
		t.Fatalf("reordered proof error = %v, want ErrWrongRoot", err)
	}

	surplusNodes := proof.Nodes()
	surplusNodes = append(surplusNodes, append([]byte(nil), surplusNodes[0]...))
	surplus, err := mpt.ProofFromNodes(surplusNodes, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("ProofFromNodes(surplus) error = %v", err)
	}
	if err := mpt.VerifyRawMembership(
		context.Background(), root, []byte("alpha"),
		[]byte("a long value that forces hashed child references 1"),
		surplus, mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrMalformedProof) {
		t.Fatalf("surplus proof error = %v, want ErrMalformedProof", err)
	}
}

func TestProofNodesAreOwnedAndEmbeddedChildrenAreDeduplicated(t *testing.T) {
	t.Parallel()

	trie := mustRawTrie(t, map[string]string{"a": "1", "b": "2"})
	proof, err := trie.Prove(context.Background(), []byte("a"))
	if err != nil {
		t.Fatalf("Prove() error = %v", err)
	}
	nodes := proof.Nodes()
	if len(nodes) != 1 {
		t.Fatalf("small proof nodes = %d, want only root", len(nodes))
	}
	original := append([]byte(nil), nodes[0]...)
	nodes[0][0] ^= 0xff
	if got := proof.Nodes()[0]; !slices.Equal(got, original) {
		t.Fatal("Proof.Nodes() aliases proof storage")
	}

	transport, err := mpt.ProofFromNodes([][]byte{original}, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("ProofFromNodes() error = %v", err)
	}
	original[0] ^= 0xff
	if transport.Nodes()[0][0] == original[0] {
		t.Fatal("ProofFromNodes() retained caller bytes")
	}
}

func TestEmptyTrieAbsenceProofAndProofLimits(t *testing.T) {
	t.Parallel()

	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	proof, err := trie.Prove(context.Background(), []byte("key"))
	if err != nil {
		t.Fatalf("Prove() error = %v", err)
	}
	if len(proof.Nodes()) != 0 {
		t.Fatalf("empty proof has %d nodes", len(proof.Nodes()))
	}
	if err := mpt.VerifyRawAbsence(
		context.Background(), mpt.EmptyRoot(), []byte("key"),
		proof, mpt.DefaultLimits(),
	); err != nil {
		t.Fatalf("VerifyRawAbsence(empty) error = %v", err)
	}

	limits := mpt.DefaultLimits()
	limits.MaxProofNodes = 1
	limits.MaxProofBytes = 1
	if _, err := mpt.ProofFromNodes([][]byte{{0x80}, {0x80}}, limits); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("ProofFromNodes(node limit) error = %v, want ErrResourceLimit", err)
	}
	if _, err := mpt.ProofFromNodes([][]byte{{0x81, 0x80}}, limits); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("ProofFromNodes(byte limit) error = %v, want ErrResourceLimit", err)
	}
}
