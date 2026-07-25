package memory_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	cache "github.com/faustbrian/golib/pkg/cache"
	"github.com/faustbrian/golib/pkg/cache/backend/memory"
)

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	valid := memory.Config{MaxEntries: 1, MaxBytes: 1, Clock: cache.SystemClock{}}
	tests := map[string]func(*memory.Config){
		"entries": func(config *memory.Config) { config.MaxEntries = 0 },
		"bytes":   func(config *memory.Config) { config.MaxBytes = 0 },
		"clock":   func(config *memory.Config) { config.Clock = nil },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			config := valid
			mutate(&config)
			if _, err := memory.New(config); !errors.Is(err, cache.ErrInvalidConfig) {
				t.Fatalf("New returned %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestBackendIsBoundedAndEvictsLeastRecentlyUsed(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)}
	backend, err := memory.New(memory.Config{MaxEntries: 2, MaxBytes: 1024, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	set(ctx, t, backend, "first", "1", clock.now)
	set(ctx, t, backend, "second", "2", clock.now)
	if _, found, err := backend.Get(ctx, "first"); err != nil || !found {
		t.Fatalf("touch first: found=%t err=%v", found, err)
	}
	set(ctx, t, backend, "third", "3", clock.now)

	if _, found, err := backend.Get(ctx, "second"); err != nil || found {
		t.Fatalf("least recently used entry was not evicted: found=%t err=%v", found, err)
	}
	if stats := backend.Stats(); stats.Entries != 2 || stats.Evictions != 1 || stats.Bytes > 1024 {
		t.Fatalf("unexpected bounded stats: %#v", stats)
	}
}

func TestBackendCopiesPayloadsAndAppliesAtomicConditions(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Now()}
	backend, err := memory.New(memory.Config{MaxEntries: 2, MaxBytes: 1024, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	record := liveRecord("original", clock.now)
	written, err := backend.Set(ctx, "key", record, cache.IfAbsent)
	if err != nil || !written {
		t.Fatalf("initial add: written=%t err=%v", written, err)
	}
	record.Payload[0] = 'X'

	written, err = backend.Set(ctx, "key", liveRecord("ignored", clock.now), cache.IfAbsent)
	if err != nil || written {
		t.Fatalf("duplicate add: written=%t err=%v", written, err)
	}
	written, err = backend.Set(ctx, "missing", liveRecord("ignored", clock.now), cache.IfPresent)
	if err != nil || written {
		t.Fatalf("replace missing: written=%t err=%v", written, err)
	}

	got, found, err := backend.Get(ctx, "key")
	if err != nil || !found || string(got.Payload) != "original" {
		t.Fatalf("stored payload aliased caller memory: record=%#v found=%t err=%v", got, found, err)
	}
	got.Payload[0] = 'Y'
	again, _, _ := backend.Get(ctx, "key")
	if string(again.Payload) != "original" {
		t.Fatalf("returned payload aliased stored memory: %q", again.Payload)
	}
}

func TestBackendExpiresRecordsWithoutSleep(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Now()}
	backend, err := memory.New(memory.Config{MaxEntries: 2, MaxBytes: 1024, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	set(ctx, t, backend, "key", "value", clock.now)
	clock.now = clock.now.Add(3 * time.Minute)

	if _, found, err := backend.Get(ctx, "key"); err != nil || found {
		t.Fatalf("expired record must miss: found=%t err=%v", found, err)
	}
	if stats := backend.Stats(); stats.Entries != 0 || stats.Expirations != 1 {
		t.Fatalf("unexpected expiration stats: %#v", stats)
	}
}

func TestDeleteTreatsExpiredRecordAsAbsent(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Now()}
	backend, err := memory.New(memory.Config{MaxEntries: 2, MaxBytes: 1024, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	set(t.Context(), t, backend, "key", "value", clock.now)
	clock.now = clock.now.Add(3 * time.Minute)

	deleted, err := backend.Delete(t.Context(), "key")
	if err != nil || deleted {
		t.Fatalf("expired Delete: deleted=%t err=%v", deleted, err)
	}
	if stats := backend.Stats(); stats.Entries != 0 || stats.Expirations != 1 {
		t.Fatalf("unexpected expiration stats: %#v", stats)
	}
}

func TestBackendRejectsOversizedRecordsAndUseAfterClose(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Now()}
	backend, err := memory.New(memory.Config{MaxEntries: 2, MaxBytes: 8, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	_, err = backend.Set(context.Background(), "long-key", liveRecord("value", clock.now), cache.Unconditional)
	if !errors.Is(err, cache.ErrCapacity) {
		t.Fatalf("expected capacity error, got %v", err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := backend.Get(context.Background(), "key"); !errors.Is(err, cache.ErrClosed) {
		t.Fatalf("expected closed error, got %v", err)
	}
}

func TestBackendHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	backend, err := memory.New(memory.Config{MaxEntries: 2, MaxBytes: 1024, Clock: &fakeClock{now: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := backend.Get(ctx, "key"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestBackendRejectsInvalidRecordsConditionsAndClosedMutations(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Now()}
	backend, err := memory.New(memory.Config{MaxEntries: 2, MaxBytes: 1024, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := backend.Set(ctx, "key", cache.Record{}, cache.Unconditional); !errors.Is(err, cache.ErrInvalidRecord) {
		t.Fatalf("invalid record returned %v", err)
	}
	if _, err := backend.Set(ctx, "key", liveRecord("value", clock.now), cache.Condition(255)); !errors.Is(err, cache.ErrInvalidPolicy) {
		t.Fatalf("invalid condition returned %v", err)
	}
	if deleted, err := backend.Delete(ctx, "missing"); err != nil || deleted {
		t.Fatalf("missing delete returned deleted=%t err=%v", deleted, err)
	}
	set(ctx, t, backend, "key", "value", clock.now)
	if deleted, err := backend.Delete(ctx, "key"); err != nil || !deleted {
		t.Fatalf("present delete returned deleted=%t err=%v", deleted, err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	if err := backend.Close(); err != nil {
		t.Fatalf("second close returned %v", err)
	}
	if _, err := backend.Set(ctx, "key", liveRecord("value", clock.now), cache.Unconditional); !errors.Is(err, cache.ErrClosed) {
		t.Fatalf("closed Set returned %v", err)
	}
	if _, err := backend.Delete(ctx, "key"); !errors.Is(err, cache.ErrClosed) {
		t.Fatalf("closed Delete returned %v", err)
	}
}

func TestBackendExpiresRecordsDuringConditionalSet(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Now()}
	observer := &backendObserver{}
	backend, err := memory.New(memory.Config{MaxEntries: 2, MaxBytes: 1024, Clock: clock, Observer: observer})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	set(ctx, t, backend, "key", "old", clock.now)
	clock.now = clock.now.Add(3 * time.Minute)
	written, err := backend.Set(ctx, "key", liveRecord("new", clock.now), cache.IfPresent)
	if err != nil || written {
		t.Fatalf("replace of expired record returned written=%t err=%v", written, err)
	}
	events := observer.Events()
	if len(events) != 1 || events[0].Operation != cache.OperationExpire {
		t.Fatalf("expiration event missing: %#v", events)
	}
}

func TestBackendReplacesExpiredRecordWithConditionalAdd(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Now()}
	observer := &backendObserver{}
	backend, err := memory.New(memory.Config{MaxEntries: 1, MaxBytes: 1024, Clock: clock, Observer: observer})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	set(ctx, t, backend, "key", "old", clock.now)
	clock.now = clock.now.Add(3 * time.Minute)
	written, err := backend.Set(ctx, "key", liveRecord("new", clock.now), cache.IfAbsent)
	if err != nil || !written {
		t.Fatalf("add over expired record returned written=%t err=%v", written, err)
	}
	record, found, err := backend.Get(ctx, "key")
	if err != nil || !found || string(record.Payload) != "new" {
		t.Fatalf("replacement record=%#v found=%t err=%v", record, found, err)
	}
	events := observer.Events()
	if len(events) != 1 || events[0].Operation != cache.OperationExpire {
		t.Fatalf("expiration event missing: %#v", events)
	}
}

func TestBackendObservesEvictionExpirationAndRetainedSize(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Now()}
	observer := &backendObserver{}
	backend, err := memory.New(memory.Config{
		MaxEntries: 1,
		MaxBytes:   1024,
		Clock:      clock,
		Observer:   observer,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	set(ctx, t, backend, "first", "1", clock.now)
	set(ctx, t, backend, "second", "2", clock.now)
	clock.now = clock.now.Add(3 * time.Minute)
	if _, _, err := backend.Get(ctx, "second"); err != nil {
		t.Fatal(err)
	}

	events := observer.Events()
	if len(events) != 2 {
		t.Fatalf("got %d backend events, want 2: %#v", len(events), events)
	}
	if events[0].Operation != cache.OperationEvict || events[0].Outcome != cache.OutcomeEvicted || events[0].Size <= 0 {
		t.Fatalf("unexpected eviction event: %#v", events[0])
	}
	if events[1].Operation != cache.OperationExpire || events[1].Outcome != cache.OutcomeExpired || events[1].Size != 0 {
		t.Fatalf("unexpected expiration event: %#v", events[1])
	}
}

func set(ctx context.Context, t *testing.T, backend *memory.Backend, key string, value string, now time.Time) {
	t.Helper()
	written, err := backend.Set(ctx, key, liveRecord(value, now), cache.Unconditional)
	if err != nil || !written {
		t.Fatalf("set %q: written=%t err=%v", key, written, err)
	}
}

func liveRecord(value string, now time.Time) cache.Record {
	return cache.Record{
		Payload:   []byte(value),
		ExpiresAt: now.Add(time.Minute),
		StaleAt:   now.Add(2 * time.Minute),
	}
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func memoryBackendForConformance() (*memory.Backend, error) {
	return memory.New(memory.Config{
		MaxEntries: 32,
		MaxBytes:   1 << 20,
		Clock:      &fakeClock{now: time.Now()},
	})
}

type backendObserver struct {
	mu     sync.Mutex
	events []cache.Event
}

func (o *backendObserver) Observe(_ context.Context, event cache.Event) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events = append(o.events, event)
	return nil
}

func (o *backendObserver) Events() []cache.Event {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]cache.Event(nil), o.events...)
}
