//go:build interoperability

package mpt_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os/exec"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

type ethereumJSRequest struct {
	Secure     bool                  `json:"secure"`
	Operations []ethereumJSOperation `json:"operations"`
}

type ethereumJSOperation struct {
	Kind  string `json:"kind"`
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

type ethereumJSResult struct {
	Root  string  `json:"root"`
	Value *string `json:"value"`
}

type ethereumJSRangeRequest struct {
	Root        string   `json:"root"`
	FirstKey    string   `json:"firstKey"`
	LastKey     string   `json:"lastKey"`
	Keys        []string `json:"keys"`
	Values      []string `json:"values"`
	Proof       []string `json:"proof"`
	StateKeys   []string `json:"stateKeys"`
	StateValues []string `json:"stateValues"`
}

type ethereumJSRangeResult struct {
	HasMore          bool `json:"hasMore"`
	EdgeNodesMatched bool `json:"edgeNodesMatched"`
}

type ethereumJSProofRequest struct {
	Secure     bool                  `json:"secure"`
	Operations []ethereumJSOperation `json:"operations"`
	Key        string                `json:"key"`
	Proof      []string              `json:"proof"`
}

type ethereumJSProofResult struct {
	Root           string   `json:"root"`
	Proof          []string `json:"proof"`
	GeneratedValue *string  `json:"generatedValue"`
	ProvidedValue  *string  `json:"providedValue"`
}

func TestEthereumJSTransactionAndReceiptRoots(t *testing.T) {
	t.Parallel()

	limits := mpt.DefaultLimits()
	legacyTransaction, err := mpt.LegacyTransactionValue([]byte{0xc1, 0x01}, limits)
	if err != nil {
		t.Fatalf("LegacyTransactionValue() error = %v", err)
	}
	legacyReceipt, err := mpt.LegacyReceiptValue([]byte{0xc1, 0x11}, limits)
	if err != nil {
		t.Fatalf("LegacyReceiptValue() error = %v", err)
	}
	transactions := []mpt.EncodedTransactionValue{legacyTransaction}
	receipts := []mpt.EncodedReceiptValue{legacyReceipt}
	for envelopeType := byte(1); envelopeType <= 4; envelopeType++ {
		transaction, transactionErr := mpt.TypedTransactionValue(
			mpt.OsakaProfile,
			envelopeType,
			[]byte{0xc1, 0x20 + envelopeType},
			limits,
		)
		if transactionErr != nil {
			t.Fatalf("TypedTransactionValue(%d) error = %v", envelopeType, transactionErr)
		}
		receipt, receiptErr := mpt.TypedReceiptValue(
			mpt.OsakaProfile,
			envelopeType,
			[]byte{0xc1, 0x30 + envelopeType},
			limits,
		)
		if receiptErr != nil {
			t.Fatalf("TypedReceiptValue(%d) error = %v", envelopeType, receiptErr)
		}
		transactions = append(transactions, transaction)
		receipts = append(receipts, receipt)
	}

	transactionRoot, err := mpt.TransactionRoot(
		context.Background(), transactions, limits,
	)
	if err != nil {
		t.Fatalf("TransactionRoot() error = %v", err)
	}
	transactionValues := make([][]byte, len(transactions))
	for index, value := range transactions {
		transactionValues[index] = value.Bytes()
	}
	if want := ethereumJSIndexedRoot(t, transactionValues); transactionRoot != want {
		t.Fatalf("transaction root = %x, ethereumjs = %x", transactionRoot, want)
	}

	receiptRoot, err := mpt.ReceiptRoot(
		context.Background(), transactions, receipts, limits,
	)
	if err != nil {
		t.Fatalf("ReceiptRoot() error = %v", err)
	}
	receiptValues := make([][]byte, len(receipts))
	for index, value := range receipts {
		receiptValues[index] = value.Bytes()
	}
	if want := ethereumJSIndexedRoot(t, receiptValues); receiptRoot != want {
		t.Fatalf("receipt root = %x, ethereumjs = %x", receiptRoot, want)
	}
}

func TestEthereumJSStateAndStorageTrieRoots(t *testing.T) {
	t.Parallel()

	limits := mpt.DefaultLimits()
	var address [20]byte
	address[19] = 0xaa
	var balance [32]byte
	balance[31] = 2
	var codeHash [32]byte
	codeHash[0] = 0xcc
	accountValue, err := mpt.NewAccountValue(
		1, balance, mpt.EmptyRoot(), codeHash, limits,
	)
	if err != nil {
		t.Fatalf("NewAccountValue() error = %v", err)
	}
	state, err := mpt.NewStateTrie(limits)
	if err != nil {
		t.Fatalf("NewStateTrie() error = %v", err)
	}
	state, err = state.UpdateAccount(context.Background(), address, accountValue)
	if err != nil {
		t.Fatalf("UpdateAccount() error = %v", err)
	}
	stateRoot, err := state.Root()
	if err != nil {
		t.Fatalf("StateTrie.Root() error = %v", err)
	}
	wantStateRoot := ethereumJSRoot(t, true, []ethereumJSOperation{{
		Kind: "put", Key: hex.EncodeToString(address[:]),
		Value: hex.EncodeToString(accountValue.Bytes()),
	}})
	if stateRoot != wantStateRoot {
		t.Fatalf("state root = %x, ethereumjs = %x", stateRoot, wantStateRoot)
	}

	var slot [32]byte
	slot[31] = 7
	var word [32]byte
	word[30] = 1
	word[31] = 0x80
	storage, err := mpt.NewStorageTrie(limits)
	if err != nil {
		t.Fatalf("NewStorageTrie() error = %v", err)
	}
	storage, err = storage.UpdateSlot(context.Background(), slot, word)
	if err != nil {
		t.Fatalf("UpdateSlot() error = %v", err)
	}
	storageRoot, err := storage.Root()
	if err != nil {
		t.Fatalf("StorageTrie.Root() error = %v", err)
	}
	wantStorageRoot := ethereumJSRoot(t, true, []ethereumJSOperation{{
		Kind: "put", Key: hex.EncodeToString(slot[:]), Value: "820180",
	}})
	if storageRoot != wantStorageRoot {
		t.Fatalf("storage root = %x, ethereumjs = %x", storageRoot, wantStorageRoot)
	}
}

func TestEthereumJSEIP1186AccountAndStorageProofs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	limits := mpt.DefaultLimits()
	var slot [32]byte
	slot[31] = 7
	var storageWord [32]byte
	storageWord[30] = 1
	storageWord[31] = 0x80
	var decoySlot [32]byte
	decoySlot[31] = 9
	var decoyWord [32]byte
	decoyWord[31] = 1

	storage, err := mpt.NewStorageTrie(limits)
	if err != nil {
		t.Fatalf("NewStorageTrie() error = %v", err)
	}
	storage, err = storage.UpdateSlot(ctx, slot, storageWord)
	if err != nil {
		t.Fatalf("UpdateSlot() error = %v", err)
	}
	storage, err = storage.UpdateSlot(ctx, decoySlot, decoyWord)
	if err != nil {
		t.Fatalf("UpdateSlot(decoy) error = %v", err)
	}
	storageRoot, err := storage.Root()
	if err != nil {
		t.Fatalf("StorageTrie.Root() error = %v", err)
	}
	storageOperations := []ethereumJSOperation{
		{
			Kind: "put", Key: hex.EncodeToString(slot[:]),
			Value: hex.EncodeToString(mustRLPString(t, []byte{0x01, 0x80})),
		},
		{
			Kind: "put", Key: hex.EncodeToString(decoySlot[:]),
			Value: hex.EncodeToString(mustRLPString(t, []byte{0x01})),
		},
	}

	var address [20]byte
	address[19] = 0xaa
	var balance [32]byte
	balance[30] = 1
	balance[31] = 0x80
	accountValue, err := mpt.NewAccountValue(
		3,
		balance,
		storageRoot,
		mpt.EmptyCodeHash(),
		limits,
	)
	if err != nil {
		t.Fatalf("NewAccountValue() error = %v", err)
	}
	var decoyAddress [20]byte
	decoyAddress[0] = 0x10
	decoyValue, err := mpt.NewAccountValue(
		0,
		[32]byte{},
		mpt.EmptyRoot(),
		mpt.EmptyCodeHash(),
		limits,
	)
	if err != nil {
		t.Fatalf("NewAccountValue(decoy) error = %v", err)
	}
	state, err := mpt.NewStateTrie(limits)
	if err != nil {
		t.Fatalf("NewStateTrie() error = %v", err)
	}
	state, err = state.UpdateAccount(ctx, address, accountValue)
	if err != nil {
		t.Fatalf("UpdateAccount() error = %v", err)
	}
	state, err = state.UpdateAccount(ctx, decoyAddress, decoyValue)
	if err != nil {
		t.Fatalf("UpdateAccount(decoy) error = %v", err)
	}
	stateRoot, err := state.Root()
	if err != nil {
		t.Fatalf("StateTrie.Root() error = %v", err)
	}
	stateOperations := []ethereumJSOperation{
		{
			Kind: "put", Key: hex.EncodeToString(address[:]),
			Value: hex.EncodeToString(accountValue.Bytes()),
		},
		{
			Kind: "put", Key: hex.EncodeToString(decoyAddress[:]),
			Value: hex.EncodeToString(decoyValue.Bytes()),
		},
	}

	accountProof, err := state.ProveAccount(ctx, address)
	if err != nil {
		t.Fatalf("ProveAccount() error = %v", err)
	}
	accountOracle := ethereumJSProofOracle(
		t,
		stateOperations,
		address[:],
		accountProof,
	)
	assertEthereumJSProofRoot(t, accountOracle, stateRoot)
	assertEthereumJSProofValue(
		t,
		accountOracle.GeneratedValue,
		accountValue.Bytes(),
		"generated account",
	)
	assertEthereumJSProofValue(
		t,
		accountOracle.ProvidedValue,
		accountValue.Bytes(),
		"provided account",
	)
	account, err := mpt.VerifyAccountProof(
		ctx,
		stateRoot,
		address,
		accountValue.Bytes(),
		proofFromEthereumJS(t, accountOracle.Proof),
		limits,
	)
	if err != nil {
		t.Fatalf("VerifyAccountProof(ethereumjs proof) error = %v", err)
	}

	storageProof, err := storage.ProveSlot(ctx, slot)
	if err != nil {
		t.Fatalf("ProveSlot() error = %v", err)
	}
	storageOracle := ethereumJSProofOracle(
		t,
		storageOperations,
		slot[:],
		storageProof,
	)
	assertEthereumJSProofRoot(t, storageOracle, storageRoot)
	assertEthereumJSProofValue(
		t,
		storageOracle.GeneratedValue,
		mustRLPString(t, []byte{0x01, 0x80}),
		"generated storage",
	)
	assertEthereumJSProofValue(
		t,
		storageOracle.ProvidedValue,
		mustRLPString(t, []byte{0x01, 0x80}),
		"provided storage",
	)
	if err := mpt.VerifyStorageProof(
		ctx,
		account,
		slot,
		[]byte{0x01, 0x80},
		proofFromEthereumJS(t, storageOracle.Proof),
		limits,
	); err != nil {
		t.Fatalf("VerifyStorageProof(ethereumjs proof) error = %v", err)
	}

	var absentAddress [20]byte
	absentAddress[0] = 0x99
	absentAccountProof, err := state.ProveAccount(ctx, absentAddress)
	if err != nil {
		t.Fatalf("ProveAccount(absent) error = %v", err)
	}
	absentAccountOracle := ethereumJSProofOracle(
		t,
		stateOperations,
		absentAddress[:],
		absentAccountProof,
	)
	assertEthereumJSProofRoot(t, absentAccountOracle, stateRoot)
	assertEthereumJSProofValue(
		t,
		absentAccountOracle.GeneratedValue,
		nil,
		"generated absent account",
	)
	assertEthereumJSProofValue(
		t,
		absentAccountOracle.ProvidedValue,
		nil,
		"provided absent account",
	)
	if err := mpt.VerifyAccountAbsence(
		ctx,
		stateRoot,
		absentAddress,
		proofFromEthereumJS(t, absentAccountOracle.Proof),
		limits,
	); err != nil {
		t.Fatalf("VerifyAccountAbsence(ethereumjs proof) error = %v", err)
	}

	var absentSlot [32]byte
	absentSlot[31] = 8
	absentStorageProof, err := storage.ProveSlot(ctx, absentSlot)
	if err != nil {
		t.Fatalf("ProveSlot(absent) error = %v", err)
	}
	absentStorageOracle := ethereumJSProofOracle(
		t,
		storageOperations,
		absentSlot[:],
		absentStorageProof,
	)
	assertEthereumJSProofRoot(t, absentStorageOracle, storageRoot)
	assertEthereumJSProofValue(
		t,
		absentStorageOracle.GeneratedValue,
		nil,
		"generated absent storage",
	)
	assertEthereumJSProofValue(
		t,
		absentStorageOracle.ProvidedValue,
		nil,
		"provided absent storage",
	)
	if err := mpt.VerifyStorageProof(
		ctx,
		account,
		absentSlot,
		nil,
		proofFromEthereumJS(t, absentStorageOracle.Proof),
		limits,
	); err != nil {
		t.Fatalf("VerifyStorageProof(ethereumjs absence proof) error = %v", err)
	}
}

