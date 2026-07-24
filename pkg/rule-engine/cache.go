package ruleengine

import (
	"container/list"
	"context"
	"sync"
)

// PlanCache stores compiled immutable plans by canonical definition hash.
type PlanCache interface {
	Get(context.Context, string) (Plan, bool, error)
	Put(context.Context, string, Plan) error
}

// CompileCached returns a matching cached plan or compiles and stores a new
// plan. Cache entries with a different embedded hash are ignored.
func (compiler Compiler) CompileCached(ctx context.Context, set RuleSet, cache PlanCache) (Plan, []Diagnostic, error) {
	if cache == nil {
		return Plan{}, nil, newError(CodeCache, "plan cache is nil")
	}
	hash, err := CanonicalHash(set)
	if err != nil {
		return Plan{}, nil, err
	}
	plan, found, err := cache.Get(ctx, hash)
	if err != nil {
		return Plan{}, nil, newError(CodeCache, "plan cache read failed")
	}
	if found && plan.hash == hash {
		return plan, nil, nil
	}
	plan, diagnostics, err := compiler.Compile(ctx, set)
	if err != nil {
		return Plan{}, diagnostics, err
	}
	plan.hash = hash
	if err := cache.Put(ctx, hash, plan); err != nil {
		return Plan{}, nil, newError(CodeCache, "plan cache write failed")
	}
	return plan, diagnostics, nil
}

type cacheEntry struct {
	key  string
	plan Plan
}

// MemoryPlanCache is a bounded concurrency-safe LRU plan cache.
type MemoryPlanCache struct {
	mu       sync.Mutex
	capacity int
	entries  map[string]*list.Element
	recent   *list.List
}

// NewMemoryPlanCache constructs a cache with a strict positive capacity.
func NewMemoryPlanCache(capacity int) (*MemoryPlanCache, error) {
	if capacity <= 0 {
		return nil, newError(CodeInvalidLimit, "cache capacity must be positive")
	}
	return &MemoryPlanCache{
		capacity: capacity,
		entries:  make(map[string]*list.Element, capacity),
		recent:   list.New(),
	}, nil
}

// Get returns and promotes a cached plan.
func (cache *MemoryPlanCache) Get(ctx context.Context, key string) (Plan, bool, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, false, err
	}
	if key == "" {
		return Plan{}, false, newError(CodeCache, "cache key is empty")
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	element, exists := cache.entries[key]
	if !exists {
		return Plan{}, false, nil
	}
	cache.recent.MoveToFront(element)
	return element.Value.(cacheEntry).plan, true, nil
}

// Put inserts or replaces a plan and evicts the least recently used entry.
func (cache *MemoryPlanCache) Put(ctx context.Context, key string, plan Plan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if key == "" {
		return newError(CodeCache, "cache key is empty")
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if element, exists := cache.entries[key]; exists {
		element.Value = cacheEntry{key: key, plan: plan}
		cache.recent.MoveToFront(element)
		return nil
	}
	element := cache.recent.PushFront(cacheEntry{key: key, plan: plan})
	cache.entries[key] = element
	if cache.recent.Len() <= cache.capacity {
		return nil
	}
	oldest := cache.recent.Back()
	entry := oldest.Value.(cacheEntry)
	delete(cache.entries, entry.key)
	cache.recent.Remove(oldest)
	return nil
}

// Len returns the current entry count.
func (cache *MemoryPlanCache) Len() int {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return len(cache.entries)
}
