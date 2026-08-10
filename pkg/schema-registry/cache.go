package schemaregistry

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"
)

var (
	// ErrNotFound marks an authoritative provider absence.
	ErrNotFound = errors.New("schema registry: not found")
	// ErrUnavailable marks a provider or policy that cannot resolve now.
	ErrUnavailable = errors.New("schema registry: unavailable")
	// ErrOfflineMiss marks a cache-only lookup with no usable local entry.
	ErrOfflineMiss = errors.New("schema registry: offline cache miss")
	// ErrResolutionMismatch marks a provider response whose identity does not
	// match the requested selector and therefore cannot be cached safely.
	ErrResolutionMismatch = errors.New("schema registry: resolution identity mismatch")
)

// Resolver is the minimal schema resolution boundary wrapped by ResolveCache.
type Resolver interface {
	Resolve(context.Context, Lookup) (ResolveResult, error)
}

// AvailabilityPolicy selects outage and offline behavior for each lookup.
type AvailabilityPolicy string

const (
	// FailClosed returns the current upstream failure rather than cached stale data.
	FailClosed AvailabilityPolicy = "fail-closed"
	// AllowStale permits an eligible validated entry only during provider unavailability.
	AllowStale AvailabilityPolicy = "allow-stale"
	// CacheOnly prohibits provider I/O and resolves only from local entries.
	CacheOnly AvailabilityPolicy = "cache-only"
	// ReturnUnavailable prohibits lookup and immediately reports unavailability.
	ReturnUnavailable AvailabilityPolicy = "unavailable"
)

// CacheState exposes how a resolution was obtained.
type CacheState string

const (
	// CacheLoaded identifies a result loaded during this call.
	CacheLoaded CacheState = "loaded"
	// CacheFresh identifies a fresh positive cache hit.
	CacheFresh CacheState = "fresh"
	// CacheStale identifies an explicitly permitted stale positive hit.
	CacheStale CacheState = "stale"
	// CacheNegative identifies a cached authoritative absence.
	CacheNegative CacheState = "negative"
)

// CacheResolution returns stale causes explicitly instead of hiding outages.
type CacheResolution struct {
	Result     ResolveResult
	State      CacheState
	Age        time.Duration
	StaleCause error
}

// Clock supplies deterministic cache time.
type Clock interface{ Now() time.Time }

// ResolveCacheObserver receives bounded metadata after each lookup. It must not
// assume calls are serialized and must not retain request contexts.
type ResolveCacheObserver interface {
	ObserveResolveCache(context.Context, ResolveCacheEvent)
}

// ResolveCacheEvent excludes schema contents, subjects, IDs, and credentials.
type ResolveCacheEvent struct {
	State   CacheState
	Outcome string
}

// ResolveCacheConfig makes cache bounds and freshness policy explicit.
type ResolveCacheConfig struct {
	MaxEntries    int
	MaxConcurrent int
	FreshFor      time.Duration
	StaleFor      time.Duration
	NegativeFor   time.Duration
	Clock         Clock
	Observer      ResolveCacheObserver
}

type resolveCacheEntry struct {
	result     ResolveResult
	storedAt   time.Time
	freshUntil time.Time
	staleUntil time.Time
	negative   bool
	sequence   uint64
}

type resolveFlight struct {
	done       chan struct{}
	resolution CacheResolution
	err        error
}

// ResolveCache is a bounded positive and negative cache. A single caller owns
// each synchronous upstream load; waiters may cancel independently. It starts
// no goroutines.
type ResolveCache struct {
	resolver Resolver
	config   ResolveCacheConfig
	slots    chan struct{}

	mu       sync.Mutex
	entries  map[Lookup]resolveCacheEntry
	flights  map[Lookup]*resolveFlight
	sequence uint64
}

// NewResolveCache validates and constructs a cache.
func NewResolveCache(resolver Resolver, config ResolveCacheConfig) (*ResolveCache, error) {
	if interfaceIsNil(resolver) || interfaceIsNil(config.Clock) {
		return nil, fmt.Errorf("%w: resolver and clock are required", ErrInvalidRequest)
	}
	if config.MaxEntries <= 0 || config.MaxConcurrent <= 0 ||
		config.FreshFor <= 0 || config.StaleFor < 0 || config.NegativeFor <= 0 {
		return nil, fmt.Errorf("%w: cache config", ErrInvalidRequest)
	}
	return &ResolveCache{
		resolver: resolver,
		config:   config,
		slots:    make(chan struct{}, config.MaxConcurrent),
		entries:  make(map[Lookup]resolveCacheEntry, config.MaxEntries),
		flights:  make(map[Lookup]*resolveFlight),
	}, nil
}

