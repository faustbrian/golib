package cache

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"
)

const (
	defaultMaxConcurrentLoaders = 64
	defaultMaxWaitersPerKey     = 1024
	defaultMaxBatch             = 1000
)

// LoadResult is the value and existence result returned by a Loader.
type LoadResult[V any] struct {
	Value V
	Found bool
}

// Loader fetches a logical key from its source of truth.
type Loader[K, V any] func(context.Context, K) (LoadResult[V], error)

// JitterSource chooses how much time to subtract from a loaded value's TTL.
type JitterSource interface {
	Duration(time.Duration) time.Duration
}

// RandomJitter samples a non-cryptographic duration in [0, max).
type RandomJitter struct{}

// Duration returns a non-negative duration below max, or zero for max <= 0.
func (RandomJitter) Duration(upperBound time.Duration) time.Duration {
	if upperBound <= 0 {
		return 0
	}
	// Jitter distributes expirations; it is not used for a security decision.
	return time.Duration(rand.Int64N(int64(upperBound))) // #nosec G404
}

// LoadPolicy bounds loading and enables optional negative and stale behavior.
type LoadPolicy struct {
	MaxConcurrent        int
	MaxWaitersPerKey     int
	NegativeTTL          time.Duration
	StaleWhileRevalidate bool
	StaleIfError         bool
	RefreshJitter        time.Duration
}

// Config contains all dependencies, limits, and policies for a Cache.
type Config[K, V any] struct {
	Backend  Backend
	Keys     KeySpace[K]
	Codec    Codec[V]
	TTL      TTLPolicy
	Clock    Clock
	MaxValue int
	MaxBatch int
	Load     LoadPolicy
	Jitter   JitterSource
	Observer Observer
}

// Cache provides typed cache operations over a backend.
type Cache[K, V any] struct {
	backend   Backend
	keys      KeySpace[K]
	codec     Codec[V]
	ttl       TTLPolicy
	clock     Clock
	maxValue  int
	maxBatch  int
	load      LoadPolicy
	jitter    JitterSource
	observer  Observer
	loadSlots chan struct{}
	loadCtx   context.Context
	cancel    context.CancelFunc
	loadMu    sync.Mutex
	flights   map[string]*loadFlight[V]
	loadWG    sync.WaitGroup
	closed    bool
}

type loadFlight[V any] struct {
	done       chan struct{}
	result     Result[V]
	err        error
	waiters    int
	mutation   sync.Mutex
	superseded bool
}

type loadContextKey struct{}

type activeLoadContext struct{ owner any }

// New validates config and constructs a typed cache.
func New[K, V any](config Config[K, V]) (*Cache[K, V], error) {
	if config.Backend == nil || config.Codec == nil || config.Clock == nil || config.MaxValue <= 0 || config.MaxBatch < 0 {
		return nil, ErrInvalidConfig
	}
	if err := config.TTL.Validate(); err != nil {
		return nil, err
	}
	if config.Load.NegativeTTL < 0 || config.Load.MaxConcurrent < 0 || config.Load.MaxWaitersPerKey < 0 || config.Load.RefreshJitter < 0 {
		return nil, &Error{Kind: PolicyError, Operation: OperationLoad, Cause: ErrInvalidPolicy}
	}
	if config.Load.StaleWhileRevalidate && config.Load.StaleIfError {
		return nil, &Error{Kind: PolicyError, Operation: OperationLoad, Cause: ErrInvalidPolicy}
	}
	if config.Load.RefreshJitter >= config.TTL.TTL && config.Load.RefreshJitter > 0 {
		return nil, &Error{Kind: PolicyError, Operation: OperationLoad, Cause: ErrInvalidPolicy}
	}
	if config.Load.RefreshJitter > 0 && config.Jitter == nil {
		config.Jitter = RandomJitter{}
	}
	if config.Load.MaxConcurrent == 0 {
		config.Load.MaxConcurrent = defaultMaxConcurrentLoaders
	}
	if config.Load.MaxWaitersPerKey == 0 {
		config.Load.MaxWaitersPerKey = defaultMaxWaitersPerKey
	}
	if config.MaxBatch == 0 {
		config.MaxBatch = defaultMaxBatch
	}
	// Close retains and invokes cancel after preventing new flights.
	loadCtx, cancel := context.WithCancel(context.Background()) // #nosec G118
	return &Cache[K, V]{
		backend:   config.Backend,
		keys:      config.Keys,
		codec:     config.Codec,
		ttl:       config.TTL,
		clock:     config.Clock,
		maxValue:  config.MaxValue,
		maxBatch:  config.MaxBatch,
		load:      config.Load,
		jitter:    config.Jitter,
		observer:  config.Observer,
		loadSlots: make(chan struct{}, config.Load.MaxConcurrent),
		loadCtx:   loadCtx,
		cancel:    cancel,
		flights:   make(map[string]*loadFlight[V]),
	}, nil
}

