package webhook

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// SignatureHeader carries one strict structured value per signing key.
	SignatureHeader = "Webhook-Signature"
)

var (
	// ErrMalformedSignatureHeader means signature header syntax was invalid or
	// ambiguous. One malformed value rejects the entire header set.
	ErrMalformedSignatureHeader = errors.New("malformed webhook signature header")
	// ErrSignatureHeadersTooLarge means header count or bytes exceeded limits.
	ErrSignatureHeadersTooLarge = errors.New("webhook signature headers too large")
	// ErrMalformedSignedHeader means a fixed behavior-changing header was
	// duplicated, oversized, or otherwise unsafe to canonicalize.
	ErrMalformedSignedHeader = errors.New("malformed webhook signed header")
)

const maxFixedSignedHeaderBytes = 256

// HeaderLimits bounds parsing before any base64 decoding or allocation based
// on decoded values.
type HeaderLimits struct {
	MaxSignatures int
	MaxBytes      int
}

// EventIDExtractor extracts a replay identifier after authentication.
type EventIDExtractor func(request *http.Request, body []byte) (string, error)

// RequestOptions controls bounded HTTP signing and verification.
type RequestOptions struct {
	MaxBodyBytes int64
	HeaderLimits HeaderLimits
	Metadata     map[string]string
	EventID      EventIDExtractor
}

// SetSignatureHeaders replaces all signature headers with strict v1 values.
func SetSignatureHeaders(header http.Header, signatures []Signature) error {
	if header == nil || len(signatures) == 0 {
		return fmt.Errorf("%w: header and signatures are required", ErrInvalidConfiguration)
	}
	values := make([]string, len(signatures))
	for index, signature := range signatures {
		value, err := formatSignatureHeader(signature)
		if err != nil {
			return err
		}
		values[index] = value
	}

	header.Del(SignatureHeader)
	for _, value := range values {
		header.Add(SignatureHeader, value)
	}

	return nil
}

// ParseSignatureHeaders parses all values strictly. Comma-combined values,
// duplicate key IDs, noncanonical timestamps, and unknown fields are rejected.
func ParseSignatureHeaders(header http.Header, limits HeaderLimits) ([]Signature, error) {
	if limits.MaxSignatures <= 0 || limits.MaxBytes <= 0 {
		return nil, fmt.Errorf("%w: positive header limits are required", ErrInvalidConfiguration)
	}
	values := header.Values(SignatureHeader)
	if len(values) == 0 {
		return nil, ErrMalformedSignatureHeader
	}
	bytes := 0
	for _, value := range values {
		bytes += len(value)
	}
	if len(values) > limits.MaxSignatures || bytes > limits.MaxBytes {
		return nil, ErrSignatureHeadersTooLarge
	}

	signatures := make([]Signature, 0, len(values))
	keyIDs := make(map[string]struct{}, len(values))
	for _, value := range values {
		signature, err := parseSignatureHeader(value)
		if err != nil {
			return nil, err
		}
		if _, duplicate := keyIDs[signature.KeyID]; duplicate {
			return nil, ErrMalformedSignatureHeader
		}
		keyIDs[signature.KeyID] = struct{}{}
		signatures = append(signatures, signature)
	}

	return signatures, nil
}

