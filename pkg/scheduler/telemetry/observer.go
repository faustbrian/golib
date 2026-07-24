// Package telemetry records scheduler lifecycle logs, metrics, and traces.
package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	gotelemetry "github.com/faustbrian/golib/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const scopeName = "github.com/faustbrian/golib/pkg/scheduler"

// ErrInvalidConfiguration reports missing telemetry dependencies.
var ErrInvalidConfiguration = errors.New("scheduler telemetry: logger and providers are required")

// Config supplies structured logging and OpenTelemetry providers.
type Config struct {
	Logger         *slog.Logger
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
}

type activeSpan struct {
	span    trace.Span
	started time.Time
}

// Observer records lifecycle logs, metrics, and execution spans.
type Observer struct {
	logger   *slog.Logger
	tracer   trace.Tracer
	events   metric.Int64Counter
	duration metric.Float64Histogram
	mu       sync.Mutex
	active   map[string]activeSpan
}

// New constructs a lifecycle telemetry observer.
func New(config Config) (*Observer, error) {
	if config.Logger == nil || config.TracerProvider == nil || config.MeterProvider == nil {
		return nil, ErrInvalidConfiguration
	}
	meter := config.MeterProvider.Meter(scopeName)
	events, err := meter.Int64Counter("scheduler.events")
	if err != nil {
		return nil, err
	}
	duration, err := meter.Float64Histogram("scheduler.execution.duration", metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	return &Observer{
		logger: config.Logger, tracer: config.TracerProvider.Tracer(scopeName),
		events: events, duration: duration, active: make(map[string]activeSpan),
	}, nil
}

// NewRuntime constructs an observer from an initialized telemetry runtime.
func NewRuntime(runtime *gotelemetry.Runtime, logger *slog.Logger) (*Observer, error) {
	if runtime == nil {
		return nil, ErrInvalidConfiguration
	}
	return New(Config{
		Logger: logger, TracerProvider: runtime.TracerProvider(), MeterProvider: runtime.MeterProvider(),
	})
}

// Observe records one scheduler lifecycle event.
func (observer *Observer) Observe(event scheduler.Event) {
	ctx := event.Context
	if ctx == nil {
		ctx = context.Background()
	}
	attributes := []attribute.KeyValue{
		attribute.String("scheduler.event", event.Type.String()),
		attribute.String("scheduler.result", event.Result.String()),
		attribute.String("scheduler.schedule", event.Occurrence.ScheduleName),
		attribute.String("scheduler.task", event.Occurrence.Task),
	}
	observer.events.Add(ctx, 1, metric.WithAttributes(attributes...))
	observer.logger.LogAttrs(
		ctx,
		logLevel(event),
		"scheduler lifecycle",
		slog.String("event", event.Type.String()),
		slog.String("result", event.Result.String()),
		slog.String("schedule", event.Occurrence.ScheduleName),
		slog.String("task", event.Occurrence.Task),
		slog.String("owner", event.Owner),
		slog.Uint64("fencing_token", event.Fencing),
		slog.Any("error", event.Err),
	)

	key := event.Occurrence.IdempotencyKey
	switch event.Type {
	case scheduler.EventBefore:
		spanCtx, span := observer.tracer.Start(ctx, "scheduler.execute", trace.WithAttributes(attributes...))
		_ = spanCtx
		observer.mu.Lock()
		if previous, ok := observer.active[key]; ok {
			previous.span.SetStatus(codes.Error, "superseded lifecycle")
			previous.span.End()
		}
		observer.active[key] = activeSpan{span: span, started: time.Now()}
		observer.mu.Unlock()
	case scheduler.EventFailure:
		observer.withSpan(key, func(span activeSpan) {
			if event.Err != nil {
				span.span.RecordError(event.Err)
			}
			span.span.SetStatus(codes.Error, "execution failed")
		})
	case scheduler.EventCompleted:
		observer.mu.Lock()
		span, ok := observer.active[key]
		delete(observer.active, key)
		observer.mu.Unlock()
		if ok {
			if event.Result == scheduler.ResultFailed {
				span.span.SetStatus(codes.Error, "execution failed")
			} else {
				span.span.SetStatus(codes.Ok, event.Result.String())
			}
			span.span.End()
			observer.duration.Record(ctx, time.Since(span.started).Seconds(), metric.WithAttributes(attributes...))
		}
	case scheduler.EventSuccess, scheduler.EventSkipped, scheduler.EventOverlap:
	}
}

func (observer *Observer) withSpan(key string, callback func(activeSpan)) {
	observer.mu.Lock()
	span, ok := observer.active[key]
	observer.mu.Unlock()
	if ok {
		callback(span)
	}
}

func logLevel(event scheduler.Event) slog.Level {
	if event.Err != nil && event.Result == scheduler.ResultFailed {
		return slog.LevelError
	}
	return slog.LevelInfo
}