// Get returns an explicit hit, miss, or stale result.
func (c *Cache[K, V]) Get(ctx context.Context, logical K) (result Result[V], err error) {
	start := c.clock.Now()
	size := 0
	defer func() {
		notify(ctx, c.observer, Event{
			Operation: OperationGet,
			Outcome:   resultOutcome(result, err),
			Duration:  elapsed(start, c.clock.Now()),
			Size:      size,
		})
	}()
	var zero Result[V]
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.isClosed() {
		return zero, ErrClosed
	}
	key, err := c.keys.Key(logical)
	if err != nil {
		return zero, &Error{Kind: InvalidKeyError, Operation: OperationGet, Cause: err}
	}
	record, found, err := c.backend.Get(ctx, key)
	if err != nil {
		return zero, operationError(OperationGet, err)
	}
	if !found {
		return Result[V]{State: Miss}, nil
	}
	if err := record.Validate(); err != nil {
		return zero, operationError(OperationGet, err)
	}
	size = len(record.Payload)

	now := c.clock.Now().Round(0)
	if !now.Before(record.StaleAt) {
		if _, err := c.backend.Delete(ctx, key); err != nil {
			return zero, operationError(OperationDelete, err)
		}
		return Result[V]{State: Miss}, nil
	}
	if record.Negative {
		return Result[V]{State: Miss, Negative: true}, nil
	}
	if len(record.Payload) > c.maxValue {
		return zero, &Error{Kind: LimitError, Operation: OperationGet, Cause: ErrValueTooLarge}
	}
	value, err := c.codec.Decode(record.Payload)
	if err != nil {
		kind := DecodeError
		if errors.Is(err, ErrSchemaMismatch) {
			kind = SchemaMismatchError
		}
		return zero, &Error{Kind: kind, Operation: OperationGet, Cause: err}
	}
	if !now.Before(record.ExpiresAt) {
		return Result[V]{State: Stale, Value: value}, nil
	}
	if c.ttl.Sliding {
		record.ExpiresAt = now.Add(c.ttl.TTL)
		record.StaleAt = record.ExpiresAt.Add(c.ttl.StaleFor)
		updated, err := c.backend.Set(ctx, key, record, IfPresent)
		if err != nil {
			return zero, operationError(OperationSet, err)
		}
		if !updated {
			return Result[V]{State: Miss}, nil
		}
	}
	return Result[V]{State: Hit, Value: value}, nil
}

// GetOrLoad returns a cached value or coalesces a bounded source load.
func (c *Cache[K, V]) GetOrLoad(ctx context.Context, logical K, loader Loader[K, V]) (Result[V], error) {
	if active, ok := ctx.Value(loadContextKey{}).(activeLoadContext); ok && active.owner == c {
		return Result[V]{}, &Error{Kind: LoaderError, Operation: OperationLoad, Cause: ErrRecursiveLoad}
	}
	result, err := c.Get(ctx, logical)
	if err != nil || result.State == Hit || result.Negative {
		return result, err
	}
	if loader == nil {
		return Result[V]{}, &Error{Kind: LoaderError, Operation: OperationLoad, Cause: errors.New("nil loader")}
	}
	key, err := c.keys.Key(logical)
	if err != nil {
		return Result[V]{}, &Error{Kind: InvalidKeyError, Operation: OperationLoad, Cause: err}
	}
	if result.State == Stale && c.load.StaleWhileRevalidate {
		if err := c.startBackgroundLoad(key, logical, loader); err != nil {
			return result, err
		}
		return result, nil
	}

	c.loadMu.Lock()
	if c.closed {
		c.loadMu.Unlock()
		return Result[V]{}, ErrClosed
	}
	flight, found := c.flights[key]
	if found {
		if flight.waiters >= c.load.MaxWaitersPerKey {
			c.loadMu.Unlock()
			return Result[V]{}, ErrWaiterLimit
		}
		flight.waiters++
	} else {
		flight = &loadFlight[V]{done: make(chan struct{}), waiters: 1}
		c.flights[key] = flight
		c.loadWG.Add(1)
		go c.runLoad(key, logical, loader, flight)
	}
	c.loadMu.Unlock()

	select {
	case <-ctx.Done():
		c.detachWaiter(flight)
		return Result[V]{}, ctx.Err()
	case <-flight.done:
		c.detachWaiter(flight)
		if flight.err != nil && result.State == Stale && c.load.StaleIfError {
			return result, flight.err
		}
		return flight.result, flight.err
	}
}

