package mpt_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
	"golang.org/x/crypto/sha3"
)

func TestRawTrieImmutableUpdateLookupAndDelete(t *testing.T) {
	t.Parallel()

	empty, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	if root := mustTrieRoot(t, empty); root != mpt.EmptyRoot() {
		t.Fatalf("empty Root() = %x", root)
	}
	if _, err := empty.Get(context.Background(), []byte("dog")); !errors.Is(err, mpt.ErrAbsentKey) {
		t.Fatalf("empty Get() error = %v, want ErrAbsentKey", err)
	}
	has, err := empty.Has(context.Background(), []byte("dog"))
	if err != nil || has {
		t.Fatalf("empty Has() = (%v, %v), want (false, nil)", has, err)
	}

	key := []byte("dog")
	value := []byte("puppy")
	withDog, err := empty.Update(context.Background(), key, value)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	key[0] = 'f'
	value[0] = 'x'
	got, err := withDog.Get(context.Background(), []byte("dog"))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !slices.Equal(got, []byte("puppy")) {
		t.Fatalf("Get() = %q", got)
	}
	got[0] = 'x'
	gotAgain, err := withDog.Get(context.Background(), []byte("dog"))
	if err != nil || !slices.Equal(gotAgain, []byte("puppy")) {
		t.Fatalf("second Get() = (%q, %v)", gotAgain, err)
	}
	if _, err := empty.Get(context.Background(), []byte("dog")); !errors.Is(err, mpt.ErrAbsentKey) {
		t.Fatalf("old snapshot changed: Get() error = %v", err)
	}

	replaced, err := withDog.Update(context.Background(), []byte("dog"), []byte("hound"))
	if err != nil {
		t.Fatalf("replacement Update() error = %v", err)
	}
	original, err := withDog.Get(context.Background(), []byte("dog"))
	if err != nil || string(original) != "puppy" {
		t.Fatalf("old snapshot Get() = (%q, %v)", original, err)
	}

	deleted, err := replaced.Delete(context.Background(), []byte("dog"))
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if root := mustTrieRoot(t, deleted); root != mpt.EmptyRoot() {
		t.Fatalf("root after final delete = %x", root)
	}
	if _, err := deleted.Delete(context.Background(), []byte("dog")); !errors.Is(err, mpt.ErrAbsentKey) {
		t.Fatalf("absent Delete() error = %v, want ErrAbsentKey", err)
	}
}

func TestRawTrieStrictPrefixesAreHistoryIndependent(t *testing.T) {
	t.Parallel()

	entries := []struct {
		key   []byte
		value []byte
	}{
		{key: []byte("do"), value: []byte("verb")},
		{key: []byte("dog"), value: []byte("puppy")},
		{key: []byte("doge"), value: []byte("coin")},
		{key: []byte("horse"), value: []byte("stallion")},
	}
	orders := [][]int{
		{0, 1, 2, 3},
		{3, 2, 1, 0},
		{1, 3, 0, 2},
	}

	var expected mpt.Root
	for orderIndex, order := range orders {
		trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
		if err != nil {
			t.Fatalf("NewRawTrie() error = %v", err)
		}
		for _, index := range order {
			trie, err = trie.Update(context.Background(), entries[index].key, entries[index].value)
			if err != nil {
				t.Fatalf("Update(%q) error = %v", entries[index].key, err)
			}
		}
		if orderIndex == 0 {
			expected = mustTrieRoot(t, trie)
		} else if root := mustTrieRoot(t, trie); root != expected {
			t.Fatalf("order %v root = %x, want %x", order, root, expected)
		}
		for _, entry := range entries {
			got, getErr := trie.Get(context.Background(), entry.key)
			if getErr != nil || !slices.Equal(got, entry.value) {
				t.Fatalf("Get(%q) = (%q, %v)", entry.key, got, getErr)
			}
		}
	}
}

func TestRawTrieDeletionCompactsToEquivalentState(t *testing.T) {
	t.Parallel()

	full := mustRawTrie(t, map[string]string{
		"do":    "verb",
		"dog":   "puppy",
		"doge":  "coin",
		"horse": "stallion",
	})
	deleted, err := full.Delete(context.Background(), []byte("dog"))
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	expected := mustRawTrie(t, map[string]string{
		"do":    "verb",
		"doge":  "coin",
		"horse": "stallion",
	})
	if got, want := mustTrieRoot(t, deleted), mustTrieRoot(t, expected); got != want {
		t.Fatalf("compacted root = %x, want %x", got, want)
	}

	deleted, err = deleted.Delete(context.Background(), []byte("do"))
	if err != nil {
		t.Fatalf("Delete(prefix) error = %v", err)
	}
	expected = mustRawTrie(t, map[string]string{
		"doge":  "coin",
		"horse": "stallion",
	})
	if got, want := mustTrieRoot(t, deleted), mustTrieRoot(t, expected); got != want {
		t.Fatalf("root after prefix delete = %x, want %x", got, want)
	}
}

