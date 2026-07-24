package otel

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	cache "github.com/faustbrian/golib/pkg/cache"
)

// Observer exports semantic cache events as OpenTelemetry metrics.
type Observer struct {
	operations metric.Int64Counter
	duration   metric.Float64Histogram
	valueSize  metric.Int64Histogram
	memorySize metric.Int64Gauge
}

// New constructs all instruments required by an Observer.
func New(meter metric.Meter) (*Observer, error) {
	if meter == nil {
		return nil, fmt.Errorf("OpenTelemetry observer requires a meter")
	}
	operations, err := meter.Int64Counter(
		"cache.operations",
		metric.WithUnit("{operation}"),
		metric.WithDescription("Cache operations by semantic outcome"),
	)
	if err != nil {
		return nil, fmt.Errorf("create operations counter: %w", err)
	}
	duration, err := meter.Float64Histogram(
		"cache.operation.duration",
		metric.WithUnit("ms"),
		metric.WithDescription("Cache operation latency"),
	)
	if err != nil {
		return nil, fmt.Errorf("create duration histogram: %w", err)
	}
	valueSize, err := meter.Int64Histogram(
		"cache.value.size",
		metric.WithUnit("By"),
		metric.WithDescription("Encoded cache value size"),
	)
	if err != nil {
		return nil, fmt.Errorf("create value size histogram: %w", err)
	}
	memorySize, err := meter.Int64Gauge(
		"cache.memory.size",
		metric.WithUnit("By"),
		metric.WithDescription("Retained memory backend size after eviction or expiration"),
	)
	if err != nil {
		return nil, fmt.Errorf("create memory size gauge: %w", err)
	}
	return &Observer{
		operations: operations,
		duration:   duration,
		valueSize:  valueSize,
		memorySize: memorySize,
	}, nil
}

// Observe records one validated, low-cardinality cache event.
func (o *Observer) Observe(ctx context.Context, event cache.Event) error {
	if !validOperation(event.Operation) || !validOutcome(event.Outcome) {
		return fmt.Errorf("invalid cache telemetry event")
	}
	attributes := metric.WithAttributes(
		attribute.String("cache.operation", string(event.Operation)),
		attribute.String("cache.outcome", string(event.Outcome)),
	)
	o.operations.Add(ctx, 1, attributes)
	o.duration.Record(ctx, float64(event.Duration.Microseconds())/1000, attributes)
	if event.Operation == cache.OperationGet || event.Operation == cache.OperationSet {
		o.valueSize.Record(ctx, int64(event.Size), attributes)
	}
	if event.Operation == cache.OperationEvict || event.Operation == cache.OperationExpire {
		o.memorySize.Record(ctx, int64(event.Size), attributes)
	}
	return nil
}

func validOperation(operation cache.Operation) bool {
	switch operation {
	case cache.OperationGet, cache.OperationSet, cache.OperationDelete,
		cache.OperationLoad, cache.OperationEvict, cache.OperationExpire:
		return true
	default:
		return false
	}
}

func validOutcome(outcome cache.Outcome) bool {
	switch outcome {
	case cache.OutcomeSuccess, cache.OutcomeHit, cache.OutcomeMiss,
		cache.OutcomeStale, cache.OutcomeNegative, cache.OutcomeRejected,
		cache.OutcomeError, cache.OutcomeEvicted, cache.OutcomeExpired:
		return true
	default:
		return false
	}
}
