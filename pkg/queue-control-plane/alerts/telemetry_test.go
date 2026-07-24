package alerts

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestNewTelemetryExporterRejectsInvalidMeter(t *testing.T) {
	t.Parallel()

	if exporter, err := NewTelemetryExporter(nil); exporter != nil ||
		!errors.Is(err, ErrInvalidTelemetryConfiguration) {
		t.Fatalf("NewTelemetryExporter(nil) = (%v, %v)", exporter, err)
	}
	want := errors.New("instrument unavailable")
	exporter, err := NewTelemetryExporter(failingAlertMeter{
		Meter: metricnoop.NewMeterProvider().Meter("test"), err: want,
	})
	if exporter != nil || !errors.Is(err, want) {
		t.Fatalf("NewTelemetryExporter(failing) = (%v, %v)", exporter, err)
	}
}

func TestTelemetryExporterRecordsOnlyBoundedKinds(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	exporter, err := NewTelemetryExporter(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewTelemetryExporter() error = %v", err)
	}
	now := time.Unix(1, 0)
	batch := []Alert{
		{Kind: KindQueueWait, TenantID: "tenant-secret", Resource: "queue-secret", Value: 2, Threshold: 1, ObservedAt: now},
		{Kind: KindCommandFailure, TenantID: "tenant-secret", Resource: "command-secret", Value: 1, ObservedAt: now},
	}
	if err := exporter.Export(context.Background(), batch); err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	counts := map[string]int64{}
	for _, scope := range data.ScopeMetrics {
		for _, metric := range scope.Metrics {
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data = %T", metric.Data)
			}
			for _, point := range sum.DataPoints {
				kind, ok := point.Attributes.Value(attribute.Key("kind"))
				if !ok || point.Attributes.Len() != 1 {
					t.Fatalf("alert attributes = %v", point.Attributes)
				}
				counts[kind.AsString()] = point.Value
			}
		}
	}
	if counts[string(KindQueueWait)] != 1 || counts[string(KindCommandFailure)] != 1 ||
		len(counts) != 2 {
		t.Fatalf("alert counts = %v", counts)
	}
}

func TestTelemetryExporterRejectsInvalidBatches(t *testing.T) {
	t.Parallel()

	exporter, err := NewTelemetryExporter(metricnoop.NewMeterProvider().Meter("test"))
	if err != nil {
		t.Fatalf("NewTelemetryExporter() error = %v", err)
	}
	valid := Alert{
		Kind: KindQueueWait, TenantID: "tenant-1", Resource: "critical",
		Value: 2, Threshold: 1, ObservedAt: time.Unix(1, 0),
	}
	invalid := valid
	invalid.Kind = Kind("tenant-controlled")
	malformed := valid
	malformed.Resource = ""
	for name, input := range map[string]struct {
		ctx    context.Context
		alerts []Alert
	}{
		"context": {alerts: []Alert{valid}},
		"empty":   {ctx: context.Background()},
		"invalid": {ctx: context.Background(), alerts: []Alert{invalid}},
		"malformed": {
			ctx: context.Background(), alerts: []Alert{malformed},
		},
		"bounded": {ctx: context.Background(), alerts: make([]Alert, MaxAlertBatch+1)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := exporter.Export(input.ctx, input.alerts); !errors.Is(err, ErrInvalidAlertBatch) {
				t.Fatalf("Export() error = %v", err)
			}
		})
	}
}

type failingAlertMeter struct {
	otelmetric.Meter
	err error
}

func (m failingAlertMeter) Int64Counter(
	string,
	...otelmetric.Int64CounterOption,
) (otelmetric.Int64Counter, error) {
	return nil, m.err
}
