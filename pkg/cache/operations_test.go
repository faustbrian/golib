package cache_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	cache "github.com/faustbrian/golib/pkg/cache"
)

func TestGetDistinguishesMissHitStaleDecodeAndBackendFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	backend := newRecordingBackend()
	store := newStringCache(t, backend, fixedClock{now: now}, cache.TTLPolicy{TTL: time.Minute, StaleFor: time.Minute})

	miss, err := store.Get(context.Background(), "missing")
	if err != nil || miss.State != cache.Miss {
		t.Fatalf("expected explicit miss, got result=%#v err=%v", miss, err)
	}

	backend.records[backendKey(t, "zero")] = cache.Record{
		Payload:   []byte{1, '"', '"'},
		ExpiresAt: now.Add(time.Minute),
		StaleAt:   now.Add(2 * time.Minute),
	}
	hit, err := store.Get(context.Background(), "zero")
	if err != nil || hit.State != cache.Hit || hit.Value != "" {
		t.Fatalf("expected stored zero-value hit, got result=%#v err=%v", hit, err)
	}

	backend.records[backendKey(t, "stale")] = cache.Record{
		Payload:   []byte{1, '"', 'o', 'l', 'd', '"'},
		ExpiresAt: now.Add(-time.Second),
		StaleAt:   now.Add(time.Minute),
	}
	stale, err := store.Get(context.Background(), "stale")
	if err != nil || stale.State != cache.Stale || stale.Value != "old" {
		t.Fatalf("expected stale value, got result=%#v err=%v", stale, err)
	}

	backend.records[backendKey(t, "invalid")] = cache.Record{
		Payload:   []byte{1, '{'},
		ExpiresAt: now.Add(time.Minute),
		StaleAt:   now.Add(2 * time.Minute),
	}
	_, err = store.Get(context.Background(), "invalid")
	if !errors.Is(err, cache.ErrDecode) || errors.Is(err, cache.ErrMiss) {
		t.Fatalf("decode failure must remain distinct from miss: %v", err)
	}

	backend.getErr = errors.New("backend unavailable")
	_, err = store.Get(context.Background(), "anything")
	if !errors.Is(err, cache.ErrBackend) || errors.Is(err, cache.ErrMiss) {
		t.Fatalf("backend failure must remain distinct from miss: %v", err)
	}
}

func TestGetRejectsCorruptBackendRecords(t *testing.T) {
	t.Parallel()

	backend := newRecordingBackend()
	backend.records[backendKey(t, "corrupt")] = cache.Record{Payload: []byte{1, '"', 'x', '"'}}
	store := newStringCache(t, backend, fixedClock{now: time.Now()}, cache.TTLPolicy{TTL: time.Minute})

	_, err := store.Get(context.Background(), "corrupt")
	if !errors.Is(err, cache.ErrBackend) || !errors.Is(err, cache.ErrInvalidRecord) {
		t.Fatalf("corrupt backend record returned %v, want backend and invalid record errors", err)
	}
}

