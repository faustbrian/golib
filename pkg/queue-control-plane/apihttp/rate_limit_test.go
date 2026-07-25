package apihttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
)

func TestNewRateLimitMiddlewareRejectsMissingLimiter(t *testing.T) {
	t.Parallel()

	var typedNil *rateLimiterStub
	for _, limiter := range []RateLimiter{nil, typedNil} {
		middleware, err := NewRateLimitMiddleware(limiter)
		if middleware != nil || !errors.Is(err, ErrInvalidRateLimitConfiguration) {
			t.Fatalf("NewRateLimitMiddleware() returned middleware=%t, error=%v", middleware != nil, err)
		}
	}
}

func TestRateLimitMiddlewareUsesPrincipalAndFailsClosed(t *testing.T) {
	t.Parallel()

	principal, err := authentication.NewPrincipal(authentication.PrincipalSpec{
		Subject: "operator-1",
		Method:  "api-key",
	})
	if err != nil {
		t.Fatalf("NewPrincipal() error = %v", err)
	}
	for name, tt := range map[string]struct {
		allow      bool
		wantStatus int
		wantCalls  int
	}{
		"allowed": {allow: true, wantStatus: http.StatusNoContent, wantCalls: 1},
		"denied":  {wantStatus: http.StatusTooManyRequests},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			limiter := &rateLimiterStub{allow: tt.allow}
			middleware, err := NewRateLimitMiddleware(limiter)
			if err != nil {
				t.Fatalf("NewRateLimitMiddleware() error = %v", err)
			}
			calls := 0
			handler := middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				calls++
				writer.WriteHeader(http.StatusNoContent)
			}))
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request = request.WithContext(authentication.ContextWithPrincipal(
				request.Context(),
				principal,
			))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != tt.wantStatus || calls != tt.wantCalls ||
				limiter.key != "subject:operator-1" {
				t.Fatalf("response = %d, calls = %d, key = %q", response.Code, calls, limiter.key)
			}
			if !tt.allow && response.Header().Get("Retry-After") != "1" {
				t.Fatalf("Retry-After = %q", response.Header().Get("Retry-After"))
			}
		})
	}
}

func TestNewFixedWindowRateLimiterRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	now := func() time.Time { return time.Unix(1, 0) }
	for _, input := range []struct {
		limit   uint32
		window  time.Duration
		maxKeys int
		now     func() time.Time
	}{
		{window: time.Second, maxKeys: 1, now: now},
		{limit: 1, maxKeys: 1, now: now},
		{limit: 1, window: -time.Second, maxKeys: 1, now: now},
		{limit: 1, window: time.Second, now: now},
		{limit: 1, window: time.Second, maxKeys: -1, now: now},
		{limit: 1, window: time.Second, maxKeys: 1},
	} {
		limiter, err := NewFixedWindowRateLimiter(input.limit, input.window, input.maxKeys, input.now)
		if limiter != nil || !errors.Is(err, ErrInvalidRateLimitConfiguration) {
			t.Fatalf("NewFixedWindowRateLimiter() = (%v, %v), want nil and stable error", limiter, err)
		}
	}
}

func TestFixedWindowRateLimiterBoundsRequestsAndResets(t *testing.T) {
	t.Parallel()

	clock := &rateLimitClock{now: time.Unix(10, 0)}
	limiter, err := NewFixedWindowRateLimiter(2, time.Minute, 2, clock.Now)
	if err != nil {
		t.Fatalf("NewFixedWindowRateLimiter() error = %v", err)
	}
	first := limiter.Allow(context.Background(), "subject:operator")
	second := limiter.Allow(context.Background(), "subject:operator")
	third := limiter.Allow(context.Background(), "subject:operator")
	if !first || !second || third {
		t.Fatal("Allow() did not enforce two requests per window")
	}

	clock.Advance(time.Minute)
	if !limiter.Allow(context.Background(), "subject:operator") {
		t.Fatal("Allow() rejected the first request in the next window")
	}

	clock.Advance(-2 * time.Minute)
	if limiter.Allow(context.Background(), "subject:operator") {
		t.Fatal("Allow() accepted a request after the clock moved backwards")
	}
}

func TestFixedWindowRateLimiterBoundsAndReclaimsRetainedKeys(t *testing.T) {
	t.Parallel()

	clock := &rateLimitClock{now: time.Unix(10, 0)}
	limiter, err := NewFixedWindowRateLimiter(1, time.Minute, 2, clock.Now)
	if err != nil {
		t.Fatalf("NewFixedWindowRateLimiter() error = %v", err)
	}
	if !limiter.Allow(context.Background(), "address:one") ||
		!limiter.Allow(context.Background(), "address:two") {
		t.Fatal("Allow() rejected keys within capacity")
	}
	if limiter.Allow(context.Background(), "address:three") ||
		limiter.Allow(context.Background(), "address:four") {
		t.Fatal("Allow() accepted keys beyond capacity")
	}

	clock.Advance(time.Minute)
	if !limiter.Allow(context.Background(), "address:three") {
		t.Fatal("Allow() did not reclaim expired key capacity")
	}
}

func TestFixedWindowRateLimiterFailsClosedForInvalidAdmission(t *testing.T) {
	t.Parallel()

	limiter, err := NewFixedWindowRateLimiter(1, time.Minute, 1, time.Now)
	if err != nil {
		t.Fatalf("NewFixedWindowRateLimiter() error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, input := range []struct {
		ctx context.Context
		key string
	}{
		{ctx: cancelled, key: "subject:operator"},
		{ctx: context.Background()},
		{ctx: context.Background(), key: strings.Repeat("x", maxRateLimitKeyBytes+1)},
	} {
		if limiter.Allow(input.ctx, input.key) {
			t.Fatalf("Allow(%q) accepted invalid admission", input.key)
		}
	}
}

func TestFixedWindowRateLimiterIsConcurrencySafe(t *testing.T) {
	t.Parallel()

	limiter, err := NewFixedWindowRateLimiter(50, time.Minute, 1, time.Now)
	if err != nil {
		t.Fatalf("NewFixedWindowRateLimiter() error = %v", err)
	}
	var wait sync.WaitGroup
	accepted := make(chan struct{}, 100)
	for range 100 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if limiter.Allow(context.Background(), "subject:operator") {
				accepted <- struct{}{}
			}
		}()
	}
	wait.Wait()
	close(accepted)
	if len(accepted) != 50 {
		t.Fatalf("accepted = %d, want 50", len(accepted))
	}
}

type rateLimitClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *rateLimitClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()

	return clock.now
}

func (clock *rateLimitClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(duration)
}
