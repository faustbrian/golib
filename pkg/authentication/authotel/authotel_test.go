package authotel_test

import (
	"context"
	"errors"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authotel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestInstrumenterEmitsBoundedTraceAndMetrics(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instrumenter, err := authotel.New(authotel.Config{
		TracerProvider: tracerProvider,
		MeterProvider:  meterProvider,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, finish := instrumenter.Start(context.Background(), authentication.CredentialBearer)
	finish(authentication.Event{
		Outcome:  authentication.OutcomeFailed,
		Failure:  authentication.FailureRejected,
		Duration: 25 * time.Millisecond,
	})

	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "authentication.authenticate" {
		t.Fatalf("spans = %#v", spans)
	}
	if !hasAttribute(spans[0].Attributes(), "authentication.credential.kind", "bearer") ||
		!hasAttribute(spans[0].Attributes(), "authentication.outcome", "failed") ||
		!hasAttribute(spans[0].Attributes(), "authentication.failure.kind", "rejected") {
		t.Fatalf("span attributes = %#v", spans[0].Attributes())
	}
	if spans[0].Status().Code != codes.Error {
		t.Fatalf("span status = %#v", spans[0].Status())
	}

	var data metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &data); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(data.ScopeMetrics) != 1 || len(data.ScopeMetrics[0].Metrics) != 2 {
		t.Fatalf("metric data = %#v", data.ScopeMetrics)
	}
	for _, metric := range data.ScopeMetrics[0].Metrics {
		switch metric.Name {
		case "authentication.attempts":
			points := metric.Data.(metricdata.Sum[int64]).DataPoints
			if len(points) != 1 || points[0].Value != 1 {
				t.Fatalf("attempt points = %#v", points)
			}
		case "authentication.duration":
			points := metric.Data.(metricdata.Histogram[float64]).DataPoints
			if len(points) != 1 || points[0].Count != 1 || points[0].Sum != 0.025 {
				t.Fatalf("duration points = %#v", points)
			}
		default:
			t.Fatalf("unexpected metric %q", metric.Name)
		}
	}
}

func TestNewRejectsMissingProviders(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	tests := []authotel.Config{
		{},
		{TracerProvider: sdktrace.NewTracerProvider()},
		{TracerProvider: tracenoop.NewTracerProvider()},
		{MeterProvider: sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))},
	}
	for _, config := range tests {
		if _, err := authotel.New(config); !errors.Is(err, authentication.ErrInvalidConfiguration) {
			t.Fatalf("New(%+v) error = %v", config, err)
		}
	}
	var typedNil *errorMeterProvider
	if _, err := authotel.New(authotel.Config{
		TracerProvider: sdktrace.NewTracerProvider(),
		MeterProvider:  typedNil,
	}); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("New(typed nil provider) error = %v", err)
	}
}

func TestNewReportsInstrumentConstructionFailures(t *testing.T) {
	t.Parallel()

	want := errors.New("instrument construction failed")
	base := metricnoop.NewMeterProvider().Meter("test")
	tests := []errorMeter{
		{Meter: base, counterErr: want},
		{Meter: base, histogramErr: want},
	}
	for _, meter := range tests {
		_, err := authotel.New(authotel.Config{
			TracerProvider: sdktrace.NewTracerProvider(),
			MeterProvider:  &errorMeterProvider{meter: meter},
		})
		if !errors.Is(err, want) {
			t.Fatalf("New() error = %v, want construction failure", err)
		}
	}
}

type errorMeterProvider struct {
	metric.MeterProvider
	meter metric.Meter
}

func (p *errorMeterProvider) Meter(string, ...metric.MeterOption) metric.Meter {
	return p.meter
}

type errorMeter struct {
	metric.Meter
	counterErr   error
	histogramErr error
}

func (m errorMeter) Int64Counter(
	string,
	...metric.Int64CounterOption,
) (metric.Int64Counter, error) {
	if m.counterErr != nil {
		return nil, m.counterErr
	}
	return m.Meter.Int64Counter("authentication.attempts")
}

func (m errorMeter) Float64Histogram(
	string,
	...metric.Float64HistogramOption,
) (metric.Float64Histogram, error) {
	if m.histogramErr != nil {
		return nil, m.histogramErr
	}
	return m.Meter.Float64Histogram("authentication.duration")
}

func hasAttribute(attributes []attribute.KeyValue, key, value string) bool {
	for _, candidate := range attributes {
		if string(candidate.Key) == key && candidate.Value.AsString() == value {
			return true
		}
	}
	return false
}
