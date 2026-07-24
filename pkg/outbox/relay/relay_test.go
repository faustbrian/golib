package relay_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/postgres"
	"github.com/faustbrian/golib/pkg/outbox/relay"
)

func TestRunOncePublishesThenMarksDelivered(t *testing.T) {
	t.Parallel()

	store := &recordingStore{claims: []postgres.Claim{claim("evt-1", 1)}}
	publisher := &recordingPublisher{}
	worker := newRelay(t, store, publisher, relay.Config{})

	result, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if result.Claimed != 1 || result.Published != 1 || result.Delivered != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if len(publisher.envelopes) != 1 || len(store.delivered) != 1 || store.delivered[0].ID != "evt-1" {
		t.Fatalf("publisher/store calls = %#v/%#v", publisher.envelopes, store.delivered)
	}
}

func TestRunOnceRequestsConfiguredSerialization(t *testing.T) {
	t.Parallel()

	store := &recordingStore{}
	worker := newRelay(t, store, &recordingPublisher{}, relay.Config{Serialization: postgres.SerializeByTopic})
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}
	if store.lastRequest.Serialization != postgres.SerializeByTopic {
		t.Fatalf("serialization = %v", store.lastRequest.Serialization)
	}
}

func TestRunOnceEmitsPayloadSafeDiagnostics(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	observer := &recordingObserver{}
	store := &recordingStore{claims: []postgres.Claim{{
		Envelope:   outbox.Envelope{ID: "evt-1", Topic: "orders", Payload: []byte("secret-payload"), Attempts: 1},
		LeaseToken: "token",
	}}}
	worker := newRelay(t, store, &recordingPublisher{err: errors.New("secret-error")}, relay.Config{
		Observer: observer,
		Logger:   slog.New(slog.NewJSONHandler(&logs, nil)),
		Backoff:  func(int) time.Duration { return time.Second },
	})
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}
	if logs.Len() == 0 || bytes.Contains(logs.Bytes(), []byte("secret-payload")) || bytes.Contains(logs.Bytes(), []byte("secret-error")) {
		t.Fatalf("unsafe diagnostics: %s", logs.Bytes())
	}
	operations := observer.operations()
	for _, operation := range []outbox.Operation{outbox.OperationClaim, outbox.OperationPublish, outbox.OperationRetry} {
		if !operations[operation] {
			t.Fatalf("missing %s event: %#v", operation, operations)
		}
	}
}

func TestRunOnceContainsObserverPanic(t *testing.T) {
	t.Parallel()

	store := &recordingStore{claims: []postgres.Claim{claim("evt-1", 1)}}
	worker := newRelay(t, store, &recordingPublisher{}, relay.Config{
		Observer: outbox.ObserverFunc(func(context.Context, outbox.Event) {
			panic("diagnostic failure")
		}),
	})

	result, err := worker.RunOnce(context.Background())
	if err != nil || result.Delivered != 1 {
		t.Fatalf("result/error = %#v/%v", result, err)
	}
}

func TestRunOnceContainsLoggerPanic(t *testing.T) {
	t.Parallel()

	store := &recordingStore{claims: []postgres.Claim{claim("evt-1", 1)}}
	worker := newRelay(t, store, &recordingPublisher{}, relay.Config{
		Logger: slog.New(panicHandler{}),
	})

	result, err := worker.RunOnce(context.Background())
	if err != nil || result.Delivered != 1 {
		t.Fatalf("result/error = %#v/%v", result, err)
	}
}

func TestReadinessAggregatesDatabaseAndPublisherState(t *testing.T) {
	t.Parallel()

	databaseErr := errors.New("database unavailable")
	publisherErr := errors.New("publisher unavailable")
	worker := newRelay(t, &recordingStore{pingErr: databaseErr}, &recordingPublisher{healthErr: publisherErr}, relay.Config{})
	err := worker.Readiness(context.Background())
	if !errors.Is(err, databaseErr) || !errors.Is(err, publisherErr) {
		t.Fatalf("readiness error = %v", err)
	}
}

