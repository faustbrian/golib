package httpclient

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// ErrInvalidRateLimitPolicy indicates malformed limiter configuration.
	ErrInvalidRateLimitPolicy = errors.New("invalid HTTP rate limit policy")
	// ErrRateLimitWaitExceeded indicates that admission exceeds its wait bound.
	ErrRateLimitWaitExceeded = errors.New("HTTP rate limit wait exceeds bound")
	// ErrRateLimitCapacity indicates that a bounded limiter queue is full.
	ErrRateLimitCapacity = errors.New("HTTP rate limit capacity exhausted")
)

const (
	defaultRateLimitMaximumWait        = 30 * time.Second
	defaultRateLimitMaximumServerDelay = time.Minute
	rateLimitMiddlewarePriority        = -750
)

// RateLimiter admits one physical request and can defer future admission.
// Implementations must be safe for concurrent use.
type RateLimiter interface {
	Acquire(context.Context, time.Duration) (time.Duration, error)
	DeferUntil(time.Time)
	Now() time.Time
}

// RateLimitObserver derives a future admission delay from one response. It
// must not mutate or consume the response.
type RateLimitObserver interface {
	Delay(*http.Response, time.Time) (time.Duration, bool, error)
}

// RateLimitObserverFunc adapts a function to RateLimitObserver.
type RateLimitObserverFunc func(*http.Response, time.Time) (time.Duration, bool, error)

// Delay implements RateLimitObserver.
func (function RateLimitObserverFunc) Delay(
	response *http.Response,
	now time.Time,
) (time.Duration, bool, error) {
	return function(response, now)
}

// RateLimitOptions configures attempt admission and response observation.
type RateLimitOptions struct {
	Name               string
	Layer              MiddlewareLayer
	Priority           int
	Limiter            RateLimiter
	Observer           RateLimitObserver
	MaximumWait        time.Duration
	MaximumServerDelay time.Duration
}

// RateLimitError reports admission or observation failure without rendering a
// custom limiter cause or response header value.
type RateLimitError struct {
	Wait  time.Duration
	Cause error
}

// Error implements error without rendering potentially sensitive causes.
func (*RateLimitError) Error() string { return "HTTP rate limit admission failed" }

// Unwrap returns the limiter or observation failure.
func (err *RateLimitError) Unwrap() error { return err.Cause }

type rateLimitOperationState struct {
	mu              sync.Mutex
	initialAdmitted bool
}

type rateLimitOperationContextKey struct{}

// NewRateLimitMiddleware creates attempt request and response middleware.
func NewRateLimitMiddleware(options RateLimitOptions) ([]Middleware, error) {
	if nilLike(options.Limiter) {
		return nil, fmt.Errorf("%w: limiter is nil", ErrInvalidRateLimitPolicy)
	}
	maximumWait := options.MaximumWait
	if maximumWait == 0 {
		maximumWait = defaultRateLimitMaximumWait
	}
	maximumServerDelay := options.MaximumServerDelay
	if maximumServerDelay == 0 {
		maximumServerDelay = defaultRateLimitMaximumServerDelay
	}
	if maximumWait < 0 || maximumServerDelay < 0 {
		return nil, fmt.Errorf("%w: duration bound is negative", ErrInvalidRateLimitPolicy)
	}
	observer := options.Observer
	if observer == nil {
		observer = retryAfterRateLimitObserver{}
	} else if nilLike(observer) {
		return nil, fmt.Errorf("%w: observer is nil", ErrInvalidRateLimitPolicy)
	}
	metadata := MiddlewareOptions{
		Name: options.Name, Scope: ScopeOperation, Layer: options.Layer,
		Priority: rateLimitMiddlewarePriority + options.Priority,
	}
	admission, err := newMiddleware(metadata, StageRequest)
	if err != nil {
		return nil, err
	}
	admission.around = func(request *http.Request, next Next) (*http.Response, error) {
		if err := acquireRateLimit(request.Context(), options.Limiter, maximumWait); err != nil {
			return nil, err
		}
		state := &rateLimitOperationState{}
		ctx := context.WithValue(request.Context(), rateLimitOperationContextKey{}, state)

		return next(request.WithContext(ctx))
	}
	attemptAdmission := admission
	attemptAdmission.information.Scope = ScopeAttempt
	attemptAdmission.around = func(request *http.Request, next Next) (*http.Response, error) {
		state, ok := request.Context().Value(rateLimitOperationContextKey{}).(*rateLimitOperationState)
		if !ok || state == nil {
			return nil, &RateLimitError{Cause: ErrInvalidRateLimitPolicy}
		}
		state.mu.Lock()
		if !state.initialAdmitted {
			state.initialAdmitted = true
			state.mu.Unlock()

			return next(request)
		}
		state.mu.Unlock()
		if err := acquireRateLimit(request.Context(), options.Limiter, maximumWait); err != nil {
			return nil, err
		}

		return next(request)
	}
	observation := attemptAdmission
	observation.information.Stage = StageResponse
	observation.around = nil
	observation.response = func(
		_ *http.Request,
		response *http.Response,
	) (*http.Response, error) {
		now := options.Limiter.Now()
		delay, observed, observeErr := observer.Delay(response, now)
		if observeErr != nil {
			return nil, &RateLimitError{Cause: observeErr}
		}
		if observed {
			if delay < 0 {
				return nil, &RateLimitError{Cause: ErrInvalidRateLimitPolicy}
			}
			if delay > maximumServerDelay {
				delay = maximumServerDelay
			}
			options.Limiter.DeferUntil(now.Add(delay))
		}

		return response, nil
	}

	return []Middleware{admission, attemptAdmission, observation}, nil
}

