package httpsignature

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestZeroValueMemoryReplayStoreFailsClosed(t *testing.T) {
	t.Parallel()

	var store MemoryReplayStore
	err := store.Consume(context.Background(), ReplayRecord{
		KeyID: "key", Nonce: "nonce", ExpiresAt: time.Now().Add(time.Minute),
	})
	if !errors.Is(err, ErrInvalidReplayConfig) {
		t.Fatalf("Consume() error = %v, want ErrInvalidReplayConfig", err)
	}
}

func TestMemoryReplayStoreEnforcesOpaqueIdentityByteBounds(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	store, err := NewMemoryReplayStore(MemoryReplayConfig{
		Capacity: 4, MaxTTL: time.Minute, MaxKeyIDBytes: 3, MaxNonceBytes: 4,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, record := range []ReplayRecord{
		{KeyID: "éé", Nonce: "one", ExpiresAt: now.Add(time.Minute)},
		{KeyID: "key", Nonce: "12345", ExpiresAt: now.Add(time.Minute)},
	} {
		if err := store.Consume(context.Background(), record); !errors.Is(err, ErrInvalidReplayRecord) {
			t.Fatalf("Consume(%#v) error = %v, want ErrInvalidReplayRecord", record, err)
		}
	}

	if err := store.Consume(context.Background(), ReplayRecord{
		KeyID: "key", Nonce: "1234", ExpiresAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("Consume(record at exact byte bounds) error = %v", err)
	}
}

func TestMemoryReplayStoreCleanupWorkIsBoundedPerConsume(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	const capacity = 8
	store, err := NewMemoryReplayStore(MemoryReplayConfig{
		Capacity: capacity, MaxTTL: time.Minute, MaxKeyIDBytes: 64, MaxNonceBytes: 64,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := range capacity {
		record := ReplayRecord{
			KeyID: "key", Nonce: fmt.Sprintf("expired-%d", index), ExpiresAt: now.Add(time.Second),
		}
		if err := store.Consume(context.Background(), record); err != nil {
			t.Fatalf("seed Consume(%d) error = %v", index, err)
		}
	}

	now = now.Add(2 * time.Second)
	for index := range capacity {
		record := ReplayRecord{
			KeyID: "key", Nonce: fmt.Sprintf("fresh-%d", index), ExpiresAt: now.Add(time.Minute),
		}
		if err := store.Consume(context.Background(), record); err != nil {
			t.Fatalf("replacement Consume(%d) error = %v", index, err)
		}

		store.mu.Lock()
		entries := len(store.state.entries)
		store.mu.Unlock()
		if entries != capacity {
			t.Fatalf("entries after replacement %d = %d, want %d; cleanup must remove at most one record", index, entries, capacity)
		}
	}

	if err := store.Consume(context.Background(), ReplayRecord{
		KeyID: "key", Nonce: "over-capacity", ExpiresAt: now.Add(time.Minute),
	}); !errors.Is(err, ErrReplayCapacity) {
		t.Fatalf("Consume() after replacing expired capacity error = %v, want ErrReplayCapacity", err)
	}
}

func TestMemoryReplayStoreReusesAnExpiredIdentityWithoutScanningPeers(t *testing.T) {
	t.Parallel()

	base := time.Unix(1_700_000_000, 0)
	now := base
	store, err := NewMemoryReplayStore(MemoryReplayConfig{
		Capacity: 3, MaxTTL: time.Minute, MaxKeyIDBytes: 64, MaxNonceBytes: 64,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range []ReplayRecord{
		{KeyID: "key", Nonce: "oldest", ExpiresAt: base.Add(time.Second)},
		{KeyID: "key", Nonce: "target", ExpiresAt: base.Add(2 * time.Second)},
		{KeyID: "key", Nonce: "live", ExpiresAt: base.Add(10 * time.Second)},
	} {
		if err := store.Consume(context.Background(), record); err != nil {
			t.Fatal(err)
		}
	}

	now = base.Add(3 * time.Second)
	target := ReplayRecord{KeyID: "key", Nonce: "target", ExpiresAt: now.Add(time.Minute)}
	if err := store.Consume(context.Background(), target); err != nil {
		t.Fatalf("Consume(expired target behind older peer) error = %v", err)
	}
	if err := store.Consume(context.Background(), target); !errors.Is(err, ErrReplayDetected) {
		t.Fatalf("second Consume(reused target) error = %v, want ErrReplayDetected", err)
	}

	store.mu.Lock()
	entries, expirations := len(store.state.entries), len(store.state.expiry)
	store.mu.Unlock()
	if entries != 2 || expirations != entries {
		t.Fatalf("retained entries = %d, expiration nodes = %d, want two synchronized records", entries, expirations)
	}
}

func TestMemoryReplayStoreDoesNotInvokeContextCallbacksUnderLock(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	store, err := NewMemoryReplayStore(MemoryReplayConfig{
		Capacity: 1, MaxTTL: time.Minute, MaxKeyIDBytes: 64, MaxNonceBytes: 64,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := &replayLockCheckingContext{Context: context.Background(), store: store}
	if err := store.Consume(ctx, ReplayRecord{
		KeyID: "key", Nonce: "nonce", ExpiresAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if ctx.callbackUnderLock.Load() {
		t.Fatal("Consume invoked a caller-owned context callback while holding replay state")
	}
}

func TestMemoryReplayStoreCancellationDoesNotWaitForContendedState(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	clockRead := make(chan struct{})
	var signalClock sync.Once
	store, err := NewMemoryReplayStore(MemoryReplayConfig{
		Capacity: 1, MaxTTL: time.Minute, MaxKeyIDBytes: 64, MaxNonceBytes: 64,
		Now: func() time.Time {
			signalClock.Do(func() { close(clockRead) })
			return now
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	store.mu.Lock()
	locked := true
	defer func() {
		if locked {
			store.mu.Unlock()
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- store.Consume(ctx, ReplayRecord{
			KeyID: "key", Nonce: "nonce", ExpiresAt: now.Add(time.Minute),
		})
	}()
	<-clockRead
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("contended Consume() error = %v, want context.Canceled", err)
		}
		store.mu.Unlock()
		locked = false
		if err := store.Consume(context.Background(), ReplayRecord{
			KeyID: "key", Nonce: "nonce", ExpiresAt: now.Add(time.Minute),
		}); err != nil {
			t.Fatalf("Consume() after canceled waiter error = %v", err)
		}
	case <-time.After(time.Second):
		store.mu.Unlock()
		locked = false
		finalErr := <-done
		t.Fatalf("canceled Consume waited for contended replay state; final error = %v", finalErr)
	}
}

func TestMemoryReplayStoreCancellationFromClockDoesNotConsumeRecord(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	ctx, cancel := context.WithCancel(context.Background())
	var cancelFromClock sync.Once
	store, err := NewMemoryReplayStore(MemoryReplayConfig{
		Capacity: 1, MaxTTL: time.Minute, MaxKeyIDBytes: 64, MaxNonceBytes: 64,
		Now: func() time.Time {
			cancelFromClock.Do(cancel)
			return now
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	record := ReplayRecord{KeyID: "key", Nonce: "nonce", ExpiresAt: now.Add(time.Minute)}
	if err := store.Consume(ctx, record); !errors.Is(err, context.Canceled) {
		t.Fatalf("Consume() canceled by clock error = %v, want context.Canceled", err)
	}
	if err := store.Consume(context.Background(), record); err != nil {
		t.Fatalf("Consume() after clock cancellation error = %v", err)
	}
}

func TestMemoryReplayStoreFailsClosedForAnInconsistentCanceledContext(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	store, err := NewMemoryReplayStore(MemoryReplayConfig{
		Capacity: 1, MaxTTL: time.Minute, MaxKeyIDBytes: 64, MaxNonceBytes: 64,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := &closedDoneContext{Context: context.Background(), done: make(chan struct{})}
	close(ctx.done)
	record := ReplayRecord{KeyID: "key", Nonce: "nonce", ExpiresAt: now.Add(time.Minute)}
	if err := store.Consume(ctx, record); !errors.Is(err, context.Canceled) {
		t.Fatalf("Consume(inconsistent canceled context) error = %v, want context.Canceled", err)
	}
	if err := store.Consume(context.Background(), record); err != nil {
		t.Fatalf("Consume() after rejected inconsistent context error = %v", err)
	}
}

func TestReplayMutexCancellationAndOwnershipBoundaries(t *testing.T) {
	t.Parallel()

	mutex := newReplayMutex()
	if !mutex.TryLock() {
		t.Fatal("TryLock() did not acquire available replay state")
	}
	if mutex.TryLock() {
		t.Fatal("TryLock() acquired already-held replay state")
	}
	mutex.Unlock()

	mutex.Lock()
	canceled := make(chan struct{})
	close(canceled)
	if mutex.keepLockUnlessCanceled(canceled) {
		t.Fatal("keepLockUnlessCanceled() retained state after cancellation")
	}

	mutex.Lock()
	if mutex.lockContext(canceled) {
		t.Fatal("lockContext() acquired state for a canceled caller")
	}
	mutex.Unlock()

	defer func() {
		if recover() == nil {
			t.Fatal("Unlock() did not reject replay state without an owner")
		}
	}()
	mutex.Unlock()
}

func TestMemoryReplayStoreNeverMovesObservedTimeBackward(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	store, err := NewMemoryReplayStore(MemoryReplayConfig{
		Capacity: 2, MaxTTL: time.Minute, MaxKeyIDBytes: 64, MaxNonceBytes: 64,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	original := ReplayRecord{KeyID: "key", Nonce: "nonce", ExpiresAt: now.Add(2 * time.Second)}
	if err := store.Consume(context.Background(), original); err != nil {
		t.Fatal(err)
	}

	now = now.Add(3 * time.Second)
	if err := store.Consume(context.Background(), ReplayRecord{
		KeyID: "key", Nonce: "later", ExpiresAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(-2 * time.Second)
	if err := store.Consume(context.Background(), original); !errors.Is(err, ErrInvalidReplayRecord) {
		t.Fatalf("Consume() after clock rollback error = %v, want ErrInvalidReplayRecord", err)
	}
}

func TestMemoryReplayStoreCopiesShareSynchronizedReplayState(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	store, err := NewMemoryReplayStore(MemoryReplayConfig{
		Capacity: 1, MaxTTL: time.Minute, MaxKeyIDBytes: 64, MaxNonceBytes: 64,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	storeCopy := *store
	record := ReplayRecord{KeyID: "key", Nonce: "nonce", ExpiresAt: now.Add(time.Second)}
	if err := store.Consume(context.Background(), record); err != nil {
		t.Fatal(err)
	}

	now = now.Add(2 * time.Second)
	record.ExpiresAt = now.Add(time.Minute)
	if err := storeCopy.Consume(context.Background(), record); err != nil {
		t.Fatalf("copied store Consume() after expiration error = %v", err)
	}
	if err := store.Consume(context.Background(), record); !errors.Is(err, ErrReplayDetected) {
		t.Fatalf("original store after copied Consume() error = %v, want ErrReplayDetected", err)
	}
}

type replayLockCheckingContext struct {
	context.Context
	store             *MemoryReplayStore
	callbackUnderLock atomic.Bool
}

type closedDoneContext struct {
	context.Context
	done chan struct{}
}

func (ctx *closedDoneContext) Done() <-chan struct{} { return ctx.done }

func (*closedDoneContext) Err() error { return nil }

func (ctx *replayLockCheckingContext) Err() error {
	ctx.checkLock()
	return nil
}

func (ctx *replayLockCheckingContext) Done() <-chan struct{} {
	ctx.checkLock()
	return ctx.Context.Done()
}

func (ctx *replayLockCheckingContext) checkLock() {
	if ctx.store.mu.TryLock() {
		ctx.store.mu.Unlock()
	} else {
		ctx.callbackUnderLock.Store(true)
	}
}
