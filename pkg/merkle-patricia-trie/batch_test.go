package mpt_test

import (
	"context"
	"errors"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

func TestRawTrieBatchIsAtomicAndMatchesSequentialUpdates(t *testing.T) {
	t.Parallel()

	original := mustRawTrie(t, map[string]string{"a": "old", "b": "remove"})
	key := []byte("a")
	value := []byte("new")
	changes := []mpt.Mutation{
		mpt.Put(key, value),
		mpt.Remove([]byte("b")),
		mpt.Put([]byte("c"), []byte("add")),
	}
	key[0] = 'x'
	value[0] = 'x'

	batched, err := original.ApplyBatch(context.Background(), changes)
	if err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}
	sequential, err := original.Update(context.Background(), []byte("a"), []byte("new"))
	if err != nil {
		t.Fatalf("sequential Update(a) error = %v", err)
	}
	sequential, err = sequential.Delete(context.Background(), []byte("b"))
	if err != nil {
		t.Fatalf("sequential Delete(b) error = %v", err)
	}
	sequential, err = sequential.Update(context.Background(), []byte("c"), []byte("add"))
	if err != nil {
		t.Fatalf("sequential Update(c) error = %v", err)
	}
	if got, want := mustTrieRoot(t, batched), mustTrieRoot(t, sequential); got != want {
		t.Fatalf("batch root = %x, sequential root = %x", got, want)
	}
	if got, err := original.Get(context.Background(), []byte("a")); err != nil || string(got) != "old" {
		t.Fatalf("old snapshot Get(a) = (%q, %v)", got, err)
	}
}

func TestBatchRejectsInvalidDuplicateAbsentAndOversizedChanges(t *testing.T) {
	t.Parallel()

	limits := mpt.DefaultLimits()
	limits.MaxBatchOperations = 2
	trie, err := mpt.NewRawTrie(limits)
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	trie, err = trie.Update(context.Background(), []byte("present"), []byte("value"))
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	before := mustTrieRoot(t, trie)

	tests := []struct {
		name    string
		changes []mpt.Mutation
		want    error
	}{
		{
			name: "duplicate",
			changes: []mpt.Mutation{
				mpt.Put([]byte("same"), []byte("one")),
				mpt.Remove([]byte("same")),
			},
			want: mpt.ErrDuplicateBatchKey,
		},
		{
			name:    "absent delete",
			changes: []mpt.Mutation{mpt.Remove([]byte("absent"))},
			want:    mpt.ErrAbsentKey,
		},
		{
			name:    "empty put value",
			changes: []mpt.Mutation{mpt.Put([]byte("key"), nil)},
			want:    mpt.ErrInvalidValue,
		},
		{
			name:    "zero mutation",
			changes: []mpt.Mutation{{}},
			want:    mpt.ErrInvalidBatch,
		},
		{
			name: "too many",
			changes: []mpt.Mutation{
				mpt.Put([]byte("a"), []byte("1")),
				mpt.Put([]byte("b"), []byte("2")),
				mpt.Put([]byte("c"), []byte("3")),
			},
			want: mpt.ErrResourceLimit,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			_, batchErr := trie.ApplyBatch(context.Background(), test.changes)
			if !errors.Is(batchErr, test.want) {
				t.Fatalf("ApplyBatch() error = %v, want %v", batchErr, test.want)
			}
			if root := mustTrieRoot(t, trie); root != before {
				t.Fatalf("failed batch changed old root to %x", root)
			}
		})
	}
}

func TestSecureTrieBatchUsesSecureProfile(t *testing.T) {
	t.Parallel()

	secure, err := mpt.NewSecureTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewSecureTrie() error = %v", err)
	}
	secure, err = secure.ApplyBatch(context.Background(), []mpt.Mutation{
		mpt.Put([]byte("a"), []byte("1")),
		mpt.Put([]byte("b"), []byte("2")),
	})
	if err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}
	if got, err := secure.Get(context.Background(), []byte("b")); err != nil || string(got) != "2" {
		t.Fatalf("Get(b) = (%q, %v)", got, err)
	}
}
