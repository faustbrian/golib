// Package cursor encrypts, authenticates, versions, and bounds typed cursor
// positions without coupling them to a transport or database.
package cursor

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
)

var (
	// ErrInvalid reports a malformed, oversized, unknown-key, or tampered cursor.
	ErrInvalid = errors.New("cursor is invalid")
	// ErrExpired reports a cursor whose configured lifetime has ended.
	ErrExpired = errors.New("cursor has expired")
	// ErrVersion reports an unsupported cursor protocol version.
	ErrVersion = errors.New("cursor version is not supported")
	// ErrSchema reports a cursor compiled for another schema revision.
	ErrSchema = errors.New("cursor schema does not match")
	// ErrSort reports a cursor compiled for another ordered sort definition.
	ErrSort = errors.New("cursor sort does not match")
	// ErrReplay reports a valid cursor already consumed under replay policy.
	ErrReplay = errors.New("cursor replay is not allowed")
)

// Direction identifies forward or backward traversal.
type Direction = apiquery.CursorDirection

const (
	Forward  Direction = apiquery.CursorForward
	Backward Direction = apiquery.CursorBackward
)

// Payload binds typed positions to schema, sort, direction, expiry, and policy.
type Payload struct {
	SchemaRevision string
	Direction      Direction
	Sorts          []apiquery.SortTerm
	Positions      []apiquery.Value
	ExpiresAt      time.Time
	Policy         string
}

// Key is one named 256-bit AES key. Secret is defensively copied.
type Key struct {
	ID     string
	Secret []byte
}

// Keyring atomically rotates one active key and optional retained decode keys.
type Keyring struct {
	mu     sync.RWMutex
	active string
	keys   map[string][]byte
}

// NewKeyring constructs a keyring with one active key and retained keys.
func NewKeyring(active Key, retained ...Key) (*Keyring, error) {
	keyring := &Keyring{}
	if err := keyring.Rotate(active, retained...); err != nil {
		return nil, err
	}
	return keyring, nil
}

// Rotate replaces the active and retained key snapshot atomically.
func (k *Keyring) Rotate(active Key, retained ...Key) error {
	keys := make(map[string][]byte, len(retained)+1)
	all := append([]Key{active}, retained...)
	for _, key := range all {
		if !validKeyID(key.ID) || len(key.Secret) != 32 {
			return ErrInvalid
		}
		if _, duplicate := keys[key.ID]; duplicate {
			return ErrInvalid
		}
		keys[key.ID] = append([]byte(nil), key.Secret...)
	}
	k.mu.Lock()
	k.active = active.ID
	k.keys = keys
	k.mu.Unlock()
	return nil
}

