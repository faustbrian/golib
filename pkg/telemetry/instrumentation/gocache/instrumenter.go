// Package gocache provides dependency-neutral instrumentation for cache.
// It intentionally accepts no cache keys or values.
package gocache

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

const scopeName = "github.com/faustbrian/golib/pkg/telemetry/instrumentation/gocache"

// Operation is a bounded cache operation label.
type Operation string

const (
	// OperationGet represents a cache lookup.
	OperationGet Operation = "get"
	// OperationSet represents a cache write.
	OperationSet Operation = "set"
	// OperationDelete represents cache invalidation.
	OperationDelete Operation = "delete"
	// OperationLoad represents cache-aside loading.
	OperationLoad Operation = "load"
	// OperationOther collapses unknown operations.
	OperationOther Operation = "other"
)

// Outcome is a bounded cache result label.
type Outcome string

const (
	// OutcomeSuccess represents a successful mutation.
	OutcomeSuccess Outcome = "success"
	// OutcomeHit represents a fresh cache hit.
	OutcomeHit Outcome = "hit"
	// OutcomeMiss represents a cache miss.
	OutcomeMiss Outcome = "miss"
	// OutcomeStale represents a stale cache hit.
	OutcomeStale Outcome = "stale"
	// OutcomeError represents a failed cache operation.
	OutcomeError Outcome = "error"
	// OutcomeOther collapses unknown outcomes.
	OutcomeOther Outcome = "other"
)

// Config selects standard OpenTelemetry providers.
type Config struct {
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
}

// EndFunc completes one cache observation. It is safe to call concurrently
// and records at most once. Raw errors are used only as a boolean signal.
type EndFunc func(Outcome, error)

// Instrumenter creates bounded cache spans and metrics.
type Instrumenter struct {
	tracer     trace.Tracer
	duration   metric.Float64Histogram
	operations metric.Int64Counter
}

// New constructs a dependency-neutral cache instrumenter.
func New(config Config) (*Instrumenter, error) {
	if config.TracerProvider == nil {
		config.TracerProvider = tracenoop.NewTracerProvider()
	}
	if config.MeterProvider == nil {
		config.MeterProvider = metricnoop.NewMeterProvider()
	}
	meter := config.MeterProvider.Meter(scopeName)
	duration, err := meter.Float64Histogram("cache.operation.duration", metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	operations, err := meter.Int64Counter("cache.operation.count", metric.WithUnit("{operation}"))
	if err != nil {
		return nil, err
	}
	return &Instrumenter{
		tracer:     config.TracerProvider.Tracer(scopeName),
		duration:   duration,
		operations: operations,
	}, nil
}

// Start begins one cache observation without accepting a key or value.
func (instrumenter *Instrumenter) Start(ctx context.Context, operation Operation) (context.Context, EndFunc) {
	operation = normalizeOperation(operation)
	attributes := []attribute.KeyValue{attribute.String("cache.operation.name", string(operation))}
	ctx, span := instrumenter.tracer.Start(
		ctx,
		"cache."+string(operation),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attributes...),
	)
	started := time.Now()
	var once sync.Once
	return ctx, func(outcome Outcome, operationErr error) {
		once.Do(func() {
			if operationErr != nil {
				outcome = OutcomeError
				span.SetStatus(codes.Error, "cache operation failed")
			} else {
				outcome = normalizeOutcome(outcome)
			}
			resultAttributes := append(
				append([]attribute.KeyValue(nil), attributes...),
				attribute.String("cache.result", string(outcome)),
			)
			span.SetAttributes(attribute.String("cache.result", string(outcome)))
			instrumenter.duration.Record(ctx, time.Since(started).Seconds(), metric.WithAttributes(resultAttributes...))
			instrumenter.operations.Add(ctx, 1, metric.WithAttributes(resultAttributes...))
			span.End()
		})
	}
}

func normalizeOperation(operation Operation) Operation {
	switch operation {
	case OperationGet, OperationSet, OperationDelete, OperationLoad:
		return operation
	default:
		return OperationOther
	}
}

func normalizeOutcome(outcome Outcome) Outcome {
	switch outcome {
	case OutcomeSuccess, OutcomeHit, OutcomeMiss, OutcomeStale, OutcomeError:
		return outcome
	default:
		return OutcomeOther
	}
}
