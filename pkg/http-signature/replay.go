package httpsignature

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	// ErrReplayDetected reports that the same key and nonce pair was already
	// consumed within its validity window.
	ErrReplayDetected = errors.New("http signature: replay detected")
	// ErrReplayCapacity reports that a bounded replay backend cannot accept a
	// new record. Callers must fail verification closed.
	ErrReplayCapacity = errors.New("http signature: replay store capacity exceeded")
	// ErrInvalidReplayRecord reports an incomplete or out-of-policy record.
	ErrInvalidReplayRecord = errors.New("http signature: invalid replay record")
	// ErrInvalidReplayConfig reports missing or non-positive resource bounds.
	ErrInvalidReplayConfig = errors.New("http signature: invalid replay store configuration")
)

// ReplayRecord identifies one verification attempt until ExpiresAt. KeyID and
// Nonce are treated as opaque values and are never included in errors.
type ReplayRecord struct {
	KeyID     string
	Nonce     string
	ExpiresAt time.Time
}

// ReplayStore atomically consumes a signature nonce. A durable implementation
// must make concurrent Consume calls for the same key and nonce linearizable:
// exactly one call succeeds before expiration and all others return
// ErrReplayDetected. Unknown backend outcomes must return an error, never nil.
type ReplayStore interface {
	Consume(context.Context, ReplayRecord) error
}

// MemoryReplayConfig defines mandatory bounds and the caller-owned clock for a
// process-local replay store. It is not a distributed replay guarantee.
type MemoryReplayConfig struct {
	Capacity int
	MaxTTL   time.Duration
	Now      func() time.Time
}

// MemoryReplayStore is a bounded process-local ReplayStore. It starts no
// goroutines; expired records are removed synchronously during Consume.
type MemoryReplayStore struct {
	mu       sync.Mutex
	entries  map[replayIdentity]time.Time
	capacity int
	maxTTL   time.Duration
	now      func() time.Time
}

type replayIdentity struct {
	keyID string
	nonce string
}

// NewMemoryReplayStore constructs a process-local store only when all resource
// bounds and the clock are explicitly supplied.
func NewMemoryReplayStore(config MemoryReplayConfig) (*MemoryReplayStore, error) {
	if config.Capacity <= 0 || config.MaxTTL <= 0 || config.Now == nil {
		return nil, ErrInvalidReplayConfig
	}

	return &MemoryReplayStore{
		entries:  make(map[replayIdentity]time.Time, config.Capacity),
		capacity: config.Capacity,
		maxTTL:   config.MaxTTL,
		now:      config.Now,
	}, nil
}

// Consume atomically reserves record until its expiration. Cancellation is
// checked before acquiring shared state and again while holding the lock.
func (store *MemoryReplayStore) Consume(ctx context.Context, record ReplayRecord) error {
	if ctx == nil {
		return errors.New("http signature: nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if store == nil {
		return ErrInvalidReplayConfig
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	now := store.now()
	if record.KeyID == "" || record.Nonce == "" || record.ExpiresAt.IsZero() ||
		!record.ExpiresAt.After(now) || record.ExpiresAt.Sub(now) > store.maxTTL {
		return ErrInvalidReplayRecord
	}

	for identity, expiresAt := range store.entries {
		if !expiresAt.After(now) {
			delete(store.entries, identity)
		}
	}

	identity := replayIdentity{keyID: record.KeyID, nonce: record.Nonce}
	if _, exists := store.entries[identity]; exists {
		return ErrReplayDetected
	}
	if len(store.entries) >= store.capacity {
		return ErrReplayCapacity
	}

	store.entries[identity] = record.ExpiresAt

	return nil
}
