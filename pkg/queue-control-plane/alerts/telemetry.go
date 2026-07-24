package alerts

import (
	"context"
	"errors"
	"fmt"
	"math"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	// MaxAlertBatch bounds one synchronous telemetry export call.
	MaxAlertBatch = 12_000
)

var (
	// ErrInvalidTelemetryConfiguration reports an unusable metric provider.
	ErrInvalidTelemetryConfiguration = errors.New("alerts: invalid telemetry configuration")
	// ErrInvalidAlertBatch reports malformed, empty, canceled, or unbounded export input.
	ErrInvalidAlertBatch = errors.New("alerts: invalid alert batch")
)

// TelemetryExporter emits fixed-cardinality alert counts through the caller's
// telemetry meter. Tenant and resource identities are intentionally absent.
type TelemetryExporter struct {
	alerts metric.Int64Counter
}

// NewTelemetryExporter creates the bounded platform alert metric bridge.
func NewTelemetryExporter(meter metric.Meter) (*TelemetryExporter, error) {
	if meter == nil {
		return nil, ErrInvalidTelemetryConfiguration
	}
	counter, err := meter.Int64Counter(
		"queue.control.alert.count",
		metric.WithUnit("{alert}"),
	)
	if err != nil {
		return nil, fmt.Errorf("alerts: create alert counter: %w", err)
	}

	return &TelemetryExporter{alerts: counter}, nil
}

// Export validates one bounded evaluation result and increments only stable
// kind labels. Delivery and scheduling remain owned by the platform caller.
func (e *TelemetryExporter) Export(ctx context.Context, alerts []Alert) error {
	if e == nil || ctx == nil || ctx.Err() != nil || len(alerts) == 0 ||
		len(alerts) > MaxAlertBatch {
		return ErrInvalidAlertBatch
	}
	for _, alert := range alerts {
		if !validAlert(alert) {
			return ErrInvalidAlertBatch
		}
	}
	for _, alert := range alerts {
		e.alerts.Add(
			ctx,
			1,
			metric.WithAttributes(attribute.String("kind", string(alert.Kind))),
		)
	}

	return nil
}

func validAlert(alert Alert) bool {
	if invalidIdentity(alert.TenantID) || invalidIdentity(alert.Resource) ||
		alert.ObservedAt.IsZero() || math.IsNaN(alert.Value) || math.IsInf(alert.Value, 0) ||
		math.IsNaN(alert.Threshold) || math.IsInf(alert.Threshold, 0) ||
		alert.Value < 0 || alert.Threshold < 0 {
		return false
	}
	switch alert.Kind {
	case KindQueueWait, KindFailureCount, KindStaleWorker,
		KindDeadLetterCount, KindCommandFailure:
		return true
	default:
		return false
	}
}
