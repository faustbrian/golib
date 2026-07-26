package goqueue

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/telemetry/testtelemetry"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
)

func TestWrapHandlerDoesNotRecordMessagesOrErrors(t *testing.T) {
	t.Parallel()

	harness := testtelemetry.New()
	instrumenter, err := New(Config{
		Backend:        BackendRedisStream,
		TracerProvider: harness.TracerProvider(),
		MeterProvider:  harness.MeterProvider(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	want := errors.New("secret handler error")
	handler := WrapHandler(instrumenter, func(context.Context, secretMessage) error {
		return want
	})
	if err := handler(context.Background(), secretMessage{Payload: "secret payload"}); !errors.Is(err, want) {
		t.Fatalf("handler error = %v, want %v", err, want)
	}

	span := harness.Spans()[0]
	text := fmt.Sprint(span)
	if span.Name != "queue.process" || span.Status.Code != codes.Error {
		t.Fatalf("span name/status = %q/%v, want queue.process/error", span.Name, span.Status.Code)
	}
	if strings.Contains(text, "secret") {
		t.Fatalf("span leaked message or handler error: %s", text)
	}
}

func TestWrapHandlerPreservesPanicsWithoutLeakingValues(t *testing.T) {
	t.Parallel()

	harness := testtelemetry.New()
	instrumenter, err := New(Config{Backend: BackendMemory, TracerProvider: harness.TracerProvider()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	handler := WrapHandler(instrumenter, func(context.Context, secretMessage) error {
		panic("secret panic value")
	})
	defer func() {
		if recovered := recover(); recovered != "secret panic value" {
			t.Fatalf("recovered = %v, want original panic", recovered)
		}
		span := harness.Spans()[0]
		if span.Status.Code != codes.Error || strings.Contains(fmt.Sprint(span), "secret") {
			t.Fatalf("panic telemetry is unsafe: %+v", span)
		}
	}()
	_ = handler(context.Background(), secretMessage{})
}

func TestConfigRejectsUnknownBackend(t *testing.T) {
	t.Parallel()

	if _, err := New(Config{Backend: Backend("secret-backend")}); err == nil {
		t.Fatal("New() error = nil, want backend validation error")
	}
}

func TestNewUsesNoopProviders(t *testing.T) {
	t.Parallel()

	instrumenter, err := New(Config{Backend: BackendOther})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	handler := WrapHandler(instrumenter, func(context.Context, struct{}) error { return nil })
	if err := handler(context.Background(), struct{}{}); err != nil {
		t.Fatalf("handler error = %v", err)
	}
}

func TestNewReportsInstrumentFailures(t *testing.T) {
	t.Parallel()

	want := errors.New("instrument failed")
	provider := errorMeterProvider{MeterProvider: metricnoop.NewMeterProvider(), meter: errorMeter{
		Meter:        metricnoop.NewMeterProvider().Meter("test"),
		histogramErr: want,
	}}
	if _, err := New(Config{Backend: BackendMemory, MeterProvider: provider}); !errors.Is(err, want) {
		t.Fatalf("New() histogram error = %v, want %v", err, want)
	}
	provider.meter = errorMeter{Meter: metricnoop.NewMeterProvider().Meter("test"), counterErr: want}
	if _, err := New(Config{Backend: BackendMemory, MeterProvider: provider}); !errors.Is(err, want) {
		t.Fatalf("New() counter error = %v, want %v", err, want)
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

func (meter errorMeter) Float64Histogram(string, ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	if meter.histogramErr != nil {
		return nil, meter.histogramErr
	}
	return meter.Meter.Float64Histogram("ok")
}

func (meter errorMeter) Int64Counter(string, ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if meter.counterErr != nil {
		return nil, meter.counterErr
	}
	return meter.Meter.Int64Counter("ok")
}

type secretMessage struct {
	Payload string
}

func (message secretMessage) String() string {
	return message.Payload
}
