package cache

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

// KeyEncoder deterministically converts a typed logical key to bytes.
type KeyEncoder[K any] interface {
	EncodeKey(K) ([]byte, error)
}

// StringKeyEncoder encodes strings without conversion.
type StringKeyEncoder struct{}

// EncodeKey returns the UTF-8 bytes of key.
func (StringKeyEncoder) EncodeKey(key string) ([]byte, error) {
	return []byte(key), nil
}

// KeySpace hashes logical keys beneath a namespace, name, and version prefix.
type KeySpace[K any] struct {
	prefix     string
	encoder    KeyEncoder[K]
	maxKeySize int
}

// NewKeySpace validates and constructs an isolated versioned key space.
func NewKeySpace[K any](
	namespace string,
	name string,
	version uint32,
	encoder KeyEncoder[K],
	maxKeySize int,
) (KeySpace[K], error) {
	if !validKeyPart(namespace) || !validKeyPart(name) || version == 0 || encoder == nil || maxKeySize <= 0 {
		return KeySpace[K]{}, ErrInvalidKey
	}
	return KeySpace[K]{
		prefix:     fmt.Sprintf("%s:%s:v%d:", namespace, name, version),
		encoder:    encoder,
		maxKeySize: maxKeySize,
	}, nil
}

// Key returns a deterministic backend key without exposing logical key bytes.
func (s KeySpace[K]) Key(logical K) (string, error) {
	encoded, err := s.encoder.EncodeKey(logical)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidKey, err)
	}
	digest := sha256.Sum256(encoded)
	key := s.prefix + base64.RawURLEncoding.EncodeToString(digest[:])
	if len(key) > s.maxKeySize {
		return "", fmt.Errorf("%w: encoded length %d exceeds %d", ErrKeyTooLarge, len(key), s.maxKeySize)
	}
	return key, nil
}

func validKeyPart(value string) bool {
	if value == "" || len(value) > 64 || strings.Contains(value, ":") {
		return false
	}
	for _, r := range value {
		if r < 'a' || r > 'z' {
			if r < '0' || r > '9' {
				if r != '-' && r != '_' {
					return false
				}
			}
		}
	}
	return true
}
