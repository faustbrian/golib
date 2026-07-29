package mpt_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

const legacyFixtureDirectory = "testdata/ethereum-tests/TrieTests"

var legacyFixtureChecksums = map[string]string{
	"hex_encoded_securetrie_test.json": "487f9e1e404e46dc0a54d526b14927d3a5ba90f7f52625e7d49cd170974ce9ff",
	"trieanyorder_secureTrie.json":     "acba24dcef034b8ddd78d4ba8e716468854bac1a5ac886cc806c93f1c93f1ed4",
	"trieanyorder.json":                "92404d5c2076524e62f02e9657a684aa0561067d49f3b489b78b5033c6fc3e2d",
	"trietest_secureTrie.json":         "98b76fd92fed69cb449d7a555cdb3eb397e7179614857de1289de2f63ac8e77a",
	"trietest.json":                    "0ce5e1151210958edf47911b332fe188696d741f9f44b1a471ee62bc666c1f0f",
	"trietestnextprev.json":            "ac9d8f62664d6b47ab25050e16617d617ef3363b4905590829267f9a9d33c6f0",
}

type legacyTrieFixture struct {
	Input      json.RawMessage `json:"in"`
	Root       string          `json:"root"`
	HexEncoded bool            `json:"hexEncoded"`
	Tests      [][]string      `json:"tests"`
}

func TestLegacyEthereumFixtureChecksums(t *testing.T) {
	t.Parallel()

	for name, want := range legacyFixtureChecksums {
		name, want := name, want
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			contents := readFixture(t, name)
			if got := fmt.Sprintf("%x", sha256.Sum256(contents)); got != want {
				t.Fatalf("SHA-256 = %s, want %s", got, want)
			}
		})
	}
}

func TestLegacyEthereumTrieRoots(t *testing.T) {
	t.Parallel()

	for name := range legacyFixtureChecksums {
		if name == "trietestnextprev.json" {
			continue
		}
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixtures := decodeLegacyFixtures(t, name)
			secure := strings.Contains(strings.ToLower(name), "securetrie")
			names := make([]string, 0, len(fixtures))
			for fixtureName := range fixtures {
				names = append(names, fixtureName)
			}
			slices.Sort(names)
			for _, fixtureName := range names {
				fixtureName := fixtureName
				t.Run(fixtureName, func(t *testing.T) {
					t.Parallel()
					runLegacyRootFixture(t, fixtures[fixtureName], secure)
				})
			}
		})
	}
}

func TestLegacyEthereumNextPreviousFixtures(t *testing.T) {
	t.Parallel()

	fixtures := decodeLegacyFixtures(t, "trietestnextprev.json")
	names := make([]string, 0, len(fixtures))
	for name := range fixtures {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		fixture := fixtures[name]
		var input []string
		if err := json.Unmarshal(fixture.Input, &input); err != nil {
			t.Fatalf("%s input: %v", name, err)
		}
		trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
		if err != nil {
			t.Fatalf("%s NewRawTrie() error = %v", name, err)
		}
		for _, key := range input {
			trie, err = trie.Update(
				context.Background(), fixtureBytes(t, key), []byte(key),
			)
			if err != nil {
				t.Fatalf("%s Update(%q) error = %v", name, key, err)
			}
		}
		var ordered [][]byte
		err = trie.Iterate(
			context.Background(),
			mpt.IterationOptions{},
			func(entry mpt.Entry) error {
				ordered = append(ordered, entry.Key())
				return nil
			},
		)
		if err != nil {
			t.Fatalf("%s Iterate() error = %v", name, err)
		}
		for _, test := range fixture.Tests {
			if len(test) != 3 {
				t.Fatalf("%s invalid neighbor vector %#v", name, test)
			}
			previous, next := neighbors(ordered, fixtureBytes(t, test[0]))
			if !bytes.Equal(previous, fixtureBytes(t, test[1])) ||
				!bytes.Equal(next, fixtureBytes(t, test[2])) {
				t.Fatalf(
					"%s neighbors(%q) = (%q, %q), want (%q, %q)",
					name, test[0], previous, next, test[1], test[2],
				)
			}
		}
	}
}

func runLegacyRootFixture(
	t *testing.T,
	fixture legacyTrieFixture,
	secure bool,
) {
	t.Helper()
	if fixture.Root == "" {
		t.Fatal("fixture has no expected root")
	}
	if secure {
		trie, err := mpt.NewSecureTrie(mpt.DefaultLimits())
		if err != nil {
			t.Fatalf("NewSecureTrie() error = %v", err)
		}
		applyLegacyOperations(t, fixture.Input, func(key, value []byte) error {
			var updateErr error
			trie, updateErr = trie.Update(context.Background(), key, value)
			return updateErr
		})
		assertFixtureRoot(t, mustSecureRoot(t, trie), fixture.Root)
		return
	}

	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	applyLegacyOperations(t, fixture.Input, func(key, value []byte) error {
		var updateErr error
		trie, updateErr = trie.Update(context.Background(), key, value)
		return updateErr
	})
	assertFixtureRoot(t, mustTrieRoot(t, trie), fixture.Root)
}

func applyLegacyOperations(
	t *testing.T,
	input json.RawMessage,
	apply func(key, value []byte) error,
) {
	t.Helper()
	var ordered [][]*string
	if err := json.Unmarshal(input, &ordered); err == nil && ordered != nil {
		for _, operation := range ordered {
			if len(operation) != 2 || operation[0] == nil {
				t.Fatalf("invalid ordered operation %#v", operation)
			}
			var value []byte
			if operation[1] != nil {
				value = fixtureBytes(t, *operation[1])
			}
			if err := apply(fixtureBytes(t, *operation[0]), value); err != nil {
				t.Fatalf("apply(%q) error = %v", *operation[0], err)
			}
		}
		return
	}

	var unordered map[string]*string
	if err := json.Unmarshal(input, &unordered); err != nil {
		t.Fatalf("decode fixture input: %v", err)
	}
	keys := make([]string, 0, len(unordered))
	for key := range unordered {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		var value []byte
		if unordered[key] != nil {
			value = fixtureBytes(t, *unordered[key])
		}
		if err := apply(fixtureBytes(t, key), value); err != nil {
			t.Fatalf("apply(%q) error = %v", key, err)
		}
	}
}

func assertFixtureRoot(t *testing.T, got mpt.Root, expected string) {
	t.Helper()
	wantBytes, err := hex.DecodeString(strings.TrimPrefix(expected, "0x"))
	if err != nil {
		t.Fatalf("decode expected root: %v", err)
	}
	want, err := mpt.RootFromBytes(wantBytes)
	if err != nil {
		t.Fatalf("RootFromBytes() error = %v", err)
	}
	if got != want {
		t.Fatalf("Root() = %x, want %x", got, want)
	}
}

func decodeLegacyFixtures(
	t *testing.T,
	name string,
) map[string]legacyTrieFixture {
	t.Helper()
	var fixtures map[string]legacyTrieFixture
	if err := json.Unmarshal(readFixture(t, name), &fixtures); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return fixtures
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(legacyFixtureDirectory, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return contents
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

func neighbors(ordered [][]byte, query []byte) ([]byte, []byte) {
	var previous, next []byte
	for _, key := range ordered {
		comparison := bytes.Compare(key, query)
		if comparison < 0 {
			previous = key
			continue
		}
		if comparison > 0 {
			next = key
			break
		}
	}
	return previous, next
}
