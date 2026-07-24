package webhook

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeliverRetriesRetryableStatusAndHonorsRetryAfter(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		attempt := requests.Add(1)
		if request.Header.Get(IdempotencyHeader) != "event-123" {
			t.Errorf("idempotency header = %q", request.Header.Get(IdempotencyHeader))
		}
		if len(request.Header.Values(SignatureHeader)) != 1 {
			t.Errorf("signature headers = %v", request.Header.Values(SignatureHeader))
		}
		if attempt == 1 {
			writer.Header().Set("Retry-After", "3")
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	now := time.Unix(1_700_000_000, 0)
	var sleeps []time.Duration
	deliverer := deliveryFixture(t, server.Client(), now, func(_ context.Context, duration time.Duration) error {
		sleeps = append(sleeps, duration)
		return nil
	})

	result, err := deliverer.Deliver(context.Background(), DeliveryRequest{
		Endpoint:       mustURL(t, server.URL),
		Body:           []byte(`{"event":true}`),
		EventID:        "event-123",
		IdempotencyKey: "event-123",
	})
	if err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	if result.ID != "id-1" || len(result.Attempts) != 2 || result.Attempts[0].ID != "id-2" || result.Attempts[1].ID != "id-3" {
		t.Fatalf("Deliver() result = %#v", result)
	}
	if len(sleeps) != 1 || sleeps[0] != 3*time.Second {
		t.Fatalf("sleep durations = %v", sleeps)
	}
	if result.Attempts[0].StatusCode != http.StatusServiceUnavailable || result.Attempts[1].StatusCode != http.StatusNoContent {
		t.Fatalf("attempt statuses = %#v", result.Attempts)
	}
}

func TestDeliverDoesNotRetryTerminalStatusAndDeadLetters(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte("invalid"))
	}))
	defer server.Close()

	now := time.Unix(1_700_000_000, 0)
	deliverer := deliveryFixture(t, server.Client(), now, func(context.Context, time.Duration) error {
		t.Fatal("unexpected retry sleep")
		return nil
	})
	var deadLetter DeliveryResult
	deliverer.deadLetter = func(_ context.Context, result DeliveryResult) error {
		deadLetter = result
		return nil
	}

	result, err := deliverer.Deliver(context.Background(), DeliveryRequest{Endpoint: mustURL(t, server.URL), Body: []byte("body"), EventID: "event"})
	if !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("Deliver() error = %v, want ErrDeliveryFailed", err)
	}
	if len(result.Attempts) != 1 || result.Attempts[0].Classification != FailureTerminal || string(result.ResponseBody) != "invalid" {
		t.Fatalf("Deliver() result = %#v", result)
	}
	if deadLetter.ID != result.ID {
		t.Fatalf("dead letter result = %#v", deadLetter)
	}
}

func TestDeliverRetriesNetworkFailureAndStopsAtBound(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	doer := &errorDoer{err: errors.New("dial failed")}
	deliverer := deliveryFixture(t, doer, now, func(context.Context, time.Duration) error { return nil })
	deliverer.retry = RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Second}

	result, err := deliverer.Deliver(context.Background(), DeliveryRequest{Endpoint: mustURL(t, "https://example.com/hook"), Body: []byte("body"), EventID: "event", IdempotencyKey: "event"})
	if !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("Deliver() error = %v, want ErrDeliveryFailed", err)
	}
	if doer.calls != 3 || len(result.Attempts) != 3 || result.Attempts[2].Classification != FailureExhausted {
		t.Fatalf("calls = %d, result = %#v", doer.calls, result)
	}
}

func TestDeliverFailsClosedWhenEndpointPolicyRejectsRetry(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	doer := &errorDoer{err: errors.New("temporary")}
	policy := &sequencePolicy{}
	deliverer := deliveryFixture(t, doer, now, func(context.Context, time.Duration) error { return nil })
	deliverer.policy = policy

	result, err := deliverer.Deliver(context.Background(), DeliveryRequest{Endpoint: mustURL(t, "https://example.com/hook"), Body: []byte("body"), EventID: "event", IdempotencyKey: "event"})
	if !errors.Is(err, ErrEndpointRejected) {
		t.Fatalf("Deliver() error = %v, want ErrEndpointRejected", err)
	}
	if policy.calls != 2 || doer.calls != 1 || len(result.Attempts) != 1 {
		t.Fatalf("policy calls = %d, doer calls = %d, attempts = %d", policy.calls, doer.calls, len(result.Attempts))
	}
}

func TestDeliverBoundsResponseBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(writer, "123456789")
	}))
	defer server.Close()

	deliverer := deliveryFixture(t, server.Client(), time.Unix(1_700_000_000, 0), func(context.Context, time.Duration) error { return nil })
	deliverer.maxResponseBytes = 8
	result, err := deliverer.Deliver(context.Background(), DeliveryRequest{Endpoint: mustURL(t, server.URL), Body: []byte("body"), EventID: "event"})
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("Deliver() error = %v, want ErrResponseTooLarge", err)
	}
	if len(result.ResponseBody) != 0 {
		t.Fatalf("response body retained after oversize: %q", result.ResponseBody)
	}
}

func TestDeliverClassifiesHTTPClientTimeoutAsExhaustedTransport(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = 10 * time.Millisecond
	deliverer := deliveryFixture(t, client, time.Unix(1_700_000_000, 0), func(context.Context, time.Duration) error { return nil })
	delivery := validDelivery(t)
	delivery.Endpoint = mustURL(t, server.URL)
	result, err := deliverer.DeliverOnce(context.Background(), delivery)
	if !errors.Is(err, ErrDeliveryFailed) || len(result.Attempts) != 1 || result.Attempts[0].Classification != FailureExhausted {
		t.Fatalf("DeliverOnce() result = %#v, error = %v", result, err)
	}
}

func TestDeliverStopsWhenContextIsCancelledDuringBackoff(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	doer := &errorDoer{err: errors.New("temporary")}
	deliverer := deliveryFixture(t, doer, time.Unix(1_700_000_000, 0), func(context.Context, time.Duration) error {
		cancel()
		return context.Canceled
	})

	result, err := deliverer.Deliver(ctx, DeliveryRequest{Endpoint: mustURL(t, "https://example.com/hook"), Body: []byte("body"), EventID: "event", IdempotencyKey: "event"})
	if !errors.Is(err, context.Canceled) || len(result.Attempts) != 1 {
		t.Fatalf("Deliver() error = %v, result = %#v", err, result)
	}
}

func TestRetryAfterSupportsHTTPDateAndCapsDelay(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	policy := RetryPolicy{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: 5 * time.Second}
	if got := policy.Delay(2, now, "120"); got != 5*time.Second {
		t.Fatalf("Delay() seconds = %v", got)
	}
	if got := policy.Delay(2, now, now.UTC().Add(4*time.Second).Format(http.TimeFormat)); got != 4*time.Second {
		t.Fatalf("Delay() date = %v", got)
	}
	if got := policy.Delay(2, now, now.UTC().Add(time.Minute).Format(http.TimeFormat)); got != 5*time.Second {
		t.Fatalf("Delay() capped date = %v", got)
	}
	if got := policy.Delay(2, now, "invalid"); got != 2*time.Second {
		t.Fatalf("Delay() fallback = %v", got)
	}
}

func TestRetryPolicyCapsOverflowingRetryAfterSeconds(t *testing.T) {
	t.Parallel()

	policy := RetryPolicy{BaseDelay: time.Second, MaxDelay: 5 * time.Second}
	if got := policy.Delay(1, time.Unix(1_700_000_000, 0), "9223372036854775807"); got != policy.MaxDelay {
		t.Fatalf("Delay() = %v, want %v", got, policy.MaxDelay)
	}
}

func TestFanOutBoundsConcurrencyAndPreservesResultOrder(t *testing.T) {
	t.Parallel()

	doer := newBlockingDoer(3)
	deliverer := deliveryFixture(t, doer, time.Unix(1_700_000_000, 0), func(context.Context, time.Duration) error { return nil })
	deliverer.maxFanOut = 16
	requests := make([]DeliveryRequest, 8)
	for index := range requests {
		requests[index] = DeliveryRequest{
			Endpoint:       mustURL(t, "https://example.com/hook"),
			Body:           []byte("body"),
			EventID:        "event-" + strconv.Itoa(index),
			IdempotencyKey: "event-" + strconv.Itoa(index),
		}
	}

	resultsChannel := make(chan []FanOutResult, 1)
	go func() {
		results, _ := deliverer.FanOut(context.Background(), requests, 3)
		resultsChannel <- results
	}()
	<-doer.full
	close(doer.release)
	results := <-resultsChannel
	if doer.maximum.Load() != 3 {
		t.Fatalf("maximum concurrency = %d, want 3", doer.maximum.Load())
	}
	if len(results) != len(requests) {
		t.Fatalf("FanOut() returned %d results", len(results))
	}
	for index, result := range results {
		if result.Err != nil || result.Result.EventID != requests[index].EventID {
			t.Fatalf("result %d = %#v", index, result)
		}
	}
}

