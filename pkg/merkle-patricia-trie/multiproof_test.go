package mpt_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

func TestRawMultiProofDeduplicatesSharedNodesAndBindsAllClaims(t *testing.T) {
	t.Parallel()
	trie := mustRawTrie(t, map[string]string{
		"alpha":  "a long value that forces hashed child references 1",
		"alpine": "a long value that forces hashed child references 2",
		"beta":   "a long value that forces hashed child references 3",
		"delta":  "a long value that forces hashed child references 4",
	})
	root := mustTrieRoot(t, trie)
	keys := [][]byte{[]byte("missing"), []byte("alpine"), []byte("alpha")}
	proof, err := trie.ProveMany(context.Background(), keys)
	if err != nil {
		t.Fatalf("ProveMany() error = %v", err)
	}
	claims := []mpt.ProofClaim{
		mpt.AbsenceClaim([]byte("missing")),
		mpt.MembershipClaim(
			[]byte("alpha"),
			[]byte("a long value that forces hashed child references 1"),
		),
		mpt.MembershipClaim(
			[]byte("alpine"),
			[]byte("a long value that forces hashed child references 2"),
		),
	}
	if err := mpt.VerifyRawMultiProof(
		context.Background(), root, claims, proof, mpt.DefaultLimits(),
	); err != nil {
		t.Fatalf("VerifyRawMultiProof() error = %v", err)
	}

	totalSingleNodes := 0
	for _, key := range keys {
		single, proveErr := trie.Prove(context.Background(), key)
		if proveErr != nil {
			t.Fatalf("Prove(%q) error = %v", key, proveErr)
		}
		totalSingleNodes += len(single.Nodes())
	}
	if got := len(proof.Nodes()); got >= totalSingleNodes {
		t.Fatalf(
			"multi-proof nodes = %d, individual total = %d; shared nodes not deduplicated",
			got,
			totalSingleNodes,
		)
	}

	reordered, err := trie.ProveMany(
		context.Background(),
		[][]byte{[]byte("alpha"), []byte("missing"), []byte("alpine")},
	)
	if err != nil {
		t.Fatalf("ProveMany(reordered) error = %v", err)
	}
	if !equalNodeSlices(proof.Nodes(), reordered.Nodes()) {
		t.Fatal("multi-proof encoding depends on caller key order")
	}
}

func TestSecureMultiProofBindsProfileAndClaims(t *testing.T) {
	t.Parallel()
	trie, err := mpt.NewSecureTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewSecureTrie() error = %v", err)
	}
	for key, value := range map[string]string{
		"alpha": "secure alpha value", "beta": "secure beta value",
	} {
		trie, err = trie.Update(
			context.Background(), []byte(key), []byte(value),
		)
		if err != nil {
			t.Fatalf("Update(%q) error = %v", key, err)
		}
	}
	root, err := trie.Root()
	if err != nil {
		t.Fatalf("Root() error = %v", err)
	}
	proof, err := trie.ProveMany(
		context.Background(), [][]byte{[]byte("absent"), []byte("alpha")},
	)
	if err != nil {
		t.Fatalf("ProveMany() error = %v", err)
	}
	claims := []mpt.ProofClaim{
		mpt.MembershipClaim([]byte("alpha"), []byte("secure alpha value")),
		mpt.AbsenceClaim([]byte("absent")),
	}
	if err := mpt.VerifySecureMultiProof(
		context.Background(), root, claims, proof, mpt.DefaultLimits(),
	); err != nil {
		t.Fatalf("VerifySecureMultiProof() error = %v", err)
	}
	if err := mpt.VerifyRawMultiProof(
		context.Background(), root, claims, proof, mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrFailedProof) {
		t.Fatalf("wrong-profile error = %v, want ErrFailedProof", err)
	}
}

