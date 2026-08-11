package schemaregistry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type resolverFunction func(context.Context, Lookup) (ResolveResult, error)

func (function resolverFunction) Resolve(ctx context.Context, lookup Lookup) (ResolveResult, error) {
	return function(ctx, lookup)
}

type manualClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *manualClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *manualClock) advance(duration time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(duration)
	clock.mu.Unlock()
}

type eventObserver struct {
	mu     sync.Mutex
	events []ResolveCacheEvent
}

type delayedCanceledContext struct{ calls int }

func (*delayedCanceledContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*delayedCanceledContext) Done() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}
func (*delayedCanceledContext) Value(any) any { return nil }
func (ctx *delayedCanceledContext) Err() error {
	ctx.calls++
	if ctx.calls > 1 {
		return context.Canceled
	}
	return nil
}

func (observer *eventObserver) ObserveResolveCache(_ context.Context, event ResolveCacheEvent) {
	observer.mu.Lock()
	observer.events = append(observer.events, event)
	observer.mu.Unlock()
}

func validCacheConfig(clock Clock) ResolveCacheConfig {
	return ResolveCacheConfig{
		MaxEntries: 2, MaxConcurrent: 1, FreshFor: time.Second,
		StaleFor: time.Second, NegativeFor: time.Second, Clock: clock,
	}
}

func validCachedResult(schema Schema, id ProviderID) ResolveResult {
	return ResolveResult{Schema: schema, ID: id, Lifecycle: LifecycleAvailable}
}

func TestResolveCacheConfigurationAndPolicyBoundaries(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(100, 0)}
	schema := internalSchema(t, FormatAvro, `"string"`, nil)
	resolver := resolverFunction(func(_ context.Context, lookup Lookup) (ResolveResult, error) {
		return validCachedResult(schema, lookup.ProviderID()), nil
	})
	var nilResolver *resolverFunction
	var nilClock *manualClock
	for _, config := range []ResolveCacheConfig{
		{},
		validCacheConfig(nilClock),
		{MaxEntries: 0, MaxConcurrent: 1, FreshFor: time.Second, NegativeFor: time.Second, Clock: clock},
		{MaxEntries: 1, MaxConcurrent: 0, FreshFor: time.Second, NegativeFor: time.Second, Clock: clock},
		{MaxEntries: 1, MaxConcurrent: 1, FreshFor: 0, NegativeFor: time.Second, Clock: clock},
		{MaxEntries: 1, MaxConcurrent: 1, FreshFor: time.Second, StaleFor: -1, NegativeFor: time.Second, Clock: clock},
		{MaxEntries: 1, MaxConcurrent: 1, FreshFor: time.Second, NegativeFor: 0, Clock: clock},
	} {
		if _, err := NewResolveCache(resolver, config); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("NewResolveCache(%+v) error = %v", config, err)
		}
	}
	if _, err := NewResolveCache(nilResolver, validCacheConfig(clock)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("NewResolveCache(typed nil) error = %v", err)
	}
	observer := &eventObserver{}
	config := validCacheConfig(clock)
	config.Observer = observer
	cache, err := NewResolveCache(resolver, config)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cache.Resolve(canceled, ByProviderID(ProviderID{Provider: "test", Value: "1"}), FailClosed); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve(canceled) error = %v", err)
	}
	if _, err := cache.Resolve(context.Background(), ByProviderID(ProviderID{Provider: "test", Value: "1"}), "invalid"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Resolve(invalid policy) error = %v", err)
	}
	if _, err := cache.Resolve(context.Background(), Lookup{}, FailClosed); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Resolve(empty) error = %v", err)
	}
	if err := cache.Invalidate(Lookup{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Invalidate(empty) error = %v", err)
	}
	if len(observer.events) != 3 || observer.events[0].Outcome != "error" {
		t.Fatalf("observer events = %+v", observer.events)
	}
}

