package cache_test

import (
	"context"
	"errors"
	"testing"
	"time"

	cache "github.com/faustbrian/golib/pkg/cache"
)

func TestBulkOperationsReportPerKeyPartialFailuresInInputOrder(t *testing.T) {
	t.Parallel()

	backend := newRecordingBackend()
	store := newBulkStringCache(t, backend, 4)
	badKey := backendKey(t, "bad")
	backend.setErrors = map[string]error{badKey: errors.New("write rejected")}

	writes, err := store.SetMany(context.Background(), []cache.Entry[string, string]{
		{Key: "first", Value: "one"},
		{Key: "bad", Value: "two"},
		{Key: "third", Value: "three"},
	})
	if err != nil {
		t.Fatalf("batch-level error: %v", err)
	}
	if len(writes) != 3 || writes[0].Key != "first" || writes[1].Key != "bad" || writes[2].Key != "third" {
		t.Fatalf("write results lost input order: %#v", writes)
	}
	if writes[0].Err != nil || !errors.Is(writes[1].Err, cache.ErrBackend) || writes[2].Err != nil {
		t.Fatalf("partial write errors were flattened: %#v", writes)
	}

	reads, err := store.GetMany(context.Background(), []string{"third", "bad", "first", "missing"})
	if err != nil {
		t.Fatalf("batch-level error: %v", err)
	}
	if len(reads) != 4 || reads[0].Value != "three" || reads[0].State != cache.Hit ||
		reads[1].State != cache.Miss || reads[2].Value != "one" || reads[3].State != cache.Miss {
		t.Fatalf("unexpected ordered read results: %#v", reads)
	}

	backend.deleteErrors = map[string]error{backendKey(t, "third"): errors.New("delete rejected")}
	deletes, err := store.DeleteMany(context.Background(), []string{"first", "third", "missing"})
	if err != nil {
		t.Fatalf("batch-level error: %v", err)
	}
	if deletes[0].Err != nil || !errors.Is(deletes[1].Err, cache.ErrBackend) || deletes[2].Err != nil {
		t.Fatalf("partial delete errors were flattened: %#v", deletes)
	}
}

func TestBulkOperationsRejectOversizedBatchBeforeBackendAccess(t *testing.T) {
	t.Parallel()

	backend := newRecordingBackend()
	store := newBulkStringCache(t, backend, 2)
	_, err := store.GetMany(context.Background(), []string{"one", "two", "three"})
	if !errors.Is(err, cache.ErrBatchTooLarge) {
		t.Fatalf("expected batch limit error, got %v", err)
	}
	backend.mu.Lock()
	getCount := backend.getCount
	backend.mu.Unlock()
	if getCount != 0 {
		t.Fatalf("oversized batch accessed backend %d times", getCount)
	}
	if _, err := store.SetMany(context.Background(), []cache.Entry[string, string]{{}, {}, {}}); !errors.Is(err, cache.ErrBatchTooLarge) {
		t.Fatalf("oversized SetMany returned %v", err)
	}
	if _, err := store.DeleteMany(context.Background(), []string{"one", "two", "three"}); !errors.Is(err, cache.ErrBatchTooLarge) {
		t.Fatalf("oversized DeleteMany returned %v", err)
	}
}

func newBulkStringCache(t *testing.T, backend cache.Backend, maxBatch int) *cache.Cache[string, string] {
	t.Helper()
	space, err := cache.NewKeySpace("test", "strings", 1, cache.StringKeyEncoder{}, 128)
	if err != nil {
		t.Fatal(err)
	}
	store, err := cache.New(cache.Config[string, string]{
		Backend:  backend,
		Keys:     space,
		Codec:    cache.JSONCodec[string]{Version: 1},
		TTL:      cache.TTLPolicy{TTL: time.Minute},
		Clock:    fixedClock{now: time.Now()},
		MaxValue: 1024,
		MaxBatch: maxBatch,
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}
