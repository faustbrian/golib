package testtelemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestHarnessRecordsDeterministicSpansAndMetrics(t *testing.T) {
	t.Parallel()

	harness := New()
	ctx, span := harness.TracerProvider().Tracer("test").Start(context.Background(), "operation")
	span.SetAttributes(attribute.String("result", "ok"))
	span.End()
	counter, err := harness.MeterProvider().Meter("test").Int64Counter("jobs.processed")
	if err != nil {
		t.Fatalf("Int64Counter() error = %v", err)
	}
	counter.Add(ctx, 2, metric.WithAttributes(attribute.String("queue", "default")))

	spans := harness.Spans()
	if len(spans) != 1 || spans[0].Name != "operation" {
		t.Fatalf("spans = %+v, want one operation span", spans)
	}
	if !spans[0].SpanContext.TraceID().IsValid() || !spans[0].SpanContext.SpanID().IsValid() {
		t.Fatal("span identifiers are invalid")
	}

	metrics, err := harness.Metrics(context.Background())
	if err != nil {
		t.Fatalf("Metrics() error = %v", err)
	}
	points := metrics.ScopeMetrics[0].Metrics[0].Data.(metricdata.Sum[int64]).DataPoints
	if len(points) != 1 || points[0].Value != 2 {
		t.Fatalf("metric points = %+v, want value 2", points)
	}
	if err := harness.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestHarnessUsesRepeatableIdentifiers(t *testing.T) {
	t.Parallel()

	first := New()
	_, firstSpan := first.TracerProvider().Tracer("test").Start(context.Background(), "first")
	firstContext := firstSpan.SpanContext()
	firstSpan.End()

	second := New()
	_, secondSpan := second.TracerProvider().Tracer("test").Start(context.Background(), "second")
	secondContext := secondSpan.SpanContext()
	secondSpan.End()

	if firstContext.TraceID() != secondContext.TraceID() || firstContext.SpanID() != secondContext.SpanID() {
		t.Fatalf("first IDs %s/%s differ from second IDs %s/%s",
			firstContext.TraceID(), firstContext.SpanID(), secondContext.TraceID(), secondContext.SpanID())
	}
}

func TestHarnessRecordsChildIDsAndResetsSpans(t *testing.T) {
	t.Parallel()

	harness := New()
	ctx, parent := harness.TracerProvider().Tracer("test").Start(context.Background(), "parent")
	_, child := harness.TracerProvider().Tracer("test").Start(ctx, "child")
	if child.SpanContext().SpanID() == parent.SpanContext().SpanID() {
		t.Fatal("child span reused parent span ID")
	}
	child.End()
	parent.End()
	if len(harness.Spans()) != 2 {
		t.Fatalf("spans = %d, want 2", len(harness.Spans()))
	}
	harness.ResetSpans()
	if len(harness.Spans()) != 0 {
		t.Fatalf("spans after reset = %d, want 0", len(harness.Spans()))
	}
}