// Resolve applies one explicit availability policy.
func (cache *ResolveCache) Resolve(
	ctx context.Context,
	lookup Lookup,
	policy AvailabilityPolicy,
) (resolution CacheResolution, err error) {
	defer func() { cache.observe(ctx, resolution, err) }()
	if err := ctx.Err(); err != nil {
		return CacheResolution{}, err
	}
	if !validAvailabilityPolicy(policy) {
		return CacheResolution{}, fmt.Errorf("%w: availability policy %q", ErrInvalidRequest, policy)
	}
	if lookup.kind == "" {
		return CacheResolution{}, fmt.Errorf("%w: empty lookup", ErrInvalidRequest)
	}
	if policy == ReturnUnavailable {
		return CacheResolution{}, ErrUnavailable
	}

	now := cache.config.Clock.Now().Round(0)
	entry, found := cache.entry(lookup, now)
	if found && entry.negative {
		return CacheResolution{State: CacheNegative, Age: nonNegativeAge(now, entry.storedAt)}, ErrNotFound
	}
	if found && now.Before(entry.freshUntil) {
		return CacheResolution{Result: entry.result, State: CacheFresh, Age: nonNegativeAge(now, entry.storedAt)}, nil
	}
	if policy == CacheOnly {
		if found && now.Before(entry.staleUntil) {
			return CacheResolution{Result: entry.result, State: CacheStale, Age: nonNegativeAge(now, entry.storedAt)}, nil
		}
		return CacheResolution{}, ErrOfflineMiss
	}

	flight, leader := cache.flight(lookup)
	if !leader {
		select {
		case <-ctx.Done():
			return CacheResolution{}, ctx.Err()
		case <-flight.done:
			return applyStalePolicy(flight.resolution, flight.err, policy, found, entry, now)
		}
	}

	resolution, err = cache.load(ctx, lookup)
	cache.finishFlight(lookup, flight, resolution, err)
	return applyStalePolicy(resolution, err, policy, found, entry, now)
}

// Invalidate removes both positive and negative state for one selector.
func (cache *ResolveCache) Invalidate(lookup Lookup) error {
	if lookup.kind == "" {
		return fmt.Errorf("%w: empty lookup", ErrInvalidRequest)
	}
	cache.mu.Lock()
	delete(cache.entries, lookup)
	cache.mu.Unlock()
	return nil
}

// Prime adds one explicitly preloaded positive result without provider I/O.
// It is fresh for the configured freshness interval and remains eligible for
// the configured stale interval.
func (cache *ResolveCache) Prime(lookup Lookup, result ResolveResult) error {
	if err := lookup.validate(lookup.providerID.Provider); err != nil {
		return err
	}
	if err := validateResolution(lookup, result); err != nil {
		return err
	}
	now := cache.config.Clock.Now().Round(0)
	cache.store(lookup, resolveCacheEntry{
		result: result, storedAt: now, freshUntil: now.Add(cache.config.FreshFor),
		staleUntil: now.Add(cache.config.FreshFor + cache.config.StaleFor),
	})
	return nil
}

func (cache *ResolveCache) load(ctx context.Context, lookup Lookup) (CacheResolution, error) {
	select {
	case cache.slots <- struct{}{}:
		defer func() { <-cache.slots }()
	case <-ctx.Done():
		return CacheResolution{}, ctx.Err()
	}
	result, err := cache.resolver.Resolve(ctx, lookup)
	now := cache.config.Clock.Now().Round(0)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			cache.store(lookup, resolveCacheEntry{
				storedAt:   now,
				freshUntil: now.Add(cache.config.NegativeFor),
				staleUntil: now.Add(cache.config.NegativeFor),
				negative:   true,
			})
			return CacheResolution{State: CacheNegative}, ErrNotFound
		}
		return CacheResolution{}, err
	}
	if err := validateResolution(lookup, result); err != nil {
		return CacheResolution{}, err
	}
	cache.store(lookup, resolveCacheEntry{
		result:     result,
		storedAt:   now,
		freshUntil: now.Add(cache.config.FreshFor),
		staleUntil: now.Add(cache.config.FreshFor + cache.config.StaleFor),
	})
	return CacheResolution{Result: result, State: CacheLoaded}, nil
}

