package httpclient

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrInvalidRetryPolicy indicates invalid retry configuration or behavior.
	ErrInvalidRetryPolicy = errors.New("invalid HTTP retry policy")
	// ErrRetryExhausted indicates that retry policy cannot make another attempt.
	ErrRetryExhausted = errors.New("HTTP retry attempts exhausted")
)

const (
	defaultRetryBaseDelay      = 100 * time.Millisecond
	defaultRetryMaximumDelay   = 2 * time.Second
	defaultRetryMaximumElapsed = 10 * time.Second
	defaultMaximumRetryAfter   = 30 * time.Second
	maximumRetryAttempts       = 100
	retryDrainBytes            = 64 << 10
	retryMiddlewarePriority    = -500
)

// RetryClock supplies deterministic time and context-aware waits.
// Implementations must be safe for concurrent use.
type RetryClock interface {
	Now() time.Time
	Wait(context.Context, time.Duration) error
}

// RetryJitter bounds one exponential backoff delay. Implementations must be
// safe for concurrent use and return a value between zero and the input.
type RetryJitter interface {
	Apply(time.Duration) time.Duration
}

// RetryJitterFunc adapts a function to RetryJitter.
type RetryJitterFunc func(time.Duration) time.Duration

// Apply implements RetryJitter.
func (function RetryJitterFunc) Apply(delay time.Duration) time.Duration {
	return function(delay)
}

// RetryAttempt describes one completed physical exchange to custom policy.
// Response and Failure are mutually exclusive.
type RetryAttempt struct {
	Request        *http.Request
	Response       *http.Response
	Failure        error
	Attempt        int
	BodyReplayable bool
	HasIdempotency bool
}

// RetryPolicy classifies completed physical exchanges. It must be safe for
// concurrent use and must not consume or close response bodies.
type RetryPolicy interface {
	ShouldRetry(RetryAttempt) bool
}

// RetryPolicyFunc adapts a function to RetryPolicy.
type RetryPolicyFunc func(RetryAttempt) bool

// ShouldRetry implements RetryPolicy.
func (function RetryPolicyFunc) ShouldRetry(attempt RetryAttempt) bool {
	return function(attempt)
}

// RetryOptions configures bounded operation retry middleware.
type RetryOptions struct {
	Name                       string
	Layer                      MiddlewareLayer
	Priority                   int
	MaximumAttempts            int
	MaximumElapsed             time.Duration
	BaseDelay                  time.Duration
	MaximumDelay               time.Duration
	MaximumRetryAfter          time.Duration
	RetryUnsafeWithIdempotency bool
	Clock                      RetryClock
	Jitter                     RetryJitter
	Policy                     RetryPolicy
}

// RetryExhaustedError reports a bounded retry stop without rendering the
// transport cause, response headers, or request data.
type RetryExhaustedError struct {
	Attempts   int
	Elapsed    time.Duration
	StatusCode int
	Cause      error
}

// Error implements error without rendering potentially sensitive causes.
func (*RetryExhaustedError) Error() string {
	return "HTTP retry attempts exhausted"
}

// Unwrap preserves the stable sentinel and final cause.
func (err *RetryExhaustedError) Unwrap() []error {
	causes := []error{ErrRetryExhausted}
	if err.Cause != nil {
		causes = append(causes, err.Cause)
	}

	return causes
}

type resolvedRetryOptions struct {
	maximumAttempts   int
	maximumElapsed    time.Duration
	baseDelay         time.Duration
	maximumDelay      time.Duration
	maximumRetryAfter time.Duration
	clock             RetryClock
	jitter            RetryJitter
	policy            RetryPolicy
}

// NewRetryMiddleware creates operation-scoped transport middleware. The
// middleware retries only replayable requests accepted by endpoint policy.
func NewRetryMiddleware(options RetryOptions) (Middleware, error) {
	resolved, err := resolveRetryOptions(options)
	if err != nil {
		return Middleware{}, err
	}

	return NewTransportMiddleware(MiddlewareOptions{
		Name: options.Name, Scope: ScopeOperation, Layer: options.Layer,
		Priority: retryMiddlewarePriority + options.Priority,
	}, func(request *http.Request, next Next) (*http.Response, error) {
		return executeRetry(request, next, resolved)
	})
}

