package awskms

import (
	"context"
	"errors"
	"reflect"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
	secretenvelope "github.com/faustbrian/golib/pkg/secret-envelope"
)

var (
	ErrClientRequired  = errors.New("AWS KMS client is required")
	ErrInvalidRequest  = errors.New("AWS KMS data-key request is invalid")
	ErrKMS             = errors.New("AWS KMS data-key operation failed")
	ErrInvalidResponse = errors.New("AWS KMS data-key response is invalid")
)

// Client is the least-privilege AWS KMS surface used by Provider.
type Client interface {
	GenerateDataKey(
		context.Context,
		*kms.GenerateDataKeyInput,
		...func(*kms.Options),
	) (*kms.GenerateDataKeyOutput, error)
	Decrypt(
		context.Context,
		*kms.DecryptInput,
		...func(*kms.Options),
	) (*kms.DecryptOutput, error)
}

// Provider wraps one-use AES-256 data keys with a symmetric KMS key.
type Provider struct {
	client Client
}

// New validates the explicit KMS client.
func New(client Client) (*Provider, error) {
	if nilLike(client) {
		return nil, ErrClientRequired
	}

	return &Provider{client: client}, nil
}

// GenerateDataKey asks KMS for a fresh AES-256 key and its wrapped copy.
func (provider *Provider) GenerateDataKey(
	ctx context.Context,
	keyReference string,
	encryptionContext secretenvelope.Context,
) (secretenvelope.DataKey, error) {
	if provider == nil || nilLike(provider.client) {
		return secretenvelope.DataKey{}, ErrClientRequired
	}
	contextValues := encryptionContext.Values()
	if ctx == nil || keyReference == "" || len(contextValues) == 0 {
		return secretenvelope.DataKey{}, ErrInvalidRequest
	}

	output, err := provider.client.GenerateDataKey(
		ctx,
		&kms.GenerateDataKeyInput{
			KeyId:             aws.String(keyReference),
			KeySpec:           types.DataKeySpecAes256,
			EncryptionContext: contextValues,
		},
	)
	if err != nil {
		return secretenvelope.DataKey{}, operationError{
			operation: "generate",
			kind:      ErrKMS,
			cause:     err,
		}
	}
	if output == nil {
		return secretenvelope.DataKey{}, ErrInvalidResponse
	}

	dataKey, err := secretenvelope.NewDataKey(
		output.Plaintext,
		output.CiphertextBlob,
		aws.ToString(output.KeyId),
	)
	if err != nil {
		zero(output.Plaintext)
		return secretenvelope.DataKey{}, ErrInvalidResponse
	}

	return dataKey, nil
}

// DecryptDataKey unwraps one data key with its exact key and context.
func (provider *Provider) DecryptDataKey(
	ctx context.Context,
	keyReference string,
	ciphertext []byte,
	encryptionContext secretenvelope.Context,
) ([]byte, error) {
	if provider == nil || nilLike(provider.client) {
		return nil, ErrClientRequired
	}
	contextValues := encryptionContext.Values()
	if ctx == nil ||
		keyReference == "" ||
		len(ciphertext) == 0 ||
		len(contextValues) == 0 {
		return nil, ErrInvalidRequest
	}

	output, err := provider.client.Decrypt(
		ctx,
		&kms.DecryptInput{
			KeyId:          aws.String(keyReference),
			CiphertextBlob: append([]byte(nil), ciphertext...),
			EncryptionAlgorithm: types.
				EncryptionAlgorithmSpecSymmetricDefault,
			EncryptionContext: contextValues,
		},
	)
	if err != nil {
		return nil, operationError{
			operation: "decrypt",
			kind:      ErrKMS,
			cause:     err,
		}
	}
	if output == nil ||
		len(output.Plaintext) != secretenvelope.DataKeySize ||
		aws.ToString(output.KeyId) != keyReference ||
		output.EncryptionAlgorithm !=
			types.EncryptionAlgorithmSpecSymmetricDefault {
		if output != nil {
			zero(output.Plaintext)
		}
		return nil, ErrInvalidResponse
	}

	return output.Plaintext, nil
}

type operationError struct {
	operation string
	kind      error
	cause     error
}

func (err operationError) Error() string {
	return "AWS KMS " + err.operation + " data key failed"
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

var _ secretenvelope.KeyProvider = (*Provider)(nil)
