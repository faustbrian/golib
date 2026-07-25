package apihttp

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sync"
	"time"
)

const maxRateLimitKeyBytes = 512

// ErrInvalidRateLimitConfiguration reports unusable admission bounds.
var ErrInvalidRateLimitConfiguration = errors.New("apihttp: invalid rate limit configuration")

// NewRateLimitMiddleware creates an authentication-aware admission layer. It
// must be composed after authentication so stable subjects take precedence
// over source addresses.
func NewRateLimitMiddleware(
	limiter RateLimiter,
) (func(http.Handler) http.Handler, error) {
	if limiter == nil || (reflect.ValueOf(limiter).Kind() == reflect.Pointer &&
		reflect.ValueOf(limiter).IsNil()) {
		return nil, ErrInvalidRateLimitConfiguration
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if !limiter.Allow(request.Context(), rateLimitKey(request)) {
				writer.Header().Set("Retry-After", "1")
				writeProblem(writer, http.StatusTooManyRequests, "rate_limited")

				return
			}

			next.ServeHTTP(writer, request)
		})
	}, nil
}

type fixedWindow struct {
	started time.Time
	count   uint32
}

// FixedWindowRateLimiter keeps a bounded number of in-memory admission
// counters. Capacity exhaustion fails closed for previously unseen keys.
type FixedWindowRateLimiter struct {
	mu        sync.Mutex
	limit     uint32
	window    time.Duration
	maxKeys   int
	now       func() time.Time
	windows   map[string]fixedWindow
	nextSweep time.Time
}

// NewFixedWindowRateLimiter creates a process-local bounded rate limiter.
func NewFixedWindowRateLimiter(
	limit uint32,
	window time.Duration,
	maxKeys int,
	now func() time.Time,
) (*FixedWindowRateLimiter, error) {
	if limit == 0 || window <= 0 || maxKeys <= 0 || now == nil {
		return nil, ErrInvalidRateLimitConfiguration
	}

	return &FixedWindowRateLimiter{
		limit:   limit,
		window:  window,
		maxKeys: maxKeys,
		now:     now,
		windows: make(map[string]fixedWindow, maxKeys),
	}, nil
}

// Allow admits at most the configured number of calls for one bounded key in
// each fixed window.
func (limiter *FixedWindowRateLimiter) Allow(ctx context.Context, key string) bool {
	if ctx == nil || ctx.Err() != nil || key == "" || len(key) > maxRateLimitKeyBytes {
		return false
	}

	now := limiter.now()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	if current, exists := limiter.windows[key]; exists {
		elapsed := now.Sub(current.started)
		if elapsed < 0 {
			return false
		}
		if elapsed >= limiter.window {
			limiter.windows[key] = fixedWindow{started: now, count: 1}

			return true
		}
		if current.count >= limiter.limit {
			return false
		}
		current.count++
		limiter.windows[key] = current

		return true
	}

	if len(limiter.windows) >= limiter.maxKeys {
		limiter.sweepExpired(now)
		if len(limiter.windows) >= limiter.maxKeys {
			return false
		}
	}
	limiter.windows[key] = fixedWindow{started: now, count: 1}

	return true
}

func (limiter *FixedWindowRateLimiter) sweepExpired(now time.Time) {
	if !limiter.nextSweep.IsZero() && now.Before(limiter.nextSweep) {
		return
	}
	for key, window := range limiter.windows {
		if !now.Before(window.started) && now.Sub(window.started) >= limiter.window {
			delete(limiter.windows, key)
		}
	}
	limiter.nextSweep = now.Add(limiter.window)
}
