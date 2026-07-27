package gotelemetry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestInstrumentationTracesAndMeasuresDispatchAndConsumption(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(recorder),
	)
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instrumentation, err := New(testRuntime{
		tracer:     tracerProvider,
		meter:      meterProvider,
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("construct instrumentation: %v", err)
	}

	live := telemetryDelivery(t, "secret-message-live", eventsourcing.DeliveryLive)
	replay := telemetryDelivery(
		t,
		"secret-message-replay",
		eventsourcing.DeliveryReplay,
	)
	nextDispatcher := &recordingDispatcher{}
	dispatcher, err := instrumentation.WrapDispatcher(nextDispatcher)
	if err != nil {
		t.Fatalf("wrap dispatcher: %v", err)
	}
	parentCtx, parent := tracerProvider.Tracer("test").Start(
		context.Background(),
		"parent",
	)
	if err := dispatcher.Dispatch(
		parentCtx,
		[]eventsourcing.Delivery{live, replay},
	); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if nextDispatcher.count != 2 ||
		nextDispatcher.span.TraceID() != parent.SpanContext().TraceID() {
		t.Fatalf(
			"downstream dispatch = count %d trace %s",
			nextDispatcher.count,
			nextDispatcher.span.TraceID(),
		)
	}

	consumed := 0
	consumer, err := instrumentation.WrapConsumer(
		eventsourcing.ConsumerFunc(func(
			ctx context.Context,
			delivery eventsourcing.Delivery,
		) error {
			consumed++
			if delivery.Mode() != eventsourcing.DeliveryLive ||
				trace.SpanContextFromContext(ctx).TraceID() !=
					parent.SpanContext().TraceID() {
				return errors.New("consumer context mismatch")
			}

			return nil
		}),
	)
	if err != nil {
		t.Fatalf("wrap consumer: %v", err)
	}
	if err := consumer(parentCtx, live); err != nil {
		t.Fatalf("consume: %v", err)
	}
	parent.End()
	if consumed != 1 {
		t.Fatalf("consumed = %d", consumed)
	}

	spans := recorder.Ended()
	if len(spans) != 3 ||
		spans[0].Name() != "event_sourcing.dispatch" ||
		spans[1].Name() != "event_sourcing.consume" ||
		spans[0].Parent().TraceID() != parent.SpanContext().TraceID() ||
		spans[1].Parent().TraceID() != parent.SpanContext().TraceID() {
		t.Fatalf("spans = %#v", spans)
	}
	telemetryText := fmt.Sprint(spans)
	if strings.Contains(telemetryText, "secret-message") {
		t.Fatalf("telemetry disclosed message identity: %s", telemetryText)
	}

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	assertMetricNames(
		t,
		metrics,
		"event_sourcing.operations",
		"event_sourcing.operation.duration",
		"event_sourcing.deliveries",
	)
	assertOperationMetric(t, metrics, "dispatch", "success", 1)
	assertOperationMetric(t, metrics, "consume", "success", 1)
	assertDeliveryMetric(t, metrics, "live", "success", 2)
	assertDeliveryMetric(t, metrics, "replay", "success", 1)
	if strings.Contains(fmt.Sprint(metrics), "secret-message") {
		t.Fatal("metrics disclosed message identity")
	}
}

