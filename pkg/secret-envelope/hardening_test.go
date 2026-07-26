package secretenvelope

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestContextRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tooMany := make(map[string]string, maxContextEntries+1)
	for index := range maxContextEntries + 1 {
		tooMany[fmt.Sprintf("key-%d", index)] = "value"
	}
	tooLarge := make(map[string]string, 20)
	for index := range 20 {
		tooLarge[fmt.Sprintf("key-%d", index)] = strings.Repeat(
			"value",
			maxContextValueSize/5,
		)
	}
	for name, values := range map[string]map[string]string{
		"empty":              {},
		"too-many":           tooMany,
		"empty-key":          {"": "value"},
		"long-key":           {strings.Repeat("k", maxContextKeySize+1): "value"},
		"long-value":         {"key": strings.Repeat("v", maxContextValueSize+1)},
		"invalid-key-utf8":   {"\xff": "value"},
		"invalid-value-utf8": {"key": "\xff"},
		"large-canonical":    tooLarge,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := NewContext(values); !errors.Is(err, ErrInvalidContext) {
				t.Fatalf("NewContext() error = %v, want ErrInvalidContext", err)
			}
		})
	}
}

func TestEnvelopeRepresentationsAreRedactedAndCallerOwned(t *testing.T) {
	t.Parallel()

	envelope := validTestEnvelope()
	ciphertext := envelope.Ciphertext()
	encryptedDataKey := envelope.EncryptedDataKey()
	ciphertext[0] ^= 0xff
	encryptedDataKey[0] ^= 0xff
	if bytes.Equal(ciphertext, envelope.ciphertext) ||
		bytes.Equal(encryptedDataKey, envelope.encryptedDataKey) {
		t.Fatal("envelope exposed mutable internal bytes")
	}
	if envelope.String() != redacted ||
		envelope.GoString() != redacted ||
		envelope.LogValue().String() != redacted {
		t.Fatal("envelope text representation was not redacted")
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if string(encoded) != `"[REDACTED]"` {
		t.Fatalf("JSON envelope = %s", encoded)
	}
	dataKey := DataKey{plaintext: []byte("secret")}
	if dataKey.String() != redacted ||
		dataKey.GoString() != redacted ||
		dataKey.LogValue().String() != redacted {
		t.Fatal("data key text representation was not redacted")
	}
	encodedDataKey, err := json.Marshal(dataKey)
	if err != nil {
		t.Fatalf("json.Marshal(DataKey) error = %v", err)
	}
	if string(encodedDataKey) != `"[REDACTED]"` {
		t.Fatalf("JSON data key = %s", encodedDataKey)
	}
	dataKey, err = NewDataKey(
		bytes.Repeat([]byte{0x42}, DataKeySize),
		[]byte("wrapped"),
		"alias/location-contracts",
	)
	if err != nil {
		t.Fatalf("NewDataKey() error = %v", err)
	}
	wrapped := dataKey.EncryptedDataKey()
	wrapped[0] ^= 0xff
	if dataKey.KeyReference() != "alias/location-contracts" ||
		bytes.Equal(wrapped, dataKey.ciphertext) {
		t.Fatal("data key metadata is not immutable")
	}
}

func TestEnvelopeRejectsInvalidFieldsAndEncodings(t *testing.T) {
	t.Parallel()

	if _, err := (Envelope{}).MarshalBinary(); !errors.Is(
		err,
		ErrInvalidEnvelope,
	) {
		t.Fatalf("zero MarshalBinary() error = %v", err)
	}

	valid := validTestEnvelope()
	encoded, err := valid.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	cases := map[string][]byte{
		"version":     replaceByte(encoded, 4, 0xff),
		"algorithm":   replaceByte(encoded, 5, 0xff),
		"key-size":    replaceByte(encoded, 7, 0xff),
		"key":         replaceByte(encoded, envelopeHeaderSize, 0x00),
		"nonce-size":  replaceByte(encoded, 12, 0xff),
		"cipher-size": replaceByte(encoded, 16, 0xff),
	}
	for name, candidate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := ParseEnvelope(candidate); !errors.Is(
				err,
				ErrInvalidEnvelope,
			) {
				t.Fatalf("ParseEnvelope() error = %v", err)
			}
		})
	}

	for name, keyReference := range map[string]string{
		"empty":        "",
		"long":         strings.Repeat("k", maxKeyReferenceSize+1),
		"invalid-utf8": "\xff",
		"control":      "alias/location\ncontracts",
	} {
		t.Run("key-"+name, func(t *testing.T) {
			t.Parallel()

			candidate := valid
			candidate.keyReference = keyReference
			if _, err := candidate.MarshalBinary(); !errors.Is(
				err,
				ErrInvalidEnvelope,
			) {
				t.Fatalf("MarshalBinary() error = %v", err)
			}
		})
	}
	for name, mutate := range map[string]func(*Envelope){
		"empty-data-key": func(candidate *Envelope) {
			candidate.encryptedDataKey = nil
		},
		"large-data-key": func(candidate *Envelope) {
			candidate.encryptedDataKey = bytes.Repeat(
				[]byte{0x42},
				maxEncryptedDataKeySize+1,
			)
		},
		"nonce": func(candidate *Envelope) {
			candidate.nonce = nil
		},
		"short-ciphertext": func(candidate *Envelope) {
			candidate.ciphertext = []byte("short")
		},
		"large-ciphertext": func(candidate *Envelope) {
			candidate.ciphertext = bytes.Repeat(
				[]byte{0x42},
				MaxPlaintextSize+17,
			)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			candidate := valid
			mutate(&candidate)
			if _, err := candidate.MarshalBinary(); !errors.Is(
				err,
				ErrInvalidEnvelope,
			) {
				t.Fatalf("MarshalBinary() error = %v", err)
			}
		})
	}
}

