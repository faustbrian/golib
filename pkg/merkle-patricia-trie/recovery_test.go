package mpt_test

import (
	"context"
	"errors"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
	"golang.org/x/crypto/sha3"
)

func TestRawTrieRecoversMissingNodeIntoImmutableSnapshot(t *testing.T) {
	t.Parallel()

	store := newTestNodeStore()
	trie := mustRawTrie(t, map[string]string{
		"alpha": "a long value that forces persisted trie nodes",
		"beta":  "another long value that forces persisted trie nodes",
	})
	committed, err := trie.Commit(context.Background(), store)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	root := mustTrieRoot(t, committed)
	encoded := append([]byte(nil), store.nodes[root]...)
	delete(store.nodes, root)

	loaded, err := mpt.LoadRawTrie(root, store, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("LoadRawTrie() error = %v", err)
	}
	_, err = loaded.Get(context.Background(), []byte("alpha"))
	var missing *mpt.MissingNodeError
	if !errors.As(err, &missing) || missing.Hash != root {
		t.Fatalf("Get() error = %v, want missing root %x", err, root)
	}

	recovered, err := loaded.RecoverNode(
		context.Background(), missing.Hash, encoded,
	)
	if err != nil {
		t.Fatalf("RecoverNode() error = %v", err)
	}
	encoded[0] ^= 0xff
	if _, err := loaded.Get(
		context.Background(), []byte("alpha"),
	); !errors.Is(err, mpt.ErrMissingNode) {
		t.Fatalf("old snapshot Get() error = %v, want ErrMissingNode", err)
	}
	if got, err := recovered.Get(
		context.Background(), []byte("alpha"),
	); err != nil || string(got) != "a long value that forces persisted trie nodes" {
		t.Fatalf("recovered Get(alpha) = (%q, %v)", got, err)
	}
	if got := mustTrieRoot(t, recovered); got != root {
		t.Fatalf("recovered root = %x, want %x", got, root)
	}
	proof, err := recovered.Prove(context.Background(), []byte("alpha"))
	if err != nil {
		t.Fatalf("Prove(alpha) error = %v", err)
	}
	if err := mpt.VerifyRawMembership(
		context.Background(),
		root,
		[]byte("alpha"),
		[]byte("a long value that forces persisted trie nodes"),
		proof,
		mpt.DefaultLimits(),
	); err != nil {
		t.Fatalf("VerifyRawMembership(alpha) error = %v", err)
	}
	multi, err := recovered.ProveMany(
		context.Background(), [][]byte{[]byte("alpha"), []byte("missing")},
	)
	if err != nil {
		t.Fatalf("ProveMany() error = %v", err)
	}
	if err := mpt.VerifyRawMultiProof(
		context.Background(),
		root,
		[]mpt.ProofClaim{
			mpt.MembershipClaim(
				[]byte("alpha"),
				[]byte("a long value that forces persisted trie nodes"),
			),
			mpt.AbsenceClaim([]byte("missing")),
		},
		multi,
		mpt.DefaultLimits(),
	); err != nil {
		t.Fatalf("VerifyRawMultiProof() error = %v", err)
	}
	var iterated int
	if err := recovered.Iterate(
		context.Background(),
		mpt.IterationOptions{},
		func(mpt.Entry) error {
			iterated++
			return nil
		},
	); err != nil {
		t.Fatalf("Iterate() error = %v", err)
	}
	if iterated != 2 {
		t.Fatalf("Iterate() count = %d, want 2", iterated)
	}
	rebuilt, err := recovered.Rebuild(context.Background())
	if err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	if got := mustTrieRoot(t, rebuilt); got != root {
		t.Fatalf("rebuilt recovered root = %x, want %x", got, root)
	}
	deleted, err := recovered.Delete(context.Background(), []byte("beta"))
	if err != nil {
		t.Fatalf("Delete(beta) error = %v", err)
	}
	if _, err := deleted.Get(
		context.Background(), []byte("beta"),
	); !errors.Is(err, mpt.ErrAbsentKey) {
		t.Fatalf("deleted Get(beta) error = %v, want ErrAbsentKey", err)
	}
	batched, err := recovered.ApplyBatch(
		context.Background(),
		[]mpt.Mutation{mpt.Put([]byte("gamma"), []byte("batch value"))},
	)
	if err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}
	if got, err := batched.Get(
		context.Background(), []byte("gamma"),
	); err != nil || string(got) != "batch value" {
		t.Fatalf("batched Get(gamma) = (%q, %v)", got, err)
	}

	_, err = recovered.Commit(context.Background(), store)
	if err != nil {
		t.Fatalf("recovery Commit() error = %v", err)
	}
	reloaded, err := mpt.LoadRawTrie(root, store, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("LoadRawTrie(repaired) error = %v", err)
	}
	if got, err := reloaded.Get(
		context.Background(), []byte("beta"),
	); err != nil || string(got) != "another long value that forces persisted trie nodes" {
		t.Fatalf("reloaded Get(beta) = (%q, %v)", got, err)
	}
}