func formatSignatureHeader(signature Signature) (string, error) {
	if signature.Version != "v1" || signature.KeyID == "" || signature.Timestamp.IsZero() || signature.Timestamp.Unix() < 0 || !validNonce(signature.Nonce) {
		return "", fmt.Errorf("%w: incomplete signature", ErrInvalidConfiguration)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(signature.Value)
	if err != nil || len(decoded) != signatureBytes(signature.Algorithm) {
		return "", fmt.Errorf("%w: invalid signature value", ErrInvalidConfiguration)
	}
	if signatureBytes(signature.Algorithm) == 0 {
		return "", fmt.Errorf("%w: unsupported signature algorithm", ErrInvalidConfiguration)
	}

	return strings.Join([]string{
		"v1",
		"algorithm=" + string(signature.Algorithm),
		"keyid=" + base64.RawURLEncoding.EncodeToString([]byte(signature.KeyID)),
		"timestamp=" + strconv.FormatInt(signature.Timestamp.Unix(), 10),
		"nonce=" + base64.RawURLEncoding.EncodeToString([]byte(signature.Nonce)),
		"signature=" + signature.Value,
	}, ";"), nil
}

func parseSignatureHeader(value string) (Signature, error) {
	if strings.Contains(value, ",") {
		return Signature{}, ErrMalformedSignatureHeader
	}
	parts := strings.Split(value, ";")
	if len(parts) != 6 || parts[0] != "v1" {
		return Signature{}, ErrMalformedSignatureHeader
	}
	algorithmValue, ok := strictField(parts[1], "algorithm")
	if !ok {
		return Signature{}, ErrMalformedSignatureHeader
	}
	algorithm := Algorithm(algorithmValue)
	if signatureBytes(algorithm) == 0 {
		return Signature{}, ErrMalformedSignatureHeader
	}
	keyValue, ok := strictField(parts[2], "keyid")
	if !ok {
		return Signature{}, ErrMalformedSignatureHeader
	}
	keyBytes, err := base64.RawURLEncoding.DecodeString(keyValue)
	if err != nil || len(keyBytes) == 0 || !utf8.Valid(keyBytes) {
		return Signature{}, ErrMalformedSignatureHeader
	}
	timestampValue, ok := strictField(parts[3], "timestamp")
	if !ok {
		return Signature{}, ErrMalformedSignatureHeader
	}
	timestamp, err := strconv.ParseInt(timestampValue, 10, 64)
	if err != nil || timestamp < 0 || strconv.FormatInt(timestamp, 10) != timestampValue {
		return Signature{}, ErrMalformedSignatureHeader
	}
	nonceValue, ok := strictField(parts[4], "nonce")
	if !ok {
		return Signature{}, ErrMalformedSignatureHeader
	}
	nonceBytes, err := base64.RawURLEncoding.DecodeString(nonceValue)
	if err != nil || !validNonce(string(nonceBytes)) {
		return Signature{}, ErrMalformedSignatureHeader
	}
	signatureValue, ok := strictField(parts[5], "signature")
	if !ok {
		return Signature{}, ErrMalformedSignatureHeader
	}
	signature, err := base64.RawURLEncoding.DecodeString(signatureValue)
	if err != nil || len(signature) != signatureBytes(algorithm) {
		return Signature{}, ErrMalformedSignatureHeader
	}

	return Signature{
		Version:   "v1",
		Algorithm: algorithm,
		KeyID:     string(keyBytes),
		Timestamp: time.Unix(timestamp, 0).UTC(),
		Nonce:     string(nonceBytes),
		Value:     signatureValue,
	}, nil
}

func strictField(part, name string) (string, bool) {
	prefix := name + "="
	return strings.TrimPrefix(part, prefix), strings.HasPrefix(part, prefix) && len(part) > len(prefix)
}

func signatureBytes(algorithm Algorithm) int {
	switch algorithm {
	case SHA256:
		return 32
	case SHA512:
		return 64
	default:
		return 0
	}
}

// SignRequest captures and restores the exact body, signs the effective
// request target, and replaces any preexisting signature headers.
func (s *Signer) SignRequest(request *http.Request, options RequestOptions) ([]Signature, []byte, error) {
	if request == nil || request.URL == nil {
		return nil, nil, fmt.Errorf("%w: request and URL are required", ErrInvalidConfiguration)
	}
	contentType, idempotencyKey, err := fixedSignedHeaders(request.Header)
	if err != nil {
		return nil, nil, err
	}
	body, err := CaptureBody(request, options.MaxBodyBytes)
	if err != nil {
		return nil, nil, err
	}
	message := requestMessage(request, body, options.Metadata, contentType, idempotencyKey)
	signatures, err := s.Sign(message)
	if err != nil {
		return nil, body, err
	}
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	if err := SetSignatureHeaders(request.Header, signatures); err != nil {
		return nil, body, err
	}
	if _, err := ParseSignatureHeaders(request.Header, options.HeaderLimits); err != nil {
		request.Header.Del(SignatureHeader)
		return nil, body, err
	}

	return signatures, body, nil
}

// VerifyRequest bounds and parses headers before capturing the exact body. It
// authenticates before invoking the event-ID extractor or replay store.
func (v *Verifier) VerifyRequest(
	ctx context.Context,
	request *http.Request,
	options RequestOptions,
) (verification Verification, body []byte, err error) {
	started := v.clock()
	defer func() {
		outcome := OutcomeSuccess
		if err != nil {
			outcome = OutcomeRejected
		}
		observeSafely(v.observer, ctx, Observation{
			Operation: OperationVerification,
			Outcome:   outcome,
			Reason:    observationReason(err),
			Duration:  elapsed(v.clock, started),
			Algorithm: v.algorithm,
		})
	}()
	if request == nil || request.URL == nil {
		return Verification{}, nil, fmt.Errorf("%w: request and URL are required", ErrInvalidConfiguration)
	}
	signatures, err := ParseSignatureHeaders(request.Header, options.HeaderLimits)
	if err != nil {
		return Verification{}, nil, &VerificationError{Kind: err, Diagnostic: "signature headers rejected"}
	}
	contentType, idempotencyKey, err := fixedSignedHeaders(request.Header)
	if err != nil {
		return Verification{}, nil, &VerificationError{Kind: err, Diagnostic: "fixed signed headers rejected"}
	}
	body, err = CaptureBody(request, options.MaxBodyBytes)
	if err != nil {
		return Verification{}, nil, &VerificationError{Kind: err, Diagnostic: "request body rejected"}
	}
	verification, err = v.Verify(requestMessage(request, body, options.Metadata, contentType, idempotencyKey), signatures)
	if err != nil {
		return Verification{}, body, err
	}
	if v.replay == nil {
		return verification, body, nil
	}
	if options.EventID == nil {
		return Verification{}, body, &VerificationError{Kind: ErrMissingEventID, Diagnostic: "event ID extractor is required"}
	}
	eventID, err := options.EventID(request, body)
	if err != nil {
		return Verification{}, body, &VerificationError{Kind: ErrMissingEventID, Diagnostic: "event ID extraction failed"}
	}
	if err := v.recordReplay(ctx, verification, eventID); err != nil {
		return Verification{}, body, err
	}

	return verification, body, nil
}

func requestMessage(request *http.Request, body []byte, metadata map[string]string, contentType, idempotencyKey string) Message {
	path := request.URL.EscapedPath()
	if path == "" {
		path = "/"
	}

	return Message{
		Method:         request.Method,
		Path:           path,
		RawQuery:       request.URL.RawQuery,
		Host:           request.Host,
		ContentType:    contentType,
		IdempotencyKey: idempotencyKey,
		Body:           body,
		Metadata:       metadata,
	}
}

func fixedSignedHeaders(header http.Header) (string, string, error) {
	contentType, err := fixedSignedHeader(header, "Content-Type")
	if err != nil {
		return "", "", err
	}
	idempotencyKey, err := fixedSignedHeader(header, IdempotencyHeader)
	if err != nil {
		return "", "", err
	}

	return contentType, idempotencyKey, nil
}

func fixedSignedHeader(header http.Header, name string) (string, error) {
	values := header.Values(name)
	if len(values) == 0 {
		return "", nil
	}
	if len(values) != 1 || len(values[0]) > maxFixedSignedHeaderBytes ||
		!utf8.ValidString(values[0]) || strings.ContainsAny(values[0], "\r\n") {
		return "", ErrMalformedSignedHeader
	}

	return values[0], nil
}

// HeaderEventID returns a strict single-header extractor with a byte limit.
func HeaderEventID(name string, maxBytes int) EventIDExtractor {
	return func(request *http.Request, _ []byte) (string, error) {
		if request == nil || name == "" || maxBytes <= 0 {
			return "", ErrMissingEventID
		}
		values := request.Header.Values(name)
		if len(values) != 1 || values[0] == "" || len(values[0]) > maxBytes {
			return "", ErrMissingEventID
		}

		return values[0], nil
	}
}
