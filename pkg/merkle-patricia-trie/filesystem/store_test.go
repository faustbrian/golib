package filesystem_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"sort"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/filesystem"
)

func TestStorePersistsPublishedTrieAcrossReopen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "trie")
	store, err := filesystem.Open(ctx, path, filesystem.DefaultLimits())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	trie, err = trie.Update(
		ctx,
		[]byte("persistent-key"),
		[]byte("persistent value long enough to create stored nodes"),
	)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	committed, err := trie.Commit(ctx, store)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	wantRoot, err := committed.Root()
	if err != nil {
		t.Fatalf("Root() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := filesystem.Open(ctx, path, filesystem.DefaultLimits())
	if err != nil {
		t.Fatalf("Open(reopen) error = %v", err)
	}
	if got := reopened.Root(); got != wantRoot {
		t.Fatalf("Root() = %x, want %x", got, wantRoot)
	}

	loaded, err := mpt.LoadRawTrie(wantRoot, reopened, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("LoadRawTrie() error = %v", err)
	}
	value, err := loaded.Get(ctx, []byte("persistent-key"))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !bytes.Equal(
		value,
		[]byte("persistent value long enough to create stored nodes"),
	) {
		t.Fatalf("Get() = %q", value)
	}

	if err := reopened.Close(); err != nil {
		t.Fatalf("Close(second) error = %v", err)
	}
	if _, err := reopened.GetNode(ctx, wantRoot); !errors.Is(err, mpt.ErrClosedStore) {
		t.Fatalf("GetNode(closed) error = %v, want ErrClosedStore", err)
	}
}

func TestPersistentConstructionMatchesOrdinaryStreamingAndRebuild(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	entries := []struct {
		key   []byte
		value []byte
	}{
		{key: []byte{0x00}, value: []byte("zero persistent value")},
		{key: []byte{0x00, 0x01}, value: []byte("prefix persistent value")},
		{key: []byte{0x10}, value: []byte("ten persistent value")},
		{key: []byte{0xff}, value: []byte("last persistent value")},
	}
	ordinary, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	builder, err := mpt.NewSortedBuilder(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewSortedBuilder() error = %v", err)
	}
	for _, entry := range entries {
		ordinary, err = ordinary.Update(ctx, entry.key, entry.value)
		if err != nil {
			t.Fatalf("Update(%x) error = %v", entry.key, err)
		}
		if err := builder.Add(ctx, entry.key, entry.value); err != nil {
			t.Fatalf("Add(%x) error = %v", entry.key, err)
		}
	}
	ordinaryRoot, err := ordinary.Root()
	if err != nil {
		t.Fatalf("Root(ordinary) error = %v", err)
	}
	streamingRoot, err := builder.Finalize(ctx)
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if streamingRoot != ordinaryRoot {
		t.Fatalf("streaming root = %x, want %x", streamingRoot, ordinaryRoot)
	}

	path := filepath.Join(t.TempDir(), "trie")
	store, err := filesystem.Open(ctx, path, filesystem.DefaultLimits())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	committed, err := ordinary.Commit(ctx, store)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	store, err = filesystem.Open(ctx, path, filesystem.DefaultLimits())
	if err != nil {
		t.Fatalf("Open(reopen) error = %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("Close(reopen) error = %v", closeErr)
		}
	}()
	loaded, err := mpt.LoadRawTrie(ordinaryRoot, store, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("LoadRawTrie() error = %v", err)
	}
	rebuilt, err := loaded.Rebuild(ctx)
	if err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	rebuiltRoot, err := rebuilt.Root()
	if err != nil {
		t.Fatalf("Root(rebuilt) error = %v", err)
	}
	committedRoot, err := committed.Root()
	if err != nil {
		t.Fatalf("Root(committed) error = %v", err)
	}
	if rebuiltRoot != ordinaryRoot || committedRoot != ordinaryRoot {
		t.Fatalf(
			"roots = rebuilt %x, committed %x, want %x",
			rebuiltRoot,
			committedRoot,
			ordinaryRoot,
		)
	}

	var keys [][]byte
	if err := loaded.Iterate(ctx, mpt.IterationOptions{}, func(
		entry mpt.Entry,
	) error {
		keys = append(keys, entry.Key())
		return nil
	}); err != nil {
		t.Fatalf("Iterate() error = %v", err)
	}
	if !sort.SliceIsSorted(keys, func(left, right int) bool {
		return bytes.Compare(keys[left], keys[right]) < 0
	}) {
		t.Fatalf("persistent iteration keys = %x", keys)
	}
}
