package httpsignature

import (
	"container/heap"
	"context"
	"errors"
	"strings"
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

// MemoryReplayConfig defines mandatory resource bounds and the caller-owned
// clock for a process-local replay store. String limits count bytes, not
// Unicode code points. It is not a distributed replay guarantee.
type MemoryReplayConfig struct {
	// Capacity is the maximum number of retained key and nonce identities.
	Capacity int
	// MaxTTL is the longest accepted interval from Now to ExpiresAt.
	MaxTTL time.Duration
	// MaxKeyIDBytes and MaxNonceBytes bound caller-controlled identity storage.
	MaxKeyIDBytes int
	MaxNonceBytes int
	// Now must be safe for concurrent use and must return promptly. The store
	// never invokes it while holding replay state.
	Now func() time.Time
}

// MemoryReplayStore is a bounded process-local ReplayStore. It starts no
// goroutines. Consume removes at most one unrelated expired identity and uses
// an expiration heap, so cleanup work is bounded and never scans all records.
// Concurrent clock observations are clamped so store time never moves
// backward. Copies refer to the same synchronized replay state.
type MemoryReplayStore struct {
	mu            *replayMutex
	state         *memoryReplayState
	capacity      int
	maxTTL        time.Duration
	maxKeyIDBytes int
	maxNonceBytes int
	now           func() time.Time
}

type memoryReplayState struct {
	entries   map[replayIdentity]*replayEntry
	expiry    replayExpiryHeap
	latestNow time.Time
}

type replayIdentity struct {
	keyID string
	nonce string
}

type replayMutex struct {
	token chan struct{}
}

func newReplayMutex() *replayMutex {
	token := make(chan struct{}, 1)
	token <- struct{}{}
	return &replayMutex{token: token}
}

func (mutex *replayMutex) Lock() {
	<-mutex.token
}

func (mutex *replayMutex) Unlock() {
	select {
	case mutex.token <- struct{}{}:
	default:
		panic("http signature: unlock of unlocked replay mutex")
	}
}

func (mutex *replayMutex) TryLock() bool {
	select {
	case <-mutex.token:
		return true
	default:
		return false
	}
}

func (mutex *replayMutex) lockContext(done <-chan struct{}) bool {
	select {
	case <-done:
		return false
	case <-mutex.token:
		return mutex.keepLockUnlessCanceled(done)
	}
}

func (mutex *replayMutex) keepLockUnlessCanceled(done <-chan struct{}) bool {
	select {
	case <-done:
		mutex.Unlock()
		return false
	default:
		return true
	}
}

type replayEntry struct {
	identity  replayIdentity
	expiresAt time.Time
	index     int
}

type replayExpiryHeap []*replayEntry

func (expiry replayExpiryHeap) Len() int { return len(expiry) }

func (expiry replayExpiryHeap) Less(left, right int) bool {
	return expiry[left].expiresAt.Before(expiry[right].expiresAt)
}

func (expiry replayExpiryHeap) Swap(left, right int) {
	expiry[left], expiry[right] = expiry[right], expiry[left]
	expiry[left].index = left
	expiry[right].index = right
}

func (expiry *replayExpiryHeap) Push(value any) {
	entry := value.(*replayEntry)
	entry.index = len(*expiry)
	*expiry = append(*expiry, entry)
}

func (expiry *replayExpiryHeap) Pop() any {
	last := len(*expiry) - 1
	entry := (*expiry)[last]
	(*expiry)[last] = nil
	*expiry = (*expiry)[:last]
	entry.index = -1
	return entry
}

// NewMemoryReplayStore constructs a process-local store only when all resource
// bounds and the clock are explicitly supplied.
func NewMemoryReplayStore(config MemoryReplayConfig) (*MemoryReplayStore, error) {
	if config.Capacity <= 0 || config.MaxTTL <= 0 || config.MaxKeyIDBytes <= 0 ||
		config.MaxNonceBytes <= 0 || config.Now == nil {
		return nil, ErrInvalidReplayConfig
	}

	return &MemoryReplayStore{
		mu:            newReplayMutex(),
		state:         &memoryReplayState{entries: make(map[replayIdentity]*replayEntry)},
		capacity:      config.Capacity,
		maxTTL:        config.MaxTTL,
		maxKeyIDBytes: config.MaxKeyIDBytes,
		maxNonceBytes: config.MaxNonceBytes,
		now:           config.Now,
	}, nil
}

// Consume atomically reserves record until its expiration. It performs a
// constant number of O(log Capacity) heap operations and never scans every
// retained identity. Caller-owned clock and context methods run without the
// replay lock held. Cancellation observed while waiting for replay state
// returns the context error without consuming the record; cancellation racing
// after lock acquisition may lose to a successful reservation.
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
	if store.mu == nil || store.state == nil || store.state.entries == nil || store.capacity <= 0 ||
		store.maxTTL <= 0 || store.maxKeyIDBytes <= 0 || store.maxNonceBytes <= 0 || store.now == nil {
		return ErrInvalidReplayConfig
	}
	if record.KeyID == "" || len(record.KeyID) > store.maxKeyIDBytes ||
		record.Nonce == "" || len(record.Nonce) > store.maxNonceBytes || record.ExpiresAt.IsZero() {
		return ErrInvalidReplayRecord
	}
	now := store.now()
	if err := ctx.Err(); err != nil {
		return err
	}
	done := ctx.Done()

	if !store.mu.lockContext(done) {
		return replayContextError(ctx)
	}
	defer store.mu.Unlock()

	if now.Before(store.state.latestNow) {
		now = store.state.latestNow
	} else {
		store.state.latestNow = now
	}
	if !record.ExpiresAt.After(now) || record.ExpiresAt.Sub(now) > store.maxTTL {
		return ErrInvalidReplayRecord
	}

	store.removeOldestExpired(now)

	identity := replayIdentity{keyID: record.KeyID, nonce: record.Nonce}
	if existing, exists := store.state.entries[identity]; exists {
		if existing.expiresAt.After(now) {
			return ErrReplayDetected
		}
		existing.expiresAt = record.ExpiresAt
		heap.Fix(&store.state.expiry, existing.index)
		return nil
	}
	if len(store.state.entries) >= store.capacity {
		return ErrReplayCapacity
	}

	identity.keyID = strings.Clone(identity.keyID)
	identity.nonce = strings.Clone(identity.nonce)
	entry := &replayEntry{identity: identity, expiresAt: record.ExpiresAt}
	store.state.entries[identity] = entry
	heap.Push(&store.state.expiry, entry)

	return nil
}

func replayContextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return context.Canceled
}

func (store *MemoryReplayStore) removeOldestExpired(now time.Time) {
	if len(store.state.expiry) == 0 || store.state.expiry[0].expiresAt.After(now) {
		return
	}
	entry := heap.Pop(&store.state.expiry).(*replayEntry)
	delete(store.state.entries, entry.identity)
}
