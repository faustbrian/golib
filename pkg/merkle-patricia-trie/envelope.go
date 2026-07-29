package mpt

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/internal/rlp"
)

// ForkProfile identifies the execution-specification fork whose typed
// transaction and receipt envelope activations are being validated.
type ForkProfile uint8

const (
	// BerlinProfile supports EIP-2930 type-1 envelopes.
	BerlinProfile ForkProfile = iota + 1
	// LondonProfile supports type-1 and EIP-1559 type-2 envelopes.
	LondonProfile
	// ParisProfile retains London's type-1 and type-2 envelope set.
	ParisProfile
	// ShanghaiProfile retains London's type-1 and type-2 envelope set.
	ShanghaiProfile
	// CancunProfile adds EIP-4844 type-3 envelopes.
	CancunProfile
	// PragueProfile adds EIP-7702 type-4 envelopes.
	PragueProfile
	// OsakaProfile retains Prague's type-1 through type-4 envelope set.
	OsakaProfile
)

type trieEnvelopeKind uint8

const (
	legacyTrieEnvelope trieEnvelopeKind = iota + 1
	typedTrieEnvelope
)

type encodedTrieValue struct {
	kind         trieEnvelopeKind
	profile      ForkProfile
	envelopeType byte
	encoded      []byte
}

// EncodedTransactionValue is one immutable structurally validated legacy or
// fork-bound typed transaction value. It validates trie framing, not complete
// transaction fields, signatures, or state-transition semantics.
type EncodedTransactionValue struct {
	value encodedTrieValue
}

// EncodedReceiptValue is one immutable structurally validated legacy or
// fork-bound typed receipt value. It validates trie framing, not receipt field
// semantics or execution results.
type EncodedReceiptValue struct {
	value encodedTrieValue
}

// LegacyTransactionValue validates and copies one canonical legacy transaction
// RLP list.
func LegacyTransactionValue(
	encoded []byte,
	limits Limits,
) (EncodedTransactionValue, error) {
	value, err := legacyEnvelopeValue(encoded, limits)
	if err != nil {
		return EncodedTransactionValue{}, err
	}
	return EncodedTransactionValue{value: value}, nil
}

// TypedTransactionValue validates and copies one typed transaction envelope
// activated by profile. The supported execution profiles define type-1 through
// type-4 payloads as canonical RLP lists; transaction fields remain opaque.
func TypedTransactionValue(
	profile ForkProfile,
	envelopeType byte,
	payload []byte,
	limits Limits,
) (EncodedTransactionValue, error) {
	value, err := typedEnvelopeValue(profile, envelopeType, payload, limits)
	if err != nil {
		return EncodedTransactionValue{}, err
	}
	return EncodedTransactionValue{value: value}, nil
}

// Bytes returns an owned copy of the exact transaction trie value.
func (value EncodedTransactionValue) Bytes() []byte {
	return append([]byte(nil), value.value.encoded...)
}

// LegacyReceiptValue validates and copies one canonical legacy receipt RLP
// list. Fork-sensitive receipt fields remain the caller's responsibility.
func LegacyReceiptValue(
	encoded []byte,
	limits Limits,
) (EncodedReceiptValue, error) {
	value, err := legacyEnvelopeValue(encoded, limits)
	if err != nil {
		return EncodedReceiptValue{}, err
	}
	return EncodedReceiptValue{value: value}, nil
}

// TypedReceiptValue validates and copies one typed receipt envelope activated
// by profile. The supported execution profiles define type-1 through type-4
// payloads as canonical RLP lists; receipt fields remain opaque.
func TypedReceiptValue(
	profile ForkProfile,
	envelopeType byte,
	payload []byte,
	limits Limits,
) (EncodedReceiptValue, error) {
	value, err := typedEnvelopeValue(profile, envelopeType, payload, limits)
	if err != nil {
		return EncodedReceiptValue{}, err
	}
	return EncodedReceiptValue{value: value}, nil
}

// Bytes returns an owned copy of the exact receipt trie value.
func (value EncodedReceiptValue) Bytes() []byte {
	return append([]byte(nil), value.value.encoded...)
}

// TransactionRoot returns the raw-trie root of ordered transaction values
// keyed by their canonical RLP indexes. Typed values in one root must use one
// fork profile.
func TransactionRoot(
	ctx context.Context,
	values []EncodedTransactionValue,
	limits Limits,
) (Root, error) {
	if err := validateIndexedRootRequest(ctx, len(values), limits); err != nil {
		return Root{}, err
	}
	encoded := make([]encodedTrieValue, len(values))
	for index, value := range values {
		encoded[index] = value.value
	}
	if err := validateEnvelopeSequence(encoded); err != nil {
		return Root{}, err
	}
	return indexedTrieRoot(ctx, encoded, limits)
}