func acquireRateLimit(ctx context.Context, limiter RateLimiter, maximumWait time.Duration) error {
	wait, acquireErr := limiter.Acquire(ctx, maximumWait)
	if acquireErr != nil {
		return &RateLimitError{Wait: wait, Cause: acquireErr}
	}
	if wait < 0 || wait > maximumWait {
		return &RateLimitError{Wait: wait, Cause: ErrInvalidRateLimitPolicy}
	}

	return nil
}

// FixedWindowOptions configures a fixed-window request limiter.
type FixedWindowOptions struct {
	Limit  int
	Window time.Duration
	Clock  RetryClock
}

// SlidingWindowOptions configures an exact sliding-window request limiter.
type SlidingWindowOptions struct {
	Limit  int
	Window time.Duration
	Clock  RetryClock
}

// TokenBucketOptions configures a continuously refilled token bucket. Rate is
// tokens per second and Burst is the maximum stored token count.
type TokenBucketOptions struct {
	Rate  float64
	Burst int
	Clock RetryClock
}

// LeakyBucketOptions configures constant-rate admission with a bounded queue.
// Rate is requests per second.
type LeakyBucketOptions struct {
	Rate     float64
	Capacity int
	Clock    RetryClock
}

type rateLimitCore struct {
	clock        RetryClock
	blockedUntil time.Time
}

func newRateLimitCore(clock RetryClock) (rateLimitCore, error) {
	if clock == nil {
		clock = systemRetryClock{}
	} else if nilLike(clock) {
		return rateLimitCore{}, fmt.Errorf("%w: clock is nil", ErrInvalidRateLimitPolicy)
	}

	return rateLimitCore{clock: clock}, nil
}

func (core *rateLimitCore) candidate(now time.Time) time.Time {
	if core.blockedUntil.After(now) {
		return core.blockedUntil
	}

	return now
}

func (core *rateLimitCore) deferUntil(deadline time.Time) {
	if deadline.After(core.blockedUntil) {
		core.blockedUntil = deadline
	}
}

func waitForRateLimit(ctx context.Context, clock RetryClock, wait time.Duration, maximum time.Duration) (time.Duration, error) {
	if ctx == nil {
		return wait, fmt.Errorf("%w: context is nil", ErrInvalidRateLimitPolicy)
	}
	if maximum < 0 {
		return wait, ErrInvalidRateLimitPolicy
	}
	if err := ctx.Err(); err != nil {
		return wait, err
	}
	if wait < 0 {
		wait = 0
	}
	if wait > maximum {
		return wait, ErrRateLimitWaitExceeded
	}
	if err := clock.Wait(ctx, wait); err != nil {
		return wait, err
	}

	return wait, nil
}