func testKeccakRoot(value []byte) mpt.Root {
	hasher := sha3.NewLegacyKeccak256()
	_, _ = hasher.Write(value)
	var root mpt.Root
	hasher.Sum(root[:0])
	return root
}

func TestRecoveredNodeCanParticipateInAtomicUpdate(t *testing.T) {
	t.Parallel()

	store := newTestNodeStore()
	trie := mustRawTrie(t, map[string]string{
		"alpha": "a long value that forces persisted trie nodes",
		"beta":  "another long value that forces persisted trie nodes",
	})
	committed, err := trie.Commit(context.Background(), store)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	root := mustTrieRoot(t, committed)
	encoded := append([]byte(nil), store.nodes[root]...)
	delete(store.nodes, root)
	loaded, err := mpt.LoadRawTrie(root, store, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("LoadRawTrie() error = %v", err)
	}
	recovered, err := loaded.RecoverNode(context.Background(), root, encoded)
	if err != nil {
		t.Fatalf("RecoverNode() error = %v", err)
	}
	updated, err := recovered.Update(
		context.Background(), []byte("gamma"), []byte("third value"),
	)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	updated, err = updated.Commit(context.Background(), store)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if got, err := updated.Get(
		context.Background(), []byte("gamma"),
	); err != nil || string(got) != "third value" {
		t.Fatalf("Get(gamma) = (%q, %v)", got, err)
	}
}

func TestAtomicUpdatePersistsRecoveredUntouchedDescendant(t *testing.T) {
	t.Parallel()

	store := newTestNodeStore()
	trie := mustRawTrie(t, map[string]string{
		"alpha": "a long value that forces persisted trie nodes",
		"beta":  "another long value that forces persisted trie nodes",
	})
	committed, err := trie.Commit(context.Background(), store)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	root := mustTrieRoot(t, committed)
	var (
		missingHash mpt.Root
		missingNode []byte
		found       bool
	)
	for hash, encoded := range store.nodes {
		if hash == root {
			continue
		}
		delete(store.nodes, hash)
		loaded, loadErr := mpt.LoadRawTrie(root, store, mpt.DefaultLimits())
		if loadErr != nil {
			t.Fatalf("LoadRawTrie() error = %v", loadErr)
		}
		_, getErr := loaded.Get(context.Background(), []byte("beta"))
		var missing *mpt.MissingNodeError
		_, alphaErr := loaded.Get(context.Background(), []byte("alpha"))
		if errors.As(getErr, &missing) && missing.Hash == hash &&
			alphaErr == nil {
			missingHash = hash
			missingNode = append([]byte(nil), encoded...)
			found = true
			break
		}
		store.nodes[hash] = encoded
	}
	if !found {
		t.Fatal("beta path did not expose a persisted child node")
	}

	loaded, err := mpt.LoadRawTrie(root, store, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("LoadRawTrie(missing child) error = %v", err)
	}
	recovered, err := loaded.RecoverNode(
		context.Background(), missingHash, missingNode,
	)
	if err != nil {
		t.Fatalf("RecoverNode() error = %v", err)
	}
	updated, err := recovered.Update(
		context.Background(), []byte("alpha"), []byte("replacement value"),
	)
	if err != nil {
		t.Fatalf("Update(alpha) error = %v", err)
	}
	updated, err = updated.Commit(context.Background(), store)
	if err != nil {
		t.Fatalf("Commit(updated) error = %v", err)
	}
	reloaded, err := mpt.LoadRawTrie(
		mustTrieRoot(t, updated), store, mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("LoadRawTrie(updated) error = %v", err)
	}
	if got, err := reloaded.Get(
		context.Background(), []byte("beta"),
	); err != nil || string(got) != "another long value that forces persisted trie nodes" {
		t.Fatalf("Get(beta) after update = (%q, %v)", got, err)
	}
}

