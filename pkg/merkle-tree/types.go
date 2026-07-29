package merkletree

import "crypto/sha256"

// RawLeaf owns an unencoded application byte string. RawLeaf is deliberately
// distinct from Digest so callers cannot accidentally pass an already-hashed
// node where the selected profile requires raw leaf bytes. Its zero value is a
// valid empty leaf.
type RawLeaf struct {
	value []byte
}

// NewRawLeaf copies value and returns an independently owned raw leaf. It does
// not impose a byte limit; callers accepting untrusted or otherwise unbounded
// input should use NewRawLeafWithLimit.
func NewRawLeaf(value []byte) RawLeaf {
	return RawLeaf{value: cloneBytes(value)}
}

// NewRawLeafWithLimit validates value against maxBytes before copying it.
// maxBytes must be nonzero. Oversized input returns a ResourceError matching
// ErrResourceExhausted without retaining or exposing the input bytes.
func NewRawLeafWithLimit(value []byte, maxBytes uint64) (RawLeaf, error) {
	if maxBytes == 0 {
		return RawLeaf{}, ErrInvalidLimits
	}

	actual := uint64(len(value))
	if actual > maxBytes {
		return RawLeaf{}, &ResourceError{
			Kind:   ResourceLeafBytes,
			Limit:  maxBytes,
			Actual: actual,
		}
	}

	return NewRawLeaf(value), nil
}

// Bytes returns an independent copy of the raw leaf bytes.
func (leaf RawLeaf) Bytes() []byte {
	return cloneBytes(leaf.value)
}

// Digest is an immutable hashed Merkle node. Its zero value is not a valid
// digest. Bytes always returns an independent copy.
type Digest struct {
	algorithm HashAlgorithm
	value     [sha256.Size]byte
}

// Algorithm returns the digest's hash algorithm.
func (digest Digest) Algorithm() HashAlgorithm {
	return digest.algorithm
}

// Bytes returns an independent copy of the digest bytes.
func (digest Digest) Bytes() []byte {
	result := make([]byte, len(digest.value))
	copy(result, digest.value[:])

	return result
}

// Root is an immutable Merkle root identity. In addition to the digest it
// binds the exact profile, profile version, hash algorithm, and tree size.
// Root values are safe for concurrent read-only use. The zero value is not a
// valid root.
type Root struct {
	profileID      ProfileID
	profileVersion uint16
	algorithm      HashAlgorithm
	treeSize       uint64
	digest         Digest
}

// ProfileID returns the root's stable profile identity.
func (root Root) ProfileID() ProfileID {
	return root.profileID
}

// ProfileVersion returns the root's profile version.
func (root Root) ProfileVersion() uint16 {
	return root.profileVersion
}

// Algorithm returns the root's hash algorithm.
func (root Root) Algorithm() HashAlgorithm {
	return root.algorithm
}

// TreeSize returns the number of ordered raw leaves committed by the root.
func (root Root) TreeSize() uint64 {
	return root.treeSize
}

// Digest returns an immutable copy of the root digest.
func (root Root) Digest() Digest {
	return root.digest
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}

	result := make([]byte, len(value))
	copy(result, value)

	return result
}
