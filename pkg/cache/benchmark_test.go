package cache_test

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	cache "github.com/faustbrian/golib/pkg/cache"
)

func BenchmarkGetHit(b *testing.B) {
	store := benchmarkCache(b)
	if err := store.Set(context.Background(), "key", "value"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := store.Get(context.Background(), "key"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetMiss(b *testing.B) {
	store := benchmarkCache(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := store.Get(context.Background(), "missing"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetStale(b *testing.B) {
	backend := newRecordingBackend()
	now := time.Now()
	store := newStringCache(b, backend, fixedClock{now: now}, cache.TTLPolicy{TTL: time.Minute, StaleFor: time.Minute})
	backend.records[backendKey(b, "key")] = cache.Record{
		Payload:   []byte{1, '"', 'v', 'a', 'l', 'u', 'e', '"'},
		ExpiresAt: now.Add(-time.Second),
		StaleAt:   now.Add(time.Minute),
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := store.Get(context.Background(), "key"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetOrLoadContended(b *testing.B) {
	store := benchmarkCache(b)
	loader := func(context.Context, string) (cache.LoadResult[string], error) {
		return cache.LoadResult[string]{Value: "value", Found: true}, nil
	}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := range b.N {
		key := strconv.Itoa(iteration)
		var group sync.WaitGroup
		for range 32 {
			group.Add(1)
			go func() {
				defer group.Done()
				if _, err := store.GetOrLoad(context.Background(), key, loader); err != nil {
					b.Error(err)
				}
			}()
		}
		group.Wait()
	}
}

func benchmarkCache(tb testing.TB) *cache.Cache[string, string] {
	return newStringCache(tb, newRecordingBackend(), cache.SystemClock{}, cache.TTLPolicy{TTL: time.Minute})
}
