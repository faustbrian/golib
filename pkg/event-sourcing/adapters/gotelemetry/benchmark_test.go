package gotelemetry

import (
	"context"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func BenchmarkDispatcher(b *testing.B) {
	instrumentation, err := New(testRuntime{
		tracer:     tracenoop.NewTracerProvider(),
		meter:      metricnoop.NewMeterProvider(),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		b.Fatal(err)
	}
	dispatcher, err := instrumentation.WrapDispatcher(
		eventsourcing.Dispatcher(discardDispatcher{}),
	)
	if err != nil {
		b.Fatal(err)
	}
	delivery := telemetryDelivery(
		b,
		"benchmark-message",
		eventsourcing.DeliveryLive,
	)
	deliveries := []eventsourcing.Delivery{delivery}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := dispatcher.Dispatch(ctx, deliveries); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConsumer(b *testing.B) {
	instrumentation, err := New(testRuntime{
		tracer:     tracenoop.NewTracerProvider(),
		meter:      metricnoop.NewMeterProvider(),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		b.Fatal(err)
	}
	consumer, err := instrumentation.WrapConsumer(
		func(context.Context, eventsourcing.Delivery) error {
			return nil
		},
	)
	if err != nil {
		b.Fatal(err)
	}
	delivery := telemetryDelivery(
		b,
		"benchmark-message",
		eventsourcing.DeliveryLive,
	)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := consumer(ctx, delivery); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEventStoreAppend(b *testing.B) {
	instrumentation, err := New(testRuntime{
		tracer:     tracenoop.NewTracerProvider(),
		meter:      metricnoop.NewMeterProvider(),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		b.Fatal(err)
	}
	store, err := instrumentation.WrapEventStore(&telemetryStore{})
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	pending := []eventsourcing.PendingMessage{{}}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := store.Append(
			ctx,
			eventsourcing.StreamID{},
			eventsourcing.ExpectNewStream(),
			pending,
		); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkProjectionRunner(b *testing.B) {
	instrumentation, err := New(testRuntime{
		tracer:     tracenoop.NewTracerProvider(),
		meter:      metricnoop.NewMeterProvider(),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		b.Fatal(err)
	}
	runner, err := instrumentation.WrapProjectionRunner(
		"benchmark-summary",
		&telemetryProjectionRunner{},
	)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		if _, err := runner.RunBatch(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

type discardDispatcher struct{}

func (discardDispatcher) Dispatch(
	context.Context,
	[]eventsourcing.Delivery,
) error {
	return nil
}
