package mpt

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/internal/rlp"
)

// EncodedAccountValue is one immutable canonical Ethereum state-account value.
// It can only be constructed from exact account field types.
type EncodedAccountValue struct {
	encoded []byte
}

// NewAccountValue constructs the canonical RLP account value
// [nonce, balance, storageRoot, codeHash]. Account lifecycle rules, including
// empty-account clearing, remain the caller's responsibility.
func NewAccountValue(
	nonce uint64,
	balance [RootBytes]byte,
	storageRoot Root,
	codeHash [RootBytes]byte,
	limits Limits,
) (EncodedAccountValue, error) {
	if err := validateTrieLimits(limits); err != nil {
		return EncodedAccountValue{}, err
	}
	encoded, err := rlp.Encode(
		rlp.List(
			rlp.String(minimalUint64(nonce)),
			rlp.String(trimWord(balance)),
			rlp.String(storageRoot[:]),
			rlp.String(codeHash[:]),
		),
		rlp.Limits{
			MaxEncodedBytes: limits.MaxValueBytes,
			MaxDepth:        4,
			MaxItems:        5,
		},
	)
	if err != nil {
		return EncodedAccountValue{}, fmt.Errorf(
			"%w: account encoding exceeds configured limits", ErrResourceLimit,
		)
	}
	return EncodedAccountValue{encoded: encoded}, nil
}

// Bytes returns an owned copy of the canonical account RLP.
func (value EncodedAccountValue) Bytes() []byte {
	return append([]byte(nil), value.encoded...)
}

// EmptyCodeHash returns Keccak-256 of the empty byte string, the canonical code
// hash for an account without bytecode.
func EmptyCodeHash() [RootBytes]byte {
	return [RootBytes]byte{
		0xc5, 0xd2, 0x46, 0x01, 0x86, 0xf7, 0x23, 0x3c,
		0x92, 0x7e, 0x7d, 0xb2, 0xdc, 0xc7, 0x03, 0xc0,
		0xe5, 0x00, 0xb6, 0x53, 0xca, 0x82, 0x27, 0x3b,
		0x7b, 0xfa, 0xd8, 0x04, 0x5d, 0x85, 0xa4, 0x70,
	}
}

// StateTrie is an immutable Ethereum account trie. It hashes exact 20-byte
// addresses once and accepts only canonical account values.
type StateTrie struct {
	trie SecureTrie
}

// NewStateTrie constructs an empty state trie.
func NewStateTrie(limits Limits) (StateTrie, error) {
	trie, err := NewSecureTrie(limits)
	if err != nil {
		return StateTrie{}, err
	}
	return StateTrie{trie: trie}, nil
}

// LoadStateTrie constructs a lazy immutable state trie from a trusted root.
func LoadStateTrie(root Root, reader NodeReader, limits Limits) (StateTrie, error) {
	trie, err := LoadSecureTrie(root, reader, limits)
	if err != nil {
		return StateTrie{}, err
	}
	return StateTrie{trie: trie}, nil
}

// Root returns the state trie's 32-byte commitment.
func (trie StateTrie) Root() (Root, error) {
	return trie.trie.Root()
}

// GetAccount returns the canonical account at address. The returned account is
// bound to this snapshot and can be used as the account input to storage-proof
// verification.
func (trie StateTrie) GetAccount(
	ctx context.Context,
	address [20]byte,
) (Account, error) {
	encoded, err := trie.trie.Get(ctx, address[:])
	if err != nil {
		return Account{}, err
	}
	return decodeAccount(encoded, trie.trie.snapshot.limits)
}

// HasAccount reports whether a canonical account is present at address.
func (trie StateTrie) HasAccount(ctx context.Context, address [20]byte) (bool, error) {
	_, err := trie.GetAccount(ctx, address)
	if errorsIsAbsent(err) {
		return false, nil
	}
	return err == nil, err
}

// UpdateAccount returns a new snapshot containing value at address. It does
// not apply fork-dependent empty-account deletion or other lifecycle rules.
func (trie StateTrie) UpdateAccount(
	ctx context.Context,
	address [20]byte,
	value EncodedAccountValue,
) (StateTrie, error) {
	if trie.trie.snapshot == nil {
		return StateTrie{}, ErrUninitialized
	}
	if len(value.encoded) == 0 {
		return StateTrie{}, ErrInvalidAccount
	}
	updated, err := trie.trie.Update(ctx, address[:], value.encoded)
	if err != nil {
		return StateTrie{}, err
	}
	return StateTrie{trie: updated}, nil
}

// DeleteAccount returns a new snapshot without address.
func (trie StateTrie) DeleteAccount(
	ctx context.Context,
	address [20]byte,
) (StateTrie, error) {
	updated, err := trie.trie.Delete(ctx, address[:])
	if err != nil {
		return StateTrie{}, err
	}
	return StateTrie{trie: updated}, nil
}

// ProveAccount constructs a membership or non-membership proof for address.
func (trie StateTrie) ProveAccount(
	ctx context.Context,
	address [20]byte,
) (Proof, error) {
	return trie.trie.Prove(ctx, address[:])
}

// Commit atomically writes every pending hashed node and publishes the root.
func (trie StateTrie) Commit(ctx context.Context, store NodeStore) (StateTrie, error) {
	committed, err := trie.trie.Commit(ctx, store)
	if err != nil {
		return StateTrie{}, err
	}
	return StateTrie{trie: committed}, nil
}

// Rebuild reconstructs a fully materialized state-trie snapshot and verifies
// that its root is unchanged.
func (trie StateTrie) Rebuild(ctx context.Context) (StateTrie, error) {
	rebuilt, err := trie.trie.Rebuild(ctx)
	if err != nil {
		return StateTrie{}, err
	}
	return StateTrie{trie: rebuilt}, nil
}

// RecoverNode verifies one missing encoded node and returns a new overlay
// snapshot that can resume the interrupted operation.
func (trie StateTrie) RecoverNode(
	ctx context.Context,
	hash Root,
	encoded []byte,
) (StateTrie, error) {
	recovered, err := trie.trie.RecoverNode(ctx, hash, encoded)
	if err != nil {
		return StateTrie{}, err
	}
	return StateTrie{trie: recovered}, nil
}

func minimalUint64(value uint64) []byte {
	if value == 0 {
		return nil
	}
	var word [8]byte
	for index := len(word) - 1; index >= 0; index-- {
		word[index] = byte(value)
		value >>= 8
	}
	index := 0
	for word[index] == 0 {
		index++
	}
	return append([]byte(nil), word[index:]...)
}

func trimWord(word [RootBytes]byte) []byte {
	index := 0
	for index < len(word) && word[index] == 0 {
		index++
	}
	return append([]byte(nil), word[index:]...)
}
