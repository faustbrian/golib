package mpt

import (
	"fmt"

	"golang.org/x/crypto/sha3"
)

// RootBytes is the byte length of an Ethereum trie root commitment.
const RootBytes = 32

// Root is a legacy Keccak-256 commitment to a canonical trie root node.
type Root [RootBytes]byte

var canonicalEmptyRoot = Root{
	0x56, 0xe8, 0x1f, 0x17, 0x1b, 0xcc, 0x55, 0xa6,
	0xff, 0x83, 0x45, 0xe6, 0x92, 0xc0, 0xf8, 0x6e,
	0x5b, 0x48, 0xe0, 0x1b, 0x99, 0x6c, 0xad, 0xc0,
	0x01, 0x62, 0x2f, 0xb5, 0xe3, 0x63, 0xb4, 0x21,
}

// EmptyRoot returns Keccak-256(RLP("")), Ethereum's canonical empty trie root.
func EmptyRoot() Root {
	return canonicalEmptyRoot
}

// RootFromBytes validates and copies a 32-byte root commitment.
func RootFromBytes(value []byte) (Root, error) {
	if len(value) != RootBytes {
		return Root{}, fmt.Errorf("%w: got %d bytes", ErrInvalidRoot, len(value))
	}
	var root Root
	copy(root[:], value)
	return root, nil
}

// Bytes returns a copy of the root commitment.
func (root Root) Bytes() []byte {
	return append([]byte(nil), root[:]...)
}

func keccakRoot(value []byte) Root {
	hash := sha3.NewLegacyKeccak256()
	_, _ = hash.Write(value)
	var root Root
	hash.Sum(root[:0])
	return root
}
