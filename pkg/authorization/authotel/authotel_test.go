package authotel

import (
	"context"
	"errors"
	"testing"
	"time"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestInstrumenterRecordsBoundedMetricsAndSpan(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	exporter := tracetest.NewInMemoryExporter()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	instrumenter, err := New(Config{MeterProvider: meterProvider, TracerProvider: tracerProvider})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, finish := instrumenter.Start(context.Background())
	finish(authorization.Event{
		Outcome: authorization.Allow, Reason: "acl-allow", Revision: 7,
		MatchedPolicyIDs: []authorization.PolicyID{"one"}, TraceCount: 2,
		Duration: 5 * time.Millisecond,
	})
	finish(authorization.Event{Outcome: authorization.Deny})
	if !trace.SpanContextFromContext(ctx).IsValid() {
		t.Error("Start() did not return span context")
	}

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	names := map[string]bool{}
	for _, scope := range metrics.ScopeMetrics {
		for _, value := range scope.Metrics {
			names[value.Name] = true
		}
	}
	if !names["authorization.decision.count"] || !names["authorization.decision.duration"] {
		t.Errorf("metric names = %v", names)
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Name != "authorization.decide" {
		t.Fatalf("spans = %+v", spans)
	}
}

func TestInstrumenterNormalizesBoundedResults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		event authorization.Event
		want  string
	}{
		{authorization.Event{Outcome: authorization.Allow}, "allow"},
		{authorization.Event{Outcome: authorization.Deny}, "deny"},
		{authorization.Event{Outcome: authorization.NotApplicable}, "not-applicable"},
		{authorization.Event{Outcome: authorization.Allow, Failed: true}, "error"},
		{authorization.Event{Outcome: 99}, "error"},
	}
	for _, test := range tests {
		if got := result(test.event); got != test.want {
			t.Errorf("result(%+v) = %q, want %q", test.event, got, test.want)
		}
	}

	instrumenter, err := New(Config{})
	if err != nil {
		t.Fatalf("New(defaults) error = %v", err)
	}
	_, finish := instrumenter.Start(context.Background())
	finish(authorization.Event{Outcome: authorization.Deny, Failed: true})
}

func TestNewReturnsInstrumentConstructionErrors(t *testing.T) {
	t.Parallel()

	want := errors.New("instrument failed")
	for _, meter := range []*failingMeter{
		{histogramErr: want},
		{counterErr: want},
	} {
		_, err := New(Config{MeterProvider: failingMeterProvider{meter: meter}})
		if !errors.Is(err, want) {
			t.Errorf("New() error = %v, want instrument error", err)
		}
	}
}

type failingMeterProvider struct {
	metric.MeterProvider
	meter metric.Meter
}

func (provider failingMeterProvider) Meter(string, ...metric.MeterOption) metric.Meter {
	return provider.meter
}

type failingMeter struct {
	metric.Meter
	histogramErr error
	counterErr   error
}

func (meter *failingMeter) Float64Histogram(
	name string,
	options ...metric.Float64HistogramOption,
) (metric.Float64Histogram, error) {
	if meter.histogramErr != nil {
		return nil, meter.histogramErr
	}
	return metricnoop.NewMeterProvider().Meter("test").Float64Histogram(name, options...)
}

func (meter *failingMeter) Int64Counter(
	name string,
	options ...metric.Int64CounterOption,
) (metric.Int64Counter, error) {
	if meter.counterErr != nil {
		return nil, meter.counterErr
	}
	return metricnoop.NewMeterProvider().Meter("test").Int64Counter(name, options...)
}
