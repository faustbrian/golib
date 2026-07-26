package gotelemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/gotelemetry"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func FuzzTelemetryLifecycle(f *testing.F) {
	f.Add(
		"tenant",
		"acme",
		"publish",
		"success",
		"message-id",
		"orders",
		1,
		int64(0),
	)

	telemetry, err := gotelemetry.New(testRuntime{
		tracer:     tracenoop.NewTracerProvider(),
		meter:      metricnoop.NewMeterProvider(),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		f.Fatalf("new telemetry: %v", err)
	}
	publisher, err := telemetry.WrapPublisher(&recordingPublisher{})
	if err != nil {
		f.Fatalf("wrap publisher: %v", err)
	}

	f.Fuzz(func(
		t *testing.T,
		key string,
		value string,
		operation string,
		outcome string,
		messageID string,
		topic string,
		count int,
		nanoseconds int64,
	) {
		for _, text := range []string{
			key,
			value,
			operation,
			outcome,
			messageID,
			topic,
		} {
			if len(text) > 256 {
				t.Skip()
			}
		}

		metadata := map[string]string{key: value}
		injected := telemetry.Inject(context.Background(), metadata)
		if len(metadata) != 1 || metadata[key] != value {
			t.Fatal("Inject() mutated caller-owned metadata")
		}
		if injected[key] != value {
			t.Fatal("Inject() did not preserve caller metadata")
		}

		telemetry.Observe(context.Background(), outbox.Event{
			Operation: outbox.Operation(operation),
			Outcome:   outbox.Outcome(outcome),
			Count:     count,
			MessageID: messageID,
			Topic:     topic,
			Duration:  time.Duration(nanoseconds),
		})
		oldest := time.Unix(0, nanoseconds)
		telemetry.RecordBacklog(context.Background(), outbox.BacklogStats{
			Pending:         int64(count),
			Leased:          int64(-count),
			Dead:            nanoseconds,
			OldestPendingAt: &oldest,
		}, time.Unix(0, 0))
		if publishErr := publisher.Publish(context.Background(), outbox.Envelope{
			ID:       messageID,
			Topic:    topic,
			Metadata: injected,
			Attempts: count,
		}); publishErr != nil {
			t.Fatalf("Publish() error = %v", publishErr)
		}
	})
}
