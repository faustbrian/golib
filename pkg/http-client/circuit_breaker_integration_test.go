package httpclient

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
	"github.com/faustbrian/golib/pkg/circuit-breaker/breakertest"
)

func TestGoCircuitBreakerRecordsOneLogicalCompletionAcrossRetries(t *testing.T) {
	t.Parallel()

	classifier, err := NewGoCircuitBreakerClassifier(nil)
	if err != nil {
		t.Fatalf("construct classifier: %v", err)
	}
	circuit, err := breaker.New(breaker.Config{
		Name:              "vendor",
		MinimumThroughput: 10,
		Classifier:        classifier,
	})
	if err != nil {
		t.Fatalf("construct breaker: %v", err)
	}
	t.Cleanup(func() { _ = circuit.Shutdown(context.Background()) })
	adapter, err := NewGoCircuitBreakerAdapter(circuit)
	if err != nil {
		t.Fatalf("construct adapter: %v", err)
	}
	circuitMiddleware, err := NewCircuitBreakerMiddleware(CircuitBreakerOptions{
		Name: "circuit", Breaker: adapter,
	})
	if err != nil {
		t.Fatalf("construct circuit middleware: %v", err)
	}
	retryMiddleware, err := NewRetryMiddleware(RetryOptions{
		Name: "retry", MaximumAttempts: 2,
		Clock:  &rateLimitTestClock{now: time.Unix(1_700_000_000, 0)},
		Jitter: RetryJitterFunc(func(time.Duration) time.Duration { return 0 }),
	})
	if err != nil {
		t.Fatalf("construct retry middleware: %v", err)
	}
	firstClosed := false
	attempts := 0
	client, err := New(Config{
		Middleware: []Middleware{circuitMiddleware, retryMiddleware},
		Transport: TransportFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				return &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Body: &retryCloseBody{
						Reader: strings.NewReader("retry"), closed: &firstClosed,
					},
				}, nil
			}

			return &http.Response{StatusCode: http.StatusNoContent}, nil
		}),
	})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	t.Cleanup(func() { closeTestClient(t, client) })
	request, _ := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close final response: %v", err)
	}
	if attempts != 2 || !firstClosed {
		t.Fatalf("attempts = %d, first response closed = %t", attempts, firstClosed)
	}
	snapshot := circuit.Snapshot()
	if snapshot.Admitted != 1 || snapshot.Completed != 1 ||
		snapshot.TotalSuccesses != 1 || snapshot.Successes != 1 ||
		snapshot.TotalFailures != 0 {
		t.Fatalf("logical-operation snapshot = %+v", snapshot)
	}
}

func TestGoCircuitBreakerHalfOpenProbeOwnsBoundedRetries(t *testing.T) {
	t.Parallel()

	clock := breakertest.NewClock(time.Unix(1_700_000_000, 0))
	circuit := newGoCircuitBreakerIntegration(t, breaker.Config{
		Name:              "half-open-retry",
		MinimumThroughput: 1,
		Opening: &breaker.OpeningRules{
			FailureCount: 1,
		},
		OpenDuration: breaker.FixedOpenDuration(time.Minute),
		HalfOpen: &breaker.HalfOpenPolicy{
			MaxProbes:         1,
			RequiredSuccesses: 1,
		},
		Clock: clock,
	})
	client := newGoCircuitBreakerRetryClient(t, circuit, func(attempt int) int {
		if attempt == 4 {
			return http.StatusNoContent
		}

		return http.StatusServiceUnavailable
	})

	request, _ := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
	response, err := client.Do(request)
	if !errors.Is(err, ErrRetryExhausted) || response != nil {
		t.Fatalf("opening response = %#v, error = %v", response, err)
	}
	if snapshot := circuit.Snapshot(); snapshot.State != breaker.StateOpen ||
		snapshot.Admitted != 1 || snapshot.Completed != 1 ||
		snapshot.TotalFailures != 1 {
		t.Fatalf("open snapshot = %+v", snapshot)
	}

	clock.Advance(time.Minute)
	response, err = client.Do(request)
	if err != nil {
		t.Fatalf("execute half-open request: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close half-open response: %v", err)
	}
	if snapshot := circuit.Snapshot(); snapshot.State != breaker.StateClosed ||
		snapshot.Admitted != 2 || snapshot.Completed != 2 ||
		snapshot.TotalFailures != 1 || snapshot.TotalSuccesses != 1 {
		t.Fatalf("recovered snapshot = %+v", snapshot)
	}
}

