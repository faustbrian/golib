package mpt

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/internal/rlp"
)

// StorageTrie is an immutable Ethereum account-storage trie. It hashes exact
// 32-byte slot keys once and stores canonical RLP unsigned 256-bit values.
type StorageTrie struct {
	trie SecureTrie
}

// NewStorageTrie constructs an empty account-storage trie.
func NewStorageTrie(limits Limits) (StorageTrie, error) {
	trie, err := NewSecureTrie(limits)
	if err != nil {
		return StorageTrie{}, err
	}
	return StorageTrie{trie: trie}, nil
}

// LoadStorageTrie constructs a lazy immutable account-storage trie from a
// trusted root.
func LoadStorageTrie(root Root, reader NodeReader, limits Limits) (StorageTrie, error) {
	trie, err := LoadSecureTrie(root, reader, limits)
	if err != nil {
		return StorageTrie{}, err
	}
	return StorageTrie{trie: trie}, nil
}

// Root returns the storage trie's 32-byte commitment.
func (trie StorageTrie) Root() (Root, error) {
	return trie.trie.Root()
}

// GetSlot returns the present storage value as a 32-byte big-endian word.
// Missing slots return ErrAbsentKey; a present zero encoding is rejected.
func (trie StorageTrie) GetSlot(
	ctx context.Context,
	slot [RootBytes]byte,
) ([RootBytes]byte, error) {
	encoded, err := trie.trie.Get(ctx, slot[:])
	if err != nil {
		return [RootBytes]byte{}, err
	}
	return decodeStorageWord(encoded, trie.trie.snapshot.limits)
}

// HasSlot reports whether a canonical non-zero value is present at slot.
func (trie StorageTrie) HasSlot(
	ctx context.Context,
	slot [RootBytes]byte,
) (bool, error) {
	_, err := trie.GetSlot(ctx, slot)
	if errorsIsAbsent(err) {
		return false, nil
	}
	return err == nil, err
}

// UpdateSlot returns a new snapshot containing value at slot. An all-zero word
// deletes the slot, matching the Ethereum storage-trie default value.
func (trie StorageTrie) UpdateSlot(
	ctx context.Context,
	slot [RootBytes]byte,
	value [RootBytes]byte,
) (StorageTrie, error) {
	trimmed := trimWord(value)
	if len(trimmed) == 0 {
		return trie.DeleteSlot(ctx, slot)
	}
	updated, err := trie.trie.Update(ctx, slot[:], encodeStorageInteger(trimmed))
	if err != nil {
		return StorageTrie{}, err
	}
	return StorageTrie{trie: updated}, nil
}

// DeleteSlot returns a new snapshot without slot.
func (trie StorageTrie) DeleteSlot(
	ctx context.Context,
	slot [RootBytes]byte,
) (StorageTrie, error) {
	updated, err := trie.trie.Delete(ctx, slot[:])
	if err != nil {
		return StorageTrie{}, err
	}
	return StorageTrie{trie: updated}, nil
}

// ProveSlot constructs a membership or non-membership proof for slot.
func (trie StorageTrie) ProveSlot(
	ctx context.Context,
	slot [RootBytes]byte,
) (Proof, error) {
	return trie.trie.Prove(ctx, slot[:])
}

// Commit atomically writes every pending hashed node and publishes the root.
func (trie StorageTrie) Commit(
	ctx context.Context,
	store NodeStore,
) (StorageTrie, error) {
	committed, err := trie.trie.Commit(ctx, store)
	if err != nil {
		return StorageTrie{}, err
	}
	return StorageTrie{trie: committed}, nil
}

// Rebuild reconstructs a fully materialized storage-trie snapshot and verifies
// that its root is unchanged.
func (trie StorageTrie) Rebuild(ctx context.Context) (StorageTrie, error) {
	rebuilt, err := trie.trie.Rebuild(ctx)
	if err != nil {
		return StorageTrie{}, err
	}
	return StorageTrie{trie: rebuilt}, nil
}

// RecoverNode verifies one missing encoded node and returns a new overlay
// snapshot that can resume the interrupted operation.
func (trie StorageTrie) RecoverNode(
	ctx context.Context,
	hash Root,
	encoded []byte,
) (StorageTrie, error) {
	recovered, err := trie.trie.RecoverNode(ctx, hash, encoded)
	if err != nil {
		return StorageTrie{}, err
	}
	return StorageTrie{trie: recovered}, nil
}

func decodeStorageWord(encoded []byte, limits Limits) ([RootBytes]byte, error) {
	if err := validateTrieLimits(limits); err != nil {
		return [RootBytes]byte{}, err
	}
	decoded, err := rlp.Decode(encoded, rlp.Limits{
		MaxEncodedBytes: limits.MaxValueBytes,
		MaxDepth:        1,
		MaxItems:        1,
	})
	if err != nil {
		return [RootBytes]byte{}, fmt.Errorf(
			"%w: malformed storage integer RLP", ErrInvalidStorageValue,
		)
	}
	if decoded.Kind() != rlp.KindString {
		return [RootBytes]byte{}, fmt.Errorf(
			"%w: malformed storage integer RLP", ErrInvalidStorageValue,
		)
	}
	value := decoded.Bytes()
	if len(value) == 0 || !canonicalUint256(value) {
		return [RootBytes]byte{}, fmt.Errorf(
			"%w: non-canonical zero or integer", ErrInvalidStorageValue,
		)
	}
	var word [RootBytes]byte
	copy(word[RootBytes-len(value):], value)
	return word, nil
}
