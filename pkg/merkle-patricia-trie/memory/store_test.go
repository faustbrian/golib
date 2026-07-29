package memory_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/memory"
)

func TestStoreCommitsAndLoadsTrie(t *testing.T) {
	t.Parallel()

	store := memory.New()
	if store.Root() != mpt.EmptyRoot() {
		t.Fatalf("initial root = %x", store.Root())
	}
	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	trie, err = trie.Update(context.Background(), []byte("key"), []byte("value"))
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	trie, err = trie.Commit(context.Background(), store)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	root, err := trie.Root()
	if err != nil {
		t.Fatalf("Root() error = %v", err)
	}
	if store.Root() != root {
		t.Fatalf("store root = %x, want %x", store.Root(), root)
	}
	loaded, err := mpt.LoadRawTrie(root, store, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("LoadRawTrie() error = %v", err)
	}
	got, err := loaded.Get(context.Background(), []byte("key"))
	if err != nil || string(got) != "value" {
		t.Fatalf("Get() = (%q, %v)", got, err)
	}
}

func TestStoreRejectsStaleRootAtomically(t *testing.T) {
	t.Parallel()

	store := memory.New()
	left, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	right := left
	left, err = left.Update(context.Background(), []byte("left"), []byte("1"))
	if err != nil {
		t.Fatalf("left Update() error = %v", err)
	}
	right, err = right.Update(context.Background(), []byte("right"), []byte("2"))
	if err != nil {
		t.Fatalf("right Update() error = %v", err)
	}
	left, err = left.Commit(context.Background(), store)
	if err != nil {
		t.Fatalf("left Commit() error = %v", err)
	}
	leftRoot, err := left.Root()
	if err != nil {
		t.Fatalf("left Root() error = %v", err)
	}

	_, err = right.Commit(context.Background(), store)
	if !errors.Is(err, mpt.ErrStorageCommit) || !errors.Is(err, mpt.ErrStaleRoot) {
		t.Fatalf("stale Commit() error = %v", err)
	}
	if store.Root() != leftRoot {
		t.Fatalf("failed stale commit changed root to %x", store.Root())
	}
}

func TestStoreReadsOwnTheirBytesAndClassifyMissing(t *testing.T) {
	t.Parallel()

	store := memory.New()
	var unknown mpt.Root
	unknown[0] = 1
	if _, err := store.GetNode(context.Background(), unknown); !errors.Is(err, mpt.ErrMissingNode) {
		t.Fatalf("GetNode(missing) error = %v", err)
	}

	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	trie, err = trie.Update(context.Background(), []byte("key"), []byte("value"))
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	trie, err = trie.Commit(context.Background(), store)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	root, err := trie.Root()
	if err != nil {
		t.Fatalf("Root() error = %v", err)
	}
	first, err := store.GetNode(context.Background(), root)
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	first[0] ^= 0xff
	second, err := store.GetNode(context.Background(), root)
	if err != nil {
		t.Fatalf("second GetNode() error = %v", err)
	}
	if first[0] == second[0] {
		t.Fatal("GetNode() returned aliased bytes")
	}
}

func TestStoreSupportsConcurrentImmutableReads(t *testing.T) {
	t.Parallel()

	store := memory.New()
	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	trie, err = trie.Update(context.Background(), []byte("key"), []byte("value"))
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	trie, err = trie.Commit(context.Background(), store)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	root, err := trie.Root()
	if err != nil {
		t.Fatalf("Root() error = %v", err)
	}

	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 100 {
				encoded, readErr := store.GetNode(context.Background(), root)
				if readErr != nil || len(encoded) == 0 {
					t.Errorf("GetNode() = (%x, %v)", encoded, readErr)
					return
				}
			}
		}()
	}
	wait.Wait()
}

