package schemaregistry_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

type resolverFunc func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error)

func (fn resolverFunc) Resolve(
	ctx context.Context,
	lookup schemaregistry.Lookup,
) (schemaregistry.ResolveResult, error) {
	return fn(ctx, lookup)
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

type panickingObserver struct{}

func (panickingObserver) ObserveResolveCache(context.Context, schemaregistry.ResolveCacheEvent) {
	panic("metrics failure")
}

func (clock *testClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *testClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(duration)
}

func TestResolveCacheExposesFreshStaleAndNegativeStates(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: time.Unix(100, 0)}
	lookup := schemaregistry.ByProviderID(schemaregistry.ProviderID{Provider: "test", Scope: "local", Value: "1"})
	want := schemaregistry.ResolveResult{
		Schema:    compileAvroString(t),
		ID:        schemaregistry.ProviderID{Provider: "test", Scope: "local", Value: "1"},
		Lifecycle: schemaregistry.LifecycleAvailable,
	}
	var calls atomic.Int32
	upstreamError := atomic.Bool{}
	cache, err := schemaregistry.NewResolveCache(
		resolverFunc(func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
			calls.Add(1)
			if upstreamError.Load() {
				return schemaregistry.ResolveResult{}, schemaregistry.ErrUnavailable
			}
			return want, nil
		}),
		schemaregistry.ResolveCacheConfig{
			MaxEntries:    8,
			MaxConcurrent: 2,
			FreshFor:      time.Minute,
			StaleFor:      time.Minute,
			NegativeFor:   10 * time.Second,
			Clock:         clock,
		},
	)
	if err != nil {
		t.Fatalf("NewResolveCache() error = %v", err)
	}

	loaded, err := cache.Resolve(context.Background(), lookup, schemaregistry.FailClosed)
	if err != nil || loaded.State != schemaregistry.CacheLoaded || loaded.Result.Lifecycle != want.Lifecycle {
		t.Fatalf("Resolve(load) = (%+v, %v)", loaded, err)
	}
	fresh, err := cache.Resolve(context.Background(), lookup, schemaregistry.FailClosed)
	if err != nil || fresh.State != schemaregistry.CacheFresh || calls.Load() != 1 {
		t.Fatalf("Resolve(fresh) = (%+v, %v), calls=%d", fresh, err, calls.Load())
	}

	clock.Advance(90 * time.Second)
	upstreamError.Store(true)
	stale, err := cache.Resolve(context.Background(), lookup, schemaregistry.AllowStale)
	if err != nil || stale.State != schemaregistry.CacheStale || !errors.Is(stale.StaleCause, schemaregistry.ErrUnavailable) {
		t.Fatalf("Resolve(stale) = (%+v, %v)", stale, err)
	}
	_, err = cache.Resolve(context.Background(), lookup, schemaregistry.FailClosed)
	if !errors.Is(err, schemaregistry.ErrUnavailable) {
		t.Fatalf("Resolve(fail closed) error = %v, want ErrUnavailable", err)
	}

	missing := schemaregistry.ByProviderID(schemaregistry.ProviderID{Provider: "test", Scope: "local", Value: "missing"})
	negativeCalls := atomic.Int32{}
	negativeCache, err := schemaregistry.NewResolveCache(
		resolverFunc(func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
			negativeCalls.Add(1)
			return schemaregistry.ResolveResult{}, schemaregistry.ErrNotFound
		}),
		schemaregistry.ResolveCacheConfig{
			MaxEntries:    8,
			MaxConcurrent: 2,
			FreshFor:      time.Minute,
			StaleFor:      time.Minute,
			NegativeFor:   10 * time.Second,
			Clock:         clock,
		},
	)
	if err != nil {
		t.Fatalf("NewResolveCache(negative) error = %v", err)
	}
	for range 2 {
		resolution, resolveErr := negativeCache.Resolve(context.Background(), missing, schemaregistry.FailClosed)
		if !errors.Is(resolveErr, schemaregistry.ErrNotFound) || resolution.State != schemaregistry.CacheNegative {
			t.Fatalf("Resolve(negative) = (%+v, %v)", resolution, resolveErr)
		}
	}
	if negativeCalls.Load() != 1 {
		t.Fatalf("negative upstream calls = %d, want 1", negativeCalls.Load())
	}
}

