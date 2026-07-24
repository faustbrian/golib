// Package goqueue provides dependency-neutral handler instrumentation for
// queue. Message values and raw handler errors are never inspected.
package goqueue

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

const scopeName = "github.com/faustbrian/golib/pkg/telemetry/instrumentation/goqueue"

// Backend is a finite queue backend label.
type Backend string

const (
	// BackendMemory identifies the in-memory queue.
	BackendMemory Backend = "memory"
	// BackendRedis identifies the Redis list queue.
	BackendRedis Backend = "redis"
	// BackendRedisStream identifies Redis Streams.
	BackendRedisStream Backend = "redis_stream"
	// BackendValkeyStream identifies Valkey Streams.
	BackendValkeyStream Backend = "valkey_stream"
	// BackendNATS identifies NATS.
	BackendNATS Backend = "nats"
	// BackendNSQ identifies NSQ.
	BackendNSQ Backend = "nsq"
	// BackendRabbitMQ identifies RabbitMQ.
	BackendRabbitMQ Backend = "rabbitmq"
	// BackendOther collapses an explicitly supported custom backend.
	BackendOther Backend = "other"
)

// Config selects the fixed backend and standard providers.
type Config struct {
	Backend        Backend
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
}

// Instrumenter owns bounded queue handler instruments.
type Instrumenter struct {
	backend  Backend
	tracer   trace.Tracer
	duration metric.Float64Histogram
	jobs     metric.Int64Counter
}

// New constructs a dependency-neutral queue instrumenter.
func New(config Config) (*Instrumenter, error) {
	if !validBackend(config.Backend) {
		return nil, errors.New("queue backend is unsupported")
	}
	if config.TracerProvider == nil {
		config.TracerProvider = tracenoop.NewTracerProvider()
	}
	if config.MeterProvider == nil {
		config.MeterProvider = metricnoop.NewMeterProvider()
	}
	meter := config.MeterProvider.Meter(scopeName)
	duration, err := meter.Float64Histogram("messaging.process.duration", metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	jobs, err := meter.Int64Counter("messaging.process.count", metric.WithUnit("{message}"))
	if err != nil {
		return nil, err
	}
	return &Instrumenter{
		backend:  config.Backend,
		tracer:   config.TracerProvider.Tracer(scopeName),
		duration: duration,
		jobs:     jobs,
	}, nil
}

// WrapHandler instruments any queue-compatible handler signature without
// importing or inspecting its message type.
func WrapHandler[Message any](
	instrumenter *Instrumenter,
	handler func(context.Context, Message) error,
) func(context.Context, Message) error {
	return func(ctx context.Context, message Message) (handlerErr error) {
		attributes := []attribute.KeyValue{
			attribute.String("messaging.system", string(instrumenter.backend)),
			attribute.String("messaging.operation.name", "process"),
		}
		ctx, span := instrumenter.tracer.Start(
			ctx,
			"queue.process",
			trace.WithSpanKind(trace.SpanKindConsumer),
			trace.WithAttributes(attributes...),
		)
		started := time.Now()
		defer func() {
			panicValue := recover()
			outcome := "success"
			if handlerErr != nil || panicValue != nil {
				outcome = "error"
				span.SetStatus(codes.Error, "queue handler failed")
			}
			resultAttributes := append(
				append([]attribute.KeyValue(nil), attributes...),
				attribute.String("error.type", outcome),
			)
			instrumenter.duration.Record(ctx, time.Since(started).Seconds(), metric.WithAttributes(resultAttributes...))
			instrumenter.jobs.Add(ctx, 1, metric.WithAttributes(resultAttributes...))
			span.End()
			if panicValue != nil {
				panic(panicValue)
			}
		}()
		return handler(ctx, message)
	}
}

func validBackend(backend Backend) bool {
	switch backend {
	case BackendMemory, BackendRedis, BackendRedisStream, BackendValkeyStream,
		BackendNATS, BackendNSQ, BackendRabbitMQ, BackendOther:
		return true
	default:
		return false
	}
}