func TestGetDistinguishesNilPointerHitFromEmptyPayload(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	backend := newRecordingBackend()
	space := mustStringKeySpace(t)
	store, err := cache.New(cache.Config[string, *testPayload]{
		Backend:  backend,
		Keys:     space,
		Codec:    cache.JSONCodec[*testPayload]{Version: 1},
		TTL:      cache.TTLPolicy{TTL: time.Minute},
		Clock:    fixedClock{now: now},
		MaxValue: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(t.Context(), "nil", nil); err != nil {
		t.Fatal(err)
	}
	result, err := store.Get(t.Context(), "nil")
	if err != nil || result.State != cache.Hit || result.Value != nil {
		t.Fatalf("nil pointer hit: result=%#v err=%v", result, err)
	}

	backend.records[backendKey(t, "empty")] = cache.Record{
		ExpiresAt: now.Add(time.Minute),
		StaleAt:   now.Add(time.Minute),
	}
	if _, err := store.Get(t.Context(), "empty"); !errors.Is(err, cache.ErrSchemaMismatch) {
		t.Fatalf("empty payload returned %v, want schema mismatch", err)
	}
}

func TestGetHandlesExpirationAndSlidingWriteRaces(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	validPayload := []byte{1, '"', 'v', 'a', 'l', 'u', 'e', '"'}

	t.Run("hard expiration delete failure", func(t *testing.T) {
		backend := newRecordingBackend()
		key := backendKey(t, "expired")
		backend.records[key] = cache.Record{
			Payload: validPayload, ExpiresAt: now.Add(-2 * time.Minute), StaleAt: now.Add(-time.Minute),
		}
		cause := errors.New("delete unavailable")
		backend.deleteErrors = map[string]error{key: cause}
		store := newStringCache(t, backend, fixedClock{now: now}, cache.TTLPolicy{TTL: time.Minute})

		_, err := store.Get(context.Background(), "expired")
		if !errors.Is(err, cache.ErrBackend) || !errors.Is(err, cause) {
			t.Fatalf("expiration delete returned %v", err)
		}
	})

	t.Run("oversized backend payload", func(t *testing.T) {
		backend := newRecordingBackend()
		backend.records[backendKey(t, "large")] = cache.Record{
			Payload: make([]byte, 1025), ExpiresAt: now.Add(time.Minute), StaleAt: now.Add(time.Minute),
		}
		store := newStringCache(t, backend, fixedClock{now: now}, cache.TTLPolicy{TTL: time.Minute})

		_, err := store.Get(context.Background(), "large")
		if !errors.Is(err, cache.ErrValueTooLarge) {
			t.Fatalf("oversized payload returned %v", err)
		}
	})

	t.Run("schema mismatch", func(t *testing.T) {
		backend := newRecordingBackend()
		backend.records[backendKey(t, "schema")] = cache.Record{
			Payload: []byte{2, '"', 'x', '"'}, ExpiresAt: now.Add(time.Minute), StaleAt: now.Add(time.Minute),
		}
		store := newStringCache(t, backend, fixedClock{now: now}, cache.TTLPolicy{TTL: time.Minute})

		_, err := store.Get(context.Background(), "schema")
		if !errors.Is(err, cache.ErrSchemaMismatch) || errors.Is(err, cache.ErrDecode) {
			t.Fatalf("schema mismatch returned %v", err)
		}
	})

	t.Run("sliding refresh failure", func(t *testing.T) {
		backend := newRecordingBackend()
		key := backendKey(t, "failure")
		backend.records[key] = cache.Record{
			Payload: validPayload, ExpiresAt: now.Add(time.Minute), StaleAt: now.Add(time.Minute),
		}
		cause := errors.New("write unavailable")
		backend.setErrors = map[string]error{key: cause}
		store := newStringCache(t, backend, fixedClock{now: now}, cache.TTLPolicy{TTL: time.Minute, Sliding: true})

		_, err := store.Get(context.Background(), "failure")
		if !errors.Is(err, cache.ErrBackend) || !errors.Is(err, cause) {
			t.Fatalf("sliding refresh returned %v", err)
		}
	})

	t.Run("sliding refresh loses concurrent delete", func(t *testing.T) {
		backend := newRecordingBackend()
		backend.records[backendKey(t, "deleted")] = cache.Record{
			Payload: validPayload, ExpiresAt: now.Add(time.Minute), StaleAt: now.Add(time.Minute),
		}
		backend.removeAfterGet = true
		store := newStringCache(t, backend, fixedClock{now: now}, cache.TTLPolicy{TTL: time.Minute, Sliding: true})

		result, err := store.Get(context.Background(), "deleted")
		if err != nil || result.State != cache.Miss {
			t.Fatalf("concurrent delete returned result=%#v err=%v", result, err)
		}
	})
}

func TestSetDeleteAndSlidingExpiration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	clock := &mutableClock{now: now}
	backend := newRecordingBackend()
	store := newStringCache(t, backend, clock, cache.TTLPolicy{
		TTL:      time.Minute,
		StaleFor: 30 * time.Second,
		Sliding:  true,
	})

	if err := store.Set(context.Background(), "invoice", "paid"); err != nil {
		t.Fatal(err)
	}
	record := backend.records[backendKey(t, "invoice")]
	if !record.ExpiresAt.Equal(now.Add(time.Minute)) || !record.StaleAt.Equal(now.Add(90*time.Second)) {
		t.Fatalf("unexpected expiration: %#v", record)
	}

	clock.now = now.Add(30 * time.Second)
	result, err := store.Get(context.Background(), "invoice")
	if err != nil || result.State != cache.Hit || result.Value != "paid" {
		t.Fatalf("unexpected sliding hit: result=%#v err=%v", result, err)
	}
	record = backend.records[backendKey(t, "invoice")]
	if !record.ExpiresAt.Equal(clock.now.Add(time.Minute)) || !record.StaleAt.Equal(clock.now.Add(90*time.Second)) {
		t.Fatalf("sliding TTL was not extended: %#v", record)
	}

	if err := store.Delete(context.Background(), "invoice"); err != nil {
		t.Fatal(err)
	}
	result, err = store.Get(context.Background(), "invoice")
	if err != nil || result.State != cache.Miss {
		t.Fatalf("deleted value must miss: result=%#v err=%v", result, err)
	}
}

func TestAddAndReplaceExposeAtomicConditionalOutcome(t *testing.T) {
	t.Parallel()

	backend := newRecordingBackend()
	store := newStringCache(t, backend, fixedClock{now: time.Now()}, cache.TTLPolicy{TTL: time.Minute})

	added, err := store.Add(context.Background(), "key", "first")
	if err != nil || !added {
		t.Fatalf("first add must succeed: added=%t err=%v", added, err)
	}
	added, err = store.Add(context.Background(), "key", "second")
	if err != nil || added {
		t.Fatalf("second add must report conflict: added=%t err=%v", added, err)
	}
	replaced, err := store.Replace(context.Background(), "missing", "value")
	if err != nil || replaced {
		t.Fatalf("replace of missing key must report conflict: replaced=%t err=%v", replaced, err)
	}
	replaced, err = store.Replace(context.Background(), "key", "second")
	if err != nil || !replaced {
		t.Fatalf("replace of existing key must succeed: replaced=%t err=%v", replaced, err)
	}

	result, err := store.Get(context.Background(), "key")
	if err != nil || result.Value != "second" {
		t.Fatalf("replace value not visible: result=%#v err=%v", result, err)
	}
}

func TestOperationsHonorCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	backend := newRecordingBackend()
	store := newStringCache(t, backend, fixedClock{now: time.Now()}, cache.TTLPolicy{TTL: time.Minute})

	if _, err := store.Get(ctx, "key"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Get returned %v, want context cancellation", err)
	}
	if err := store.Set(ctx, "key", "value"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Set returned %v, want context cancellation", err)
	}
	if err := store.Delete(ctx, "key"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete returned %v, want context cancellation", err)
	}
}

