package idempotencytelemetry_test

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencytelemetry"
	"go.opentelemetry.io/otel/attribute"
	metricapi "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestObserverRecordsBoundedMetricAttributes(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	observer, err := idempotencytelemetry.New(provider)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	observer.Observe(context.Background(), idempotency.Observation{
		Transition:  idempotency.TransitionAcquire,
		Outcome:     idempotency.OutcomeAcquired,
		Reason:      idempotency.ReasonUnavailable,
		Durable:     true,
		Correlation: "must-not-be-a-metric-label",
	})

	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	metrics := data.ScopeMetrics[0].Metrics
	if len(metrics) != 1 || metrics[0].Name != "idempotency.transitions" {
		t.Fatalf("metrics = %#v", metrics)
	}
	sum, ok := metrics[0].Data.(metricdata.Sum[int64])
	if !ok || len(sum.DataPoints) != 1 || sum.DataPoints[0].Value != 1 {
		t.Fatalf("metric data = %#v", metrics[0].Data)
	}
	want := attribute.NewSet(
		attribute.String("transition", "acquire"),
		attribute.String("outcome", "acquired"),
		attribute.String("reason", "unavailable"),
		attribute.Bool("durable", true),
	)
	if !sum.DataPoints[0].Attributes.Equals(&want) {
		t.Fatalf("attributes = %v, want %v", sum.DataPoints[0].Attributes, want)
	}
}

func TestNewRejectsNilProviderAndInstrumentErrors(t *testing.T) {
	observer, err := idempotencytelemetry.New(nil)
	if observer != nil || !errors.Is(err, idempotencytelemetry.ErrNilMeterProvider) {
		t.Fatalf("New(nil) = %#v, %v", observer, err)
	}

	want := errors.New("instrument failed")
	provider := errorProvider{
		MeterProvider: metric.NewMeterProvider(),
		meter: errorMeter{
			Meter: metric.NewMeterProvider().Meter("test"),
			err:   want,
		},
	}
	observer, err = idempotencytelemetry.New(provider)
	if observer != nil || !errors.Is(err, want) {
		t.Fatalf("New(error provider) = %#v, %v", observer, err)
	}
}

type errorProvider struct {
	metricapi.MeterProvider
	meter metricapi.Meter
}

func (provider errorProvider) Meter(string, ...metricapi.MeterOption) metricapi.Meter {
	return provider.meter
}

type errorMeter struct {
	metricapi.Meter
	err error
}

func (meter errorMeter) Int64Counter(
	string,
	...metricapi.Int64CounterOption,
) (metricapi.Int64Counter, error) {
	return nil, meter.err
}
