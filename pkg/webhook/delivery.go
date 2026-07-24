package webhook

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const IdempotencyHeader = "Idempotency-Key"

var (
	ErrDeliveryFailed   = errors.New("webhook delivery failed")
	ErrEndpointRejected = errors.New("webhook endpoint rejected")
	ErrResponseTooLarge = errors.New("webhook response too large")
	ErrFanOutLimit      = errors.New("webhook fan-out limit exceeded")
)

// HTTPDoer is implemented by http.Client and compatible client wrappers.
type HTTPDoer interface {
	Do(request *http.Request) (*http.Response, error)
}

// EndpointPolicy validates an endpoint immediately before every attempt.
type EndpointPolicy interface {
	Validate(ctx context.Context, endpoint *url.URL) error
}

// EndpointPolicyFunc adapts a function into an endpoint policy.
type EndpointPolicyFunc func(ctx context.Context, endpoint *url.URL) error

// Validate implements EndpointPolicy.
func (f EndpointPolicyFunc) Validate(ctx context.Context, endpoint *url.URL) error {
	return f(ctx, endpoint)
}

// SleepFunc provides cancellable, injectable delivery backoff.
type SleepFunc func(ctx context.Context, duration time.Duration) error

// FailureClassification is a stable delivery outcome category.
type FailureClassification string

const (
	FailureNone      FailureClassification = "none"
	FailureRetryable FailureClassification = "retryable"
	FailureTerminal  FailureClassification = "terminal"
	FailureExhausted FailureClassification = "exhausted"
)

// RetryPolicy bounds attempts and exponential backoff.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

// Delay returns a Retry-After delay when valid, otherwise exponential backoff.
func (p RetryPolicy) Delay(attempt int, now time.Time, retryAfter string) time.Duration {
	delay := time.Duration(0)
	if seconds, err := strconv.ParseInt(retryAfter, 10, 64); err == nil && seconds >= 0 {
		if seconds > int64(p.MaxDelay/time.Second) {
			delay = p.MaxDelay
		} else {
			delay = time.Duration(seconds) * time.Second
		}
	} else if parsed, err := http.ParseTime(retryAfter); err == nil && parsed.After(now) {
		delay = parsed.Sub(now)
	} else {
		delay = p.BaseDelay
		for current := 1; current < attempt && delay < p.MaxDelay; current++ {
			if delay > p.MaxDelay/2 {
				delay = p.MaxDelay
				break
			}
			delay *= 2
		}
	}
	if delay > p.MaxDelay {
		return p.MaxDelay
	}

	return delay
}

// DeliveryAttempt records one actual HTTP attempt without payloads, secrets,
// signatures, endpoint query strings, or sensitive response headers.
type DeliveryAttempt struct {
	ID             string
	Number         int
	StartedAt      time.Time
	CompletedAt    time.Time
	StatusCode     int
	Classification FailureClassification
	RetryAfter     time.Duration
	Diagnostic     string
}

// DeliveryResult contains bounded delivery evidence.
type DeliveryResult struct {
	ID           string
	EventID      string
	Attempts     []DeliveryAttempt
	ResponseBody []byte
}

// DeliveryRequest describes one endpoint delivery.
type DeliveryRequest struct {
	Endpoint       *url.URL
	Body           []byte
	EventID        string
	IdempotencyKey string
	Headers        http.Header
	Metadata       map[string]string
}

// DeadLetterFunc receives a terminal bounded result for durable handling by
// the application. The core does not implement storage or a queue.
type DeadLetterFunc func(ctx context.Context, result DeliveryResult) error

// ReplayHook records an operator-requested replay before a new delivery starts.
type ReplayHook func(ctx context.Context, originalDeliveryID, eventID string) error

