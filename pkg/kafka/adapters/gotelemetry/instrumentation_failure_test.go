package gotelemetry

import (
	"errors"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestNewRedactsEveryInstrumentCreationFailure(t *testing.T) {
	t.Parallel()

	names := []string{
		"messaging.client.operation.duration",
		"messaging.process.duration",
		"messaging.client.consumed.messages",
		"kafka.client.operations",
		"kafka.client.operation.duration",
		"kafka.client.request.size",
		"kafka.client.request.queue.duration",
		"kafka.client.throttle.duration",
	}
	cause := errors.New("credential=secret")
	base := metricnoop.NewMeterProvider().Meter("test")
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			instrumentation, err := New(Config{Runtime: failingRuntime{
				meter: failingMeterProvider{
					MeterProvider: metricnoop.NewMeterProvider(),
					meter: failingMeter{
						Meter:    base,
						failName: name,
						cause:    cause,
					},
				},
			}})
			if instrumentation != nil ||
				!errors.Is(err, ErrInstrumentCreation) ||
				!errors.Is(err, cause) {
				t.Fatalf("New() = %#v, %v", instrumentation, err)
			}
			var instrumentError *InstrumentError
			if !errors.As(err, &instrumentError) ||
				strings.Contains(err.Error(), "secret") {
				t.Fatalf("unsafe instrument error = %T/%q", err, err)
			}
		})
	}
}

type failingRuntime struct {
	meter metric.MeterProvider
}

func (runtime failingRuntime) TracerProvider() trace.TracerProvider {
	return tracenoop.NewTracerProvider()
}

func (runtime failingRuntime) MeterProvider() metric.MeterProvider {
	return runtime.meter
}

type failingMeterProvider struct {
	metric.MeterProvider
	meter metric.Meter
}

func (provider failingMeterProvider) Meter(
	string,
	...metric.MeterOption,
) metric.Meter {
	return provider.meter
}

type failingMeter struct {
	metric.Meter
	failName string
	cause    error
}

func (meter failingMeter) Int64Counter(
	name string,
	options ...metric.Int64CounterOption,
) (metric.Int64Counter, error) {
	if name == meter.failName {
		return nil, meter.cause
	}

	return meter.Meter.Int64Counter(name, options...)
}

func (meter failingMeter) Int64Histogram(
	name string,
	options ...metric.Int64HistogramOption,
) (metric.Int64Histogram, error) {
	if name == meter.failName {
		return nil, meter.cause
	}

	return meter.Meter.Int64Histogram(name, options...)
}

func (meter failingMeter) Float64Histogram(
	name string,
	options ...metric.Float64HistogramOption,
) (metric.Float64Histogram, error) {
	if name == meter.failName {
		return nil, meter.cause
	}

	return meter.Meter.Float64Histogram(name, options...)
}
