package mpt_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

func TestRawTrieIterationOrderPrefixRangeAndLimit(t *testing.T) {
	t.Parallel()

	trie := mustRawTrie(t, map[string]string{
		"":     "empty",
		"a":    "a",
		"aa":   "aa",
		"ab":   "ab",
		"b":    "b",
		"\xff": "ff",
	})

	if got := collectRawEntries(t, trie, mpt.IterationOptions{}); !slices.Equal(got, []string{
		"=empty", "a=a", "aa=aa", "ab=ab", "b=b", "\xff=ff",
	}) {
		t.Fatalf("full iteration = %q", got)
	}
	if got := collectRawEntries(t, trie, mpt.IterationOptions{
		Prefix: []byte("a"),
	}); !slices.Equal(got, []string{"a=a", "aa=aa", "ab=ab"}) {
		t.Fatalf("prefix iteration = %q", got)
	}
	if got := collectRawEntries(t, trie, mpt.IterationOptions{
		Start: []byte("aa"),
		End:   []byte("b"),
	}); !slices.Equal(got, []string{"aa=aa", "ab=ab"}) {
		t.Fatalf("range iteration = %q", got)
	}
	if got := collectRawEntries(t, trie, mpt.IterationOptions{
		Limit: 2,
	}); !slices.Equal(got, []string{"=empty", "a=a"}) {
		t.Fatalf("limited iteration = %q", got)
	}
}

func TestIterationOwnsEntryBytesAndPreservesCallbackError(t *testing.T) {
	t.Parallel()

	trie := mustRawTrie(t, map[string]string{"key": "value"})
	var retained mpt.Entry
	err := trie.Iterate(context.Background(), mpt.IterationOptions{}, func(entry mpt.Entry) error {
		retained = entry
		key := entry.Key()
		value := entry.Value()
		key[0] = 'x'
		value[0] = 'x'
		return nil
	})
	if err != nil {
		t.Fatalf("Iterate() error = %v", err)
	}
	if string(retained.Key()) != "key" || string(retained.Value()) != "value" {
		t.Fatalf("retained Entry = (%q, %q)", retained.Key(), retained.Value())
	}

	callbackErr := errors.New("stop")
	err = trie.Iterate(context.Background(), mpt.IterationOptions{}, func(mpt.Entry) error {
		return callbackErr
	})
	if !errors.Is(err, callbackErr) {
		t.Fatalf("Iterate(callback error) = %v", err)
	}
}

func TestLoadedTrieIterationIsSnapshotConsistent(t *testing.T) {
	t.Parallel()

	trie := mustRawTrie(t, map[string]string{"a": "1", "b": "2", "c": "3"})
	store := newTestNodeStore()
	committed, err := trie.Commit(context.Background(), store)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	loaded, err := mpt.LoadRawTrie(mustTrieRoot(t, committed), store, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("LoadRawTrie() error = %v", err)
	}

	if got := collectRawEntries(t, loaded, mpt.IterationOptions{}); !slices.Equal(got, []string{
		"a=1", "b=2", "c=3",
	}) {
		t.Fatalf("loaded iteration = %q", got)
	}
}

func TestSecureIterationReturnsOnlyHashedKeys(t *testing.T) {
	t.Parallel()

	trie, err := mpt.NewSecureTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewSecureTrie() error = %v", err)
	}
	trie, err = trie.Update(context.Background(), []byte("key"), []byte("value"))
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	var entries []mpt.Entry
	err = trie.IterateHashed(
		context.Background(),
		mpt.IterationOptions{},
		func(entry mpt.Entry) error {
			entries = append(entries, entry)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("IterateHashed() error = %v", err)
	}
	if len(entries) != 1 ||
		!slices.Equal(entries[0].Key(), legacyKeccakForTest([]byte("key"))) ||
		string(entries[0].Value()) != "value" {
		t.Fatalf("hashed entries = %#v", entries)
	}
}

func TestIterationValidatesOptionsContextAndBounds(t *testing.T) {
	t.Parallel()

	limits := mpt.DefaultLimits()
	limits.MaxIteratorResults = 1
	trie, err := mpt.NewRawTrie(limits)
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	trie, err = trie.Update(context.Background(), []byte("a"), []byte("1"))
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	trie, err = trie.Update(context.Background(), []byte("b"), []byte("2"))
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	err = trie.Iterate(context.Background(), mpt.IterationOptions{}, func(mpt.Entry) error {
		return nil
	})
	if !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("unbounded Iterate() error = %v, want ErrResourceLimit", err)
	}
	err = trie.Iterate(context.Background(), mpt.IterationOptions{
		Start: []byte("b"),
		End:   []byte("a"),
	}, func(mpt.Entry) error { return nil })
	if !errors.Is(err, mpt.ErrInvalidIterator) {
		t.Fatalf("invalid range error = %v, want ErrInvalidIterator", err)
	}
	err = trie.Iterate(context.Background(), mpt.IterationOptions{Limit: 2}, func(mpt.Entry) error {
		return nil
	})
	if !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("oversized limit error = %v, want ErrResourceLimit", err)
	}
	err = trie.Iterate(context.Background(), mpt.IterationOptions{}, nil)
	if !errors.Is(err, mpt.ErrInvalidIterator) {
		t.Fatalf("nil callback error = %v, want ErrInvalidIterator", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = trie.Iterate(ctx, mpt.IterationOptions{Limit: 1}, func(mpt.Entry) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Iterate() error = %v", err)
	}
}

func collectRawEntries(
	t *testing.T,
	trie mpt.RawTrie,
	options mpt.IterationOptions,
) []string {
	t.Helper()
	var entries []string
	err := trie.Iterate(context.Background(), options, func(entry mpt.Entry) error {
		entries = append(entries, string(entry.Key())+"="+string(entry.Value()))
		return nil
	})
	if err != nil {
		t.Fatalf("Iterate() error = %v", err)
	}
	return entries
}
