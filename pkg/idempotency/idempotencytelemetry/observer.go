// Package idempotencytelemetry adapts bounded observations to OpenTelemetry
// metrics. The telemetry runtime exposes the standard MeterProvider used by
// New, so the integration does not require global provider registration.
package idempotencytelemetry

import (
	"context"
	"errors"
	"fmt"

	"github.com/faustbrian/golib/pkg/idempotency"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const instrumentationScope = "github.com/faustbrian/golib/pkg/idempotency"

// ErrNilMeterProvider reports an unusable telemetry configuration.
var ErrNilMeterProvider = errors.New("idempotencytelemetry: nil meter provider")

// Observer counts semantic transitions using bounded attributes.
type Observer struct {
	transitions metric.Int64Counter
}

// New constructs an observer from a standard OpenTelemetry meter provider.
// Pass telemetry Runtime.MeterProvider() to bind an application runtime.
func New(provider metric.MeterProvider) (*Observer, error) {
	if provider == nil {
		return nil, ErrNilMeterProvider
	}

	transitions, err := provider.Meter(instrumentationScope).Int64Counter(
		"idempotency.transitions",
		metric.WithDescription("Idempotency semantic service transitions"),
		metric.WithUnit("{transition}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create transition counter: %w", err)
	}

	return &Observer{transitions: transitions}, nil
}

// Observe increments the transition counter. Correlation is deliberately
// excluded because keyed digests remain high-cardinality metric attributes.
func (observer *Observer) Observe(ctx context.Context, event idempotency.Observation) {
	observer.transitions.Add(ctx, 1, metric.WithAttributes(
		attribute.String("transition", string(event.Transition)),
		attribute.String("outcome", string(event.Outcome)),
		attribute.String("reason", string(event.Reason)),
		attribute.Bool("durable", event.Durable),
	))
}

var _ idempotency.Observer = (*Observer)(nil)