func validateResolution(lookup Lookup, result ResolveResult) error {
	if result.Schema.Fingerprint() == (Fingerprint{}) ||
		result.ID.Provider == "" || result.ID.Value == "" || !result.Lifecycle.valid() {
		return ErrResolutionMismatch
	}
	matches := false
	switch lookup.kind {
	case LookupByProviderID:
		matches = result.ID == lookup.providerID
	case LookupByFingerprint:
		matches = result.Schema.Fingerprint() == lookup.fingerprint
	case LookupByVersion:
		matches = result.Subject == lookup.subject && result.Version == lookup.version
	case LookupLatest:
		matches = result.Subject == lookup.subject && result.Version.valid()
	}
	if !matches {
		return ErrResolutionMismatch
	}
	return nil
}

func (state LifecycleState) valid() bool {
	switch state {
	case LifecyclePending, LifecycleAvailable, LifecycleDeleting, LifecycleDeleted, LifecycleFailed, LifecycleUnknown:
		return true
	default:
		return false
	}
}

func (cache *ResolveCache) entry(lookup Lookup, now time.Time) (resolveCacheEntry, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry, found := cache.entries[lookup]
	if !found {
		return resolveCacheEntry{}, false
	}
	expires := entry.staleUntil
	if entry.negative {
		expires = entry.freshUntil
	}
	if !now.Before(expires) {
		delete(cache.entries, lookup)
		return resolveCacheEntry{}, false
	}
	cache.sequence++
	entry.sequence = cache.sequence
	cache.entries[lookup] = entry
	return entry, true
}

func (cache *ResolveCache) store(lookup Lookup, entry resolveCacheEntry) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.sequence++
	entry.sequence = cache.sequence
	if _, exists := cache.entries[lookup]; !exists && len(cache.entries) >= cache.config.MaxEntries {
		oldestLookup := slices.MinFunc(slices.Collect(maps.Keys(cache.entries)), func(left, right Lookup) int {
			return cmp.Compare(cache.entries[left].sequence, cache.entries[right].sequence)
		})
		delete(cache.entries, oldestLookup)
	}
	cache.entries[lookup] = entry
}

func (cache *ResolveCache) flight(lookup Lookup) (*resolveFlight, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if flight, found := cache.flights[lookup]; found {
		return flight, false
	}
	flight := &resolveFlight{done: make(chan struct{})}
	cache.flights[lookup] = flight
	return flight, true
}

func (cache *ResolveCache) finishFlight(
	lookup Lookup,
	flight *resolveFlight,
	resolution CacheResolution,
	err error,
) {
	cache.mu.Lock()
	flight.resolution = resolution
	flight.err = err
	delete(cache.flights, lookup)
	close(flight.done)
	cache.mu.Unlock()
}

func (cache *ResolveCache) observe(ctx context.Context, resolution CacheResolution, err error) {
	if interfaceIsNil(cache.config.Observer) {
		return
	}
	// Metrics must never change registry behavior. An observer is deliberately
	// best-effort and receives no sensitive data.
	defer func() { _ = recover() }()
	outcome := "success"
	if err != nil {
		outcome = "error"
	}
	cache.config.Observer.ObserveResolveCache(ctx, ResolveCacheEvent{State: resolution.State, Outcome: outcome})
}

func validAvailabilityPolicy(policy AvailabilityPolicy) bool {
	switch policy {
	case FailClosed, AllowStale, CacheOnly, ReturnUnavailable:
		return true
	default:
		return false
	}
}

func nonNegativeAge(now, storedAt time.Time) time.Duration {
	return max(now.Sub(storedAt), 0)
}

func applyStalePolicy(
	resolution CacheResolution,
	err error,
	policy AvailabilityPolicy,
	found bool,
	entry resolveCacheEntry,
	now time.Time,
) (CacheResolution, error) {
	if !errors.Is(err, ErrUnavailable) || policy != AllowStale || !found || !now.Before(entry.staleUntil) {
		return resolution, err
	}
	return CacheResolution{
		Result:     entry.result,
		State:      CacheStale,
		Age:        nonNegativeAge(now, entry.storedAt),
		StaleCause: err,
	}, nil
}