// ReceiptRoot returns the raw-trie root of ordered receipt values keyed by
// their canonical RLP indexes. Each receipt must have the same legacy or typed
// envelope kind, type, and fork profile as its transaction at that index.
func ReceiptRoot(
	ctx context.Context,
	transactions []EncodedTransactionValue,
	receipts []EncodedReceiptValue,
	limits Limits,
) (Root, error) {
	if err := validateIndexedRootRequest(ctx, len(transactions), limits); err != nil {
		return Root{}, err
	}
	if len(transactions) != len(receipts) {
		return Root{}, fmt.Errorf(
			"%w: transaction and receipt counts differ", ErrInvalidEnvelope,
		)
	}
	encodedTransactions := make([]encodedTrieValue, len(transactions))
	encodedReceipts := make([]encodedTrieValue, len(receipts))
	for index := range transactions {
		transaction := transactions[index].value
		receipt := receipts[index].value
		if transaction.kind != receipt.kind {
			return Root{}, fmt.Errorf(
				"%w: receipt envelope does not match transaction", ErrInvalidEnvelope,
			)
		}
		if transaction.profile != receipt.profile {
			return Root{}, fmt.Errorf(
				"%w: receipt envelope does not match transaction", ErrInvalidEnvelope,
			)
		}
		if transaction.envelopeType != receipt.envelopeType {
			return Root{}, fmt.Errorf(
				"%w: receipt envelope does not match transaction", ErrInvalidEnvelope,
			)
		}
		encodedTransactions[index] = transaction
		encodedReceipts[index] = receipt
	}
	if err := validateEnvelopeSequence(encodedTransactions); err != nil {
		return Root{}, err
	}
	return indexedTrieRoot(ctx, encodedReceipts, limits)
}

func legacyEnvelopeValue(encoded []byte, limits Limits) (encodedTrieValue, error) {
	if err := validateTrieLimits(limits); err != nil {
		return encodedTrieValue{}, err
	}
	if err := validateEnvelopeList(encoded, limits); err != nil {
		return encodedTrieValue{}, err
	}
	return encodedTrieValue{
		kind: legacyTrieEnvelope, encoded: append([]byte(nil), encoded...),
	}, nil
}

func typedEnvelopeValue(
	profile ForkProfile,
	envelopeType byte,
	payload []byte,
	limits Limits,
) (encodedTrieValue, error) {
	if err := validateTrieLimits(limits); err != nil {
		return encodedTrieValue{}, err
	}
	maximumType, err := profile.maximumEnvelopeType()
	if err != nil {
		return encodedTrieValue{}, err
	}
	if envelopeType == 0 || envelopeType > maximumType ||
		len(payload) == 0 || len(payload) > limits.MaxValueBytes-1 {
		return encodedTrieValue{}, ErrInvalidEnvelope
	}
	if err := validateEnvelopeList(payload, limits); err != nil {
		return encodedTrieValue{}, err
	}
	encoded := make([]byte, len(payload)+1)
	encoded[0] = envelopeType
	copy(encoded[1:], payload)
	return encodedTrieValue{
		kind:         typedTrieEnvelope,
		profile:      profile,
		envelopeType: envelopeType,
		encoded:      encoded,
	}, nil
}

func validateEnvelopeList(encoded []byte, limits Limits) error {
	if len(encoded) == 0 {
		return ErrInvalidEnvelope
	}
	if len(encoded) > limits.MaxValueBytes {
		return ErrInvalidEnvelope
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
		return fmt.Errorf(
			"%w: envelope payload is not a canonical RLP list",
			ErrInvalidEnvelope,
		)
	}
	return nil
}

func validateEnvelopeSequence(values []encodedTrieValue) error {
	var profile ForkProfile
	for _, value := range values {
		if value.kind != legacyTrieEnvelope && value.kind != typedTrieEnvelope {
			return ErrInvalidEnvelope
		}
		if len(value.encoded) == 0 {
			return ErrInvalidEnvelope
		}
		if value.kind == typedTrieEnvelope {
			if profile == 0 {
				profile = value.profile
			} else if profile != value.profile {
				return fmt.Errorf(
					"%w: typed values use different fork profiles", ErrInvalidEnvelope,
				)
			}
		}
	}
	return nil
}

func (profile ForkProfile) maximumEnvelopeType() (byte, error) {
	switch profile {
	case BerlinProfile:
		return 1, nil
	case LondonProfile, ParisProfile, ShanghaiProfile:
		return 2, nil
	case CancunProfile:
		return 3, nil
	case PragueProfile, OsakaProfile:
		return 4, nil
	default:
		return 0, ErrUnsupportedProtocolProfile
	}
}

func indexedTrieRoot(
	ctx context.Context,
	values []encodedTrieValue,
	limits Limits,
) (Root, error) {
	if err := validateIndexedRootRequest(ctx, len(values), limits); err != nil {
		return Root{}, err
	}
	trie, _ := NewRawTrie(limits)
	mutations := make([]Mutation, len(values))
	for index, value := range values {
		if len(value.encoded) > limits.MaxValueBytes {
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

func validateIndexedRootRequest(ctx context.Context, count int, limits Limits) error {
	if err := validateTrieLimits(limits); err != nil {
		return err
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	if count > limits.MaxBatchOperations {
		return fmt.Errorf(
			"%w: indexed value bound exceeded",
			ErrResourceLimit,
		)
	}
	return nil
}
