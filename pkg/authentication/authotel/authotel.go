// Package authotel adapts authentication instrumentation to OpenTelemetry.
package authotel

import (
	"context"
	"fmt"
	"reflect"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "github.com/faustbrian/golib/pkg/authentication/authotel"

// Config supplies the standard providers exposed by telemetry runtimes.
type Config struct {
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
}

// Instrumenter emits bounded authentication traces and metrics.
type Instrumenter struct {
	tracer   trace.Tracer
	attempts metric.Int64Counter
	duration metric.Float64Histogram
}

// New creates OpenTelemetry authentication instrumentation.
func New(config Config) (*Instrumenter, error) {
	if isNil(config.TracerProvider) || isNil(config.MeterProvider) {
		return nil, fmt.Errorf("%w: missing OpenTelemetry provider", authentication.ErrInvalidConfiguration)
	}

	meter := config.MeterProvider.Meter(instrumentationName)
	attempts, err := meter.Int64Counter(
		"authentication.attempts",
		metric.WithDescription("Completed authentication attempts"),
		metric.WithUnit("{attempt}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create attempts counter: %w", err)
	}
	duration, err := meter.Float64Histogram(
		"authentication.duration",
		metric.WithDescription("Authentication attempt duration"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create duration histogram: %w", err)
	}

	return &Instrumenter{
		tracer:   config.TracerProvider.Tracer(instrumentationName),
		attempts: attempts,
		duration: duration,
	}, nil
}

// Start implements authentication.Instrumenter.
func (i *Instrumenter) Start(
	ctx context.Context,
	kind authentication.CredentialKind,
) (context.Context, func(authentication.Event)) {
	kindAttribute := attribute.String("authentication.credential.kind", string(kind))
	next, span := i.tracer.Start(
		ctx,
		"authentication.authenticate",
		trace.WithAttributes(kindAttribute),
	)

	return next, func(event authentication.Event) {
		attributes := []attribute.KeyValue{
			kindAttribute,
			attribute.String("authentication.outcome", string(event.Outcome)),
			attribute.String("authentication.failure.kind", string(event.Failure)),
		}
		span.SetAttributes(attributes[1:]...)
		if event.Outcome == authentication.OutcomeFailed {
			span.SetStatus(codes.Error, "authentication failed")
		}
		i.attempts.Add(next, 1, metric.WithAttributes(attributes...))
		i.duration.Record(next, event.Duration.Seconds(), metric.WithAttributes(attributes...))
		span.End()
	}
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

var _ authentication.Instrumenter = (*Instrumenter)(nil)