func resolveRetryOptions(options RetryOptions) (resolvedRetryOptions, error) {
	if options.MaximumAttempts < 2 || options.MaximumAttempts > maximumRetryAttempts {
		return resolvedRetryOptions{}, fmt.Errorf("%w: maximum attempts must be between 2 and 100", ErrInvalidRetryPolicy)
	}
	baseDelay := options.BaseDelay
	if baseDelay == 0 {
		baseDelay = defaultRetryBaseDelay
	}
	maximumDelay := options.MaximumDelay
	if maximumDelay == 0 {
		maximumDelay = defaultRetryMaximumDelay
	}
	maximumElapsed := options.MaximumElapsed
	if maximumElapsed == 0 {
		maximumElapsed = defaultRetryMaximumElapsed
	}
	maximumRetryAfter := options.MaximumRetryAfter
	if maximumRetryAfter == 0 {
		maximumRetryAfter = defaultMaximumRetryAfter
	}
	if baseDelay < 0 || maximumDelay < baseDelay || maximumElapsed < 0 || maximumRetryAfter < 0 {
		return resolvedRetryOptions{}, fmt.Errorf("%w: retry duration bounds are invalid", ErrInvalidRetryPolicy)
	}
	clock := options.Clock
	if clock == nil {
		clock = systemRetryClock{}
	} else if nilLike(clock) {
		return resolvedRetryOptions{}, fmt.Errorf("%w: clock is nil", ErrInvalidRetryPolicy)
	}
	jitter := options.Jitter
	if jitter == nil {
		jitter = cryptoRetryJitter{reader: rand.Reader}
	} else if nilLike(jitter) {
		return resolvedRetryOptions{}, fmt.Errorf("%w: jitter is nil", ErrInvalidRetryPolicy)
	}
	policy := options.Policy
	if policy == nil {
		policy = defaultRetryPolicy{unsafeIdempotency: options.RetryUnsafeWithIdempotency}
	} else if nilLike(policy) {
		return resolvedRetryOptions{}, fmt.Errorf("%w: policy is nil", ErrInvalidRetryPolicy)
	}

	return resolvedRetryOptions{
		maximumAttempts: options.MaximumAttempts, maximumElapsed: maximumElapsed,
		baseDelay: baseDelay, maximumDelay: maximumDelay,
		maximumRetryAfter: maximumRetryAfter,
		clock:             clock, jitter: jitter, policy: policy,
	}, nil
}

func executeRetry(request *http.Request, next Next, options resolvedRetryOptions) (*http.Response, error) {
	var pendingResponse *http.Response
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = drainAndCloseRetryResponse(pendingResponse)
			panic(recovered)
		}
	}()
	started := options.clock.Now()
	bodyReplayable := retryBodyReplayable(request)
	hasIdempotency := idempotencyPolicyApplied(request.Context())
	current := request

	for attempt := 1; ; attempt++ {
		response, failure := next(current)
		pendingResponse = response
		metadata := RetryAttempt{
			Request: snapshotRequest(current), Response: response, Failure: failure,
			Attempt: attempt, BodyReplayable: bodyReplayable,
			HasIdempotency: hasIdempotency,
		}
		shouldRetry := options.policy.ShouldRetry(metadata)
		if !shouldRetry {
			if failure != nil {
				pendingResponse = nil
				return nil, retryExhausted(attempt, options.clock.Now().Sub(started), 0, failure)
			}

			pendingResponse = nil
			return response, nil
		}
		statusCode := 0
		if response != nil {
			statusCode = response.StatusCode
		}
		if attempt == options.maximumAttempts || !bodyReplayable {
			closeErr := drainAndCloseRetryResponse(response)
			pendingResponse = nil

			return nil, retryExhausted(attempt, options.clock.Now().Sub(started), statusCode, errors.Join(failure, closeErr))
		}

		delay := retryDelay(response, attempt, options)
		elapsed := options.clock.Now().Sub(started)
		if options.maximumElapsed > 0 && (elapsed >= options.maximumElapsed || delay > options.maximumElapsed-elapsed) {
			closeErr := drainAndCloseRetryResponse(response)
			pendingResponse = nil

			return nil, retryExhausted(attempt, elapsed, statusCode, errors.Join(failure, closeErr))
		}
		if closeErr := drainAndCloseRetryResponse(response); closeErr != nil {
			pendingResponse = nil
			return nil, retryExhausted(attempt, elapsed, statusCode, errors.Join(failure, closeErr))
		}
		pendingResponse = nil
		if waitErr := options.clock.Wait(request.Context(), delay); waitErr != nil {
			return nil, retryExhausted(attempt, options.clock.Now().Sub(started), statusCode, waitErr)
		}
		replayed, replayErr := replayRequest(request)
		if replayErr != nil {
			return nil, retryExhausted(attempt, options.clock.Now().Sub(started), statusCode, replayErr)
		}
		current = replayed
	}

}

