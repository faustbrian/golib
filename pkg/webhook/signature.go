// Package webhook provides protocol-independent webhook signing, verification,
// replay protection, and delivery primitives.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Algorithm identifies a supported HMAC construction.
type Algorithm string

const (
	// SHA256 is HMAC-SHA-256.
	SHA256 Algorithm = "sha256"
	// SHA512 is HMAC-SHA-512.
	SHA512 Algorithm = "sha512"
)

var (
	// ErrInvalidSignature means that no supplied signature authenticated.
	ErrInvalidSignature = errors.New("invalid webhook signature")
	// ErrInvalidTimestamp means that a signature timestamp is missing, invalid,
	// or outside the configured tolerance.
	ErrInvalidTimestamp = errors.New("invalid webhook timestamp")
	// ErrInvalidConfiguration means a signer or verifier configuration is not
	// safe to use.
	ErrInvalidConfiguration = errors.New("invalid webhook configuration")
	// ErrNoActiveKey means no configured signing key is active.
	ErrNoActiveKey = errors.New("no active webhook signing key")
	// ErrReplay means an authenticated event ID was already atomically stored.
	ErrReplay = errors.New("webhook replay detected")
	// ErrMissingEventID means replay protection was requested without an ID.
	ErrMissingEventID = errors.New("webhook event ID is required")
	// ErrReplayStore means replay state could not be checked and the request was
	// rejected closed.
	ErrReplayStore = errors.New("webhook replay store unavailable")
	// ErrNonceGeneration means a signer could not produce a safe nonce.
	ErrNonceGeneration = errors.New("webhook nonce generation failed")
)

const maxNonceBytes = 128

// NonceGenerator returns a fresh public nonce for one signing operation.
// Implementations must be concurrency-safe.
type NonceGenerator func() (string, error)

// ReplayStore atomically checks and records replay keys. Implementations MUST
// return true only when the key was absent and was recorded with expiresAt in
// the same atomic operation. false, nil means the key already existed.
type ReplayStore interface {
	CheckAndRecord(ctx context.Context, key string, expiresAt time.Time) (recorded bool, err error)
}

// Message contains the exact request components covered by a signature. Body
// is never transformed before hashing.
type Message struct {
	Timestamp      time.Time
	Nonce          string
	Method         string
	Path           string
	RawQuery       string
	Host           string
	ContentType    string
	IdempotencyKey string
	Body           []byte
	Metadata       map[string]string
}

// SigningKey is a versioned outbound secret. Empty validity bounds are open.
type SigningKey struct {
	ID        string
	Secret    []byte
	NotBefore time.Time
	NotAfter  time.Time
	Revoked   bool
}

// VerificationKey is a versioned inbound secret. Empty validity bounds are
// open.
type VerificationKey struct {
	ID        string
	Secret    []byte
	NotBefore time.Time
	NotAfter  time.Time
	Revoked   bool
}

// Signature is a transport-neutral signature value.
type Signature struct {
	Version   string
	Algorithm Algorithm
	KeyID     string
	Timestamp time.Time
	Nonce     string
	Value     string
}

// Verification describes the key and algorithm that authenticated a message.
type Verification struct {
	KeyID     string
	Algorithm Algorithm
	Timestamp time.Time
	Nonce     string
}

// VerificationError separates a stable caller-facing message from internal
// diagnostics. Diagnostic must only be sent to a secret-safe internal sink.
type VerificationError struct {
	Kind       error
	Diagnostic string
}

// Error implements error without exposing internal diagnostic details.
func (e *VerificationError) Error() string {
	if e == nil || e.Kind == nil {
		return "webhook verification failed"
	}

	return e.Kind.Error()
}

// Unwrap enables errors.Is checks against the failure category.
func (e *VerificationError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Kind
}

// SafeMessage returns a stable response suitable for an untrusted caller.
func (e *VerificationError) SafeMessage() string {
	return "webhook verification failed"
}

