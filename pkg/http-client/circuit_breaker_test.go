package httpclient

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
)

func TestCircuitBreakerWrapsOneLogicalOperationAcrossRetries(t *testing.T) {
	t.Parallel()

	circuit := &circuitTestBreaker{}
	middleware, err := NewCircuitBreakerMiddleware(CircuitBreakerOptions{
		Name: "vendor-circuit", Layer: MiddlewareClient, Breaker: circuit,
	})
	if err != nil {
		t.Fatalf("construct circuit middleware: %v", err)
	}
	retry, err := NewRetryMiddleware(RetryOptions{
		Name: "vendor-retry", MaximumAttempts: 2,
		Clock:  &rateLimitTestClock{now: time.Unix(1_700_000_000, 0)},
		Jitter: RetryJitterFunc(func(time.Duration) time.Duration { return 0 }),
	})
	if err != nil {
		t.Fatalf("construct retry middleware: %v", err)
	}
	attempts := 0
	client, err := New(Config{
		Middleware: []Middleware{middleware, retry},
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
	if circuit.executions != 1 || attempts != 2 {
		t.Fatalf("circuit executions = %d, attempts = %d", circuit.executions, attempts)
	}
	inspection := client.InspectPipeline()
	var transports []string
	for _, information := range inspection.Operation {
		if information.Stage == StageTransport {
			transports = append(transports, information.Name)
		}
	}
	if len(transports) != 2 || transports[0] != "vendor-circuit" || transports[1] != "vendor-retry" {
		t.Fatalf("operation transport order = %v", transports)
	}
}

func TestRateLimitRejectionBypassesCircuitAdmission(t *testing.T) {
	t.Parallel()

	limiter := &rateLimitTestLimiter{acquireErr: ErrRateLimitCapacity}
	rateLimit, err := NewRateLimitMiddleware(RateLimitOptions{Name: "limit", Limiter: limiter})
	if err != nil {
		t.Fatalf("construct rate limit middleware: %v", err)
	}
	circuit := &circuitTestBreaker{}
	circuitMiddleware, err := NewCircuitBreakerMiddleware(CircuitBreakerOptions{
		Name: "circuit", Breaker: circuit,
	})
	if err != nil {
		t.Fatalf("construct circuit middleware: %v", err)
	}
	client, err := New(Config{
		Middleware: append(rateLimit, circuitMiddleware),
		Transport: TransportFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("transport must not run")

			return nil, nil
		}),
	})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer closeTestClient(t, client)
	request, _ := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
	_, err = client.Do(request)
	if !errors.Is(err, ErrRateLimitCapacity) || circuit.executions != 0 {
		t.Fatalf("rate rejection = %v, circuit executions = %d", err, circuit.executions)
	}
}

func TestCircuitRejectionIsTypedSecretSafeAndSkipsNetwork(t *testing.T) {
	t.Parallel()

	secret := errors.New("breaker secret do-not-render")
	circuit := &circuitTestBreaker{failure: &CircuitBreakerError{Cause: secret}}
	middleware, err := NewCircuitBreakerMiddleware(CircuitBreakerOptions{Name: "circuit", Breaker: circuit})
	if err != nil {
		t.Fatalf("construct middleware: %v", err)
	}
	client, err := New(Config{
		Middleware: []Middleware{middleware},
		Transport: TransportFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("transport must not run")

			return nil, nil
		}),
	})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer closeTestClient(t, client)
	request, _ := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
	_, err = client.Do(request)
	var circuitError *CircuitBreakerError
	if !errors.As(err, &circuitError) || !errors.Is(err, ErrCircuitRejected) || !errors.Is(err, secret) {
		t.Fatalf("circuit rejection = %#v", err)
	}
	if strings.Contains(err.Error(), "do-not-render") {
		t.Fatalf("circuit rejection rendered cause: %q", err)
	}
}

