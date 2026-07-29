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
	legacy, err := mpt.LegacyTrieValue(legacyEncoding, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("LegacyTrieValue() error = %v", err)
	}
	typed, err := mpt.TypedTrieValue(2, []byte{0xc1, 0x03}, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("TypedTrieValue() error = %v", err)
	}
	values := []mpt.EncodedTrieValue{legacy, typed}

	transactionRoot, err := mpt.TransactionRoot(
		context.Background(), values, mpt.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("TransactionRoot() error = %v", err)
	}
	receiptRoot, err := mpt.ReceiptRoot(
		context.Background(), values, mpt.DefaultLimits(),
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

func TestEncodedTrieValuesValidateAndOwnBytes(t *testing.T) {
	t.Parallel()

	limits := mpt.DefaultLimits()
	if _, err := mpt.LegacyTrieValue([]byte{0x80}, limits); !errors.Is(err, mpt.ErrInvalidEnvelope) {
		t.Fatalf("LegacyTrieValue(string) error = %v", err)
	}
	if _, err := mpt.LegacyTrieValue([]byte{0xf8, 0x00}, limits); !errors.Is(err, mpt.ErrInvalidEnvelope) {
		t.Fatalf("LegacyTrieValue(non-canonical) error = %v", err)
	}
	if _, err := mpt.TypedTrieValue(0x80, []byte{1}, limits); !errors.Is(err, mpt.ErrInvalidEnvelope) {
		t.Fatalf("TypedTrieValue(type) error = %v", err)
	}
	if _, err := mpt.TypedTrieValue(1, nil, limits); !errors.Is(err, mpt.ErrInvalidEnvelope) {
		t.Fatalf("TypedTrieValue(empty payload) error = %v", err)
	}

	payload := []byte{0xc1, 0x01}
	value, err := mpt.TypedTrieValue(1, payload, limits)
	if err != nil {
		t.Fatalf("TypedTrieValue() error = %v", err)
	}
	payload[0] = 0
	first := value.Bytes()
	if !slices.Equal(first, []byte{1, 0xc1, 0x01}) {
		t.Fatalf("Bytes() = %x", first)
	}
	first[0] = 9
	if slices.Equal(first, value.Bytes()) {
		t.Fatal("EncodedTrieValue.Bytes() returned aliased bytes")
	}
}

func TestIndexedRootHelpersValidateLimitsContextAndValues(t *testing.T) {
	t.Parallel()

	if root, err := mpt.TransactionRoot(
		context.Background(), nil, mpt.DefaultLimits(),
	); err != nil || root != mpt.EmptyRoot() {
		t.Fatalf("TransactionRoot(empty) = (%x, %v)", root, err)
	}
	var zero mpt.EncodedTrieValue
	if _, err := mpt.TransactionRoot(
		context.Background(), []mpt.EncodedTrieValue{zero}, mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrInvalidEnvelope) {
		t.Fatalf("TransactionRoot(zero value) error = %v", err)
	}
	limits := mpt.DefaultLimits()
	limits.MaxBatchOperations = 1
	value, err := mpt.TypedTrieValue(1, []byte{0xc0}, limits)
	if err != nil {
		t.Fatalf("TypedTrieValue() error = %v", err)
	}
	if _, err := mpt.ReceiptRoot(
		context.Background(),
		[]mpt.EncodedTrieValue{value, value},
		limits,
	); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("ReceiptRoot(oversized) error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := mpt.TransactionRoot(
		ctx, []mpt.EncodedTrieValue{value}, limits,
	); !errors.Is(err, mpt.ErrCanceled) {
		t.Fatalf("TransactionRoot(canceled) error = %v", err)
	}
}