// Canonicalize constructs the versioned, unambiguous bytes signed by v1.
func Canonicalize(message Message, keyID string, algorithm Algorithm) ([]byte, error) {
	if _, err := hashFactory(algorithm); err != nil {
		return nil, err
	}
	if message.Timestamp.IsZero() || message.Timestamp.Unix() < 0 {
		return nil, fmt.Errorf("%w: timestamp is required", ErrInvalidTimestamp)
	}
	if !validNonce(message.Nonce) {
		return nil, fmt.Errorf("%w: nonce is required and must be bounded UTF-8", ErrInvalidConfiguration)
	}
	if message.Method == "" || strings.ContainsAny(message.Method, "\r\n") {
		return nil, fmt.Errorf("%w: method is required and cannot contain a line break", ErrInvalidConfiguration)
	}

	query, err := canonicalQuery(message.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid query: %v", ErrInvalidConfiguration, err)
	}

	bodyDigest := sha256.Sum256(message.Body)
	metadata := canonicalMetadata(message.Metadata)
	encode := base64.RawURLEncoding.EncodeToString
	canonical := strings.Join([]string{
		"webhook-v1",
		"algorithm:" + string(algorithm),
		fmt.Sprintf("timestamp:%d", message.Timestamp.Unix()),
		"nonce:" + encode([]byte(message.Nonce)),
		"key-id:" + encode([]byte(keyID)),
		"method:" + message.Method,
		"path:" + encode([]byte(message.Path)),
		"query:" + encode([]byte(query)),
		"host:" + encode([]byte(strings.ToLower(message.Host))),
		"content-type:" + encode([]byte(message.ContentType)),
		"idempotency-key:" + encode([]byte(message.IdempotencyKey)),
		"body-sha256:" + encode(bodyDigest[:]),
		"metadata:" + encode([]byte(metadata)),
		"",
	}, "\n")

	return []byte(canonical), nil
}

func canonicalQuery(raw string) (string, error) {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "", err
	}

	return values.Encode(), nil
}

func canonicalMetadata(metadata map[string]string) string {
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	encode := base64.RawURLEncoding.EncodeToString
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, encode([]byte(key))+"="+encode([]byte(metadata[key])))
	}

	return strings.Join(lines, "\n")
}

// SignerConfig configures outbound signatures.
type SignerConfig struct {
	Algorithm      Algorithm
	Keys           []SigningKey
	Clock          func() time.Time
	NonceGenerator NonceGenerator
}

// Signer creates signatures for every active key, newest first.
type Signer struct {
	algorithm Algorithm
	hash      func() hash.Hash
	keys      []SigningKey
	clock     func() time.Time
	nonce     NonceGenerator
}

// NewSigner validates and copies signer configuration.
func NewSigner(config SignerConfig) (*Signer, error) {
	factory, err := hashFactory(config.Algorithm)
	if err != nil {
		return nil, err
	}
	if len(config.Keys) == 0 {
		return nil, fmt.Errorf("%w: at least one key is required", ErrInvalidConfiguration)
	}
	clock := config.Clock
	if clock == nil {
		clock = time.Now
	}
	nonce := config.NonceGenerator
	if nonce == nil {
		nonce = defaultNonce
	}
	keys := make([]SigningKey, len(config.Keys))
	for index, key := range config.Keys {
		if key.ID == "" || len(key.Secret) == 0 ||
			(!key.NotBefore.IsZero() && !key.NotAfter.IsZero() && key.NotAfter.Before(key.NotBefore)) {
			return nil, fmt.Errorf("%w: key ID, secret, and valid window are required", ErrInvalidConfiguration)
		}
		key.Secret = append([]byte(nil), key.Secret...)
		keys[index] = key
	}

	return &Signer{algorithm: config.Algorithm, hash: factory, keys: keys, clock: clock, nonce: nonce}, nil
}

// Sign creates one signature for each active key.
func (s *Signer) Sign(message Message) ([]Signature, error) {
	now := s.clock().UTC()
	if message.Timestamp.IsZero() {
		message.Timestamp = now
	}
	message.Timestamp = time.Unix(message.Timestamp.Unix(), 0).UTC()
	if message.Nonce == "" {
		if s.nonce == nil {
			return nil, ErrNonceGeneration
		}
		nonce, err := s.nonce()
		if err != nil || !validNonce(nonce) {
			return nil, ErrNonceGeneration
		}
		message.Nonce = nonce
	} else if !validNonce(message.Nonce) {
		return nil, fmt.Errorf("%w: nonce is invalid", ErrInvalidConfiguration)
	}
	keys := make([]SigningKey, 0, len(s.keys))
	for _, key := range s.keys {
		if activeKey(key.NotBefore, key.NotAfter, key.Revoked, message.Timestamp) {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return nil, ErrNoActiveKey
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].NotBefore.Equal(keys[j].NotBefore) {
			return keys[i].ID < keys[j].ID
		}

		return keys[i].NotBefore.After(keys[j].NotBefore)
	})

	signatures := make([]Signature, 0, len(keys))
	for _, key := range keys {
		canonical, err := Canonicalize(message, key.ID, s.algorithm)
		if err != nil {
			return nil, err
		}
		value := signWithHash(s.hash, key.Secret, canonical)
		signatures = append(signatures, Signature{
			Version:   "v1",
			Algorithm: s.algorithm,
			KeyID:     key.ID,
			Timestamp: message.Timestamp,
			Nonce:     message.Nonce,
			Value:     base64.RawURLEncoding.EncodeToString(value),
		})
	}

	return signatures, nil
}

