package mpt

import "math/bits"

// RLPIndexKey returns the canonical RLP integer encoding used as the raw trie
// key for transaction and receipt indexes. Zero is the RLP empty string.
func RLPIndexKey(index uint64) []byte {
	if index == 0 {
		return []byte{0x80}
	}
	if index < 0x80 {
		return []byte{byte(index)}
	}

	length := (bits.Len64(index) + 7) / 8
	encoded := make([]byte, length+1)
	encoded[0] = 0x80 + byte(length)
	for position := len(encoded) - 1; position > 0; position-- {
		encoded[position] = byte(index)
		index >>= 8
	}
	return encoded
}
