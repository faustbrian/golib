package httpclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRetryReplaysSafeRequestsWithDeterministicBackoff(t *testing.T) {
	t.Parallel()

	clock := &retryTestClock{now: time.Unix(1_700_000_000, 0)}
	retry, err := NewRetryMiddleware(RetryOptions{
		Name: "vendor-retry", Layer: MiddlewareEndpoint, MaximumAttempts: 3,
		BaseDelay: time.Second, MaximumDelay: 10 * time.Second, Clock: clock,
		Jitter: RetryJitterFunc(func(delay time.Duration) time.Duration { return delay }),
	})
	if err != nil {
		t.Fatalf("construct retry middleware: %v", err)
	}
	attempts := 0
	client, err := New(Config{
		Middleware: []Middleware{retry},
		Transport: TransportFunc(func(request *http.Request) (*http.Response, error) {
			attempts++
			if attempts < 3 {
				return nil, errors.New("temporary network failure")
			}

			return &http.Response{StatusCode: http.StatusNoContent}, nil
		}),
	})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer closeTestClient(t, client)

	request, err := http.NewRequest(http.MethodGet, "https://api.example.test/items", nil)
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if got := clock.Delays(); len(got) != 2 || got[0] != time.Second || got[1] != 2*time.Second {
		t.Fatalf("backoff delays = %v", got)
	}
}

func TestRetryRequiresReplayableBodyAndExplicitUnsafeOptIn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		method     string
		body       io.Reader
		idempotent bool
		contextKey bool
		unsafe     bool
		want       int
	}{
		{name: "safe get", method: http.MethodGet, want: 2},
		{name: "post has no endpoint contract", method: http.MethodPost, body: strings.NewReader("body"), idempotent: true, want: 1},
		{name: "post contract and key", method: http.MethodPost, body: strings.NewReader("body"), idempotent: true, unsafe: true, want: 2},
		{name: "post contract without key", method: http.MethodPost, body: strings.NewReader("body"), unsafe: true, want: 1},
		{name: "context key without endpoint policy", method: http.MethodPost, body: strings.NewReader("body"), contextKey: true, unsafe: true, want: 1},
		{name: "non replayable body", method: http.MethodPut, body: &retryOneShotReader{Reader: strings.NewReader("body")}, want: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			clock := &retryTestClock{now: time.Unix(1_700_000_000, 0)}
			retry, err := NewRetryMiddleware(RetryOptions{
				Name: "retry", Layer: MiddlewareEndpoint, MaximumAttempts: 2,
				BaseDelay: time.Millisecond, MaximumDelay: time.Millisecond,
				Clock: clock, RetryUnsafeWithIdempotency: test.unsafe,
			})
			if err != nil {
				t.Fatalf("construct retry middleware: %v", err)
			}
			middleware := []Middleware{retry}
			if test.idempotent {
				idempotency, keyErr := NewIdempotencyMiddleware(IdempotencyOptions{
					Name: "idempotency", Layer: MiddlewareEndpoint,
					Generator: IdentifierGeneratorFunc(func(context.Context) (GeneratedIdentifier, error) {
						return GeneratedIdentifier{Value: "stable-key", EntropyBits: 128}, nil
					}),
				})
				if keyErr != nil {
					t.Fatalf("construct idempotency middleware: %v", keyErr)
				}
				middleware = append(middleware, idempotency...)
			}
			attempts := 0
			client, err := New(Config{
				Middleware: middleware,
				Transport: TransportFunc(func(*http.Request) (*http.Response, error) {
					attempts++

					return nil, errors.New("temporary")
				}),
			})
			if err != nil {
				t.Fatalf("construct client: %v", err)
			}
			defer closeTestClient(t, client)
			request, err := http.NewRequest(test.method, "https://api.example.test", test.body)
			if err != nil {
				t.Fatalf("construct request: %v", err)
			}
			if test.contextKey {
				ctx, keyErr := WithIdempotencyKey(request.Context(), "context-only-key")
				if keyErr != nil {
					t.Fatalf("attach context key: %v", keyErr)
				}
				request = request.WithContext(ctx)
			}
			_, err = client.Do(request)
			var exhausted *RetryExhaustedError
			if !errors.As(err, &exhausted) {
				t.Fatalf("retry error = %#v", err)
			}
			if attempts != test.want {
				t.Fatalf("attempts = %d, want %d", attempts, test.want)
			}
		})
	}
}

