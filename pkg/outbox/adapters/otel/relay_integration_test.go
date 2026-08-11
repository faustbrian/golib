package outboxotel_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/otel"
	"github.com/faustbrian/golib/pkg/outbox/postgres"
	"github.com/faustbrian/golib/pkg/outbox/relay"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestTelemetryCannotAmplifyRelayPublicationOrTransitions(t *testing.T) {
	t.Parallel()

	publishFailure := errors.New("publisher failure")
	panicValue := &privacyPanic{value: "publisher panic"}
	tests := []struct {
		name              string
		runtime           func(*testing.T) outboxotel.Runtime
		publisher         *recordingPublisher
		wantResult        relay.Result
		wantTransition    string
		wantTransitionErr error
	}{
		{
			name: "no-op success",
			runtime: func(*testing.T) outboxotel.Runtime {
				return testRuntime{
					tracer: tracenoop.NewTracerProvider(), meter: metricnoop.NewMeterProvider(), propagator: propagation.TraceContext{},
				}
			},
			publisher:      &recordingPublisher{},
			wantResult:     relay.Result{Claimed: 1, Published: 1, Delivered: 1},
			wantTransition: "delivered",
		},
		{
			name: "panicking provider failure",
			runtime: func(*testing.T) outboxotel.Runtime {
				baseMeter := metricnoop.NewMeterProvider().Meter("relay-proof")
				return testRuntime{
					tracer: tracenoop.NewTracerProvider(),
					meter: panickingMeterProvider{
						MeterProvider: metricnoop.NewMeterProvider(),
						meter:         panickingMeter{Meter: baseMeter},
					},
					propagator: panickingPropagator{},
				}
			},
			publisher:         &recordingPublisher{err: publishFailure},
			wantResult:        relay.Result{Claimed: 1, Retried: 1},
			wantTransition:    "retried",
			wantTransitionErr: publishFailure,
		},
		{
			name: "sampled shutdown provider panic",
			runtime: func(t *testing.T) outboxotel.Runtime {
				tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample()))
				meterProvider := sdkmetric.NewMeterProvider()
				if err := tracerProvider.Shutdown(context.Background()); err != nil {
					t.Fatalf("shutdown tracer provider: %v", err)
				}
				if err := meterProvider.Shutdown(context.Background()); err != nil {
					t.Fatalf("shutdown meter provider: %v", err)
				}

				return testRuntime{
					tracer: tracerProvider, meter: meterProvider, propagator: propagation.TraceContext{},
				}
			},
			publisher:         &recordingPublisher{panicValue: panicValue},
			wantResult:        relay.Result{Claimed: 1, Retried: 1},
			wantTransition:    "retried",
			wantTransitionErr: relay.ErrPublisherPanic,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			instrumentation, err := outboxotel.New(test.runtime(t))
			if err != nil {
				t.Fatalf("new telemetry: %v", err)
			}
			publisher, err := instrumentation.WrapPublisher(test.publisher)
			if err != nil {
				t.Fatalf("wrap publisher: %v", err)
			}
			store := &relayProofStore{claim: postgres.Claim{
				Envelope: outbox.Envelope{ID: "message", Attempts: 1}, LeaseToken: "lease",
			}}
			worker, err := relay.New(store, publisher, relay.Config{
				Owner:                "telemetry-proof",
				BatchSize:            1,
				Workers:              1,
				LeaseDuration:        time.Second,
				LeaseRenewalInterval: time.Millisecond,
				MaxAttempts:          3,
				PollInterval:         time.Millisecond,
				TransitionTimeout:    time.Second,
				Backoff:              func(int) time.Duration { return 0 },
				Heartbeat:            waitForPublishCompletion,
				Observer:             instrumentation,
			})
			if err != nil {
				t.Fatalf("new relay: %v", err)
			}

			result, err := worker.RunOnce(context.Background())
			if err != nil {
				t.Fatalf("run once: %v", err)
			}
			if result != test.wantResult {
				t.Fatalf("result = %#v, want %#v", result, test.wantResult)
			}
			if test.publisher.calls != 1 {
				t.Fatalf("downstream publish calls = %d, want 1", test.publisher.calls)
			}
			if got := store.transition(); got != test.wantTransition {
				t.Fatalf("relay transition = %q, want %q", got, test.wantTransition)
			}
			if test.wantTransitionErr != nil && !errors.Is(store.transitionErr, test.wantTransitionErr) {
				t.Fatalf("transition error = %v, want %v", store.transitionErr, test.wantTransitionErr)
			}
		})
	}
}

func waitForPublishCompletion(ctx context.Context, _ time.Duration, _ func(context.Context) error) error {
	<-ctx.Done()

	return ctx.Err()
}

type relayProofStore struct {
	claim         postgres.Claim
	delivered     int
	retried       int
	deadLettered  int
	released      int
	transitionErr error
}

func (*relayProofStore) Ping(context.Context) error { return nil }

func (store *relayProofStore) Claim(context.Context, postgres.ClaimRequest) ([]postgres.Claim, error) {
	return []postgres.Claim{store.claim}, nil
}

func (*relayProofStore) ExtendLease(
	context.Context,
	postgres.LeaseRef,
	time.Duration,
) (time.Time, error) {
	return time.Now(), nil
}

func (store *relayProofStore) MarkDelivered(context.Context, postgres.LeaseRef) error {
	store.delivered++

	return nil
}

func (store *relayProofStore) Retry(
	_ context.Context,
	_ postgres.LeaseRef,
	_ time.Duration,
	cause error,
) error {
	store.retried++
	store.transitionErr = cause

	return nil
}

func (store *relayProofStore) DeadLetter(_ context.Context, _ postgres.LeaseRef, cause error) error {
	store.deadLettered++
	store.transitionErr = cause

	return nil
}

func (store *relayProofStore) ReleaseLease(context.Context, postgres.LeaseRef) error {
	store.released++

	return nil
}

func (store *relayProofStore) transition() string {
	switch {
	case store.delivered == 1 && store.retried == 0 && store.deadLettered == 0 && store.released == 0:
		return "delivered"
	case store.delivered == 0 && store.retried == 1 && store.deadLettered == 0 && store.released == 0:
		return "retried"
	case store.delivered == 0 && store.retried == 0 && store.deadLettered == 1 && store.released == 0:
		return "dead_lettered"
	case store.delivered == 0 && store.retried == 0 && store.deadLettered == 0 && store.released == 1:
		return "released"
	default:
		return "invalid"
	}
}