func TestGoCircuitBreakerClassifiesHTTPExecutionBoundaries(t *testing.T) {
	t.Parallel()

	t.Run("pre-admission cancellation bypasses completion", func(t *testing.T) {
		t.Parallel()

		circuit := newGoCircuitBreakerIntegration(t, breaker.Config{
			Name: "pre-admission-cancellation",
		})
		adapter, err := NewGoCircuitBreakerAdapter(circuit)
		if err != nil {
			t.Fatalf("construct adapter: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = adapter.Execute(ctx, func(context.Context) (*http.Response, error) {
			t.Fatal("operation must not run")

			return nil, nil
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("execution error = %v, want context cancellation", err)
		}
		if snapshot := circuit.Snapshot(); snapshot.Admitted != 0 || snapshot.Completed != 0 {
			t.Fatalf("pre-admission cancellation snapshot = %+v", snapshot)
		}
	})

	t.Run("local limiter rejection bypasses admission", func(t *testing.T) {
		t.Parallel()

		circuit := newGoCircuitBreakerIntegration(t, breaker.Config{
			Name: "local-rate-rejection",
		})
		adapter, err := NewGoCircuitBreakerAdapter(circuit)
		if err != nil {
			t.Fatalf("construct adapter: %v", err)
		}
		circuitMiddleware, err := NewCircuitBreakerMiddleware(CircuitBreakerOptions{
			Name: "circuit", Breaker: adapter,
		})
		if err != nil {
			t.Fatalf("construct circuit middleware: %v", err)
		}
		limiter := &rateLimitTestLimiter{acquireErr: ErrRateLimitCapacity}
		rateLimitMiddleware, err := NewRateLimitMiddleware(RateLimitOptions{
			Name: "limit", Limiter: limiter,
		})
		if err != nil {
			t.Fatalf("construct rate limit middleware: %v", err)
		}
		client, err := New(Config{
			Middleware: append(rateLimitMiddleware, circuitMiddleware),
			Transport: TransportFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("transport must not run")

				return nil, nil
			}),
		})
		if err != nil {
			t.Fatalf("construct client: %v", err)
		}
		t.Cleanup(func() { closeTestClient(t, client) })
		request, _ := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
		_, err = client.Do(request)
		if !errors.Is(err, ErrRateLimitCapacity) {
			t.Fatalf("request error = %v, want ErrRateLimitCapacity", err)
		}
		if snapshot := circuit.Snapshot(); snapshot.Admitted != 0 || snapshot.Completed != 0 {
			t.Fatalf("local rejection snapshot = %+v", snapshot)
		}
	})

	tests := []struct {
		name         string
		operation    func(context.Context, context.CancelFunc) error
		failure      error
		wantFailures uint64
		wantIgnored  uint64
	}{
		{
			name: "caller cancellation is ignored",
			operation: func(ctx context.Context, cancel context.CancelFunc) error {
				cancel()
				return ctx.Err()
			},
			failure:     context.Canceled,
			wantIgnored: 1,
		},
		{
			name: "dependency cancellation is a failure",
			operation: func(context.Context, context.CancelFunc) error {
				return context.Canceled
			},
			failure:      context.Canceled,
			wantFailures: 1,
		},
		{
			name: "dependency deadline is a failure",
			operation: func(context.Context, context.CancelFunc) error {
				return context.DeadlineExceeded
			},
			failure:      context.DeadlineExceeded,
			wantFailures: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			circuit := newGoCircuitBreakerIntegration(t, breaker.Config{Name: test.name})
			adapter, err := NewGoCircuitBreakerAdapter(circuit)
			if err != nil {
				t.Fatalf("construct adapter: %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, err = adapter.Execute(ctx, func(ctx context.Context) (*http.Response, error) {
				return nil, test.operation(ctx, cancel)
			})
			if !errors.Is(err, test.failure) {
				t.Fatalf("execution error = %v, want %v", err, test.failure)
			}
			snapshot := circuit.Snapshot()
			if snapshot.Admitted != 1 || snapshot.Completed != 1 ||
				snapshot.TotalFailures != test.wantFailures ||
				snapshot.TotalIgnored != test.wantIgnored {
				t.Fatalf("boundary snapshot = %+v", snapshot)
			}
		})
	}
}

