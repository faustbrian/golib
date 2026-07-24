package cache

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const defaultMaxEncodedSize = 1 << 20

// Codec serializes and deserializes typed cache values.
type Codec[V any] interface {
	Encode(V) ([]byte, error)
	Decode([]byte) (V, error)
}

// JSONCodec stores strict JSON behind a one-byte schema version.
type JSONCodec[V any] struct {
	Version        byte
	MaxEncodedSize int
}

// Encode serializes a value with its configured schema version.
func (c JSONCodec[V]) Encode(value V) ([]byte, error) {
	if c.Version == 0 {
		return nil, fmt.Errorf("%w: version must be non-zero", ErrSchemaMismatch)
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDecode, err)
	}
	limit := c.sizeLimit()
	if len(payload)+1 > limit {
		return nil, fmt.Errorf("%w: encoded length %d exceeds %d", ErrValueTooLarge, len(payload)+1, limit)
	}
	encoded := make([]byte, len(payload)+1)
	encoded[0] = c.Version
	copy(encoded[1:], payload)
	return encoded, nil
}

// Decode validates size and schema version before strict JSON decoding.
func (c JSONCodec[V]) Decode(encoded []byte) (V, error) {
	var zero V
	if len(encoded) > c.sizeLimit() {
		return zero, fmt.Errorf("%w: encoded length %d exceeds %d", ErrValueTooLarge, len(encoded), c.sizeLimit())
	}
	if len(encoded) == 0 || encoded[0] != c.Version {
		return zero, fmt.Errorf("%w: got version %d, want %d", ErrSchemaMismatch, versionOf(encoded), c.Version)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded[1:]))
	decoder.DisallowUnknownFields()
	var value V
	if err := decoder.Decode(&value); err != nil {
		return zero, fmt.Errorf("%w: %w", ErrDecode, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return zero, fmt.Errorf("%w: trailing data", ErrDecode)
	}
	return value, nil
}

func (c JSONCodec[V]) sizeLimit() int {
	if c.MaxEncodedSize <= 0 {
		return defaultMaxEncodedSize
	}
	return c.MaxEncodedSize
}

func versionOf(encoded []byte) byte {
	if len(encoded) == 0 {
		return 0
	}
	return encoded[0]
}
