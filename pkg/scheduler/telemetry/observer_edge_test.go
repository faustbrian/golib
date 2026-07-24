package telemetry_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	schedulertelemetry "github.com/faustbrian/golib/pkg/scheduler/telemetry"
	gotelemetry "github.com/faustbrian/golib/pkg/telemetry"
	"github.com/faustbrian/golib/pkg/telemetry/testtelemetry"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestObserverReportsInstrumentConstructionFailures(t *testing.T) {
	t.Parallel()

	want := errors.New("instrument unavailable")
	provider := errorMeterProvider{
		MeterProvider: metricnoop.NewMeterProvider(),
		meter: errorMeter{
			Meter:      metricnoop.NewMeterProvider().Meter("test"),
			counterErr: want,
		},
	}
	config := schedulertelemetry.Config{
		Logger:         slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		TracerProvider: tracenoop.NewTracerProvider(),
		MeterProvider:  provider,
	}
	if _, err := schedulertelemetry.New(config); !errors.Is(err, want) {
		t.Fatalf("New() counter error = %v, want %v", err, want)
	}

	provider.meter = errorMeter{
		Meter:        metricnoop.NewMeterProvider().Meter("test"),
		histogramErr: want,
	}
	config.MeterProvider = provider
	if _, err := schedulertelemetry.New(config); !errors.Is(err, want) {
		t.Fatalf("New() histogram error = %v, want %v", err, want)
	}
}

func TestObserverBuildsFromTelemetryRuntime(t *testing.T) {
	t.Parallel()

	if _, err := schedulertelemetry.NewRuntime(nil, slog.Default()); !errors.Is(err, schedulertelemetry.ErrInvalidConfiguration) {
		t.Fatalf("NewRuntime(nil) error = %v", err)
	}

	config := gotelemetry.DefaultConfig("scheduler-test", "1.0.0")
	config.Traces.Enabled = false
	config.Metrics.Enabled = false
	config.RegisterGlobal = false
	runtime, err := gotelemetry.Init(context.Background(), config)
	if err != nil {
		t.Fatalf("telemetry.Init() error = %v", err)
	}
	t.Cleanup(func() { _ = runtime.Shutdown(context.Background()) })
	if _, err := schedulertelemetry.NewRuntime(runtime, slog.Default()); err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	if _, err := schedulertelemetry.NewRuntime(runtime, nil); !errors.Is(err, schedulertelemetry.ErrInvalidConfiguration) {
		t.Fatalf("NewRuntime(nil logger) error = %v", err)
	}
}

func TestObserverRecordsFailureAndSupersededLifecycles(t *testing.T) {
	t.Parallel()

	harness := testtelemetry.New()
	t.Cleanup(func() { _ = harness.Shutdown(context.Background()) })
	var logs bytes.Buffer
	observer, err := schedulertelemetry.New(schedulertelemetry.Config{
		Logger:         slog.New(slog.NewJSONHandler(&logs, nil)),
		TracerProvider: harness.TracerProvider(),
		MeterProvider:  harness.MeterProvider(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	occurrence := scheduler.Occurrence{
		ScheduleID: "schedule-id", ScheduleName: "failed-report", Task: "reports.generate",
		IdempotencyKey: "occurrence-id", ScheduledAt: time.Now(),
	}
	observer.Observe(scheduler.Event{Type: scheduler.EventBefore, Occurrence: occurrence})
	observer.Observe(scheduler.Event{Type: scheduler.EventBefore, Occurrence: occurrence})
	want := errors.New("report failed")
	observer.Observe(scheduler.Event{
		Type: scheduler.EventFailure, Result: scheduler.ResultFailed,
		Occurrence: occurrence, Err: want,
	})
	observer.Observe(scheduler.Event{
		Type: scheduler.EventCompleted, Result: scheduler.ResultFailed,
		Occurrence: occurrence, Err: want,
	})
	observer.Observe(scheduler.Event{
		Type: scheduler.EventFailure, Result: scheduler.ResultFailed,
		Occurrence: scheduler.Occurrence{IdempotencyKey: "missing"}, Err: want,
	})
	observer.Observe(scheduler.Event{
		Type: scheduler.EventCompleted, Result: scheduler.ResultFailed,
		Occurrence: scheduler.Occurrence{IdempotencyKey: "missing"}, Err: want,
	})

	spans := harness.Spans()
	if len(spans) != 2 {
		t.Fatalf("spans = %+v", spans)
	}
	if !bytes.Contains(logs.Bytes(), []byte(`"level":"ERROR"`)) {
		t.Fatalf("logs = %s", logs.String())
	}
}

type errorMeterProvider struct {
	metric.MeterProvider
	meter metric.Meter
}

func (provider errorMeterProvider) Meter(string, ...metric.MeterOption) metric.Meter {
	return provider.meter
}

type errorMeter struct {
	metric.Meter
	histogramErr error
	counterErr   error
}

func (meter errorMeter) Float64Histogram(
	string,
	...metric.Float64HistogramOption,
) (metric.Float64Histogram, error) {
	if meter.histogramErr != nil {
		return nil, meter.histogramErr
	}
	return meter.Meter.Float64Histogram("ok")
}

func (meter errorMeter) Int64Counter(
	string,
	...metric.Int64CounterOption,
) (metric.Int64Counter, error) {
	if meter.counterErr != nil {
		return nil, meter.counterErr
	}
	return meter.Meter.Int64Counter("ok")
}
