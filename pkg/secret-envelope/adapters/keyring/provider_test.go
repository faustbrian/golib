package keyring

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	secretenvelope "github.com/faustbrian/golib/pkg/secret-envelope"
)

func TestProviderWrapsAndUnwrapsDataKeysByReferenceAndContext(
	t *testing.T,
) {
	t.Parallel()

	provider, err := New(map[string][]byte{
		"location-metadata-v1": bytes.Repeat([]byte{1}, 32),
		"location-metadata-v2": bytes.Repeat([]byte{2}, 32),
	})
	if err != nil {
		t.Fatalf("construct keyring provider: %v", err)
	}
	encryptionContext, err := secretenvelope.NewContext(map[string]string{
		"owner_id": "owner-1",
		"purpose":  "vendor-metadata",
	})
	if err != nil {
		t.Fatalf("construct encryption context: %v", err)
	}
	dataKey, err := provider.GenerateDataKey(
		context.Background(),
		"location-metadata-v1",
		encryptionContext,
	)
	if err != nil {
		t.Fatalf("generate data key: %v", err)
	}
	plaintext := dataKeyPlaintext(t, provider, dataKey, encryptionContext)
	if len(plaintext) != secretenvelope.DataKeySize ||
		dataKey.KeyReference() != "location-metadata-v1" ||
		bytes.Contains(dataKey.EncryptedDataKey(), plaintext) {
		t.Fatal("data key was not wrapped under its requested reference")
	}

	rotated, err := New(map[string][]byte{
		"location-metadata-v1": bytes.Repeat([]byte{1}, 32),
		"location-metadata-v2": bytes.Repeat([]byte{3}, 32),
	})
	if err != nil {
		t.Fatalf("construct rotated keyring provider: %v", err)
	}
	decrypted, err := rotated.DecryptDataKey(
		context.Background(),
		dataKey.KeyReference(),
		dataKey.EncryptedDataKey(),
		encryptionContext,
	)
	if err != nil {
		t.Fatalf("decrypt with retained wrapping key: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatal("retained wrapping key did not decrypt historical data key")
	}
}

func TestProviderRejectsInvalidKeyrings(t *testing.T) {
	t.Parallel()

	tooMany := make(map[string][]byte, maximumKeys+1)
	for index := range maximumKeys + 1 {
		tooMany[fmt.Sprintf("key-%d", index)] = bytes.Repeat([]byte{1}, 32)
	}
	for name, keys := range map[string]map[string][]byte{
		"empty":             {},
		"too-many":          tooMany,
		"empty-reference":   {"": bytes.Repeat([]byte{1}, 32)},
		"spaced-reference":  {" key": bytes.Repeat([]byte{1}, 32)},
		"control-reference": {"key\nvalue": bytes.Repeat([]byte{1}, 32)},
		"invalid-utf8-reference": {
			string([]byte{0xff}): bytes.Repeat([]byte{1}, 32),
		},
		"long-reference": {
			strings.Repeat("k", maximumKeyReferenceSize+1): bytes.Repeat([]byte{1}, 32),
		},
		"short-key": {"key": bytes.Repeat([]byte{1}, 31)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := New(keys); !errors.Is(err, ErrInvalidKeyring) {
				t.Fatalf("New() error = %v, want invalid keyring", err)
			}
		})
	}
}

func TestProviderAcceptsDocumentedKeyringBounds(t *testing.T) {
	t.Parallel()

	keys := make(map[string][]byte, maximumKeys)
	for index := range maximumKeys {
		keys[fmt.Sprintf("key-%d", index)] = bytes.Repeat([]byte{1}, 32)
	}
	keys[strings.Repeat("k", maximumKeyReferenceSize)] = keys["key-0"]
	delete(keys, "key-0")

	if _, err := New(keys); err != nil {
		t.Fatalf("New() at documented bounds returned error: %v", err)
	}
}

func TestProviderRejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	provider := newProvider(t)
	validContext := newContext(t, "owner-1")
	var nilProvider *Provider
	invalidEntropy := *provider
	invalidEntropy.entropy = nil
	invalidKeys := *provider
	invalidKeys.keys = nil

	for name, testCase := range map[string]struct {
		provider          *Provider
		ctx               context.Context
		reference         string
		encryptionContext secretenvelope.Context
		want              error
	}{
		"nil-provider": {
			nilProvider, context.Background(), "key-v1", validContext,
			ErrInvalidKeyring,
		},
		"nil-entropy": {
			&invalidEntropy, context.Background(), "key-v1", validContext,
			ErrInvalidKeyring,
		},
		"empty-keyring": {
			&invalidKeys, context.Background(), "key-v1", validContext,
			ErrInvalidKeyring,
		},
		"nil-context": {
			provider, nil, "key-v1", validContext, ErrInvalidRequest,
		},
		"invalid-reference": {
			provider, context.Background(), " key-v1", validContext,
			ErrInvalidRequest,
		},
		"empty-encryption-context": {
			provider, context.Background(), "key-v1", secretenvelope.Context{},
			ErrInvalidRequest,
		},
		"missing-key": {
			provider, context.Background(), "missing", validContext,
			ErrKeyNotFound,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := testCase.provider.GenerateDataKey(
				testCase.ctx,
				testCase.reference,
				testCase.encryptionContext,
			)
			if !errors.Is(err, testCase.want) {
				t.Fatalf(
					"GenerateDataKey() error = %v, want %v",
					err,
					testCase.want,
				)
			}
		})
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.GenerateDataKey(
		canceled,
		"key-v1",
		validContext,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request error = %v, want context canceled", err)
	}
	if _, err := provider.DecryptDataKey(
		context.Background(),
		"missing",
		[]byte("ciphertext"),
		validContext,
	); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("decrypt missing key error = %v, want key not found", err)
	}
}