func ethereumJSProofOracle(
	t *testing.T,
	operations []ethereumJSOperation,
	key []byte,
	proof mpt.Proof,
) ethereumJSProofResult {
	t.Helper()

	proofNodes := proof.Nodes()
	requestProof := make([]string, len(proofNodes))
	for index, node := range proofNodes {
		requestProof[index] = hex.EncodeToString(node)
	}
	request, err := json.Marshal(ethereumJSProofRequest{
		Secure:     true,
		Operations: operations,
		Key:        hex.EncodeToString(key),
		Proof:      requestProof,
	})
	if err != nil {
		t.Fatalf("json.Marshal(proof request) error = %v", err)
	}
	command := exec.CommandContext(
		context.Background(),
		"node",
		"scripts/ethereumjs-proof-oracle.mjs",
	)
	command.Stdin = bytes.NewReader(request)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("ethereumjs proof oracle error = %v: %s", err, output)
	}
	var result ethereumJSProofResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("json.Unmarshal(proof result) error = %v: %s", err, output)
	}
	return result
}

func assertEthereumJSProofRoot(
	t *testing.T,
	result ethereumJSProofResult,
	want mpt.Root,
) {
	t.Helper()

	if got := hex.EncodeToString(want[:]); result.Root != got {
		t.Fatalf("ethereumjs proof root = %s, want %s", result.Root, got)
	}
}

