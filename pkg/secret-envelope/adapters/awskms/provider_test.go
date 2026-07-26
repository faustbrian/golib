package awskms

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
	secretenvelope "github.com/faustbrian/golib/pkg/secret-envelope"
)

func TestProviderGeneratesAnAES256DataKey(t *testing.T) {
	t.Parallel()

	const resolvedKey = "arn:aws:kms:eu-north-1:123456789012:key/example"
	client := &recordingClient{
		generateOutput: &kms.GenerateDataKeyOutput{
			Plaintext:      bytes.Repeat([]byte{0x42}, secretenvelope.DataKeySize),
			CiphertextBlob: []byte("wrapped-key"),
			KeyId:          aws.String(resolvedKey),
		},
	}
	provider, err := New(client)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	encryptionContext, _ := secretenvelope.NewContext(map[string]string{
		"service":   "location",
		"source_id": "source-a",
	})

	dataKey, err := provider.GenerateDataKey(
		context.Background(),
		"alias/location-contracts",
		encryptionContext,
	)
	if err != nil {
		t.Fatalf("GenerateDataKey() error = %v", err)
	}
	if client.generateInput == nil ||
		aws.ToString(client.generateInput.KeyId) != "alias/location-contracts" ||
		client.generateInput.KeySpec != types.DataKeySpecAes256 {
		t.Fatalf("GenerateDataKey input = %#v", client.generateInput)
	}
	if client.generateInput.EncryptionContext["source_id"] != "source-a" {
		t.Fatalf(
			"encryption context = %#v",
			client.generateInput.EncryptionContext,
		)
	}
	if dataKey.KeyReference() != resolvedKey ||
		!bytes.Equal(dataKey.EncryptedDataKey(), []byte("wrapped-key")) {
		t.Fatalf("data key metadata does not match KMS output")
	}
}

func TestProviderDecryptsWithTheExactKeyAndContext(t *testing.T) {
	t.Parallel()

	const resolvedKey = "arn:aws:kms:eu-north-1:123456789012:key/example"
	client := &recordingClient{
		decryptOutput: &kms.DecryptOutput{
			Plaintext: bytes.Repeat([]byte{0x42}, secretenvelope.DataKeySize),
			KeyId:     aws.String(resolvedKey),
			EncryptionAlgorithm: types.
				EncryptionAlgorithmSpecSymmetricDefault,
		},
	}
	provider, err := New(client)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	encryptionContext, _ := secretenvelope.NewContext(map[string]string{
		"service":   "location",
		"source_id": "source-a",
	})

	plaintext, err := provider.DecryptDataKey(
		context.Background(),
		resolvedKey,
		[]byte("wrapped-key"),
		encryptionContext,
	)
	if err != nil {
		t.Fatalf("DecryptDataKey() error = %v", err)
	}
	if client.decryptInput == nil ||
		aws.ToString(client.decryptInput.KeyId) != resolvedKey ||
		client.decryptInput.EncryptionAlgorithm !=
			types.EncryptionAlgorithmSpecSymmetricDefault ||
		!bytes.Equal(client.decryptInput.CiphertextBlob, []byte("wrapped-key")) {
		t.Fatalf("Decrypt input = %#v", client.decryptInput)
	}
	if client.decryptInput.EncryptionContext["source_id"] != "source-a" {
		t.Fatalf(
			"encryption context = %#v",
			client.decryptInput.EncryptionContext,
		)
	}
	if len(plaintext) != secretenvelope.DataKeySize {
		t.Fatalf("plaintext key size = %d", len(plaintext))
	}
}

func TestProviderRejectsMalformedResponsesWithoutRenderingClientErrors(
	t *testing.T,
) {
	t.Parallel()

	const secret = "kms-client-secret"
	provider, err := New(&recordingClient{
		generateErr: errors.New(secret),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	encryptionContext, _ := secretenvelope.NewContext(map[string]string{
		"service": "location",
	})

	_, err = provider.GenerateDataKey(
		context.Background(),
		"alias/location-contracts",
		encryptionContext,
	)
	if !errors.Is(err, ErrKMS) {
		t.Fatalf("GenerateDataKey() error = %v, want ErrKMS", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error exposed KMS cause: %q", err)
	}
}

type recordingClient struct {
	generateInput  *kms.GenerateDataKeyInput
	generateOutput *kms.GenerateDataKeyOutput
	generateErr    error
	decryptInput   *kms.DecryptInput
	decryptOutput  *kms.DecryptOutput
	decryptErr     error
}

func (client *recordingClient) GenerateDataKey(
	_ context.Context,
	input *kms.GenerateDataKeyInput,
	_ ...func(*kms.Options),
) (*kms.GenerateDataKeyOutput, error) {
	client.generateInput = input

	return client.generateOutput, client.generateErr
}

func (client *recordingClient) Decrypt(
	_ context.Context,
	input *kms.DecryptInput,
	_ ...func(*kms.Options),
) (*kms.DecryptOutput, error) {
	client.decryptInput = input

	return client.decryptOutput, client.decryptErr
}