func TestStoreZeroValueAndInvalidUse(t *testing.T) {
	t.Parallel()

	var store memory.Store
	if store.Root() != mpt.EmptyRoot() {
		t.Fatalf("zero store root = %x", store.Root())
	}
	var unknown mpt.Root
	if _, err := store.GetNode(context.Background(), unknown); !errors.Is(err, mpt.ErrMissingNode) {
		t.Fatalf("zero store GetNode() error = %v", err)
	}

	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	trie, err = trie.Update(context.Background(), []byte("key"), []byte("value"))
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if _, err = trie.Commit(context.Background(), &store); err != nil {
		t.Fatalf("zero store Commit() error = %v", err)
	}

	var nilStore *memory.Store
	if nilStore.Root() != (mpt.Root{}) {
		t.Fatalf("nil store root = %x", nilStore.Root())
	}
	if _, err = nilStore.GetNode(context.Background(), unknown); !errors.Is(err, mpt.ErrInvalidStore) {
		t.Fatalf("nil store GetNode() error = %v", err)
	}
	if err = nilStore.CommitTrie(context.Background(), mpt.StoreCommit{}); !errors.Is(err, mpt.ErrInvalidStore) {
		t.Fatalf("nil store CommitTrie() error = %v", err)
	}
}

func TestStoreHonorsContext(t *testing.T) {
	t.Parallel()

	store := memory.New()
	var unknown mpt.Root
	var nilContext context.Context
	if _, err := store.GetNode(nilContext, unknown); !errors.Is(err, mpt.ErrInvalidContext) {
		t.Fatalf("GetNode(nil context) error = %v", err)
	}
	if err := store.CommitTrie(nilContext, mpt.StoreCommit{}); !errors.Is(err, mpt.ErrInvalidContext) {
		t.Fatalf("CommitTrie(nil context) error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.GetNode(ctx, unknown); !errors.Is(err, mpt.ErrCanceled) ||
		!errors.Is(err, context.Canceled) {
		t.Fatalf("GetNode(canceled) error = %v", err)
	}
	if err := store.CommitTrie(ctx, mpt.StoreCommit{}); !errors.Is(err, mpt.ErrCanceled) ||
		!errors.Is(err, context.Canceled) {
		t.Fatalf("CommitTrie(canceled) error = %v", err)
	}
}

func TestStoreIteratesNodesDeterministicallyWithinBound(t *testing.T) {
	t.Parallel()

	store := memory.New()
	trie := mustMemoryTrie(t, map[string]string{
		"alpha": "a long value that creates persisted child nodes",
		"beta":  "another long value that creates persisted child nodes",
	})
	if _, err := trie.Commit(context.Background(), store); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	var hashes []mpt.Root
	err := store.IterateNodes(
		context.Background(),
		100,
		func(hash mpt.Root, encoded []byte) error {
			hashes = append(hashes, hash)
			encoded[0] ^= 0xff
			stored, readErr := store.GetNode(context.Background(), hash)
			if readErr != nil {
				return readErr
			}
			if stored[0] == encoded[0] {
				t.Fatal("IterateNodes() returned aliased bytes")
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("IterateNodes() error = %v", err)
	}
	if len(hashes) < 2 {
		t.Fatalf("IterateNodes() returned %d nodes", len(hashes))
	}
	if !slices.IsSortedFunc(hashes, func(left, right mpt.Root) int {
		return slices.Compare(left[:], right[:])
	}) {
		t.Fatalf("IterateNodes() hashes are not sorted: %x", hashes)
	}
	called := false
	err = store.IterateNodes(
		context.Background(),
		len(hashes)-1,
		func(mpt.Root, []byte) error {
			called = true
			return nil
		},
	)
	if !errors.Is(err, mpt.ErrResourceLimit) || called {
		t.Fatalf("bounded IterateNodes() = (called %t, %v)", called, err)
	}
}

func TestStoreIterationValidatesBoundCallbackAndContext(t *testing.T) {
	t.Parallel()

	store := memory.New()
	trie := mustMemoryTrie(t, map[string]string{"key": "value"})
	if _, err := trie.Commit(context.Background(), store); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if err := store.IterateNodes(context.Background(), 0, func(mpt.Root, []byte) error {
		return nil
	}); !errors.Is(err, mpt.ErrInvalidIterator) {
		t.Fatalf("IterateNodes(zero bound) error = %v", err)
	}
	if err := store.IterateNodes(context.Background(), 1, nil); !errors.Is(err, mpt.ErrInvalidIterator) {
		t.Fatalf("IterateNodes(nil callback) error = %v", err)
	}
	if err := store.IterateNodes(context.Background(), 0, nil); !errors.Is(err, mpt.ErrInvalidIterator) {
		t.Fatalf("IterateNodes(invalid inputs) error = %v", err)
	}

	callbackErr := errors.New("stop")
	if err := store.IterateNodes(context.Background(), 1, func(mpt.Root, []byte) error {
		return callbackErr
	}); !errors.Is(err, callbackErr) {
		t.Fatalf("IterateNodes(callback error) = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.IterateNodes(ctx, 1, func(mpt.Root, []byte) error {
		return nil
	}); !errors.Is(err, mpt.ErrCanceled) {
		t.Fatalf("IterateNodes(canceled) error = %v", err)
	}
}

func TestStorePrunesOnlyReleasedHistoricalRoots(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()
	trie := mustMemoryTrie(t, map[string]string{
		"alpha": "a long value that creates a persisted alpha child",
		"beta":  "a long value that creates a persisted beta child",
	})
	trie, err := trie.Commit(ctx, store)
	if err != nil {
		t.Fatalf("first Commit() error = %v", err)
	}
	firstRoot, err := trie.Root()
	if err != nil {
		t.Fatalf("first Root() error = %v", err)
	}
	lease, err := store.RetainRoot(
		ctx, firstRoot, mpt.DefaultReachabilityLimits(),
	)
	if err != nil {
		t.Fatalf("RetainRoot() error = %v", err)
	}
	if lease.Root() != firstRoot {
		t.Fatalf("lease root = %x, want %x", lease.Root(), firstRoot)
	}

	trie, err = trie.Delete(ctx, []byte("alpha"))
	if err != nil {
		t.Fatalf("Delete(alpha) error = %v", err)
	}
	trie, err = trie.Commit(ctx, store)
	if err != nil {
		t.Fatalf("second Commit() error = %v", err)
	}
	secondRoot, err := trie.Root()
	if err != nil {
		t.Fatalf("second Root() error = %v", err)
	}

	retained, err := store.Prune(ctx, mpt.DefaultReachabilityLimits())
	if err != nil {
		t.Fatalf("Prune(retained) error = %v", err)
	}
	if retained.RemovedNodes() != 0 {
		t.Fatalf("Prune(retained) removed %d nodes", retained.RemovedNodes())
	}
	oldTrie, err := mpt.LoadRawTrie(firstRoot, store, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("LoadRawTrie(first root) error = %v", err)
	}
	if value, getErr := oldTrie.Get(ctx, []byte("alpha")); getErr != nil ||
		len(value) == 0 {
		t.Fatalf("retained old Get(alpha) = (%q, %v)", value, getErr)
	}

	if err := lease.Release(ctx); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	pruned, err := store.Prune(ctx, mpt.DefaultReachabilityLimits())
	if err != nil {
		t.Fatalf("Prune(released) error = %v", err)
	}
	if pruned.RemovedNodes() == 0 || pruned.StoredAfter() >= pruned.StoredBefore() {
		t.Fatalf(
			"Prune(released) = before %d, after %d, removed %d",
			pruned.StoredBefore(),
			pruned.StoredAfter(),
			pruned.RemovedNodes(),
		)
	}
	if _, err := store.GetNode(ctx, firstRoot); !errors.Is(err, mpt.ErrMissingNode) {
		t.Fatalf("GetNode(pruned root) error = %v", err)
	}
	current, err := mpt.LoadRawTrie(secondRoot, store, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("LoadRawTrie(current root) error = %v", err)
	}
	if value, getErr := current.Get(ctx, []byte("beta")); getErr != nil ||
		len(value) == 0 {
		t.Fatalf("current Get(beta) = (%q, %v)", value, getErr)
	}
}

func mustMemoryTrie(t *testing.T, values map[string]string) mpt.RawTrie {
	t.Helper()
	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	for key, value := range values {
		trie, err = trie.Update(context.Background(), []byte(key), []byte(value))
		if err != nil {
			t.Fatalf("Update(%q) error = %v", key, err)
		}
	}
	return trie
}
