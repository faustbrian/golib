package secretenvelope

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
	"log/slog"
	"reflect"

	"encoding/json"
)

var (
	ErrServiceRequired = errors.New("secret envelope service is required")
	ErrKeyProvider     = errors.New("secret envelope key provider failed")
	ErrInvalidRequest  = errors.New("secret envelope request is invalid")
	ErrEntropy         = errors.New("secret envelope nonce generation failed")
	ErrAuthentication  = errors.New("secret envelope authentication failed")
)

// DataKey transfers ownership of wrapped and plaintext key bytes to Service.
// Its plaintext is not publicly accessible and Service zeroizes it before
// returning.
type DataKey struct {
	plaintext    []byte
	ciphertext   []byte
	keyReference string
}

func (DataKey) String() string   { return redacted }
func (DataKey) GoString() string { return redacted }

// LogValue prevents data-key material from entering slog.
func (DataKey) LogValue() slog.Value {
	return slog.StringValue(redacted)
}

// MarshalJSON prevents accidental JSON disclosure of data-key material.
func (DataKey) MarshalJSON() ([]byte, error) {
	return json.Marshal(redacted)
}

// NewDataKey validates and takes ownership of key-provider response bytes.
func NewDataKey(
	plaintext []byte,
	ciphertext []byte,
	keyReference string,
) (DataKey, error) {
	if len(plaintext) != DataKeySize ||
		len(ciphertext) == 0 ||
		len(ciphertext) > maxEncryptedDataKeySize ||
		!validKeyReference(keyReference) {
		return DataKey{}, ErrInvalidEnvelope
	}

	return DataKey{
		plaintext:    plaintext,
		ciphertext:   ciphertext,
		keyReference: keyReference,
	}, nil
}

// KeyReference identifies the wrapping key without exposing plaintext.
func (dataKey DataKey) KeyReference() string {
	return dataKey.keyReference
}

// EncryptedDataKey returns a caller-owned copy of the wrapped key.
func (dataKey DataKey) EncryptedDataKey() []byte {
	return append([]byte(nil), dataKey.ciphertext...)
}

// KeyProvider wraps and unwraps one-use AES-256 data keys.
type KeyProvider interface {
	GenerateDataKey(context.Context, string, Context) (DataKey, error)
	DecryptDataKey(context.Context, string, []byte, Context) ([]byte, error)
}

// EncryptRequest owns one plaintext payload and its non-secret binding context.
type EncryptRequest struct {
	Plaintext    []byte
	KeyReference string
	Context      Context
}

// DecryptRequest owns one envelope and its expected non-secret binding context.
type DecryptRequest struct {
	Envelope Envelope
	Context  Context
}

type serviceOptions struct {
	nonceReader io.Reader
}

// Option configures a Service without introducing ambient state.
type Option func(*serviceOptions) error

// WithNonceReader replaces crypto/rand for deterministic tests.
func WithNonceReader(reader io.Reader) Option {
	return func(options *serviceOptions) error {
		if nilLike(reader) {
			return ErrInvalidRequest
		}
		options.nonceReader = reader

		return nil
	}
}

// Service performs local AES-256-GCM operations around a key provider.
type Service struct {
	provider    KeyProvider
	nonceReader io.Reader
}

// NewService validates explicit key and entropy dependencies.
func NewService(provider KeyProvider, options ...Option) (*Service, error) {
	if nilLike(provider) {
		return nil, ErrServiceRequired
	}
	settings := serviceOptions{nonceReader: rand.Reader}
	for _, option := range options {
		if option == nil {
			return nil, ErrInvalidRequest
		}
		if err := option(&settings); err != nil {
			return nil, err
		}
	}

	return &Service{
		provider:    provider,
		nonceReader: settings.nonceReader,
	}, nil
}

// Encrypt wraps a fresh data key and authenticates the caller's context.
func (service *Service) Encrypt(
	ctx context.Context,
	request EncryptRequest,
) (Envelope, error) {
	if service == nil ||
		nilLike(service.provider) ||
		nilLike(service.nonceReader) {
		return Envelope{}, ErrServiceRequired
	}
	if ctx == nil ||
		len(request.Plaintext) == 0 ||
		len(request.Plaintext) > MaxPlaintextSize ||
		!validKeyReference(request.KeyReference) ||
		!request.Context.valid() {
		return Envelope{}, ErrInvalidRequest
	}

	dataKey, err := service.provider.GenerateDataKey(
		ctx,
		request.KeyReference,
		request.Context,
	)
	if err != nil {
		return Envelope{}, operationError{
			operation: "encrypt",
			kind:      ErrKeyProvider,
			cause:     err,
		}
	}
	defer zero(dataKey.plaintext)
	if len(dataKey.plaintext) != DataKeySize ||
		len(dataKey.ciphertext) == 0 ||
		len(dataKey.ciphertext) > maxEncryptedDataKeySize ||
		!validKeyReference(dataKey.keyReference) {
		return Envelope{}, ErrInvalidEnvelope
	}

	block, _ := aes.NewCipher(dataKey.plaintext)
	authenticatedCipher, _ := cipher.NewGCM(block)
	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(service.nonceReader, nonce); err != nil {
		zero(nonce)
		return Envelope{}, operationError{
			operation: "encrypt",
			kind:      ErrEntropy,
			cause:     err,
		}
	}

	return Envelope{
		keyReference:     dataKey.keyReference,
		encryptedDataKey: append([]byte(nil), dataKey.ciphertext...),
		nonce:            nonce,
		ciphertext: authenticatedCipher.Seal(
			nil,
			nonce,
			request.Plaintext,
			request.Context.additionalData,
		),
	}, nil
}

// Decrypt unwraps a data key and authenticates the expected context.
func (service *Service) Decrypt(
	ctx context.Context,
	request DecryptRequest,
) ([]byte, error) {
	if service == nil || nilLike(service.provider) {
		return nil, ErrServiceRequired
	}
	if ctx == nil || !request.Envelope.valid() || !request.Context.valid() {
		return nil, ErrInvalidRequest
	}

	plaintextKey, err := service.provider.DecryptDataKey(
		ctx,
		request.Envelope.keyReference,
		request.Envelope.EncryptedDataKey(),
		request.Context,
	)
	if err != nil {
		return nil, operationError{
			operation: "decrypt",
			kind:      ErrKeyProvider,
			cause:     err,
		}
	}
	defer zero(plaintextKey)
	if len(plaintextKey) != DataKeySize {
		return nil, ErrInvalidEnvelope
	}

	block, _ := aes.NewCipher(plaintextKey)
	authenticatedCipher, _ := cipher.NewGCM(block)
	plaintext, err := authenticatedCipher.Open(
		nil,
		request.Envelope.nonce,
		request.Envelope.ciphertext,
		request.Context.additionalData,
	)
	if err != nil {
		return nil, ErrAuthentication
	}

	return plaintext, nil
}

type operationError struct {
	operation string
	kind      error
	cause     error
}

func (err operationError) Error() string {
	return "secret envelope " + err.operation + " failed"
}

func (err operationError) Unwrap() []error {
	return []error{err.kind, err.cause}
}

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func nilLike(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
