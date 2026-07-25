// Package authotel records bounded authorization metrics and traces through
// standard OpenTelemetry providers, including providers owned by telemetry.
package authotel

import (
	"context"
	"strconv"
	"sync"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

const scopeName = "github.com/faustbrian/golib/pkg/authorization/authotel"

type Config struct {
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
}

type Instrumenter struct {
	tracer    trace.Tracer
	duration  metric.Float64Histogram
	decisions metric.Int64Counter
}

func New(config Config) (*Instrumenter, error) {
	if config.TracerProvider == nil {
		config.TracerProvider = tracenoop.NewTracerProvider()
	}
	if config.MeterProvider == nil {
		config.MeterProvider = metricnoop.NewMeterProvider()
	}
	meter := config.MeterProvider.Meter(scopeName)
	duration, err := meter.Float64Histogram(
		"authorization.decision.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Authorization decision latency"),
	)
	if err != nil {
		return nil, err
	}
	decisions, err := meter.Int64Counter(
		"authorization.decision.count",
		metric.WithUnit("{decision}"),
		metric.WithDescription("Authorization decisions by bounded result"),
	)
	if err != nil {
		return nil, err
	}
	return &Instrumenter{
		tracer:   config.TracerProvider.Tracer(scopeName),
		duration: duration, decisions: decisions,
	}, nil
}

func (instrumenter *Instrumenter) Start(
	ctx context.Context,
) (context.Context, func(authorization.Event)) {
	ctx, span := instrumenter.tracer.Start(
		ctx,
		"authorization.decide",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	var once sync.Once
	return ctx, func(event authorization.Event) {
		once.Do(func() {
			decisionResult := result(event)
			attributes := []attribute.KeyValue{
				attribute.String("authorization.result", decisionResult),
			}
			instrumenter.decisions.Add(ctx, 1, metric.WithAttributes(attributes...))
			instrumenter.duration.Record(
				ctx,
				max(event.Duration.Seconds(), 0),
				metric.WithAttributes(attributes...),
			)
			span.SetAttributes(
				attribute.String("authorization.result", decisionResult),
				attribute.String("authorization.reason", string(event.Reason)),
				attribute.String("authorization.revision", strconv.FormatUint(uint64(event.Revision), 10)),
				attribute.Int("authorization.matched_policy_count", len(event.MatchedPolicyIDs)),
				attribute.Bool("authorization.matched_policy_ids_truncated", event.MatchedPolicyIDsTruncated),
				attribute.Int("authorization.trace_count", event.TraceCount),
				attribute.Bool("authorization.trace_truncated", event.TraceTruncated),
			)
			if event.Failed {
				span.SetStatus(codes.Error, "authorization evaluation failed")
			}
			span.End()
		})
	}
}

func result(event authorization.Event) string {
	if event.Failed {
		return "error"
	}
	switch event.Outcome {
	case authorization.Allow:
		return "allow"
	case authorization.Deny:
		return "deny"
	case authorization.NotApplicable:
		return "not-applicable"
	default:
		return "error"
	}
}

var _ authorization.Instrumenter = (*Instrumenter)(nil)