func TestRunOnceClampsRegressingDiagnosticClock(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)
	times := []time.Time{now, now.Add(-time.Second)}
	var clockCalls int
	observer := &recordingObserver{}
	worker := newRelay(t, &recordingStore{}, &recordingPublisher{}, relay.Config{
		Observer: observer,
		Clock: func() time.Time {
			result := times[clockCalls]
			clockCalls++

			return result
		},
	})
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}
	if len(observer.events) != 1 || observer.events[0].Duration != 0 {
		t.Fatalf("events = %#v", observer.events)
	}
}

func TestRunOnceContainsDiagnosticClockPanic(t *testing.T) {
	t.Parallel()

	worker := newRelay(t, &recordingStore{}, &recordingPublisher{}, relay.Config{
		Clock: func() time.Time { panic("diagnostic clock failure") },
	})
	result, err := worker.RunOnce(context.Background())
	if err != nil || result != (relay.Result{}) {
		t.Fatalf("result/error = %#v/%v", result, err)
	}
}

func TestRunOnceSchedulesClassifiedRetry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)
	publishErr := errors.New("publisher unavailable")
	store := &recordingStore{claims: []postgres.Claim{claim("evt-1", 2)}}
	publisher := &recordingPublisher{err: publishErr}
	worker := newRelay(t, store, publisher, relay.Config{
		Clock:   func() time.Time { return now },
		Backoff: func(int) time.Duration { return 30 * time.Second },
		ClassifyError: func(err error) relay.ErrorClass {
			if !errors.Is(err, publishErr) {
				t.Fatalf("classifier error = %v", err)
			}
			return relay.ErrorTransient
		},
	})

	result, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if result.Retried != 1 || len(store.retried) != 1 {
		t.Fatalf("unexpected retry result: %#v/%#v", result, store.retried)
	}
	if store.retried[0].delay != 30*time.Second || !errors.Is(store.retried[0].cause, publishErr) {
		t.Fatalf("unexpected retry: %#v", store.retried[0])
	}
}

func TestRunOnceContainsInvalidFailurePolicies(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)
	tests := map[string]struct {
		classify  func(error) relay.ErrorClass
		backoff   func(int) time.Duration
		want      error
		wantDelay time.Duration
	}{
		"classifier panic": {
			classify:  func(error) relay.ErrorClass { panic("secret") },
			backoff:   func(int) time.Duration { return 0 },
			want:      relay.ErrClassifierPanic,
			wantDelay: 0,
		},
		"invalid classifier": {
			classify:  func(error) relay.ErrorClass { return relay.ErrorClass(255) },
			backoff:   func(int) time.Duration { return 0 },
			want:      relay.ErrInvalidErrorClass,
			wantDelay: 0,
		},
		"backoff panic": {
			backoff:   func(int) time.Duration { panic("secret") },
			want:      relay.ErrBackoffPanic,
			wantDelay: 0,
		},
		"negative backoff": {
			backoff:   func(int) time.Duration { return -time.Second },
			want:      relay.ErrInvalidBackoff,
			wantDelay: 0,
		},
		"oversized backoff": {
			backoff:   func(int) time.Duration { return 24 * time.Hour },
			want:      relay.ErrInvalidBackoff,
			wantDelay: time.Minute,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			publishErr := errors.New("publisher secret")
			store := &recordingStore{claims: []postgres.Claim{claim("evt-1", 1)}}
			worker := newRelay(t, store, &recordingPublisher{err: publishErr}, relay.Config{
				Clock: func() time.Time { return now }, ClassifyError: test.classify, Backoff: test.backoff,
			})
			result, err := worker.RunOnce(context.Background())
			if !errors.Is(err, test.want) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("error = %v, want payload-safe %v", err, test.want)
			}
			if result.Retried != 1 || len(store.retried) != 1 ||
				store.retried[0].delay != test.wantDelay ||
				!errors.Is(store.retried[0].cause, publishErr) {
				t.Fatalf("result/retry = %#v/%#v", result, store.retried)
			}
		})
	}
}

func TestRunOnceSkipsFailurePolicyAfterMaximumAttempts(t *testing.T) {
	t.Parallel()

	store := &recordingStore{claims: []postgres.Claim{claim("evt-1", 3)}}
	worker := newRelay(t, store, &recordingPublisher{err: errors.New("publish failed")}, relay.Config{
		MaxAttempts: 3,
		ClassifyError: func(error) relay.ErrorClass {
			panic("classifier must not run")
		},
	})
	result, err := worker.RunOnce(context.Background())
	if err != nil || result.DeadLettered != 1 || len(store.dead) != 1 {
		t.Fatalf("result/store/error = %#v/%#v/%v", result, store, err)
	}
}