func assertEthereumJSProofValue(
	t *testing.T,
	got *string,
	want []byte,
	label string,
) {
	t.Helper()

	if want == nil {
		if got != nil {
			t.Fatalf("%s value = %s, want absence", label, *got)
		}
		return
	}
	if got == nil || *got != hex.EncodeToString(want) {
		t.Fatalf("%s value = %v, want %x", label, got, want)
	}
}

func proofFromEthereumJS(t *testing.T, encoded []string) mpt.Proof {
	t.Helper()

	nodes := make([][]byte, len(encoded))
	for index, node := range encoded {
		decoded, err := hex.DecodeString(node)
		if err != nil {
			t.Fatalf("decode ethereumjs proof node %d: %v", index, err)
		}
		nodes[index] = decoded
	}
	proof, err := mpt.ProofFromNodes(nodes, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("ProofFromNodes(ethereumjs proof) error = %v", err)
	}
	return proof
}

func ethereumJSRoot(
	t *testing.T,
	secure bool,
	operations []ethereumJSOperation,
) mpt.Root {
	t.Helper()
	request, err := json.Marshal(ethereumJSRequest{
		Secure: secure, Operations: operations,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	command := exec.CommandContext(
		context.Background(), "node", "scripts/ethereumjs-oracle.mjs",
	)
	command.Stdin = bytes.NewReader(request)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("ethereumjs oracle error = %v: %s", err, output)
	}
	var results []ethereumJSResult
	if err := json.Unmarshal(output, &results); err != nil {
		t.Fatalf("json.Unmarshal() error = %v: %s", err, output)
	}
	if len(results) != len(operations) || len(results) == 0 {
		t.Fatalf("ethereumjs result count = %d, want %d", len(results), len(operations))
	}
	encoded, err := hex.DecodeString(results[len(results)-1].Root)
	if err != nil || len(encoded) != mpt.RootBytes {
		t.Fatalf("ethereumjs root = %q, decode error = %v", results[len(results)-1].Root, err)
	}
	var root mpt.Root
	copy(root[:], encoded)
	return root
}

func ethereumJSIndexedRoot(t *testing.T, values [][]byte) mpt.Root {
	t.Helper()

	operations := make([]ethereumJSOperation, len(values))
	for index, value := range values {
		operations[index] = ethereumJSOperation{
			Kind:  "put",
			Key:   hex.EncodeToString(mpt.RLPIndexKey(uint64(index))),
			Value: hex.EncodeToString(value),
		}
	}
	request, err := json.Marshal(ethereumJSRequest{Operations: operations})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	command := exec.CommandContext(
		context.Background(), "node", "scripts/ethereumjs-oracle.mjs",
	)
	command.Stdin = bytes.NewReader(request)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("ethereumjs oracle error = %v: %s", err, output)
	}
	var results []ethereumJSResult
	if err := json.Unmarshal(output, &results); err != nil {
		t.Fatalf("json.Unmarshal() error = %v: %s", err, output)
	}
	if len(results) != len(values) {
		t.Fatalf("ethereumjs result count = %d, want %d", len(results), len(values))
	}
	encoded, err := hex.DecodeString(results[len(results)-1].Root)
	if err != nil || len(encoded) != mpt.RootBytes {
		t.Fatalf("ethereumjs root = %q, decode error = %v", results[len(results)-1].Root, err)
	}
	var root mpt.Root
	copy(root[:], encoded)
	return root
}