func TestServiceRejectsInvalidConstructionAndRequests(t *testing.T) {
	t.Parallel()

	var typedNilProvider *recordingProvider
	for name, construct := range map[string]func() error{
		"nil-provider": func() error {
			_, err := NewService(nil)
			return err
		},
		"typed-nil-provider": func() error {
			_, err := NewService(typedNilProvider)
			return err
		},
		"nil-option": func() error {
			_, err := NewService(&recordingProvider{}, nil)
			return err
		},
		"nil-reader": func() error {
			_, err := NewService(
				&recordingProvider{},
				WithNonceReader(nil),
			)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := construct(); err == nil {
				t.Fatal("construction error = nil")
			}
		})
	}

	encryptionContext, _ := NewContext(map[string]string{"service": "location"})
	provider := &recordingProvider{
		plaintextKey: bytes.Repeat([]byte{0x42}, DataKeySize),
		encryptedKey: []byte("wrapped-data-key"),
		resolvedKey:  "alias/location-contracts",
	}
	service, _ := NewService(provider)
	for name, request := range map[string]EncryptRequest{
		"empty-plaintext": {
			KeyReference: "alias/location-contracts",
			Context:      encryptionContext,
		},
		"large-plaintext": {
			Plaintext:    bytes.Repeat([]byte{0x42}, MaxPlaintextSize+1),
			KeyReference: "alias/location-contracts",
			Context:      encryptionContext,
		},
		"empty-key": {
			Plaintext: []byte("secret"),
			Context:   encryptionContext,
		},
		"empty-context": {
			Plaintext:    []byte("secret"),
			KeyReference: "alias/location-contracts",
		},
	} {
		t.Run("encrypt-"+name, func(t *testing.T) {
			t.Parallel()

			if _, err := service.Encrypt(
				context.Background(),
				request,
			); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("Encrypt() error = %v, want ErrInvalidRequest", err)
			}
		})
	}
	//lint:ignore SA1012 Verifies explicit rejection of a nil context.
	if _, err := service.Encrypt(nil, EncryptRequest{ //nolint:staticcheck // Explicit nil rejection.
		Plaintext:    []byte("secret"),
		KeyReference: "alias/location-contracts",
		Context:      encryptionContext,
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Encrypt(nil) error = %v", err)
	}
	var nilService *Service
	if _, err := nilService.Encrypt(
		context.Background(),
		EncryptRequest{},
	); !errors.Is(err, ErrServiceRequired) {
		t.Fatalf("nil Encrypt() error = %v", err)
	}
}

func TestServiceRejectsInvalidKeyMaterialAndEntropy(t *testing.T) {
	t.Parallel()

	encryptionContext, _ := NewContext(map[string]string{"service": "location"})
	validRequest := EncryptRequest{
		Plaintext:    []byte("secret"),
		KeyReference: "alias/location-contracts",
		Context:      encryptionContext,
	}
	for name, provider := range map[string]*recordingProvider{
		"key-size": {
			rawDataKey: &DataKey{
				plaintext:    []byte("short"),
				ciphertext:   []byte("wrapped"),
				keyReference: "alias/location-contracts",
			},
		},
		"empty-wrapped": {
			rawDataKey: &DataKey{
				plaintext:    bytes.Repeat([]byte{0x42}, DataKeySize),
				keyReference: "alias/location-contracts",
			},
		},
		"large-wrapped": {
			rawDataKey: &DataKey{
				plaintext: bytes.Repeat([]byte{0x42}, DataKeySize),
				ciphertext: bytes.Repeat(
					[]byte{0x42},
					maxEncryptedDataKeySize+1,
				),
				keyReference: "alias/location-contracts",
			},
		},
		"empty-resolved-key": {
			rawDataKey: &DataKey{
				plaintext:  bytes.Repeat([]byte{0x42}, DataKeySize),
				ciphertext: []byte("wrapped"),
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			service, _ := NewService(provider)
			if _, err := service.Encrypt(
				context.Background(),
				validRequest,
			); !errors.Is(err, ErrInvalidEnvelope) {
				t.Fatalf("Encrypt() error = %v, want ErrInvalidEnvelope", err)
			}
		})
	}

	service, _ := NewService(
		&recordingProvider{
			plaintextKey: bytes.Repeat([]byte{0x42}, DataKeySize),
			encryptedKey: []byte("wrapped"),
			resolvedKey:  "alias/location-contracts",
		},
		WithNonceReader(errorReader{err: io.ErrUnexpectedEOF}),
	)
	if _, err := service.Encrypt(
		context.Background(),
		validRequest,
	); !errors.Is(err, ErrEntropy) {
		t.Fatalf("Encrypt() error = %v, want ErrEntropy", err)
	}
}