func TestResolveCacheDoesNotServeStaleForDefinitiveErrors(t *testing.T) {
	t.Parallel()

	for _, upstreamError := range []error{
		schemaregistry.ErrNotFound,
		schemaregistry.ErrUnauthorized,
		schemaregistry.ErrResolutionMismatch,
		context.Canceled,
	} {
		upstreamError := upstreamError
		t.Run(upstreamError.Error(), func(t *testing.T) {
			t.Parallel()
			clock := &testClock{now: time.Unix(100, 0)}
			lookup := schemaregistry.ByProviderID(schemaregistry.ProviderID{Provider: "test", Value: "1"})
			schema := compileAvroString(t)
			calls := 0
			cache, err := schemaregistry.NewResolveCache(
				resolverFunc(func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
					calls++
					if calls == 1 {
						return schemaregistry.ResolveResult{Schema: schema, ID: lookup.ProviderID(), Lifecycle: schemaregistry.LifecycleAvailable}, nil
					}
					return schemaregistry.ResolveResult{}, upstreamError
				}),
				schemaregistry.ResolveCacheConfig{
					MaxEntries: 1, MaxConcurrent: 1, FreshFor: time.Second,
					StaleFor: time.Minute, NegativeFor: time.Second, Clock: clock,
				},
			)
			if err != nil {
				t.Fatalf("NewResolveCache() error = %v", err)
			}
			if _, err := cache.Resolve(context.Background(), lookup, schemaregistry.FailClosed); err != nil {
				t.Fatalf("Resolve(seed) error = %v", err)
			}
			clock.Advance(2 * time.Second)
			resolution, err := cache.Resolve(context.Background(), lookup, schemaregistry.AllowStale)
			if !errors.Is(err, upstreamError) || resolution.State == schemaregistry.CacheStale {
				t.Fatalf("Resolve(allow stale) = (%+v, %v), want definitive error %v", resolution, err, upstreamError)
			}
		})
	}
}

func TestResolveCacheRejectsProviderIDWithoutProviderWhenPriming(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: time.Unix(1, 0)}
	cache, err := schemaregistry.NewResolveCache(
		resolverFunc(func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
			t.Fatal("resolver called while priming")
			return schemaregistry.ResolveResult{}, nil
		}),
		schemaregistry.ResolveCacheConfig{MaxEntries: 1, MaxConcurrent: 1, FreshFor: time.Minute, StaleFor: time.Minute, NegativeFor: time.Minute, Clock: clock},
	)
	if err != nil {
		t.Fatal(err)
	}
	lookup := schemaregistry.ByProviderID(schemaregistry.ProviderID{Value: "1"})
	if err := cache.Prime(lookup, schemaregistry.ResolveResult{ID: lookup.ProviderID()}); !errors.Is(err, schemaregistry.ErrInvalidRequest) {
		t.Fatalf("Prime() error = %v", err)
	}
}