func TestRunOnceRecoversPublisherPanicAsRetryableFailure(t *testing.T) {
	t.Parallel()

	store := &recordingStore{claims: []postgres.Claim{claim("evt-1", 1)}}
	worker := newRelay(t, store, &recordingPublisher{panicValue: "secret panic value"}, relay.Config{
		Backoff: func(int) time.Duration { return 0 },
	})
	result, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if result.Retried != 1 || len(store.retried) != 1 ||
		!errors.Is(store.retried[0].cause, relay.ErrPublisherPanic) {
		t.Fatalf("result/retries = %#v/%#v", result, store.retried)
	}
}

func TestRunOnceDeadLettersPermanentOrExhaustedFailure(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		attempts int
		class    relay.ErrorClass
	}{
		"permanent": {attempts: 1, class: relay.ErrorPermanent},
		"exhausted": {attempts: 3, class: relay.ErrorTransient},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := &recordingStore{claims: []postgres.Claim{claim("evt-1", test.attempts)}}
			worker := newRelay(t, store, &recordingPublisher{err: errors.New("publish failed")}, relay.Config{
				MaxAttempts:   3,
				ClassifyError: func(error) relay.ErrorClass { return test.class },
			})

			result, err := worker.RunOnce(context.Background())
			if err != nil {
				t.Fatalf("run once: %v", err)
			}
			if result.DeadLettered != 1 || len(store.dead) != 1 || len(store.retried) != 0 {
				t.Fatalf("unexpected result: %#v store=%#v", result, store)
			}
		})
	}
}

func TestRunOnceBoundsPublisherConcurrency(t *testing.T) {
	t.Parallel()

	claims := make([]postgres.Claim, 12)
	for index := range claims {
		claims[index] = claim(string(rune('a'+index)), 1)
	}
	store := &recordingStore{claims: claims}
	publisher := &recordingPublisher{block: make(chan struct{})}
	worker := newRelay(t, store, publisher, relay.Config{Workers: 3, BatchSize: 12})

	done := make(chan error, 1)
	go func() {
		_, err := worker.RunOnce(context.Background())
		done <- err
	}()

	deadline := time.After(2 * time.Second)
	for publisher.maximum() < 3 {
		select {
		case <-deadline:
			t.Fatalf("publisher reached concurrency %d, want 3", publisher.maximum())
		default:
			time.Sleep(time.Millisecond)
		}
	}
	close(publisher.block)
	if err := <-done; err != nil {
		t.Fatalf("run once: %v", err)
	}
	if publisher.maximum() != 3 {
		t.Fatalf("maximum concurrency = %d, want 3", publisher.maximum())
	}
}

func TestRunOnceRenewsLeaseDuringPublication(t *testing.T) {
	t.Parallel()

	extended := make(chan struct{})
	store := &recordingStore{claims: []postgres.Claim{claim("evt-1", 1)}}
	publisher := &recordingPublisher{block: make(chan struct{})}
	worker := newRelay(t, store, publisher, relay.Config{
		LeaseDuration: 30 * time.Second,
		Heartbeat: func(ctx context.Context, interval time.Duration, extend func(context.Context) error) error {
			if interval != 15*time.Second {
				t.Errorf("heartbeat interval = %s", interval)
			}
			if err := extend(ctx); err != nil {
				return err
			}
			close(extended)
			<-ctx.Done()

			return nil
		},
	})
	done := make(chan error, 1)
	go func() {
		_, err := worker.RunOnce(context.Background())
		done <- err
	}()
	<-extended
	close(publisher.block)
	if err := <-done; err != nil {
		t.Fatalf("run once: %v", err)
	}
	if len(store.extended) != 1 || store.extended[0].lease.ID != "evt-1" ||
		store.extended[0].duration != 30*time.Second {
		t.Fatalf("extensions = %#v", store.extended)
	}
}