func retryExhausted(attempts int, elapsed time.Duration, status int, cause error) *RetryExhaustedError {
	return &RetryExhaustedError{Attempts: attempts, Elapsed: elapsed, StatusCode: status, Cause: cause}
}

type defaultRetryPolicy struct{ unsafeIdempotency bool }

func (policy defaultRetryPolicy) ShouldRetry(attempt RetryAttempt) bool {
	if !attempt.BodyReplayable || attempt.Request == nil || attempt.Request.Context().Err() != nil {
		return false
	}
	if !retrySafeMethod(attempt.Request.Method) && (!policy.unsafeIdempotency || !attempt.HasIdempotency) {
		return false
	}
	if attempt.Failure != nil {
		return !errors.Is(attempt.Failure, context.Canceled) && !errors.Is(attempt.Failure, context.DeadlineExceeded)
	}
	if attempt.Response == nil {
		return false
	}
	switch attempt.Response.StatusCode {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func retrySafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace,
		http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

func retryBodyReplayable(request *http.Request) bool {
	return request != nil && (request.Body == nil || request.Body == http.NoBody || request.GetBody != nil)
}

func replayRequest(original *http.Request) (*http.Request, error) {
	replayed := original.Clone(original.Context())
	if original.Body == nil || original.Body == http.NoBody {
		replayed.Body = original.Body

		return replayed, nil
	}
	body, err := original.GetBody()
	if err != nil {
		return nil, &BodyOpenError{Cause: err}
	}
	if body == nil {
		return nil, &BodyOpenError{Cause: ErrInvalidBody}
	}
	replayed.Body = body

	return replayed, nil
}

func retryDelay(response *http.Response, attempt int, options resolvedRetryOptions) time.Duration {
	if response != nil {
		if delay, ok := parseRetryAfter(response.Header.Get("Retry-After"), options.clock.Now()); ok {
			if delay > options.maximumRetryAfter {
				return options.maximumRetryAfter
			}

			return delay
		}
	}
	delay := options.baseDelay
	for index := 1; index < attempt && delay < options.maximumDelay; index++ {
		if delay > options.maximumDelay/2 {
			delay = options.maximumDelay
			break
		}
		delay *= 2
	}
	if delay > options.maximumDelay {
		delay = options.maximumDelay
	}
	jittered := options.jitter.Apply(delay)
	if jittered < 0 || jittered > delay {
		return delay
	}

	return jittered
}

func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseUint(value, 10, 31); err == nil {
		return time.Duration(seconds) * time.Second, true
	}
	date, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	delay := date.Sub(now)
	if delay < 0 {
		delay = 0
	}

	return delay, true
}

func drainAndCloseRetryResponse(response *http.Response) error {
	if response == nil || response.Body == nil {
		return nil
	}
	_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, retryDrainBytes+1))
	closeErr := response.Body.Close()

	return errors.Join(readErr, closeErr)
}

type systemRetryClock struct{}

func (systemRetryClock) Now() time.Time { return time.Now() }

func (systemRetryClock) Wait(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type cryptoRetryJitter struct{ reader io.Reader }

func (jitter cryptoRetryJitter) Apply(delay time.Duration) time.Duration {
	if delay <= 0 {
		return 0
	}
	upperBound := int64(delay)
	if upperBound == int64(^uint64(0)>>1) {
		upperBound--
	}
	value, err := rand.Int(jitter.reader, big.NewInt(upperBound+1))
	if err != nil {
		return delay
	}

	return time.Duration(value.Int64())
}
