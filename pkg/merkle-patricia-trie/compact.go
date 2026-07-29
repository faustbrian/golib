package mpt

import "fmt"

// MaxCompactPathNibbles is the hard allocation bound used by the standalone
// compact-path helpers. Trie operations apply their configured, potentially
// smaller, path limit before calling these helpers.
const MaxCompactPathNibbles = 8192

// CompactPath is a decoded canonical Ethereum hex-prefix path. Leaf termination
// is structural metadata and is never included in Nibbles.
type CompactPath struct {
	nibbles []byte
	leaf    bool
}

// Nibbles returns a copy of the path's ordered nibbles.
func (path CompactPath) Nibbles() []byte {
	return append([]byte(nil), path.nibbles...)
}

// Leaf reports whether the compact path terminates at a leaf.
func (path CompactPath) Leaf() bool {
	return path.leaf
}

// EncodeCompactPath encodes nibbles using Ethereum's canonical hex-prefix
// compact encoding. Every input nibble must be in the range 0..15.
func EncodeCompactPath(nibbles []byte, leaf bool) ([]byte, error) {
	if len(nibbles) > MaxCompactPathNibbles {
		return nil, fmt.Errorf("%w: nibble limit exceeded", ErrInvalidCompactPath)
	}
	for _, nibble := range nibbles {
		if nibble > 0x0f {
			return nil, fmt.Errorf("%w: nibble outside 0..15", ErrInvalidCompactPath)
		}
	}

	odd := len(nibbles)%2 == 1
	flag := byte(0)
	if leaf {
		flag = 2
	}

	encoded := make([]byte, (len(nibbles)+2)/2)
	offset := 0
	if odd {
		encoded[0] = ((flag + 1) << 4) | nibbles[0]
		offset = 1
	} else {
		encoded[0] = flag << 4
	}
	for index := offset; index < len(nibbles); index += 2 {
		encoded[1+(index-offset)/2] = nibbles[index]<<4 | nibbles[index+1]
	}

	return encoded, nil
}

// DecodeCompactPath decodes one complete canonical Ethereum hex-prefix path.
// The returned path owns its memory.
func DecodeCompactPath(encoded []byte) (CompactPath, error) {
	if len(encoded) == 0 {
		return CompactPath{}, fmt.Errorf("%w: empty encoding", ErrInvalidCompactPath)
	}
	if len(encoded) > MaxCompactPathNibbles/2+1 {
		return CompactPath{}, fmt.Errorf("%w: nibble limit exceeded", ErrInvalidCompactPath)
	}

	flag := encoded[0] >> 4
	if flag > 3 {
		return CompactPath{}, fmt.Errorf("%w: unsupported flag", ErrInvalidCompactPath)
	}
	odd := flag&1 == 1
	if !odd && encoded[0]&0x0f != 0 {
		return CompactPath{}, fmt.Errorf("%w: non-zero even-path padding", ErrInvalidCompactPath)
	}

	count := len(encoded)*2 - 2
	if odd {
		count++
	}
	if count > MaxCompactPathNibbles {
		return CompactPath{}, fmt.Errorf("%w: nibble limit exceeded", ErrInvalidCompactPath)
	}
	nibbles := make([]byte, count)
	source := 1
	target := 0
	if odd {
		nibbles[0] = encoded[0] & 0x0f
		target = 1
	}
	for ; source < len(encoded); source++ {
		nibbles[target] = encoded[source] >> 4
		nibbles[target+1] = encoded[source] & 0x0f
		target += 2
	}

	return CompactPath{nibbles: nibbles, leaf: flag&2 == 2}, nil
}
