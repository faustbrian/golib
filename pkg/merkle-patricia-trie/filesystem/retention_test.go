package filesystem_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/filesystem"
)

func TestStorePersistsIndependentRootRetentionsAcrossReopen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "trie")
	limits := filesystem.DefaultLimits()
	store, err := filesystem.Open(ctx, path, limits)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	trie := mustFilesystemRetentionTrie(t)
	trie, err = trie.Commit(ctx, store)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	root, err := trie.Root()
	if err != nil {
		t.Fatalf("Root() error = %v", err)
	}
	first, err := store.RetainRoot(
		ctx,
		root,
		mpt.DefaultReachabilityLimits(),
	)
	if err != nil {
		t.Fatalf("RetainRoot(first) error = %v", err)
	}
	second, err := store.RetainRoot(
		ctx,
		root,
		mpt.DefaultReachabilityLimits(),
	)
	if err != nil {
		t.Fatalf("RetainRoot(second) error = %v", err)
	}
	if first.Root() != root || second.Root() != root {
		t.Fatalf(
			"retained roots = (%x, %x), want %x",
			first.Root(),
			second.Root(),
			root,
		)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	store, err = filesystem.Open(ctx, path, limits)
	if err != nil {
		t.Fatalf("Open(reopen) error = %v", err)
	}
	if _, err := store.Retentions(
		ctx,
		1,
	); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("Retentions(undersized maximum) error = %v", err)
	}
	retentions, err := store.Retentions(ctx, limits.MaxRetentions)
	if err != nil {
		t.Fatalf("Retentions() error = %v", err)
	}
	if len(retentions) != 2 {
		t.Fatalf("Retentions() count = %d, want 2", len(retentions))
	}
	for index, retention := range retentions {
		if retention.Root() != root {
			t.Fatalf("Retentions()[%d].Root() = %x, want %x", index, retention.Root(), root)
		}
	}
	if err := retentions[0].Release(ctx); err != nil {
		t.Fatalf("Release(first recovered) error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close(after release) error = %v", err)
	}

	store, err = filesystem.Open(ctx, path, limits)
	if err != nil {
		t.Fatalf("Open(second reopen) error = %v", err)
	}
	retentions, err = store.Retentions(ctx, limits.MaxRetentions)
	if err != nil {
		t.Fatalf("Retentions(second reopen) error = %v", err)
	}
	if len(retentions) != 1 || retentions[0].Root() != root {
		t.Fatalf("Retentions(second reopen) = %v, want one lease for %x", retentions, root)
	}
	if err := retentions[0].Release(ctx); err != nil {
		t.Fatalf("Release(second recovered) error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close(second reopen) error = %v", err)
	}

	store, err = filesystem.Open(ctx, path, limits)
	if err != nil {
		t.Fatalf("Open(final) error = %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("Close(final) error = %v", closeErr)
		}
	}()
	retentions, err = store.Retentions(ctx, limits.MaxRetentions)
	if err != nil {
		t.Fatalf("Retentions(final) error = %v", err)
	}
	if len(retentions) != 0 {
		t.Fatalf("Retentions(final) count = %d, want 0", len(retentions))
	}
	if err := first.Release(ctx); !errors.Is(err, mpt.ErrClosedStore) {
		t.Fatalf("Release(original closed lease) error = %v", err)
	}
}

func TestStoreRetentionValidatesRootLimitsAndEnumerationBounds(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "trie")
	limits := filesystem.DefaultLimits()
	limits.MaxRetentions = 1
	store, err := filesystem.Open(ctx, path, limits)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	}()
	trie := mustFilesystemRetentionTrie(t)
	trie, err = trie.Commit(ctx, store)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	root, err := trie.Root()
	if err != nil {
		t.Fatalf("Root() error = %v", err)
	}
	var missing mpt.Root
	missing[0] = 1
	if _, err := store.RetainRoot(
		ctx,
		missing,
		mpt.DefaultReachabilityLimits(),
	); !errors.Is(err, mpt.ErrMissingNode) {
		t.Fatalf("RetainRoot(missing) error = %v", err)
	}
	if _, err := store.RetainRoot(
		ctx,
		root,
		mpt.DefaultReachabilityLimits(),
	); err != nil {
		t.Fatalf("RetainRoot() error = %v", err)
	}
	if _, err := store.RetainRoot(
		ctx,
		root,
		mpt.DefaultReachabilityLimits(),
	); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("RetainRoot(over limit) error = %v", err)
	}
	if _, err := store.Retentions(ctx, 0); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("Retentions(zero maximum) error = %v", err)
	}
	if _, err := store.Retentions(ctx, 1); err != nil {
		t.Fatalf("Retentions(exact maximum) error = %v", err)
	}
}

func mustFilesystemRetentionTrie(t *testing.T) mpt.RawTrie {
	t.Helper()

	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	for key, value := range map[string]string{
		"alpha": "a long persistent alpha value",
		"beta":  "a long persistent beta value",
	} {
		trie, err = trie.Update(context.Background(), []byte(key), []byte(value))
		if err != nil {
			t.Fatalf("Update(%q) error = %v", key, err)
		}
	}
	return trie
}
