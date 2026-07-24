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