func TestProviderRejectsEntropyFailures(t *testing.T) {
	t.Parallel()

	validContext := newContext(t, "owner-1")
	provider := newProvider(t)
	provider.entropy = errorReader{}
	if _, err := provider.GenerateDataKey(
		context.Background(),
		"key-v1",
		validContext,
	); !errors.Is(err, ErrEntropy) {
		t.Fatalf("data-key entropy error = %v, want entropy failure", err)
	}

	provider = newProvider(t)
	provider.entropy = io.MultiReader(
		bytes.NewReader(bytes.Repeat([]byte{1}, 32)),
		errorReader{},
	)
	if _, err := provider.GenerateDataKey(
		context.Background(),
		"key-v1",
		validContext,
	); !errors.Is(err, ErrEntropy) {
		t.Fatalf("nonce entropy error = %v, want entropy failure", err)
	}
}

func TestProviderAuthenticatesReferenceContextAndCiphertext(t *testing.T) {
	t.Parallel()

	provider := newProvider(t)
	validContext := newContext(t, "owner-1")
	dataKey, err := provider.GenerateDataKey(
		context.Background(),
		"key-v1",
		validContext,
	)
	if err != nil {
		t.Fatalf("generate data key: %v", err)
	}
	validCiphertext := dataKey.EncryptedDataKey()
	wrongVersion := append([]byte(nil), validCiphertext...)
	wrongVersion[0]++
	tampered := append([]byte(nil), validCiphertext...)
	tampered[len(tampered)-1] ^= 1

	for name, testCase := range map[string]struct {
		reference         string
		ciphertext        []byte
		encryptionContext secretenvelope.Context
	}{
		"empty-ciphertext": {"key-v1", nil, validContext},
		"wrong-version":    {"key-v1", wrongVersion, validContext},
		"tampered":         {"key-v1", tampered, validContext},
		"wrong-context": {
			"key-v1", validCiphertext, newContext(t, "owner-2"),
		},
		"wrong-key": {"key-v2", validCiphertext, validContext},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := provider.DecryptDataKey(
				context.Background(),
				testCase.reference,
				testCase.ciphertext,
				testCase.encryptionContext,
			)
			if !errors.Is(err, ErrAuthentication) {
				t.Fatalf("DecryptDataKey() error = %v, want authentication", err)
			}
		})
	}
}

func TestProviderRedactsWrappingKeys(t *testing.T) {
	t.Parallel()

	provider := newProvider(t)
	logValue := provider.LogValue()
	if fmt.Sprint(provider) != redacted ||
		fmt.Sprintf("%#v", provider) != redacted ||
		logValue.Kind() != slog.KindString || logValue.String() != redacted {
		t.Fatal("provider text representation was not redacted")
	}
	encoded, err := json.Marshal(provider)
	if err != nil {
		t.Fatalf("marshal provider: %v", err)
	}
	if string(encoded) != `"[REDACTED]"` {
		t.Fatalf("provider JSON = %s, want redaction", encoded)
	}
}

func TestProviderUsesExactBoundedWireAllocations(t *testing.T) {
	provider := newProvider(t)
	provider.entropy = zeroReader{}
	plaintext := bytes.Repeat([]byte{2}, secretenvelope.DataKeySize)
	additionalData := []byte("bounded-context")
	key := provider.keys["key-v1"]

	wrapped, err := provider.wrap(
		key,
		"key-v1",
		plaintext,
		additionalData,
	)
	if err != nil {
		t.Fatalf("wrap data key: %v", err)
	}
	if cap(wrapped) != len(wrapped) {
		t.Fatalf("wrapped key capacity = %d, want exact size %d", cap(wrapped), len(wrapped))
	}
	allocations := testing.AllocsPerRun(100, func() {
		candidate, wrapErr := provider.wrap(
			key,
			"key-v1",
			plaintext,
			additionalData,
		)
		if wrapErr != nil || len(candidate) != len(wrapped) {
			t.Fatal("repeated wrapping did not preserve the bounded wire contract")
		}
	})
	if allocations > 5 {
		t.Fatalf("wrap allocations = %.0f, want at most 5", allocations)
	}

	authenticated := authenticatedData("key-v1", additionalData)
	if cap(authenticated) != len(authenticated) {
		t.Fatalf(
			"authenticated data capacity = %d, want exact size %d",
			cap(authenticated),
			len(authenticated),
		)
	}
}

func dataKeyPlaintext(
	t *testing.T,
	provider *Provider,
	dataKey secretenvelope.DataKey,
	encryptionContext secretenvelope.Context,
) []byte {
	t.Helper()

	plaintext, err := provider.DecryptDataKey(
		context.Background(),
		dataKey.KeyReference(),
		dataKey.EncryptedDataKey(),
		encryptionContext,
	)
	if err != nil {
		t.Fatalf("decrypt data key: %v", err)
	}

	return plaintext
}

func newProvider(t *testing.T) *Provider {
	t.Helper()

	provider, err := New(map[string][]byte{
		"key-v1": bytes.Repeat([]byte{1}, 32),
		"key-v2": bytes.Repeat([]byte{2}, 32),
	})
	if err != nil {
		t.Fatalf("construct provider: %v", err)
	}

	return provider
}

func newContext(t *testing.T, owner string) secretenvelope.Context {
	t.Helper()

	encryptionContext, err := secretenvelope.NewContext(map[string]string{
		"owner_id": owner,
		"purpose":  "vendor-metadata",
	})
	if err != nil {
		t.Fatalf("construct encryption context: %v", err)
	}

	return encryptionContext
}

type errorReader struct{}

type zeroReader struct{}

func (zeroReader) Read(value []byte) (int, error) {
	clear(value)

	return len(value), nil
}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}
