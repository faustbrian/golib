package mpt_test

import (
	"context"
	"errors"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

func TestRawTrieRebuildMatchesRootAndCanMoveStores(t *testing.T) {
	t.Parallel()

	source := newTestNodeStore()
	trie := mustRawTrie(t, map[string]string{
		"":      "root value",
		"alpha": "first",
		"beta":  "second",
	})
	committed, err := trie.Commit(context.Background(), source)
	if err != nil {
		t.Fatalf("source Commit() error = %v", err)
	}
	loaded, err := mpt.LoadRawTrie(mustTrieRoot(t, committed), source, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("LoadRawTrie() error = %v", err)
	}

	rebuilt, err := loaded.Rebuild(context.Background())
	if err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	if got, want := mustTrieRoot(t, rebuilt), mustTrieRoot(t, loaded); got != want {
		t.Fatalf("rebuilt root = %x, want %x", got, want)
	}

	destination := newTestNodeStore()
	moved, err := rebuilt.Commit(context.Background(), destination)
	if err != nil {
		t.Fatalf("destination Commit() error = %v", err)
	}
	reloaded, err := mpt.LoadRawTrie(mustTrieRoot(t, moved), destination, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("destination LoadRawTrie() error = %v", err)
	}
	if got, getErr := reloaded.Get(context.Background(), []byte("beta")); getErr != nil ||
		string(got) != "second" {
		t.Fatalf("destination Get(beta) = (%q, %v)", got, getErr)
	}
}

func TestSecureTrieRebuildPreservesSecureProfile(t *testing.T) {
	t.Parallel()

	trie, err := mpt.NewSecureTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewSecureTrie() error = %v", err)
	}
	trie, err = trie.Update(context.Background(), []byte("key"), []byte("value"))
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	rebuilt, err := trie.Rebuild(context.Background())
	if err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	if got, want := mustSecureRoot(t, rebuilt), mustSecureRoot(t, trie); got != want {
		t.Fatalf("rebuilt root = %x, want %x", got, want)
	}
	if got, getErr := rebuilt.Get(context.Background(), []byte("key")); getErr != nil ||
		string(got) != "value" {
		t.Fatalf("rebuilt Get(key) = (%q, %v)", got, getErr)
	}
}

func TestRebuildPreservesSourceAndPropagatesReadFailures(t *testing.T) {
	t.Parallel()

	trie := mustRawTrie(t, map[string]string{"key": "value"})
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
	delete(store.nodes, root)

	if _, err := loaded.Rebuild(context.Background()); !errors.Is(err, mpt.ErrMissingNode) {
		t.Fatalf("Rebuild() error = %v, want ErrMissingNode", err)
	}
	if got := mustTrieRoot(t, loaded); got != root {
		t.Fatalf("failed rebuild changed source root to %x", got)
	}
}

func TestRebuildHonorsWorkAndCancellationBounds(t *testing.T) {
	t.Parallel()

	limits := mpt.DefaultLimits()
	limits.MaxRebuildNodes = 1
	trie, err := mpt.NewRawTrie(limits)
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	trie, err = trie.Update(context.Background(), []byte("a"), []byte("1"))
	if err != nil {
		t.Fatalf("Update(a) error = %v", err)
	}
	trie, err = trie.Update(context.Background(), []byte("b"), []byte("2"))
	if err != nil {
		t.Fatalf("Update(b) error = %v", err)
	}
	if _, err := trie.Rebuild(context.Background()); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("node-limited Rebuild() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := trie.Rebuild(ctx); !errors.Is(err, context.Canceled) ||
		!errors.Is(err, mpt.ErrCanceled) {
		t.Fatalf("canceled Rebuild() error = %v", err)
	}
}

func mustSecureRoot(t *testing.T, trie mpt.SecureTrie) mpt.Root {
	t.Helper()
	root, err := trie.Root()
	if err != nil {
		t.Fatalf("Root() error = %v", err)
	}
	return root
}
