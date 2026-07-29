// Package rlp implements the canonical Recursive Length Prefix subset used by
// Ethereum trie nodes.
package rlp

import (
	"errors"
	"fmt"
)

// Kind identifies an RLP string or list.
type Kind uint8

const (
	// KindString is an RLP byte string.
	KindString Kind = iota
	// KindList is an RLP list.
	KindList
)

var (
	// ErrMalformed identifies truncated input, trailing input, or invalid
	// structural lengths.
	ErrMalformed = errors.New("rlp: malformed input")
	// ErrNonCanonical identifies a validly framed but non-minimal RLP encoding.
	ErrNonCanonical = errors.New("rlp: non-canonical input")
	// ErrLimit identifies work rejected by a configured resource limit.
	ErrLimit = errors.New("rlp: resource limit exceeded")
)

// Limits bounds encoding and decoding work.
type Limits struct {
	MaxEncodedBytes int
	MaxDepth        int
	MaxItems        int
}

// DefaultLimits returns conservative standalone RLP limits. Trie operations
// may apply smaller profile-specific limits.
func DefaultLimits() Limits {
	return Limits{
		MaxEncodedBytes: 16 << 20,
		MaxDepth:        1024,
		MaxItems:        1 << 20,
	}
}

// Value is an immutable decoded or caller-constructed RLP value.
type Value struct {
	kind     Kind
	bytes    []byte
	elements []Value
}

// String constructs an owned RLP byte string.
func String(value []byte) Value {
	return Value{kind: KindString, bytes: append([]byte(nil), value...)}
}

// List constructs an owned RLP list.
func List(values ...Value) Value {
	return Value{kind: KindList, elements: append([]Value(nil), values...)}
}

// Kind returns whether value is a string or list.
func (value Value) Kind() Kind {
	return value.kind
}

// Bytes returns a copy of a string value, or nil for a list.
func (value Value) Bytes() []byte {
	if value.kind != KindString {
		return nil
	}
	return append([]byte(nil), value.bytes...)
}

// Elements returns a copy of a list's elements, or nil for a string.
func (value Value) Elements() []Value {
	if value.kind != KindList {
		return nil
	}
	return append([]Value(nil), value.elements...)
}