func TestGoCircuitBreakerConcretePipelineOrderAndShortCircuits(t *testing.T) {
	t.Parallel()

	var events []string
	errInvalid := errors.New("local validation failed")
	validation, err := NewRequestMiddleware(MiddlewareOptions{
		Name: "validation", Scope: ScopeOperation, Priority: -1100,
	}, func(request *http.Request, next Next) (*http.Response, error) {
		if request.Header.Get("X-Invalid") == "true" {
			events = append(events, "validation")

			return nil, errInvalid
		}

		return next(request)
	})
	if err != nil {
		t.Fatalf("construct validation middleware: %v", err)
	}
	cache, err := NewRequestMiddleware(MiddlewareOptions{
		Name: "cache", Scope: ScopeOperation, Priority: -1000,
	}, func(request *http.Request, next Next) (*http.Response, error) {
		events = append(events, "cache")
		if request.Header.Get("X-Cache") == "hit" {
			return &http.Response{StatusCode: http.StatusOK}, nil
		}

		return next(request)
	})
	if err != nil {
		t.Fatalf("construct cache middleware: %v", err)
	}
	limiter := &circuitIntegrationLimiter{
		now: time.Unix(1_700_000_000, 0), events: &events,
	}
	rateLimit, err := NewRateLimitMiddleware(RateLimitOptions{
		Name: "limiter", Limiter: limiter,
	})
	if err != nil {
		t.Fatalf("construct rate limit middleware: %v", err)
	}
	circuit := newGoCircuitBreakerIntegration(t, breaker.Config{Name: "pipeline"})
	adapter, err := NewGoCircuitBreakerAdapter(circuit)
	if err != nil {
		t.Fatalf("construct adapter: %v", err)
	}
	circuitMiddleware, err := NewCircuitBreakerMiddleware(CircuitBreakerOptions{
		Name: "breaker",
		Breaker: CircuitBreakerFunc(func(
			ctx context.Context,
			operation func(context.Context) (*http.Response, error),
		) (*http.Response, error) {
			events = append(events, "breaker")

			return adapter.Execute(ctx, operation)
		}),
	})
	if err != nil {
		t.Fatalf("construct circuit middleware: %v", err)
	}
	retry, err := NewRetryMiddleware(RetryOptions{
		Name: "retry", MaximumAttempts: 2,
		Clock:  &rateLimitTestClock{now: time.Unix(1_700_000_000, 0)},
		Jitter: RetryJitterFunc(func(time.Duration) time.Duration { return 0 }),
	})
	if err != nil {
		t.Fatalf("construct retry middleware: %v", err)
	}
	authentication, err := NewAuthenticationMiddleware(AuthenticationOptions{
		Name: "authentication",
	}, RequestEditorFunc(func(request *http.Request) error {
		events = append(events, "authentication")
		request.Header.Set("Authorization", "Bearer opaque")

		return nil
	}))
	if err != nil {
		t.Fatalf("construct authentication middleware: %v", err)
	}
	hmacEditor, err := NewHMACAuth(HMACOptions{
		Secret:  []byte("test-only-secret"),
		NewHash: sha256.New,
		Canonicalize: func(*http.Request) ([]byte, error) {
			return []byte("canonical"), nil
		},
		ApplySignature: func(request *http.Request, signature []byte) error {
			events = append(events, "signing")
			request.Header.Set("X-Signature", string(signature))

			return nil
		},
	})
	if err != nil {
		t.Fatalf("construct HMAC editor: %v", err)
	}
	signing, err := NewRequestEditorMiddleware(MiddlewareOptions{
		Name: "signing", Scope: ScopeAttempt, Layer: MiddlewareClient, Priority: 100,
	}, hmacEditor)
	if err != nil {
		t.Fatalf("construct signing middleware: %v", err)
	}
	telemetry, err := NewTransportMiddleware(MiddlewareOptions{
		Name: "telemetry", Scope: ScopeAttempt, Priority: 200,
	}, func(request *http.Request, next Next) (*http.Response, error) {
		events = append(events, "telemetry")

		return next(request)
	})
	if err != nil {
		t.Fatalf("construct telemetry middleware: %v", err)
	}
	middleware := []Middleware{validation, cache}
	middleware = append(middleware, rateLimit...)
	middleware = append(middleware, circuitMiddleware, retry)
	middleware = append(middleware, authentication...)
	middleware = append(middleware, signing, telemetry)
	attempts := 0
	client, err := New(Config{
		Middleware: middleware,
		Transport: TransportFunc(func(*http.Request) (*http.Response, error) {
			events = append(events, "transport")
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
	t.Cleanup(func() { closeTestClient(t, client) })

	request, _ := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("execute miss: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close miss response: %v", err)
	}
	want := []string{
		"cache", "limiter", "breaker",
		"authentication", "signing", "telemetry", "transport",
		"limiter", "authentication", "signing", "telemetry", "transport",
	}
	if !slices.Equal(events, want) {
		t.Fatalf("pipeline events = %v, want %v", events, want)
	}
	if snapshot := circuit.Snapshot(); snapshot.Admitted != 1 || snapshot.Completed != 1 {
		t.Fatalf("miss snapshot = %+v", snapshot)
	}

	events = events[:0]
	request, _ = http.NewRequest(http.MethodGet, "https://api.example.test", nil)
	request.Header.Set("X-Cache", "hit")
	response, err = client.Do(request)
	if err != nil {
		t.Fatalf("execute cache hit: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close cache response: %v", err)
	}
	if !slices.Equal(events, []string{"cache"}) {
		t.Fatalf("cache-hit events = %v", events)
	}

	events = events[:0]
	request, _ = http.NewRequest(http.MethodGet, "https://api.example.test", nil)
	request.Header.Set("X-Invalid", "true")
	_, err = client.Do(request)
	if !errors.Is(err, errInvalid) || !slices.Equal(events, []string{"validation"}) {
		t.Fatalf("validation error = %v, events = %v", err, events)
	}
	if snapshot := circuit.Snapshot(); snapshot.Admitted != 1 || snapshot.Completed != 1 {
		t.Fatalf("short-circuit snapshot = %+v", snapshot)
	}
}

func newGoCircuitBreakerIntegration(t *testing.T, config breaker.Config) *breaker.Breaker {
	t.Helper()

	classifier, err := NewGoCircuitBreakerClassifier(nil)
	if err != nil {
		t.Fatalf("construct classifier: %v", err)
	}
	config.Classifier = classifier
	circuit, err := breaker.New(config)
	if err != nil {
		t.Fatalf("construct breaker: %v", err)
	}
	t.Cleanup(func() { _ = circuit.Shutdown(context.Background()) })

	return circuit
}

func newGoCircuitBreakerRetryClient(
	t *testing.T,
	circuit *breaker.Breaker,
	status func(int) int,
) *Client {
	t.Helper()

	adapter, err := NewGoCircuitBreakerAdapter(circuit)
	if err != nil {
		t.Fatalf("construct adapter: %v", err)
	}
	circuitMiddleware, err := NewCircuitBreakerMiddleware(CircuitBreakerOptions{
		Name: "circuit", Breaker: adapter,
	})
	if err != nil {
		t.Fatalf("construct circuit middleware: %v", err)
	}
	retryMiddleware, err := NewRetryMiddleware(RetryOptions{
		Name: "retry", MaximumAttempts: 2,
		Clock:  &rateLimitTestClock{now: time.Unix(1_700_000_000, 0)},
		Jitter: RetryJitterFunc(func(time.Duration) time.Duration { return 0 }),
	})
	if err != nil {
		t.Fatalf("construct retry middleware: %v", err)
	}
	attempts := 0
	client, err := New(Config{
		Middleware: []Middleware{circuitMiddleware, retryMiddleware},
		Transport: TransportFunc(func(*http.Request) (*http.Response, error) {
			attempts++

			return &http.Response{StatusCode: status(attempts)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	t.Cleanup(func() { closeTestClient(t, client) })

	return client
}

type circuitIntegrationLimiter struct {
	now    time.Time
	events *[]string
}

func (limiter *circuitIntegrationLimiter) Acquire(context.Context, time.Duration) (time.Duration, error) {
	*limiter.events = append(*limiter.events, "limiter")

	return 0, nil
}

func (*circuitIntegrationLimiter) DeferUntil(time.Time) {}

func (limiter *circuitIntegrationLimiter) Now() time.Time { return limiter.now }
