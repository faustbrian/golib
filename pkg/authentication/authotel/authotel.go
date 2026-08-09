// Package authotel adapts authentication instrumentation to bounded,
// payload-free OpenTelemetry traces and metrics. Providers and their lifecycle
// remain caller-owned; the package does not use global providers.
package authotel

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "github.com/faustbrian/golib/pkg/authentication/authotel"

// Config supplies caller-owned providers. Both providers are required.
type Config struct {
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
}

// Instrumenter emits bounded authentication traces and metrics. It is safe for
// concurrent use and starts no goroutines.
type Instrumenter struct {
	tracer   trace.Tracer
	attempts metric.Int64Counter
	duration metric.Float64Histogram
}

// New creates OpenTelemetry authentication instrumentation. It creates but
// does not own, flush, or shut down instruments or providers.
func New(config Config) (instrumenter *Instrumenter, err error) {
	defer func() {
		if recover() != nil {
			instrumenter = nil
			err = fmt.Errorf("%w: OpenTelemetry provider failure", authentication.ErrInvalidConfiguration)
		}
	}()
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
		return nil, fmt.Errorf("%w: attempts instrument failure", authentication.ErrInvalidConfiguration)
	}
	duration, err := meter.Float64Histogram(
		"authentication.duration",
		metric.WithDescription("Authentication attempt duration"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: duration instrument failure", authentication.ErrInvalidConfiguration)
	}

	return &Instrumenter{
		tracer:   config.TracerProvider.Tracer(instrumentationName),
		attempts: attempts,
		duration: duration,
	}, nil
}

// Start implements authentication.Instrumenter. The returned completion
// callback records only its first invocation and is safe for concurrent use.
func (i *Instrumenter) Start(
	ctx context.Context,
	kind authentication.CredentialKind,
) (next context.Context, finish func(authentication.Event)) {
	next = ctx
	defer func() {
		if recover() != nil {
			next = ctx
			finish = noopFinish
		}
	}()
	kindAttribute := attribute.String("authentication.credential.kind", credentialKind(kind))
	next, span := i.tracer.Start(
		ctx,
		"authentication.authenticate",
		trace.WithAttributes(kindAttribute),
	)

	var completed atomic.Bool
	return next, func(event authentication.Event) {
		if !completed.CompareAndSwap(false, true) {
			return
		}
		outcome := outcome(event.Outcome)
		attributes := []attribute.KeyValue{
			kindAttribute,
			attribute.String("authentication.outcome", outcome),
			attribute.String("authentication.failure.kind", failureKind(event.Outcome, event.Failure)),
		}
		setAttributes(span, attributes[1:])
		if outcome == string(authentication.OutcomeFailed) {
			setErrorStatus(span)
		}
		addAttempt(i.attempts, next, attributes)
		recordDuration(i.duration, next, max(event.Duration.Seconds(), 0), attributes)
		endSpan(span)
	}
}

func noopFinish(authentication.Event) {}

func setAttributes(span trace.Span, attributes []attribute.KeyValue) {
	defer func() { _ = recover() }()
	span.SetAttributes(attributes...)
}

func setErrorStatus(span trace.Span) {
	defer func() { _ = recover() }()
	span.SetStatus(codes.Error, "authentication failed")
}

func addAttempt(counter metric.Int64Counter, ctx context.Context, attributes []attribute.KeyValue) {
	defer func() { _ = recover() }()
	counter.Add(ctx, 1, metric.WithAttributes(attributes...))
}

func recordDuration(
	histogram metric.Float64Histogram,
	ctx context.Context,
	duration float64,
	attributes []attribute.KeyValue,
) {
	defer func() { _ = recover() }()
	histogram.Record(ctx, duration, metric.WithAttributes(attributes...))
}

func endSpan(span trace.Span) {
	defer func() { _ = recover() }()
	span.End()
}

func credentialKind(kind authentication.CredentialKind) string {
	switch kind {
	case authentication.CredentialBasic,
		authentication.CredentialBearer,
		authentication.CredentialAPIKey:
		return string(kind)
	default:
		return "unknown"
	}
}

func outcome(value authentication.Outcome) string {
	switch value {
	case authentication.OutcomeAuthenticated,
		authentication.OutcomeAnonymous,
		authentication.OutcomeFailed:
		return string(value)
	default:
		return "unknown"
	}
}

func failureKind(outcome authentication.Outcome, failure authentication.FailureKind) string {
	if outcome == authentication.OutcomeAuthenticated || outcome == authentication.OutcomeAnonymous {
		return "none"
	}
	if outcome != authentication.OutcomeFailed {
		return "unknown"
	}
	switch failure {
	case authentication.FailureAbsent,
		authentication.FailureInvalid,
		authentication.FailureRejected,
		authentication.FailureUnavailable,
		authentication.FailureAmbiguous:
		return string(failure)
	default:
		return "unknown"
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