func TestFanOutRejectsUnboundedInputs(t *testing.T) {
	t.Parallel()

	deliverer := deliveryFixture(t, &errorDoer{err: errors.New("unused")}, time.Unix(1_700_000_000, 0), func(context.Context, time.Duration) error { return nil })
	deliverer.maxFanOut = 1
	requests := []DeliveryRequest{{}, {}}
	if _, err := deliverer.FanOut(context.Background(), requests, 1); !errors.Is(err, ErrFanOutLimit) {
		t.Fatalf("FanOut() error = %v, want ErrFanOutLimit", err)
	}
	if _, err := deliverer.FanOut(context.Background(), requests[:1], 0); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("FanOut() worker error = %v, want ErrInvalidConfiguration", err)
	}
}

func TestDeliveryPreservesDeadLetterHookFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	hookErr := errors.New("dead-letter storage failed")
	deliverer := deliveryFixture(t, server.Client(), time.Unix(1_700_000_000, 0), func(context.Context, time.Duration) error { return nil })
	deliverer.deadLetter = func(context.Context, DeliveryResult) error { return hookErr }
	_, err := deliverer.Deliver(context.Background(), DeliveryRequest{Endpoint: mustURL(t, server.URL), Body: []byte("body"), EventID: "event"})
	if !errors.Is(err, ErrDeliveryFailed) || !errors.Is(err, hookErr) {
		t.Fatalf("Deliver() error = %v, want delivery and hook failures", err)
	}
}

func TestReplayAuditsBeforeStartingNewDelivery(t *testing.T) {
	t.Parallel()

	doer := &errorDoer{err: errors.New("must not be called")}
	deliverer := deliveryFixture(t, doer, time.Unix(1_700_000_000, 0), func(context.Context, time.Duration) error { return nil })
	hookErr := errors.New("replay audit failed")
	deliverer.replayHook = func(_ context.Context, originalID, eventID string) error {
		if originalID != "delivery-old" || eventID != "event" {
			t.Fatalf("replay hook arguments = %q, %q", originalID, eventID)
		}
		return hookErr
	}

	_, err := deliverer.Replay(context.Background(), "delivery-old", DeliveryRequest{
		Endpoint: mustURL(t, "https://example.com/hook"), Body: []byte("body"), EventID: "event",
	})
	if !errors.Is(err, hookErr) || doer.calls != 0 {
		t.Fatalf("Replay() error = %v, doer calls = %d", err, doer.calls)
	}
}

func deliveryFixture(t *testing.T, doer HTTPDoer, now time.Time, sleep SleepFunc) *Deliverer {
	t.Helper()

	signer, err := NewSigner(SignerConfig{
		Algorithm: SHA256,
		Keys:      []SigningKey{{ID: "key", Secret: []byte("secret")}},
		Clock:     func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	var nextID atomic.Int32
	deliverer, err := NewDeliverer(DeliveryConfig{
		Client:           doer,
		Signer:           signer,
		EndpointPolicy:   EndpointPolicyFunc(func(context.Context, *url.URL) error { return nil }),
		Retry:            RetryPolicy{MaxAttempts: 2, BaseDelay: time.Second, MaxDelay: 10 * time.Second},
		Clock:            func() time.Time { return now },
		Sleep:            sleep,
		IDGenerator:      func() (string, error) { return "id-" + strconv.Itoa(int(nextID.Add(1))), nil },
		MaxRequestBytes:  1024,
		MaxResponseBytes: 1024,
		MaxFanOut:        64,
		HeaderLimits:     HeaderLimits{MaxSignatures: 2, MaxBytes: 512},
	})
	if err != nil {
		t.Fatalf("NewDeliverer() error = %v", err)
	}

	return deliverer
}

func mustURL(t *testing.T, value string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}

	return parsed
}

type errorDoer struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (d *errorDoer) Do(*http.Request) (*http.Response, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++

	return nil, d.err
}

type sequencePolicy struct {
	calls int
}

type blockingDoer struct {
	active  atomic.Int32
	maximum atomic.Int32
	full    chan struct{}
	release chan struct{}
	once    sync.Once
	want    int32
}

func newBlockingDoer(want int32) *blockingDoer {
	return &blockingDoer{full: make(chan struct{}), release: make(chan struct{}), want: want}
}

func (d *blockingDoer) Do(*http.Request) (*http.Response, error) {
	active := d.active.Add(1)
	for {
		maximum := d.maximum.Load()
		if active <= maximum || d.maximum.CompareAndSwap(maximum, active) {
			break
		}
	}
	if active == d.want {
		d.once.Do(func() { close(d.full) })
	}
	<-d.release
	d.active.Add(-1)

	return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody}, nil
}

func (p *sequencePolicy) Validate(context.Context, *url.URL) error {
	p.calls++
	if p.calls > 1 {
		return ErrEndpointRejected
	}

	return nil
}
