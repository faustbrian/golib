package filesystem_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/filesystem"
)

func BenchmarkFilesystemWarmGet(b *testing.B) {
	ctx := context.Background()
	store, loaded, key, _ := benchmarkFilesystemTrie(b, ctx)
	b.Cleanup(func() {
		if err := store.Close(); err != nil {
			b.Errorf("Close() error = %v", err)
		}
	})

	b.ReportAllocs()
	b.SetBytes(int64(len(key)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := loaded.Get(ctx, key); err != nil {
			b.Fatalf("Get() error = %v", err)
		}
	}
}

func BenchmarkFilesystemOpenAndGet(b *testing.B) {
	ctx := context.Background()
	store, loaded, key, path := benchmarkFilesystemTrie(b, ctx)
	root, err := loaded.Root()
	if err != nil {
		b.Fatalf("Root() error = %v", err)
	}
	if err := store.Close(); err != nil {
		b.Fatalf("Close() error = %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		opened, openErr := filesystem.Open(
			ctx,
			path,
			filesystem.DefaultLimits(),
		)
		if openErr != nil {
			b.Fatalf("Open() error = %v", openErr)
		}
		snapshot, loadErr := mpt.LoadRawTrie(
			root,
			opened,
			mpt.DefaultLimits(),
		)
		if loadErr != nil {
			b.Fatalf("LoadRawTrie() error = %v", loadErr)
		}
		if _, getErr := snapshot.Get(ctx, key); getErr != nil {
			b.Fatalf("Get() error = %v", getErr)
		}
		if closeErr := opened.Close(); closeErr != nil {
			b.Fatalf("Close() error = %v", closeErr)
		}
	}
}

func BenchmarkFilesystemCommit(b *testing.B) {
	ctx := context.Background()
	store, trie, _, _ := benchmarkFilesystemTrie(b, ctx)
	b.Cleanup(func() {
		if err := store.Close(); err != nil {
			b.Errorf("Close() error = %v", err)
		}
	})

	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; b.Loop(); index++ {
		next, err := trie.Update(
			ctx,
			[]byte(fmt.Sprintf("commit-%08d", index)),
			[]byte("filesystem benchmark value long enough to hash"),
		)
		if err != nil {
			b.Fatalf("Update() error = %v", err)
		}
		trie, err = next.Commit(ctx, store)
		if err != nil {
			b.Fatalf("Commit() error = %v", err)
		}
	}
}

func benchmarkFilesystemTrie(
	b *testing.B,
	ctx context.Context,
) (*filesystem.Store, mpt.RawTrie, []byte, string) {
	b.Helper()
	path := filepath.Join(b.TempDir(), "trie")
	store, err := filesystem.Open(
		ctx,
		path,
		filesystem.DefaultLimits(),
	)
	if err != nil {
		b.Fatalf("Open() error = %v", err)
	}
	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		b.Fatalf("NewRawTrie() error = %v", err)
	}
	var key []byte
	for index := range 256 {
		key = []byte(fmt.Sprintf("key-%04d", index))
		trie, err = trie.Update(
			ctx,
			key,
			[]byte(fmt.Sprintf("persistent benchmark value %04d", index)),
		)
		if err != nil {
			b.Fatalf("Update() error = %v", err)
		}
	}
	trie, err = trie.Commit(ctx, store)
	if err != nil {
		b.Fatalf("Commit() error = %v", err)
	}
	root, err := trie.Root()
	if err != nil {
		b.Fatalf("Root() error = %v", err)
	}
	loaded, err := mpt.LoadRawTrie(root, store, mpt.DefaultLimits())
	if err != nil {
		b.Fatalf("LoadRawTrie() error = %v", err)
	}
	return store, loaded, key, path
}