func TestMultiProofRejectsMissingDuplicateReorderedAndSurplusNodes(t *testing.T) {
	t.Parallel()
	trie := mustRawTrie(t, map[string]string{
		"alpha":  "a long value that forces hashed child references 1",
		"alpine": "a long value that forces hashed child references 2",
		"beta":   "a long value that forces hashed child references 3",
	})
	root := mustTrieRoot(t, trie)
	proof, err := trie.ProveMany(
		context.Background(), [][]byte{[]byte("alpha"), []byte("beta")},
	)
	if err != nil {
		t.Fatalf("ProveMany() error = %v", err)
	}
	nodes := proof.Nodes()
	if len(nodes) < 3 {
		t.Fatalf("multi-proof node count = %d, need at least 3", len(nodes))
	}
	claims := []mpt.ProofClaim{
		mpt.MembershipClaim(
			[]byte("alpha"),
			[]byte("a long value that forces hashed child references 1"),
		),
		mpt.MembershipClaim(
			[]byte("beta"),
			[]byte("a long value that forces hashed child references 3"),
		),
	}
	verify := func(t *testing.T, changed [][]byte, want error) {
		t.Helper()
		transport, transportErr := mpt.MultiProofFromNodes(
			changed, mpt.DefaultLimits(),
		)
		if transportErr != nil {
			t.Fatalf("MultiProofFromNodes() error = %v", transportErr)
		}
		if err := mpt.VerifyRawMultiProof(
			context.Background(), root, claims, transport, mpt.DefaultLimits(),
		); !errors.Is(err, want) {
			t.Fatalf("verification error = %v, want %v", err, want)
		}
	}

	verify(t, nodes[:len(nodes)-1], mpt.ErrIncompleteProof)
	duplicated := append(proof.Nodes(), append([]byte(nil), nodes[0]...))
	verify(t, duplicated, mpt.ErrMalformedProof)
	reordered := proof.Nodes()
	reordered[1], reordered[2] = reordered[2], reordered[1]
	verify(t, reordered, mpt.ErrMalformedProof)
	surplusSingle, err := trie.Prove(context.Background(), []byte("alpine"))
	if err != nil {
		t.Fatalf("Prove(surplus) error = %v", err)
	}
	surplus := proof.Nodes()
	for _, encoded := range surplusSingle.Nodes() {
		if !containsNode(surplus, encoded) {
			surplus = append(surplus, encoded)
		}
	}
	verify(t, surplus, mpt.ErrMalformedProof)

	wrongRoot := root
	wrongRoot[0] ^= 0xff
	if err := mpt.VerifyRawMultiProof(
		context.Background(), wrongRoot, claims, proof, mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrWrongRoot) {
		t.Fatalf("wrong-root error = %v, want ErrWrongRoot", err)
	}
}

func TestMultiProofClaimsAndNodesOwnCallerBytes(t *testing.T) {
	t.Parallel()
	trie := mustRawTrie(t, map[string]string{"alpha": "value"})
	root := mustTrieRoot(t, trie)
	key := []byte("alpha")
	value := []byte("value")
	claim := mpt.MembershipClaim(key, value)
	proof, err := trie.ProveMany(context.Background(), [][]byte{key})
	if err != nil {
		t.Fatalf("ProveMany() error = %v", err)
	}
	key[0] = 'X'
	value[0] = 'X'
	nodes := proof.Nodes()
	transport, err := mpt.MultiProofFromNodes(nodes, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("MultiProofFromNodes() error = %v", err)
	}
	nodes[0][0] ^= 0xff
	if err := mpt.VerifyRawMultiProof(
		context.Background(), root, []mpt.ProofClaim{claim},
		transport, mpt.DefaultLimits(),
	); err != nil {
		t.Fatalf("VerifyRawMultiProof() error = %v", err)
	}
}