func TestDefaultCircuitOutcomeClassification(t *testing.T) {
	t.Parallel()

	classifier := DefaultCircuitOutcomeClassifier()
	request, _ := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
	tests := []struct {
		name     string
		response *http.Response
		failure  error
		want     CircuitOutcome
	}{
		{name: "success", response: &http.Response{StatusCode: http.StatusOK}, want: CircuitOutcomeSuccess},
		{name: "server failure", response: &http.Response{StatusCode: http.StatusServiceUnavailable}, want: CircuitOutcomeFailure},
		{name: "rate response ignored", response: &http.Response{StatusCode: http.StatusTooManyRequests}, want: CircuitOutcomeIgnored},
		{name: "contextless cancellation fails", failure: context.Canceled, want: CircuitOutcomeFailure},
		{name: "deadline failure", failure: context.DeadlineExceeded, want: CircuitOutcomeFailure},
		{name: "local rate rejection ignored", failure: &RateLimitError{Cause: ErrRateLimitCapacity}, want: CircuitOutcomeIgnored},
		{name: "transport failure", failure: errors.New("transport"), want: CircuitOutcomeFailure},
		{name: "impossible empty result", want: CircuitOutcomeIgnored},
	}
	for _, test := range tests {
		if test.response != nil {
			test.response.Request = request
		}
		if got := classifier.Classify(test.response, test.failure); got != test.want {
			t.Fatalf("%s outcome = %v, want %v", test.name, got, test.want)
		}
	}
	if got := CircuitOutcomeClassifierFunc(func(*http.Response, error) CircuitOutcome {
		return CircuitOutcomeFailure
	}).Classify(nil, nil); got != CircuitOutcomeFailure {
		t.Fatalf("classifier adapter outcome = %v", got)
	}
}

func TestGoCircuitBreakerInvokesOneClassifierContract(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(context.Background(), circuitClassifierContextKey{}, "request")
	classifier := &contextCircuitTestClassifier{}
	adapter, err := NewGoCircuitBreakerClassifier(classifier)
	if err != nil {
		t.Fatalf("construct classifier adapter: %v", err)
	}
	outcome := adapter(breaker.Completion{Context: ctx})
	if outcome != breaker.OutcomeSuccess || classifier.context != ctx ||
		classifier.legacyCalls != 0 || classifier.contextCalls != 1 {
		t.Fatalf(
			"outcome = %v, context = %#v, calls = legacy:%d contextual:%d",
			outcome, classifier.context, classifier.legacyCalls, classifier.contextCalls,
		)
	}
}

func TestGoCircuitBreakerAdapterRejectsOpenCircuit(t *testing.T) {
	t.Parallel()

	classifier, err := NewGoCircuitBreakerClassifier(nil)
	if err != nil {
		t.Fatalf("construct Go classifier: %v", err)
	}
	circuit, err := breaker.New(breaker.Config{
		Name: "vendor", MinimumThroughput: 1,
		Opening:    &breaker.OpeningRules{FailureCount: 1},
		Classifier: classifier,
	})
	if err != nil {
		t.Fatalf("construct Go breaker: %v", err)
	}
	adapter, err := NewGoCircuitBreakerAdapter(circuit)
	if err != nil {
		t.Fatalf("construct adapter: %v", err)
	}
	middleware, err := NewCircuitBreakerMiddleware(CircuitBreakerOptions{Name: "circuit", Breaker: adapter})
	if err != nil {
		t.Fatalf("construct middleware: %v", err)
	}
	transportCalls := 0
	client, err := New(Config{
		Middleware: []Middleware{middleware},
		Transport: TransportFunc(func(*http.Request) (*http.Response, error) {
			transportCalls++

			return &http.Response{StatusCode: http.StatusServiceUnavailable}, nil
		}),
	})
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
	if !errors.Is(err, ErrCircuitRejected) || !errors.Is(err, breaker.ErrOpen) || transportCalls != 1 {
		t.Fatalf("open rejection = %v, transport calls = %d", err, transportCalls)
	}
	if err := circuit.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown breaker: %v", err)
	}
}