func TestInstrumentationPreservesFailuresAndPanicsWithoutDisclosure(
	t *testing.T,
) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	instrumentation, err := New(testRuntime{
		tracer: sdktrace.NewTracerProvider(
			sdktrace.WithSpanProcessor(recorder),
		),
		meter: sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(reader),
		),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatal(err)
	}
	secretFailure := errors.New("credential=secret")
	dispatcher, err := instrumentation.WrapDispatcher(
		&recordingDispatcher{err: secretFailure},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.Dispatch(
		context.Background(),
		[]eventsourcing.Delivery{telemetryDelivery(
			t,
			"message-1",
			eventsourcing.DeliveryLive,
		)},
	); !errors.Is(err, secretFailure) {
		t.Fatalf("dispatch error = %v", err)
	}
	consumer, err := instrumentation.WrapConsumer(
		eventsourcing.ConsumerFunc(func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			return secretFailure
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := consumer(
		context.Background(),
		telemetryDelivery(t, "message-2", eventsourcing.DeliveryReplay),
	); !errors.Is(err, secretFailure) {
		t.Fatalf("consumer error = %v", err)
	}
	for _, span := range recorder.Ended() {
		if span.Status().Code != codes.Error ||
			strings.Contains(fmt.Sprint(span), "secret") {
			t.Fatalf("unsafe failure span = %#v", span)
		}
	}

	assertPanicPreserved(t, "dispatch panic", func() {
		panicDispatcher, wrapErr := instrumentation.WrapDispatcher(
			&recordingDispatcher{panicValue: "dispatch panic"},
		)
		if wrapErr != nil {
			t.Fatal(wrapErr)
		}
		_ = panicDispatcher.Dispatch(context.Background(), nil)
	})
	assertPanicPreserved(t, "consumer panic", func() {
		panicConsumer, wrapErr := instrumentation.WrapConsumer(
			eventsourcing.ConsumerFunc(func(
				context.Context,
				eventsourcing.Delivery,
			) error {
				panic("consumer panic")
			}),
		)
		if wrapErr != nil {
			t.Fatal(wrapErr)
		}
		_ = panicConsumer(context.Background(), eventsourcing.Delivery{})
	})
	for _, span := range recorder.Ended() {
		if strings.Contains(fmt.Sprint(span), "panic") {
			t.Fatalf("panic value disclosed in span: %#v", span)
		}
	}
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("collect failure metrics: %v", err)
	}
	assertOperationMetric(t, metrics, "dispatch", "error", 1)
	assertOperationMetric(t, metrics, "consume", "error", 1)
	assertOperationMetric(t, metrics, "dispatch", "panic", 1)
	assertOperationMetric(t, metrics, "consume", "panic", 1)
	assertDeliveryMetric(t, metrics, "live", "error", 1)
	assertDeliveryMetric(t, metrics, "replay", "error", 1)
	assertDeliveryMetric(t, metrics, "unknown", "panic", 1)
}

func TestInstrumentationValidatesDependenciesAndInstrumentConstruction(
	t *testing.T,
) {
	t.Parallel()

	tests := []struct {
		name    string
		runtime Runtime
		want    error
	}{
		{"nil runtime", nil, ErrRuntimeRequired},
		{
			"nil tracer",
			testRuntime{
				meter:      metricnoop.NewMeterProvider(),
				propagator: propagation.TraceContext{},
			},
			ErrRuntimeRequired,
		},
		{
			"nil meter",
			testRuntime{
				tracer:     sdktrace.NewTracerProvider(),
				propagator: propagation.TraceContext{},
			},
			ErrRuntimeRequired,
		},
		{
			"nil propagator",
			testRuntime{
				tracer: sdktrace.NewTracerProvider(),
				meter:  metricnoop.NewMeterProvider(),
			},
			ErrRuntimeRequired,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			instrumentation, err := New(test.runtime)
			if instrumentation != nil || !errors.Is(err, test.want) {
				t.Fatalf("instrumentation = %#v, error = %v", instrumentation, err)
			}
		})
	}

	failure := errors.New("instrument unavailable")
	base := metricnoop.NewMeterProvider().Meter("test")
	for name, meter := range map[string]metric.Meter{
		"operations": failingMeter{Meter: base, counterErr: failure},
		"duration":   failingMeter{Meter: base, histogramErr: failure},
		"deliveries": failingMeter{
			Meter:           base,
			deliveriesError: failure,
		},
		"projection messages": failingMeter{
			Meter:                   base,
			projectionMessagesError: failure,
		},
		"projection lag": failingMeter{
			Meter:              base,
			projectionLagError: failure,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := New(testRuntime{
				tracer: sdktrace.NewTracerProvider(),
				meter: failingMeterProvider{
					MeterProvider: metricnoop.NewMeterProvider(),
					meter:         meter,
				},
				propagator: propagation.TraceContext{},
			})
			if !errors.Is(err, failure) {
				t.Fatalf("construction error = %v", err)
			}
			if !errors.Is(err, ErrInstrumentCreation) {
				t.Fatalf("error category = %v", err)
			}
			var instrumentError *InstrumentError
			if !errors.As(err, &instrumentError) ||
				instrumentError.Error() != ErrInstrumentCreation.Error() {
				t.Fatalf("error type or diagnostic = %T/%q", err, err)
			}
		})
	}
}

