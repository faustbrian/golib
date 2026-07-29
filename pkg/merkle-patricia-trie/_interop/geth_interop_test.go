//go:build interoperability

package interop_test

import (
	"context"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	gethtrie "github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

func TestGethRawTrieDifferentialMutationTrace(t *testing.T) {
	t.Parallel()

	oracle := gethtrie.NewEmpty(
		triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil),
	)
	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	runGethDifferentialTrace(
		t,
		func(key, value []byte) error {
			var updateErr error
			trie, updateErr = trie.Update(context.Background(), key, value)
			return updateErr
		},
		func(key []byte) error {
			var deleteErr error
			trie, deleteErr = trie.Delete(context.Background(), key)
			return deleteErr
		},
		func(key []byte) ([]byte, error) {
			return trie.Get(context.Background(), key)
		},
		func() mpt.Root {
			root, rootErr := trie.Root()
			if rootErr != nil {
				t.Fatalf("Root() error = %v", rootErr)
			}
			return root
		},
		oracle.Update,
		oracle.Delete,
		oracle.Get,
		oracle.Hash,
	)
}

func TestGethSecureTrieDifferentialMutationTrace(t *testing.T) {
	t.Parallel()

	database := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	oracle, err := gethtrie.NewSecure(
		common.Hash{}, common.Hash{}, types.EmptyRootHash, database,
	)
	if err != nil {
		t.Fatalf("geth NewSecure() error = %v", err)
	}
	trie, err := mpt.NewSecureTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewSecureTrie() error = %v", err)
	}
	runGethDifferentialTrace(
		t,
		func(key, value []byte) error {
			var updateErr error
			trie, updateErr = trie.Update(context.Background(), key, value)
			return updateErr
		},
		func(key []byte) error {
			var deleteErr error
			trie, deleteErr = trie.Delete(context.Background(), key)
			return deleteErr
		},
		func(key []byte) ([]byte, error) {
			return trie.Get(context.Background(), key)
		},
		func() mpt.Root {
			root, rootErr := trie.Root()
			if rootErr != nil {
				t.Fatalf("Root() error = %v", rootErr)
			}
			return root
		},
		func(key, value []byte) error {
			oracle.MustUpdate(key, value)
			return nil
		},
		func(key []byte) error {
			oracle.MustDelete(key)
			return nil
		},
		func(key []byte) ([]byte, error) {
			return oracle.MustGet(key), nil
		},
		oracle.Hash,
	)
}

func runGethDifferentialTrace(
	t *testing.T,
	update func(key, value []byte) error,
	remove func(key []byte) error,
	get func(key []byte) ([]byte, error),
	root func() mpt.Root,
	oracleUpdate func(key, value []byte) error,
	oracleRemove func(key []byte) error,
	oracleGet func(key []byte) ([]byte, error),
	oracleRoot func() common.Hash,
) {
	t.Helper()
	generator := rand.New(rand.NewPCG(0x6d7074, 0x67657468))
	state := make(map[string][]byte)
	keys := make([][]byte, 24)
	for index := range keys {
		key := make([]byte, generator.IntN(9))
		for offset := range key {
			key[offset] = byte(generator.Uint32())
		}
		keys[index] = key
	}

	for step := range 256 {
		key := keys[generator.IntN(len(keys))]
		keyString := string(key)
		if _, present := state[keyString]; present && generator.IntN(4) == 0 {
			if err := remove(key); err != nil {
				t.Fatalf("step %d Delete() error = %v", step, err)
			}
			if err := oracleRemove(key); err != nil {
				t.Fatalf("step %d geth Delete() error = %v", step, err)
			}
			delete(state, keyString)
		} else {
			value := make([]byte, generator.IntN(64)+1)
			for index := range value {
				value[index] = byte(generator.Uint32())
			}
			if err := update(key, value); err != nil {
				t.Fatalf("step %d Update() error = %v", step, err)
			}
			if err := oracleUpdate(key, value); err != nil {
				t.Fatalf("step %d geth Update() error = %v", step, err)
			}
			state[keyString] = append([]byte(nil), value...)
		}

		if got, want := root(), oracleRoot(); common.Hash(got) != want {
			t.Fatalf("step %d root = %x, geth = %x", step, got, want)
		}
		for stateKey, want := range state {
			got, err := get([]byte(stateKey))
			if err != nil {
				t.Fatalf("step %d Get(%x) error = %v", step, stateKey, err)
			}
			oracleValue, err := oracleGet([]byte(stateKey))
			if err != nil {
				t.Fatalf("step %d geth Get(%x) error = %v", step, stateKey, err)
			}
			if !slices.Equal(got, want) || !slices.Equal(oracleValue, want) {
				t.Fatalf(
					"step %d value(%x) = %x, geth = %x, want %x",
					step,
					stateKey,
					got,
					oracleValue,
					want,
				)
			}
		}
	}
}