func TestEthereumJSDifferentialMutationTrace(t *testing.T) {
	t.Parallel()

	for _, secure := range []bool{false, true} {
		secure := secure
		t.Run(fmt.Sprintf("secure=%t", secure), func(t *testing.T) {
			t.Parallel()
			runEthereumJSDifferentialTrace(t, secure)
		})
	}
}

func TestEthereumJSAcceptsGeneratedRawRangeProof(t *testing.T) {
	t.Parallel()

	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	keys := make([][]byte, 5)
	values := make([][]byte, len(keys))
	for index, suffix := range []byte{0x10, 0x20, 0x30, 0x40, 0x50} {
		keys[index] = make([]byte, mpt.RootBytes)
		keys[index][mpt.RootBytes-1] = suffix
		values[index] = []byte(fmt.Sprintf(
			"a distinct long persisted range value %02x", suffix,
		))
		trie, err = trie.Update(
			context.Background(),
			keys[index],
			values[index],
		)
		if err != nil {
			t.Fatalf("Update(%x) error = %v", keys[index], err)
		}
	}
	proof, items, err := trie.ProveRange(
		context.Background(), keys[1], keys[4],
	)
	if err != nil {
		t.Fatalf("ProveRange() error = %v", err)
	}
	root, err := trie.Root()
	if err != nil {
		t.Fatalf("Root() error = %v", err)
	}
	request := ethereumJSRangeRequest{
		Root:        hex.EncodeToString(root[:]),
		FirstKey:    hex.EncodeToString(items[0].Key()),
		LastKey:     hex.EncodeToString(items[len(items)-1].Key()),
		Keys:        make([]string, len(items)),
		Values:      make([]string, len(items)),
		Proof:       make([]string, len(proof.Nodes())),
		StateKeys:   make([]string, len(keys)),
		StateValues: make([]string, len(keys)),
	}
	for index, item := range items {
		request.Keys[index] = hex.EncodeToString(item.Key())
		request.Values[index] = hex.EncodeToString(item.Value())
	}
	for index, encoded := range proof.Nodes() {
		request.Proof[index] = hex.EncodeToString(encoded)
	}
	for index, key := range keys {
		request.StateKeys[index] = hex.EncodeToString(key)
		request.StateValues[index] = hex.EncodeToString(values[index])
	}
	input, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	command := exec.CommandContext(
		context.Background(),
		"node",
		"scripts/ethereumjs-range-oracle.mjs",
	)
	command.Stdin = bytes.NewReader(input)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("ethereumjs range oracle error = %v: %s", err, output)
	}
	var result ethereumJSRangeResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("json.Unmarshal() error = %v: %s", err, output)
	}
	if !result.HasMore {
		t.Fatal("ethereumjs range proof did not find the leaf after the range")
	}
	if !result.EdgeNodesMatched {
		t.Fatal("generated witness did not contain ethereumjs edge proof nodes")
	}
}