func TestRetryUsesBoundedRetryAfterAndClosesDiscardedResponses(t *testing.T) {
	t.Parallel()

	clock := &retryTestClock{now: time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)}
	retry, err := NewRetryMiddleware(RetryOptions{
		Name: "retry", Layer: MiddlewareEndpoint, MaximumAttempts: 3,
		BaseDelay: time.Second, MaximumDelay: 10 * time.Second,
		MaximumRetryAfter: 4 * time.Second, Clock: clock,
	})
	if err != nil {
		t.Fatalf("construct retry middleware: %v", err)
	}
	closed := make([]bool, 2)
	attempts := 0
	client, err := New(Config{
		Middleware: []Middleware{retry},
		Transport: TransportFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			if attempts <= 2 {
				index := attempts - 1
				header := "30"
				if attempts == 2 {
					header = clock.now.Add(3 * time.Second).Format(http.TimeFormat)
				}

				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header:     http.Header{"Retry-After": {header}},
					Body:       &retryCloseBody{Reader: strings.NewReader("retry"), closed: &closed[index]},
				}, nil
			}

			return &http.Response{StatusCode: http.StatusNoContent}, nil
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
	if got := clock.Delays(); len(got) != 2 || got[0] != 4*time.Second || got[1] != 3*time.Second {
		t.Fatalf("Retry-After delays = %v", got)
	}
	if !closed[0] || !closed[1] {
		t.Fatalf("discarded response closure = %v", closed)
	}
}

func TestRetryReplaysBodyAndIdentityThroughRealServer(t *testing.T) {
	t.Parallel()

	var bodies []string
	var keys []string
	var identities []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		content, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		bodies = append(bodies, string(content))
		keys = append(keys, request.Header.Get("Idempotency-Key"))
		identities = append(identities, request.Header.Get("X-Test-Operation"))
		if len(bodies) == 1 {
			writer.WriteHeader(http.StatusServiceUnavailable)

			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	idempotency, err := NewIdempotencyMiddleware(IdempotencyOptions{
		Name: "idempotency", Layer: MiddlewareEndpoint,
		Generator: IdentifierGeneratorFunc(func(context.Context) (GeneratedIdentifier, error) {
			return GeneratedIdentifier{Value: "server-key", EntropyBits: 128}, nil
		}),
	})
	if err != nil {
		t.Fatalf("construct idempotency middleware: %v", err)
	}
	identityHeader := mustRequestMiddleware(t, MiddlewareOptions{
		Name: "identity-header", Scope: ScopeAttempt, Layer: MiddlewareClient, Priority: -1400,
	}, func(request *http.Request, next Next) (*http.Response, error) {
		identity, ok := OperationIdentityFromContext(request.Context())
		if !ok {
			t.Fatal("attempt has no operation identity")
		}
		request.Header.Set("X-Test-Operation", identity.ID)

		return next(request)
	})
	retry, err := NewRetryMiddleware(RetryOptions{
		Name: "retry", Layer: MiddlewareEndpoint, MaximumAttempts: 2,
		BaseDelay: time.Millisecond, MaximumDelay: time.Millisecond,
		Clock:                      &retryTestClock{now: time.Unix(1_700_000_000, 0)},
		Jitter:                     RetryJitterFunc(func(delay time.Duration) time.Duration { return delay }),
		RetryUnsafeWithIdempotency: true,
	})
	if err != nil {
		t.Fatalf("construct retry middleware: %v", err)
	}
	client, err := New(Config{Middleware: append([]Middleware{retry, identityHeader}, idempotency...)})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer closeTestClient(t, client)
	request, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("stable-body"))
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response: %v", err)
	}
	if len(bodies) != 2 || bodies[0] != "stable-body" || bodies[1] != bodies[0] {
		t.Fatalf("request bodies = %v", bodies)
	}
	if keys[0] != "server-key" || keys[1] != keys[0] {
		t.Fatalf("idempotency keys = %v", keys)
	}
	if identities[0] == "" || identities[1] != identities[0] {
		t.Fatalf("operation identities = %v", identities)
	}
}

