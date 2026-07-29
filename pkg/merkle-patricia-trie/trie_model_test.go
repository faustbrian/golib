package mpt_test

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"slices"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

func TestRawTrieExhaustiveSmallOperationHistories(t *testing.T) {
	t.Parallel()

	keys := [][]byte{nil, {0x10}, {0x11}, {0x10, 0x20}}
	values := [][]byte{{0x01}, {0x80, 0x02}}
	initial, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}

	var explore func(mpt.RawTrie, map[string][]byte, int)
	explore = func(trie mpt.RawTrie, model map[string][]byte, remaining int) {
		assertTrieMatchesModel(t, trie, model, keys)
		if remaining == 0 {
			return
		}
		for _, key := range keys {
			for _, value := range values {
				next, updateErr := trie.Update(context.Background(), key, value)
				if updateErr != nil {
					t.Fatalf("Update(%x, %x) error = %v", key, value, updateErr)
				}
				nextModel := maps.Clone(model)
				nextModel[string(key)] = append([]byte(nil), value...)
				explore(next, nextModel, remaining-1)
			}

			next, deleteErr := trie.Delete(context.Background(), key)
			_, present := model[string(key)]
			if present {
				if deleteErr != nil {
					t.Fatalf("Delete(%x) error = %v", key, deleteErr)
				}
				nextModel := maps.Clone(model)
				delete(nextModel, string(key))
				explore(next, nextModel, remaining-1)
			} else if !errors.Is(deleteErr, mpt.ErrAbsentKey) {
				t.Fatalf("Delete(absent %x) error = %v", key, deleteErr)
			}
		}
	}

	explore(initial, map[string][]byte{}, 4)
}

func assertTrieMatchesModel(
	t *testing.T,
	trie mpt.RawTrie,
	model map[string][]byte,
	keys [][]byte,
) {
	t.Helper()

	for _, key := range keys {
		got, err := trie.Get(context.Background(), key)
		want, present := model[string(key)]
		if present {
			if err != nil || !slices.Equal(got, want) {
				t.Fatalf("Get(%x) = (%x, %v), want %x", key, got, err, want)
			}
		} else if !errors.Is(err, mpt.ErrAbsentKey) {
			t.Fatalf("Get(absent %x) error = %v", key, err)
		}
	}

	rebuilt, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	ordered := make([]string, 0, len(model))
	for key := range model {
		ordered = append(ordered, key)
	}
	slices.SortFunc(ordered, func(left, right string) int {
		return bytes.Compare([]byte(left), []byte(right))
	})
	for _, key := range ordered {
		rebuilt, err = rebuilt.Update(context.Background(), []byte(key), model[key])
		if err != nil {
			t.Fatalf("rebuild Update(%x) error = %v", key, err)
		}
	}
	if got, want := mustTrieRoot(t, trie), mustTrieRoot(t, rebuilt); got != want {
		t.Fatalf("history root = %x, canonical rebuild root = %x", got, want)
	}
}
