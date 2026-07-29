package mpt_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/internal/rlp"
)

func TestCommitAndLoadRawTrie(t *testing.T) {
	t.Parallel()

	trie := mustRawTrie(t, map[string]string{
		"do":    "verb",
		"dog":   "puppy",
		"horse": "stallion",
	})
	store := newTestNodeStore()
	committed, err := trie.Commit(context.Background(), store)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	root := mustTrieRoot(t, committed)
	if store.root != root {
		t.Fatalf("published root = %x, want %x", store.root, root)
	}
	if len(store.nodes) == 0 {
		t.Fatal("Commit() wrote no nodes")
	}
	for hash, encoded := range store.nodes {
		if got := legacyKeccakForTest(encoded); !slices.Equal(got, hash.Bytes()) {
			t.Fatalf("stored node %x hashes to %x", hash, got)
		}
	}

	loaded, err := mpt.LoadRawTrie(root, store, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("LoadRawTrie() error = %v", err)
	}
	for key, want := range map[string]string{
		"do":    "verb",
		"dog":   "puppy",
		"horse": "stallion",
	} {
		got, getErr := loaded.Get(context.Background(), []byte(key))
		if getErr != nil || string(got) != want {
			t.Fatalf("Get(%q) = (%q, %v), want %q", key, got, getErr, want)
		}
	}

	updated, err := loaded.Update(context.Background(), []byte("doge"), []byte("coin"))
	if err != nil {
		t.Fatalf("loaded Update() error = %v", err)
	}
	if _, err := updated.Commit(context.Background(), store); err != nil {
		t.Fatalf("second Commit() error = %v", err)
	}
}

func TestLoadEmptyRootDoesNotReadStore(t *testing.T) {
	t.Parallel()

	store := newTestNodeStore()
	trie, err := mpt.LoadRawTrie(mpt.EmptyRoot(), store, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("LoadRawTrie() error = %v", err)
	}
	if _, err := trie.Get(context.Background(), []byte("key")); !errors.Is(err, mpt.ErrAbsentKey) {
		t.Fatalf("Get() error = %v, want ErrAbsentKey", err)
	}
	if store.reads != 0 {
		t.Fatalf("empty-root reads = %d, want 0", store.reads)
	}
}

func TestLoadedTrieClassifiesMissingCorruptAndFailedReads(t *testing.T) {
	t.Parallel()

	trie := mustRawTrie(t, map[string]string{"key": "value"})
	store := newTestNodeStore()
	committed, err := trie.Commit(context.Background(), store)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	root := mustTrieRoot(t, committed)

	delete(store.nodes, root)
	loaded, err := mpt.LoadRawTrie(root, store, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("LoadRawTrie() error = %v", err)
	}
	_, err = loaded.Get(context.Background(), []byte("key"))
	var missing *mpt.MissingNodeError
	if !errors.Is(err, mpt.ErrMissingNode) || !errors.As(err, &missing) || missing.Hash != root {
		t.Fatalf("missing Get() error = %v", err)
	}

	store.nodes[root] = []byte{0x80}
	_, err = loaded.Get(context.Background(), []byte("key"))
	var corrupt *mpt.CorruptNodeError
	if !errors.Is(err, mpt.ErrCorruptNode) || !errors.As(err, &corrupt) || corrupt.Hash != root {
		t.Fatalf("corrupt Get() error = %v", err)
	}

	store.readErr = errors.New("backend unavailable")
	_, err = loaded.Get(context.Background(), []byte("key"))
	if !errors.Is(err, mpt.ErrStorageRead) || !errors.Is(err, store.readErr) {
		t.Fatalf("failed Get() error = %v", err)
	}
}

func TestLoadedTrieRejectsExtensionToHashedCompactNode(t *testing.T) {
	t.Parallel()

	leaf, err := rlp.Encode(
		rlp.List(rlp.String([]byte{0x32}), rlp.String([]byte("value"))),
		rlp.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("encode leaf: %v", err)
	}
	leafHash, err := mpt.RootFromBytes(legacyKeccakForTest(leaf))
	if err != nil {
		t.Fatalf("leaf RootFromBytes() error = %v", err)
	}
	extension, err := rlp.Encode(
		rlp.List(rlp.String([]byte{0x11}), rlp.String(leafHash.Bytes())),
		rlp.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("encode extension: %v", err)
	}
	root, err := mpt.RootFromBytes(legacyKeccakForTest(extension))
	if err != nil {
		t.Fatalf("root RootFromBytes() error = %v", err)
	}
	store := newTestNodeStore()
	store.nodes[root] = extension
	store.nodes[leafHash] = leaf

	trie, err := mpt.LoadRawTrie(root, store, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("LoadRawTrie() error = %v", err)
	}
	_, err = trie.Get(context.Background(), []byte{0x12})
	if !errors.Is(err, mpt.ErrCorruptNode) {
		t.Fatalf("Get(non-canonical stored trie) error = %v, want ErrCorruptNode", err)
	}
}

