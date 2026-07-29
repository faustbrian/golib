package mpt_test

import (
	"context"
	"encoding/hex"
	"slices"
	"strings"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

// These byte-for-byte expected roots are from TrieTests/trieanyorder.json at
// ethereum/tests c67e485ff8b5be9abc8ad15345ec21aa22e290d9.
func TestLegacyEthereumAnyOrderRoots(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries map[string]string
		root    string
	}{
		{
			name: "singleItem",
			entries: map[string]string{
				"A": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			},
			root: "d23786fb4a010da3ce639d66d5e904a11dbc02746d1ce25029e53290cabf28ab",
		},
		{
			name: "dogs",
			entries: map[string]string{
				"doe":          "reindeer",
				"dog":          "puppy",
				"dogglesworth": "cat",
			},
			root: "8aad789dff2f538bca5d8ea56e8abe10f4c7ba3a5dea95fea4cd6e7c3a1168d3",
		},
		{
			name: "puppy",
			entries: map[string]string{
				"do":    "verb",
				"horse": "stallion",
				"doge":  "coin",
				"dog":   "puppy",
			},
			root: "5991bb8c6514148a29db676a14ac506cd2cd5775ace63c30a4fe457715e9ac84",
		},
		{
			name: "foo",
			entries: map[string]string{
				"foo":  "bar",
				"food": "bass",
			},
			root: "17beaa1648bafa633cda809c90c04af50fc8aed3cb40d16efbddee6fdf63c4c3",
		},
		{
			name: "hex",
			entries: map[string]string{
				"0x0045": "0x0123456789",
				"0x4500": "0x9876543210",
			},
			root: "285505fcabe84badc8aa310e2aae17eddc7d120aabec8a476902c8184b3a3503",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
			if err != nil {
				t.Fatalf("NewRawTrie() error = %v", err)
			}
			keys := make([]string, 0, len(test.entries))
			for key := range test.entries {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			for _, key := range keys {
				trie, err = trie.Update(
					context.Background(),
					fixtureBytes(t, key),
					fixtureBytes(t, test.entries[key]),
				)
				if err != nil {
					t.Fatalf("Update(%q) error = %v", key, err)
				}
			}
			wantBytes, err := hex.DecodeString(test.root)
			if err != nil {
				t.Fatalf("decode expected root: %v", err)
			}
			want, err := mpt.RootFromBytes(wantBytes)
			if err != nil {
				t.Fatalf("RootFromBytes() error = %v", err)
			}
			if got := mustTrieRoot(t, trie); got != want {
				t.Fatalf("Root() = %x, want %x", got, want)
			}
		})
	}
}

func fixtureBytes(t *testing.T, value string) []byte {
	t.Helper()
	if !strings.HasPrefix(value, "0x") {
		return []byte(value)
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	if err != nil {
		t.Fatalf("decode fixture hex %q: %v", value, err)
	}
	return decoded
}
