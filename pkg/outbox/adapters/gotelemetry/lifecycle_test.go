package gotelemetry_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/gotelemetry"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestPublicationSurvivesSamplingAndSDKShutdown(t *testing.T) {
	t.Parallel()

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.NeverSample()),
	)
	meterProvider := sdkmetric.NewMeterProvider()
	instrumentation, err := gotelemetry.New(testRuntime{
		tracer: tracerProvider, meter: meterProvider, propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	if err := tracerProvider.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown tracer provider: %v", err)
	}
	if err := meterProvider.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown meter provider: %v", err)
	}

	want := errors.New("publisher failure")
	publisher, err := instrumentation.WrapPublisher(&recordingPublisher{err: want})
	if err != nil {
		t.Fatalf("wrap publisher: %v", err)
	}
	if err := publisher.Publish(context.Background(), outbox.Envelope{}); err != want {
		t.Fatalf("publish error = %v, want exact %v", err, want)
	}
	instrumentation.Observe(context.Background(), outbox.Event{
		Operation: outbox.OperationPublish,
		Outcome:   outbox.OutcomeFailure,
		Count:     1,
	})
	instrumentation.RecordBacklog(context.Background(), outbox.BacklogStats{}, time.Now())
}

func TestTelemetryIsSafeForConcurrentPublicationAndObservation(t *testing.T) {
	t.Parallel()

	tracerProvider := sdktrace.NewTracerProvider()
	meterProvider := sdkmetric.NewMeterProvider()
	instrumentation, err := gotelemetry.New(testRuntime{
		tracer: tracerProvider, meter: meterProvider, propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	downstream := &atomicPublisher{}
	publisher, err := instrumentation.WrapPublisher(downstream)
	if err != nil {
		t.Fatalf("wrap publisher: %v", err)
	}

	const publications = 64
	var wait sync.WaitGroup
	wait.Add(publications)
	for range publications {
		go func() {
			defer wait.Done()
			if err := publisher.Publish(context.Background(), outbox.Envelope{Attempts: 2}); err != nil {
				t.Errorf("publish: %v", err)
			}
			instrumentation.Observe(context.Background(), outbox.Event{
				Operation: outbox.OperationPublish,
				Outcome:   outbox.OutcomeSuccess,
				Count:     1,
				Attempts:  2,
			})
		}()
	}
	wait.Wait()
	if got := downstream.calls.Load(); got != publications {
		t.Fatalf("downstream calls = %d, want %d", got, publications)
	}
}

type atomicPublisher struct {
	calls atomic.Int64
}

func (publisher *atomicPublisher) Publish(context.Context, outbox.Envelope) error {
	publisher.calls.Add(1)

	return nil
}
