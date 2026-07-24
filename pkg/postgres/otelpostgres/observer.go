// Package otelpostgres adapts postgres observations to standard
// OpenTelemetry metrics without recording SQL, arguments, DSNs, or raw errors.
package otelpostgres

import (
	"context"

	postgres "github.com/faustbrian/golib/pkg/postgres"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
)

const scopeName = "github.com/faustbrian/golib/pkg/postgres/otelpostgres"

// Config selects the standard OpenTelemetry meter provider.
type Config struct {
	MeterProvider metric.MeterProvider
}

// Observer records bounded lifecycle and transaction metrics.
type Observer struct {
	duration    metric.Float64Histogram
	operations  metric.Int64Counter
	connections metric.Int64Gauge
}

// New constructs an OpenTelemetry observer with standard database metric
// names and a no-op provider when none is supplied.
func New(config Config) (*Observer, error) {
	if config.MeterProvider == nil {
		config.MeterProvider = metricnoop.NewMeterProvider()
	}

	meter := config.MeterProvider.Meter(scopeName)
	duration, err := meter.Float64Histogram(
		"db.client.operation.duration",
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}
	operations, err := meter.Int64Counter(
		"db.client.operation.count",
		metric.WithUnit("{operation}"),
	)
	if err != nil {
		return nil, err
	}
	connections, err := meter.Int64Gauge(
		"db.client.connection.count",
		metric.WithUnit("{connection}"),
	)
	if err != nil {
		return nil, err
	}

	return &Observer{
		duration:    duration,
		operations:  operations,
		connections: connections,
	}, nil
}

// Observe implements postgres.Observer.
func (o *Observer) Observe(ctx context.Context, observation postgres.Observation) {
	attributes := []attribute.KeyValue{
		attribute.String("db.system.name", "postgresql"),
		attribute.String("db.operation.name", string(observation.Operation)),
		attribute.String("error.type", string(observation.ErrorKind)),
		attribute.String("operation.outcome", string(observation.Outcome)),
	}
	if observation.SQLState != "" {
		attributes = append(
			attributes,
			attribute.String("db.response.status_code", observation.SQLState),
		)
	}
	recordOptions := metric.WithAttributes(attributes...)
	o.duration.Record(ctx, observation.Duration.Seconds(), recordOptions)
	o.operations.Add(ctx, 1, recordOptions)

	if !observation.HasPoolStats {
		return
	}
	o.connections.Record(ctx, int64(observation.Pool.AcquiredConns), metric.WithAttributes(
		attribute.String("pool.state", "acquired"),
	))
	o.connections.Record(ctx, int64(observation.Pool.IdleConns), metric.WithAttributes(
		attribute.String("pool.state", "idle"),
	))
	o.connections.Record(ctx, int64(observation.Pool.TotalConns), metric.WithAttributes(
		attribute.String("pool.state", "total"),
	))
	o.connections.Record(ctx, int64(observation.Pool.MaxConns), metric.WithAttributes(
		attribute.String("pool.state", "max"),
	))
}

var _ postgres.Observer = (*Observer)(nil)