func TestCircuitBreakerRejectsInvalidPorts(t *testing.T) {
	t.Parallel()

	var nilCircuit *circuitTestBreaker
	if _, err := NewCircuitBreakerMiddleware(CircuitBreakerOptions{Name: "circuit", Breaker: nilCircuit}); !errors.Is(err, ErrInvalidCircuitBreaker) {
		t.Fatalf("typed-nil circuit error = %v", err)
	}
	var nilClassifier *circuitTestClassifier
	if _, err := NewGoCircuitBreakerClassifier(nilClassifier); !errors.Is(err, ErrInvalidCircuitBreaker) {
		t.Fatalf("typed-nil classifier error = %v", err)
	}
	var nilGoBreaker *breaker.Breaker
	if _, err := NewGoCircuitBreakerAdapter(nilGoBreaker); !errors.Is(err, ErrInvalidCircuitBreaker) {
		t.Fatalf("nil Go breaker error = %v", err)
	}
	invalidClassifier, err := NewGoCircuitBreakerClassifier(CircuitOutcomeClassifierFunc(
		func(*http.Response, error) CircuitOutcome { return CircuitOutcome(99) },
	))
	if err != nil {
		t.Fatalf("construct invalid-outcome classifier: %v", err)
	}
	if got := invalidClassifier(breaker.Completion{}); got <= breaker.OutcomeIgnored {
		t.Fatalf("invalid HTTP outcome mapped to %v", got)
	}
	for _, outcome := range []CircuitOutcome{CircuitOutcomeSuccess, CircuitOutcomeIgnored} {
		mapped, mapErr := NewGoCircuitBreakerClassifier(CircuitOutcomeClassifierFunc(
			func(*http.Response, error) CircuitOutcome { return outcome },
		))
		if mapErr != nil || breaker.Outcome(outcome) != mapped(breaker.Completion{}) {
			t.Fatalf("HTTP outcome %v mapping error = %v", outcome, mapErr)
		}
	}
}

func TestCircuitBreakerBoundaryFailures(t *testing.T) {
	t.Parallel()

	delegated := false
	adapter := CircuitBreakerFunc(func(
		ctx context.Context,
		operation func(context.Context) (*http.Response, error),
	) (*http.Response, error) {
		delegated = true

		return operation(ctx)
	})
	response, err := adapter.Execute(context.Background(), func(context.Context) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNoContent}, nil
	})
	if err != nil || response.StatusCode != http.StatusNoContent || !delegated {
		t.Fatalf("circuit function adapter = %#v, %v", response, err)
	}

	tests := []struct {
		name    string
		breaker CircuitBreaker
		want    error
		closed  *bool
	}{
		{
			name: "nil operation context",
			breaker: CircuitBreakerFunc(func(
				_ context.Context,
				operation func(context.Context) (*http.Response, error),
			) (*http.Response, error) {
				return operation(nil)
			}),
			want: ErrInvalidCircuitBreaker,
		},
		{
			name: "untyped rejection",
			breaker: CircuitBreakerFunc(func(
				context.Context,
				func(context.Context) (*http.Response, error),
			) (*http.Response, error) {
				return nil, ErrCircuitRejected
			}),
			want: ErrCircuitRejected,
		},
	}
	closed := false
	tests = append(tests, struct {
		name    string
		breaker CircuitBreaker
		want    error
		closed  *bool
	}{
		name: "rejection response cleanup",
		breaker: CircuitBreakerFunc(func(
			context.Context,
			func(context.Context) (*http.Response, error),
		) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Body:       &retryCloseBody{Reader: strings.NewReader("body"), closed: &closed},
			}, &CircuitBreakerError{}
		}),
		want: ErrCircuitRejected, closed: &closed,
	})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			middleware, middlewareErr := NewCircuitBreakerMiddleware(CircuitBreakerOptions{
				Name: "circuit", Breaker: test.breaker,
			})
			if middlewareErr != nil {
				t.Fatalf("construct middleware: %v", middlewareErr)
			}
			client, clientErr := New(Config{
				Middleware: []Middleware{middleware},
				Transport: TransportFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusNoContent}, nil
				}),
			})
			if clientErr != nil {
				t.Fatalf("construct client: %v", clientErr)
			}
			defer closeTestClient(t, client)
			request, _ := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
			_, requestErr := client.Do(request)
			if !errors.Is(requestErr, test.want) {
				t.Fatalf("boundary error = %v", requestErr)
			}
			if test.closed != nil && !*test.closed {
				t.Fatal("rejection response body was not closed")
			}
		})
	}

	goCircuit, err := breaker.New(breaker.Config{Name: "operation-error"})
	if err != nil {
		t.Fatalf("construct Go breaker: %v", err)
	}
	goAdapter, err := NewGoCircuitBreakerAdapter(goCircuit)
	if err != nil {
		t.Fatalf("construct Go adapter: %v", err)
	}
	operationFailure := errors.New("operation failure")
	_, err = goAdapter.Execute(context.Background(), func(context.Context) (*http.Response, error) {
		return nil, operationFailure
	})
	if !errors.Is(err, operationFailure) {
		t.Fatalf("Go adapter operation error = %v", err)
	}
	_ = goCircuit.Shutdown(context.Background())
}