func TestMultiProofEmptyTrieAndInputValidation(t *testing.T) {
	t.Parallel()
	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	proof, err := trie.ProveMany(
		context.Background(), [][]byte{[]byte("a"), []byte("b")},
	)
	if err != nil {
		t.Fatalf("ProveMany() error = %v", err)
	}
	if err := mpt.VerifyRawMultiProof(
		context.Background(),
		mpt.EmptyRoot(),
		[]mpt.ProofClaim{
			mpt.AbsenceClaim([]byte("a")), mpt.AbsenceClaim([]byte("b")),
		},
		proof,
		mpt.DefaultLimits(),
	); err != nil {
		t.Fatalf("VerifyRawMultiProof(empty) error = %v", err)
	}
	if err := mpt.VerifyRawMultiProof(
		context.Background(),
		mpt.EmptyRoot(),
		[]mpt.ProofClaim{mpt.MembershipClaim([]byte("a"), []byte("value"))},
		proof,
		mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrFailedProof) {
		t.Fatalf("empty membership error = %v", err)
	}

	if _, err := trie.ProveMany(context.Background(), nil); !errors.Is(err, mpt.ErrInvalidProofClaim) {
		t.Fatalf("empty ProveMany() error = %v", err)
	}
	if _, err := trie.ProveMany(
		context.Background(), [][]byte{[]byte("a"), []byte("a")},
	); !errors.Is(err, mpt.ErrDuplicateProofKey) {
		t.Fatalf("duplicate ProveMany() error = %v", err)
	}
	if err := mpt.VerifyRawMultiProof(
		context.Background(), mpt.EmptyRoot(), nil, proof, mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrInvalidProofClaim) {
		t.Fatalf("empty claims error = %v", err)
	}
	if err := mpt.VerifyRawMultiProof(
		context.Background(),
		mpt.EmptyRoot(),
		[]mpt.ProofClaim{{}},
		proof,
		mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrInvalidProofClaim) {
		t.Fatalf("zero claim error = %v", err)
	}
	if err := mpt.VerifyRawMultiProof(
		context.Background(),
		mpt.EmptyRoot(),
		[]mpt.ProofClaim{
			mpt.AbsenceClaim([]byte("a")), mpt.AbsenceClaim([]byte("a")),
		},
		proof,
		mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrDuplicateProofKey) {
		t.Fatalf("duplicate claims error = %v", err)
	}

	limits := mpt.DefaultLimits()
	limits.MaxProofKeys = 1
	if _, err := trie.ProveMany(
		context.Background(), [][]byte{[]byte("a"), []byte("b")},
	); err != nil {
		t.Fatalf("default-limit ProveMany() error = %v", err)
	}
	if err := mpt.VerifyRawMultiProof(
		context.Background(),
		mpt.EmptyRoot(),
		[]mpt.ProofClaim{
			mpt.AbsenceClaim([]byte("a")), mpt.AbsenceClaim([]byte("b")),
		},
		proof,
		limits,
	); !errors.Is(err, mpt.ErrInvalidProofClaim) {
		t.Fatalf("claim-limit error = %v", err)
	}
}

func TestRawMultiProofTraversesBranchValuesAndChildren(t *testing.T) {
	t.Parallel()
	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	entries := []struct{ key, value []byte }{
		{key: nil, value: []byte("root value")},
		{key: []byte{0x00}, value: []byte("zero child value that is long")},
		{key: []byte{0x10}, value: []byte("one child value that is long")},
	}
	for _, entry := range entries {
		trie, err = trie.Update(context.Background(), entry.key, entry.value)
		if err != nil {
			t.Fatalf("Update(%x) error = %v", entry.key, err)
		}
	}
	root := mustTrieRoot(t, trie)
	proof, err := trie.ProveMany(
		context.Background(),
		[][]byte{nil, {0x00}, {0x10}, {0x20}},
	)
	if err != nil {
		t.Fatalf("ProveMany() error = %v", err)
	}
	if err := mpt.VerifyRawMultiProof(
		context.Background(),
		root,
		[]mpt.ProofClaim{
			mpt.MembershipClaim(nil, []byte("root value")),
			mpt.MembershipClaim(
				[]byte{0x00}, []byte("zero child value that is long"),
			),
			mpt.MembershipClaim(
				[]byte{0x10}, []byte("one child value that is long"),
			),
			mpt.AbsenceClaim([]byte{0x20}),
		},
		proof,
		mpt.DefaultLimits(),
	); err != nil {
		t.Fatalf("VerifyRawMultiProof() error = %v", err)
	}
}

func TestLoadedTrieMultiProofReadsEachSharedNodeOnce(t *testing.T) {
	t.Parallel()
	trie := mustRawTrie(t, map[string]string{
		"alpha":  "a long value that forces hashed child references 1",
		"alpine": "a long value that forces hashed child references 2",
		"beta":   "a long value that forces hashed child references 3",
		"delta":  "a long value that forces hashed child references 4",
	})
	store := newTestNodeStore()
	committed, err := trie.Commit(context.Background(), store)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	root := mustTrieRoot(t, committed)
	loaded, err := mpt.LoadRawTrie(root, store, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("LoadRawTrie() error = %v", err)
	}
	store.reads = 0
	proof, err := loaded.ProveMany(
		context.Background(),
		[][]byte{[]byte("alpha"), []byte("alpine"), []byte("beta")},
	)
	if err != nil {
		t.Fatalf("ProveMany() error = %v", err)
	}
	if store.reads != len(proof.Nodes()) {
		t.Fatalf(
			"store reads = %d, unique proof nodes = %d",
			store.reads,
			len(proof.Nodes()),
		)
	}
}

func containsNode(nodes [][]byte, candidate []byte) bool {
	for _, node := range nodes {
		if slices.Equal(node, candidate) {
			return true
		}
	}
	return false
}

func equalNodeSlices(left, right [][]byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !slices.Equal(left[index], right[index]) {
			return false
		}
	}
	return true
}
