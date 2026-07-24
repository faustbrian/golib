package httpclient

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFirstPartyRateLimitAlgorithmsReserveDeterministically(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		new  func(*rateLimitTestClock) (RateLimiter, error)
		want []time.Duration
	}{
		{
			name: "fixed window",
			new: func(clock *rateLimitTestClock) (RateLimiter, error) {
				return NewFixedWindowLimiter(FixedWindowOptions{Limit: 2, Window: 10 * time.Second, Clock: clock})
			},
			want: []time.Duration{0, 0, 10 * time.Second},
		},
		{
			name: "sliding window",
			new: func(clock *rateLimitTestClock) (RateLimiter, error) {
				return NewSlidingWindowLimiter(SlidingWindowOptions{Limit: 2, Window: 10 * time.Second, Clock: clock})
			},
			want: []time.Duration{0, 0, 10 * time.Second},
		},
		{
			name: "token bucket",
			new: func(clock *rateLimitTestClock) (RateLimiter, error) {
				return NewTokenBucketLimiter(TokenBucketOptions{Rate: 1, Burst: 2, Clock: clock})
			},
			want: []time.Duration{0, 0, time.Second, 2 * time.Second},
		},
		{
			name: "leaky bucket",
			new: func(clock *rateLimitTestClock) (RateLimiter, error) {
				return NewLeakyBucketLimiter(LeakyBucketOptions{Rate: 2, Capacity: 3, Clock: clock})
			},
			want: []time.Duration{0, 500 * time.Millisecond, time.Second},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			clock := &rateLimitTestClock{now: time.Unix(1_700_000_000, 0)}
			limiter, err := test.new(clock)
			if err != nil {
				t.Fatalf("construct limiter: %v", err)
			}
			for index, want := range test.want {
				wait, err := limiter.Acquire(context.Background(), time.Minute)
				if err != nil {
					t.Fatalf("acquire %d: %v", index, err)
				}
				if wait != want {
					t.Fatalf("acquire %d wait = %v, want %v", index, wait, want)
				}
			}
		})
	}
}

func TestRateLimitMiddlewareDelaysFutureAdmissionFromResponse(t *testing.T) {
	t.Parallel()

	clock := &rateLimitTestClock{now: time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC), advance: true}
	limiter, err := NewTokenBucketLimiter(TokenBucketOptions{Rate: 100, Burst: 100, Clock: clock})
	if err != nil {
		t.Fatalf("construct limiter: %v", err)
	}
	middleware, err := NewRateLimitMiddleware(RateLimitOptions{
		Name: "vendor-limit", Layer: MiddlewareClient, Limiter: limiter,
		MaximumWait: 5 * time.Second, MaximumServerDelay: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("construct middleware: %v", err)
	}
	attempts := 0
	client, err := New(Config{
		Middleware: middleware,
		Transport: TransportFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			header := make(http.Header)
			if attempts == 1 {
				header.Set("Retry-After", "3")
			}

			return &http.Response{StatusCode: http.StatusNoContent, Header: header}, nil
		}),
	})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer closeTestClient(t, client)
	for range 2 {
		request, _ := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
		response, requestErr := client.Do(request)
		if requestErr != nil {
			t.Fatalf("execute request: %v", requestErr)
		}
		_ = response.Body.Close()
	}
	if got := clock.Delays(); len(got) != 2 || got[0] != 0 || got[1] != 3*time.Second {
		t.Fatalf("admission delays = %v", got)
	}
}

