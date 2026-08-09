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

const (
	instrumentationName = "github.com/faustbrian/golib/pkg/outbox"

	// InstrumentationVersion versions this adapter's span and metric schema.
	InstrumentationVersion = "1.0.0"
	traceParentKey         = "traceparent"
	traceStateKey          = "tracestate"
)

var (
	// ErrRuntimeRequired reports a missing or incomplete telemetry runtime.
	ErrRuntimeRequired = errors.New("outbox/gotelemetry: runtime is required")
	// ErrPublisherRequired reports a missing downstream publisher.
	ErrPublisherRequired = errors.New("outbox/gotelemetry: publisher is required")
	// ErrInstrumentCreation categorizes provider construction failures.
	ErrInstrumentCreation = errors.New("outbox/gotelemetry: instrument creation failed")
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
func New(runtime Runtime) (telemetry *Telemetry, err error) {
	tracerProvider, meterProvider, propagator, err := runtimeDependencies(runtime)
	if err != nil {
		return nil, err
	}
	defer func() {
		if recover() != nil {
			telemetry = nil
			err = ErrInstrumentCreation
		}
	}()
	meter := meterProvider.Meter(
		instrumentationName,
		metric.WithInstrumentationVersion(InstrumentationVersion),
	)
	operations, err := meter.Int64Counter("outbox.operations",
		metric.WithDescription("Completed outbox operations"),
		metric.WithUnit("{operation}"))
	if err != nil {
		return nil, fmt.Errorf("%w: create operations counter: %w", ErrInstrumentCreation, err)
	}
	duration, err := meter.Float64Histogram("outbox.operation.duration",
		metric.WithDescription("Outbox operation latency"),
		metric.WithUnit("s"))
	if err != nil {
		return nil, fmt.Errorf("%w: create duration histogram: %w", ErrInstrumentCreation, err)
	}
	backlogDepth, err := meter.Int64Gauge("outbox.backlog.depth",
		metric.WithDescription("Current outbox backlog depth"),
		metric.WithUnit("{message}"))
	if err != nil {
		return nil, fmt.Errorf("%w: create backlog depth gauge: %w", ErrInstrumentCreation, err)
	}
	oldestPendingAge, err := meter.Float64Gauge("outbox.backlog.oldest_pending_age",
		metric.WithDescription("Age of the oldest pending outbox message"),
		metric.WithUnit("s"))
	if err != nil {
		return nil, fmt.Errorf("%w: create oldest pending age gauge: %w", ErrInstrumentCreation, err)
	}

	telemetry = &Telemetry{
		propagator: propagator,
		tracer: tracerProvider.Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(InstrumentationVersion),
		),
		operations:       operations,
		duration:         duration,
		backlogDepth:     backlogDepth,
		oldestPendingAge: oldestPendingAge,
	}
	if !telemetry.valid() {
		return nil, ErrInstrumentCreation
	}

	return telemetry, nil
}

func runtimeDependencies(
	runtime Runtime,
) (tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, propagator propagation.TextMapPropagator, err error) {
	if runtime == nil {
		return nil, nil, nil, ErrRuntimeRequired
	}
	defer func() {
		if recover() != nil {
			tracerProvider, meterProvider, propagator = nil, nil, nil
			err = ErrRuntimeRequired
		}
	}()
	tracerProvider = runtime.TracerProvider()
	meterProvider = runtime.MeterProvider()
	propagator = runtime.Propagator()
	if tracerProvider == nil || meterProvider == nil || propagator == nil {
		return nil, nil, nil, ErrRuntimeRequired
	}

	return tracerProvider, meterProvider, propagator, nil
}

// Inject copies metadata and writes the runtime's propagation fields into the
// copy. Caller-owned metadata is never mutated.
func (telemetry *Telemetry) Inject(
	ctx context.Context,
	metadata map[string]string,
) (injected map[string]string) {
	injected = make(map[string]string, len(metadata))
	for key, value := range metadata {
		injected[key] = value
	}
	defer func() { _ = recover() }()
	carrier := propagation.MapCarrier{}
	telemetry.propagator.Inject(ctx, carrier)
	copyTraceContext(injected, carrier)

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
		attribute.String("outbox.operation", boundedOperation(event.Operation)),
		attribute.String("outbox.outcome", boundedOutcome(event.Outcome)),
		attribute.String("outbox.retry.state", retryState(event.Attempts)),
	)
	containTelemetry(func() { telemetry.operations.Add(ctx, count, attributes) })
	containTelemetry(func() { telemetry.duration.Record(ctx, event.Duration.Seconds(), attributes) })
}

// RecordBacklog records a payload-safe snapshot returned by Store.Backlog.
// The caller supplies now so collection is deterministic and clock-injectable.
func (telemetry *Telemetry) RecordBacklog(ctx context.Context, stats outbox.BacklogStats, now time.Time) {
	for state, depth := range map[string]int64{
		"pending": stats.Pending,
		"leased":  stats.Leased,
		"dead":    stats.Dead,
	} {
		containTelemetry(func() {
			telemetry.backlogDepth.Record(ctx, depth,
				metric.WithAttributes(attribute.String("outbox.state", state)))
		})
	}
	if stats.OldestPendingAt == nil {
		return
	}
	age := max(now.Sub(*stats.OldestPendingAt), 0)
	containTelemetry(func() { telemetry.oldestPendingAge.Record(ctx, age.Seconds()) })
}

