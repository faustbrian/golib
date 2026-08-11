package gotelemetry

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func isolateTelemetry(operation func()) {
	defer func() {
		_ = recover()
	}()
	operation()
}

func isolateTelemetryValue[Value any](fallback Value, operation func() Value) (
	result Value,
) {
	result = fallback
	defer func() {
		_ = recover()
	}()
	return operation()
}

type isolatedTracer struct {
	trace.Tracer
}

func (tracer isolatedTracer) Start(
	ctx context.Context,
	name string,
	options ...trace.SpanStartOption,
) (spanContext context.Context, span trace.Span) {
	spanContext = ctx
	span = isolatedSpan{Span: trace.SpanFromContext(context.Background())}
	defer func() {
		if recover() != nil {
			spanContext = ctx
		}
	}()
	spanContext, span = tracer.Tracer.Start(ctx, name, options...)
	span = isolatedSpan{Span: span}
	spanContext = trace.ContextWithSpan(spanContext, span)
	return spanContext, span
}

type isolatedSpan struct {
	trace.Span
}

func (span isolatedSpan) End(options ...trace.SpanEndOption) {
	isolateTelemetry(func() { span.Span.End(options...) })
}

func (span isolatedSpan) AddEvent(name string, options ...trace.EventOption) {
	isolateTelemetry(func() { span.Span.AddEvent(name, options...) })
}

func (span isolatedSpan) AddLink(link trace.Link) {
	isolateTelemetry(func() { span.Span.AddLink(link) })
}

func (span isolatedSpan) IsRecording() bool {
	return isolateTelemetryValue(false, span.Span.IsRecording)
}

func (span isolatedSpan) RecordError(err error, options ...trace.EventOption) {
	isolateTelemetry(func() { span.Span.RecordError(err, options...) })
}

func (span isolatedSpan) SpanContext() trace.SpanContext {
	return isolateTelemetryValue(trace.SpanContext{}, span.Span.SpanContext)
}

func (span isolatedSpan) SetStatus(code codes.Code, description string) {
	isolateTelemetry(func() { span.Span.SetStatus(code, description) })
}

func (span isolatedSpan) SetAttributes(attributes ...attribute.KeyValue) {
	isolateTelemetry(func() { span.Span.SetAttributes(attributes...) })
}

func (span isolatedSpan) SetName(name string) {
	isolateTelemetry(func() { span.Span.SetName(name) })
}

func (span isolatedSpan) TracerProvider() trace.TracerProvider {
	return isolateTelemetryValue[trace.TracerProvider](
		tracenoop.NewTracerProvider(),
		span.Span.TracerProvider,
	)
}

type isolatedInt64Counter struct {
	metric.Int64Counter
}

func (counter isolatedInt64Counter) Add(
	ctx context.Context,
	value int64,
	options ...metric.AddOption,
) {
	isolateTelemetry(func() { counter.Int64Counter.Add(ctx, value, options...) })
}

type isolatedFloat64Histogram struct {
	metric.Float64Histogram
}

func (histogram isolatedFloat64Histogram) Record(
	ctx context.Context,
	value float64,
	options ...metric.RecordOption,
) {
	isolateTelemetry(func() {
		histogram.Float64Histogram.Record(ctx, value, options...)
	})
}

type isolatedInt64Histogram struct {
	metric.Int64Histogram
}

func (histogram isolatedInt64Histogram) Record(
	ctx context.Context,
	value int64,
	options ...metric.RecordOption,
) {
	isolateTelemetry(func() {
		histogram.Int64Histogram.Record(ctx, value, options...)
	})
}