func TestVendorRateLimitHeadersCanDeferAdmission(t *testing.T) {
	t.Parallel()

	clock := &rateLimitTestClock{now: time.Unix(1_700_000_000, 0), advance: true}
	limiter, err := NewFixedWindowLimiter(FixedWindowOptions{Limit: 100, Window: time.Second, Clock: clock})
	if err != nil {
		t.Fatalf("construct limiter: %v", err)
	}
	observer, err := NewHeaderRateLimitObserver(HeaderRateLimitOptions{
		RemainingHeader: "X-RateLimit-Remaining", ResetHeader: "X-RateLimit-Reset",
		Reset: RateLimitResetUnixSeconds,
	})
	if err != nil {
		t.Fatalf("construct observer: %v", err)
	}
	middleware, err := NewRateLimitMiddleware(RateLimitOptions{
		Name: "vendor-limit", Layer: MiddlewareClient, Limiter: limiter,
		Observer: observer, MaximumWait: 10 * time.Second, MaximumServerDelay: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("construct middleware: %v", err)
	}
	attempt := 0
	client, err := New(Config{Middleware: middleware, Transport: TransportFunc(func(*http.Request) (*http.Response, error) {
		attempt++
		header := make(http.Header)
		if attempt == 1 {
			header.Set("X-RateLimit-Remaining", "0")
			header.Set("X-RateLimit-Reset", "1700000004")
		}

		return &http.Response{StatusCode: http.StatusOK, Header: header}, nil
	})})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer closeTestClient(t, client)
	for range 2 {
		request, _ := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
		response, requestErr := client.Do(request)
		if requestErr != nil {
			t.Fatalf("execute request: %v", requestErr)
		}
		_ = response.Body.Close()
	}
	if got := clock.Delays(); len(got) != 2 || got[1] != 4*time.Second {
		t.Fatalf("vendor admission delays = %v", got)
	}
}

func TestRateLimitRejectsWaitBeyondBoundWithoutNetwork(t *testing.T) {
	t.Parallel()

	clock := &rateLimitTestClock{now: time.Unix(1_700_000_000, 0)}
	limiter, err := NewFixedWindowLimiter(FixedWindowOptions{Limit: 1, Window: time.Minute, Clock: clock})
	if err != nil {
		t.Fatalf("construct limiter: %v", err)
	}
	middleware, err := NewRateLimitMiddleware(RateLimitOptions{
		Name: "limit", Layer: MiddlewareClient, Limiter: limiter, MaximumWait: time.Second,
	})
	if err != nil {
		t.Fatalf("construct middleware: %v", err)
	}
	transportCalls := 0
	client, err := New(Config{Middleware: middleware, Transport: TransportFunc(func(*http.Request) (*http.Response, error) {
		transportCalls++

		return &http.Response{StatusCode: http.StatusNoContent}, nil
	})})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer closeTestClient(t, client)
	request, _ := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	_ = response.Body.Close()
	request, _ = http.NewRequest(http.MethodGet, "https://api.example.test", nil)
	_, err = client.Do(request)
	var limitError *RateLimitError
	if !errors.As(err, &limitError) || !errors.Is(err, ErrRateLimitWaitExceeded) {
		t.Fatalf("rate limit error = %#v", err)
	}
	if transportCalls != 1 || strings.Contains(err.Error(), "do-not-render") {
		t.Fatalf("transport calls = %d, error = %q", transportCalls, err)
	}
}

func TestRateLimitConfigurationValidation(t *testing.T) {
	t.Parallel()

	var nilClock *rateLimitTestClock
	var nilLimiter *rateLimitTestLimiter
	var nilObserver *rateLimitTestObserver
	for _, construct := range []func() error{
		func() error { _, err := NewFixedWindowLimiter(FixedWindowOptions{}); return err },
		func() error {
			_, err := NewFixedWindowLimiter(FixedWindowOptions{Limit: 1, Window: time.Second, Clock: nilClock})
			return err
		},
		func() error {
			_, err := NewSlidingWindowLimiter(SlidingWindowOptions{Limit: -1, Window: time.Second})
			return err
		},
		func() error {
			_, err := NewSlidingWindowLimiter(SlidingWindowOptions{Limit: 1, Window: time.Second, Clock: nilClock})
			return err
		},
		func() error { _, err := NewTokenBucketLimiter(TokenBucketOptions{Rate: -1, Burst: 1}); return err },
		func() error {
			_, err := NewTokenBucketLimiter(TokenBucketOptions{Rate: math.SmallestNonzeroFloat64, Burst: 1})
			return err
		},
		func() error {
			_, err := NewTokenBucketLimiter(TokenBucketOptions{Rate: 1, Burst: 1, Clock: nilClock})
			return err
		},
		func() error { _, err := NewLeakyBucketLimiter(LeakyBucketOptions{Rate: 1, Capacity: 0}); return err },
		func() error {
			_, err := NewLeakyBucketLimiter(LeakyBucketOptions{Rate: 1, Capacity: 1, Clock: nilClock})
			return err
		},
		func() error {
			_, err := NewHeaderRateLimitObserver(HeaderRateLimitOptions{RemainingHeader: "Bad Header"})
			return err
		},
		func() error {
			_, err := NewHeaderRateLimitObserver(HeaderRateLimitOptions{ResetHeader: "Bad Header"})
			return err
		},
		func() error {
			_, err := NewHeaderRateLimitObserver(HeaderRateLimitOptions{Reset: RateLimitResetMode(99)})
			return err
		},
		func() error {
			_, err := NewRateLimitMiddleware(RateLimitOptions{Name: "limit", Limiter: nilLimiter})
			return err
		},
		func() error {
			_, err := NewRateLimitMiddleware(RateLimitOptions{Name: "limit", Limiter: &rateLimitTestLimiter{}, Observer: nilObserver})
			return err
		},
		func() error {
			_, err := NewRateLimitMiddleware(RateLimitOptions{Name: "limit", Limiter: &rateLimitTestLimiter{}, MaximumWait: -1})
			return err
		},
	} {
		if err := construct(); !errors.Is(err, ErrInvalidRateLimitPolicy) {
			t.Fatalf("invalid configuration error = %v", err)
		}
	}
	if _, err := NewRateLimitMiddleware(RateLimitOptions{
		Name: "Invalid", Limiter: &rateLimitTestLimiter{},
	}); !errors.Is(err, ErrInvalidMiddleware) {
		t.Fatalf("invalid middleware metadata error = %v", err)
	}
}

func TestRateLimitAdaptersAndErrors(t *testing.T) {
	t.Parallel()

	called := false
	observer := RateLimitObserverFunc(func(*http.Response, time.Time) (time.Duration, bool, error) {
		called = true

		return time.Second, true, nil
	})
	delay, ok, err := observer.Delay(nil, time.Time{})
	if err != nil || !ok || delay != time.Second || !called {
		t.Fatalf("observer adapter = %v, %v, %v", delay, ok, err)
	}
	secret := errors.New("limiter secret do-not-render")
	limitError := &RateLimitError{Wait: time.Second, Cause: secret}
	if !errors.Is(limitError, secret) || strings.Contains(limitError.Error(), "do-not-render") {
		t.Fatalf("rate limit error = %q", limitError)
	}
	defaultClockLimiter, err := NewFixedWindowLimiter(FixedWindowOptions{Limit: 1, Window: time.Second})
	if err != nil || defaultClockLimiter.Now().IsZero() {
		t.Fatalf("default limiter clock = %v, %v", defaultClockLimiter, err)
	}
}

func TestRateLimitMiddlewareContainsHostilePorts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		limiter  *rateLimitTestLimiter
		observer RateLimitObserver
		want     error
	}{
		{name: "limiter failure", limiter: &rateLimitTestLimiter{acquireErr: errors.New("limiter failure")}},
		{name: "negative limiter wait", limiter: &rateLimitTestLimiter{wait: -time.Second}, want: ErrInvalidRateLimitPolicy},
		{name: "unbounded limiter wait", limiter: &rateLimitTestLimiter{wait: time.Minute}, want: ErrInvalidRateLimitPolicy},
		{
			name: "observer failure", limiter: &rateLimitTestLimiter{},
			observer: RateLimitObserverFunc(func(*http.Response, time.Time) (time.Duration, bool, error) {
				return 0, false, errors.New("observer failure")
			}),
		},
		{
			name: "negative observation", limiter: &rateLimitTestLimiter{}, want: ErrInvalidRateLimitPolicy,
			observer: RateLimitObserverFunc(func(*http.Response, time.Time) (time.Duration, bool, error) {
				return -time.Second, true, nil
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			middleware, err := NewRateLimitMiddleware(RateLimitOptions{
				Name: "limit", Limiter: test.limiter, Observer: test.observer,
				MaximumWait: time.Second,
			})
			if err != nil {
				t.Fatalf("construct middleware: %v", err)
			}
			client, err := New(Config{Middleware: middleware, Transport: TransportFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusNoContent}, nil
			})})
			if err != nil {
				t.Fatalf("construct client: %v", err)
			}
			defer closeTestClient(t, client)
			request, _ := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
			_, err = client.Do(request)
			var rateError *RateLimitError
			if !errors.As(err, &rateError) {
				t.Fatalf("hostile port error = %#v", err)
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("hostile port cause = %v", err)
			}
		})
	}

	limiter := &rateLimitTestLimiter{}
	middleware, err := NewRateLimitMiddleware(RateLimitOptions{
		Name: "limit", Limiter: limiter, MaximumServerDelay: time.Second,
		Observer: RateLimitObserverFunc(func(*http.Response, time.Time) (time.Duration, bool, error) {
			return time.Minute, true, nil
		}),
	})
	if err != nil {
		t.Fatalf("construct clamping middleware: %v", err)
	}
	client, err := New(Config{Middleware: middleware, Transport: TransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNoContent}, nil
	})})
	if err != nil {
		t.Fatalf("construct clamping client: %v", err)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("clamping request: %v", err)
	}
	_ = response.Body.Close()
	_ = client.Close()
	if limiter.deferred.Sub(limiter.Now()) != time.Second {
		t.Fatalf("clamped server delay = %v", limiter.deferred.Sub(limiter.Now()))
	}
}

