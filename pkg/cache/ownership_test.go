package cache_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	cache "github.com/faustbrian/golib/pkg/cache"
)

func TestSetIfOwnedUsesAtomicBackendCapability(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	backend := &ownershipBackend{recordingBackend: newRecordingBackend()}
	store := newStringCache(t, backend, fixedClock{now: now}, cache.TTLPolicy{
		TTL:      time.Minute,
		StaleFor: 30 * time.Second,
	})
	guard := ownershipGuard{key: "lease:key", owner: "worker", token: "42"}

	if err := store.SetIfOwned(context.Background(), "catalog", "fresh", guard); err != nil {
		t.Fatalf("SetIfOwned() error = %v", err)
	}
	record := backend.records[backendKey(t, "catalog")]
	if string(record.Payload) != "\x01\"fresh\"" ||
		!record.ExpiresAt.Equal(now.Add(time.Minute)) ||
		!record.StaleAt.Equal(now.Add(90*time.Second)) {
		t.Fatalf("protected record = %#v", record)
	}
	if backend.guard != guard {
		t.Fatalf("protected guard = %#v, want %#v", backend.guard, guard)
	}
}

func TestSetNegativeIfOwnedUsesAtomicBackendCapability(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	backend := &ownershipBackend{recordingBackend: newRecordingBackend()}
	store, err := cache.New(cache.Config[string, string]{
		Backend:  backend,
		Keys:     mustStringKeySpace(t),
		Codec:    cache.JSONCodec[string]{Version: 1},
		TTL:      cache.TTLPolicy{TTL: time.Minute},
		Clock:    fixedClock{now: now},
		MaxValue: 1024,
		Load: cache.LoadPolicy{
			NegativeTTL: 30 * time.Second,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	guard := ownershipGuard{key: "lease:key", owner: "worker", token: "42"}

	if err := store.SetNegativeIfOwned(
		context.Background(),
		"catalog",
		guard,
	); err != nil {
		t.Fatalf("SetNegativeIfOwned() error = %v", err)
	}
	record := backend.records[backendKey(t, "catalog")]
	if len(record.Payload) != 0 ||
		!record.Negative ||
		!record.ExpiresAt.Equal(now.Add(30*time.Second)) ||
		!record.StaleAt.Equal(now.Add(30*time.Second)) {
		t.Fatalf("protected negative record = %#v", record)
	}
	if backend.guard != guard {
		t.Fatalf("protected guard = %#v, want %#v", backend.guard, guard)
	}
}

func TestSetNegativeIfOwnedRequiresConfiguredTTL(t *testing.T) {
	t.Parallel()

	backend := &ownershipBackend{recordingBackend: newRecordingBackend()}
	store := newStringCache(
		t,
		backend,
		fixedClock{now: time.Now()},
		cache.TTLPolicy{TTL: time.Minute},
	)
	guard := ownershipGuard{key: "lease:key", owner: "worker", token: "42"}

	if err := store.SetNegativeIfOwned(
		t.Context(),
		"catalog",
		guard,
	); !errors.Is(err, cache.ErrInvalidPolicy) {
		t.Fatalf("SetNegativeIfOwned() error = %v", err)
	}
	if len(backend.records) != 0 {
		t.Fatal("invalid protected negative write mutated the cache")
	}
}

func TestSetIfOwnedFailsClosed(t *testing.T) {
	t.Parallel()

	guard := ownershipGuard{key: "lease:key", owner: "worker", token: "42"}
	unsupported := newStringCache(
		t,
		newRecordingBackend(),
		fixedClock{now: time.Now()},
		cache.TTLPolicy{TTL: time.Minute},
	)
	if err := unsupported.SetIfOwned(t.Context(), "catalog", "fresh", guard); !errors.Is(
		err,
		cache.ErrOwnershipUnsupported,
	) {
		t.Fatalf("SetIfOwned(unsupported) error = %v", err)
	}

	backend := &ownershipBackend{
		recordingBackend: newRecordingBackend(),
		err:              cache.ErrOwnershipLost,
	}
	store := newStringCache(
		t,
		backend,
		fixedClock{now: time.Now()},
		cache.TTLPolicy{TTL: time.Minute},
	)
	if err := store.SetIfOwned(t.Context(), "catalog", "fresh", guard); !errors.Is(
		err,
		cache.ErrOwnershipLost,
	) || !errors.Is(err, cache.ErrBackend) {
		t.Fatalf("SetIfOwned(lost) error = %v", err)
	}
	if len(backend.records) != 0 {
		t.Fatal("rejected protected write mutated the cache")
	}
}

func TestSetIfOwnedRejectsInvalidCallsBeforePublishing(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	guard := ownershipGuard{key: "lease:key", owner: "worker", token: "42"}
	backend := &ownershipBackend{recordingBackend: newRecordingBackend()}
	store := newStringCache(
		t,
		backend,
		fixedClock{now: time.Now()},
		cache.TTLPolicy{TTL: time.Minute},
	)
	if err := store.SetIfOwned(ctx, "catalog", "fresh", guard); !errors.Is(err, context.Canceled) {
		t.Fatalf("SetIfOwned(canceled) error = %v", err)
	}
	if err := store.SetIfOwned(t.Context(), "catalog", "fresh", nil); !errors.Is(
		err,
		cache.ErrInvalidPolicy,
	) {
		t.Fatalf("SetIfOwned(nil guard) error = %v", err)
	}
	if err := store.SetIfOwned(t.Context(), "catalog", "fresh", ownershipGuard{}); !errors.Is(
		err,
		cache.ErrInvalidPolicy,
	) {
		t.Fatalf("SetIfOwned(invalid guard) error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.SetIfOwned(t.Context(), "catalog", "fresh", guard); !errors.Is(err, cache.ErrClosed) {
		t.Fatalf("SetIfOwned(closed) error = %v", err)
	}

	space, err := cache.NewKeySpace(
		"test",
		"owned",
		1,
		failingKeyEncoder{err: errors.New("bad key")},
		128,
	)
	if err != nil {
		t.Fatal(err)
	}
	invalidKeyStore, err := cache.New(cache.Config[string, string]{
		Backend:  backend,
		Keys:     space,
		Codec:    cache.JSONCodec[string]{Version: 1},
		TTL:      cache.TTLPolicy{TTL: time.Minute},
		Clock:    fixedClock{now: time.Now()},
		MaxValue: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := invalidKeyStore.SetIfOwned(
		t.Context(),
		"catalog",
		"fresh",
		guard,
	); !errors.Is(err, cache.ErrInvalidKey) {
		t.Fatalf("SetIfOwned(invalid key) error = %v", err)
	}

	functionStore, err := cache.New(cache.Config[string, func()]{
		Backend:  backend,
		Keys:     mustStringKeySpace(t),
		Codec:    cache.JSONCodec[func()]{Version: 1},
		TTL:      cache.TTLPolicy{TTL: time.Minute},
		Clock:    fixedClock{now: time.Now()},
		MaxValue: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := functionStore.SetIfOwned(
		t.Context(),
		"catalog",
		func() {},
		guard,
	); !errors.Is(err, cache.ErrDecode) {
		t.Fatalf("SetIfOwned(codec) error = %v", err)
	}

	limited, err := cache.New(cache.Config[string, string]{
		Backend:  backend,
		Keys:     mustStringKeySpace(t),
		Codec:    cache.JSONCodec[string]{Version: 1},
		TTL:      cache.TTLPolicy{TTL: time.Minute},
		Clock:    fixedClock{now: time.Now()},
		MaxValue: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := limited.SetIfOwned(t.Context(), "catalog", "fresh", guard); !errors.Is(
		err,
		cache.ErrValueTooLarge,
	) {
		t.Fatalf("SetIfOwned(limit) error = %v", err)
	}

	overflow := newStringCache(
		t,
		backend,
		fixedClock{now: time.Unix(0, math.MaxInt64)},
		cache.TTLPolicy{TTL: time.Minute},
	)
	if err := overflow.SetIfOwned(t.Context(), "catalog", "fresh", guard); !errors.Is(
		err,
		cache.ErrInvalidRecord,
	) {
		t.Fatalf("SetIfOwned(deadline overflow) error = %v", err)
	}
}

func TestSetIfOwnedSupersedesActiveLocalLoad(t *testing.T) {
	t.Parallel()

	backend := &ownershipBackend{recordingBackend: newRecordingBackend()}
	store := newStringCache(
		t,
		backend,
		fixedClock{now: time.Now()},
		cache.TTLPolicy{TTL: time.Minute},
	)
	started := make(chan struct{})
	release := make(chan struct{})
	loaded := make(chan error, 1)
	go func() {
		_, err := store.GetOrLoad(t.Context(), "catalog", func(
			context.Context,
			string,
		) (cache.LoadResult[string], error) {
			close(started)
			<-release
			return cache.LoadResult[string]{Value: "obsolete", Found: true}, nil
		})
		loaded <- err
	}()
	<-started
	guard := ownershipGuard{key: "lease:key", owner: "worker", token: "42"}
	if err := store.SetIfOwned(t.Context(), "catalog", "fresh", guard); err != nil {
		t.Fatalf("SetIfOwned() error = %v", err)
	}
	close(release)
	if err := <-loaded; err != nil {
		t.Fatalf("GetOrLoad() error = %v", err)
	}
	result, err := store.Get(t.Context(), "catalog")
	if err != nil || result.State != cache.Hit || result.Value != "fresh" {
		t.Fatalf("protected mutation was resurrected: result=%#v err=%v", result, err)
	}
}

func TestSetNegativeIfOwnedSupersedesActiveLocalLoad(t *testing.T) {
	t.Parallel()

	backend := &ownershipBackend{recordingBackend: newRecordingBackend()}
	store, err := cache.New(cache.Config[string, string]{
		Backend:  backend,
		Keys:     mustStringKeySpace(t),
		Codec:    cache.JSONCodec[string]{Version: 1},
		TTL:      cache.TTLPolicy{TTL: time.Minute},
		Clock:    cache.SystemClock{},
		MaxValue: 1024,
		Load: cache.LoadPolicy{
			NegativeTTL: 30 * time.Second,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	loaded := make(chan error, 1)
	go func() {
		_, err := store.GetOrLoad(t.Context(), "catalog", func(
			context.Context,
			string,
		) (cache.LoadResult[string], error) {
			close(started)
			<-release
			return cache.LoadResult[string]{
				Value: "obsolete",
				Found: true,
			}, nil
		})
		loaded <- err
	}()
	<-started
	guard := ownershipGuard{key: "lease:key", owner: "worker", token: "42"}
	if err := store.SetNegativeIfOwned(
		t.Context(),
		"catalog",
		guard,
	); err != nil {
		t.Fatalf("SetNegativeIfOwned() error = %v", err)
	}
	close(release)
	if err := <-loaded; err != nil {
		t.Fatalf("GetOrLoad() error = %v", err)
	}
	result, err := store.Get(t.Context(), "catalog")
	if err != nil || result.State != cache.Miss || !result.Negative {
		t.Fatalf(
			"protected negative mutation was resurrected: result=%#v err=%v",
			result,
			err,
		)
	}
}

type ownershipGuard struct {
	key   string
	owner string
	token string
}

func (guard ownershipGuard) StorageKey() string { return guard.key }

func (guard ownershipGuard) Owner() string { return guard.owner }

func (guard ownershipGuard) Token() string { return guard.token }

type ownershipBackend struct {
	*recordingBackend
	guard cache.OwnershipGuard
	err   error
}

func (backend *ownershipBackend) SetIfOwned(
	ctx context.Context,
	key string,
	record cache.Record,
	guard cache.OwnershipGuard,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if backend.err != nil {
		return backend.err
	}
	backend.guard = guard
	backend.records[key] = record.Clone()
	return nil
}