func TestRetryHonorsCancellationAndReportsSecretSafeExhaustion(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	secret := errors.New("network secret do-not-render")
	clock := &retryTestClock{now: time.Unix(1_700_000_000, 0), wait: func(context.Context, time.Duration) error {
		cancel()

		return context.Canceled
	}}
	retry, err := NewRetryMiddleware(RetryOptions{
		Name: "retry", Layer: MiddlewareEndpoint, MaximumAttempts: 3,
		BaseDelay: time.Second, MaximumDelay: time.Second, Clock: clock,
	})
	if err != nil {
		t.Fatalf("construct retry middleware: %v", err)
	}
	client, err := New(Config{Middleware: []Middleware{retry}, Transport: TransportFunc(func(*http.Request) (*http.Response, error) {
		return nil, secret
	})})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer closeTestClient(t, client)
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.test", nil)
	_, err = client.Do(request)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
	if strings.Contains(err.Error(), "do-not-render") {
		t.Fatalf("retry error rendered cause: %q", err)
	}
}

func TestRetryRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	var typedNilClock *retryTestClock
	var typedNilJitter *retryTestJitter
	var typedNilPolicy *retryTestPolicy
	for _, options := range []RetryOptions{
		{},
		{Name: "retry", MaximumAttempts: 1},
		{Name: "retry", MaximumAttempts: maximumRetryAttempts + 1},
		{Name: "retry", MaximumAttempts: 2, BaseDelay: -1},
		{Name: "retry", MaximumAttempts: 2, BaseDelay: time.Second, MaximumDelay: time.Millisecond},
		{Name: "retry", MaximumAttempts: 2, BaseDelay: time.Second, MaximumDelay: time.Second, MaximumElapsed: -1},
		{Name: "retry", MaximumAttempts: 2, BaseDelay: time.Second, MaximumDelay: time.Second, MaximumRetryAfter: -1},
		{Name: "retry", MaximumAttempts: 2, BaseDelay: time.Second, MaximumDelay: time.Second, Clock: typedNilClock},
		{Name: "retry", MaximumAttempts: 2, BaseDelay: time.Second, MaximumDelay: time.Second, Jitter: typedNilJitter},
		{Name: "retry", MaximumAttempts: 2, BaseDelay: time.Second, MaximumDelay: time.Second, Policy: typedNilPolicy},
	} {
		if _, err := NewRetryMiddleware(options); !errors.Is(err, ErrInvalidRetryPolicy) {
			t.Fatalf("invalid options accepted: %#v, error %v", options, err)
		}
	}
	if _, err := NewRetryMiddleware(RetryOptions{Name: "retry", MaximumAttempts: 2}); err != nil {
		t.Fatalf("default retry options: %v", err)
	}
}