func TestRateLimiterCancellationBoundsAndDeferral(t *testing.T) {
	t.Parallel()

	clock := &rateLimitTestClock{now: time.Unix(1_700_000_000, 0)}
	fixed, _ := NewFixedWindowLimiter(FixedWindowOptions{Limit: 1, Window: time.Second, Clock: clock})
	sliding, _ := NewSlidingWindowLimiter(SlidingWindowOptions{Limit: 1, Window: time.Second, Clock: clock})
	token, _ := NewTokenBucketLimiter(TokenBucketOptions{Rate: 1, Burst: 1, Clock: clock})
	leaky, _ := NewLeakyBucketLimiter(LeakyBucketOptions{Rate: 1, Capacity: 10, Clock: clock})
	for _, limiter := range []RateLimiter{fixed, sliding, token, leaky} {
		if limiter.Now() != clock.now {
			t.Fatalf("limiter time = %v", limiter.Now())
		}
		limiter.DeferUntil(clock.now.Add(time.Second))
		limiter.DeferUntil(clock.now.Add(time.Millisecond))
		wait, err := limiter.Acquire(context.Background(), time.Millisecond)
		if !errors.Is(err, ErrRateLimitWaitExceeded) || wait != time.Second {
			t.Fatalf("bounded acquire = %v, %v", wait, err)
		}
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fixed.Acquire(canceled, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled fixed acquire = %v", err)
	}
	var nilContext context.Context
	if _, err := waitForRateLimit(nilContext, clock, 0, time.Second); !errors.Is(err, ErrInvalidRateLimitPolicy) {
		t.Fatalf("nil wait context = %v", err)
	}
	if _, err := waitForRateLimit(context.Background(), clock, 0, -1); !errors.Is(err, ErrInvalidRateLimitPolicy) {
		t.Fatalf("negative maximum = %v", err)
	}
	if wait, err := waitForRateLimit(context.Background(), clock, -time.Second, time.Second); err != nil || wait != 0 {
		t.Fatalf("negative wait = %v, %v", wait, err)
	}
	failingClock := &rateLimitFailingClock{now: clock.now, err: errors.New("wait failure")}
	if _, err := waitForRateLimit(context.Background(), failingClock, time.Second, time.Second); !errors.Is(err, failingClock.err) {
		t.Fatalf("clock wait failure = %v", err)
	}
}

func TestLeakyBucketCapacityIsBounded(t *testing.T) {
	t.Parallel()

	clock := &rateLimitTestClock{now: time.Unix(1_700_000_000, 0)}
	limiter, err := NewLeakyBucketLimiter(LeakyBucketOptions{Rate: 1, Capacity: 2, Clock: clock})
	if err != nil {
		t.Fatalf("construct limiter: %v", err)
	}
	for range 2 {
		if _, err := limiter.Acquire(context.Background(), time.Minute); err != nil {
			t.Fatalf("fill leaky bucket: %v", err)
		}
	}
	if _, err := limiter.Acquire(context.Background(), time.Minute); !errors.Is(err, ErrRateLimitCapacity) {
		t.Fatalf("capacity error = %v", err)
	}
}

func TestRateLimitHeaderObservationMatrix(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	defaultObserver, err := NewHeaderRateLimitObserver(HeaderRateLimitOptions{})
	if err != nil {
		t.Fatalf("construct default observer: %v", err)
	}
	if _, ok, _ := defaultObserver.Delay(nil, now); ok {
		t.Fatal("nil response produced delay")
	}
	response := &http.Response{Header: make(http.Header)}
	if _, ok, _ := defaultObserver.Delay(response, now); ok {
		t.Fatal("absent headers produced delay")
	}
	response.Header.Set("Retry-After", "2")
	if delay, ok, _ := defaultObserver.Delay(response, now); !ok || delay != 2*time.Second {
		t.Fatalf("Retry-After precedence = %v, %v", delay, ok)
	}
	response.Header.Del("Retry-After")
	response.Header.Set("RateLimit-Remaining", "invalid")
	response.Header.Set("RateLimit-Reset", "2")
	if _, ok, _ := defaultObserver.Delay(response, now); ok {
		t.Fatal("invalid remaining produced delay")
	}
	response.Header.Set("RateLimit-Remaining", "1")
	if _, ok, _ := defaultObserver.Delay(response, now); ok {
		t.Fatal("positive remaining produced delay")
	}
	response.Header.Set("RateLimit-Remaining", "0")
	if delay, ok, _ := defaultObserver.Delay(response, now); !ok || delay != 2*time.Second {
		t.Fatalf("delta reset = %v, %v", delay, ok)
	}
	response.Header.Set("RateLimit-Reset", "invalid")
	if _, ok, _ := defaultObserver.Delay(response, now); ok {
		t.Fatal("invalid delta reset produced delay")
	}

	for _, test := range []struct {
		mode  RateLimitResetMode
		value string
		want  time.Duration
		ok    bool
	}{
		{mode: RateLimitResetUnixSeconds, value: strconv.FormatInt(now.Add(3*time.Second).Unix(), 10), want: 3 * time.Second, ok: true},
		{mode: RateLimitResetUnixSeconds, value: strconv.FormatInt(now.Add(-time.Second).Unix(), 10), ok: true},
		{mode: RateLimitResetUnixSeconds, value: "invalid"},
		{mode: RateLimitResetHTTPDate, value: now.Add(4 * time.Second).Format(http.TimeFormat), want: 4 * time.Second, ok: true},
		{mode: RateLimitResetHTTPDate, value: now.Add(-time.Second).Format(http.TimeFormat), ok: true},
		{mode: RateLimitResetHTTPDate, value: "invalid"},
	} {
		observer := headerRateLimitObserver{mode: test.mode}
		delay, ok, parseErr := observer.parseReset(test.value, now)
		if parseErr != nil || delay != test.want || ok != test.ok {
			t.Fatalf("reset %d %q = %v, %v, %v", test.mode, test.value, delay, ok, parseErr)
		}
	}
	if _, _, err := (headerRateLimitObserver{mode: RateLimitResetMode(99)}).parseReset("0", now); !errors.Is(err, ErrInvalidRateLimitPolicy) {
		t.Fatalf("unknown reset mode = %v", err)
	}
	if _, ok, _ := (retryAfterRateLimitObserver{}).Delay(nil, now); ok {
		t.Fatal("nil Retry-After response produced delay")
	}
}

func TestRateLimitRunsOncePerRetryAttemptBeforeAuthentication(t *testing.T) {
	t.Parallel()

	limiter := &rateLimitCountingLimiter{now: time.Unix(1_700_000_000, 0)}
	rateLimit, err := NewRateLimitMiddleware(RateLimitOptions{
		Name: "vendor-limit", Limiter: limiter,
	})
	if err != nil {
		t.Fatalf("construct rate limit middleware: %v", err)
	}
	authentication, err := NewAuthenticationMiddleware(AuthenticationOptions{
		Name: "vendor-auth", Layer: MiddlewareClient,
	}, RequestEditorFunc(func(*http.Request) error {
		if limiter.acquires == 0 {
			t.Fatal("authentication ran before admission")
		}

		return nil
	}))
	if err != nil {
		t.Fatalf("construct authentication middleware: %v", err)
	}
	retry, err := NewRetryMiddleware(RetryOptions{
		Name: "vendor-retry", MaximumAttempts: 2,
		Clock:  &rateLimitTestClock{now: limiter.now},
		Jitter: RetryJitterFunc(func(time.Duration) time.Duration { return 0 }),
	})
	if err != nil {
		t.Fatalf("construct retry middleware: %v", err)
	}
	attempts := 0
	middleware := append([]Middleware{retry}, rateLimit...)
	middleware = append(middleware, authentication...)
	client, err := New(Config{
		Middleware: middleware,
		Transport: TransportFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			status := http.StatusNoContent
			if attempts == 1 {
				status = http.StatusServiceUnavailable
			}

			return &http.Response{StatusCode: status}, nil
		}),
	})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer closeTestClient(t, client)
	request, _ := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}
	_ = response.Body.Close()
	if attempts != 2 || limiter.acquires != 2 {
		t.Fatalf("attempts = %d, admissions = %d", attempts, limiter.acquires)
	}
	inspection := client.InspectPipeline()
	var requestNames []string
	for _, information := range inspection.Attempt {
		if information.Stage == StageRequest {
			requestNames = append(requestNames, information.Name)
		}
	}
	if len(requestNames) != 2 || requestNames[0] != "vendor-limit" || requestNames[1] != "vendor-auth" {
		t.Fatalf("attempt request order = %v", requestNames)
	}
}