type fixedWindowLimiter struct {
	mu          sync.Mutex
	core        rateLimitCore
	limit       int
	window      time.Duration
	windowStart time.Time
	used        int
}

// NewFixedWindowLimiter constructs a fixed-window limiter.
func NewFixedWindowLimiter(options FixedWindowOptions) (RateLimiter, error) {
	if options.Limit < 1 || options.Window <= 0 {
		return nil, fmt.Errorf("%w: fixed window bounds are invalid", ErrInvalidRateLimitPolicy)
	}
	core, err := newRateLimitCore(options.Clock)
	if err != nil {
		return nil, err
	}

	return &fixedWindowLimiter{core: core, limit: options.Limit, window: options.Window}, nil
}

func (limiter *fixedWindowLimiter) Acquire(ctx context.Context, maximum time.Duration) (time.Duration, error) {
	limiter.mu.Lock()
	now := limiter.core.clock.Now()
	candidate := limiter.core.candidate(now)
	start := limiter.windowStart
	used := limiter.used
	if start.IsZero() || !candidate.Before(start.Add(limiter.window)) {
		start = candidate
		used = 0
	}
	if used >= limiter.limit {
		start = start.Add(limiter.window)
		used = 0
	}
	if candidate.Before(start) {
		candidate = start
	}
	wait := candidate.Sub(now)
	if wait > maximum || ctx == nil || ctx.Err() != nil {
		limiter.mu.Unlock()

		return waitForRateLimit(ctx, limiter.core.clock, wait, maximum)
	}
	limiter.windowStart = start
	limiter.used = used + 1
	limiter.mu.Unlock()

	return waitForRateLimit(ctx, limiter.core.clock, wait, maximum)
}

func (limiter *fixedWindowLimiter) DeferUntil(deadline time.Time) {
	limiter.mu.Lock()
	limiter.core.deferUntil(deadline)
	limiter.mu.Unlock()
}

// Now returns the limiter clock's current time.
func (limiter *fixedWindowLimiter) Now() time.Time { return limiter.core.clock.Now() }

type slidingWindowLimiter struct {
	mu           sync.Mutex
	core         rateLimitCore
	limit        int
	window       time.Duration
	reservations []time.Time
}

// NewSlidingWindowLimiter constructs an exact sliding-window limiter.
func NewSlidingWindowLimiter(options SlidingWindowOptions) (RateLimiter, error) {
	if options.Limit < 1 || options.Window <= 0 {
		return nil, fmt.Errorf("%w: sliding window bounds are invalid", ErrInvalidRateLimitPolicy)
	}
	core, err := newRateLimitCore(options.Clock)
	if err != nil {
		return nil, err
	}

	return &slidingWindowLimiter{core: core, limit: options.Limit, window: options.Window}, nil
}

func (limiter *slidingWindowLimiter) Acquire(ctx context.Context, maximum time.Duration) (time.Duration, error) {
	limiter.mu.Lock()
	now := limiter.core.clock.Now()
	candidate := limiter.core.candidate(now)
	reservations := append([]time.Time(nil), limiter.reservations...)
	for {
		cutoff := candidate.Add(-limiter.window)
		index := 0
		for index < len(reservations) && !reservations[index].After(cutoff) {
			index++
		}
		reservations = reservations[index:]
		if len(reservations) < limiter.limit {
			break
		}
		candidate = reservations[0].Add(limiter.window)
	}
	wait := candidate.Sub(now)
	if wait > maximum || ctx == nil || ctx.Err() != nil {
		limiter.mu.Unlock()

		return waitForRateLimit(ctx, limiter.core.clock, wait, maximum)
	}
	limiter.reservations = append(reservations, candidate)
	limiter.mu.Unlock()

	return waitForRateLimit(ctx, limiter.core.clock, wait, maximum)
}

func (limiter *slidingWindowLimiter) DeferUntil(deadline time.Time) {
	limiter.mu.Lock()
	limiter.core.deferUntil(deadline)
	limiter.mu.Unlock()
}

// Now returns the limiter clock's current time.
func (limiter *slidingWindowLimiter) Now() time.Time { return limiter.core.clock.Now() }