func TestSecureTrieRecoveryPreservesKeyProfile(t *testing.T) {
	t.Parallel()

	store := newTestNodeStore()
	trie, err := mpt.NewSecureTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewSecureTrie() error = %v", err)
	}
	trie, err = trie.Update(
		context.Background(), []byte("key"), []byte("secure value"),
	)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	committed, err := trie.Commit(context.Background(), store)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	root := mustSecureRoot(t, committed)
	encoded := append([]byte(nil), store.nodes[root]...)
	delete(store.nodes, root)
	loaded, err := mpt.LoadSecureTrie(root, store, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("LoadSecureTrie() error = %v", err)
	}
	recovered, err := loaded.RecoverNode(context.Background(), root, encoded)
	if err != nil {
		t.Fatalf("RecoverNode() error = %v", err)
	}
	if got, err := recovered.Get(
		context.Background(), []byte("key"),
	); err != nil || string(got) != "secure value" {
		t.Fatalf("Get(key) = (%q, %v)", got, err)
	}
}

func TestRecoverNodeRejectsInvalidInputAndBounds(t *testing.T) {
	t.Parallel()

	var zero mpt.RawTrie
	if _, err := zero.RecoverNode(
		context.Background(), mpt.Root{}, []byte{0x80},
	); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero RecoverNode() error = %v", err)
	}
	var zeroSecure mpt.SecureTrie
	if _, err := zeroSecure.RecoverNode(
		context.Background(), mpt.Root{}, []byte{0x80},
	); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero secure RecoverNode() error = %v", err)
	}

	limits := mpt.DefaultLimits()
	trie, err := mpt.NewRawTrie(limits)
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	if _, err := trie.RecoverNode(
		context.Background(), mpt.Root{}, []byte{0x80},
	); !errors.Is(err, mpt.ErrInvalidStore) {
		t.Fatalf("storeless RecoverNode() error = %v", err)
	}
	trie, err = mpt.LoadRawTrie(
		mpt.EmptyRoot(), newTestNodeStore(), limits,
	)
	if err != nil {
		t.Fatalf("LoadRawTrie() error = %v", err)
	}
	var nilContext context.Context
	if _, err := trie.RecoverNode(
		nilContext, mpt.Root{}, []byte{0x80},
	); !errors.Is(err, mpt.ErrInvalidContext) {
		t.Fatalf("nil-context RecoverNode() error = %v", err)
	}
	if _, err := trie.RecoverNode(
		context.Background(), mpt.Root{1}, []byte{0x80},
	); !errors.Is(err, mpt.ErrCorruptNode) {
		t.Fatalf("hash-mismatch RecoverNode() error = %v", err)
	}
	nullHash := mpt.EmptyRoot()
	if _, err := trie.RecoverNode(
		context.Background(), nullHash, []byte{0x80},
	); !errors.Is(err, mpt.ErrMalformedNode) {
		t.Fatalf("null RecoverNode() error = %v", err)
	}
	malformed := []byte{0xff}
	if _, err := trie.RecoverNode(
		context.Background(), testKeccakRoot(malformed), malformed,
	); !errors.Is(err, mpt.ErrMalformedNode) {
		t.Fatalf("malformed RecoverNode() error = %v", err)
	}

	validTrie := mustRawTrie(t, map[string]string{"key": "value"})
	validStore := newTestNodeStore()
	validTrie, err = validTrie.Commit(context.Background(), validStore)
	if err != nil {
		t.Fatalf("valid Commit() error = %v", err)
	}
	validRoot := mustTrieRoot(t, validTrie)
	recovered, err := validTrie.RecoverNode(
		context.Background(), validRoot, validStore.nodes[validRoot],
	)
	if err != nil {
		t.Fatalf("valid RecoverNode() error = %v", err)
	}
	if _, err := recovered.RecoverNode(
		context.Background(), validRoot, []byte{0x80},
	); !errors.Is(err, mpt.ErrCorruptNode) {
		t.Fatalf("conflicting RecoverNode() error = %v", err)
	}
}

