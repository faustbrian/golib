package gotelemetry

import (
	"context"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/processmanager"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func BenchmarkDispatcher(b *testing.B) {
	delivery := telemetryDelivery(
		b,
		"benchmark-message",
		eventsourcing.DeliveryLive,
	)
	deliveries := []eventsourcing.Delivery{delivery}
	ctx := context.Background()
	cases := []struct {
		name  string
		build func(*testing.B) eventsourcing.Dispatcher
	}{
		{name: "direct", build: func(*testing.B) eventsourcing.Dispatcher {
			return discardDispatcher{}
		}},
		{name: "noop", build: func(b *testing.B) eventsourcing.Dispatcher {
			return benchmarkDispatcher(b, tracenoop.NewTracerProvider(), metricnoop.NewMeterProvider())
		}},
		{name: "sampled_out", build: func(b *testing.B) eventsourcing.Dispatcher {
			tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample()))
			meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader()))
			b.Cleanup(func() {
				_ = tracerProvider.Shutdown(context.Background())
				_ = meterProvider.Shutdown(context.Background())
			})
			return benchmarkDispatcher(b, tracerProvider, meterProvider)
		}},
		{name: "recording", build: func(b *testing.B) eventsourcing.Dispatcher {
			tracerProvider := sdktrace.NewTracerProvider(
				sdktrace.WithSampler(sdktrace.AlwaysSample()),
				sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(discardSpanExporter{})),
			)
			meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader()))
			b.Cleanup(func() {
				_ = tracerProvider.Shutdown(context.Background())
				_ = meterProvider.Shutdown(context.Background())
			})
			return benchmarkDispatcher(b, tracerProvider, meterProvider)
		}},
	}
	for _, test := range cases {
		b.Run(test.name, func(b *testing.B) {
			dispatcher := test.build(b)
			b.ReportAllocs()
			if err := dispatcher.Dispatch(ctx, deliveries); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for b.Loop() {
				if err := dispatcher.Dispatch(ctx, deliveries); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func benchmarkDispatcher(
	b *testing.B,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
) eventsourcing.Dispatcher {
	b.Helper()
	instrumentation, err := New(testRuntime{
		tracer:     tracerProvider,
		meter:      meterProvider,
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		b.Fatal(err)
	}
	dispatcher, err := instrumentation.WrapDispatcher(discardDispatcher{})
	if err != nil {
		b.Fatal(err)
	}
	return dispatcher
}

type discardSpanExporter struct{}

func (discardSpanExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	return nil
}

func (discardSpanExporter) Shutdown(context.Context) error {
	return nil
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

func BenchmarkProjectionCheckpointStore(b *testing.B) {
	instrumentation, err := New(testRuntime{
		tracer:     tracenoop.NewTracerProvider(),
		meter:      metricnoop.NewMeterProvider(),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		b.Fatal(err)
	}
	status, err := projection.NewStatus(projection.StatusInput{
		State:         projection.StateRunning,
		Checkpoint:    1,
		HasCheckpoint: true,
	})
	if err != nil {
		b.Fatal(err)
	}
	store, err := instrumentation.WrapProjectionCheckpointStore(
		&telemetryCheckpointStore{status: status},
	)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		if err := store.Save(
			ctx,
			"benchmark-summary",
			1,
			2,
		); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkProjectionController(b *testing.B) {
	instrumentation, err := New(testRuntime{
		tracer:     tracenoop.NewTracerProvider(),
		meter:      metricnoop.NewMeterProvider(),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		b.Fatal(err)
	}
	status, err := projection.NewStatus(projection.StatusInput{
		State: projection.StatePaused,
	})
	if err != nil {
		b.Fatal(err)
	}
	controller, err := instrumentation.WrapProjectionController(
		"benchmark-summary",
		&telemetryProjectionController{pause: status},
	)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		if _, err := controller.Pause(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkProjectionHandler(b *testing.B) {
	instrumentation, err := New(testRuntime{
		tracer:     tracenoop.NewTracerProvider(),
		meter:      metricnoop.NewMeterProvider(),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		b.Fatal(err)
	}
	handler, err := instrumentation.WrapProjectionHandler(
		"benchmark-summary",
		func(context.Context, eventsourcing.Delivery) error {
			return nil
		},
	)
	if err != nil {
		b.Fatal(err)
	}
	delivery := telemetryDelivery(
		b,
		"benchmark-projection-handler",
		eventsourcing.DeliveryReplay,
	)
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		if err := handler(ctx, delivery); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkProcessManager(b *testing.B) {
	instrumentation, err := New(testRuntime{
		tracer:     tracenoop.NewTracerProvider(),
		meter:      metricnoop.NewMeterProvider(),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		b.Fatal(err)
	}
	manager, err := processmanager.New(processmanager.Config[uint64]{
		Name:   "benchmark-planner",
		Replay: processmanager.RejectReplay,
		EventNames: []eventsourcing.EventName{
			telemetryEventName(b),
		},
		MaxCommands: 1,
		Planner: func(
			context.Context,
			eventsourcing.Delivery,
		) ([]uint64, error) {
			return []uint64{1}, nil
		},
	})
	if err != nil {
		b.Fatal(err)
	}
	wrapped, err := WrapProcessManager(
		instrumentation,
		"benchmark-planner",
		manager,
	)
	if err != nil {
		b.Fatal(err)
	}
	delivery := telemetryDelivery(
		b,
		"benchmark-process-manager",
		eventsourcing.DeliveryLive,
	)
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		if _, err := wrapped.Plan(ctx, delivery); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPayloadCodec(b *testing.B) {
	instrumentation, err := New(testRuntime{
		tracer:     tracenoop.NewTracerProvider(),
		meter:      metricnoop.NewMeterProvider(),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		b.Fatal(err)
	}
	decoded, encoded := telemetrySerializationEvents(b)
	codec, err := instrumentation.WrapPayloadCodec(
		&legacyTelemetryPayloadCodec{
			decoded: decoded,
			encoded: encoded,
		},
	)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		if _, err := codec.EncodeContext(ctx, decoded); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUpcaster(b *testing.B) {
	instrumentation, err := New(testRuntime{
		tracer:     tracenoop.NewTracerProvider(),
		meter:      metricnoop.NewMeterProvider(),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		b.Fatal(err)
	}
	_, encoded := telemetrySerializationEvents(b)
	input, err := eventsourcing.NewUpcastEvent(encoded, nil)
	if err != nil {
		b.Fatal(err)
	}
	upcaster, err := instrumentation.WrapUpcaster(
		legacyTelemetryUpcaster(func(
			event eventsourcing.UpcastEvent,
		) ([]eventsourcing.UpcastEvent, error) {
			return []eventsourcing.UpcastEvent{event}, nil
		}),
	)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		if _, err := upcaster.UpcastContext(ctx, input); err != nil {
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
