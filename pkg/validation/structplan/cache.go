package structplan

import (
	"fmt"
	"reflect"
	"sync"

	validation "github.com/faustbrian/golib/pkg/validation"
)

// Cache is an instance-owned bounded cache of immutable compiled tag plans.
type Cache struct {
	mu     sync.RWMutex
	limits validation.Limits
	plans  map[reflect.Type]any
}

// NewCache creates an empty bounded plan cache.
func NewCache(limits validation.Limits) *Cache {
	return &Cache{limits: limits, plans: make(map[reflect.Type]any)}
}

// CompileCached compiles or reuses the cached plan for T.
func CompileCached[T any](cache *Cache) (*TagPlan[T], error) {
	if cache == nil {
		return nil, ErrInvalidPlan
	}
	typeOf := reflect.TypeFor[T]()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if existing, ok := cache.plans[typeOf]; ok {
		return existing.(*TagPlan[T]), nil
	}
	if len(cache.plans) >= cache.limits.MaxCompiledPlans {
		return nil, fmt.Errorf("%w: compiled plans", validation.ErrLimitExceeded)
	}
	plan, err := CompileTags[T](cache.limits)
	if err != nil {
		return nil, err
	}
	cache.plans[typeOf] = plan
	return plan, nil
}

// Len returns the current number of cached plans.
func (cache *Cache) Len() int {
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	return len(cache.plans)
}

// Clear removes all cached immutable plans.
func (cache *Cache) Clear() {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.plans = make(map[reflect.Type]any)
}
