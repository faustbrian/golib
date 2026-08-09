package gotelemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/gotelemetry"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func BenchmarkObserve(b *testing.B) {
	telemetry, err := gotelemetry.New(testRuntime{
		tracer:     tracenoop.NewTracerProvider(),
		meter:      metricnoop.NewMeterProvider(),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	event := outbox.Event{
		Operation: outbox.OperationPublish,
		Outcome:   outbox.OutcomeSuccess,
		Count:     1,
		Duration:  time.Millisecond,
	}

	b.ReportAllocs()
	for b.Loop() {
		telemetry.Observe(ctx, event)
	}
}

func BenchmarkPublish(b *testing.B) {
	benchmarks := map[string]gotelemetry.Runtime{
		"no-op": testRuntime{
			tracer: tracenoop.NewTracerProvider(), meter: metricnoop.NewMeterProvider(), propagator: propagation.TraceContext{},
		},
		"sampled-out": testRuntime{
			tracer: sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample())),
			meter:  metricnoop.NewMeterProvider(), propagator: propagation.TraceContext{},
		},
		"recording": testRuntime{
			tracer: sdktrace.NewTracerProvider(sdktrace.WithSyncer(discardSpanExporter{})),
			meter:  metricnoop.NewMeterProvider(), propagator: propagation.TraceContext{},
		},
	}
	for name, runtime := range benchmarks {
		b.Run(name, func(b *testing.B) {
			instrumentation, err := gotelemetry.New(runtime)
			if err != nil {
				b.Fatal(err)
			}
			publisher, err := instrumentation.WrapPublisher(examplePublisher{})
			if err != nil {
				b.Fatal(err)
			}
			ctx := context.Background()
			envelope := outbox.Envelope{Attempts: 2}
			b.ReportAllocs()
			for b.Loop() {
				if err := publisher.Publish(ctx, envelope); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

type discardSpanExporter struct{}

func (discardSpanExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	return nil
}

func (discardSpanExporter) Shutdown(context.Context) error { return nil }