// DeliveryConfig configures a bounded deliverer.
type DeliveryConfig struct {
	Client           HTTPDoer
	Signer           *Signer
	EndpointPolicy   EndpointPolicy
	Retry            RetryPolicy
	Clock            func() time.Time
	Sleep            SleepFunc
	IDGenerator      func() (string, error)
	MaxRequestBytes  int64
	MaxResponseBytes int64
	MaxFanOut        int
	HeaderLimits     HeaderLimits
	DeadLetter       DeadLetterFunc
	ReplayHook       ReplayHook
	Observer         Observer
}

// Deliverer signs and sends bounded webhook attempts.
type Deliverer struct {
	client           HTTPDoer
	signer           *Signer
	policy           EndpointPolicy
	retry            RetryPolicy
	clock            func() time.Time
	sleep            SleepFunc
	id               func() (string, error)
	maxRequestBytes  int64
	maxResponseBytes int64
	maxFanOut        int
	headerLimits     HeaderLimits
	deadLetter       DeadLetterFunc
	replayHook       ReplayHook
	observer         Observer
}

// NewDeliverer validates every mandatory safety bound and injectable.
func NewDeliverer(config DeliveryConfig) (*Deliverer, error) {
	if config.Client == nil || config.Signer == nil || config.EndpointPolicy == nil ||
		config.IDGenerator == nil || config.MaxRequestBytes <= 0 || config.MaxRequestBytes == math.MaxInt64 ||
		config.MaxResponseBytes <= 0 || config.MaxResponseBytes == math.MaxInt64 ||
		config.MaxFanOut <= 0 ||
		config.HeaderLimits.MaxSignatures <= 0 || config.HeaderLimits.MaxBytes <= 0 ||
		config.Retry.MaxAttempts <= 0 || config.Retry.BaseDelay < 0 || config.Retry.MaxDelay < config.Retry.BaseDelay {
		return nil, fmt.Errorf("%w: incomplete or unsafe delivery configuration", ErrInvalidConfiguration)
	}
	clock := config.Clock
	if clock == nil {
		clock = time.Now
	}
	sleep := config.Sleep
	if sleep == nil {
		sleep = sleepContext
	}

	return &Deliverer{
		client:           config.Client,
		signer:           config.Signer,
		policy:           config.EndpointPolicy,
		retry:            config.Retry,
		clock:            clock,
		sleep:            sleep,
		id:               config.IDGenerator,
		maxRequestBytes:  config.MaxRequestBytes,
		maxResponseBytes: config.MaxResponseBytes,
		maxFanOut:        config.MaxFanOut,
		headerLimits:     config.HeaderLimits,
		deadLetter:       config.DeadLetter,
		replayHook:       config.ReplayHook,
		observer:         config.Observer,
	}, nil
}

