package cache_test

import (
	"errors"
	"math"
	"testing"
	"time"

	cache "github.com/faustbrian/golib/pkg/cache"
)

func TestRecordValidationRejectsAmbiguousDeadlinesAndNegativePayloads(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tests := map[string]cache.Record{
		"zero expiration": {StaleAt: now},
		"zero stale":      {ExpiresAt: now},
		"inverted window": {ExpiresAt: now.Add(time.Minute), StaleAt: now},
		"outside wire range": {
			ExpiresAt: time.Unix(0, math.MaxInt64).Add(time.Nanosecond),
			StaleAt:   time.Unix(0, math.MaxInt64).Add(time.Nanosecond),
		},
		"negative with payload": {
			ExpiresAt: now.Add(time.Minute),
			StaleAt:   now.Add(time.Minute),
			Negative:  true,
			Payload:   []byte("poison"),
		},
	}
	for name, record := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := record.Validate(); !errors.Is(err, cache.ErrInvalidRecord) {
				t.Fatalf("expected invalid record error, got %v", err)
			}
		})
	}
}

func TestSetRejectsDeadlineOutsidePortableWireRange(t *testing.T) {
	t.Parallel()

	now := time.Unix(0, math.MaxInt64)
	backend := newRecordingBackend()
	store := newStringCache(t, backend, fixedClock{now: now}, cache.TTLPolicy{TTL: time.Nanosecond})
	if err := store.Set(t.Context(), "key", "value"); !errors.Is(err, cache.ErrInvalidRecord) {
		t.Fatalf("expected portable deadline error, got %v", err)
	}
	if len(backend.records) != 0 {
		t.Fatal("invalid deadline reached backend")
	}
}

func TestSetCreatesPortableWallClockDeadlines(t *testing.T) {
	t.Parallel()

	now := time.Now()
	backend := newRecordingBackend()
	store := newStringCache(t, backend, fixedClock{now: now}, cache.TTLPolicy{
		TTL: time.Minute, StaleFor: time.Minute, Sliding: true,
	})
	if err := store.Set(t.Context(), "key", "value"); err != nil {
		t.Fatal(err)
	}
	record := backend.records[backendKey(t, "key")]
	if record.ExpiresAt != record.ExpiresAt.Round(0) || record.StaleAt != record.StaleAt.Round(0) {
		t.Fatalf("Set retained monotonic deadline data: %#v", record)
	}
}
