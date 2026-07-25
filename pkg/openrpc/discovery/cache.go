package discovery

import (
	"context"
	"sync"
)

// Discoverer produces discovery snapshots.
type Discoverer interface {
	Discover(context.Context) (Snapshot, error)
}

// Cache is an explicitly owned, concurrency-safe discovery cache. Concurrent
// misses are deduplicated. The caller that starts a refresh owns its context;
// canceling a waiter does not cancel that shared refresh.
type Cache struct {
	discoverer Discoverer

	mu         sync.Mutex
	snapshot   Snapshot
	valid      bool
	loading    bool
	done       chan struct{}
	generation *byte
}

// NewCache wraps a discoverer without loading it or starting a goroutine.
func NewCache(discoverer Discoverer) (*Cache, error) {
	if discoverer == nil {
		return nil, ErrInvalidOptions
	}
	return &Cache{discoverer: discoverer}, nil
}

// Discover returns the cached snapshot or synchronously performs one
// deduplicated refresh.
func (cache *Cache) Discover(ctx context.Context) (Snapshot, error) {
	if cache == nil || cache.discoverer == nil || ctx == nil {
		return Snapshot{}, ErrInvalidOptions
	}
	for {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		cache.mu.Lock()
		if cache.valid {
			snapshot := cache.snapshot
			cache.mu.Unlock()
			return snapshot, nil
		}
		if cache.loading {
			done := cache.done
			cache.mu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return Snapshot{}, ctx.Err()
			}
		}

		cache.loading = true
		cache.done = make(chan struct{})
		done := cache.done
		generation := cache.generation
		cache.mu.Unlock()

		snapshot, err := cache.discoverer.Discover(ctx)
		cache.mu.Lock()
		if err == nil && cache.generation == generation {
			cache.snapshot = snapshot
			cache.valid = true
		}
		cache.loading = false
		close(done)
		cache.mu.Unlock()
		return snapshot, err
	}
}

// Invalidate makes the next discovery call refresh the snapshot. If a refresh
// is already running, its result is returned to its leader but is not cached.
func (cache *Cache) Invalidate() {
	if cache == nil {
		return
	}
	cache.mu.Lock()
	cache.valid = false
	cache.generation = new(byte)
	cache.mu.Unlock()
}
