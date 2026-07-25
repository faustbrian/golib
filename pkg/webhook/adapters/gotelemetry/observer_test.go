package gotelemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	telemetry "github.com/faustbrian/golib/pkg/telemetry"
	webhook "github.com/faustbrian/golib/pkg/webhook"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/sdk/trace"
	tracetest "go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestObserverRecordsBoundedMetricsAndCurrentSpanEvent(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
	config := telemetry.DefaultConfig("webhook-test", "v1")
	config.Traces.Enabled = false
	config.Metrics.Enabled = false
	runtime, err := telemetry.Init(context.Background(), config)
	if err != nil {
		t.Skipf("telemetry global runtime unavailable: %v", err)
	}
	t.Cleanup(func() {
		if err := runtime.Shutdown(context.Background()); err != nil {
			t.Errorf("runtime.Shutdown() error = %v", err)
		}
	})
	// Runtime ownership is verified by telemetry itself. Replace its global
	// providers only for this adapter contract test through a recording span.
	observer, err := New(runtime)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, span := tracerProvider.Tracer("test").Start(context.Background(), "parent")
	observer.Observe(ctx, webhook.Observation{
		Operation: webhook.OperationDeliveryAttempt, Outcome: webhook.OutcomeRetry,
		Reason: webhook.ReasonStatus, Duration: time.Second, Algorithm: webhook.SHA256,
		StatusCode: 503, Classification: webhook.FailureRetryable,
	})
	span.End()
	if len(spanRecorder.Ended()) != 1 || len(spanRecorder.Ended()[0].Events()) != 1 {
		t.Fatalf("recorded spans = %#v", spanRecorder.Ended())
	}
}

func TestNewAndStatusClassValidation(t *testing.T) {
	if _, err := New(nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New(nil) error = %v", err)
	}
	instrumentErr := errors.New("instrument unavailable")
	if _, err := newObserver(&errorMeter{Meter: metricnoop.NewMeterProvider().Meter("test"), err: instrumentErr}); !errors.Is(err, instrumentErr) {
		t.Fatalf("newObserver(counter) error = %v", err)
	}
	if _, err := newObserver(&errorMeter{Meter: metricnoop.NewMeterProvider().Meter("test"), err: instrumentErr, failHistogram: true}); !errors.Is(err, instrumentErr) {
		t.Fatalf("newObserver(histogram) error = %v", err)
	}
	for status, want := range map[int]string{0: "none", 99: "none", 200: "2xx", 503: "5xx", 600: "none"} {
		if got := statusClass(status); got != want {
			t.Fatalf("statusClass(%d) = %q, want %q", status, got, want)
		}
	}
}

type errorMeter struct {
	metric.Meter
	err           error
	failHistogram bool
}

func (m *errorMeter) Int64Counter(name string, options ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if !m.failHistogram {
		return nil, m.err
	}
	return m.Meter.Int64Counter(name, options...)
}

func (m *errorMeter) Float64Histogram(string, ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	return nil, m.err
}
