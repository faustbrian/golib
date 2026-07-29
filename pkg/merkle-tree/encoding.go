package merkletree

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
)

const (
	canonicalEncodingVersion = byte(1)
	encodingObjectRoot       = byte(1)
	encodingHeaderSize       = 10
	encodedRootSize          = encodingHeaderSize + 8 + sha256.Size
	defaultMaxEncodedBytes   = uint64(32 << 20)
)

var canonicalEncodingMagic = [4]byte{'M', 'T', 'R', 'E'}

// EncodingLimits bounds bytes consumed by canonical binary decoders. MaxBytes
// must be nonzero. Values are copied and remain immutable during an operation.
type EncodingLimits struct {
	MaxBytes uint64
}

// DefaultEncodingLimits permits canonical objects up to 32 MiB.
func DefaultEncodingLimits() EncodingLimits {
	return EncodingLimits{MaxBytes: defaultMaxEncodedBytes}
}

func (limits EncodingLimits) validate() error {
	if limits.MaxBytes == 0 {
		return ErrInvalidLimits
	}

	return nil
}

// MarshalBinary returns the version-1 canonical binary root encoding. The
// result owns its bytes and implements encoding.BinaryMarshaler.
func (root Root) MarshalBinary() ([]byte, error) {
	profile, err := profileForRootEncoding(root)
	if err != nil {
		return nil, err
	}

	result := make([]byte, encodedRootSize)
	appendEncodingHeader(result, encodingObjectRoot, profile)
	binary.BigEndian.PutUint64(
		result[encodingHeaderSize:encodingHeaderSize+8],
		root.treeSize,
	)
	copy(result[encodingHeaderSize+8:], root.digest.value[:])

	return result, nil
}

// ParseRoot decodes one complete version-1 canonical binary root. It rejects
// non-canonical, truncated, trailing, unsupported, or oversized input.
func ParseRoot(data []byte, limits EncodingLimits) (Root, error) {
	if err := limits.validate(); err != nil {
		return Root{}, err
	}
	dataSize := uint64(len(data))
	if dataSize > limits.MaxBytes {
		return Root{}, &ResourceError{
			Kind:   ResourceEncodedBytes,
			Limit:  limits.MaxBytes,
			Actual: dataSize,
		}
	}
	if len(data) != encodedRootSize {
		return Root{}, ErrMalformedEncoding
	}

	profile, err := parseEncodingHeader(data, encodingObjectRoot)
	if err != nil {
		return Root{}, err
	}
	treeSize := binary.BigEndian.Uint64(
		data[encodingHeaderSize : encodingHeaderSize+8],
	)
	var digest [sha256.Size]byte
	copy(digest[:], data[encodingHeaderSize+8:])
	if treeSize == 0 {
		empty := sha256.Sum256(nil)
		if subtle.ConstantTimeCompare(digest[:], empty[:]) != 1 {
			return Root{}, ErrMalformedEncoding
		}
	}

	return newRoot(profile, treeSize, digest), nil
}

func appendEncodingHeader(
	target []byte,
	objectType byte,
	profile Profile,
) {
	copy(target[:4], canonicalEncodingMagic[:])
	target[4] = canonicalEncodingVersion
	target[5] = objectType
	target[6] = byte(profile.id)
	binary.BigEndian.PutUint16(target[7:9], profile.version)
	target[9] = byte(profile.algorithm)
}

func parseEncodingHeader(
	data []byte,
	expectedObject byte,
) (Profile, error) {
	if !bytes.Equal(data[:4], canonicalEncodingMagic[:]) {
		return Profile{}, ErrMalformedEncoding
	}
	if data[4] != canonicalEncodingVersion {
		return Profile{}, ErrUnsupportedEncodingVersion
	}
	if data[5] != expectedObject {
		return Profile{}, ErrMalformedEncoding
	}

	profile := Profile{
		id:        ProfileID(data[6]),
		version:   binary.BigEndian.Uint16(data[7:9]),
		algorithm: HashAlgorithm(data[9]),
	}
	if profile.algorithm != HashSHA256 {
		return Profile{}, ErrUnsupportedAlgorithm
	}
	if err := profile.validate(); err != nil {
		return Profile{}, err
	}

	return profile, nil
}

func profileForRootEncoding(root Root) (Profile, error) {
	profile := Profile{
		id:        root.profileID,
		version:   root.profileVersion,
		algorithm: root.algorithm,
	}
	if profile.validate() != nil ||
		root.digest.algorithm != root.algorithm {
		return Profile{}, ErrMalformedEncoding
	}
	if root.treeSize == 0 {
		empty := sha256.Sum256(nil)
		if subtle.ConstantTimeCompare(
			root.digest.value[:],
			empty[:],
		) != 1 {
			return Profile{}, ErrMalformedEncoding
		}
	}

	return profile, nil
}
