package awskms

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
)

const (
	maximumRawSignatureMessageBytes = 4096
	maximumSignatureBytes           = 1024
	maximumSignatureKeyBytes        = 2048
)

var (
	// ErrInvalidSignatureVerifier means construction or receiver state is not
	// safe for signature verification.
	ErrInvalidSignatureVerifier = errors.New(
		"AWS KMS signature verifier is invalid",
	)
	// ErrInvalidSignatureRequest means the key, message, signature, or context
	// cannot be sent to KMS safely.
	ErrInvalidSignatureRequest = errors.New(
		"AWS KMS signature verification request is invalid",
	)
	// ErrKMSSignatureVerification means KMS could not execute verification.
	ErrKMSSignatureVerification = errors.New(
		"AWS KMS signature verification failed",
	)
	// ErrSignatureRejected means KMS authenticated neither the message nor its
	// signature with the configured key and algorithm.
	ErrSignatureRejected = errors.New("AWS KMS signature was rejected")
	// ErrInvalidSignatureResponse means KMS returned an incomplete or
	// contradictory successful response.
	ErrInvalidSignatureResponse = errors.New(
		"AWS KMS signature verification response is invalid",
	)
)

// SignatureClient is the least-privilege AWS KMS surface used to authenticate
// an externally signed statement.
type SignatureClient interface {
	Verify(
		context.Context,
		*kms.VerifyInput,
		...func(*kms.Options),
	) (*kms.VerifyOutput, error)
}

// SignatureVerifier authenticates bounded raw messages with one explicitly
// reviewed asymmetric KMS signing algorithm. It never signs messages.
type SignatureVerifier struct {
	client    SignatureClient
	algorithm types.SigningAlgorithmSpec
}

// NewSignatureVerifier validates the KMS client and fixes the accepted
// algorithm for the verifier's lifetime.
func NewSignatureVerifier(
	client SignatureClient,
	algorithm types.SigningAlgorithmSpec,
) (*SignatureVerifier, error) {
	if nilLike(client) || !validRawSignatureAlgorithm(algorithm) {
		return nil, ErrInvalidSignatureVerifier
	}

	return &SignatureVerifier{client: client, algorithm: algorithm}, nil
}

// Verify authenticates one exact raw message. The method owns copies of caller
// bytes before invoking KMS and returns stable, secret-safe failure classes.
func (verifier *SignatureVerifier) Verify(
	ctx context.Context,
	keyReference string,
	message []byte,
	signature []byte,
) error {
	if verifier == nil || nilLike(verifier.client) ||
		!validRawSignatureAlgorithm(verifier.algorithm) {
		return ErrInvalidSignatureVerifier
	}
	if ctx == nil || !validSignatureKeyReference(keyReference) ||
		len(message) == 0 ||
		len(message) > maximumRawSignatureMessageBytes ||
		len(signature) == 0 || len(signature) > maximumSignatureBytes {
		return ErrInvalidSignatureRequest
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	output, err := verifier.client.Verify(ctx, &kms.VerifyInput{
		KeyId:            aws.String(keyReference),
		Message:          bytes.Clone(message),
		MessageType:      types.MessageTypeRaw,
		Signature:        bytes.Clone(signature),
		SigningAlgorithm: verifier.algorithm,
	})
	if err != nil {
		kind := ErrKMSSignatureVerification
		var rejected *types.KMSInvalidSignatureException
		if errors.As(err, &rejected) {
			kind = ErrSignatureRejected
		}

		return signatureOperationError{kind: kind, cause: err}
	}
	if output == nil {
		return ErrInvalidSignatureResponse
	}
	if !output.SignatureValid {
		return ErrSignatureRejected
	}
	if aws.ToString(output.KeyId) != keyReference ||
		output.SigningAlgorithm != verifier.algorithm {
		return ErrInvalidSignatureResponse
	}

	return nil
}

func validRawSignatureAlgorithm(
	algorithm types.SigningAlgorithmSpec,
) bool {
	switch algorithm {
	case types.SigningAlgorithmSpecRsassaPssSha256,
		types.SigningAlgorithmSpecRsassaPssSha384,
		types.SigningAlgorithmSpecRsassaPssSha512,
		types.SigningAlgorithmSpecEcdsaSha256,
		types.SigningAlgorithmSpecEcdsaSha384,
		types.SigningAlgorithmSpecEcdsaSha512,
		types.SigningAlgorithmSpecEd25519Sha512:
		return true
	default:
		return false
	}
}

func validSignatureKeyReference(value string) bool {
	if value == "" || len(value) > maximumSignatureKeyBytes ||
		strings.TrimSpace(value) != value || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	parts := strings.SplitN(value, ":", 6)
	if len(parts) != 6 || parts[0] != "arn" || parts[1] == "" ||
		parts[2] != "kms" || parts[3] == "" || parts[4] == "" ||
		!strings.HasPrefix(parts[5], "key/") || len(parts[5]) == len("key/") {
		return false
	}

	return true
}

type signatureOperationError struct {
	kind  error
	cause error
}

func (err signatureOperationError) Error() string {
	return "AWS KMS signature verification failed"
}

func (err signatureOperationError) Unwrap() []error {
	return []error{err.kind, err.cause}
}

func (err signatureOperationError) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, err.Error())
}
