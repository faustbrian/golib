package fleet

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// ErrInvalidTelemetryConfiguration reports unusable fleet metric settings.
var ErrInvalidTelemetryConfiguration = errors.New("fleet: invalid telemetry configuration")

// RegisterTelemetry exports fixed-cardinality fleet state and rejection
// measurements. The caller owns the returned registration lifecycle.
func (r *Registry) RegisterTelemetry(
	meter metric.Meter,
	now func() time.Time,
	staleAfter time.Duration,
) (metric.Registration, error) {
	if r == nil || meter == nil || now == nil || staleAfter <= 0 {
		return nil, ErrInvalidTelemetryConfiguration
	}
	workers, err := meter.Int64ObservableGauge(
		"queue.control.fleet.worker.count",
		metric.WithUnit("{worker}"),
	)
	if err != nil {
		return nil, fmt.Errorf("fleet: create worker gauge: %w", err)
	}
	rejections, err := meter.Int64ObservableCounter(
		"queue.control.fleet.heartbeat.rejected",
		metric.WithUnit("{heartbeat}"),
	)
	if err != nil {
		return nil, fmt.Errorf("fleet: create rejection counter: %w", err)
	}
	return meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		snapshot := r.Snapshot(now(), staleAfter)
		counts := map[State]int64{}
		for _, worker := range snapshot.Workers {
			counts[worker.State]++
		}
		for _, state := range []State{
			StateRunning, StatePaused, StateDraining, StateStopped,
			StateStale, StateUnknown,
		} {
			observer.ObserveInt64(
				workers,
				counts[state],
				metric.WithAttributes(attribute.String("state", string(state))),
			)
		}
		observer.ObserveInt64(rejections, boundedMetricCount(snapshot.Rejected))

		return nil
	}, workers, rejections)
}

func boundedMetricCount(value uint64) int64 {
	if value > math.MaxInt64 {
		return math.MaxInt64
	}

	return int64(value)
}