func TestRetryPolicyAndDelayBoundaryHelpers(t *testing.T) {
	t.Parallel()

	called := false
	policy := RetryPolicyFunc(func(attempt RetryAttempt) bool {
		called = attempt.Attempt == 7

		return true
	})
	if !policy.ShouldRetry(RetryAttempt{Attempt: 7}) || !called {
		t.Fatal("RetryPolicyFunc did not delegate")
	}
	defaultPolicy := defaultRetryPolicy{}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	canceledRequest, _ := http.NewRequestWithContext(canceled, http.MethodGet, "https://api.example.test", nil)
	if defaultPolicy.ShouldRetry(RetryAttempt{Request: canceledRequest, BodyReplayable: true, Failure: errors.New("failure")}) {
		t.Fatal("canceled request was retryable")
	}
	request, _ := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
	if defaultPolicy.ShouldRetry(RetryAttempt{Request: request, BodyReplayable: true}) {
		t.Fatal("empty result was retryable")
	}
	if defaultPolicy.ShouldRetry(RetryAttempt{Request: request, BodyReplayable: true, Failure: context.DeadlineExceeded}) {
		t.Fatal("deadline failure was retryable")
	}

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		value string
		want  time.Duration
		ok    bool
	}{
		{value: "", ok: false},
		{value: "malformed", ok: false},
		{value: " 2 ", want: 2 * time.Second, ok: true},
		{value: now.Add(-time.Second).Format(http.TimeFormat), want: 0, ok: true},
	} {
		got, ok := parseRetryAfter(test.value, now)
		if got != test.want || ok != test.ok {
			t.Fatalf("parseRetryAfter(%q) = %v, %v", test.value, got, ok)
		}
	}
	clock := &retryTestClock{now: now}
	options := resolvedRetryOptions{
		baseDelay: 3 * time.Second, maximumDelay: 5 * time.Second,
		maximumRetryAfter: time.Minute, clock: clock,
		jitter: RetryJitterFunc(func(time.Duration) time.Duration { return -1 }),
	}
	if got := retryDelay(nil, 3, options); got != 5*time.Second {
		t.Fatalf("bounded exponential delay = %v", got)
	}
	options.baseDelay = 10 * time.Second
	options.maximumDelay = 5 * time.Second
	if got := retryDelay(nil, 1, options); got != 5*time.Second {
		t.Fatalf("maximum delay clamp = %v", got)
	}
}

func TestRetryExecutionBoundaryFailures(t *testing.T) {
	t.Parallel()

	request, _ := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
	clock := &retryTestClock{now: time.Unix(1_700_000_000, 0)}
	options := resolvedRetryOptions{
		maximumAttempts: 2, maximumElapsed: time.Millisecond,
		baseDelay: time.Second, maximumDelay: time.Second,
		maximumRetryAfter: time.Second, clock: clock,
		jitter: RetryJitterFunc(func(delay time.Duration) time.Duration { return delay }),
		policy: RetryPolicyFunc(func(RetryAttempt) bool { return true }),
	}
	responseBody := &retryCloseBody{Reader: strings.NewReader("retry"), closed: new(bool)}
	_, err := executeRetry(request, func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: responseBody}, nil
	}, options)
	var exhausted *RetryExhaustedError
	if !errors.As(err, &exhausted) || exhausted.StatusCode != http.StatusServiceUnavailable || !*responseBody.closed {
		t.Fatalf("elapsed exhaustion = %#v, closed %v", err, *responseBody.closed)
	}

	closeFailure := errors.New("close failure")
	options.maximumElapsed = time.Minute
	_, err = executeRetry(request, func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: retryFailingBody{closeErr: closeFailure}}, nil
	}, options)
	if !errors.Is(err, closeFailure) {
		t.Fatalf("discard close failure = %v", err)
	}

	nonReplayable, _ := http.NewRequest(http.MethodPut, "https://api.example.test", &retryOneShotReader{Reader: strings.NewReader("body")})
	_, err = executeRetry(nonReplayable, func(*http.Request) (*http.Response, error) {
		return nil, errors.New("temporary")
	}, options)
	if !errors.Is(err, ErrRetryExhausted) {
		t.Fatalf("non-replayable exhaustion = %v", err)
	}

	replayFailure := errors.New("replay failure")
	replayRequestValue, _ := http.NewRequest(http.MethodPut, "https://api.example.test", strings.NewReader("body"))
	replayRequestValue.GetBody = func() (io.ReadCloser, error) { return nil, replayFailure }
	_, err = executeRetry(replayRequestValue, func(*http.Request) (*http.Response, error) {
		return nil, errors.New("temporary")
	}, options)
	if !errors.Is(err, replayFailure) {
		t.Fatalf("replay failure = %v", err)
	}
	replayRequestValue.GetBody = func() (io.ReadCloser, error) { return nil, nil }
	_, err = replayRequest(replayRequestValue)
	if !errors.Is(err, ErrInvalidBody) {
		t.Fatalf("nil replay body = %v", err)
	}
}

