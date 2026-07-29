package mpt_test

import (
	"context"
	"errors"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/memory"
)

func TestZeroTrieValuesRejectEveryOperation(t *testing.T) {
	t.Parallel()

	var raw mpt.RawTrie
	if _, err := raw.Has(context.Background(), nil); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero RawTrie Has() error = %v", err)
	}
	if _, err := raw.Update(context.Background(), nil, []byte{1}); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero RawTrie Update() error = %v", err)
	}
	if _, err := raw.Delete(context.Background(), nil); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero RawTrie Delete() error = %v", err)
	}
	if _, err := raw.Commit(context.Background(), memory.New()); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero RawTrie Commit() error = %v", err)
	}
	if err := raw.Iterate(
		context.Background(), mpt.IterationOptions{}, func(mpt.Entry) error { return nil },
	); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero RawTrie Iterate() error = %v", err)
	}
	if _, err := raw.Prove(context.Background(), nil); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero RawTrie Prove() error = %v", err)
	}
	if _, err := raw.Rebuild(context.Background()); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero RawTrie Rebuild() error = %v", err)
	}

	var secure mpt.SecureTrie
	if _, err := secure.Root(); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero SecureTrie Root() error = %v", err)
	}
	if _, err := secure.Has(context.Background(), nil); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero SecureTrie Has() error = %v", err)
	}
	if _, err := secure.Update(context.Background(), nil, []byte{1}); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero SecureTrie Update() error = %v", err)
	}
	if _, err := secure.Delete(context.Background(), nil); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero SecureTrie Delete() error = %v", err)
	}
	if _, err := secure.ApplyBatch(
		context.Background(), []mpt.Mutation{mpt.Put(nil, []byte{1})},
	); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero SecureTrie ApplyBatch() error = %v", err)
	}
	if _, err := secure.Commit(context.Background(), memory.New()); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero SecureTrie Commit() error = %v", err)
	}
	if err := secure.IterateHashed(
		context.Background(), mpt.IterationOptions{}, func(mpt.Entry) error { return nil },
	); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero SecureTrie IterateHashed() error = %v", err)
	}
	if _, err := secure.Prove(context.Background(), nil); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero SecureTrie Prove() error = %v", err)
	}
	if _, err := secure.Rebuild(context.Background()); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero SecureTrie Rebuild() error = %v", err)
	}
}

func TestSecureTrieCompleteOperationSurface(t *testing.T) {
	t.Parallel()

	trie, err := mpt.NewSecureTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewSecureTrie() error = %v", err)
	}
	if has, err := trie.Has(context.Background(), []byte("key")); err != nil || has {
		t.Fatalf("Has(absent) = (%t, %v)", has, err)
	}
	trie, err = trie.ApplyBatch(context.Background(), []mpt.Mutation{
		mpt.Put([]byte("key"), []byte("value")),
		mpt.Put([]byte("other"), []byte("second")),
	})
	if err != nil {
		t.Fatalf("ApplyBatch() error = %v", err)
	}
	if has, err := trie.Has(context.Background(), []byte("key")); err != nil || !has {
		t.Fatalf("Has(present) = (%t, %v)", has, err)
	}
	trie, err = trie.Delete(context.Background(), []byte("other"))
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	store := memory.New()
	trie, err = trie.Commit(context.Background(), store)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	loaded, err := mpt.LoadSecureTrie(mustSecureRoot(t, trie), store, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("LoadSecureTrie() error = %v", err)
	}
	if got, err := loaded.Get(context.Background(), []byte("key")); err != nil ||
		string(got) != "value" {
		t.Fatalf("loaded Get(key) = (%q, %v)", got, err)
	}
	if _, err := loaded.ApplyBatch(context.Background(), []mpt.Mutation{
		mpt.Remove([]byte("missing")),
	}); !errors.Is(err, mpt.ErrAbsentKey) {
		t.Fatalf("ApplyBatch(absent delete) error = %v", err)
	}
}

func TestPublicValidationErrorBoundaries(t *testing.T) {
	t.Parallel()

	invalidLimits := mpt.DefaultLimits()
	invalidLimits.MaxRebuildNodes = 0
	if _, err := mpt.NewSecureTrie(invalidLimits); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("NewSecureTrie(invalid limits) error = %v", err)
	}
	if _, err := mpt.LoadSecureTrie(
		mpt.EmptyRoot(), nil, mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrInvalidStore) {
		t.Fatalf("LoadSecureTrie(nil store) error = %v", err)
	}

	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := trie.ApplyBatch(ctx, []mpt.Mutation{
		mpt.Put([]byte("key"), []byte("value")),
	}); !errors.Is(err, mpt.ErrCanceled) {
		t.Fatalf("ApplyBatch(canceled) error = %v", err)
	}
	if err := trie.Iterate(
		context.Background(),
		mpt.IterationOptions{Limit: -1},
		func(mpt.Entry) error { return nil },
	); !errors.Is(err, mpt.ErrInvalidIterator) {
		t.Fatalf("Iterate(negative limit) error = %v", err)
	}
	oversized := make([]byte, mpt.DefaultLimits().MaxKeyBytes+1)
	if err := trie.Iterate(
		context.Background(),
		mpt.IterationOptions{Prefix: oversized},
		func(mpt.Entry) error { return nil },
	); !errors.Is(err, mpt.ErrInvalidIterator) {
		t.Fatalf("Iterate(oversized prefix) error = %v", err)
	}
}
