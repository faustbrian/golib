package fleet

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestRegistryTelemetryRejectsInvalidConfigurationAndInstruments(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(1)
	now := func() time.Time { return time.Unix(1, 0) }
	if registration, err := registry.RegisterTelemetry(nil, now, time.Second); registration != nil || !errors.Is(err, ErrInvalidTelemetryConfiguration) {
		t.Fatalf("RegisterTelemetry(nil) = (%v, %v)", registration, err)
	}
	if registration, err := registry.RegisterTelemetry(metricnoop.NewMeterProvider().Meter("test"), nil, time.Second); registration != nil || !errors.Is(err, ErrInvalidTelemetryConfiguration) {
		t.Fatalf("RegisterTelemetry(nil clock) = (%v, %v)", registration, err)
	}
	if registration, err := registry.RegisterTelemetry(metricnoop.NewMeterProvider().Meter("test"), now, 0); registration != nil || !errors.Is(err, ErrInvalidTelemetryConfiguration) {
		t.Fatalf("RegisterTelemetry(zero stale) = (%v, %v)", registration, err)
	}
	var nilRegistry *Registry
	if registration, err := nilRegistry.RegisterTelemetry(metricnoop.NewMeterProvider().Meter("test"), now, time.Second); registration != nil || !errors.Is(err, ErrInvalidTelemetryConfiguration) {
		t.Fatalf("nil RegisterTelemetry() = (%v, %v)", registration, err)
	}

	instrumentErr := errors.New("instrument unavailable")
	base := metricnoop.NewMeterProvider().Meter("test")
	for _, meter := range []otelmetric.Meter{
		failingFleetMeter{Meter: base, gaugeErr: instrumentErr},
		failingFleetMeter{Meter: base, counterErr: instrumentErr},
		failingFleetMeter{Meter: base, registerErr: instrumentErr},
	} {
		registration, err := registry.RegisterTelemetry(meter, now, time.Second)
		if registration != nil || !errors.Is(err, instrumentErr) {
			t.Fatalf("RegisterTelemetry(failing) = (%v, %v)", registration, err)
		}
	}
}

func TestRegistryTelemetryReportsBoundedFleetStates(t *testing.T) {
	t.Parallel()

	now := time.Unix(100, 0)
	registry := NewRegistry(2)
	registry.Upsert(validHeartbeat("tenant-1", "fresh", now.Add(-time.Second)))
	registry.Upsert(validHeartbeat("tenant-2", "stale", now.Add(-time.Minute)))
	registry.Upsert(validHeartbeat("tenant-3", "rejected", now))
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	registration, err := registry.RegisterTelemetry(
		provider.Meter("test"), func() time.Time { return now }, 30*time.Second,
	)
	if err != nil {
		t.Fatalf("RegisterTelemetry() error = %v", err)
	}
	defer func() { _ = registration.Unregister() }()

	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(data.ScopeMetrics) != 1 || len(data.ScopeMetrics[0].Metrics) != 2 {
		t.Fatalf("metrics = %+v, want worker and rejection metrics", data.ScopeMetrics)
	}
	states := map[string]int64{}
	var rejected int64
	for _, metric := range data.ScopeMetrics[0].Metrics {
		switch points := metric.Data.(type) {
		case metricdata.Gauge[int64]:
			for _, point := range points.DataPoints {
				state, ok := point.Attributes.Value(attribute.Key("state"))
				if !ok {
					t.Fatal("worker gauge lacks state")
				}
				states[state.AsString()] = point.Value
			}
		case metricdata.Sum[int64]:
			if len(points.DataPoints) != 1 {
				t.Fatalf("rejection points = %+v", points.DataPoints)
			}
			rejected = points.DataPoints[0].Value
		default:
			t.Fatalf("metric data = %T", metric.Data)
		}
	}
	if states[string(StateRunning)] != 1 || states[string(StateStale)] != 1 ||
		len(states) != 6 || rejected != 1 {
		t.Fatalf("states = %v, rejected = %d", states, rejected)
	}
}

func TestBoundedMetricCountSaturates(t *testing.T) {
	t.Parallel()

	if boundedMetricCount(1) != 1 || boundedMetricCount(math.MaxUint64) != math.MaxInt64 {
		t.Fatal("boundedMetricCount() did not preserve or saturate the count")
	}
}

type failingFleetMeter struct {
	otelmetric.Meter
	gaugeErr    error
	counterErr  error
	registerErr error
}

func (m failingFleetMeter) Int64ObservableGauge(name string, options ...otelmetric.Int64ObservableGaugeOption) (otelmetric.Int64ObservableGauge, error) {
	if m.gaugeErr != nil {
		return nil, m.gaugeErr
	}
	return m.Meter.Int64ObservableGauge(name, options...)
}

func (m failingFleetMeter) Int64ObservableCounter(name string, options ...otelmetric.Int64ObservableCounterOption) (otelmetric.Int64ObservableCounter, error) {
	if m.counterErr != nil {
		return nil, m.counterErr
	}
	return m.Meter.Int64ObservableCounter(name, options...)
}

func (m failingFleetMeter) RegisterCallback(callback otelmetric.Callback, instruments ...otelmetric.Observable) (otelmetric.Registration, error) {
	if m.registerErr != nil {
		return nil, m.registerErr
	}
	return m.Meter.RegisterCallback(callback, instruments...)
}