func TestRateLimitAttemptBoundaryRequiresOperationState(t *testing.T) {
	t.Parallel()

	limiter := &rateLimitSequenceLimiter{now: time.Unix(1_700_000_000, 0), failures: []error{nil, ErrRateLimitCapacity}}
	middleware, err := NewRateLimitMiddleware(RateLimitOptions{Name: "limit", Limiter: limiter})
	if err != nil {
		t.Fatalf("construct middleware: %v", err)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://api.example.test", nil)
	attemptOnly, err := NewPipeline(middleware[1])
	if err != nil {
		t.Fatalf("construct attempt-only pipeline: %v", err)
	}
	_, err = attemptOnly.Execute(request, TransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNoContent}, nil
	}))
	if !errors.Is(err, ErrInvalidRateLimitPolicy) {
		t.Fatalf("missing operation state error = %v", err)
	}

	retry, err := NewRetryMiddleware(RetryOptions{
		Name: "retry", MaximumAttempts: 2,
		Clock:  &rateLimitTestClock{now: limiter.now},
		Jitter: RetryJitterFunc(func(time.Duration) time.Duration { return 0 }),
	})
	if err != nil {
		t.Fatalf("construct retry: %v", err)
	}
	client, err := New(Config{
		Middleware: append(middleware, retry),
		Transport: TransportFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusServiceUnavailable}, nil
		}),
	})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer closeTestClient(t, client)
	request, _ = http.NewRequest(http.MethodGet, "https://api.example.test", nil)
	_, err = client.Do(request)
	if !errors.Is(err, ErrRateLimitCapacity) {
		t.Fatalf("retry admission error = %v", err)
	}
}

type circuitTestBreaker struct {
	executions int
	failure    error
}

func (circuit *circuitTestBreaker) Execute(
	ctx context.Context,
	operation func(context.Context) (*http.Response, error),
) (*http.Response, error) {
	circuit.executions++
	if circuit.failure != nil {
		return nil, circuit.failure
	}

	return operation(ctx)
}

type circuitTestClassifier struct{}

func (*circuitTestClassifier) Classify(*http.Response, error) CircuitOutcome {
	return CircuitOutcomeIgnored
}

type circuitClassifierContextKey struct{}

type contextCircuitTestClassifier struct {
	context      context.Context
	legacyCalls  int
	contextCalls int
}

func (classifier *contextCircuitTestClassifier) Classify(*http.Response, error) CircuitOutcome {
	classifier.legacyCalls++

	return CircuitOutcomeFailure
}

func (classifier *contextCircuitTestClassifier) ClassifyContext(
	ctx context.Context,
	_ *http.Response,
	_ error,
) CircuitOutcome {
	classifier.context = ctx
	classifier.contextCalls++

	return CircuitOutcomeSuccess
}

type rateLimitSequenceLimiter struct {
	now      time.Time
	failures []error
	calls    int
}

func (limiter *rateLimitSequenceLimiter) Acquire(context.Context, time.Duration) (time.Duration, error) {
	index := limiter.calls
	limiter.calls++
	if index < len(limiter.failures) {
		return 0, limiter.failures[index]
	}

	return 0, nil
}
func (*rateLimitSequenceLimiter) DeferUntil(time.Time)   {}
func (limiter *rateLimitSequenceLimiter) Now() time.Time { return limiter.now }