func TestInstrumentationRejectsInvalidWrappersAndContexts(t *testing.T) {
	t.Parallel()

	instrumentation, err := New(testRuntime{
		tracer:     sdktrace.NewTracerProvider(),
		meter:      metricnoop.NewMeterProvider(),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instrumentation.WrapDispatcher(nil); !errors.Is(
		err,
		ErrDispatcherRequired,
	) {
		t.Fatalf("nil dispatcher error = %v", err)
	}
	if _, err := instrumentation.WrapConsumer(nil); !errors.Is(
		err,
		ErrConsumerRequired,
	) {
		t.Fatalf("nil consumer error = %v", err)
	}
	var nilInstrumentation *Instrumentation
	if _, err := nilInstrumentation.WrapDispatcher(&recordingDispatcher{}); !errors.Is(
		err,
		ErrRuntimeRequired,
	) {
		t.Fatalf("nil instrumentation dispatcher error = %v", err)
	}
	if _, err := nilInstrumentation.WrapConsumer(
		eventsourcing.ConsumerFunc(func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			return nil
		}),
	); !errors.Is(err, ErrRuntimeRequired) {
		t.Fatalf("nil instrumentation consumer error = %v", err)
	}

	dispatcher, err := instrumentation.WrapDispatcher(&recordingDispatcher{})
	if err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.Dispatch(nilContext(), nil); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("nil dispatch context error = %v", err)
	}
	consumer, err := instrumentation.WrapConsumer(
		eventsourcing.ConsumerFunc(func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := consumer(nil, eventsourcing.Delivery{}); !errors.Is(
		err,
		ErrContextRequired,
	) {
		t.Fatalf("nil consumer context error = %v", err)
	}
}

func TestDeliveryModeClassification(t *testing.T) {
	t.Parallel()

	live := telemetryDelivery(t, "message-live", eventsourcing.DeliveryLive)
	replay := telemetryDelivery(t, "message-replay", eventsourcing.DeliveryReplay)
	tests := []struct {
		name       string
		deliveries []eventsourcing.Delivery
		want       string
	}{
		{"empty", nil, "empty"},
		{"live", []eventsourcing.Delivery{live}, "live"},
		{"replay", []eventsourcing.Delivery{replay}, "replay"},
		{"mixed", []eventsourcing.Delivery{live, replay}, "mixed"},
		{"unknown", []eventsourcing.Delivery{{}}, "unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := deliveryMode(test.deliveries); got != test.want {
				t.Fatalf("mode = %q, want %q", got, test.want)
			}
		})
	}
}

