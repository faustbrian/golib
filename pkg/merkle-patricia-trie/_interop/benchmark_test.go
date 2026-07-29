//go:build interoperability

package interop_test

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	gethtrie "github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

var gethBenchmarkBytes []byte

func BenchmarkComparableRawGetOwned(b *testing.B) {
	oracle := gethtrie.NewEmpty(
		triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil),
	)
	local, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	for index := range 1024 {
		key := comparableKey(index)
		value := comparableValue(index)
		if err = oracle.Update(key, value); err != nil {
			b.Fatal(err)
		}
		local, err = local.Update(ctx, key, value)
		if err != nil {
			b.Fatal(err)
		}
	}
	localRoot, err := local.Root()
	if err != nil {
		b.Fatal(err)
	}
	if common.Hash(localRoot) != oracle.Hash() {
		b.Fatalf("comparison corpus root mismatch")
	}
	key := comparableKey(512)
	b.ResetTimer()
	for b.Loop() {
		value, getErr := oracle.Get(key)
		if getErr != nil {
			b.Fatal(getErr)
		}
		gethBenchmarkBytes = append([]byte(nil), value...)
	}
}

func comparableKey(index int) []byte {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, uint64(index))
	return key
}

func comparableValue(index int) []byte {
	value := make([]byte, 32)
	binary.BigEndian.PutUint64(value[24:], uint64(index+1))
	return value
}
