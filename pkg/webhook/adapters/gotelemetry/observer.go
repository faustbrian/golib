// Package gotelemetry adapts secret-safe webhook observations to the
// telemetry runtime.
package gotelemetry

import (
	"context"
	"errors"
	"strconv"

	telemetry "github.com/faustbrian/golib/pkg/telemetry"
	webhook "github.com/faustbrian/golib/pkg/webhook"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const scopeName = "github.com/faustbrian/golib/pkg/webhook/adapters/gotelemetry"

var ErrInvalidConfig = errors.New("gotelemetry: telemetry runtime is required")

// Observer records one bounded counter and duration measurement per webhook
// observation and adds an event to an already-active span. It never creates
// attributes from payloads, URLs, headers, IDs, keys, signatures, or errors.
type Observer struct {
	events   metric.Int64Counter
	duration metric.Float64Histogram
}

var _ webhook.Observer = (*Observer)(nil)

// New constructs an observer from a telemetry runtime.
func New(runtime *telemetry.Runtime) (*Observer, error) {
	if runtime == nil {
		return nil, ErrInvalidConfig
	}
	return newObserver(runtime.Meter(scopeName))
}

func newObserver(meter metric.Meter) (*Observer, error) {
	events, err := meter.Int64Counter("webhook.operation.count", metric.WithUnit("{operation}"))
	if err != nil {
		return nil, err
	}
	duration, err := meter.Float64Histogram("webhook.operation.duration", metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}

	return &Observer{events: events, duration: duration}, nil
}

// Observe implements webhook.Observer with a fixed low-cardinality schema.
func (o *Observer) Observe(ctx context.Context, event webhook.Observation) {
	attributes := []attribute.KeyValue{
		attribute.String("webhook.operation", string(event.Operation)),
		attribute.String("webhook.outcome", string(event.Outcome)),
		attribute.String("webhook.reason", string(event.Reason)),
		attribute.String("webhook.algorithm", string(event.Algorithm)),
		attribute.String("webhook.classification", string(event.Classification)),
		attribute.String("http.response.status_class", statusClass(event.StatusCode)),
	}
	options := metric.WithAttributes(attributes...)
	o.events.Add(ctx, 1, options)
	o.duration.Record(ctx, event.Duration.Seconds(), options)
	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		span.AddEvent("webhook."+string(event.Operation), trace.WithAttributes(attributes...))
	}
}

func statusClass(status int) string {
	if status < 100 || status > 599 {
		return "none"
	}

	return strconv.Itoa(status/100) + "xx"
}
