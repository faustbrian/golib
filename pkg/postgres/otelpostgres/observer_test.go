package otelpostgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	postgres "github.com/faustbrian/golib/pkg/postgres"
	metricapi "go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestObserverRecordsBoundedLifecycleMetrics(t *testing.T) {
	t.Parallel()

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	observer, err := New(Config{MeterProvider: provider})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	observer.Observe(context.Background(), postgres.Observation{
		Operation:    postgres.OperationAcquire,
		Outcome:      postgres.OutcomeError,
		Duration:     25 * time.Millisecond,
		ErrorKind:    postgres.ErrorPoolExhaustion,
		SQLState:     "53300",
		HasPoolStats: true,
		Pool: postgres.Stats{
			AcquiredConns: 4,
			IdleConns:     1,
			TotalConns:    5,
			MaxConns:      10,
		},
	})

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	text := fmt.Sprint(metrics)
	for _, expected := range []string{
		"db.client.operation.duration",
		"db.client.operation.count",
		"db.client.connection.count",
		"pool.acquire",
		"pool_exhaustion",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("metrics do not contain %q: %s", expected, text)
		}
	}
	for _, forbidden := range []string{"SELECT", "password", "arguments", "dsn"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("metrics contain forbidden value %q: %s", forbidden, text)
		}
	}
}

func TestObserverDoesNotRecordPoolGaugesWithoutSnapshot(t *testing.T) {
	t.Parallel()

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	observer, err := New(Config{MeterProvider: provider})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	observer.Observe(context.Background(), postgres.Observation{
		Operation: postgres.OperationTransaction,
		Outcome:   postgres.OutcomeSuccess,
	})

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if strings.Contains(fmt.Sprint(metrics), "db.client.connection.count") {
		t.Fatalf("transaction observation recorded absent pool gauges: %v", metrics)
	}
}

func TestNewPreservesInstrumentConstructionErrors(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("instrument failed")
	base := metricnoop.NewMeterProvider().Meter("test")
	tests := []errorMeter{
		{Meter: base, histogramErr: sentinel},
		{Meter: base, counterErr: sentinel},
		{Meter: base, gaugeErr: sentinel},
	}
	for _, meter := range tests {
		provider := errorMeterProvider{
			MeterProvider: metricnoop.NewMeterProvider(),
			meter:         meter,
		}
		if _, err := New(Config{MeterProvider: provider}); !errors.Is(err, sentinel) {
			t.Fatalf("New() error = %v, want sentinel", err)
		}
	}
}

func TestObserverUsesNoopProviderByDefault(t *testing.T) {
	t.Parallel()

	observer, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	observer.Observe(context.Background(), postgres.Observation{})
}

func BenchmarkObserverInstrumentation(b *testing.B) {
	observer, err := New(Config{})
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	observation := postgres.Observation{
		Operation:    postgres.OperationAcquire,
		Outcome:      postgres.OutcomeSuccess,
		HasPoolStats: true,
		Pool:         postgres.Stats{MaxConns: 10},
	}

	for b.Loop() {
		observer.Observe(ctx, observation)
	}
}

type errorMeterProvider struct {
	metricapi.MeterProvider
	meter metricapi.Meter
}

func (p errorMeterProvider) Meter(string, ...metricapi.MeterOption) metricapi.Meter {
	return p.meter
}

type errorMeter struct {
	metricapi.Meter
	histogramErr error
	counterErr   error
	gaugeErr     error
}

func (m errorMeter) Float64Histogram(string, ...metricapi.Float64HistogramOption) (metricapi.Float64Histogram, error) {
	if m.histogramErr != nil {
		return nil, m.histogramErr
	}

	return m.Meter.Float64Histogram("ok")
}

func (m errorMeter) Int64Counter(string, ...metricapi.Int64CounterOption) (metricapi.Int64Counter, error) {
	if m.counterErr != nil {
		return nil, m.counterErr
	}

	return m.Meter.Int64Counter("ok")
}

func (m errorMeter) Int64Gauge(string, ...metricapi.Int64GaugeOption) (metricapi.Int64Gauge, error) {
	if m.gaugeErr != nil {
		return nil, m.gaugeErr
	}

	return m.Meter.Int64Gauge("ok")
}
