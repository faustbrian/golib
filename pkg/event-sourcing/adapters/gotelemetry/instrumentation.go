// Package gotelemetry instruments event-sourcing boundaries with standard
// OpenTelemetry providers without recording event data or application errors.
package gotelemetry

import (
	"context"
	"errors"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "github.com/faustbrian/golib/pkg/event-sourcing"

var (
	// ErrRuntimeRequired reports a missing or incomplete telemetry runtime.
	ErrRuntimeRequired = errors.New(
		"event-sourcing/gotelemetry: runtime is required",
	)
	// ErrDispatcherRequired reports a missing downstream dispatcher.
	ErrDispatcherRequired = errors.New(
		"event-sourcing/gotelemetry: dispatcher is required",
	)
	// ErrConsumerRequired reports a missing downstream consumer.
	ErrConsumerRequired = errors.New(
		"event-sourcing/gotelemetry: consumer is required",
	)
	// ErrContextRequired reports a nil operation context.
	ErrContextRequired = errors.New(
		"event-sourcing/gotelemetry: context is required",
	)
	// ErrInstrumentCreation categorizes OpenTelemetry instrument construction
	// failures without exposing provider diagnostics.
	ErrInstrumentCreation = errors.New(
		"event-sourcing/gotelemetry: instrument creation failed",
	)
)

// Runtime is the standard-provider surface implemented by telemetry.Runtime.
// Keeping the interface here prevents telemetry from entering the core module.
type Runtime interface {
	TracerProvider() trace.TracerProvider
	MeterProvider() metric.MeterProvider
	Propagator() propagation.TextMapPropagator
}

// InstrumentError preserves an OpenTelemetry provider error while presenting
// a stable redacted diagnostic.
type InstrumentError struct {
	cause error
}

// Error implements error without exposing provider diagnostics.
func (*InstrumentError) Error() string {
	return ErrInstrumentCreation.Error()
}

// Unwrap preserves the stable category and provider cause.
func (err *InstrumentError) Unwrap() []error {
	return []error{ErrInstrumentCreation, err.cause}
}

// Instrumentation owns immutable event-sourcing tracing and metric
// instruments. It starts no goroutines.
type Instrumentation struct {
	propagator         propagation.TextMapPropagator
	tracer             trace.Tracer
	operations         metric.Int64Counter
	duration           metric.Float64Histogram
	deliveries         metric.Int64Counter
	projectionMessages metric.Int64Counter
	projectionLag      metric.Int64Histogram
}

// New constructs instrumentation from explicit standard providers.
func New(runtime Runtime) (*Instrumentation, error) {
	if runtime == nil {
		return nil, ErrRuntimeRequired
	}
	tracerProvider := runtime.TracerProvider()
	meterProvider := runtime.MeterProvider()
	propagator := runtime.Propagator()
	if tracerProvider == nil || meterProvider == nil || propagator == nil {
		return nil, ErrRuntimeRequired
	}

	meter := meterProvider.Meter(instrumentationName)
	operations, err := meter.Int64Counter(
		"event_sourcing.operations",
		metric.WithDescription("Completed event-sourcing operations"),
		metric.WithUnit("{operation}"),
	)
	if err != nil {
		return nil, instrumentFailure(err)
	}
	duration, err := meter.Float64Histogram(
		"event_sourcing.operation.duration",
		metric.WithDescription("Event-sourcing operation latency"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, instrumentFailure(err)
	}
	deliveries, err := meter.Int64Counter(
		"event_sourcing.deliveries",
		metric.WithDescription(
			"Submitted event deliveries by mode and operation outcome",
		),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return nil, instrumentFailure(err)
	}
	projectionMessages, err := meter.Int64Counter(
		"event_sourcing.projection.messages",
		metric.WithDescription(
			"Projection messages observed by bounded replay results",
		),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return nil, instrumentFailure(err)
	}
	projectionLag, err := meter.Int64Histogram(
		"event_sourcing.projection.lag",
		metric.WithDescription(
			"Caller-observed projection distance from a durable high watermark",
		),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return nil, instrumentFailure(err)
	}

	return &Instrumentation{
		propagator:         propagator,
		tracer:             tracerProvider.Tracer(instrumentationName),
		operations:         operations,
		duration:           duration,
		deliveries:         deliveries,
		projectionMessages: projectionMessages,
		projectionLag:      projectionLag,
	}, nil
}

// WrapDispatcher instruments one synchronous dispatcher without changing its
// ordering, error, cancellation, reentrancy, or panic behavior.
func (instrumentation *Instrumentation) WrapDispatcher(
	next eventsourcing.Dispatcher,
) (eventsourcing.Dispatcher, error) {
	if !instrumentation.valid() {
		return nil, ErrRuntimeRequired
	}
	if next == nil {
		return nil, ErrDispatcherRequired
	}

	return dispatcher{instrumentation: instrumentation, next: next}, nil
}

// WrapConsumer instruments one synchronous consumer function without
// recording message identity, payload, metadata, errors, or panic values.
func (instrumentation *Instrumentation) WrapConsumer(
	next eventsourcing.ConsumerFunc,
) (eventsourcing.ConsumerFunc, error) {
	if !instrumentation.valid() {
		return nil, ErrRuntimeRequired
	}
	if next == nil {
		return nil, ErrConsumerRequired
	}

	return consumer{instrumentation: instrumentation, next: next}.consume, nil
}

func (instrumentation *Instrumentation) valid() bool {
	return instrumentation != nil &&
		instrumentation.propagator != nil &&
		instrumentation.tracer != nil &&
		instrumentation.operations != nil &&
		instrumentation.duration != nil &&
		instrumentation.deliveries != nil &&
		instrumentation.projectionMessages != nil &&
		instrumentation.projectionLag != nil
}

type dispatcher struct {
	instrumentation *Instrumentation
	next            eventsourcing.Dispatcher
}

func (dispatcher dispatcher) Dispatch(
	ctx context.Context,
	deliveries []eventsourcing.Delivery,
) (operationErr error) {
	if ctx == nil {
		return ErrContextRequired
	}
	mode := deliveryMode(deliveries)
	ctx, span := dispatcher.instrumentation.tracer.Start(
		ctx,
		"event_sourcing.dispatch",
		trace.WithAttributes(
			attribute.String("event_sourcing.delivery.mode", mode),
			attribute.Int("event_sourcing.delivery.count", len(deliveries)),
		),
	)
	started := time.Now()
	defer func() {
		panicValue := recover()
		outcome := operationOutcome(operationErr, panicValue)
		if outcome != "success" {
			span.SetStatus(codes.Error, "event-sourcing dispatch failed")
		}
		dispatcher.instrumentation.recordOperation(
			ctx,
			"dispatch",
			outcome,
			time.Since(started),
		)
		dispatcher.instrumentation.recordDeliveries(
			ctx,
			deliveries,
			outcome,
		)
		span.End()
		if panicValue != nil {
			panic(panicValue)
		}
	}()

	return dispatcher.next.Dispatch(ctx, deliveries)
}

type consumer struct {
	instrumentation *Instrumentation
	next            eventsourcing.ConsumerFunc
}

func (consumer consumer) consume(
	ctx context.Context,
	delivery eventsourcing.Delivery,
) (operationErr error) {
	if ctx == nil {
		return ErrContextRequired
	}
	mode := delivery.Mode().String()
	ctx, span := consumer.instrumentation.tracer.Start(
		ctx,
		"event_sourcing.consume",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("event_sourcing.delivery.mode", mode),
		),
	)
	started := time.Now()
	defer func() {
		panicValue := recover()
		outcome := operationOutcome(operationErr, panicValue)
		if outcome != "success" {
			span.SetStatus(codes.Error, "event-sourcing consumer failed")
		}
		consumer.instrumentation.recordOperation(
			ctx,
			"consume",
			outcome,
			time.Since(started),
		)
		consumer.instrumentation.recordDeliveries(
			ctx,
			[]eventsourcing.Delivery{delivery},
			outcome,
		)
		span.End()
		if panicValue != nil {
			panic(panicValue)
		}
	}()

	return consumer.next(ctx, delivery)
}

func (instrumentation *Instrumentation) recordOperation(
	ctx context.Context,
	operation string,
	outcome string,
	duration time.Duration,
) {
	attributes := metric.WithAttributes(
		attribute.String("event_sourcing.operation", operation),
		attribute.String("event_sourcing.outcome", outcome),
	)
	instrumentation.operations.Add(ctx, 1, attributes)
	instrumentation.duration.Record(ctx, duration.Seconds(), attributes)
}

func (instrumentation *Instrumentation) recordDeliveries(
	ctx context.Context,
	deliveries []eventsourcing.Delivery,
	outcome string,
) {
	var live, replay, unknown int64
	for _, delivery := range deliveries {
		switch delivery.Mode() {
		case eventsourcing.DeliveryLive:
			live++
		case eventsourcing.DeliveryReplay:
			replay++
		default:
			unknown++
		}
	}
	instrumentation.recordDeliveryCount(ctx, "live", outcome, live)
	instrumentation.recordDeliveryCount(ctx, "replay", outcome, replay)
	instrumentation.recordDeliveryCount(ctx, "unknown", outcome, unknown)
}

func (instrumentation *Instrumentation) recordDeliveryCount(
	ctx context.Context,
	mode string,
	outcome string,
	count int64,
) {
	if count == 0 {
		return
	}
	instrumentation.deliveries.Add(
		ctx,
		count,
		metric.WithAttributes(
			attribute.String("event_sourcing.delivery.mode", mode),
			attribute.String("event_sourcing.outcome", outcome),
		),
	)
}

func deliveryMode(deliveries []eventsourcing.Delivery) string {
	if len(deliveries) == 0 {
		return "empty"
	}
	mode := deliveries[0].Mode().String()
	for _, delivery := range deliveries[1:] {
		if delivery.Mode().String() != mode {
			return "mixed"
		}
	}

	return mode
}

func operationOutcome(operationErr error, panicValue any) string {
	if panicValue != nil {
		return "panic"
	}
	if operationErr != nil {
		return "error"
	}

	return "success"
}

func instrumentFailure(cause error) error {
	return &InstrumentError{cause: cause}
}