func TestRunOnceCancelsPublishWhenLeaseRenewalFails(t *testing.T) {
	t.Parallel()

	want := errors.New("lease renewal unavailable")
	store := &recordingStore{claims: []postgres.Claim{claim("evt-1", 1)}, extendErr: want}
	worker := newRelay(t, store, &recordingPublisher{block: make(chan struct{})}, relay.Config{
		Heartbeat: func(ctx context.Context, _ time.Duration, extend func(context.Context) error) error {
			return extend(ctx)
		},
	})
	if _, err := worker.RunOnce(context.Background()); !errors.Is(err, want) {
		t.Fatalf("run once error = %v, want %v", err, want)
	}
	if len(store.delivered)+len(store.retried)+len(store.dead)+len(store.released) != 0 {
		t.Fatalf("transitioned after lease loss: %#v", store)
	}
}

func TestRunOnceContainsHeartbeatPanic(t *testing.T) {
	t.Parallel()

	store := &recordingStore{claims: []postgres.Claim{claim("evt-1", 1)}}
	worker := newRelay(t, store, relayPublisherFunc(func(ctx context.Context, _ outbox.Envelope) error {
		<-ctx.Done()

		return ctx.Err()
	}), relay.Config{
		Heartbeat: func(context.Context, time.Duration, func(context.Context) error) error {
			panic("heartbeat secret")
		},
	})
	result, err := worker.RunOnce(context.Background())
	if !errors.Is(err, relay.ErrHeartbeatPanic) ||
		strings.Contains(err.Error(), "secret") {
		t.Fatalf("result/error = %#v/%v, want payload-safe heartbeat panic", result, err)
	}
	if result.Claimed != 1 || len(store.delivered)+len(store.retried)+len(store.dead)+len(store.released) != 0 {
		t.Fatalf("result/transitions = %#v/%#v, want untouched lease", result, store)
	}
}

func TestRunOnceIgnoresLocallyCanceledInFlightRenewalAfterPublish(t *testing.T) {
	t.Parallel()

	extendStarted := make(chan struct{})
	store := &recordingStore{
		claims:                    []postgres.Claim{claim("evt-1", 1)},
		extendStarted:             extendStarted,
		extendWaitForCancellation: true,
	}
	worker := newRelay(t, store, waitingPublisher{ready: extendStarted}, relay.Config{
		Heartbeat: func(ctx context.Context, _ time.Duration, extend func(context.Context) error) error {
			return extend(ctx)
		},
	})
	result, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if result.Delivered != 1 || len(store.delivered) != 1 {
		t.Fatalf("result/deliveries = %#v/%#v", result, store.delivered)
	}
}

func TestRunOnceReleasesWhenHeartbeatReportsParentCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := &recordingStore{claims: []postgres.Claim{claim("evt-1", 1)}}
	worker := newRelay(t, store, &recordingPublisher{block: make(chan struct{})}, relay.Config{
		Heartbeat: func(ctx context.Context, _ time.Duration, _ func(context.Context) error) error {
			return ctx.Err()
		},
	})
	result, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if result.Released != 1 || len(store.released) != 1 {
		t.Fatalf("result/releases = %#v/%#v", result, store.released)
	}
}

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	store := &recordingStore{}
	publisher := &recordingPublisher{}
	if _, err := relay.New(nil, publisher, relay.Config{Owner: "relay"}); !errors.Is(err, relay.ErrStoreRequired) {
		t.Fatalf("nil store error = %v", err)
	}
	if _, err := relay.New(store, nil, relay.Config{Owner: "relay"}); !errors.Is(err, relay.ErrPublisherRequired) {
		t.Fatalf("nil publisher error = %v", err)
	}
	if _, err := relay.New(store, publisher, relay.Config{}); !errors.Is(err, relay.ErrOwnerRequired) {
		t.Fatalf("empty owner error = %v", err)
	}
	invalid := []relay.Config{
		{Owner: "relay", BatchSize: -1},
		{Owner: "relay", BatchSize: 1001},
		{Owner: "relay", Workers: -1},
		{Owner: "relay", Workers: 257},
		{Owner: "relay", LeaseDuration: -1},
		{Owner: "relay", LeaseDuration: 24*time.Hour + time.Nanosecond},
		{Owner: "relay", MaxAttempts: -1},
		{Owner: "relay", MaxAttempts: 10001},
		{Owner: "relay", PollInterval: -1},
		{Owner: "relay", LeaseRenewalInterval: -1},
		{Owner: "relay", Serialization: 255},
	}
	for _, config := range invalid {
		if _, err := relay.New(store, publisher, config); !errors.Is(err, relay.ErrInvalidConfig) {
			t.Fatalf("config %#v error = %v", config, err)
		}
	}
}