func (k *Keyring) activeKey() (string, []byte, bool) {
	if k == nil {
		return "", nil, false
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	secret, exists := k.keys[k.active]
	return k.active, append([]byte(nil), secret...), exists
}

func (k *Keyring) key(id string) ([]byte, bool) {
	if k == nil {
		return nil, false
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	secret, exists := k.keys[id]
	return append([]byte(nil), secret...), exists
}

// Config contains hard cursor protocol limits and dependencies.
type Config struct {
	Version         string
	Keys            *Keyring
	MaxEncodedBytes int
	MaxPositions    int
	MaxStringBytes  int
	MaxTTL          time.Duration
	Clock           func() time.Time
	ReplayGuard     ReplayGuard
	Random          io.Reader
}

// ReplayGuard atomically accepts a new opaque fingerprint or rejects replay.
// Codec serializes calls, while the guard owns bounded retention until expiry.
type ReplayGuard func(fingerprint [32]byte, expiresAt time.Time) bool

// Codec encrypts and validates cursor payloads.
type Codec struct {
	version         string
	keys            *Keyring
	maxEncodedBytes int
	maxPositions    int
	maxStringBytes  int
	maxTTL          time.Duration
	clockMu         sync.RWMutex
	clock           func() time.Time
	replayMu        sync.Mutex
	replayGuard     ReplayGuard
	randomMu        sync.Mutex
	random          io.Reader
}

// NewCodec validates a bounded cursor protocol configuration.
func NewCodec(config Config) (*Codec, error) {
	if !validKeyID(config.Version) || config.Keys == nil || config.MaxEncodedBytes <= 0 ||
		config.MaxPositions <= 0 || config.MaxTTL <= 0 {
		return nil, ErrInvalid
	}
	if _, _, exists := config.Keys.activeKey(); !exists {
		return nil, ErrInvalid
	}
	if config.MaxStringBytes <= 0 {
		config.MaxStringBytes = 256
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	return &Codec{version: config.Version, keys: config.Keys,
		maxEncodedBytes: config.MaxEncodedBytes, maxPositions: config.MaxPositions,
		maxStringBytes: config.MaxStringBytes, maxTTL: config.MaxTTL,
		clock: config.Clock, replayGuard: config.ReplayGuard, random: config.Random}, nil
}

// SetClock replaces the time source atomically. It is intended for controlled
// tests and applications with an injected clock.
func (c *Codec) SetClock(clock func() time.Time) {
	if clock == nil {
		return
	}
	c.clockMu.Lock()
	c.clock = clock
	c.clockMu.Unlock()
}

// Encode returns an encrypted and authenticated opaque cursor.
func (c *Codec) Encode(payload Payload) (string, error) {
	now := c.now()
	if !c.validPayload(payload, now) {
		return "", ErrInvalid
	}
	keyID, secret, exists := c.keys.activeKey()
	if !exists {
		return "", ErrInvalid
	}
	wire := wirePayload{Version: c.version, SchemaRevision: payload.SchemaRevision,
		Direction: payload.Direction, Sorts: append([]apiquery.SortTerm(nil), payload.Sorts...),
		Positions: append([]apiquery.Value(nil), payload.Positions...),
		ExpiresAt: payload.ExpiresAt.UTC(), Policy: payload.Policy}
	plain, err := json.Marshal(wire)
	if err != nil || len(plain) > c.maxEncodedBytes {
		return "", ErrInvalid
	}
	aead, err := newAEAD(secret)
	if err != nil {
		return "", ErrInvalid
	}
	nonce := make([]byte, aead.NonceSize())
	c.randomMu.Lock()
	_, randomErr := io.ReadFull(c.random, nonce)
	c.randomMu.Unlock()
	if randomErr != nil {
		return "", ErrInvalid
	}
	aad := c.version + "." + keyID
	sealed := aead.Seal(nonce, nonce, plain, []byte(aad))
	token := aad + "." + base64.RawURLEncoding.EncodeToString(sealed)
	if len(token) > c.maxEncodedBytes {
		return "", ErrInvalid
	}
	return token, nil
}

// Decode authenticates, decrypts, and binds a cursor to the expected schema
// revision and exact ordered sort definition.
func (c *Codec) Decode(token, expectedSchema string, expectedSorts []apiquery.SortTerm) (Payload, error) {
	if len(token) == 0 || len(token) > c.maxEncodedBytes {
		return Payload{}, ErrInvalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Payload{}, ErrInvalid
	}
	if parts[0] != c.version {
		return Payload{}, ErrVersion
	}
	secret, exists := c.keys.key(parts[1])
	if !exists {
		return Payload{}, ErrInvalid
	}
	sealed, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Payload{}, ErrInvalid
	}
	aead, err := newAEAD(secret)
	if err != nil || len(sealed) < aead.NonceSize()+aead.Overhead() {
		return Payload{}, ErrInvalid
	}
	nonce, ciphertext := sealed[:aead.NonceSize()], sealed[aead.NonceSize():]
	plain, err := aead.Open(nil, nonce, ciphertext, []byte(parts[0]+"."+parts[1]))
	if err != nil || len(plain) > c.maxEncodedBytes {
		return Payload{}, ErrInvalid
	}
	var wire wirePayload
	decoder := json.NewDecoder(bytes.NewReader(plain))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return Payload{}, ErrInvalid
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return Payload{}, ErrInvalid
	}
	payload := Payload{SchemaRevision: wire.SchemaRevision, Direction: wire.Direction,
		Sorts: wire.Sorts, Positions: wire.Positions, ExpiresAt: wire.ExpiresAt,
		Policy: wire.Policy}
	now := c.now()
	if wire.Version != c.version || !c.validPayload(payload, now) {
		if !wire.ExpiresAt.After(now) {
			return Payload{}, ErrExpired
		}
		return Payload{}, ErrInvalid
	}
	if subtle.ConstantTimeCompare([]byte(wire.SchemaRevision), []byte(expectedSchema)) != 1 {
		return Payload{}, ErrSchema
	}
	if !equalSorts(wire.Sorts, expectedSorts) {
		return Payload{}, ErrSort
	}
	if c.replayGuard != nil {
		fingerprint := sha256.Sum256([]byte(token))
		c.replayMu.Lock()
		accepted := c.replayGuard(fingerprint, wire.ExpiresAt)
		c.replayMu.Unlock()
		if !accepted {
			return Payload{}, ErrReplay
		}
	}
	return payload, nil
}

// DecodeCursor implements apiquery.CursorDecoder using this authenticated
// cursor protocol.
func (c *Codec) DecodeCursor(_ context.Context, token, expectedSchema string,
	expectedSorts []apiquery.SortTerm) (apiquery.CursorState, error) {
	payload, err := c.Decode(token, expectedSchema, expectedSorts)
	if err != nil {
		return apiquery.CursorState{}, err
	}
	return apiquery.CursorState{Direction: payload.Direction,
		Positions: append([]apiquery.Value(nil), payload.Positions...), Policy: payload.Policy}, nil
}

type wirePayload struct {
	Version        string              `json:"version"`
	SchemaRevision string              `json:"schema_revision"`
	Direction      Direction           `json:"direction"`
	Sorts          []apiquery.SortTerm `json:"sorts"`
	Positions      []apiquery.Value    `json:"positions"`
	ExpiresAt      time.Time           `json:"expires_at"`
	Policy         string              `json:"policy,omitempty"`
}

func (c *Codec) validPayload(payload Payload, now time.Time) bool {
	if len(payload.SchemaRevision) == 0 || len(payload.SchemaRevision) > c.maxStringBytes ||
		len(payload.Policy) > c.maxStringBytes || len(payload.Positions) == 0 ||
		len(payload.Positions) > c.maxPositions || len(payload.Positions) != len(payload.Sorts) ||
		!payload.ExpiresAt.After(now) || payload.ExpiresAt.Sub(now) > c.maxTTL ||
		(payload.Direction != Forward && payload.Direction != Backward) {
		return false
	}
	for index, sort := range payload.Sorts {
		if len(sort.Name) == 0 || len(sort.Name) > c.maxStringBytes ||
			(sort.Direction != apiquery.Ascending && sort.Direction != apiquery.Descending) ||
			(sort.Nulls != "" && sort.Nulls != apiquery.NullsFirst && sort.Nulls != apiquery.NullsLast) ||
			len(payload.Positions[index].String()) > c.maxStringBytes || payload.Positions[index].Type() == "" {
			return false
		}
	}
	return true
}

func (c *Codec) now() time.Time {
	c.clockMu.RLock()
	clock := c.clock
	c.clockMu.RUnlock()
	return clock().UTC()
}

func newAEAD(secret []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(secret)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrInvalid
	}
	return nil
}

func equalSorts(left, right []apiquery.SortTerm) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func validKeyID(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') && char != '_' && char != '-' {
			return false
		}
	}
	return true
}
