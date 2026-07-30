package memory_test

import (
	"context"
	"slices"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/memory"
)

type retainedFuzzRoot struct {
	lease mpt.RootRetention
	model map[string][]byte
}

func FuzzCommitRetentionAndPruningStateMachine(f *testing.F) {
	f.Add([]byte{0x20, 0x01, 0x88, 0x31, 0x90})
	f.Add([]byte{0x00, 0x20, 0x10, 0x08})
	f.Add([]byte(nil))

	f.Fuzz(func(t *testing.T, operations []byte) {
		if len(operations) > 64 {
			return
		}
		ctx := context.Background()
		limits := mpt.DefaultLimits()
		limits.MaxKeyBytes = 1
		limits.MaxValueBytes = 64
		reachability := mpt.DefaultReachabilityLimits()
		store := memory.New()
		trie, err := mpt.NewRawTrie(limits)
		if err != nil {
			t.Fatalf("NewRawTrie() error = %v", err)
		}
		model := make(map[string][]byte)
		retained := make([]retainedFuzzRoot, 0, 8)

		for step, operation := range operations {
			if step == 24 {
				break
			}
			key := []byte{operation & 0x07}
			if operation&0x80 != 0 {
				if _, exists := model[string(key)]; exists {
					trie, err = trie.Delete(ctx, key)
					if err != nil {
						t.Fatalf("Delete(%x) error = %v", key, err)
					}
					delete(model, string(key))
				}
			} else {
				value := make([]byte, 48)
				for index := range value {
					value[index] = byte(index) ^ operation ^ byte(step)
				}
				trie, err = trie.Update(ctx, key, value)
				if err != nil {
					t.Fatalf("Update(%x) error = %v", key, err)
				}
				model[string(key)] = append([]byte(nil), value...)
			}
			trie, err = trie.Commit(ctx, store)
			if err != nil {
				t.Fatalf("Commit() error = %v", err)
			}

			if operation&0x20 != 0 && len(retained) < 8 {
				root, rootErr := trie.Root()
				if rootErr != nil {
					t.Fatalf("Root() error = %v", rootErr)
				}
				lease, retainErr := store.RetainRoot(
					ctx,
					root,
					reachability,
				)
				if retainErr != nil {
					t.Fatalf("RetainRoot() error = %v", retainErr)
				}
				retained = append(retained, retainedFuzzRoot{
					lease: lease,
					model: cloneFuzzModel(model),
				})
			}
			if operation&0x10 != 0 && len(retained) != 0 {
				if err := retained[0].lease.Release(ctx); err != nil {
					t.Fatalf("Release() error = %v", err)
				}
				retained = retained[1:]
			}
			if operation&0x08 != 0 {
				if _, err := store.Prune(ctx, reachability); err != nil {
					t.Fatalf("Prune() error = %v", err)
				}
			}
			verifyStoredFuzzRoot(t, store, store.Root(), model, limits)
			for _, historical := range retained {
				verifyStoredFuzzRoot(
					t,
					store,
					historical.lease.Root(),
					historical.model,
					limits,
				)
			}
		}

		for _, historical := range retained {
			if err := historical.lease.Release(ctx); err != nil {
				t.Fatalf("final Release() error = %v", err)
			}
		}
		if _, err := store.Prune(ctx, reachability); err != nil {
			t.Fatalf("final Prune() error = %v", err)
		}
		verifyStoredFuzzRoot(t, store, store.Root(), model, limits)
	})
}

func verifyStoredFuzzRoot(
	t *testing.T,
	store *memory.Store,
	root mpt.Root,
	model map[string][]byte,
	limits mpt.Limits,
) {
	t.Helper()
	loaded, err := mpt.LoadRawTrie(root, store, limits)
	if err != nil {
		t.Fatalf("LoadRawTrie(%x) error = %v", root, err)
	}
	seen := make(map[string][]byte)
	err = loaded.Iterate(
		context.Background(),
		mpt.IterationOptions{},
		func(entry mpt.Entry) error {
			seen[string(entry.Key())] = entry.Value()
			return nil
		},
	)
	if err != nil {
		t.Fatalf("Iterate(%x) error = %v", root, err)
	}
	if len(seen) != len(model) {
		t.Fatalf("root %x entry count = %d, want %d", root, len(seen), len(model))
	}
	for key, want := range model {
		if !slices.Equal(seen[key], want) {
			t.Fatalf("root %x value(%x) = %x, want %x", root, key, seen[key], want)
		}
	}
}

func cloneFuzzModel(source map[string][]byte) map[string][]byte {
	cloned := make(map[string][]byte, len(source))
	for key, value := range source {
		cloned[key] = append([]byte(nil), value...)
	}
	return cloned
}