func TestResolveCacheCoalescesLoadsAndCancelsWaiters(t *testing.T) {
	t.Parallel()

	schema := compileAvroString(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	var startedOnce sync.Once
	cache, err := schemaregistry.NewResolveCache(
		resolverFunc(func(ctx context.Context, _ schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
			calls.Add(1)
			startedOnce.Do(func() { close(started) })
			select {
			case <-release:
				return schemaregistry.ResolveResult{
					Schema:    schema,
					ID:        schemaregistry.ProviderID{Provider: "test", Scope: "local", Value: "1"},
					Lifecycle: schemaregistry.LifecycleAvailable,
				}, nil
			case <-ctx.Done():
				return schemaregistry.ResolveResult{}, ctx.Err()
			}
		}),
		schemaregistry.ResolveCacheConfig{
			MaxEntries:    8,
			MaxConcurrent: 1,
			FreshFor:      time.Minute,
			StaleFor:      time.Minute,
			NegativeFor:   time.Second,
			Clock:         &testClock{now: time.Unix(100, 0)},
		},
	)
	if err != nil {
		t.Fatalf("NewResolveCache() error = %v", err)
	}
	lookup := schemaregistry.ByProviderID(schemaregistry.ProviderID{Provider: "test", Scope: "local", Value: "1"})
	leaderDone := make(chan error, 1)
	go func() {
		_, resolveErr := cache.Resolve(context.Background(), lookup, schemaregistry.FailClosed)
		leaderDone <- resolveErr
	}()
	<-started

	waiterCtx, cancelWaiter := context.WithCancel(context.Background())
	waiterDone := make(chan error, 1)
	go func() {
		_, resolveErr := cache.Resolve(waiterCtx, lookup, schemaregistry.FailClosed)
		waiterDone <- resolveErr
	}()
	cancelWaiter()
	if err := <-waiterDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter error = %v, want context.Canceled", err)
	}
	close(release)
	if err := <-leaderDone; err != nil {
		t.Fatalf("leader error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream calls = %d, want 1", calls.Load())
	}
}

func TestResolveCacheAppliesEachWaitersStalePolicy(t *testing.T) {
	t.Parallel()

	schema := compileAvroString(t)
	clock := &testClock{now: time.Unix(100, 0)}
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	var startedOnce sync.Once
	cache, err := schemaregistry.NewResolveCache(
		resolverFunc(func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
			if calls.Add(1) == 1 {
				return schemaregistry.ResolveResult{
					Schema:    schema,
					ID:        schemaregistry.ProviderID{Provider: "test", Value: "1"},
					Lifecycle: schemaregistry.LifecycleAvailable,
				}, nil
			}
			startedOnce.Do(func() { close(started) })
			<-release
			return schemaregistry.ResolveResult{}, schemaregistry.ErrUnavailable
		}),
		schemaregistry.ResolveCacheConfig{
			MaxEntries: 1, MaxConcurrent: 1, FreshFor: time.Second,
			StaleFor: time.Minute, NegativeFor: time.Second, Clock: clock,
		},
	)
	if err != nil {
		t.Fatalf("NewResolveCache() error = %v", err)
	}
	lookup := schemaregistry.ByProviderID(schemaregistry.ProviderID{Provider: "test", Value: "1"})
	if _, err := cache.Resolve(context.Background(), lookup, schemaregistry.FailClosed); err != nil {
		t.Fatalf("Resolve(seed) error = %v", err)
	}
	clock.Advance(2 * time.Second)
	leader := make(chan error, 1)
	go func() {
		_, resolveErr := cache.Resolve(context.Background(), lookup, schemaregistry.FailClosed)
		leader <- resolveErr
	}()
	<-started
	waiter := make(chan struct {
		resolution schemaregistry.CacheResolution
		err        error
	}, 1)
	go func() {
		resolution, resolveErr := cache.Resolve(context.Background(), lookup, schemaregistry.AllowStale)
		waiter <- struct {
			resolution schemaregistry.CacheResolution
			err        error
		}{resolution: resolution, err: resolveErr}
	}()
	time.Sleep(10 * time.Millisecond)
	close(release)
	if err := <-leader; !errors.Is(err, schemaregistry.ErrUnavailable) {
		t.Fatalf("leader error = %v, want ErrUnavailable", err)
	}
	got := <-waiter
	if got.err != nil || got.resolution.State != schemaregistry.CacheStale ||
		!errors.Is(got.resolution.StaleCause, schemaregistry.ErrUnavailable) {
		t.Fatalf("waiter result = (%+v, %v), want stale", got.resolution, got.err)
	}
}

