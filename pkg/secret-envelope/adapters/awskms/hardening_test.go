package awskms

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
	secretenvelope "github.com/faustbrian/golib/pkg/secret-envelope"
)

func TestProviderRejectsNilClientsAndReceivers(t *testing.T) {
	t.Parallel()

	var typedNil *recordingClient
	for name, client := range map[string]Client{
		"nil":       nil,
		"typed-nil": typedNil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := New(client); !errors.Is(err, ErrClientRequired) {
				t.Fatalf("New() error = %v, want ErrClientRequired", err)
			}
		})
	}
	if !nilLike(nil) || !nilLike(typedNil) || nilLike(42) {
		t.Fatal("nilLike() classification is incorrect")
	}

	var provider *Provider
	encryptionContext, _ := secretenvelope.NewContext(
		map[string]string{"service": "location"},
	)
	if _, err := provider.GenerateDataKey(
		context.Background(),
		"alias/location",
		encryptionContext,
	); !errors.Is(err, ErrClientRequired) {
		t.Fatalf("nil GenerateDataKey() error = %v", err)
	}
	if _, err := provider.DecryptDataKey(
		context.Background(),
		"alias/location",
		[]byte("wrapped"),
		encryptionContext,
	); !errors.Is(err, ErrClientRequired) {
		t.Fatalf("nil DecryptDataKey() error = %v", err)
	}
}

func TestProviderRejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	provider, _ := New(&recordingClient{})
	encryptionContext, _ := secretenvelope.NewContext(
		map[string]string{"service": "location"},
	)
	for name, test := range map[string]struct {
		ctx          context.Context
		keyReference string
		ciphertext   []byte
		context      secretenvelope.Context
		decrypt      bool
	}{
		"generate-context": {
			ctx:          context.Background(),
			keyReference: "alias/location",
		},
		"generate-key": {
			ctx:     context.Background(),
			context: encryptionContext,
		},
		"generate-go-context": {
			keyReference: "alias/location",
			context:      encryptionContext,
		},
		"decrypt-context": {
			ctx:          context.Background(),
			keyReference: "alias/location",
			ciphertext:   []byte("wrapped"),
			decrypt:      true,
		},
		"decrypt-key": {
			ctx:        context.Background(),
			ciphertext: []byte("wrapped"),
			context:    encryptionContext,
			decrypt:    true,
		},
		"decrypt-ciphertext": {
			ctx:          context.Background(),
			keyReference: "alias/location",
			context:      encryptionContext,
			decrypt:      true,
		},
		"decrypt-go-context": {
			keyReference: "alias/location",
			ciphertext:   []byte("wrapped"),
			context:      encryptionContext,
			decrypt:      true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var err error
			if test.decrypt {
				_, err = provider.DecryptDataKey(
					test.ctx,
					test.keyReference,
					test.ciphertext,
					test.context,
				)
			} else {
				_, err = provider.GenerateDataKey(
					test.ctx,
					test.keyReference,
					test.context,
				)
			}
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("operation error = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

func TestProviderRejectsInvalidGenerateResponsesAndZeroizesKeys(t *testing.T) {
	t.Parallel()

	const resolvedKey = "arn:aws:kms:eu-north-1:123456789012:key/example"
	encryptionContext, _ := secretenvelope.NewContext(
		map[string]string{"service": "location"},
	)
	for name, output := range map[string]*kms.GenerateDataKeyOutput{
		"nil": nil,
		"key-size": {
			Plaintext:      []byte{0x42},
			CiphertextBlob: []byte("wrapped"),
			KeyId:          aws.String(resolvedKey),
		},
		"ciphertext": {
			Plaintext: bytes.Repeat(
				[]byte{0x42},
				secretenvelope.DataKeySize,
			),
			KeyId: aws.String(resolvedKey),
		},
		"key-reference": {
			Plaintext: bytes.Repeat(
				[]byte{0x42},
				secretenvelope.DataKeySize,
			),
			CiphertextBlob: []byte("wrapped"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client := &recordingClient{generateOutput: output}
			provider, _ := New(client)
			_, err := provider.GenerateDataKey(
				context.Background(),
				"alias/location",
				encryptionContext,
			)
			if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf(
					"GenerateDataKey() error = %v, want ErrInvalidResponse",
					err,
				)
			}
			if output != nil && !allZero(output.Plaintext) {
				t.Fatal("invalid plaintext key was not zeroized")
			}
		})
	}
}

func TestProviderRejectsDecryptFailuresAndMalformedResponses(t *testing.T) {
	t.Parallel()

	const (
		resolvedKey = "arn:aws:kms:eu-north-1:123456789012:key/example"
		secret      = "kms-decrypt-secret"
	)
	encryptionContext, _ := secretenvelope.NewContext(
		map[string]string{"service": "location"},
	)
	provider, _ := New(&recordingClient{
		decryptErr: errors.New(secret),
	})
	_, err := provider.DecryptDataKey(
		context.Background(),
		resolvedKey,
		[]byte("wrapped"),
		encryptionContext,
	)
	if !errors.Is(err, ErrKMS) || strings.Contains(err.Error(), secret) {
		t.Fatalf("DecryptDataKey() error = %v", err)
	}

	for name, output := range map[string]*kms.DecryptOutput{
		"nil": nil,
		"key-size": {
			Plaintext:           []byte{0x42},
			KeyId:               aws.String(resolvedKey),
			EncryptionAlgorithm: types.EncryptionAlgorithmSpecSymmetricDefault,
		},
		"key-reference": {
			Plaintext: bytes.Repeat(
				[]byte{0x42},
				secretenvelope.DataKeySize,
			),
			KeyId:               aws.String("different-key"),
			EncryptionAlgorithm: types.EncryptionAlgorithmSpecSymmetricDefault,
		},
		"algorithm": {
			Plaintext: bytes.Repeat(
				[]byte{0x42},
				secretenvelope.DataKeySize,
			),
			KeyId: aws.String(resolvedKey),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client := &recordingClient{decryptOutput: output}
			provider, _ := New(client)
			_, err := provider.DecryptDataKey(
				context.Background(),
				resolvedKey,
				[]byte("wrapped"),
				encryptionContext,
			)
			if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf(
					"DecryptDataKey() error = %v, want ErrInvalidResponse",
					err,
				)
			}
			if output != nil && !allZero(output.Plaintext) {
				t.Fatal("invalid plaintext key was not zeroized")
			}
		})
	}
}

func TestOperationErrorPreservesCauseWithoutRenderingIt(t *testing.T) {
	t.Parallel()

	cause := errors.New("sensitive-cause")
	err := operationError{
		operation: "test",
		kind:      ErrKMS,
		cause:     cause,
	}
	if !errors.Is(err, ErrKMS) ||
		!errors.Is(err, cause) ||
		err.Error() != "AWS KMS test data key failed" {
		t.Fatalf("operation error = %v", err)
	}
	if fmt.Sprintf("%v", err) != err.Error() {
		t.Fatal("formatted operation error changed")
	}
}

func allZero(value []byte) bool {
	for _, item := range value {
		if item != 0 {
			return false
		}
	}

	return true
}
