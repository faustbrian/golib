// Package keyring wraps envelope data keys with versioned AES-256 keys that
// are supplied by the application's secret-delivery boundary.
package keyring

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"unicode"
	"unicode/utf8"

	secretenvelope "github.com/faustbrian/golib/pkg/secret-envelope"
)

const (
	wrappedDataKeyVersion   byte = 1
	maximumKeys                  = 32
	maximumKeyReferenceSize      = 2048
	redacted                     = "[REDACTED]"
)

var (
	ErrInvalidKeyring = errors.New("keyring configuration is invalid")
	ErrInvalidRequest = errors.New("keyring data-key request is invalid")
	ErrKeyNotFound    = errors.New("keyring key reference is unavailable")
	ErrEntropy        = errors.New("keyring entropy failed")
	ErrAuthentication = errors.New("keyring data-key authentication failed")
)

// Provider wraps fresh envelope data keys with an immutable versioned keyring.
// It is safe for concurrent use. Keys remain in process memory for the
// provider's lifetime and are never included in text, JSON, or slog output.
type Provider struct {
	keys    map[string][secretenvelope.DataKeySize]byte
	entropy io.Reader
}

// New validates and copies a non-empty keyring. Each value must contain
// exactly 32 bytes and each stable reference must remain available while
// ciphertext bearing that reference can still be read.
func New(keys map[string][]byte) (*Provider, error) {
	if len(keys) == 0 || len(keys) > maximumKeys {
		return nil, ErrInvalidKeyring
	}
	cloned := make(
		map[string][secretenvelope.DataKeySize]byte,
		len(keys),
	)
	for reference, key := range keys {
		if !validKeyReference(reference) ||
			len(key) != secretenvelope.DataKeySize {
			return nil, ErrInvalidKeyring
		}
		var fixed [secretenvelope.DataKeySize]byte
		copy(fixed[:], key)
		cloned[reference] = fixed
	}

	return &Provider{keys: cloned, entropy: rand.Reader}, nil
}

// GenerateDataKey creates a fresh data key and wraps it under the requested
// versioned key reference with the canonical encryption context as AAD.
func (provider *Provider) GenerateDataKey(
	ctx context.Context,
	keyReference string,
	encryptionContext secretenvelope.Context,
) (secretenvelope.DataKey, error) {
	key, additionalData, err := provider.request(
		ctx,
		keyReference,
		encryptionContext,
	)
	if err != nil {
		return secretenvelope.DataKey{}, err
	}
	plaintext := make([]byte, secretenvelope.DataKeySize)
	if _, err := io.ReadFull(provider.entropy, plaintext); err != nil {
		zero(plaintext)
		return secretenvelope.DataKey{}, ErrEntropy
	}
	wrapped, err := provider.wrap(
		key,
		keyReference,
		plaintext,
		additionalData,
	)
	if err != nil {
		zero(plaintext)
		return secretenvelope.DataKey{}, err
	}
	return secretenvelope.NewDataKey(
		plaintext,
		wrapped,
		keyReference,
	)
}

// DecryptDataKey authenticates and unwraps a data key with the exact retained
// key reference and canonical encryption context.
func (provider *Provider) DecryptDataKey(
	ctx context.Context,
	keyReference string,
	ciphertext []byte,
	encryptionContext secretenvelope.Context,
) ([]byte, error) {
	key, additionalData, err := provider.request(
		ctx,
		keyReference,
		encryptionContext,
	)
	if err != nil {
		return nil, err
	}
	block, _ := aes.NewCipher(key[:])
	authenticatedCipher, _ := cipher.NewGCM(block)
	wantSize := 1 + authenticatedCipher.NonceSize() +
		secretenvelope.DataKeySize + authenticatedCipher.Overhead()
	if len(ciphertext) != wantSize || ciphertext[0] != wrappedDataKeyVersion {
		return nil, ErrAuthentication
	}
	nonceEnd := 1 + authenticatedCipher.NonceSize()
	plaintext, err := authenticatedCipher.Open(
		nil,
		ciphertext[1:nonceEnd],
		ciphertext[nonceEnd:],
		authenticatedData(keyReference, additionalData),
	)
	if err != nil {
		zero(plaintext)
		return nil, ErrAuthentication
	}

	return plaintext, nil
}

func (provider *Provider) request(
	ctx context.Context,
	keyReference string,
	encryptionContext secretenvelope.Context,
) ([secretenvelope.DataKeySize]byte, []byte, error) {
	if provider == nil || provider.entropy == nil || len(provider.keys) == 0 {
		return [secretenvelope.DataKeySize]byte{}, nil, ErrInvalidKeyring
	}
	if ctx == nil || !validKeyReference(keyReference) {
		return [secretenvelope.DataKeySize]byte{}, nil, ErrInvalidRequest
	}
	if err := ctx.Err(); err != nil {
		return [secretenvelope.DataKeySize]byte{}, nil, err
	}
	additionalData := encryptionContext.AdditionalData()
	if len(additionalData) == 0 {
		return [secretenvelope.DataKeySize]byte{}, nil, ErrInvalidRequest
	}
	key, exists := provider.keys[keyReference]
	if !exists {
		return [secretenvelope.DataKeySize]byte{}, nil, ErrKeyNotFound
	}

	return key, additionalData, nil
}

func (provider *Provider) wrap(
	key [secretenvelope.DataKeySize]byte,
	keyReference string,
	plaintext []byte,
	additionalData []byte,
) ([]byte, error) {
	block, _ := aes.NewCipher(key[:])
	authenticatedCipher, _ := cipher.NewGCM(block)
	nonce := make([]byte, authenticatedCipher.NonceSize())
	if _, err := io.ReadFull(provider.entropy, nonce); err != nil {
		zero(nonce)
		return nil, ErrEntropy
	}
	wrapped := make([]byte, 1, 1+len(nonce)+len(plaintext)+
		authenticatedCipher.Overhead())
	wrapped[0] = wrappedDataKeyVersion
	wrapped = append(wrapped, nonce...)
	wrapped = authenticatedCipher.Seal(
		wrapped,
		nonce,
		plaintext,
		authenticatedData(keyReference, additionalData),
	)
	zero(nonce)

	return wrapped, nil
}

func authenticatedData(keyReference string, context []byte) []byte {
	encoded := make([]byte, 4, 4+len(keyReference)+len(context))
	binary.BigEndian.PutUint32(encoded, uint32(len(keyReference)))
	encoded = append(encoded, keyReference...)
	encoded = append(encoded, context...)

	return encoded
}

func validKeyReference(value string) bool {
	if value == "" || len(value) > maximumKeyReferenceSize ||
		strings.TrimSpace(value) != value || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}

	return true
}

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func (*Provider) String() string   { return redacted }
func (*Provider) GoString() string { return redacted }

// LogValue prevents wrapping keys from entering structured logs.
func (*Provider) LogValue() slog.Value {
	return slog.StringValue(redacted)
}

// MarshalJSON prevents wrapping keys from entering JSON output.
func (*Provider) MarshalJSON() ([]byte, error) {
	return json.Marshal(redacted)
}

var _ secretenvelope.KeyProvider = (*Provider)(nil)