func TestResolveCacheOfflinePoliciesAndInvalidationAvoidHiddenIO(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	cache, err := schemaregistry.NewResolveCache(
		resolverFunc(func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
			calls.Add(1)
			return schemaregistry.ResolveResult{}, nil
		}),
		schemaregistry.ResolveCacheConfig{
			MaxEntries:    1,
			MaxConcurrent: 1,
			FreshFor:      time.Minute,
			StaleFor:      time.Minute,
			NegativeFor:   time.Second,
			Clock:         &testClock{now: time.Unix(100, 0)},
		},
	)
	if err != nil {
		t.Fatalf("NewResolveCache() error = %v", err)
	}
	lookup := schemaregistry.ByProviderID(schemaregistry.ProviderID{Provider: "test", Value: "1"})

	_, err = cache.Resolve(context.Background(), lookup, schemaregistry.CacheOnly)
	if !errors.Is(err, schemaregistry.ErrOfflineMiss) {
		t.Fatalf("Resolve(cache only) error = %v, want ErrOfflineMiss", err)
	}
	_, err = cache.Resolve(context.Background(), lookup, schemaregistry.ReturnUnavailable)
	if !errors.Is(err, schemaregistry.ErrUnavailable) {
		t.Fatalf("Resolve(unavailable) error = %v, want ErrUnavailable", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("offline policy upstream calls = %d, want 0", calls.Load())
	}

	if err := cache.Invalidate(lookup); err != nil {
		t.Fatalf("Invalidate() error = %v", err)
	}
}

func TestResolveCacheRejectsPoisonedIdentityAndSupportsExplicitPreload(t *testing.T) {
	t.Parallel()

	schema := compileAvroString(t)
	clock := &testClock{now: time.Unix(100, 0)}
	lookup := schemaregistry.ByProviderID(schemaregistry.ProviderID{Provider: "test", Value: "wanted"})
	var calls atomic.Int32
	cache, err := schemaregistry.NewResolveCache(
		resolverFunc(func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
			calls.Add(1)
			return schemaregistry.ResolveResult{Schema: schema, ID: schemaregistry.ProviderID{Provider: "test", Value: "other"}, Lifecycle: schemaregistry.LifecycleAvailable}, nil
		}),
		schemaregistry.ResolveCacheConfig{
			MaxEntries: 1, MaxConcurrent: 1, FreshFor: time.Minute,
			StaleFor: time.Minute, NegativeFor: time.Second, Clock: clock,
		},
	)
	if err != nil {
		t.Fatalf("NewResolveCache() error = %v", err)
	}
	for range 2 {
		_, err := cache.Resolve(context.Background(), lookup, schemaregistry.FailClosed)
		if !errors.Is(err, schemaregistry.ErrResolutionMismatch) {
			t.Fatalf("Resolve(poisoned) error = %v, want ErrResolutionMismatch", err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("poisoned upstream calls = %d, want 2", calls.Load())
	}

	preloaded := schemaregistry.ResolveResult{Schema: schema, ID: lookup.ProviderID(), Lifecycle: schemaregistry.LifecycleAvailable}
	if err := cache.Prime(lookup, preloaded); err != nil {
		t.Fatalf("Prime() error = %v", err)
	}
	resolution, err := cache.Resolve(context.Background(), lookup, schemaregistry.CacheOnly)
	if err != nil || resolution.State != schemaregistry.CacheFresh || resolution.Result.ID != preloaded.ID || calls.Load() != 2 {
		t.Fatalf("Resolve(preloaded) = (%+v, %v), calls=%d", resolution, err, calls.Load())
	}
}

func TestResolveCacheObserverCannotBreakResolution(t *testing.T) {
	t.Parallel()

	lookup := schemaregistry.ByProviderID(schemaregistry.ProviderID{Provider: "test", Value: "1"})
	schema := compileAvroString(t)
	cache, err := schemaregistry.NewResolveCache(
		resolverFunc(func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
			return schemaregistry.ResolveResult{Schema: schema, ID: lookup.ProviderID(), Lifecycle: schemaregistry.LifecycleAvailable}, nil
		}),
		schemaregistry.ResolveCacheConfig{
			MaxEntries: 1, MaxConcurrent: 1, FreshFor: time.Minute,
			StaleFor: time.Minute, NegativeFor: time.Second,
			Clock: &testClock{now: time.Unix(100, 0)}, Observer: panickingObserver{},
		},
	)
	if err != nil {
		t.Fatalf("NewResolveCache() error = %v", err)
	}
	if _, err := cache.Resolve(context.Background(), lookup, schemaregistry.FailClosed); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
}
