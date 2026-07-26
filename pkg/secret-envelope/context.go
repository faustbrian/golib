package secretenvelope

import (
	"encoding/binary"
	"errors"
	"sort"
	"unicode/utf8"
)

const (
	maxContextEntries   = 32
	maxContextKeySize   = 256
	maxContextValueSize = 1024
	maxContextSize      = 16 * 1024
)

var ErrInvalidContext = errors.New("encryption context is invalid")

// Context is immutable, canonical non-secret associated data.
type Context struct {
	values         map[string]string
	additionalData []byte
}

// NewContext validates and canonically orders non-secret associated data.
func NewContext(values map[string]string) (Context, error) {
	if len(values) == 0 || len(values) > maxContextEntries {
		return Context{}, ErrInvalidContext
	}

	keys := make([]string, 0, len(values))
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		if key == "" ||
			len(key) > maxContextKeySize ||
			len(value) > maxContextValueSize ||
			!utf8.ValidString(key) ||
			!utf8.ValidString(value) {
			return Context{}, ErrInvalidContext
		}
		keys = append(keys, key)
		cloned[key] = value
	}
	sort.Strings(keys)

	additionalData := make([]byte, 0)
	length := make([]byte, 4)
	for _, key := range keys {
		value := cloned[key]
		binary.BigEndian.PutUint32(length, uint32(len(key)))
		additionalData = append(additionalData, length...)
		additionalData = append(additionalData, key...)
		binary.BigEndian.PutUint32(length, uint32(len(value)))
		additionalData = append(additionalData, length...)
		additionalData = append(additionalData, value...)
		if len(additionalData) > maxContextSize {
			return Context{}, ErrInvalidContext
		}
	}

	return Context{
		values:         cloned,
		additionalData: additionalData,
	}, nil
}

// Values returns a caller-owned copy for a key-provider API.
func (encryptionContext Context) Values() map[string]string {
	cloned := make(map[string]string, len(encryptionContext.values))
	for key, value := range encryptionContext.values {
		cloned[key] = value
	}

	return cloned
}

// AdditionalData returns a caller-owned canonical encoding.
func (encryptionContext Context) AdditionalData() []byte {
	return append([]byte(nil), encryptionContext.additionalData...)
}

func (encryptionContext Context) valid() bool {
	return len(encryptionContext.values) > 0 &&
		len(encryptionContext.additionalData) > 0
}
