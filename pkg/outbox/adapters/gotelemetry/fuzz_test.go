package gotelemetry_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/gotelemetry"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
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

func FuzzWrappedPublicationIsolation(f *testing.F) {
	f.Add("secret", uint8(0), uint8(0), false, 1)
	f.Add("credential", uint8(4), uint8(2), true, 6)

	f.Fuzz(func(
		t *testing.T,
		secret string,
		providerMode uint8,
		publisherMode uint8,
		canceled bool,
		attempts int,
	) {
		if len(secret) > 256 {
			t.Skip()
		}

		var tracer trace.TracerProvider = tracenoop.NewTracerProvider()
		var propagator propagation.TextMapPropagator = propagation.TraceContext{}
		switch providerMode % 6 {
		case 1:
			propagator = panickingPropagator{}
		case 2:
			propagator = replacingPropagator{}
		case 3:
			propagator = nilContextPropagator{}
		case 4:
			tracer = panickingTracerProvider{TracerProvider: tracer}
		case 5:
			tracer = invalidStartTracerProvider{TracerProvider: tracer, nilSpan: true}
		}
		telemetry, err := gotelemetry.New(testRuntime{
			tracer: tracer, meter: metricnoop.NewMeterProvider(), propagator: propagator,
		})
		if err != nil {
			t.Fatalf("new telemetry: %v", err)
		}

		var wantErr error
		var wantPanic any
		downstream := &recordingPublisher{}
		switch publisherMode % 3 {
		case 1:
			wantErr = errors.New(secret)
			downstream.err = wantErr
		case 2:
			wantPanic = &fuzzPanic{value: secret}
			downstream.panicValue = wantPanic
		}
		publisher, err := telemetry.WrapPublisher(downstream)
		if err != nil {
			t.Fatalf("wrap publisher: %v", err)
		}
		envelope := outbox.Envelope{
			ID: secret, Topic: secret, Payload: []byte(secret),
			Metadata:    map[string]string{"authorization": secret},
			OrderingKey: secret, IdempotencyKey: secret, Attempts: attempts,
		}
		ctx := context.Background()
		if canceled {
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		}

		var gotErr error
		var gotPanic any
		func() {
			defer func() { gotPanic = recover() }()
			gotErr = publisher.Publish(ctx, envelope)
		}()
		if gotErr != wantErr || gotPanic != wantPanic {
			t.Fatalf("publish error/panic = %v/%#v, want exact %v/%#v", gotErr, gotPanic, wantErr, wantPanic)
		}
		if downstream.calls != 1 || !reflect.DeepEqual(downstream.envelope, envelope) {
			t.Fatalf("downstream calls/envelope = %d/%#v", downstream.calls, downstream.envelope)
		}
		if canceled && !errors.Is(downstream.context.Err(), context.Canceled) {
			t.Fatalf("downstream context error = %v", downstream.context.Err())
		}
	})
}

type fuzzPanic struct {
	value string
}