// Deliver performs bounded attempts. Retries are disabled for requests that
// do not carry an explicit idempotency key.
func (d *Deliverer) Deliver(ctx context.Context, delivery DeliveryRequest) (DeliveryResult, error) {
	result := DeliveryResult{EventID: delivery.EventID}
	if delivery.Endpoint == nil || delivery.EventID == "" || int64(len(delivery.Body)) > d.maxRequestBytes {
		return result, fmt.Errorf("%w: endpoint, event ID, and bounded body are required", ErrInvalidConfiguration)
	}
	deliveryID, err := d.id()
	if err != nil || deliveryID == "" {
		return result, fmt.Errorf("%w: delivery ID generation failed", ErrDeliveryFailed)
	}
	result.ID = deliveryID
	maxAttempts := d.retry.MaxAttempts
	if delivery.IdempotencyKey == "" {
		maxAttempts = 1
	}

	for number := 1; ; number++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		endpoint := *delivery.Endpoint
		if err := d.policy.Validate(ctx, &endpoint); err != nil {
			failure := fmt.Errorf("%w: policy denied attempt", ErrEndpointRejected)
			return result, d.terminalError(ctx, result, failure)
		}
		attemptID, err := d.id()
		if err != nil || attemptID == "" {
			failure := fmt.Errorf("%w: attempt ID generation failed", ErrDeliveryFailed)
			return result, d.terminalError(ctx, result, failure)
		}
		attempt := DeliveryAttempt{ID: attemptID, Number: number, StartedAt: d.clock().UTC(), Classification: FailureNone}
		request, err := d.request(ctx, &endpoint, delivery)
		if err != nil {
			attempt.CompletedAt = d.clock().UTC()
			attempt.Classification = FailureTerminal
			attempt.Diagnostic = "request construction failed"
			d.recordAttempt(ctx, &result, attempt)
			return result, d.terminalError(ctx, result, err)
		}

		response, doErr := d.client.Do(request)
		attempt.CompletedAt = d.clock().UTC()
		if doErr != nil {
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
			attempt.Diagnostic = "HTTP transport failed"
			if err := ctx.Err(); err != nil {
				attempt.Classification = FailureTerminal
				d.recordAttempt(ctx, &result, attempt)
				return result, err
			}
			if number == maxAttempts {
				attempt.Classification = FailureExhausted
				d.recordAttempt(ctx, &result, attempt)
				failure := fmt.Errorf("%w: transport attempts exhausted", ErrDeliveryFailed)
				return result, d.terminalError(ctx, result, failure)
			}
			attempt.Classification = FailureRetryable
			attempt.RetryAfter = d.retry.Delay(number, d.clock().UTC(), "")
			d.recordAttempt(ctx, &result, attempt)
			if err := d.sleep(ctx, attempt.RetryAfter); err != nil {
				return result, err
			}
			continue
		}

		body, bodyErr := readResponse(response, d.maxResponseBytes)
		attempt.StatusCode = response.StatusCode
		if bodyErr != nil {
			attempt.Classification = FailureTerminal
			attempt.Diagnostic = "response body exceeded limit or could not be read"
			d.recordAttempt(ctx, &result, attempt)
			return result, d.terminalError(ctx, result, bodyErr)
		}
		result.ResponseBody = body
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			d.recordAttempt(ctx, &result, attempt)
			return result, nil
		}

		if retryableStatus(response.StatusCode) && number < maxAttempts {
			attempt.Classification = FailureRetryable
			attempt.RetryAfter = d.retry.Delay(number, d.clock().UTC(), response.Header.Get("Retry-After"))
			d.recordAttempt(ctx, &result, attempt)
			if err := d.sleep(ctx, attempt.RetryAfter); err != nil {
				return result, err
			}
			continue
		}
		if retryableStatus(response.StatusCode) {
			attempt.Classification = FailureExhausted
		} else {
			attempt.Classification = FailureTerminal
		}
		attempt.Diagnostic = "endpoint returned non-success status"
		d.recordAttempt(ctx, &result, attempt)
		failure := fmt.Errorf("%w: endpoint returned status %d", ErrDeliveryFailed, response.StatusCode)

		return result, d.terminalError(ctx, result, failure)
	}
}

// DeliverOnce performs exactly one HTTP attempt even when the Deliverer retry
// policy allows more. Queue and outbox consumers use this to avoid nested
// retry multiplication.
func (d *Deliverer) DeliverOnce(ctx context.Context, delivery DeliveryRequest) (DeliveryResult, error) {
	single := *d
	single.retry.MaxAttempts = 1

	return single.Deliver(ctx, delivery)
}

func (d *Deliverer) request(ctx context.Context, endpoint *url.URL, delivery DeliveryRequest) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(delivery.Body))
	if err != nil {
		return nil, fmt.Errorf("%w: request creation failed", ErrDeliveryFailed)
	}
	request.Header = delivery.Headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	request.Header.Set("Content-Type", "application/json")
	if delivery.IdempotencyKey != "" {
		request.Header.Set(IdempotencyHeader, delivery.IdempotencyKey)
	}
	_, _, err = d.signer.SignRequest(request, RequestOptions{
		MaxBodyBytes: d.maxRequestBytes,
		HeaderLimits: d.headerLimits,
		Metadata:     delivery.Metadata,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: request signing failed", ErrDeliveryFailed)
	}

	return request, nil
}