func TestRunOnceRejectsOversizedStoreResponse(t *testing.T) {
	t.Parallel()

	store := &recordingStore{claims: []postgres.Claim{claim("first", 1), claim("second", 1)}}
	worker := newRelay(t, store, &recordingPublisher{}, relay.Config{BatchSize: 1})
	result, err := worker.RunOnce(context.Background())
	if !errors.Is(err, relay.ErrClaimBatchOverflow) {
		t.Fatalf("result/error = %#v/%v, want claim overflow", result, err)
	}
	if result.Claimed != 2 || len(store.delivered)+len(store.retried)+len(store.dead) != 0 {
		t.Fatalf("oversized claim result/store = %#v/%#v", result, store)
	}
}

func TestRunOncePreservesStoreFailures(t *testing.T) {
	t.Parallel()

	storeErr := errors.New("database unavailable")
	tests := map[string]struct {
		store     *recordingStore
		publisher *recordingPublisher
	}{
		"claim": {
			store:     &recordingStore{claimErr: storeErr},
			publisher: &recordingPublisher{},
		},
		"mark delivered": {
			store:     &recordingStore{claims: []postgres.Claim{claim("id", 1)}, deliveredErr: storeErr},
			publisher: &recordingPublisher{},
		},
		"retry": {
			store:     &recordingStore{claims: []postgres.Claim{claim("id", 1)}, retryErr: storeErr},
			publisher: &recordingPublisher{err: errors.New("publish failed")},
		},
		"dead letter": {
			store:     &recordingStore{claims: []postgres.Claim{claim("id", 1)}, deadErr: storeErr},
			publisher: &recordingPublisher{err: errors.New("publish failed")},
		},
		"dead letter exhausted": {
			store:     &recordingStore{claims: []postgres.Claim{claim("id", 10)}, deadErr: storeErr},
			publisher: &recordingPublisher{err: errors.New("publish failed")},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			config := relay.Config{Owner: "relay", Workers: 1, BatchSize: 1, Backoff: func(int) time.Duration { return -1 }}
			if name == "dead letter" {
				config.ClassifyError = func(error) relay.ErrorClass { return relay.ErrorPermanent }
			}
			worker, err := relay.New(test.store, test.publisher, config)
			if err != nil {
				t.Fatalf("create relay: %v", err)
			}
			_, err = worker.RunOnce(context.Background())
			if !errors.Is(err, storeErr) {
				t.Fatalf("run error = %v, want %v", err, storeErr)
			}
		})
	}
}

func TestRunOnceHandlesEmptyClaim(t *testing.T) {
	t.Parallel()

	worker := newRelay(t, &recordingStore{}, &recordingPublisher{}, relay.Config{})
	result, err := worker.RunOnce(context.Background())
	if err != nil || result != (relay.Result{}) {
		t.Fatalf("result/error = %#v/%v", result, err)
	}
}

func TestRunOnceReleasesClaimsOnCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := &recordingStore{claims: []postgres.Claim{claim("id", 1)}}
	worker := newRelay(t, store, &recordingPublisher{block: make(chan struct{})}, relay.Config{})

	result, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if result.Released != 1 || len(store.released) != 1 {
		t.Fatalf("result/store = %#v/%#v", result, store.released)
	}
}

func TestRunPollsUntilCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	store := &recordingStore{}
	waitCalls := 0
	worker := newRelay(t, store, &recordingPublisher{}, relay.Config{
		PollInterval: 250 * time.Millisecond,
		Wait: func(waitCtx context.Context, interval time.Duration) error {
			waitCalls++
			if interval != 250*time.Millisecond {
				t.Fatalf("poll interval = %s", interval)
			}
			cancel()

			return waitCtx.Err()
		},
	})

	if err := worker.Run(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}
	if store.claimCalls != 1 || waitCalls != 1 {
		t.Fatalf("claim/wait calls = %d/%d", store.claimCalls, waitCalls)
	}
}

