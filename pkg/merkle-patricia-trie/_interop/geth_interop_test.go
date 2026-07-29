//go:build interoperability

package interop_test

import (
	"context"
	"math/big"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	gethtrie "github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

func TestGethTransactionAndReceiptRoots(t *testing.T) {
	t.Parallel()

	address := common.HexToAddress("0x1234")
	transactions := types.Transactions{
		types.NewTx(&types.LegacyTx{
			Nonce: 1, GasPrice: big.NewInt(2), Gas: 21_000, To: &address,
			Value: big.NewInt(3), V: big.NewInt(27), R: big.NewInt(1),
			S: big.NewInt(2),
		}),
		types.NewTx(&types.AccessListTx{
			ChainID: big.NewInt(1), Nonce: 2, GasPrice: big.NewInt(3),
			Gas: 22_000, To: &address, Value: big.NewInt(4),
			AccessList: types.AccessList{}, V: big.NewInt(0), R: big.NewInt(2),
			S: big.NewInt(3),
		}),
		types.NewTx(&types.DynamicFeeTx{
			ChainID: big.NewInt(1), Nonce: 3, GasTipCap: big.NewInt(2),
			GasFeeCap: big.NewInt(5), Gas: 23_000, To: &address,
			Value: big.NewInt(5), AccessList: types.AccessList{},
			V: big.NewInt(1), R: big.NewInt(3), S: big.NewInt(4),
		}),
		types.NewTx(&types.BlobTx{
			ChainID: uint256.NewInt(1), Nonce: 4,
			GasTipCap: uint256.NewInt(2), GasFeeCap: uint256.NewInt(6),
			Gas: 24_000, To: address, Value: uint256.NewInt(6),
			AccessList: types.AccessList{}, BlobFeeCap: uint256.NewInt(7),
			BlobHashes: []common.Hash{{1}}, V: uint256.NewInt(1),
			R: uint256.NewInt(4), S: uint256.NewInt(5),
		}),
		types.NewTx(&types.SetCodeTx{
			ChainID: uint256.NewInt(1), Nonce: 5,
			GasTipCap: uint256.NewInt(2), GasFeeCap: uint256.NewInt(7),
			Gas: 25_000, To: address, Value: uint256.NewInt(7),
			AccessList: types.AccessList{}, AuthList: []types.SetCodeAuthorization{},
			V: uint256.NewInt(0), R: uint256.NewInt(5), S: uint256.NewInt(6),
		}),
	}
	receipts := make(types.Receipts, len(transactions))
	localTransactions := make([]mpt.EncodedTransactionValue, len(transactions))
	localReceipts := make([]mpt.EncodedReceiptValue, len(transactions))
	for index, transaction := range transactions {
		encoded, err := transaction.MarshalBinary()
		if err != nil {
			t.Fatalf("transaction %d MarshalBinary() error = %v", index, err)
		}
		if transaction.Type() == types.LegacyTxType {
			localTransactions[index], err = mpt.LegacyTransactionValue(
				encoded, mpt.DefaultLimits(),
			)
		} else {
			localTransactions[index], err = mpt.TypedTransactionValue(
				mpt.OsakaProfile, transaction.Type(), encoded[1:], mpt.DefaultLimits(),
			)
		}
		if err != nil {
			t.Fatalf("transaction %d local encoding error = %v", index, err)
		}
		if !slices.Equal(localTransactions[index].Bytes(), encoded) {
			t.Fatalf("transaction %d bytes differ from geth", index)
		}

		receipt := &types.Receipt{
			Type: transaction.Type(), Status: types.ReceiptStatusSuccessful,
			CumulativeGasUsed: uint64(index+1) * 21_000, Logs: []*types.Log{},
		}
		receipts[index] = receipt
		encoded, err = receipt.MarshalBinary()
		if err != nil {
			t.Fatalf("receipt %d MarshalBinary() error = %v", index, err)
		}
		if receipt.Type == types.LegacyTxType {
			localReceipts[index], err = mpt.LegacyReceiptValue(
				encoded, mpt.DefaultLimits(),
			)
		} else {
			localReceipts[index], err = mpt.TypedReceiptValue(
				mpt.OsakaProfile, receipt.Type, encoded[1:], mpt.DefaultLimits(),
			)
		}
		if err != nil {
			t.Fatalf("receipt %d local encoding error = %v", index, err)
		}
		if !slices.Equal(localReceipts[index].Bytes(), encoded) {
			t.Fatalf("receipt %d bytes differ from geth", index)
		}
	}

	transactionRoot, err := mpt.TransactionRoot(
		context.Background(), localTransactions, mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("TransactionRoot() error = %v", err)
	}
	if want := types.DeriveSha(
		transactions, gethtrie.NewStackTrie(nil),
	); common.Hash(transactionRoot) != want {
		t.Fatalf("transaction root = %x, geth = %x", transactionRoot, want)
	}
	receiptRoot, err := mpt.ReceiptRoot(
		context.Background(), localTransactions, localReceipts, mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("ReceiptRoot() error = %v", err)
	}
	if want := types.DeriveSha(
		receipts, gethtrie.NewStackTrie(nil),
	); common.Hash(receiptRoot) != want {
		t.Fatalf("receipt root = %x, geth = %x", receiptRoot, want)
	}
}

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

func TestGethAcceptsGeneratedRawRangeProof(t *testing.T) {
	t.Parallel()

	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	for _, key := range []byte{0x10, 0x20, 0x30, 0x40, 0x50} {
		trie, err = trie.Update(
			context.Background(),
			[]byte{key},
			[]byte("a value long enough to persist range proof children"),
		)
		if err != nil {
			t.Fatalf("Update(%x) error = %v", key, err)
		}
	}
	proof, items, err := trie.ProveRange(
		context.Background(), []byte{0x20}, []byte{0x50},
	)
	if err != nil {
		t.Fatalf("ProveRange() error = %v", err)
	}
	keys := make([][]byte, len(items))
	values := make([][]byte, len(items))
	for index, item := range items {
		keys[index] = item.Key()
		values[index] = item.Value()
	}
	proofDatabase := memorydb.New()
	for _, encoded := range proof.Nodes() {
		if err := proofDatabase.Put(crypto.Keccak256(encoded), encoded); err != nil {
			t.Fatalf("proof Put() error = %v", err)
		}
	}
	root, err := trie.Root()
	if err != nil {
		t.Fatalf("Root() error = %v", err)
	}
	if err := mpt.VerifyRawRange(
		context.Background(),
		root,
		[]byte{0x20},
		[]byte{0x50},
		items,
		proof,
		mpt.DefaultLimits(),
	); err != nil {
		t.Fatalf("VerifyRawRange(shared hashed leaves) error = %v", err)
	}
	hasMore, err := gethtrie.VerifyRangeProof(
		common.Hash(root), []byte{0x20}, keys, values, proofDatabase,
	)
	if err != nil {
		t.Fatalf("geth VerifyRangeProof() error = %v", err)
	}
	if !hasMore {
		t.Fatal("geth VerifyRangeProof() did not find the leaf after the range")
	}
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