func (c *Cache[K, V]) startBackgroundLoad(key string, logical K, loader Loader[K, V]) error {
	c.loadMu.Lock()
	defer c.loadMu.Unlock()
	if c.closed {
		return ErrClosed
	}
	if _, found := c.flights[key]; found {
		return nil
	}
	flight := &loadFlight[V]{done: make(chan struct{})}
	c.flights[key] = flight
	c.loadWG.Add(1)
	go c.runLoad(key, logical, loader, flight)
	return nil
}

func (c *Cache[K, V]) runLoad(key string, logical K, loader Loader[K, V], flight *loadFlight[V]) {
	var loadStarted time.Time
	loadOutcome := OutcomeSuccess
	didLoad := false
	defer c.loadWG.Done()
	defer func() {
		if recovered := recover(); recovered != nil {
			flight.err = &Error{
				Kind:      LoaderError,
				Operation: OperationLoad,
				Cause:     fmt.Errorf("%w: %v", ErrLoaderPanic, recovered),
			}
		}
		if didLoad {
			if flight.err != nil {
				loadOutcome = OutcomeError
			}
			notify(c.loadCtx, c.observer, Event{
				Operation: OperationLoad,
				Outcome:   loadOutcome,
				Duration:  elapsed(loadStarted, c.clock.Now()),
			})
		}
		c.loadMu.Lock()
		delete(c.flights, key)
		close(flight.done)
		c.loadMu.Unlock()
	}()

	current, err := c.Get(c.loadCtx, logical)
	if err != nil {
		flight.err = err
		return
	}
	if current.State == Hit || current.Negative {
		flight.result = current
		return
	}

	select {
	case c.loadSlots <- struct{}{}:
		defer func() { <-c.loadSlots }()
	case <-c.loadCtx.Done():
		flight.err = c.loadCtx.Err()
		return
	}

	didLoad = true
	loadStarted = c.clock.Now()
	loaderCtx := context.WithValue(c.loadCtx, loadContextKey{}, activeLoadContext{owner: c})
	loaded, err := loader(loaderCtx, logical)
	if err != nil {
		flight.err = &Error{Kind: LoaderError, Operation: OperationLoad, Cause: err}
		return
	}
	flight.mutation.Lock()
	if flight.superseded {
		flight.mutation.Unlock()
		current, err := c.Get(c.loadCtx, logical)
		flight.result = current
		flight.err = err
		return
	}
	defer flight.mutation.Unlock()
	if !loaded.Found {
		loadOutcome = OutcomeNegative
		flight.result = Result[V]{State: Miss, Negative: true}
		if c.load.NegativeTTL > 0 {
			now := c.clock.Now().Round(0)
			record := Record{
				ExpiresAt: now.Add(c.load.NegativeTTL),
				StaleAt:   now.Add(c.load.NegativeTTL),
				Negative:  true,
			}
			if err := record.Validate(); err != nil {
				flight.err = err
				return
			}
			_, err := c.backend.Set(c.loadCtx, key, record, Unconditional)
			if err != nil {
				flight.err = operationError(OperationSet, err)
			}
		}
		return
	}
	if err := c.setLoaded(c.loadCtx, logical, loaded.Value); err != nil {
		flight.err = err
		return
	}
	flight.result = Result[V]{State: Hit, Value: loaded.Value}
}

func (c *Cache[K, V]) detachWaiter(flight *loadFlight[V]) {
	c.loadMu.Lock()
	flight.waiters--
	c.loadMu.Unlock()
}

// Close cancels active loads, waits for their cleanup, and rejects new work.
func (c *Cache[K, V]) Close() error {
	c.loadMu.Lock()
	if c.closed {
		c.loadMu.Unlock()
		return nil
	}
	c.closed = true
	c.cancel()
	c.loadMu.Unlock()
	c.loadWG.Wait()
	return nil
}

func (c *Cache[K, V]) isClosed() bool {
	c.loadMu.Lock()
	defer c.loadMu.Unlock()
	return c.closed
}