type rateLimitTestClock struct {
	mu      sync.Mutex
	now     time.Time
	delays  []time.Duration
	advance bool
}

func (clock *rateLimitTestClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()

	return clock.now
}

func (clock *rateLimitTestClock) Wait(ctx context.Context, delay time.Duration) error {
	clock.mu.Lock()
	clock.delays = append(clock.delays, delay)
	if clock.advance {
		clock.now = clock.now.Add(delay)
	}
	clock.mu.Unlock()

	return ctx.Err()
}

func (clock *rateLimitTestClock) Delays() []time.Duration {
	clock.mu.Lock()
	defer clock.mu.Unlock()

	return append([]time.Duration(nil), clock.delays...)
}

type rateLimitTestLimiter struct {
	wait       time.Duration
	acquireErr error
	now        time.Time
	deferred   time.Time
}

func (limiter *rateLimitTestLimiter) Acquire(context.Context, time.Duration) (time.Duration, error) {
	return limiter.wait, limiter.acquireErr
}
func (limiter *rateLimitTestLimiter) DeferUntil(deadline time.Time) { limiter.deferred = deadline }
func (limiter *rateLimitTestLimiter) Now() time.Time {
	if limiter.now.IsZero() {
		return time.Unix(1_700_000_000, 0)
	}

	return limiter.now
}

type rateLimitFailingClock struct {
	now time.Time
	err error
}

type rateLimitCountingLimiter struct {
	now      time.Time
	acquires int
}

func (limiter *rateLimitCountingLimiter) Acquire(context.Context, time.Duration) (time.Duration, error) {
	limiter.acquires++

	return 0, nil
}
func (*rateLimitCountingLimiter) DeferUntil(time.Time)   {}
func (limiter *rateLimitCountingLimiter) Now() time.Time { return limiter.now }

func (clock *rateLimitFailingClock) Now() time.Time { return clock.now }
func (clock *rateLimitFailingClock) Wait(context.Context, time.Duration) error {
	return clock.err
}

type rateLimitTestObserver struct{}

func (*rateLimitTestObserver) Delay(*http.Response, time.Time) (time.Duration, bool, error) {
	return 0, false, nil
}