// Encode returns the canonical RLP encoding of value.
func Encode(value Value, limits Limits) ([]byte, error) {
	if err := validateLimits(limits); err != nil {
		return nil, err
	}
	items := 0
	encoded, err := encode(value, limits, 0, &items)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func encode(value Value, limits Limits, depth int, items *int) ([]byte, error) {
	if depth > limits.MaxDepth {
		return nil, fmt.Errorf("%w: maximum depth", ErrLimit)
	}
	if *items >= limits.MaxItems {
		return nil, fmt.Errorf("%w: maximum items", ErrLimit)
	}
	*items++

	switch value.kind {
	case KindString:
		if len(value.bytes) == 1 && value.bytes[0] < 0x80 {
			return append([]byte(nil), value.bytes...), nil
		}
		return prefixPayload(0x80, 0xb7, value.bytes, limits.MaxEncodedBytes)
	case KindList:
		payload := make([]byte, 0)
		for _, child := range value.elements {
			encoded, err := encode(child, limits, depth+1, items)
			if err != nil {
				return nil, err
			}
			payload, err = appendListPayload(
				payload, encoded, limits.MaxEncodedBytes,
			)
			if err != nil {
				return nil, err
			}
		}
		return prefixPayload(0xc0, 0xf7, payload, limits.MaxEncodedBytes)
	default:
		return nil, fmt.Errorf("%w: unknown value kind", ErrMalformed)
	}
}

func appendListPayload(payload, encoded []byte, maximum int) ([]byte, error) {
	if len(encoded) > maximum-len(payload)-1 {
		return nil, fmt.Errorf("%w: maximum encoded bytes", ErrLimit)
	}
	return append(payload, encoded...), nil
}

func prefixPayload(shortBase, longBase byte, payload []byte, maximum int) ([]byte, error) {
	if len(payload) <= 55 {
		if len(payload)+1 > maximum {
			return nil, fmt.Errorf("%w: maximum encoded bytes", ErrLimit)
		}
		encoded := make([]byte, 1, len(payload)+1)
		encoded[0] = shortBase + byte(len(payload))
		return append(encoded, payload...), nil
	}

	length := encodeLength(len(payload))
	prefixBytes := 1 + len(length)
	if len(payload) > maximum-prefixBytes {
		return nil, fmt.Errorf("%w: maximum encoded bytes", ErrLimit)
	}
	encoded := make([]byte, prefixBytes, prefixBytes+len(payload))
	encoded[0] = longBase + byte(len(length))
	copy(encoded[1:], length)
	return append(encoded, payload...), nil
}

func encodeLength(length int) []byte {
	var storage [8]byte
	index := len(storage)
	for length > 0 {
		index--
		storage[index] = byte(length)
		length >>= 8
	}
	return append([]byte(nil), storage[index:]...)
}

// Decode decodes exactly one canonical RLP value and rejects trailing bytes.
// Returned strings own their memory.
func Decode(encoded []byte, limits Limits) (Value, error) {
	if err := validateLimits(limits); err != nil {
		return Value{}, err
	}
	if len(encoded) == 0 {
		return Value{}, fmt.Errorf("%w: empty input", ErrMalformed)
	}
	if len(encoded) > limits.MaxEncodedBytes {
		return Value{}, fmt.Errorf("%w: maximum encoded bytes", ErrLimit)
	}

	items := 0
	value, consumed, err := decode(encoded, limits, 0, &items)
	if err != nil {
		return Value{}, err
	}
	if consumed != len(encoded) {
		return Value{}, fmt.Errorf("%w: trailing bytes", ErrMalformed)
	}
	return value, nil
}

func decode(encoded []byte, limits Limits, depth int, items *int) (Value, int, error) {
	if depth > limits.MaxDepth {
		return Value{}, 0, fmt.Errorf("%w: maximum depth", ErrLimit)
	}
	if *items >= limits.MaxItems {
		return Value{}, 0, fmt.Errorf("%w: maximum items", ErrLimit)
	}
	if len(encoded) == 0 {
		return Value{}, 0, fmt.Errorf("%w: truncated item", ErrMalformed)
	}
	*items++

	prefix := encoded[0]
	switch {
	case prefix <= 0x7f:
		return String(encoded[:1]), 1, nil
	case prefix <= 0xb7:
		length := int(prefix) - 128
		value, err := stringPayload(encoded, 1, length)
		if err != nil {
			return Value{}, 0, err
		}
		if length == 1 && value[0] < 0x80 {
			return Value{}, 0, fmt.Errorf("%w: overlong single byte", ErrNonCanonical)
		}
		return String(value), 1 + length, nil
	case prefix <= 0xbf:
		length, offset, err := longPayloadLength(encoded, int(prefix-0xb7))
		if err != nil {
			return Value{}, 0, err
		}
		if length < 56 {
			return Value{}, 0, fmt.Errorf("%w: long string below 56 bytes", ErrNonCanonical)
		}
		value, err := stringPayload(encoded, offset, length)
		if err != nil {
			return Value{}, 0, err
		}
		return String(value), offset + length, nil
	case prefix <= 0xf7:
		length := int(prefix - 0xc0)
		return decodeList(encoded, 1, length, limits, depth, items)
	default:
		length, offset, err := longPayloadLength(encoded, int(prefix-0xf7))
		if err != nil {
			return Value{}, 0, err
		}
		if length < 56 {
			return Value{}, 0, fmt.Errorf("%w: long list below 56 bytes", ErrNonCanonical)
		}
		return decodeList(encoded, offset, length, limits, depth, items)
	}
}

func stringPayload(encoded []byte, offset, length int) ([]byte, error) {
	if length > len(encoded)-offset {
		return nil, fmt.Errorf("%w: truncated payload", ErrMalformed)
	}
	return encoded[offset : offset+length], nil
}

func longPayloadLength(encoded []byte, lengthBytes int) (int, int, error) {
	offset := 1 + lengthBytes
	if lengthBytes == 0 || offset > len(encoded) {
		return 0, 0, fmt.Errorf("%w: truncated length", ErrMalformed)
	}
	if encoded[1] == 0 {
		return 0, 0, fmt.Errorf("%w: leading-zero length", ErrNonCanonical)
	}

	maximum := int(^uint(0) >> 1)
	length := 0
	for _, current := range encoded[1:offset] {
		if length > (maximum-int(current))/256 {
			return 0, 0, fmt.Errorf("%w: length overflow", ErrLimit)
		}
		length = length*256 + int(current)
	}
	return length, offset, nil
}

func decodeList(
	encoded []byte,
	offset int,
	length int,
	limits Limits,
	depth int,
	items *int,
) (Value, int, error) {
	payload, err := stringPayload(encoded, offset, length)
	if err != nil {
		return Value{}, 0, err
	}

	values := make([]Value, 0)
	for len(payload) != 0 {
		value, consumed, decodeErr := decode(payload, limits, depth+1, items)
		switch decodeErr {
		case nil:
			values = append(values, value)
			payload = payload[consumed:]
		default:
			return Value{}, 0, decodeErr
		}
	}
	return List(values...), offset + length, nil
}

func validateLimits(limits Limits) error {
	if limits.MaxEncodedBytes <= 0 || limits.MaxDepth < 0 || limits.MaxItems <= 0 {
		return fmt.Errorf("%w: invalid limits", ErrLimit)
	}
	return nil
}