// VerifierConfig configures inbound signature verification.
type VerifierConfig struct {
	Algorithm       Algorithm
	Keys            []VerificationKey
	Clock           func() time.Time
	Tolerance       time.Duration
	ReplayStore     ReplayStore
	ReplayTTL       time.Duration
	ReplayNamespace string
	Observer        Observer
}

// Verifier authenticates signatures with bounded clock skew.
type Verifier struct {
	algorithm Algorithm
	hash      func() hash.Hash
	keys      map[string]VerificationKey
	clock     func() time.Time
	tolerance time.Duration
	replay    ReplayStore
	replayTTL time.Duration
	namespace string
	observer  Observer
}

// NewVerifier validates and copies verifier configuration.
func NewVerifier(config VerifierConfig) (*Verifier, error) {
	factory, err := hashFactory(config.Algorithm)
	if err != nil {
		return nil, err
	}
	if len(config.Keys) == 0 || config.Tolerance < 0 {
		return nil, fmt.Errorf("%w: keys are required and tolerance cannot be negative", ErrInvalidConfiguration)
	}
	if config.ReplayStore != nil && (config.ReplayTTL <= 0 || config.ReplayNamespace == "") {
		return nil, fmt.Errorf("%w: replay TTL and namespace are required", ErrInvalidConfiguration)
	}
	if config.ReplayStore == nil && (config.ReplayTTL != 0 || config.ReplayNamespace != "") {
		return nil, fmt.Errorf("%w: replay settings require a store", ErrInvalidConfiguration)
	}
	clock := config.Clock
	if clock == nil {
		clock = time.Now
	}
	keys := make(map[string]VerificationKey, len(config.Keys))
	for _, key := range config.Keys {
		if key.ID == "" || len(key.Secret) == 0 ||
			(!key.NotBefore.IsZero() && !key.NotAfter.IsZero() && key.NotAfter.Before(key.NotBefore)) {
			return nil, fmt.Errorf("%w: key ID, secret, and valid window are required", ErrInvalidConfiguration)
		}
		if _, duplicate := keys[key.ID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate key ID", ErrInvalidConfiguration)
		}
		key.Secret = append([]byte(nil), key.Secret...)
		keys[key.ID] = key
	}

	return &Verifier{
		algorithm: config.Algorithm,
		hash:      factory,
		keys:      keys,
		clock:     clock,
		tolerance: config.Tolerance,
		replay:    config.ReplayStore,
		replayTTL: config.ReplayTTL,
		namespace: config.ReplayNamespace,
		observer:  config.Observer,
	}, nil
}

// Verify accepts the first valid signature without exposing which checks
// failed. All signature comparisons use hmac.Equal.
func (v *Verifier) Verify(message Message, signatures []Signature) (Verification, error) {
	now := v.clock().UTC()
	for _, signature := range signatures {
		if signature.Version != "v1" || signature.Algorithm != v.algorithm || signature.Timestamp.IsZero() || !validNonce(signature.Nonce) {
			continue
		}
		if now.Sub(signature.Timestamp).Abs() > v.tolerance {
			continue
		}
		if !message.Timestamp.IsZero() && message.Timestamp.Unix() != signature.Timestamp.Unix() {
			continue
		}
		if message.Nonce != "" && message.Nonce != signature.Nonce {
			continue
		}
		key, exists := v.keys[signature.KeyID]
		if !exists || !activeKey(key.NotBefore, key.NotAfter, key.Revoked, signature.Timestamp) {
			continue
		}
		candidate, err := base64.RawURLEncoding.DecodeString(signature.Value)
		if err != nil {
			continue
		}
		message.Timestamp = signature.Timestamp
		message.Nonce = signature.Nonce
		canonical, err := Canonicalize(message, signature.KeyID, v.algorithm)
		if err != nil {
			return Verification{}, &VerificationError{Kind: ErrInvalidSignature, Diagnostic: "canonical request rejected"}
		}
		expected := signWithHash(v.hash, key.Secret, canonical)
		if hmac.Equal(candidate, expected) {
			return Verification{KeyID: key.ID, Algorithm: v.algorithm, Timestamp: signature.Timestamp, Nonce: signature.Nonce}, nil
		}
	}

	return Verification{}, &VerificationError{Kind: ErrInvalidSignature, Diagnostic: "no supplied signature authenticated"}
}

