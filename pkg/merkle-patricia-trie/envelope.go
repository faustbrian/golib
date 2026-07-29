package mpt

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/internal/rlp"
)

type trieEnvelopeKind uint8

const (
	legacyTrieEnvelope trieEnvelopeKind = iota + 1
	typedTrieEnvelope
)

// EncodedTrieValue is one immutable structurally validated legacy RLP or
// EIP-2718 typed-envelope value for a transaction or receipt trie. It does not
// validate transaction, receipt, or fork semantics.
type EncodedTrieValue struct {
	kind    trieEnvelopeKind
	encoded []byte
}

// LegacyTrieValue validates and copies one canonical legacy RLP list value.
func LegacyTrieValue(encoded []byte, limits Limits) (EncodedTrieValue, error) {
	if err := validateTrieLimits(limits); err != nil {
		return EncodedTrieValue{}, err
	}
	if len(encoded) > limits.MaxValueBytes {
		return EncodedTrieValue{}, ErrInvalidEnvelope
	}
	value, err := rlp.Decode(
		encoded,
		rlp.Limits{
			MaxEncodedBytes: limits.MaxValueBytes,
			MaxDepth:        limits.MaxTraversalDepth,
			MaxItems:        limits.MaxTraversalNodes,
		},
	)
	if err != nil || value.Kind() != rlp.KindList {
		return EncodedTrieValue{}, fmt.Errorf(
			"%w: legacy value is not a canonical RLP list",
			ErrInvalidEnvelope,
		)
	}
	return EncodedTrieValue{
		kind: legacyTrieEnvelope, encoded: append([]byte(nil), encoded...),
	}, nil
}

// TypedTrieValue validates and copies one structurally framed EIP-2718 value.
// The type byte is preserved exactly and payload semantics remain the caller's
// fork-specific responsibility.
func TypedTrieValue(
	envelopeType byte,
	payload []byte,
	limits Limits,
) (EncodedTrieValue, error) {
	if err := validateTrieLimits(limits); err != nil {
		return EncodedTrieValue{}, err
	}
	if envelopeType >= 0x80 ||
		len(payload) == 0 ||
		len(payload) > limits.MaxValueBytes-1 {
		return EncodedTrieValue{}, ErrInvalidEnvelope
	}
	encoded := make([]byte, len(payload)+1)
	encoded[0] = envelopeType
	copy(encoded[1:], payload)
	return EncodedTrieValue{kind: typedTrieEnvelope, encoded: encoded}, nil
}

// Bytes returns an owned copy of the exact trie value.
func (value EncodedTrieValue) Bytes() []byte {
	return append([]byte(nil), value.encoded...)
}

// TransactionRoot returns the raw-trie root of ordered transaction values
// keyed by their canonical RLP indexes.
func TransactionRoot(
	ctx context.Context,
	values []EncodedTrieValue,
	limits Limits,
) (Root, error) {
	return indexedTrieRoot(ctx, values, limits)
}

// ReceiptRoot returns the raw-trie root of ordered receipt values keyed by
// their canonical RLP indexes.
func ReceiptRoot(
	ctx context.Context,
	values []EncodedTrieValue,
	limits Limits,
) (Root, error) {
	return indexedTrieRoot(ctx, values, limits)
}

func indexedTrieRoot(
	ctx context.Context,
	values []EncodedTrieValue,
	limits Limits,
) (Root, error) {
	if err := validateTrieLimits(limits); err != nil {
		return Root{}, err
	}
	if err := checkContext(ctx); err != nil {
		return Root{}, err
	}
	if len(values) > limits.MaxBatchOperations {
		return Root{}, fmt.Errorf(
			"%w: indexed value bound exceeded",
			ErrResourceLimit,
		)
	}
	trie, _ := NewRawTrie(limits)
	mutations := make([]Mutation, len(values))
	for index, value := range values {
		if value.kind != legacyTrieEnvelope &&
			value.kind != typedTrieEnvelope {
			return Root{}, ErrInvalidEnvelope
		}
		if len(value.encoded) == 0 || len(value.encoded) > limits.MaxValueBytes {
			return Root{}, ErrInvalidEnvelope
		}
		mutations[index] = Put(RLPIndexKey(uint64(index)), value.encoded)
	}
	trie, err := trie.ApplyBatch(ctx, mutations)
	if err != nil {
		return Root{}, err
	}
	return trie.Root()
}