func TestOperationsClassifyKeyCodecLimitAndBackendContextFailures(t *testing.T) {
	t.Parallel()

	now := time.Now()
	space, err := cache.NewKeySpace("test", "failures", 1, failingKeyEncoder{err: errors.New("bad key")}, 128)
	if err != nil {
		t.Fatal(err)
	}
	invalidKeys, err := cache.New(cache.Config[string, string]{
		Backend: newRecordingBackend(), Keys: space, Codec: cache.JSONCodec[string]{Version: 1},
		TTL: cache.TTLPolicy{TTL: time.Minute}, Clock: fixedClock{now: now}, MaxValue: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invalidKeys.Get(context.Background(), "key"); !errors.Is(err, cache.ErrInvalidKey) {
		t.Fatalf("Get invalid key returned %v", err)
	}
	if err := invalidKeys.Set(context.Background(), "key", "value"); !errors.Is(err, cache.ErrInvalidKey) {
		t.Fatalf("Set invalid key returned %v", err)
	}
	if err := invalidKeys.Delete(context.Background(), "key"); !errors.Is(err, cache.ErrInvalidKey) {
		t.Fatalf("Delete invalid key returned %v", err)
	}
	loaderCalled := false
	if _, err := invalidKeys.GetOrLoad(context.Background(), "key", func(context.Context, string) (cache.LoadResult[string], error) {
		loaderCalled = true
		return cache.LoadResult[string]{}, nil
	}); !errors.Is(err, cache.ErrInvalidKey) {
		t.Fatalf("GetOrLoad invalid key returned %v", err)
	}
	if loaderCalled {
		t.Fatal("invalid key reached loader")
	}

	functionSpace := mustStringKeySpace(t)
	unsupported, err := cache.New(cache.Config[string, func()]{
		Backend: newRecordingBackend(), Keys: functionSpace, Codec: cache.JSONCodec[func()]{Version: 1},
		TTL: cache.TTLPolicy{TTL: time.Minute}, Clock: fixedClock{now: now}, MaxValue: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := unsupported.Set(context.Background(), "key", func() {}); !errors.Is(err, cache.ErrDecode) {
		t.Fatalf("unsupported codec value returned %v", err)
	}

	limited, err := cache.New(cache.Config[string, string]{
		Backend: newRecordingBackend(), Keys: functionSpace, Codec: cache.JSONCodec[string]{Version: 1},
		TTL: cache.TTLPolicy{TTL: time.Minute}, Clock: fixedClock{now: now}, MaxValue: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := limited.Set(context.Background(), "key", "value"); !errors.Is(err, cache.ErrValueTooLarge) {
		t.Fatalf("oversized Set returned %v", err)
	}

	backend := newRecordingBackend()
	backend.getErr = context.DeadlineExceeded
	store := newStringCache(t, backend, fixedClock{now: now}, cache.TTLPolicy{TTL: time.Minute})
	if _, err := store.Get(context.Background(), "key"); !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, cache.ErrBackend) {
		t.Fatalf("backend deadline returned %v", err)
	}
	key := backendKey(t, "key")
	backend.getErr = nil
	backend.setErrors = map[string]error{key: context.DeadlineExceeded}
	if err := store.Set(context.Background(), "key", "value"); !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, cache.ErrBackend) {
		t.Fatalf("Set backend deadline returned %v", err)
	}
	backend.deleteErrors = map[string]error{key: context.DeadlineExceeded}
	if err := store.Delete(context.Background(), "key"); !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, cache.ErrBackend) {
		t.Fatalf("Delete backend deadline returned %v", err)
	}
}

func newStringCache(t testing.TB, backend cache.Backend, clock cache.Clock, policy cache.TTLPolicy) *cache.Cache[string, string] {
	t.Helper()
	space := mustStringKeySpace(t)
	store, err := cache.New(cache.Config[string, string]{
		Backend:  backend,
		Keys:     space,
		Codec:    cache.JSONCodec[string]{Version: 1},
		TTL:      policy,
		Clock:    clock,
		MaxValue: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func mustStringKeySpace(t testing.TB) cache.KeySpace[string] {
	t.Helper()
	space, err := cache.NewKeySpace("test", "strings", 1, cache.StringKeyEncoder{}, 128)
	if err != nil {
		t.Fatal(err)
	}
	return space
}

func backendKey(t testing.TB, logical string) string {
	t.Helper()
	space, err := cache.NewKeySpace("test", "strings", 1, cache.StringKeyEncoder{}, 128)
	if err != nil {
		t.Fatal(err)
	}
	key, err := space.Key(logical)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type mutableClock struct{ now time.Time }

func (c *mutableClock) Now() time.Time { return c.now }

type recordingBackend struct {
	mu             sync.Mutex
	records        map[string]cache.Record
	getErr         error
	getErrors      map[int]error
	recordOnGet    map[int]cache.Record
	getCount       int
	wantGets       int
	getsReached    chan struct{}
	setCount       int
	wantSets       int
	setsReached    chan struct{}
	setErrors      map[string]error
	deleteErrors   map[string]error
	removeAfterGet bool
}

func newRecordingBackend() *recordingBackend {
	return &recordingBackend{records: make(map[string]cache.Record)}
}

func (b *recordingBackend) Get(ctx context.Context, key string) (cache.Record, bool, error) {
	if err := ctx.Err(); err != nil {
		return cache.Record{}, false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.getCount++
	if b.wantGets > 0 && b.getCount == b.wantGets {
		close(b.getsReached)
	}
	if b.getErr != nil {
		return cache.Record{}, false, b.getErr
	}
	if err := b.getErrors[b.getCount]; err != nil {
		return cache.Record{}, false, err
	}
	if record, ok := b.recordOnGet[b.getCount]; ok {
		return record.Clone(), true, nil
	}
	record, found := b.records[key]
	if b.removeAfterGet {
		delete(b.records, key)
	}
	return record.Clone(), found, nil
}

func (b *recordingBackend) notifyAfterGets(count int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.wantGets = count
	b.getsReached = make(chan struct{})
}

func (b *recordingBackend) notifyAfterSets(count int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.wantSets = count
	b.setsReached = make(chan struct{})
}

func (b *recordingBackend) Set(ctx context.Context, key string, record cache.Record, condition cache.Condition) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.setErrors[key]; err != nil {
		return false, err
	}
	_, found := b.records[key]
	if condition == cache.IfAbsent && found || condition == cache.IfPresent && !found {
		return false, nil
	}
	b.records[key] = record.Clone()
	b.setCount++
	if b.wantSets > 0 && b.setCount == b.wantSets {
		close(b.setsReached)
	}
	return true, nil
}

func (b *recordingBackend) Delete(ctx context.Context, key string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.deleteErrors[key]; err != nil {
		return false, err
	}
	_, found := b.records[key]
	delete(b.records, key)
	return found, nil
}