// VerifyAndRecord authenticates a message and then uses the replay store's
// atomic check-and-record operation. It fails closed on every storage error.
func (v *Verifier) VerifyAndRecord(
	ctx context.Context,
	message Message,
	signatures []Signature,
	eventID string,
) (Verification, error) {
	verification, err := v.Verify(message, signatures)
	if err != nil {
		return Verification{}, err
	}
	if v.replay == nil {
		return verification, nil
	}
	if eventID == "" {
		return Verification{}, &VerificationError{Kind: ErrMissingEventID, Diagnostic: "replay protection requires an event ID"}
	}

	if err := v.recordReplay(ctx, verification, eventID); err != nil {
		return Verification{}, err
	}

	return verification, nil
}

func (v *Verifier) recordReplay(ctx context.Context, verification Verification, eventID string) (err error) {
	started := v.clock()
	defer func() {
		outcome := OutcomeSuccess
		if err != nil {
			outcome = OutcomeRejected
		}
		observeSafely(v.observer, ctx, Observation{
			Operation: OperationReplay,
			Outcome:   outcome,
			Reason:    observationReason(err),
			Duration:  elapsed(v.clock, started),
			Algorithm: verification.Algorithm,
		})
	}()
	if eventID == "" {
		return &VerificationError{Kind: ErrMissingEventID, Diagnostic: "replay protection requires an event ID"}
	}
	key := replayKey(v.namespace, eventID)
	recorded, err := v.replay.CheckAndRecord(ctx, key, v.clock().UTC().Add(v.replayTTL))
	if err != nil {
		return &VerificationError{Kind: ErrReplayStore, Diagnostic: "atomic check-and-record failed"}
	}
	if !recorded {
		return &VerificationError{Kind: ErrReplay, Diagnostic: "event ID was already recorded"}
	}

	return nil
}

func replayKey(namespace, eventID string) string {
	digest := sha256.New()
	for _, value := range []string{"go-webhook-replay-v1", namespace, eventID} {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(value)))
		_, _ = digest.Write(size[:])
		_, _ = digest.Write([]byte(value))
	}

	return hex.EncodeToString(digest.Sum(nil))
}

func activeKey(notBefore, notAfter time.Time, revoked bool, at time.Time) bool {
	return !revoked && (notBefore.IsZero() || !at.Before(notBefore)) && (notAfter.IsZero() || !at.After(notAfter))
}

func hashFactory(algorithm Algorithm) (func() hash.Hash, error) {
	switch algorithm {
	case SHA256:
		return sha256.New, nil
	case SHA512:
		return sha512.New, nil
	default:
		return nil, fmt.Errorf("%w: unsupported algorithm %q", ErrInvalidConfiguration, algorithm)
	}
}

func sign(algorithm Algorithm, secret, message []byte) ([]byte, error) {
	factory, err := hashFactory(algorithm)
	if err != nil {
		return nil, err
	}
	return signWithHash(factory, secret, message), nil
}

func signWithHash(factory func() hash.Hash, secret, message []byte) []byte {
	mac := hmac.New(factory, secret)
	_, _ = mac.Write(message)

	return mac.Sum(nil)
}

func defaultNonce() (string, error) {
	return generateNonce(rand.Reader)
}

func generateNonce(reader io.Reader) (string, error) {
	value := make([]byte, 18)
	if _, err := io.ReadFull(reader, value); err != nil {
		return "", fmt.Errorf("%w: entropy unavailable", ErrNonceGeneration)
	}

	return base64.RawURLEncoding.EncodeToString(value), nil
}

func validNonce(nonce string) bool {
	return nonce != "" && len(nonce) <= maxNonceBytes && utf8.ValidString(nonce)
}
