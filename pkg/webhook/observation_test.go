package webhook

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestVerifierObservesVerificationAndReplayWithoutSensitiveData(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	observer := &recordingObserver{}
	store := &recordingReplayStore{recorded: false}
	request := signedRequestFixture(t, now)
	request.Header.Set("Webhook-Id", "sensitive-event-id")
	verifier, err := NewVerifier(VerifierConfig{
		Algorithm:       SHA256,
		Keys:            []VerificationKey{{ID: "key", Secret: []byte("secret")}},
		Clock:           func() time.Time { return now },
		Tolerance:       time.Minute,
		ReplayStore:     store,
		ReplayTTL:       time.Hour,
		ReplayNamespace: "tenant",
		Observer:        observer,
	})
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}

	_, _, err = verifier.VerifyRequest(context.Background(), request, RequestOptions{
		MaxBodyBytes: 64,
		HeaderLimits: HeaderLimits{MaxSignatures: 1, MaxBytes: 256},
		EventID:      HeaderEventID("Webhook-Id", 64),
	})
	if !errors.Is(err, ErrReplay) {
		t.Fatalf("VerifyRequest() error = %v, want ErrReplay", err)
	}
	events := observer.snapshot()
	if len(events) != 2 || events[0].Operation != OperationReplay || events[0].Outcome != OutcomeRejected ||
		events[1].Operation != OperationVerification || events[1].Outcome != OutcomeRejected {
		t.Fatalf("observations = %#v", events)
	}
	if events[0].Reason != ReasonReplay || events[1].Reason != ReasonReplay {
		t.Fatalf("observation reasons = %#v", events)
	}
}

func TestDelivererObservesRetryAndSuccessAndContainsObserverPanic(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	observer := &recordingObserver{panicAfterRecord: true}
	deliverer := deliveryFixture(t, server.Client(), time.Unix(1_700_000_000, 0), func(context.Context, time.Duration) error { return nil })
	deliverer.observer = observer
	_, err := deliverer.Deliver(context.Background(), DeliveryRequest{
		Endpoint: mustURL(t, server.URL), Body: []byte("secret payload"), EventID: "event", IdempotencyKey: "event",
	})
	if err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	events := observer.snapshot()
	if len(events) != 2 || events[0].Operation != OperationDeliveryAttempt || events[0].Outcome != OutcomeRetry ||
		events[1].Outcome != OutcomeSuccess {
		t.Fatalf("observations = %#v", events)
	}
	if events[0].StatusCode != http.StatusServiceUnavailable || events[0].Attempt != 1 || events[1].Attempt != 2 {
		t.Fatalf("attempt observations = %#v", events)
	}
}

func TestDelivererClampsRegressingClockObservationDuration(t *testing.T) {
	t.Parallel()

	observer := &recordingObserver{}
	deliverer := &Deliverer{observer: observer}
	deliverer.recordAttempt(context.Background(), &DeliveryResult{}, DeliveryAttempt{
		StartedAt:      time.Unix(2, 0),
		CompletedAt:    time.Unix(1, 0),
		Classification: FailureNone,
	})

	events := observer.snapshot()
	if len(events) != 1 || events[0].Duration != 0 {
		t.Fatalf("observations = %#v, want a zero duration", events)
	}
}

func TestObserverFuncAndReasonClassification(t *testing.T) {
	t.Parallel()

	called := false
	var observer Observer = ObserverFunc(func(context.Context, Observation) { called = true })
	observeSafely(observer, context.Background(), Observation{})
	if !called {
		t.Fatal("ObserverFunc was not called")
	}
	for err, want := range map[error]Reason{
		ErrInvalidSignature:         ReasonSignature,
		ErrInvalidTimestamp:         ReasonTimestamp,
		ErrReplay:                   ReasonReplay,
		ErrReplayStore:              ReasonStore,
		ErrBodyTooLarge:             ReasonLimit,
		ErrSignatureHeadersTooLarge: ReasonLimit,
		ErrMalformedSignedHeader:    ReasonSignature,
		context.Canceled:            ReasonCanceled,
	} {
		if got := observationReason(err); got != want {
			t.Fatalf("observationReason(%v) = %q, want %q", err, got, want)
		}
	}
}

type recordingObserver struct {
	mu               sync.Mutex
	events           []Observation
	panicAfterRecord bool
}

func (o *recordingObserver) Observe(_ context.Context, observation Observation) {
	o.mu.Lock()
	o.events = append(o.events, observation)
	o.mu.Unlock()
	if o.panicAfterRecord {
		panic("observer panic")
	}
}

func (o *recordingObserver) snapshot() []Observation {
	o.mu.Lock()
	defer o.mu.Unlock()

	return append([]Observation(nil), o.events...)
}
