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

func ExampleStateTrie() {
	limits := mpt.DefaultLimits()
	var address [20]byte
	address[19] = 0xaa
	var balance [32]byte
	balance[31] = 42
	value, _ := mpt.NewAccountValue(
		1, balance, mpt.EmptyRoot(), mpt.EmptyCodeHash(), limits,
	)
	trie, _ := mpt.NewStateTrie(limits)
	trie, _ = trie.UpdateAccount(context.Background(), address, value)
	account, _ := trie.GetAccount(context.Background(), address)
	fmt.Println(account.Nonce(), account.Balance()[31])
	// Output: 1 42
}

func ExampleVerifyAccountProof() {
	ctx := context.Background()
	limits := mpt.DefaultLimits()
	var address [20]byte
	address[19] = 0xaa
	var balance [32]byte
	balance[31] = 42
	value, _ := mpt.NewAccountValue(
		1, balance, mpt.EmptyRoot(), mpt.EmptyCodeHash(), limits,
	)
	trie, _ := mpt.NewStateTrie(limits)
	trie, _ = trie.UpdateAccount(ctx, address, value)
	root, _ := trie.Root()
	proof, _ := trie.ProveAccount(ctx, address)

	account, err := mpt.VerifyAccountProof(
		ctx,
		root,
		address,
		value.Bytes(),
		proof,
		limits,
	)
	fmt.Println(err == nil, account.Nonce(), account.Balance()[31])
	// Output: true 1 42
}

func ExampleVerifyStorageProofs() {
	ctx := context.Background()
	limits := mpt.DefaultLimits()
	storage, _ := mpt.NewStorageTrie(limits)
	storageRoot, _ := storage.Root()

	var address [20]byte
	value, _ := mpt.NewAccountValue(
		0, [32]byte{}, storageRoot, mpt.EmptyCodeHash(), limits,
	)
	state, _ := mpt.NewStateTrie(limits)
	state, _ = state.UpdateAccount(ctx, address, value)
	stateRoot, _ := state.Root()
	accountProof, _ := state.ProveAccount(ctx, address)
	account, _ := mpt.VerifyAccountProof(
		ctx, stateRoot, address, value.Bytes(), accountProof, limits,
	)

	var firstSlot [32]byte
	var secondSlot [32]byte
	secondSlot[31] = 1
	firstProof, _ := storage.ProveSlot(ctx, firstSlot)
	secondProof, _ := storage.ProveSlot(ctx, secondSlot)
	err := mpt.VerifyStorageProofs(
		ctx,
		account,
		[]mpt.StorageProofClaim{
			mpt.StorageAbsenceClaim(firstSlot, firstProof),
			mpt.StorageAbsenceClaim(secondSlot, secondProof),
		},
		limits,
	)
	fmt.Println(err == nil)
	// Output: true
}

func ExampleStorageTrie() {
	var slot [32]byte
	slot[31] = 7
	var word [32]byte
	word[31] = 42
	trie, _ := mpt.NewStorageTrie(mpt.DefaultLimits())
	trie, _ = trie.UpdateSlot(context.Background(), slot, word)
	stored, _ := trie.GetSlot(context.Background(), slot)
	fmt.Println(stored[31])
	// Output: 42
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