func TestRecoverNodeEnforcesAggregateNodeAndByteLimits(t *testing.T) {
	t.Parallel()

	store := newTestNodeStore()
	trie := mustRawTrie(t, map[string]string{
		"alpha": "a long value that forces persisted trie nodes",
		"beta":  "another long value that forces persisted trie nodes",
	})
	committed, err := trie.Commit(context.Background(), store)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	var recoveredNodes []struct {
		hash    mpt.Root
		encoded []byte
	}
	for hash, encoded := range store.nodes {
		recoveredNodes = append(recoveredNodes, struct {
			hash    mpt.Root
			encoded []byte
		}{hash: hash, encoded: append([]byte(nil), encoded...)})
		if len(recoveredNodes) == 2 {
			break
		}
	}
	if len(recoveredNodes) != 2 {
		t.Fatalf("stored node count = %d, want at least 2", len(recoveredNodes))
	}

	limits := mpt.DefaultLimits()
	limits.MaxRecoveryNodes = 1
	limits.MaxRecoveryBytes = mpt.DefaultLimits().MaxRecoveryBytes
	loaded, err := mpt.LoadRawTrie(
		mustTrieRoot(t, committed), store, limits,
	)
	if err != nil {
		t.Fatalf("LoadRawTrie() error = %v", err)
	}
	recovered, err := loaded.RecoverNode(
		context.Background(),
		recoveredNodes[0].hash,
		recoveredNodes[0].encoded,
	)
	if err != nil {
		t.Fatalf("RecoverNode(first) error = %v", err)
	}
	if _, err := recovered.RecoverNode(
		context.Background(),
		recoveredNodes[0].hash,
		recoveredNodes[0].encoded,
	); err != nil {
		t.Fatalf("RecoverNode(idempotent) error = %v", err)
	}
	if _, err := recovered.RecoverNode(
		context.Background(),
		recoveredNodes[1].hash,
		recoveredNodes[1].encoded,
	); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("RecoverNode(node limit) error = %v", err)
	}

	invalid := mpt.DefaultLimits()
	invalid.MaxRecoveryNodes = 0
	if _, err := mpt.NewRawTrie(
		invalid,
	); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("NewRawTrie(zero recovery nodes) error = %v", err)
	}
	invalid = mpt.DefaultLimits()
	invalid.MaxRecoveryBytes = 0
	if _, err := mpt.NewRawTrie(
		invalid,
	); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("NewRawTrie(zero recovery bytes) error = %v", err)
	}

	byteLimits := mpt.DefaultLimits()
	byteLimits.MaxRecoveryNodes = 2
	byteLimits.MaxRecoveryBytes = len(recoveredNodes[0].encoded) +
		len(recoveredNodes[1].encoded) - 1
	loaded, err = mpt.LoadRawTrie(
		mustTrieRoot(t, committed), store, byteLimits,
	)
	if err != nil {
		t.Fatalf("LoadRawTrie(byte limits) error = %v", err)
	}
	recovered, err = loaded.RecoverNode(
		context.Background(),
		recoveredNodes[0].hash,
		recoveredNodes[0].encoded,
	)
	if err != nil {
		t.Fatalf("RecoverNode(byte first) error = %v", err)
	}
	if _, err := recovered.RecoverNode(
		context.Background(),
		recoveredNodes[1].hash,
		recoveredNodes[1].encoded,
	); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("RecoverNode(byte limit) error = %v", err)
	}
}