func TestInstrumentationConcurrentUse(t *testing.T) {
	t.Parallel()

	instrumentation, err := New(testRuntime{
		tracer:     sdktrace.NewTracerProvider(),
		meter:      metricnoop.NewMeterProvider(),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatal(err)
	}
	nextDispatcher := &concurrentDispatcher{}
	dispatcher, err := instrumentation.WrapDispatcher(nextDispatcher)
	if err != nil {
		t.Fatal(err)
	}
	var consumed atomic.Int64
	consumer, err := instrumentation.WrapConsumer(
		eventsourcing.ConsumerFunc(func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			consumed.Add(1)

			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	delivery := telemetryDelivery(t, "concurrent-message", eventsourcing.DeliveryLive)

	const operations = 32
	var wait sync.WaitGroup
	wait.Add(operations)
	for range operations {
		go func() {
			defer wait.Done()
			if err := dispatcher.Dispatch(
				context.Background(),
				[]eventsourcing.Delivery{delivery},
			); err != nil {
				t.Errorf("dispatch: %v", err)
			}
			if err := consumer(context.Background(), delivery); err != nil {
				t.Errorf("consume: %v", err)
			}
		}()
	}
	wait.Wait()
	if nextDispatcher.count.Load() != operations ||
		consumed.Load() != operations {
		t.Fatalf(
			"operations = dispatch %d consume %d",
			nextDispatcher.count.Load(),
			consumed.Load(),
		)
	}
}

type testRuntime struct {
	tracer     trace.TracerProvider
	meter      metric.MeterProvider
	propagator propagation.TextMapPropagator
}

func nilContext() context.Context {
	return nil
}

func (runtime testRuntime) TracerProvider() trace.TracerProvider {
	return runtime.tracer
}

func (runtime testRuntime) MeterProvider() metric.MeterProvider {
	return runtime.meter
}

func (runtime testRuntime) Propagator() propagation.TextMapPropagator {
	return runtime.propagator
}

type recordingDispatcher struct {
	err        error
	panicValue any
	count      int
	span       trace.SpanContext
}

type concurrentDispatcher struct {
	count atomic.Int64
}

func (dispatcher *concurrentDispatcher) Dispatch(
	_ context.Context,
	deliveries []eventsourcing.Delivery,
) error {
	dispatcher.count.Add(int64(len(deliveries)))

	return nil
}

func (dispatcher *recordingDispatcher) Dispatch(
	ctx context.Context,
	deliveries []eventsourcing.Delivery,
) error {
	if dispatcher.panicValue != nil {
		panic(dispatcher.panicValue)
	}
	dispatcher.count += len(deliveries)
	dispatcher.span = trace.SpanContextFromContext(ctx)

	return dispatcher.err
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
	counterErr              error
	histogramErr            error
	deliveriesError         error
	projectionMessagesError error
	projectionLagError      error
}

func (meter failingMeter) Int64Counter(
	name string,
	options ...metric.Int64CounterOption,
) (metric.Int64Counter, error) {
	if name == "event_sourcing.operations" && meter.counterErr != nil {
		return nil, meter.counterErr
	}
	if name == "event_sourcing.deliveries" &&
		meter.deliveriesError != nil {
		return nil, meter.deliveriesError
	}
	if name == "event_sourcing.projection.messages" &&
		meter.projectionMessagesError != nil {
		return nil, meter.projectionMessagesError
	}

	return meter.Meter.Int64Counter(name, options...)
}

func (meter failingMeter) Int64Histogram(
	name string,
	options ...metric.Int64HistogramOption,
) (metric.Int64Histogram, error) {
	if name == "event_sourcing.projection.lag" &&
		meter.projectionLagError != nil {
		return nil, meter.projectionLagError
	}

	return meter.Meter.Int64Histogram(name, options...)
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

func telemetryDelivery(
	t testing.TB,
	messageID string,
	mode eventsourcing.DeliveryMode,
) eventsourcing.Delivery {
	t.Helper()

	stream, err := eventsourcing.NewStreamID("account", "aggregate-secret")
	if err != nil {
		t.Fatal(err)
	}
	event, err := eventsourcing.NewEncodedEvent(
		eventsourcing.EncodedEventInput{
			Name:        "account.changed",
			Version:     1,
			ContentType: "application/json",
			Payload:     []byte(`{"secret":"payload"}`),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:         messageID,
			Stream:     stream,
			Event:      event,
			Metadata:   map[string]string{"secret": "metadata"},
			RecordedAt: time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:       pending,
		StreamVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := eventsourcing.NewDelivery(message, mode)
	if err != nil {
		t.Fatal(err)
	}

	return delivery
}

func telemetryEventName(t testing.TB) eventsourcing.EventName {
	t.Helper()

	name, err := eventsourcing.NewEventName("account.changed")
	if err != nil {
		t.Fatal(err)
	}

	return name
}

func assertPanicPreserved(t *testing.T, want any, operation func()) {
	t.Helper()

	defer func() {
		if recovered := recover(); recovered != want {
			t.Fatalf("recovered = %#v, want %#v", recovered, want)
		}
	}()
	operation()
	t.Fatal("operation did not panic")
}

func assertMetricNames(
	t *testing.T,
	metrics metricdata.ResourceMetrics,
	names ...string,
) {
	t.Helper()

	got := make(map[string]struct{}, len(names))
	for _, scope := range metrics.ScopeMetrics {
		for _, item := range scope.Metrics {
			got[item.Name] = struct{}{}
		}
	}
	for _, name := range names {
		if _, exists := got[name]; !exists {
			t.Fatalf("metric %q is missing from %#v", name, got)
		}
	}
}

func assertOperationMetric(
	t *testing.T,
	metrics metricdata.ResourceMetrics,
	operation string,
	outcome string,
	want int64,
) {
	t.Helper()

	for _, scope := range metrics.ScopeMetrics {
		for _, item := range scope.Metrics {
			if item.Name != "event_sourcing.operations" {
				continue
			}
			sum, ok := item.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("operations data = %T", item.Data)
			}
			for _, point := range sum.DataPoints {
				if attributeValue(point.Attributes, "event_sourcing.operation") == operation &&
					attributeValue(point.Attributes, "event_sourcing.outcome") == outcome {
					if point.Value != want {
						t.Fatalf("operation count = %d, want %d", point.Value, want)
					}

					return
				}
			}
		}
	}
	t.Fatalf("operation metric %s/%s is missing", operation, outcome)
}

func assertDeliveryMetric(
	t *testing.T,
	metrics metricdata.ResourceMetrics,
	mode string,
	outcome string,
	want int64,
) {
	t.Helper()

	for _, scope := range metrics.ScopeMetrics {
		for _, item := range scope.Metrics {
			if item.Name != "event_sourcing.deliveries" {
				continue
			}
			sum, ok := item.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("deliveries data = %T", item.Data)
			}
			for _, point := range sum.DataPoints {
				if attributeValue(
					point.Attributes,
					"event_sourcing.delivery.mode",
				) == mode &&
					attributeValue(
						point.Attributes,
						"event_sourcing.outcome",
					) == outcome {
					if point.Value != want {
						t.Fatalf("delivery count = %d, want %d", point.Value, want)
					}

					return
				}
			}
		}
	}
	t.Fatalf("delivery metric %s/%s is missing", mode, outcome)
}

func attributeValue(set attribute.Set, key string) string {
	value, exists := set.Value(attribute.Key(key))
	if !exists {
		return ""
	}

	return value.AsString()
}