func runEthereumJSDifferentialTrace(t *testing.T, secure bool) {
	t.Helper()
	operations := ethereumJSOperations()
	request, err := json.Marshal(ethereumJSRequest{
		Secure:     secure,
		Operations: operations,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	command := exec.CommandContext(
		context.Background(),
		"node",
		"scripts/ethereumjs-oracle.mjs",
	)
	command.Stdin = bytes.NewReader(request)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("ethereumjs oracle error = %v: %s", err, output)
	}
	var oracleResults []ethereumJSResult
	if err := json.Unmarshal(output, &oracleResults); err != nil {
		t.Fatalf("json.Unmarshal() error = %v: %s", err, output)
	}
	if len(oracleResults) != len(operations) {
		t.Fatalf(
			"ethereumjs result count = %d, want %d",
			len(oracleResults),
			len(operations),
		)
	}

	var (
		update func(context.Context, []byte, []byte) error
		remove func(context.Context, []byte) error
		get    func(context.Context, []byte) ([]byte, error)
		root   func() (mpt.Root, error)
	)
	if secure {
		trie, newErr := mpt.NewSecureTrie(mpt.DefaultLimits())
		err = newErr
		update = func(ctx context.Context, key, value []byte) error {
			var updateErr error
			trie, updateErr = trie.Update(ctx, key, value)
			return updateErr
		}
		remove = func(ctx context.Context, key []byte) error {
			var deleteErr error
			trie, deleteErr = trie.Delete(ctx, key)
			return deleteErr
		}
		get = func(ctx context.Context, key []byte) ([]byte, error) {
			return trie.Get(ctx, key)
		}
		root = func() (mpt.Root, error) {
			return trie.Root()
		}
	} else {
		trie, newErr := mpt.NewRawTrie(mpt.DefaultLimits())
		err = newErr
		update = func(ctx context.Context, key, value []byte) error {
			var updateErr error
			trie, updateErr = trie.Update(ctx, key, value)
			return updateErr
		}
		remove = func(ctx context.Context, key []byte) error {
			var deleteErr error
			trie, deleteErr = trie.Delete(ctx, key)
			return deleteErr
		}
		get = func(ctx context.Context, key []byte) ([]byte, error) {
			return trie.Get(ctx, key)
		}
		root = func() (mpt.Root, error) {
			return trie.Root()
		}
	}
	if err != nil {
		t.Fatalf("NewTrie() error = %v", err)
	}
	for index, operation := range operations {
		key, decodeErr := hex.DecodeString(operation.Key)
		if decodeErr != nil {
			t.Fatalf("step %d key decode error = %v", index, decodeErr)
		}
		switch operation.Kind {
		case "put":
			value, valueErr := hex.DecodeString(operation.Value)
			if valueErr != nil {
				t.Fatalf("step %d value decode error = %v", index, valueErr)
			}
			err = update(context.Background(), key, value)
		case "delete":
			err = remove(context.Background(), key)
		default:
			t.Fatalf("step %d unsupported operation %q", index, operation.Kind)
		}
		if err != nil {
			t.Fatalf("step %d %s error = %v", index, operation.Kind, err)
		}

		rootValue, rootErr := root()
		if rootErr != nil {
			t.Fatalf("step %d Root() error = %v", index, rootErr)
		}
		if got := hex.EncodeToString(rootValue[:]); got != oracleResults[index].Root {
			t.Fatalf(
				"step %d operation = %#v, root = %s, ethereumjs = %s",
				index,
				operation,
				got,
				oracleResults[index].Root,
			)
		}
		got, getErr := get(context.Background(), key)
		if oracleResults[index].Value == nil {
			if getErr == nil {
				t.Fatalf("step %d Get() = %x, want absent", index, got)
			}
		} else {
			if getErr != nil {
				t.Fatalf("step %d Get() error = %v", index, getErr)
			}
			if want := *oracleResults[index].Value; hex.EncodeToString(got) != want {
				t.Fatalf("step %d value = %x, ethereumjs = %s", index, got, want)
			}
		}
	}
}

func ethereumJSOperations() []ethereumJSOperation {
	generator := rand.New(rand.NewPCG(0x6d7074, 0x6574686a73))
	keys := make([][]byte, 24)
	for index := range keys {
		key := make([]byte, generator.IntN(8)+1)
		for offset := range key {
			key[offset] = byte(generator.Uint32())
		}
		keys[index] = key
	}

	state := make(map[string]struct{})
	operations := make([]ethereumJSOperation, 0, 256)
	for range 256 {
		key := keys[generator.IntN(len(keys))]
		keyString := string(key)
		if _, present := state[keyString]; present && generator.IntN(4) == 0 {
			operations = append(operations, ethereumJSOperation{
				Kind: "delete",
				Key:  hex.EncodeToString(key),
			})
			delete(state, keyString)
			continue
		}
		value := make([]byte, generator.IntN(64)+1)
		for index := range value {
			value[index] = byte(generator.Uint32())
		}
		operations = append(operations, ethereumJSOperation{
			Kind:  "put",
			Key:   hex.EncodeToString(key),
			Value: hex.EncodeToString(value),
		})
		state[keyString] = struct{}{}
	}
	return operations
}