// WrapPublisher extracts producer context from envelope metadata and creates
// a publish span around the downstream publisher call.
func (telemetry *Telemetry) WrapPublisher(next Publisher) (Publisher, error) {
	if !telemetry.valid() {
		return nil, ErrRuntimeRequired
	}
	if next == nil {
		return nil, ErrPublisherRequired
	}

	wrapper := publisher{telemetry: telemetry, next: next}
	if health, ok := next.(interface{ Health(context.Context) error }); ok {
		return publisherWithHealth{publisher: wrapper, health: health}, nil
	}

	return wrapper, nil
}

func (telemetry *Telemetry) valid() bool {
	return telemetry != nil && telemetry.propagator != nil && telemetry.tracer != nil &&
		telemetry.operations != nil && telemetry.duration != nil && telemetry.backlogDepth != nil &&
		telemetry.oldestPendingAge != nil
}

type publisher struct {
	telemetry *Telemetry
	next      Publisher
}

type publisherWithHealth struct {
	publisher
	health interface{ Health(context.Context) error }
}

func (publisher publisherWithHealth) Health(ctx context.Context) error {
	return publisher.health.Health(ctx)
}

func (publisher publisher) Publish(ctx context.Context, envelope outbox.Envelope) (err error) {
	ctx = publisher.telemetry.extract(ctx, envelope.Metadata)
	ctx, span := publisher.telemetry.startPublishSpan(ctx, envelope.Attempts)
	defer func() {
		panicValue := recover()
		if span != nil {
			completeSpan(span, completionOutcome(err, panicValue))
		}
		if panicValue != nil {
			panic(panicValue)
		}
	}()

	err = publisher.next.Publish(ctx, envelope)

	return err
}

func completionOutcome(err error, panicValue any) string {
	if panicValue != nil {
		return "panic"
	}
	if err != nil {
		return "failure"
	}

	return "success"
}

func completeSpan(span trace.Span, outcome string) {
	containTelemetry(func() {
		span.SetAttributes(attribute.String("outbox.outcome", outcome))
	})
	if outcome != "success" {
		containTelemetry(func() {
			span.SetStatus(codes.Error, "publisher rejected message")
		})
	}
	endSpan(span)
}

func endSpan(span trace.Span) {
	containTelemetry(func() { span.End() })
}

func containTelemetry(operation func()) {
	defer func() { _ = recover() }()
	operation()
}

func (telemetry *Telemetry) startPublishSpan(
	ctx context.Context,
	attempts int,
) (spanContext context.Context, span trace.Span) {
	spanContext = ctx
	defer func() {
		if recover() != nil {
			spanContext = ctx
			span = nil
		}
	}()
	spanContext, span = telemetry.tracer.Start(ctx, "outbox.publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("outbox.operation", string(outbox.OperationPublish)),
			attribute.String("outbox.retry.state", retryState(attempts)),
		),
	)
	if spanContext == nil {
		spanContext = ctx
	}
	if span == nil {
		spanContext = ctx
	}

	return spanContext, span
}

func (telemetry *Telemetry) extract(ctx context.Context, metadata map[string]string) (extracted context.Context) {
	extracted = ctx
	defer func() {
		if recover() != nil {
			extracted = ctx
		}
	}()
	carrier := propagation.MapCarrier{}
	copyTraceContext(carrier, metadata)
	extracted = telemetry.propagator.Extract(ctx, carrier)
	if extracted == nil {
		return ctx
	}
	spanContext := trace.SpanContextFromContext(extracted)
	if !spanContext.IsValid() {
		return ctx
	}
	extracted = trace.ContextWithRemoteSpanContext(ctx, spanContext)

	return extracted
}

func copyTraceContext(destination, source map[string]string) {
	for _, key := range []string{traceParentKey, traceStateKey} {
		if value, exists := source[key]; exists {
			destination[key] = value
		}
	}
}

func retryState(attempts int) string {
	switch {
	case attempts <= 0:
		return "none"
	case attempts == 1:
		return "first"
	case attempts <= 5:
		return "repeated"
	default:
		return "many"
	}
}

func boundedOperation(operation outbox.Operation) string {
	switch operation {
	case outbox.OperationClaim,
		outbox.OperationPublish,
		outbox.OperationDeliver,
		outbox.OperationRetry,
		outbox.OperationDeadLetter,
		outbox.OperationRelease,
		outbox.OperationExtendLease,
		outbox.OperationReplay,
		outbox.OperationPrune,
		outbox.OperationArchive:
		return string(operation)
	default:
		return "unknown"
	}
}

func boundedOutcome(outcome outbox.Outcome) string {
	switch outcome {
	case outbox.OutcomeSuccess, outbox.OutcomeFailure:
		return string(outcome)
	default:
		return "unknown"
	}
}