func TestNewDataKeyRejectsInvalidMaterial(t *testing.T) {
	t.Parallel()

	for name, input := range map[string]struct {
		plaintext    []byte
		ciphertext   []byte
		keyReference string
	}{
		"plaintext": {
			plaintext:    []byte("short"),
			ciphertext:   []byte("wrapped"),
			keyReference: "alias/location",
		},
		"ciphertext": {
			plaintext:    bytes.Repeat([]byte{0x42}, DataKeySize),
			keyReference: "alias/location",
		},
		"large-ciphertext": {
			plaintext: bytes.Repeat([]byte{0x42}, DataKeySize),
			ciphertext: bytes.Repeat(
				[]byte{0x42},
				maxEncryptedDataKeySize+1,
			),
			keyReference: "alias/location",
		},
		"key-reference": {
			plaintext:  bytes.Repeat([]byte{0x42}, DataKeySize),
			ciphertext: []byte("wrapped"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := NewDataKey(
				input.plaintext,
				input.ciphertext,
				input.keyReference,
			); !errors.Is(err, ErrInvalidEnvelope) {
				t.Fatalf("NewDataKey() error = %v", err)
			}
		})
	}
}

func TestServiceRejectsInvalidDecryptRequestsAndKeys(t *testing.T) {
	t.Parallel()

	encryptionContext, _ := NewContext(map[string]string{"service": "location"})
	validEnvelope := validTestEnvelope()
	service, _ := NewService(failingProvider{err: errors.New("cause")})
	for name, request := range map[string]DecryptRequest{
		"envelope": {Context: encryptionContext},
		"context":  {Envelope: validEnvelope},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := service.Decrypt(
				context.Background(),
				request,
			); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("Decrypt() error = %v, want ErrInvalidRequest", err)
			}
		})
	}
	//lint:ignore SA1012 Verifies explicit rejection of a nil context.
	if _, err := service.Decrypt(nil, DecryptRequest{ //nolint:staticcheck // Explicit nil rejection.
		Envelope: validEnvelope,
		Context:  encryptionContext,
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Decrypt(nil) error = %v", err)
	}
	if _, err := service.Decrypt(context.Background(), DecryptRequest{
		Envelope: validEnvelope,
		Context:  encryptionContext,
	}); !errors.Is(err, ErrKeyProvider) {
		t.Fatalf("Decrypt() error = %v, want ErrKeyProvider", err)
	}
	var nilService *Service
	if _, err := nilService.Decrypt(
		context.Background(),
		DecryptRequest{},
	); !errors.Is(err, ErrServiceRequired) {
		t.Fatalf("nil Decrypt() error = %v", err)
	}

	shortKeyProvider := &recordingProvider{plaintextKey: []byte("short")}
	service, _ = NewService(shortKeyProvider)
	if _, err := service.Decrypt(context.Background(), DecryptRequest{
		Envelope: validEnvelope,
		Context:  encryptionContext,
	}); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("Decrypt() error = %v, want ErrInvalidEnvelope", err)
	}
}

func validTestEnvelope() Envelope {
	return Envelope{
		keyReference:     "alias/location-contracts",
		encryptedDataKey: []byte("wrapped-data-key"),
		nonce:            bytes.Repeat([]byte{0x24}, NonceSize),
		ciphertext:       bytes.Repeat([]byte{0x42}, 32),
	}
}

func replaceByte(source []byte, offset int, replacement byte) []byte {
	cloned := append([]byte(nil), source...)
	cloned[offset] = replacement

	return cloned
}

type errorReader struct {
	err error
}

func (reader errorReader) Read([]byte) (int, error) {
	return 0, reader.err
}
