package mpt_test

import (
	"context"
	"fmt"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/memory"
)

func ExampleRawTrie() {
	trie, _ := mpt.NewRawTrie(mpt.DefaultLimits())
	trie, _ = trie.Update(context.Background(), []byte("dog"), []byte("puppy"))
	value, _ := trie.Get(context.Background(), []byte("dog"))
	fmt.Println(string(value))
	// Output: puppy
}

func ExampleSecureTrie() {
	trie, _ := mpt.NewSecureTrie(mpt.DefaultLimits())
	trie, _ = trie.Update(context.Background(), []byte("address"), []byte("account"))
	value, _ := trie.Get(context.Background(), []byte("address"))
	fmt.Println(string(value))
	// Output: account
}

func ExampleRawTrie_Prove() {
	trie, _ := mpt.NewRawTrie(mpt.DefaultLimits())
	trie, _ = trie.Update(context.Background(), []byte("dog"), []byte("puppy"))
	root, _ := trie.Root()
	proof, _ := trie.Prove(context.Background(), []byte("dog"))
	err := mpt.VerifyRawMembership(
		context.Background(),
		root,
		[]byte("dog"),
		[]byte("puppy"),
		proof,
		mpt.DefaultLimits(),
	)
	fmt.Println(err == nil)
	// Output: true
}

func ExampleNodeStore() {
	store := memory.New()
	trie, _ := mpt.NewRawTrie(mpt.DefaultLimits())
	trie, _ = trie.Update(context.Background(), []byte("key"), []byte("value"))
	trie, _ = trie.Commit(context.Background(), store)
	root, _ := trie.Root()

	loaded, _ := mpt.LoadRawTrie(root, store, mpt.DefaultLimits())
	value, _ := loaded.Get(context.Background(), []byte("key"))
	fmt.Println(string(value))
	// Output: value
}
