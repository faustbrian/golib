// Package gotelemetry links outbox publications and operations to the
// standard providers exposed by github.com/faustbrian/golib/pkg/telemetry.
package gotelemetry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "github.com/faustbrian/golib/pkg/outbox"

var (
	ErrRuntimeRequired   = errors.New("outbox/gotelemetry: runtime is required")
	ErrPublisherRequired = errors.New("outbox/gotelemetry: publisher is required")
)

// Runtime is the standard-provider surface implemented by telemetry's
// Runtime. Keeping the interface here avoids adding telemetry to core.
type Runtime interface {
	TracerProvider() trace.TracerProvider
	MeterProvider() metric.MeterProvider
	Propagator() propagation.TextMapPropagator
}

// Publisher is the relay-compatible publication contract.
type Publisher interface {
	Publish(context.Context, outbox.Envelope) error
}

// Telemetry injects and extracts W3C context and implements outbox.Observer.
type Telemetry struct {
	propagator       propagation.TextMapPropagator
	tracer           trace.Tracer
	operations       metric.Int64Counter
	duration         metric.Float64Histogram
	backlogDepth     metric.Int64Gauge
	oldestPendingAge metric.Float64Gauge
}

// New creates instrumentation from a telemetry-compatible runtime.
func New(runtime Runtime) (*Telemetry, error) {
	if runtime == nil {
		return nil, ErrRuntimeRequired
	}
	meter := runtime.MeterProvider().Meter(instrumentationName)
	operations, err := meter.Int64Counter("outbox.operations",
		metric.WithDescription("Completed outbox operations"))
	if err != nil {
		return nil, fmt.Errorf("outbox/gotelemetry: create operations counter: %w", err)
	}
	duration, err := meter.Float64Histogram("outbox.operation.duration",
		metric.WithDescription("Outbox operation latency"),
		metric.WithUnit("s"))
	if err != nil {
		return nil, fmt.Errorf("outbox/gotelemetry: create duration histogram: %w", err)
	}
	backlogDepth, err := meter.Int64Gauge("outbox.backlog.depth",
		metric.WithDescription("Current outbox backlog depth"))
	if err != nil {
		return nil, fmt.Errorf("outbox/gotelemetry: create backlog depth gauge: %w", err)
	}
	oldestPendingAge, err := meter.Float64Gauge("outbox.backlog.oldest_pending_age",
		metric.WithDescription("Age of the oldest pending outbox message"),
		metric.WithUnit("s"))
	if err != nil {
		return nil, fmt.Errorf("outbox/gotelemetry: create oldest pending age gauge: %w", err)
	}

	return &Telemetry{
		propagator:       runtime.Propagator(),
		tracer:           runtime.TracerProvider().Tracer(instrumentationName),
		operations:       operations,
		duration:         duration,
		backlogDepth:     backlogDepth,
		oldestPendingAge: oldestPendingAge,
	}, nil
}

// Inject copies metadata and writes the runtime's propagation fields into the
// copy. Caller-owned metadata is never mutated.
func (telemetry *Telemetry) Inject(ctx context.Context, metadata map[string]string) map[string]string {
	injected := make(map[string]string, len(metadata)+3)
	for key, value := range metadata {
		injected[key] = value
	}
	telemetry.propagator.Inject(ctx, propagation.MapCarrier(injected))

	return injected
}

// Observe records low-cardinality counts and latency. Message IDs and topics
// are intentionally excluded from metric attributes.
func (telemetry *Telemetry) Observe(ctx context.Context, event outbox.Event) {
	count := int64(event.Count)
	if count <= 0 {
		count = 1
	}
	attributes := metric.WithAttributes(
		attribute.String("outbox.operation", string(event.Operation)),
		attribute.String("outbox.outcome", string(event.Outcome)),
	)
	telemetry.operations.Add(ctx, count, attributes)
	telemetry.duration.Record(ctx, event.Duration.Seconds(), attributes)
}

// RecordBacklog records a payload-safe snapshot returned by Store.Backlog.
// The caller supplies now so collection is deterministic and clock-injectable.
func (telemetry *Telemetry) RecordBacklog(ctx context.Context, stats outbox.BacklogStats, now time.Time) {
	for state, depth := range map[string]int64{
		"pending": stats.Pending,
		"leased":  stats.Leased,
		"dead":    stats.Dead,
	} {
		telemetry.backlogDepth.Record(ctx, depth,
			metric.WithAttributes(attribute.String("outbox.state", state)))
	}
	if stats.OldestPendingAt == nil {
		return
	}
	age := now.Sub(*stats.OldestPendingAt)
	if age < 0 {
		age = 0
	}
	telemetry.oldestPendingAge.Record(ctx, age.Seconds())
}

// WrapPublisher extracts producer context from envelope metadata and creates
// a publish span around the downstream publisher call.
func (telemetry *Telemetry) WrapPublisher(next Publisher) (Publisher, error) {
	if next == nil {
		return nil, ErrPublisherRequired
	}

	return publisher{telemetry: telemetry, next: next}, nil
}

type publisher struct {
	telemetry *Telemetry
	next      Publisher
}

func (publisher publisher) Publish(ctx context.Context, envelope outbox.Envelope) error {
	ctx = publisher.telemetry.propagator.Extract(ctx, propagation.MapCarrier(envelope.Metadata))
	ctx, span := publisher.telemetry.tracer.Start(ctx, "outbox.publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.destination.name", envelope.Topic),
			attribute.String("outbox.message.id", envelope.ID),
			attribute.Int("outbox.message.attempts", envelope.Attempts),
		),
	)
	defer span.End()

	if err := publisher.next.Publish(ctx, envelope); err != nil {
		span.SetStatus(codes.Error, "publisher rejected message")

		return err
	}

	return nil
}