func TestRunHandlesCancellationAndOperationalErrors(t *testing.T) {
	t.Parallel()

	t.Run("already canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		store := &recordingStore{}
		worker := newRelay(t, store, &recordingPublisher{}, relay.Config{})
		if err := worker.Run(ctx); err != nil || store.claimCalls != 0 {
			t.Fatalf("error/claims = %v/%d", err, store.claimCalls)
		}
	})

	t.Run("claim failure", func(t *testing.T) {
		storeErr := errors.New("database unavailable")
		worker := newRelay(t, &recordingStore{claimErr: storeErr}, &recordingPublisher{}, relay.Config{})
		if err := worker.Run(context.Background()); !errors.Is(err, storeErr) {
			t.Fatalf("error = %v, want %v", err, storeErr)
		}
	})

	t.Run("canceled after cycle", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		worker := newRelay(t, &recordingStore{claims: []postgres.Claim{claim("id", 1)}}, &recordingPublisher{after: cancel}, relay.Config{BatchSize: 2})
		if err := worker.Run(ctx); err != nil {
			t.Fatalf("run: %v", err)
		}
	})

	t.Run("full batch repolls immediately", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		store := &recordingStore{claimsByCall: [][]postgres.Claim{{claim("id", 1)}, nil}}
		worker := newRelay(t, store, &recordingPublisher{}, relay.Config{
			BatchSize: 1,
			Wait: func(context.Context, time.Duration) error {
				cancel()

				return context.Canceled
			},
		})
		if err := worker.Run(ctx); err != nil || store.claimCalls != 2 {
			t.Fatalf("error/claims = %v/%d", err, store.claimCalls)
		}
	})

	t.Run("wait failure", func(t *testing.T) {
		waitErr := errors.New("timer failed")
		worker := newRelay(t, &recordingStore{}, &recordingPublisher{}, relay.Config{
			Wait: func(context.Context, time.Duration) error { return waitErr },
		})
		if err := worker.Run(context.Background()); !errors.Is(err, waitErr) {
			t.Fatalf("error = %v, want %v", err, waitErr)
		}
	})
}

func TestRunOncePreservesReleaseFailure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	releaseErr := errors.New("database unavailable")
	store := &recordingStore{claims: []postgres.Claim{claim("id", 1)}, releaseErr: releaseErr}
	worker := newRelay(t, store, &recordingPublisher{block: make(chan struct{})}, relay.Config{})
	if _, err := worker.RunOnce(ctx); !errors.Is(err, releaseErr) {
		t.Fatalf("error = %v, want %v", err, releaseErr)
	}
}

func newRelay(t *testing.T, store relay.Store, publisher relay.Publisher, config relay.Config) *relay.Relay {
	t.Helper()

	if config.Owner == "" {
		config.Owner = "relay-a"
	}
	worker, err := relay.New(store, publisher, config)
	if err != nil {
		t.Fatalf("create relay: %v", err)
	}

	return worker
}

func claim(id string, attempts int) postgres.Claim {
	return postgres.Claim{
		Envelope:   outbox.Envelope{ID: id, Topic: "topic", Attempts: attempts},
		Owner:      "relay-a",
		LeaseToken: "token-" + id,
	}
}

type retryCall struct {
	lease postgres.LeaseRef
	delay time.Duration
	cause error
}

type extendCall struct {
	lease    postgres.LeaseRef
	duration time.Duration
}

type recordingStore struct {
	claims                    []postgres.Claim
	claimsByCall              [][]postgres.Claim
	claimErr                  error
	delivered                 []postgres.LeaseRef
	deliveredErr              error
	retried                   []retryCall
	retryErr                  error
	dead                      []postgres.LeaseRef
	deadErr                   error
	released                  []postgres.LeaseRef
	releaseErr                error
	extended                  []extendCall
	extendErr                 error
	extendStarted             chan struct{}
	extendWaitForCancellation bool
	claimCalls                int
	lastRequest               postgres.ClaimRequest
	pingErr                   error
	mu                        sync.Mutex
}

