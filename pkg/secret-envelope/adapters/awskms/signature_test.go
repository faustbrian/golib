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
)

const signatureTestKeyARN = "arn:aws:kms:eu-north-1:123456789012:key/example"

func TestSignatureVerifierAuthenticatesExactRawMessage(t *testing.T) {
	t.Parallel()

	client := &recordingSignatureClient{
		output: &kms.VerifyOutput{
			KeyId:            aws.String(signatureTestKeyARN),
			SignatureValid:   true,
			SigningAlgorithm: types.SigningAlgorithmSpecEcdsaSha256,
		},
	}
	verifier, err := NewSignatureVerifier(
		client,
		types.SigningAlgorithmSpecEcdsaSha256,
	)
	if err != nil {
		t.Fatalf("NewSignatureVerifier() error = %v", err)
	}
	message := []byte("canonical approval statement")
	signature := []byte("DER signature")
	if err := verifier.Verify(
		context.Background(),
		signatureTestKeyARN,
		message,
		signature,
	); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if client.input == nil ||
		aws.ToString(client.input.KeyId) != signatureTestKeyARN ||
		client.input.MessageType != types.MessageTypeRaw ||
		client.input.SigningAlgorithm !=
			types.SigningAlgorithmSpecEcdsaSha256 ||
		!bytes.Equal(client.input.Message, message) ||
		!bytes.Equal(client.input.Signature, signature) {
		t.Fatalf("Verify input = %#v", client.input)
	}
	message[0] = 'X'
	signature[0] = 'X'
	if bytes.Equal(client.input.Message, message) ||
		bytes.Equal(client.input.Signature, signature) {
		t.Fatal("Verify retained caller-owned bytes")
	}
}

func TestSignatureVerifierRejectsInvalidConstructionAndRequests(t *testing.T) {
	t.Parallel()

	var typedNil *recordingSignatureClient
	for name, test := range map[string]struct {
		client    SignatureClient
		algorithm types.SigningAlgorithmSpec
	}{
		"nil client": {
			algorithm: types.SigningAlgorithmSpecEcdsaSha256,
		},
		"typed nil client": {
			client: typedNil, algorithm: types.SigningAlgorithmSpecEcdsaSha256,
		},
		"unsupported algorithm": {
			client:    &recordingSignatureClient{},
			algorithm: types.SigningAlgorithmSpecRsassaPkcs1V15Sha256,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewSignatureVerifier(
				test.client,
				test.algorithm,
			); !errors.Is(err, ErrInvalidSignatureVerifier) {
				t.Fatalf("NewSignatureVerifier() error = %v", err)
			}
		})
	}

	verifier, err := NewSignatureVerifier(
		&recordingSignatureClient{},
		types.SigningAlgorithmSpecEcdsaSha256,
	)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for name, test := range map[string]struct {
		ctx          context.Context
		keyReference string
		message      []byte
		signature    []byte
		want         error
	}{
		"nil context": {
			keyReference: signatureTestKeyARN, message: []byte("message"),
			signature: []byte("signature"), want: ErrInvalidSignatureRequest,
		},
		"cancelled context": {
			ctx: cancelled, keyReference: signatureTestKeyARN,
			message:   []byte("message"),
			signature: []byte("signature"), want: context.Canceled,
		},
		"empty key": {
			ctx: context.Background(), message: []byte("message"),
			signature: []byte("signature"), want: ErrInvalidSignatureRequest,
		},
		"unsafe key": {
			ctx:          context.Background(),
			keyReference: " " + signatureTestKeyARN,
			message:      []byte("message"), signature: []byte("signature"),
			want: ErrInvalidSignatureRequest,
		},
		"large key": {
			ctx: context.Background(),
			keyReference: strings.Repeat(
				"k",
				maximumSignatureKeyBytes+1,
			),
			message: []byte("message"), signature: []byte("signature"),
			want: ErrInvalidSignatureRequest,
		},
		"invalid UTF-8 key": {
			ctx: context.Background(), keyReference: string([]byte{0xff}),
			message: []byte("message"), signature: []byte("signature"),
			want: ErrInvalidSignatureRequest,
		},
		"control key": {
			ctx:          context.Background(),
			keyReference: signatureTestKeyARN + "\x00",
			message:      []byte("message"), signature: []byte("signature"),
			want: ErrInvalidSignatureRequest,
		},
		"alias key": {
			ctx: context.Background(), keyReference: "alias/key",
			message: []byte("message"), signature: []byte("signature"),
			want: ErrInvalidSignatureRequest,
		},
		"non-key ARN": {
			ctx:          context.Background(),
			keyReference: "arn:aws:kms:eu-north-1:123456789012:alias/example",
			message:      []byte("message"), signature: []byte("signature"),
			want: ErrInvalidSignatureRequest,
		},
		"empty message": {
			ctx: context.Background(), keyReference: signatureTestKeyARN,
			signature: []byte("signature"), want: ErrInvalidSignatureRequest,
		},
		"large message": {
			ctx: context.Background(), keyReference: signatureTestKeyARN,
			message:   make([]byte, maximumRawSignatureMessageBytes+1),
			signature: []byte("signature"), want: ErrInvalidSignatureRequest,
		},
		"empty signature": {
			ctx: context.Background(), keyReference: signatureTestKeyARN,
			message: []byte("message"), want: ErrInvalidSignatureRequest,
		},
		"large signature": {
			ctx: context.Background(), keyReference: signatureTestKeyARN,
			message:   []byte("message"),
			signature: make([]byte, maximumSignatureBytes+1),
			want:      ErrInvalidSignatureRequest,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := verifier.Verify(
				test.ctx,
				test.keyReference,
				test.message,
				test.signature,
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("Verify() error = %v, want %v", err, test.want)
			}
		})
	}

	var nilVerifier *SignatureVerifier
	if err := nilVerifier.Verify(
		context.Background(),
		signatureTestKeyARN,
		[]byte("message"),
		[]byte("signature"),
	); !errors.Is(err, ErrInvalidSignatureVerifier) {
		t.Fatalf("nil Verify() error = %v", err)
	}
}

