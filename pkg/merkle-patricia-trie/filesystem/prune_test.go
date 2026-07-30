package filesystem_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/filesystem"
)

func TestStorePrunesOnlyReleasedDurableRootsAcrossReopen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "trie")
	store, err := filesystem.Open(ctx, path, filesystem.DefaultLimits())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	trie := mustFilesystemRetentionTrie(t)
	trie, err = trie.Commit(ctx, store)
	if err != nil {
		t.Fatalf("Commit(first) error = %v", err)
	}
	firstRoot, err := trie.Root()
	if err != nil {
		t.Fatalf("Root(first) error = %v", err)
	}
	if _, err := store.RetainRoot(
		ctx,
		firstRoot,
		mpt.DefaultReachabilityLimits(),
	); err != nil {
		t.Fatalf("RetainRoot() error = %v", err)
	}
	trie, err = trie.Delete(ctx, []byte("alpha"))
	if err != nil {
		t.Fatalf("Delete(alpha) error = %v", err)
	}
	trie, err = trie.Commit(ctx, store)
	if err != nil {
		t.Fatalf("Commit(second) error = %v", err)
	}
	secondRoot, err := trie.Root()
	if err != nil {
		t.Fatalf("Root(second) error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	store, err = filesystem.Open(ctx, path, filesystem.DefaultLimits())
	if err != nil {
		t.Fatalf("Open(reopen retained) error = %v", err)
	}
	retentions, err := store.Retentions(
		ctx,
		filesystem.DefaultLimits().MaxRetentions,
	)
	if err != nil {
		t.Fatalf("Retentions() error = %v", err)
	}
	if len(retentions) != 1 || retentions[0].Root() != firstRoot {
		t.Fatalf("Retentions() = %v, want first root %x", retentions, firstRoot)
	}
	if _, err := store.Prune(
		ctx,
		mpt.DefaultReachabilityLimits(),
	); err != nil {
		t.Fatalf("Prune(retained) error = %v", err)
	}
	assertFilesystemRootValue(t, store, firstRoot, "alpha")
	assertFilesystemRootValue(t, store, secondRoot, "beta")
	if err := retentions[0].Release(ctx); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close(after release) error = %v", err)
	}

	store, err = filesystem.Open(ctx, path, filesystem.DefaultLimits())
	if err != nil {
		t.Fatalf("Open(reopen released) error = %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("Close(final) error = %v", closeErr)
		}
	}()
	result, err := store.Prune(ctx, mpt.DefaultReachabilityLimits())
	if err != nil {
		t.Fatalf("Prune(released) error = %v", err)
	}
	if result.RemovedNodes() == 0 ||
		result.StoredAfter() >= result.StoredBefore() {
		t.Fatalf(
			"Prune(released) = before %d, after %d, removed %d",
			result.StoredBefore(),
			result.StoredAfter(),
			result.RemovedNodes(),
		)
	}
	if _, err := store.GetNode(
		ctx,
		firstRoot,
	); !errors.Is(err, mpt.ErrMissingNode) {
		t.Fatalf("GetNode(pruned first root) error = %v", err)
	}
	assertFilesystemRootValue(t, store, secondRoot, "beta")
	repeated, err := store.Prune(ctx, mpt.DefaultReachabilityLimits())
	if err != nil {
		t.Fatalf("Prune(repeated) error = %v", err)
	}
	if repeated.RemovedNodes() != 0 {
		t.Fatalf("Prune(repeated) removed %d nodes", repeated.RemovedNodes())
	}
}

func assertFilesystemRootValue(
	t *testing.T,
	store *filesystem.Store,
	root mpt.Root,
	key string,
) {
	t.Helper()

	trie, err := mpt.LoadRawTrie(root, store, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("LoadRawTrie(%x) error = %v", root, err)
	}
	if _, err := trie.Get(context.Background(), []byte(key)); err != nil {
		t.Fatalf("Get(%x, %q) error = %v", root, key, err)
	}
}