func (store *recordingStore) Ping(context.Context) error {
	return store.pingErr
}

func (store *recordingStore) ExtendLease(ctx context.Context, lease postgres.LeaseRef, duration time.Duration) (time.Time, error) {
	store.mu.Lock()
	store.extended = append(store.extended, extendCall{lease: lease, duration: duration})
	started := store.extendStarted
	waitForCancellation := store.extendWaitForCancellation
	extendErr := store.extendErr
	store.mu.Unlock()
	if started != nil {
		close(started)
	}
	if waitForCancellation {
		<-ctx.Done()

		return time.Time{}, ctx.Err()
	}

	return time.Now().Add(duration), extendErr
}

func (store *recordingStore) Claim(_ context.Context, request postgres.ClaimRequest) ([]postgres.Claim, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.claimCalls++
	store.lastRequest = request
	if len(store.claimsByCall) >= store.claimCalls {
		return append([]postgres.Claim(nil), store.claimsByCall[store.claimCalls-1]...), store.claimErr
	}
	return append([]postgres.Claim(nil), store.claims...), store.claimErr
}

func (store *recordingStore) MarkDelivered(_ context.Context, lease postgres.LeaseRef) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.delivered = append(store.delivered, lease)

	return store.deliveredErr
}

func (store *recordingStore) Retry(_ context.Context, lease postgres.LeaseRef, delay time.Duration, cause error) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.retried = append(store.retried, retryCall{lease: lease, delay: delay, cause: cause})

	return store.retryErr
}

func (store *recordingStore) DeadLetter(_ context.Context, lease postgres.LeaseRef, _ error) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.dead = append(store.dead, lease)

	return store.deadErr
}

func (store *recordingStore) ReleaseLease(_ context.Context, lease postgres.LeaseRef) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.released = append(store.released, lease)

	return store.releaseErr
}

type recordingPublisher struct {
	err        error
	block      chan struct{}
	envelopes  []outbox.Envelope
	active     int
	maxActive  int
	after      func()
	healthErr  error
	panicValue any
	mu         sync.Mutex
}

type relayPublisherFunc func(context.Context, outbox.Envelope) error

func (publish relayPublisherFunc) Publish(ctx context.Context, envelope outbox.Envelope) error {
	return publish(ctx, envelope)
}

type waitingPublisher struct {
	ready <-chan struct{}
}

func (publisher waitingPublisher) Publish(context.Context, outbox.Envelope) error {
	<-publisher.ready

	return nil
}

func (publisher *recordingPublisher) Health(context.Context) error {
	return publisher.healthErr
}

func (publisher *recordingPublisher) Publish(ctx context.Context, envelope outbox.Envelope) error {
	if publisher.panicValue != nil {
		panic(publisher.panicValue)
	}
	publisher.mu.Lock()
	publisher.envelopes = append(publisher.envelopes, envelope)
	publisher.active++
	if publisher.active > publisher.maxActive {
		publisher.maxActive = publisher.active
	}
	publisher.mu.Unlock()

	if publisher.block != nil {
		select {
		case <-publisher.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	publisher.mu.Lock()
	publisher.active--
	publisher.mu.Unlock()
	if publisher.after != nil {
		publisher.after()
	}

	return publisher.err
}

func (publisher *recordingPublisher) maximum() int {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()

	return publisher.maxActive
}

type recordingObserver struct {
	events []outbox.Event
	mu     sync.Mutex
}

type panicHandler struct{}

func (panicHandler) Enabled(context.Context, slog.Level) bool { return true }
func (panicHandler) Handle(context.Context, slog.Record) error {
	panic("diagnostic failure")
}
func (handler panicHandler) WithAttrs([]slog.Attr) slog.Handler { return handler }
func (handler panicHandler) WithGroup(string) slog.Handler      { return handler }

func (observer *recordingObserver) Observe(_ context.Context, event outbox.Event) {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	observer.events = append(observer.events, event)
}

func (observer *recordingObserver) operations() map[outbox.Operation]bool {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	operations := make(map[outbox.Operation]bool, len(observer.events))
	for _, event := range observer.events {
		operations[event.Operation] = true
	}

	return operations
}