func TestSignatureVerifierClassifiesFailuresWithoutRenderingCauses(
	t *testing.T,
) {
	t.Parallel()

	const secret = "kms-signature-secret"
	for name, test := range map[string]struct {
		output *kms.VerifyOutput
		err    error
		want   error
	}{
		"KMS failure": {
			err: errors.New(secret), want: ErrKMSSignatureVerification,
		},
		"invalid signature exception": {
			err:  &types.KMSInvalidSignatureException{Message: aws.String(secret)},
			want: ErrSignatureRejected,
		},
		"nil output": {
			want: ErrInvalidSignatureResponse,
		},
		"rejected output": {
			output: &kms.VerifyOutput{
				KeyId:            aws.String(signatureTestKeyARN),
				SigningAlgorithm: types.SigningAlgorithmSpecEcdsaSha256,
			},
			want: ErrSignatureRejected,
		},
		"empty resolved key": {
			output: &kms.VerifyOutput{
				SignatureValid:   true,
				SigningAlgorithm: types.SigningAlgorithmSpecEcdsaSha256,
			},
			want: ErrInvalidSignatureResponse,
		},
		"algorithm mismatch": {
			output: &kms.VerifyOutput{
				KeyId: aws.String(signatureTestKeyARN), SignatureValid: true,
				SigningAlgorithm: types.SigningAlgorithmSpecEcdsaSha384,
			},
			want: ErrInvalidSignatureResponse,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			verifier, err := NewSignatureVerifier(
				&recordingSignatureClient{
					output: test.output,
					err:    test.err,
				},
				types.SigningAlgorithmSpecEcdsaSha256,
			)
			if err != nil {
				t.Fatal(err)
			}
			err = verifier.Verify(
				context.Background(),
				signatureTestKeyARN,
				[]byte("message"),
				[]byte("signature"),
			)
			if !errors.Is(err, test.want) ||
				strings.Contains(fmt.Sprintf("%v %#v", err, err), secret) {
				t.Fatalf("Verify() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestSignatureVerifierAcceptsReviewedRawAlgorithms(t *testing.T) {
	t.Parallel()

	algorithms := []types.SigningAlgorithmSpec{
		types.SigningAlgorithmSpecRsassaPssSha256,
		types.SigningAlgorithmSpecRsassaPssSha384,
		types.SigningAlgorithmSpecRsassaPssSha512,
		types.SigningAlgorithmSpecEcdsaSha256,
		types.SigningAlgorithmSpecEcdsaSha384,
		types.SigningAlgorithmSpecEcdsaSha512,
		types.SigningAlgorithmSpecEd25519Sha512,
	}
	for _, algorithm := range algorithms {
		if _, err := NewSignatureVerifier(
			&recordingSignatureClient{},
			algorithm,
		); err != nil {
			t.Fatalf("algorithm %q error = %v", algorithm, err)
		}
	}
}

type recordingSignatureClient struct {
	input  *kms.VerifyInput
	output *kms.VerifyOutput
	err    error
}

func (client *recordingSignatureClient) Verify(
	_ context.Context,
	input *kms.VerifyInput,
	_ ...func(*kms.Options),
) (*kms.VerifyOutput, error) {
	client.input = input

	return client.output, client.err
}
