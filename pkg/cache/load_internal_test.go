package cache

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestWaiterAccountingControlsAdmissionAndReleasesCapacity(t *testing.T) {
	t.Parallel()

	store := newInternalLoadingCache(t, &internalMemoryBackend{})
	key, err := store.keys.Key("key")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	loader := func(ctx context.Context, _ string) (LoadResult[string], error) {
		startedOnce.Do(func() { close(started) })
		select {
		case <-release:
			return LoadResult[string]{Value: "loaded", Found: true}, nil
		case <-ctx.Done():
			return LoadResult[string]{}, ctx.Err()
		}
	}

	leaderDone := make(chan error, 1)
	go func() {
		_, err := store.GetOrLoad(t.Context(), "key", loader)
		leaderDone <- err
	}()
	waitInternalSignal(t, started)

	followerCtx, cancelFollower := context.WithCancel(t.Context())
	followerDone := make(chan error, 1)
	go func() {
		_, err := store.GetOrLoad(followerCtx, "key", loader)
		followerDone <- err
	}()
	waitForInternalWaiters(t, store, key, 2)

	if _, err := store.GetOrLoad(t.Context(), "key", loader); !errors.Is(err, ErrWaiterLimit) {
		t.Fatalf("overflow waiter returned %v, want ErrWaiterLimit", err)
	}

	cancelFollower()
	if err := <-followerDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled follower returned %v", err)
	}
	waitForInternalWaiters(t, store, key, 1)

	replacementDone := make(chan error, 1)
	go func() {
		_, err := store.GetOrLoad(t.Context(), "key", loader)
		replacementDone <- err
	}()
	waitForInternalWaiters(t, store, key, 2)

	close(release)
	if err := <-leaderDone; err != nil {
		t.Fatalf("leader returned %v", err)
	}
	if err := <-replacementDone; err != nil {
		t.Fatalf("replacement returned %v", err)
	}
}

func TestRecursiveLoadOwnershipIsScopedToOneCache(t *testing.T) {
	t.Parallel()

	first := newInternalLoadingCache(t, &internalMemoryBackend{})
	second := newInternalLoadingCache(t, &internalMemoryBackend{})

	result, err := first.GetOrLoad(t.Context(), "outer", func(ctx context.Context, _ string) (LoadResult[string], error) {
		nested, err := second.GetOrLoad(ctx, "inner", func(context.Context, string) (LoadResult[string], error) {
			return LoadResult[string]{Value: "nested", Found: true}, nil
		})
		return LoadResult[string]{Value: nested.Value, Found: nested.State == Hit}, err
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != Hit || result.Value != "nested" {
		t.Fatalf("cross-cache load returned %#v", result)
	}
}

func newInternalLoadingCache(t *testing.T, backend Backend) *Cache[string, string] {
	t.Helper()
	keys, err := NewKeySpace("test", "internal", 1, StringKeyEncoder{}, 128)
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(Config[string, string]{
		Backend:  backend,
		Keys:     keys,
		Codec:    JSONCodec[string]{Version: 1},
		TTL:      TTLPolicy{TTL: time.Minute},
		Clock:    SystemClock{},
		MaxValue: 1024,
		Load: LoadPolicy{
			MaxConcurrent:    1,
			MaxWaitersPerKey: 2,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return store
}

func waitForInternalWaiters(
	t *testing.T,
	store *Cache[string, string],
	key string,
	want int,
) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		store.loadMu.Lock()
		flight := store.flights[key]
		got := 0
		if flight != nil {
			got = flight.waiters
		}
		store.loadMu.Unlock()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiters = %d, want %d", got, want)
		}
		time.Sleep(time.Millisecond)
	}
}

func waitInternalSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for loader")
	}
}

type internalMemoryBackend struct {
	mu      sync.Mutex
	records map[string]Record
}

func (b *internalMemoryBackend) Get(ctx context.Context, key string) (Record, bool, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	record, found := b.records[key]
	return record.Clone(), found, nil
}

func (b *internalMemoryBackend) Set(
	ctx context.Context,
	key string,
	record Record,
	condition Condition,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.records == nil {
		b.records = make(map[string]Record)
	}
	_, found := b.records[key]
	if condition == IfAbsent && found || condition == IfPresent && !found {
		return false, nil
	}
	b.records[key] = record.Clone()
	return true, nil
}

func (b *internalMemoryBackend) Delete(ctx context.Context, key string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	_, found := b.records[key]
	delete(b.records, key)
	return found, nil
}