// Set writes a value without an existence precondition.
func (c *Cache[K, V]) Set(ctx context.Context, logical K, value V) error {
	_, err := c.set(ctx, logical, value, Unconditional, c.ttl.TTL, true)
	return err
}

// Add writes a value only when the key has no live record.
func (c *Cache[K, V]) Add(ctx context.Context, logical K, value V) (bool, error) {
	return c.set(ctx, logical, value, IfAbsent, c.ttl.TTL, true)
}

// Replace writes a value only when the key has a live record.
func (c *Cache[K, V]) Replace(ctx context.Context, logical K, value V) (bool, error) {
	return c.set(ctx, logical, value, IfPresent, c.ttl.TTL, true)
}

func (c *Cache[K, V]) setLoaded(ctx context.Context, logical K, value V) error {
	ttl := c.ttl.TTL
	if c.load.RefreshJitter > 0 {
		jitter := c.jitter.Duration(c.load.RefreshJitter)
		if jitter < 0 || jitter > c.load.RefreshJitter || jitter >= ttl {
			return &Error{Kind: PolicyError, Operation: OperationLoad, Cause: ErrInvalidPolicy}
		}
		ttl -= jitter
	}
	_, err := c.set(ctx, logical, value, Unconditional, ttl, false)
	return err
}

func (c *Cache[K, V]) set(
	ctx context.Context,
	logical K,
	value V,
	condition Condition,
	ttl time.Duration,
	supersede bool,
) (written bool, err error) {
	start := c.clock.Now()
	size := 0
	defer func() {
		outcome := OutcomeSuccess
		if err != nil {
			outcome = OutcomeError
		} else if !written {
			outcome = OutcomeRejected
		}
		notify(ctx, c.observer, Event{
			Operation: OperationSet,
			Outcome:   outcome,
			Duration:  elapsed(start, c.clock.Now()),
			Size:      size,
		})
	}()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if c.isClosed() {
		return false, ErrClosed
	}
	key, err := c.keys.Key(logical)
	if err != nil {
		return false, &Error{Kind: InvalidKeyError, Operation: OperationSet, Cause: err}
	}
	flight := c.lockFlightMutation(key, supersede)
	if flight != nil {
		defer flight.mutation.Unlock()
	}
	payload, err := c.codec.Encode(value)
	if err != nil {
		return false, err
	}
	if len(payload) > c.maxValue {
		return false, &Error{Kind: LimitError, Operation: OperationSet, Cause: ErrValueTooLarge}
	}
	size = len(payload)
	now := c.clock.Now().Round(0)
	record := Record{
		Payload:   payload,
		ExpiresAt: now.Add(ttl),
		StaleAt:   now.Add(ttl).Add(c.ttl.StaleFor),
	}
	if err := record.Validate(); err != nil {
		return false, err
	}
	written, err = c.backend.Set(ctx, key, record, condition)
	if err != nil {
		return false, operationError(OperationSet, err)
	}
	if written && flight != nil {
		flight.superseded = true
	}
	return written, nil
}

// Delete removes a logical key. Deleting an absent key succeeds.
func (c *Cache[K, V]) Delete(ctx context.Context, logical K) (err error) {
	start := c.clock.Now()
	defer func() {
		outcome := OutcomeSuccess
		if err != nil {
			outcome = OutcomeError
		}
		notify(ctx, c.observer, Event{
			Operation: OperationDelete,
			Outcome:   outcome,
			Duration:  elapsed(start, c.clock.Now()),
		})
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.isClosed() {
		return ErrClosed
	}
	key, err := c.keys.Key(logical)
	if err != nil {
		return &Error{Kind: InvalidKeyError, Operation: OperationDelete, Cause: err}
	}
	flight := c.lockFlightMutation(key, true)
	if flight != nil {
		defer flight.mutation.Unlock()
	}
	if _, err := c.backend.Delete(ctx, key); err != nil {
		return operationError(OperationDelete, err)
	}
	if flight != nil {
		flight.superseded = true
	}
	return nil
}

func (c *Cache[K, V]) lockFlightMutation(key string, lock bool) *loadFlight[V] {
	if !lock {
		return nil
	}
	c.loadMu.Lock()
	flight := c.flights[key]
	c.loadMu.Unlock()
	if flight != nil {
		flight.mutation.Lock()
	}
	return flight
}

func operationError(operation Operation, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return &Error{Kind: BackendError, Operation: operation, Cause: err}
}
