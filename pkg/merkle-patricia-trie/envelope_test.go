package mpt_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/internal/rlp"
)

func TestTransactionAndReceiptRootsUseRLPIndexesAndExactEnvelopes(t *testing.T) {
	t.Parallel()

	legacyEncoding, err := rlp.Encode(
		rlp.List(rlp.String([]byte{1}), rlp.String([]byte{2})),
		rlp.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("encode legacy value: %v", err)
	}
	legacyTransaction, err := mpt.LegacyTransactionValue(
		legacyEncoding, mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("LegacyTransactionValue() error = %v", err)
	}
	typedTransaction, err := mpt.TypedTransactionValue(
		mpt.LondonProfile, 2, []byte{0xc1, 0x03}, mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("TypedTransactionValue() error = %v", err)
	}
	legacyReceipt, err := mpt.LegacyReceiptValue(
		legacyEncoding, mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("LegacyReceiptValue() error = %v", err)
	}
	typedReceipt, err := mpt.TypedReceiptValue(
		mpt.LondonProfile, 2, []byte{0xc1, 0x03}, mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("TypedReceiptValue() error = %v", err)
	}
	transactions := []mpt.EncodedTransactionValue{
		legacyTransaction, typedTransaction,
	}
	receipts := []mpt.EncodedReceiptValue{legacyReceipt, typedReceipt}

	transactionRoot, err := mpt.TransactionRoot(
		context.Background(), transactions, mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("TransactionRoot() error = %v", err)
	}
	receiptRoot, err := mpt.ReceiptRoot(
		context.Background(), transactions, receipts, mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("ReceiptRoot() error = %v", err)
	}
	if transactionRoot != receiptRoot {
		t.Fatalf("transaction root = %x, receipt root = %x", transactionRoot, receiptRoot)
	}

	raw, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	raw, err = raw.Update(context.Background(), mpt.RLPIndexKey(0), legacyEncoding)
	if err != nil {
		t.Fatalf("Update(legacy) error = %v", err)
	}
	raw, err = raw.Update(
		context.Background(),
		mpt.RLPIndexKey(1),
		[]byte{2, 0xc1, 0x03},
	)
	if err != nil {
		t.Fatalf("Update(typed) error = %v", err)
	}
	if want := mustTrieRoot(t, raw); transactionRoot != want {
		t.Fatalf("TransactionRoot() = %x, want %x", transactionRoot, want)
	}
}

func TestEncodedEnvelopeValuesValidateAndOwnBytes(t *testing.T) {
	t.Parallel()

	limits := mpt.DefaultLimits()
	if _, err := mpt.LegacyTransactionValue([]byte{0x80}, limits); !errors.Is(err, mpt.ErrInvalidEnvelope) {
		t.Fatalf("LegacyTransactionValue(string) error = %v", err)
	}
	if _, err := mpt.LegacyReceiptValue([]byte{0xf8, 0x00}, limits); !errors.Is(err, mpt.ErrInvalidEnvelope) {
		t.Fatalf("LegacyReceiptValue(non-canonical) error = %v", err)
	}
	if _, err := mpt.TypedTransactionValue(
		mpt.OsakaProfile, 5, []byte{0xc0}, limits,
	); !errors.Is(err, mpt.ErrInvalidEnvelope) {
		t.Fatalf("TypedTransactionValue(type) error = %v", err)
	}
	if _, err := mpt.TypedReceiptValue(
		mpt.BerlinProfile, 1, nil, limits,
	); !errors.Is(err, mpt.ErrInvalidEnvelope) {
		t.Fatalf("TypedReceiptValue(empty payload) error = %v", err)
	}

	payload := []byte{0xc1, 0x01}
	value, err := mpt.TypedTransactionValue(
		mpt.BerlinProfile, 1, payload, limits,
	)
	if err != nil {
		t.Fatalf("TypedTransactionValue() error = %v", err)
	}
	payload[0] = 0
	first := value.Bytes()
	if !slices.Equal(first, []byte{1, 0xc1, 0x01}) {
		t.Fatalf("Bytes() = %x", first)
	}
	first[0] = 9
	if slices.Equal(first, value.Bytes()) {
		t.Fatal("EncodedTransactionValue.Bytes() returned aliased bytes")
	}
	receipt, err := mpt.TypedReceiptValue(
		mpt.BerlinProfile, 1, []byte{0xc0}, limits,
	)
	if err != nil {
		t.Fatalf("TypedReceiptValue() error = %v", err)
	}
	receiptBytes := receipt.Bytes()
	receiptBytes[0] = 9
	if slices.Equal(receiptBytes, receipt.Bytes()) {
		t.Fatal("EncodedReceiptValue.Bytes() returned aliased bytes")
	}
}

func TestIndexedRootHelpersValidateLimitsContextAndValues(t *testing.T) {
	t.Parallel()

	if root, err := mpt.TransactionRoot(
		context.Background(), nil, mpt.DefaultLimits(),
	); err != nil || root != mpt.EmptyRoot() {
		t.Fatalf("TransactionRoot(empty) = (%x, %v)", root, err)
	}
	if root, err := mpt.ReceiptRoot(
		context.Background(), nil, nil, mpt.DefaultLimits(),
	); err != nil || root != mpt.EmptyRoot() {
		t.Fatalf("ReceiptRoot(empty) = (%x, %v)", root, err)
	}
	var zero mpt.EncodedTransactionValue
	if _, err := mpt.TransactionRoot(
		context.Background(), []mpt.EncodedTransactionValue{zero}, mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrInvalidEnvelope) {
		t.Fatalf("TransactionRoot(zero value) error = %v", err)
	}
	limits := mpt.DefaultLimits()
	limits.MaxBatchOperations = 1
	transaction, err := mpt.TypedTransactionValue(
		mpt.BerlinProfile, 1, []byte{0xc0}, limits,
	)
	if err != nil {
		t.Fatalf("TypedTransactionValue() error = %v", err)
	}
	receipt, err := mpt.TypedReceiptValue(
		mpt.BerlinProfile, 1, []byte{0xc0}, limits,
	)
	if err != nil {
		t.Fatalf("TypedReceiptValue() error = %v", err)
	}
	if _, err := mpt.ReceiptRoot(
		context.Background(),
		[]mpt.EncodedTransactionValue{transaction, transaction},
		[]mpt.EncodedReceiptValue{receipt, receipt},
		limits,
	); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("ReceiptRoot(oversized) error = %v", err)
	}
	zeroTransactions := make([]mpt.EncodedTransactionValue, 2)
	if _, err := mpt.TransactionRoot(
		context.Background(), zeroTransactions, limits,
	); !errors.Is(err, mpt.ErrResourceLimit) ||
		errors.Is(err, mpt.ErrInvalidEnvelope) {
		t.Fatalf("TransactionRoot(bound precedence) error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := mpt.TransactionRoot(
		ctx, []mpt.EncodedTransactionValue{transaction}, limits,
	); !errors.Is(err, mpt.ErrCanceled) {
		t.Fatalf("TransactionRoot(canceled) error = %v", err)
	}
}