type tokenBucketLimiter struct {
	mu     sync.Mutex
	core   rateLimitCore
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

// NewTokenBucketLimiter constructs a token-bucket limiter.
func NewTokenBucketLimiter(options TokenBucketOptions) (RateLimiter, error) {
	if !validRate(options.Rate) || options.Burst < 1 {
		return nil, fmt.Errorf("%w: token bucket bounds are invalid", ErrInvalidRateLimitPolicy)
	}
	core, err := newRateLimitCore(options.Clock)
	if err != nil {
		return nil, err
	}

	return &tokenBucketLimiter{
		core: core, rate: options.Rate, burst: float64(options.Burst), tokens: float64(options.Burst),
	}, nil
}

func (limiter *tokenBucketLimiter) Acquire(ctx context.Context, maximum time.Duration) (time.Duration, error) {
	limiter.mu.Lock()
	now := limiter.core.clock.Now()
	candidate := limiter.core.candidate(now)
	tokens := limiter.tokens
	last := limiter.last
	if last.IsZero() {
		last = candidate
	}
	if candidate.Before(last) {
		candidate = last
	} else {
		tokens = math.Min(limiter.burst, tokens+candidate.Sub(last).Seconds()*limiter.rate)
		last = candidate
	}
	if tokens < 1 {
		missing := 1 - tokens
		delay := time.Duration(math.Ceil(missing / limiter.rate * float64(time.Second)))
		candidate = candidate.Add(delay)
		tokens = math.Min(limiter.burst, tokens+delay.Seconds()*limiter.rate)
		last = candidate
	}
	wait := candidate.Sub(now)
	if wait > maximum || ctx == nil || ctx.Err() != nil {
		limiter.mu.Unlock()

		return waitForRateLimit(ctx, limiter.core.clock, wait, maximum)
	}
	limiter.tokens = tokens - 1
	limiter.last = last
	limiter.mu.Unlock()

	return waitForRateLimit(ctx, limiter.core.clock, wait, maximum)
}

func (limiter *tokenBucketLimiter) DeferUntil(deadline time.Time) {
	limiter.mu.Lock()
	limiter.core.deferUntil(deadline)
	limiter.mu.Unlock()
}

// Now returns the limiter clock's current time.
func (limiter *tokenBucketLimiter) Now() time.Time { return limiter.core.clock.Now() }

type leakyBucketLimiter struct {
	mu       sync.Mutex
	core     rateLimitCore
	interval time.Duration
	capacity int
	next     time.Time
}

// NewLeakyBucketLimiter constructs a constant-rate bounded-queue limiter.
func NewLeakyBucketLimiter(options LeakyBucketOptions) (RateLimiter, error) {
	if !validRate(options.Rate) || options.Capacity < 1 {
		return nil, fmt.Errorf("%w: leaky bucket bounds are invalid", ErrInvalidRateLimitPolicy)
	}
	interval := time.Duration(math.Ceil(float64(time.Second) / options.Rate))
	core, err := newRateLimitCore(options.Clock)
	if err != nil {
		return nil, err
	}

	return &leakyBucketLimiter{core: core, interval: interval, capacity: options.Capacity}, nil
}

func (limiter *leakyBucketLimiter) Acquire(ctx context.Context, maximum time.Duration) (time.Duration, error) {
	limiter.mu.Lock()
	now := limiter.core.clock.Now()
	candidate := limiter.core.candidate(now)
	if limiter.next.After(candidate) {
		candidate = limiter.next
	}
	wait := candidate.Sub(now)
	queued := int(math.Ceil(float64(wait) / float64(limiter.interval)))
	if queued >= limiter.capacity {
		limiter.mu.Unlock()

		return wait, ErrRateLimitCapacity
	}
	if wait > maximum || ctx == nil || ctx.Err() != nil {
		limiter.mu.Unlock()

		return waitForRateLimit(ctx, limiter.core.clock, wait, maximum)
	}
	limiter.next = candidate.Add(limiter.interval)
	limiter.mu.Unlock()

	return waitForRateLimit(ctx, limiter.core.clock, wait, maximum)
}

func (limiter *leakyBucketLimiter) DeferUntil(deadline time.Time) {
	limiter.mu.Lock()
	limiter.core.deferUntil(deadline)
	limiter.mu.Unlock()
}

// Now returns the limiter clock's current time.
func (limiter *leakyBucketLimiter) Now() time.Time { return limiter.core.clock.Now() }

func validRate(rate float64) bool {
	minimum := float64(time.Second) / float64(math.MaxInt64)

	return rate >= minimum && !math.IsNaN(rate) && !math.IsInf(rate, 0)
}

type retryAfterRateLimitObserver struct{}

func (retryAfterRateLimitObserver) Delay(response *http.Response, now time.Time) (time.Duration, bool, error) {
	if response == nil {
		return 0, false, nil
	}
	delay, ok := parseRetryAfter(response.Header.Get("Retry-After"), now)

	return delay, ok, nil
}

// RateLimitResetMode identifies vendor reset header representation.
type RateLimitResetMode uint8

const (
	// RateLimitResetDeltaSeconds interprets reset as seconds from observation.
	RateLimitResetDeltaSeconds RateLimitResetMode = iota
	// RateLimitResetUnixSeconds interprets reset as a Unix timestamp.
	RateLimitResetUnixSeconds
	// RateLimitResetHTTPDate interprets reset as an HTTP date.
	RateLimitResetHTTPDate
)

// HeaderRateLimitOptions configures vendor remaining/reset observation.
type HeaderRateLimitOptions struct {
	RemainingHeader string
	ResetHeader     string
	Reset           RateLimitResetMode
}

type headerRateLimitObserver struct {
	remaining string
	reset     string
	mode      RateLimitResetMode
}

// NewHeaderRateLimitObserver creates configurable remaining/reset observation.
func NewHeaderRateLimitObserver(options HeaderRateLimitOptions) (RateLimitObserver, error) {
	remaining := options.RemainingHeader
	if remaining == "" {
		remaining = "RateLimit-Remaining"
	}
	reset := options.ResetHeader
	if reset == "" {
		reset = "RateLimit-Reset"
	}
	remaining, err := validateHeaderName(remaining)
	if err != nil {
		return nil, fmt.Errorf("%w: remaining header is malformed", ErrInvalidRateLimitPolicy)
	}
	reset, err = validateHeaderName(reset)
	if err != nil {
		return nil, fmt.Errorf("%w: reset header is malformed", ErrInvalidRateLimitPolicy)
	}
	if options.Reset > RateLimitResetHTTPDate {
		return nil, fmt.Errorf("%w: reset representation is unknown", ErrInvalidRateLimitPolicy)
	}

	return headerRateLimitObserver{remaining: remaining, reset: reset, mode: options.Reset}, nil
}

func (observer headerRateLimitObserver) Delay(
	response *http.Response,
	now time.Time,
) (time.Duration, bool, error) {
	if response == nil {
		return 0, false, nil
	}
	if delay, ok := parseRetryAfter(response.Header.Get("Retry-After"), now); ok {
		return delay, true, nil
	}
	remaining := strings.TrimSpace(response.Header.Get(observer.remaining))
	reset := strings.TrimSpace(response.Header.Get(observer.reset))
	if remaining == "" || reset == "" {
		return 0, false, nil
	}
	remainingValue, err := strconv.ParseUint(remaining, 10, 63)
	if err != nil || remainingValue != 0 {
		return 0, false, nil
	}

	return observer.parseReset(reset, now)
}

func (observer headerRateLimitObserver) parseReset(value string, now time.Time) (time.Duration, bool, error) {
	switch observer.mode {
	case RateLimitResetDeltaSeconds:
		seconds, err := strconv.ParseUint(value, 10, 31)
		if err != nil {
			return 0, false, nil
		}

		return time.Duration(seconds) * time.Second, true, nil
	case RateLimitResetUnixSeconds:
		seconds, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return 0, false, nil
		}
		delay := time.Unix(seconds, 0).Sub(now)
		if delay < 0 {
			delay = 0
		}

		return delay, true, nil
	case RateLimitResetHTTPDate:
		date, err := http.ParseTime(value)
		if err != nil {
			return 0, false, nil
		}
		delay := date.Sub(now)
		if delay < 0 {
			delay = 0
		}

		return delay, true, nil
	default:
		return 0, false, ErrInvalidRateLimitPolicy
	}
}