func readResponse(response *http.Response, maxBytes int64) ([]byte, error) {
	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("%w: missing response body", ErrDeliveryFailed)
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		return nil, fmt.Errorf("%w: response read failed", ErrDeliveryFailed)
	}
	if int64(len(body)) > maxBytes {
		return nil, ErrResponseTooLarge
	}

	return body, nil
}

func retryableStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func (d *Deliverer) recordAttempt(ctx context.Context, result *DeliveryResult, attempt DeliveryAttempt) {
	result.Attempts = append(result.Attempts, attempt)
	outcome := OutcomeFailure
	reason := ReasonTransport
	switch attempt.Classification {
	case FailureNone:
		outcome = OutcomeSuccess
		reason = ReasonNone
	case FailureRetryable:
		outcome = OutcomeRetry
		if attempt.StatusCode != 0 {
			reason = ReasonStatus
		}
	case FailureTerminal, FailureExhausted:
		if attempt.StatusCode != 0 {
			reason = ReasonStatus
		}
	}
	observeSafely(d.observer, ctx, Observation{
		Operation:      OperationDeliveryAttempt,
		Outcome:        outcome,
		Reason:         reason,
		Duration:       elapsed(func() time.Time { return attempt.CompletedAt }, attempt.StartedAt),
		StatusCode:     attempt.StatusCode,
		Attempt:        attempt.Number,
		Classification: attempt.Classification,
	})
}

func (d *Deliverer) terminalError(ctx context.Context, result DeliveryResult, primary error) error {
	if d.deadLetter == nil {
		return primary
	}
	if err := d.deadLetter(ctx, result); err != nil {
		return errors.Join(primary, fmt.Errorf("dead-letter hook failed: %w", err))
	}

	return primary
}

// FanOutResult preserves the input order of a bounded fan-out operation.
type FanOutResult struct {
	Result DeliveryResult
	Err    error
}

// FanOut runs deliveries with a bounded worker pool and no durable queue.
func (d *Deliverer) FanOut(ctx context.Context, deliveries []DeliveryRequest, workers int) ([]FanOutResult, error) {
	if workers <= 0 {
		return nil, fmt.Errorf("%w: positive fan-out workers required", ErrInvalidConfiguration)
	}
	if len(deliveries) > d.maxFanOut {
		return nil, ErrFanOutLimit
	}
	results := make([]FanOutResult, len(deliveries))
	if len(deliveries) == 0 {
		return results, nil
	}
	workers = min(workers, len(deliveries))
	jobs := make(chan int)
	done := make(chan struct{})
	for range workers {
		go func() {
			defer func() { done <- struct{}{} }()
			for index := range jobs {
				results[index].Result, results[index].Err = d.Deliver(ctx, deliveries[index])
			}
		}()
	}
	for index := range deliveries {
		jobs <- index
	}
	close(jobs)
	for range workers {
		<-done
	}

	return results, nil
}

// Replay audits operator intent before starting a new independently identified
// delivery. It does not mutate or reuse the original attempt record.
func (d *Deliverer) Replay(
	ctx context.Context,
	originalDeliveryID string,
	delivery DeliveryRequest,
) (DeliveryResult, error) {
	if originalDeliveryID == "" {
		return DeliveryResult{}, fmt.Errorf("%w: original delivery ID required", ErrInvalidConfiguration)
	}
	if d.replayHook != nil {
		if err := d.replayHook(ctx, originalDeliveryID, delivery.EventID); err != nil {
			return DeliveryResult{}, fmt.Errorf("replay hook failed: %w", err)
		}
	}

	return d.Deliver(ctx, delivery)
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
