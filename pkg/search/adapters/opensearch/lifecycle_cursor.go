package opensearch

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math"
	"time"
)

const (
	// MaximumReindexCursorKeyBytes bounds retained encryption-key material.
	MaximumReindexCursorKeyBytes = 4 << 10
	// MaximumReindexCursorBytes bounds encoded task-cursor parsing and retention.
	MaximumReindexCursorBytes = 16 << 10
	// MaximumReindexCursorTTL bounds replay lifetime for one backend task token.
	MaximumReindexCursorTTL = 24 * time.Hour
)

var (
	minimumReindexCursorTime = time.Unix(0, math.MinInt64)
	maximumReindexCursorTime = time.Unix(0, math.MaxInt64)
)

var (
	ErrInvalidReindexCursorCodec    = errors.New("search/opensearch: reindex cursor codec is invalid")
	ErrInvalidReindexCursor         = errors.New("search/opensearch: reindex cursor is invalid")
	ErrReindexCursorBinding         = errors.New("search/opensearch: reindex cursor belongs to another lifecycle operation")
	ErrReindexCursorExpired         = errors.New("search/opensearch: reindex cursor expired")
	ErrLifecycleCursorCodecRequired = errors.New("search/opensearch: reindex cursor codec is required")
)

type reindexCursorEnvelope struct {
	Version     uint8  `json:"v"`
	Tenant      string `json:"t"`
	Source      string `json:"s"`
	Target      string `json:"d"`
	Task        string `json:"k"`
	ExpiresUnix int64  `json:"e"`
}

// ReindexCursorCodec encrypts bounded backend task identifiers and binds them to
// one tenant and physical source/target pair. It is safe for concurrent use
// when the supplied clock is concurrency-safe.
type ReindexCursorCodec struct {
	aead     cipher.AEAD
	random   io.Reader
	now      func() time.Time
	maxBytes int
	ttl      time.Duration
}

// NewReindexCursorCodec constructs an expiring AES-256-GCM task cursor codec.
func NewReindexCursorCodec(key []byte, now func() time.Time, maxBytes int, ttl time.Duration) (*ReindexCursorCodec, error) {
	if len(key) < sha256.Size {
		return nil, ErrInvalidReindexCursorCodec
	}
	if len(key) > MaximumReindexCursorKeyBytes {
		return nil, ErrInvalidReindexCursorCodec
	}
	if now == nil {
		return nil, ErrInvalidReindexCursorCodec
	}
	if maxBytes <= 0 {
		return nil, ErrInvalidReindexCursorCodec
	}
	if maxBytes > MaximumReindexCursorBytes {
		return nil, ErrInvalidReindexCursorCodec
	}
	if ttl <= 0 {
		return nil, ErrInvalidReindexCursorCodec
	}
	if ttl > MaximumReindexCursorTTL {
		return nil, ErrInvalidReindexCursorCodec
	}
	derived := sha256.Sum256(key)
	block, _ := aes.NewCipher(derived[:])
	aead, _ := cipher.NewGCM(block)
	return &ReindexCursorCodec{aead: aead, random: rand.Reader, now: now, maxBytes: maxBytes, ttl: ttl}, nil
}

func (codec *ReindexCursorCodec) encode(tenant, source, target, task string) (string, error) {
	if !validReindexBinding(tenant, source, target) {
		return "", ErrInvalidReindexCursor
	}
	if task == "" {
		return "", ErrInvalidReindexCursor
	}
	if len(task) > 512 {
		return "", ErrInvalidReindexCursor
	}
	if containsUnsafePath(task) {
		return "", ErrInvalidReindexCursor
	}
	now := codec.now()
	expiresAt := now.Add(codec.ttl).UTC()
	if expiresAt.Before(minimumReindexCursorTime) {
		return "", ErrInvalidReindexCursor
	}
	if expiresAt.After(maximumReindexCursorTime) {
		return "", ErrInvalidReindexCursor
	}
	expires := expiresAt.UnixNano()
	payload, _ := json.Marshal(reindexCursorEnvelope{
		Version: 1, Tenant: tenant, Source: source, Target: target, Task: task,
		ExpiresUnix: expires,
	})
	return codec.seal(payload)
}

func (codec *ReindexCursorCodec) seal(payload []byte) (string, error) {
	nonce := make([]byte, codec.aead.NonceSize())
	if _, err := io.ReadFull(codec.random, nonce); err != nil {
		return "", ErrInvalidReindexCursor
	}
	sealed := codec.aead.Seal(nonce, nonce, payload, nil)
	token := base64.RawURLEncoding.EncodeToString(sealed)
	if len(token) > codec.maxBytes {
		return "", ErrInvalidReindexCursor
	}
	return token, nil
}

func (codec *ReindexCursorCodec) decode(token, tenant, source, target string) (string, error) {
	if len(token) > codec.maxBytes {
		return "", ErrInvalidReindexCursor
	}
	if !validReindexBinding(tenant, source, target) {
		return "", ErrInvalidReindexCursor
	}
	sealed, decodeErr := base64.RawURLEncoding.DecodeString(token)
	if decodeErr != nil {
		return "", ErrInvalidReindexCursor
	}
	if base64.RawURLEncoding.EncodeToString(sealed) != token {
		return "", ErrInvalidReindexCursor
	}
	nonce := make([]byte, codec.aead.NonceSize())
	nonceBytes := min(len(sealed), len(nonce))
	copy(nonce, sealed[:nonceBytes])
	ciphertext := sealed[nonceBytes:]
	payload, openErr := codec.aead.Open(nil, nonce, ciphertext, nil)
	if openErr != nil {
		return "", ErrInvalidReindexCursor
	}
	var envelope reindexCursorEnvelope
	if json.Unmarshal(payload, &envelope) != nil {
		return "", ErrInvalidReindexCursor
	}
	if envelope.Version != 1 {
		return "", ErrInvalidReindexCursor
	}
	if envelope.Task == "" {
		return "", ErrInvalidReindexCursor
	}
	if len(envelope.Task) > 512 {
		return "", ErrInvalidReindexCursor
	}
	if containsUnsafePath(envelope.Task) {
		return "", ErrInvalidReindexCursor
	}
	if envelope.Tenant != tenant {
		return "", ErrReindexCursorBinding
	}
	if envelope.Source != source {
		return "", ErrReindexCursorBinding
	}
	if envelope.Target != target {
		return "", ErrReindexCursorBinding
	}
	if !time.Unix(0, envelope.ExpiresUnix).After(codec.now()) {
		return "", ErrReindexCursorExpired
	}
	return envelope.Task, nil
}

func validReindexBinding(tenant, source, target string) bool {
	if !validLifecycleTenant(tenant) {
		return false
	}
	if !indexTargetPattern.MatchString(source) {
		return false
	}
	if !indexTargetPattern.MatchString(target) {
		return false
	}
	return source != target
}