func TestCommitFailureDoesNotPublishOrChangeSnapshot(t *testing.T) {
	t.Parallel()

	trie := mustRawTrie(t, map[string]string{"key": "value"})
	store := newTestNodeStore()
	store.commitErr = errors.New("disk full")

	_, err := trie.Commit(context.Background(), store)
	if !errors.Is(err, mpt.ErrStorageCommit) || !errors.Is(err, store.commitErr) {
		t.Fatalf("Commit() error = %v", err)
	}
	if store.root != mpt.EmptyRoot() || len(store.nodes) != 0 {
		t.Fatalf("failed commit published root %x or %d nodes", store.root, len(store.nodes))
	}
	got, getErr := trie.Get(context.Background(), []byte("key"))
	if getErr != nil || string(got) != "value" {
		t.Fatalf("old snapshot Get() = (%q, %v)", got, getErr)
	}
}

func TestCommitSuccessRemainsSuccessAfterPublication(t *testing.T) {
	t.Parallel()

	trie := mustRawTrie(t, map[string]string{"key": "value"})
	store := newTestNodeStore()
	ctx, cancel := context.WithCancel(context.Background())
	store.afterCommit = cancel

	committed, err := trie.Commit(ctx, store)
	if err != nil {
		t.Fatalf("Commit() error after publication = %v", err)
	}
	if got := mustTrieRoot(t, committed); got != store.root {
		t.Fatalf("committed root = %x, published root = %x", got, store.root)
	}
}

func TestLoadedTrieRejectsCommitToDifferentStore(t *testing.T) {
	t.Parallel()

	source := newTestNodeStore()
	trie := mustRawTrie(t, map[string]string{
		"alpha": "a long value that forces a hashed sibling reference",
		"beta":  "another long value that remains only in the source store",
	})
	committed, err := trie.Commit(context.Background(), source)
	if err != nil {
		t.Fatalf("source Commit() error = %v", err)
	}
	loaded, err := mpt.LoadRawTrie(mustTrieRoot(t, committed), source, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("LoadRawTrie() error = %v", err)
	}
	updated, err := loaded.Update(context.Background(), []byte("alpha"), []byte("replacement"))
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	destination := newTestNodeStore()
	if _, err := updated.Commit(context.Background(), destination); !errors.Is(err, mpt.ErrInvalidStore) {
		t.Fatalf("cross-store Commit() error = %v, want ErrInvalidStore", err)
	}
	if destination.root != mpt.EmptyRoot() || len(destination.nodes) != 0 {
		t.Fatalf("cross-store commit published %x with %d nodes", destination.root, len(destination.nodes))
	}
}

func TestFreshEmptyTrieStillUsesStoreCompareAndSwap(t *testing.T) {
	t.Parallel()

	store := newTestNodeStore()
	populated := mustRawTrie(t, map[string]string{"key": "value"})
	if _, err := populated.Commit(context.Background(), store); err != nil {
		t.Fatalf("populated Commit() error = %v", err)
	}
	published := store.root

	empty, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	if _, err := empty.Commit(context.Background(), store); !errors.Is(err, mpt.ErrStorageCommit) ||
		!errors.Is(err, mpt.ErrStaleRoot) {
		t.Fatalf("empty Commit() error = %v", err)
	}
	if store.root != published {
		t.Fatalf("failed empty commit changed root to %x", store.root)
	}
}

func TestStoreConstructorsAndOperationsValidateInputs(t *testing.T) {
	t.Parallel()

	if _, err := mpt.LoadRawTrie(mpt.EmptyRoot(), nil, mpt.DefaultLimits()); !errors.Is(err, mpt.ErrInvalidStore) {
		t.Fatalf("LoadRawTrie(nil store) error = %v, want ErrInvalidStore", err)
	}
	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	if _, err := trie.Commit(context.Background(), nil); !errors.Is(err, mpt.ErrInvalidStore) {
		t.Fatalf("Commit(nil store) error = %v, want ErrInvalidStore", err)
	}

	limits := mpt.DefaultLimits()
	limits.MaxNodeReads = 0
	if _, err := mpt.LoadRawTrie(mpt.EmptyRoot(), newTestNodeStore(), limits); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("LoadRawTrie(invalid limits) error = %v, want ErrResourceLimit", err)
	}
}

type testNodeStore struct {
	root        mpt.Root
	nodes       map[mpt.Root][]byte
	reads       int
	readErr     error
	commitErr   error
	afterCommit func()
}

func newTestNodeStore() *testNodeStore {
	return &testNodeStore{
		root:  mpt.EmptyRoot(),
		nodes: make(map[mpt.Root][]byte),
	}
}

func (store *testNodeStore) GetNode(_ context.Context, hash mpt.Root) ([]byte, error) {
	store.reads++
	if store.readErr != nil {
		return nil, store.readErr
	}
	encoded, ok := store.nodes[hash]
	if !ok {
		return nil, mpt.ErrMissingNode
	}
	return encoded, nil
}

func (store *testNodeStore) CommitTrie(_ context.Context, commit mpt.StoreCommit) error {
	if store.commitErr != nil {
		return store.commitErr
	}
	if store.root != commit.PreviousRoot() {
		return fmt.Errorf(
			"%w: have %x, want %x",
			mpt.ErrStaleRoot,
			store.root,
			commit.PreviousRoot(),
		)
	}
	next := make(map[mpt.Root][]byte, len(store.nodes)+len(commit.Nodes()))
	for hash, encoded := range store.nodes {
		next[hash] = append([]byte(nil), encoded...)
	}
	for _, stored := range commit.Nodes() {
		next[stored.Hash()] = stored.Encoded()
	}
	store.nodes = next
	store.root = commit.Root()
	if store.afterCommit != nil {
		store.afterCommit()
	}
	return nil
}
