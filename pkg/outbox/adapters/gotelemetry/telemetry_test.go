package gotelemetry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/gotelemetry"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestTelemetryLinksPublishToInjectedProducerTrace(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	runtime := testRuntime{tracer: provider, meter: metricnoop.NewMeterProvider(), propagator: propagation.TraceContext{}}
	telemetry, err := gotelemetry.New(runtime)
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	producerContext, producer := provider.Tracer("test").Start(context.Background(), "producer")
	metadata := map[string]string{"tenant": "safe"}
	injected := telemetry.Inject(producerContext, metadata)
	producer.End()
	if _, exists := metadata["traceparent"]; exists || injected["traceparent"] == "" {
		t.Fatalf("metadata/injected = %#v/%#v", metadata, injected)
	}

	downstream := &recordingPublisher{}
	publisher, err := telemetry.WrapPublisher(downstream)
	if err != nil {
		t.Fatalf("wrap publisher: %v", err)
	}
	envelope := outbox.Envelope{ID: "message-id", Topic: "orders", Metadata: injected}
	if err := publisher.Publish(context.Background(), envelope); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if downstream.span.TraceID() != trace.SpanContextFromContext(producerContext).TraceID() {
		t.Fatalf("downstream trace = %s", downstream.span.TraceID())
	}
	spans := recorder.Ended()
	if len(spans) != 2 || spans[1].Parent().TraceID() != spans[0].SpanContext().TraceID() {
		t.Fatalf("spans = %#v", spans)
	}
}

func TestTelemetryRecordsPayloadSafeMetricsAndPublishFailure(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	runtime := testRuntime{tracer: sdktrace.NewTracerProvider(), meter: provider, propagator: propagation.TraceContext{}}
	telemetry, err := gotelemetry.New(runtime)
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	telemetry.Observe(context.Background(), outbox.Event{
		Operation: outbox.OperationRetry,
		Outcome:   outbox.OutcomeFailure,
		Count:     2,
		MessageID: "must-not-be-an-attribute",
		Topic:     "must-not-be-an-attribute",
		Duration:  time.Second,
	})
	telemetry.Observe(context.Background(), outbox.Event{
		Operation: outbox.OperationClaim,
		Outcome:   outbox.OutcomeSuccess,
	})
	oldest := time.Now().Add(-5 * time.Second)
	telemetry.RecordBacklog(context.Background(), outbox.BacklogStats{
		Pending: 3, Leased: 2, Dead: 1, OldestPendingAt: &oldest,
	}, oldest.Add(5*time.Second))
	telemetry.RecordBacklog(context.Background(), outbox.BacklogStats{}, oldest)
	future := oldest.Add(time.Minute)
	telemetry.RecordBacklog(context.Background(), outbox.BacklogStats{OldestPendingAt: &future}, oldest)
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	if len(metrics.ScopeMetrics) != 1 || len(metrics.ScopeMetrics[0].Metrics) != 4 {
		t.Fatalf("metrics = %#v", metrics)
	}

	want := errors.New("secret publisher error")
	publisher, err := telemetry.WrapPublisher(&recordingPublisher{err: want})
	if err != nil {
		t.Fatalf("wrap publisher: %v", err)
	}
	if err := publisher.Publish(context.Background(), outbox.Envelope{}); !errors.Is(err, want) {
		t.Fatalf("publish error = %v, want %v", err, want)
	}
}

func TestTelemetryRejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	if _, err := gotelemetry.New(nil); !errors.Is(err, gotelemetry.ErrRuntimeRequired) {
		t.Fatalf("nil runtime error = %v", err)
	}
	telemetry, err := gotelemetry.New(testRuntime{
		tracer: sdktrace.NewTracerProvider(), meter: metricnoop.NewMeterProvider(), propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	if _, err := telemetry.WrapPublisher(nil); !errors.Is(err, gotelemetry.ErrPublisherRequired) {
		t.Fatalf("nil publisher error = %v", err)
	}
}

func TestTelemetryPreservesInstrumentConstructionFailures(t *testing.T) {
	t.Parallel()

	failure := errors.New("instrument unavailable")
	base := metricnoop.NewMeterProvider().Meter("test")
	tests := map[string]metric.Meter{
		"counter":     failingMeter{Meter: base, counterErr: failure},
		"histogram":   failingMeter{Meter: base, histogramErr: failure},
		"depth gauge": failingMeter{Meter: base, depthGaugeErr: failure},
		"age gauge":   failingMeter{Meter: base, ageGaugeErr: failure},
	}
	for name, meter := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			runtime := testRuntime{
				tracer:     sdktrace.NewTracerProvider(),
				meter:      failingMeterProvider{MeterProvider: metricnoop.NewMeterProvider(), meter: meter},
				propagator: propagation.TraceContext{},
			}
			if _, err := gotelemetry.New(runtime); !errors.Is(err, failure) {
				t.Fatalf("new error = %v, want %v", err, failure)
			}
		})
	}
}

type testRuntime struct {
	tracer     trace.TracerProvider
	meter      metric.MeterProvider
	propagator propagation.TextMapPropagator
}

func (runtime testRuntime) TracerProvider() trace.TracerProvider      { return runtime.tracer }
func (runtime testRuntime) MeterProvider() metric.MeterProvider       { return runtime.meter }
func (runtime testRuntime) Propagator() propagation.TextMapPropagator { return runtime.propagator }

type recordingPublisher struct {
	err  error
	span trace.SpanContext
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
	counterErr    error
	histogramErr  error
	depthGaugeErr error
	ageGaugeErr   error
}

func (meter failingMeter) Int64Counter(
	name string,
	options ...metric.Int64CounterOption,
) (metric.Int64Counter, error) {
	if meter.counterErr != nil {
		return nil, meter.counterErr
	}

	return meter.Meter.Int64Counter(name, options...)
}

func (meter failingMeter) Float64Histogram(
	name string,
	options ...metric.Float64HistogramOption,
) (metric.Float64Histogram, error) {
	if meter.histogramErr != nil {
		return nil, meter.histogramErr
	}

	return meter.Meter.Float64Histogram(name, options...)
}

func (meter failingMeter) Int64Gauge(
	name string,
	options ...metric.Int64GaugeOption,
) (metric.Int64Gauge, error) {
	if meter.depthGaugeErr != nil {
		return nil, meter.depthGaugeErr
	}

	return meter.Meter.Int64Gauge(name, options...)
}

func (meter failingMeter) Float64Gauge(
	name string,
	options ...metric.Float64GaugeOption,
) (metric.Float64Gauge, error) {
	if meter.ageGaugeErr != nil {
		return nil, meter.ageGaugeErr
	}

	return meter.Meter.Float64Gauge(name, options...)
}

func (publisher *recordingPublisher) Publish(ctx context.Context, _ outbox.Envelope) error {
	publisher.span = trace.SpanContextFromContext(ctx)

	return publisher.err
}
