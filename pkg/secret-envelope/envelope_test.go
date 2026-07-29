package secretenvelope

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestServiceEncryptsAndDecryptsAnAuthenticatedEnvelope(t *testing.T) {
	t.Parallel()

	provider := &recordingProvider{
		plaintextKey: bytes.Repeat([]byte{0x42}, DataKeySize),
		encryptedKey: []byte("wrapped-data-key"),
		resolvedKey:  "arn:aws:kms:eu-north-1:123456789012:key/example",
	}
	service, err := NewService(
		provider,
		WithNonceReader(bytes.NewReader(bytes.Repeat([]byte{0x24}, NonceSize))),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	encryptionContext, err := NewContext(map[string]string{
		"service":   "location",
		"source_id": "01K00000000000000000000000",
	})
	if err != nil {
		t.Fatalf("NewContext() error = %v", err)
	}
	plaintext := []byte(`{"oauth":{"access_token":"sensitive"}}`)

	envelope, err := service.Encrypt(context.Background(), EncryptRequest{
		Plaintext:    plaintext,
		KeyReference: "alias/location-contracts",
		Context:      encryptionContext,
	})
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if envelope.KeyReference() != provider.resolvedKey {
		t.Fatalf(
			"KeyReference() = %q, want %q",
			envelope.KeyReference(),
			provider.resolvedKey,
		)
	}
	if bytes.Contains(envelope.Ciphertext(), plaintext) {
		t.Fatal("ciphertext contains plaintext")
	}
	if !allZero(provider.generatedPlaintextKey) {
		t.Fatal("generated plaintext data key was not zeroized")
	}

	encoded, err := envelope.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	parsed, err := ParseEnvelope(encoded)
	if err != nil {
		t.Fatalf("ParseEnvelope() error = %v", err)
	}
	actual, err := service.Decrypt(context.Background(), DecryptRequest{
		Envelope: parsed,
		Context:  encryptionContext,
	})
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if !bytes.Equal(actual, plaintext) {
		t.Fatalf("Decrypt() = %q, want %q", actual, plaintext)
	}
	if !allZero(provider.decryptedPlaintextKey) {
		t.Fatal("decrypted plaintext data key was not zeroized")
	}
}

func TestServiceRoundTripsFourMiBPayload(t *testing.T) {
	t.Parallel()

	provider := &recordingProvider{
		plaintextKey: bytes.Repeat([]byte{0x42}, DataKeySize),
		encryptedKey: []byte("wrapped-data-key"),
		resolvedKey:  "alias/postal-reconciliation",
	}
	service, err := NewService(
		provider,
		WithNonceReader(bytes.NewReader(bytes.Repeat([]byte{0x24}, NonceSize))),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	encryptionContext, err := NewContext(map[string]string{
		"service": "postal",
		"record":  "reconciliation-report",
	})
	if err != nil {
		t.Fatalf("NewContext() error = %v", err)
	}
	plaintext := bytes.Repeat([]byte{0x42}, 4<<20)

	envelope, err := service.Encrypt(context.Background(), EncryptRequest{
		Plaintext: plaintext, KeyReference: provider.resolvedKey,
		Context: encryptionContext,
	})
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	encoded, err := envelope.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	parsed, err := ParseEnvelope(encoded)
	if err != nil {
		t.Fatalf("ParseEnvelope() error = %v", err)
	}
	actual, err := service.Decrypt(context.Background(), DecryptRequest{
		Envelope: parsed, Context: encryptionContext,
	})
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if !bytes.Equal(actual, plaintext) {
		t.Fatal("Decrypt() changed maximum-sized plaintext")
	}
}

func TestServiceRejectsAContextSwap(t *testing.T) {
	t.Parallel()

	provider := &recordingProvider{
		plaintextKey: bytes.Repeat([]byte{0x42}, DataKeySize),
		encryptedKey: []byte("wrapped-data-key"),
		resolvedKey:  "alias/location-contracts",
	}
	service, err := NewService(
		provider,
		WithNonceReader(bytes.NewReader(bytes.Repeat([]byte{0x24}, NonceSize))),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	original, _ := NewContext(map[string]string{"source_id": "source-a"})
	swapped, _ := NewContext(map[string]string{"source_id": "source-b"})
	envelope, err := service.Encrypt(context.Background(), EncryptRequest{
		Plaintext:    []byte("sensitive"),
		KeyReference: "alias/location-contracts",
		Context:      original,
	})
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	_, err = service.Decrypt(context.Background(), DecryptRequest{
		Envelope: envelope,
		Context:  swapped,
	})
	if !errors.Is(err, ErrAuthentication) {
		t.Fatalf("Decrypt() error = %v, want ErrAuthentication", err)
	}
}

func TestServiceErrorsDoNotRenderSecretsOrProviderCauses(t *testing.T) {
	t.Parallel()

	const secret = "provider-secret-cause"
	service, err := NewService(failingProvider{err: errors.New(secret)})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	encryptionContext, _ := NewContext(map[string]string{"source_id": "source-a"})

	_, err = service.Encrypt(context.Background(), EncryptRequest{
		Plaintext:    []byte("plaintext-secret"),
		KeyReference: "alias/location-contracts",
		Context:      encryptionContext,
	})
	if err == nil {
		t.Fatal("Encrypt() error = nil, want error")
	}
	rendered := err.Error()
	if strings.Contains(rendered, secret) ||
		strings.Contains(rendered, "plaintext-secret") {
		t.Fatalf("error exposed secret material: %q", rendered)
	}
	if !errors.Is(err, ErrKeyProvider) {
		t.Fatalf("Encrypt() error = %v, want ErrKeyProvider", err)
	}
}

func TestContextIsCanonicalAndImmutable(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"source_id": "source-a",
		"service":   "location",
	}
	encryptionContext, err := NewContext(values)
	if err != nil {
		t.Fatalf("NewContext() error = %v", err)
	}
	first := encryptionContext.AdditionalData()
	values["source_id"] = "changed"
	cloned := encryptionContext.Values()
	cloned["service"] = "changed"
	second := encryptionContext.AdditionalData()

	if !bytes.Equal(first, second) {
		t.Fatal("context changed through a caller-owned map")
	}
	if string(first) != "\x00\x00\x00\x07service\x00\x00\x00\x08location"+
		"\x00\x00\x00\x09source_id\x00\x00\x00\x08source-a" {
		t.Fatalf("canonical context = %q", first)
	}
}

func TestEnvelopeParsingRejectsMalformedOrOversizedInput(t *testing.T) {
	t.Parallel()

	for _, encoded := range [][]byte{
		nil,
		[]byte("not-an-envelope"),
		append([]byte(envelopeMagic), 0xff),
		bytes.Repeat([]byte{0x42}, MaxEnvelopeSize+1),
	} {
		if _, err := ParseEnvelope(encoded); !errors.Is(err, ErrInvalidEnvelope) {
			t.Fatalf("ParseEnvelope(%x) error = %v, want ErrInvalidEnvelope", encoded, err)
		}
	}
}

type recordingProvider struct {
	plaintextKey          []byte
	encryptedKey          []byte
	resolvedKey           string
	generatedPlaintextKey []byte
	decryptedPlaintextKey []byte
	rawDataKey            *DataKey
}

func (provider *recordingProvider) GenerateDataKey(
	context.Context,
	string,
	Context,
) (DataKey, error) {
	if provider.rawDataKey != nil {
		return *provider.rawDataKey, nil
	}
	provider.generatedPlaintextKey = append([]byte(nil), provider.plaintextKey...)

	return NewDataKey(
		provider.generatedPlaintextKey,
		append([]byte(nil), provider.encryptedKey...),
		provider.resolvedKey,
	)
}

func (provider *recordingProvider) DecryptDataKey(
	context.Context,
	string,
	[]byte,
	Context,
) ([]byte, error) {
	provider.decryptedPlaintextKey = append([]byte(nil), provider.plaintextKey...)

	return provider.decryptedPlaintextKey, nil
}

type failingProvider struct {
	err error
}

func (provider failingProvider) GenerateDataKey(
	context.Context,
	string,
	Context,
) (DataKey, error) {
	return DataKey{}, provider.err
}

func (provider failingProvider) DecryptDataKey(
	context.Context,
	string,
	[]byte,
	Context,
) ([]byte, error) {
	return nil, provider.err
}

func allZero(value []byte) bool {
	for _, item := range value {
		if item != 0 {
			return false
		}
	}

	return true
}