func TestRetryContainsPolicyPanicAndClosesResponse(t *testing.T) {
	t.Parallel()

	retry, err := NewRetryMiddleware(RetryOptions{
		Name: "retry", Layer: MiddlewareEndpoint, MaximumAttempts: 2,
		Policy: RetryPolicyFunc(func(RetryAttempt) bool { panic("policy secret") }),
	})
	if err != nil {
		t.Fatalf("construct retry middleware: %v", err)
	}
	closed := false
	client, err := New(Config{
		Middleware: []Middleware{retry},
		Transport: TransportFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Body:       &retryCloseBody{Reader: strings.NewReader("body"), closed: &closed},
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer closeTestClient(t, client)
	request, _ := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
	_, err = client.Do(request)
	var panicError *MiddlewarePanicError
	if !errors.As(err, &panicError) || !closed {
		t.Fatalf("policy panic result = %#v, body closed = %v", err, closed)
	}
	if strings.Contains(err.Error(), "policy secret") {
		t.Fatalf("policy panic rendered value: %q", err)
	}
}

func TestSystemRetryClockAndCryptoJitter(t *testing.T) {
	t.Parallel()

	clock := systemRetryClock{}
	if clock.Now().IsZero() {
		t.Fatal("system clock returned zero time")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := clock.Wait(canceled, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled wait = %v", err)
	}
	if err := clock.Wait(context.Background(), time.Nanosecond); err != nil {
		t.Fatalf("completed wait = %v", err)
	}
	during, cancelDuring := context.WithCancel(context.Background())
	canceledDuring := make(chan struct{})
	go func() {
		time.Sleep(time.Millisecond)
		cancelDuring()
		close(canceledDuring)
	}()
	if err := clock.Wait(during, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled wait = %v", err)
	}
	<-canceledDuring

	jitter := cryptoRetryJitter{reader: bytes.NewReader(make([]byte, 32))}
	if got := jitter.Apply(0); got != 0 {
		t.Fatalf("zero jitter = %v", got)
	}
	if got := jitter.Apply(time.Second); got < 0 || got > time.Second {
		t.Fatalf("crypto jitter = %v", got)
	}
	failing := cryptoRetryJitter{reader: errorReader{err: errors.New("entropy failure")}}
	if got := failing.Apply(time.Second); got != time.Second {
		t.Fatalf("failed crypto jitter = %v", got)
	}
	maximum := time.Duration(int64(^uint64(0) >> 1))
	jitter = cryptoRetryJitter{reader: bytes.NewReader(make([]byte, 32))}
	if got := jitter.Apply(maximum); got < 0 || got > maximum {
		t.Fatalf("maximum crypto jitter = %v", got)
	}
}

type retryTestClock struct {
	mu     sync.Mutex
	now    time.Time
	delays []time.Duration
	wait   func(context.Context, time.Duration) error
}

func (clock *retryTestClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()

	return clock.now
}

func (clock *retryTestClock) Wait(ctx context.Context, delay time.Duration) error {
	clock.mu.Lock()
	clock.delays = append(clock.delays, delay)
	clock.now = clock.now.Add(delay)
	wait := clock.wait
	clock.mu.Unlock()
	if wait != nil {
		return wait(ctx, delay)
	}

	return nil
}

func (clock *retryTestClock) Delays() []time.Duration {
	clock.mu.Lock()
	defer clock.mu.Unlock()

	return append([]time.Duration(nil), clock.delays...)
}

type retryTestJitter struct{}

func (*retryTestJitter) Apply(time.Duration) time.Duration { return 0 }

type retryTestPolicy struct{}

func (*retryTestPolicy) ShouldRetry(RetryAttempt) bool { return false }

type retryOneShotReader struct{ io.Reader }

type retryCloseBody struct {
	io.Reader
	closed *bool
}

func (body *retryCloseBody) Close() error {
	*body.closed = true

	return nil
}

type retryFailingBody struct{ closeErr error }

func (retryFailingBody) Read([]byte) (int, error) { return 0, io.EOF }
func (body retryFailingBody) Close() error        { return body.closeErr }

func closeTestClient(t *testing.T, client *Client) {
	t.Helper()
	if err := client.Close(); err != nil {
		t.Errorf("close client: %v", err)
	}
}