func TestResolveCacheExpiryEvictionAndLookupIdentity(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(100, 0)}
	schema := internalSchema(t, FormatAvro, `"string"`, nil)
	var calls int
	resolver := resolverFunction(func(_ context.Context, lookup Lookup) (ResolveResult, error) {
		calls++
		return validCachedResult(schema, lookup.ProviderID()), nil
	})
	cache, err := NewResolveCache(resolver, validCacheConfig(clock))
	if err != nil {
		t.Fatal(err)
	}
	first := ByProviderID(ProviderID{Provider: "test", Value: "1"})
	second := ByProviderID(ProviderID{Provider: "test", Value: "2"})
	third := ByProviderID(ProviderID{Provider: "test", Value: "3"})
	if err := cache.Prime(first, validCachedResult(schema, first.ProviderID())); err != nil {
		t.Fatal(err)
	}
	clock.advance(1500 * time.Millisecond)
	if resolution, err := cache.Resolve(context.Background(), first, CacheOnly); err != nil || resolution.State != CacheStale {
		t.Fatalf("Resolve(stale cache only) = (%+v, %v)", resolution, err)
	}
	clock.advance(time.Second)
	if _, err := cache.Resolve(context.Background(), first, CacheOnly); !errors.Is(err, ErrOfflineMiss) {
		t.Fatalf("Resolve(expired cache only) error = %v", err)
	}
	clock.now = time.Unix(200, 0)
	for _, item := range []struct {
		lookup Lookup
		result ResolveResult
	}{{first, validCachedResult(schema, first.ProviderID())}, {second, validCachedResult(schema, second.ProviderID())}, {third, validCachedResult(schema, third.ProviderID())}} {
		if err := cache.Prime(item.lookup, item.result); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := cache.Resolve(context.Background(), first, CacheOnly); !errors.Is(err, ErrOfflineMiss) {
		t.Fatalf("Resolve(evicted) error = %v", err)
	}
	if err := cache.Invalidate(second); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Resolve(context.Background(), second, CacheOnly); !errors.Is(err, ErrOfflineMiss) {
		t.Fatalf("Resolve(invalidated) error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("offline calls = %d", calls)
	}
}

func TestResolveCacheLoadCancellationAndAllSelectorValidation(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(100, 0)}
	schema := internalSchema(t, FormatAvro, `"string"`, nil)
	cache, err := NewResolveCache(resolverFunction(func(_ context.Context, lookup Lookup) (ResolveResult, error) {
		return validCachedResult(schema, lookup.ProviderID()), nil
	}), validCacheConfig(clock))
	if err != nil {
		t.Fatal(err)
	}
	cache.slots <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cache.load(ctx, ByProviderID(ProviderID{Provider: "test", Value: "1"}), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("load(canceled slot) error = %v", err)
	}
	<-cache.slots
	waitingLookup := ByProviderID(ProviderID{Provider: "test", Value: "waiting"})
	waitingFlight := &resolveFlight{done: make(chan struct{})}
	cache.flights[waitingLookup] = waitingFlight
	if _, err := cache.Resolve(&delayedCanceledContext{}, waitingLookup, FailClosed); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve(canceled waiter) error = %v", err)
	}
	delete(cache.flights, waitingLookup)
	subject := Subject{Registry: "registry", Name: "subject"}
	version := Version{Number: 2}
	valid := []struct {
		lookup Lookup
		result ResolveResult
	}{
		{ByProviderID(ProviderID{Provider: "test", Value: "1"}), validCachedResult(schema, ProviderID{Provider: "test", Value: "1"})},
		{ByFingerprint(schema.Fingerprint()), validCachedResult(schema, ProviderID{Provider: "test", Value: "1"})},
		{AtVersion(subject, version), func() ResolveResult {
			result := validCachedResult(schema, ProviderID{Provider: "test", Value: "1"})
			result.Subject, result.Version = subject, version
			return result
		}()},
		{Latest(subject), func() ResolveResult {
			result := validCachedResult(schema, ProviderID{Provider: "test", Value: "1"})
			result.Subject, result.Version = subject, version
			return result
		}()},
	}
	for _, item := range valid {
		if err := validateResolution(item.lookup, item.result); err != nil {
			t.Fatalf("validateResolution(%s) error = %v", item.lookup.Kind(), err)
		}
		if err := cache.Prime(item.lookup, item.result); err != nil {
			t.Fatalf("Prime(%s) error = %v", item.lookup.Kind(), err)
		}
	}
	for _, lookup := range []Lookup{
		ByProviderID(ProviderID{Provider: "test", Value: "1"}),
		ByFingerprint(schema.Fingerprint()), AtVersion(subject, version), Latest(subject), Lookup{kind: "invalid"},
	} {
		if err := validateResolution(lookup, ResolveResult{}); !errors.Is(err, ErrResolutionMismatch) {
			t.Fatalf("validateResolution(%s mismatch) error = %v", lookup.Kind(), err)
		}
	}
	if err := cache.Prime(Lookup{}, ResolveResult{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Prime(invalid lookup) error = %v", err)
	}
	if err := cache.Prime(ByProviderID(ProviderID{Provider: "test", Value: "wanted"}), ResolveResult{ID: ProviderID{Provider: "test", Value: "other"}}); !errors.Is(err, ErrResolutionMismatch) {
		t.Fatalf("Prime(mismatch) error = %v", err)
	}
	if nonNegativeAge(time.Unix(1, 0), time.Unix(2, 0)) != 0 {
		t.Fatal("nonNegativeAge returned negative duration")
	}
	for _, policy := range []AvailabilityPolicy{FailClosed, AllowStale, CacheOnly, ReturnUnavailable} {
		if !validAvailabilityPolicy(policy) {
			t.Fatalf("validAvailabilityPolicy(%s) = false", policy)
		}
	}
}

func TestResolveCacheDetachedFlightBookkeeping(t *testing.T) {
	t.Parallel()

	cache, err := NewResolveCache(
		resolverFunction(func(context.Context, Lookup) (ResolveResult, error) {
			t.Fatal("resolver called")
			return ResolveResult{}, nil
		}),
		validCacheConfig(&manualClock{now: time.Unix(100, 0)}),
	)
	if err != nil {
		t.Fatal(err)
	}
	lookup := ByProviderID(ProviderID{Provider: "test", Value: "1"})
	first, leader := cache.flight(lookup)
	if !leader {
		t.Fatal("first flight was not leader")
	}
	if err := cache.Invalidate(lookup); err != nil {
		t.Fatal(err)
	}
	second, leader := cache.flight(lookup)
	if !leader || second.generation == nil || second.generation == first.generation {
		t.Fatal("fenced flight did not receive a new generation")
	}

	cache.finishFlight(lookup, first, CacheResolution{}, nil)
	if cache.flights[lookup] != second || len(cache.activeFlights[lookup]) != 1 ||
		cache.generations[lookup] != second.generation {
		t.Fatal("detached flight completion removed current generation state")
	}
	cache.finishFlight(lookup, second, CacheResolution{}, nil)
	if _, found := cache.flights[lookup]; found || len(cache.activeFlights[lookup]) != 0 {
		t.Fatal("completed flight state was retained")
	}
	if _, found := cache.generations[lookup]; found {
		t.Fatal("completed generation was retained")
	}
}