func TestEmptyValueHasDeletionSemantics(t *testing.T) {
	t.Parallel()

	trie := mustRawTrie(t, map[string]string{"key": "value"})
	deleted, err := trie.Update(context.Background(), []byte("key"), nil)
	if err != nil {
		t.Fatalf("Update(empty) error = %v", err)
	}
	if root := mustTrieRoot(t, deleted); root != mpt.EmptyRoot() {
		t.Fatalf("Update(empty) root = %x", root)
	}
	_, err = deleted.Update(context.Background(), []byte("absent"), nil)
	if !errors.Is(err, mpt.ErrAbsentKey) {
		t.Fatalf("Update(absent, empty) error = %v, want ErrAbsentKey", err)
	}
}

func TestSecureTrieHashesKeysExactlyOnce(t *testing.T) {
	t.Parallel()

	secure, err := mpt.NewSecureTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewSecureTrie() error = %v", err)
	}
	secure, err = secure.Update(context.Background(), []byte("key"), []byte("value"))
	if err != nil {
		t.Fatalf("secure Update() error = %v", err)
	}
	raw, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	raw, err = raw.Update(
		context.Background(),
		legacyKeccakForTest([]byte("key")),
		[]byte("value"),
	)
	if err != nil {
		t.Fatalf("raw Update() error = %v", err)
	}
	if secureRoot, rawRoot := mustTrieRoot(t, secure), mustTrieRoot(t, raw); secureRoot != rawRoot {
		t.Fatalf("secure root = %x, raw transformed root = %x", secureRoot, rawRoot)
	}
	if _, err := secure.Get(context.Background(), []byte("key")); err != nil {
		t.Fatalf("secure Get() error = %v", err)
	}
	if _, err := secure.Get(context.Background(), legacyKeccakForTest([]byte("key"))); !errors.Is(err, mpt.ErrAbsentKey) {
		t.Fatalf("secure Get(prehashed key) error = %v, want ErrAbsentKey", err)
	}
}

func TestTrieValidatesLimitsInputsAndContext(t *testing.T) {
	t.Parallel()

	var zero mpt.RawTrie
	if _, err := zero.Root(); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero RawTrie Root() error = %v, want ErrUninitialized", err)
	}

	invalidLimits := mpt.Limits{}
	if _, err := mpt.NewRawTrie(invalidLimits); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("NewRawTrie(invalid limits) error = %v, want ErrResourceLimit", err)
	}
	limits := mpt.DefaultLimits()
	limits.MaxKeyBytes = 1
	limits.MaxValueBytes = 1
	trie, err := mpt.NewRawTrie(limits)
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	if _, err := trie.Update(context.Background(), []byte{1, 2}, []byte{1}); !errors.Is(err, mpt.ErrInvalidKey) {
		t.Fatalf("Update(long key) error = %v, want ErrInvalidKey", err)
	}
	if _, err := trie.Update(context.Background(), []byte{1}, []byte{1, 2}); !errors.Is(err, mpt.ErrInvalidValue) {
		t.Fatalf("Update(long value) error = %v, want ErrInvalidValue", err)
	}
	var nilContext context.Context
	if _, err := trie.Get(nilContext, []byte{1}); !errors.Is(err, mpt.ErrInvalidContext) {
		t.Fatalf("Get(nil context) error = %v, want ErrInvalidContext", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := trie.Get(ctx, []byte{1}); !errors.Is(err, context.Canceled) || !errors.Is(err, mpt.ErrCanceled) {
		t.Fatalf("Get(canceled) error = %v", err)
	}
}

func TestTrieBoundsEncodingAndHashWork(t *testing.T) {
	t.Parallel()

	nodeLimited := mpt.DefaultLimits()
	nodeLimited.MaxEncodingNodes = 1
	trie, err := mpt.NewRawTrie(nodeLimited)
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	trie, err = trie.Update(context.Background(), []byte{0x10}, []byte{1})
	if err != nil {
		t.Fatalf("first Update() error = %v", err)
	}
	if _, err := trie.Update(context.Background(), []byte{0x20}, []byte{2}); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("node-limited Update() error = %v, want ErrResourceLimit", err)
	}
	if _, err := trie.Get(context.Background(), []byte{0x20}); !errors.Is(err, mpt.ErrAbsentKey) {
		t.Fatalf("failed update changed old snapshot: Get() error = %v", err)
	}

	hashLimited := mpt.DefaultLimits()
	hashLimited.MaxHashOperations = 1
	secure, err := mpt.NewSecureTrie(hashLimited)
	if err != nil {
		t.Fatalf("NewSecureTrie() error = %v", err)
	}
	if _, err := secure.Update(context.Background(), []byte("key"), []byte("value")); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("hash-limited secure Update() error = %v, want ErrResourceLimit", err)
	}
}

func mustRawTrie(t *testing.T, entries map[string]string) mpt.RawTrie {
	t.Helper()
	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	for key, value := range entries {
		trie, err = trie.Update(context.Background(), []byte(key), []byte(value))
		if err != nil {
			t.Fatalf("Update(%q) error = %v", key, err)
		}
	}
	return trie
}

func legacyKeccakForTest(value []byte) []byte {
	hash := sha3.NewLegacyKeccak256()
	_, _ = hash.Write(value)
	return hash.Sum(nil)
}

type rootedTrie interface {
	Root() (mpt.Root, error)
}

func mustTrieRoot(t *testing.T, trie rootedTrie) mpt.Root {
	t.Helper()
	root, err := trie.Root()
	if err != nil {
		t.Fatalf("Root() error = %v", err)
	}
	return root
}
