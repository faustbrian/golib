package gotelemetry_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/gotelemetry"
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

func TestWrappedPublisherDoesNotExportEnvelopeContents(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	telemetry, err := gotelemetry.New(testRuntime{
		tracer: provider, meter: metricnoop.NewMeterProvider(), propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	publisher, err := telemetry.WrapPublisher(&recordingPublisher{})
	if err != nil {
		t.Fatalf("wrap publisher: %v", err)
	}

	envelope := outbox.Envelope{
		ID:             "secret-id",
		Topic:          "secret-topic",
		Payload:        []byte("secret-payload"),
		Metadata:       map[string]string{"authorization": "secret-credential"},
		IdempotencyKey: "secret-idempotency-key",
		Attempts:       91,
	}
	if err := publisher.Publish(context.Background(), envelope); err != nil {
		t.Fatalf("publish: %v", err)
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	for _, attribute := range spans[0].Attributes() {
		value := attribute.Value.String()
		for _, secret := range []string{
			"secret-id", "secret-topic", "secret-payload", "secret-credential",
			"secret-idempotency-key", "91",
		} {
			if value == secret {
				t.Fatalf("span attribute %q exported forbidden value %q", attribute.Key, value)
			}
		}
	}
}

func TestWrappedPublisherContainsPropagationPanic(t *testing.T) {
	t.Parallel()

	telemetry, err := gotelemetry.New(testRuntime{
		tracer:     sdktrace.NewTracerProvider(),
		meter:      metricnoop.NewMeterProvider(),
		propagator: panickingPropagator{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	want := errors.New("publisher failure")
	downstream := &recordingPublisher{err: want}
	publisher, err := telemetry.WrapPublisher(downstream)
	if err != nil {
		t.Fatalf("wrap publisher: %v", err)
	}

	if err := publisher.Publish(context.Background(), outbox.Envelope{}); err != want {
		t.Fatalf("publish error = %v, want exact %v", err, want)
	}
}

func TestWrappedPublisherPreservesCancellationFromHostilePropagator(t *testing.T) {
	t.Parallel()

	telemetry, err := gotelemetry.New(testRuntime{
		tracer: sdktrace.NewTracerProvider(), meter: metricnoop.NewMeterProvider(), propagator: replacingPropagator{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	downstream := &recordingPublisher{}
	publisher, err := telemetry.WrapPublisher(downstream)
	if err != nil {
		t.Fatalf("wrap publisher: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := publisher.Publish(ctx, outbox.Envelope{}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !errors.Is(downstream.context.Err(), context.Canceled) {
		t.Fatalf("downstream context error = %v", downstream.context.Err())
	}
}

func TestWrappedPublisherPreservesContextWhenProviderReturnsNil(t *testing.T) {
	t.Parallel()

	for name, tracer := range map[string]trace.TracerProvider{
		"span context": invalidStartTracerProvider{
			TracerProvider: sdktrace.NewTracerProvider(), nilContext: true,
		},
		"span": invalidStartTracerProvider{
			TracerProvider: sdktrace.NewTracerProvider(), nilSpan: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			telemetry, err := gotelemetry.New(testRuntime{
				tracer: tracer, meter: metricnoop.NewMeterProvider(), propagator: propagation.TraceContext{},
			})
			if err != nil {
				t.Fatalf("new telemetry: %v", err)
			}
			downstream := &recordingPublisher{}
			publisher, err := telemetry.WrapPublisher(downstream)
			if err != nil {
				t.Fatalf("wrap publisher: %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := publisher.Publish(ctx, outbox.Envelope{}); err != nil {
				t.Fatalf("publish: %v", err)
			}
			if !errors.Is(downstream.context.Err(), context.Canceled) {
				t.Fatalf("downstream context error = %v", downstream.context.Err())
			}
		})
	}

	telemetry, err := gotelemetry.New(testRuntime{
		tracer: sdktrace.NewTracerProvider(), meter: metricnoop.NewMeterProvider(), propagator: nilContextPropagator{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	downstream := &recordingPublisher{}
	publisher, err := telemetry.WrapPublisher(downstream)
	if err != nil {
		t.Fatalf("wrap publisher: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := publisher.Publish(ctx, outbox.Envelope{}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !errors.Is(downstream.context.Err(), context.Canceled) {
		t.Fatalf("nil extraction context error = %v", downstream.context.Err())
	}
}

func TestInjectContainsPropagationPanicAndPreservesMetadata(t *testing.T) {
	t.Parallel()

	telemetry, err := gotelemetry.New(testRuntime{
		tracer: sdktrace.NewTracerProvider(), meter: metricnoop.NewMeterProvider(), propagator: panickingPropagator{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	metadata := map[string]string{"tenant": "safe"}

	injected := telemetry.Inject(context.Background(), metadata)
	if injected["tenant"] != "safe" || metadata["tenant"] != "safe" {
		t.Fatalf("metadata/injected = %#v/%#v", metadata, injected)
	}
}

func TestPropagationCannotInspectOrAddArbitraryMetadata(t *testing.T) {
	t.Parallel()

	propagator := &recordingPropagator{}
	telemetry, err := gotelemetry.New(testRuntime{
		tracer: sdktrace.NewTracerProvider(), meter: metricnoop.NewMeterProvider(), propagator: propagator,
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	metadata := map[string]string{
		"authorization": "secret-credential",
		"tenant":        "secret-tenant",
		"traceparent":   "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"tracestate":    "vendor=value",
	}

	injected := telemetry.Inject(context.Background(), metadata)
	if injected["authorization"] != metadata["authorization"] || injected["tenant"] != metadata["tenant"] {
		t.Fatalf("injected metadata = %#v", injected)
	}
	if _, exists := injected["baggage"]; exists {
		t.Fatalf("injected metadata contains provider baggage: %#v", injected)
	}
	publisher, err := telemetry.WrapPublisher(&recordingPublisher{})
	if err != nil {
		t.Fatalf("wrap publisher: %v", err)
	}
	if err := publisher.Publish(context.Background(), outbox.Envelope{Metadata: metadata}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !reflect.DeepEqual(propagator.extracted, map[string]string{
		"traceparent": metadata["traceparent"],
		"tracestate":  metadata["tracestate"],
	}) {
		t.Fatalf("propagator carrier = %#v", propagator.extracted)
	}
}

func TestObservationContainsInstrumentPanics(t *testing.T) {
	t.Parallel()

	base := metricnoop.NewMeterProvider().Meter("test")
	telemetry, err := gotelemetry.New(testRuntime{
		tracer: sdktrace.NewTracerProvider(),
		meter: panickingMeterProvider{
			MeterProvider: metricnoop.NewMeterProvider(),
			meter:         panickingMeter{Meter: base},
		},
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}

	telemetry.Observe(context.Background(), outbox.Event{
		Operation: outbox.OperationPublish,
		Outcome:   outbox.OutcomeSuccess,
		Count:     1,
		Duration:  time.Second,
	})
	oldest := time.Now().Add(-time.Second)
	telemetry.RecordBacklog(context.Background(), outbox.BacklogStats{
		Pending: 1, Leased: 2, Dead: 3, OldestPendingAt: &oldest,
	}, time.Now())
}

func TestObservationBoundsCallerConstructedDimensions(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	telemetry, err := gotelemetry.New(testRuntime{
		tracer:     sdktrace.NewTracerProvider(),
		meter:      sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	telemetry.Observe(context.Background(), outbox.Event{
		Operation: outbox.Operation("secret-operation"),
		Outcome:   outbox.Outcome("secret-outcome"),
		Count:     1,
		Attempts:  3,
		Duration:  time.Second,
	})

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	for _, scope := range metrics.ScopeMetrics {
		for _, measurement := range scope.Metrics {
			switch data := measurement.Data.(type) {
			case metricdata.Sum[int64]:
				for _, point := range data.DataPoints {
					assertBoundedOperationAttributes(t, point.Attributes)
				}
			case metricdata.Histogram[float64]:
				for _, point := range data.DataPoints {
					assertBoundedOperationAttributes(t, point.Attributes)
				}
			}
		}
	}
}

func TestObservationNormalizesCounts(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	telemetry, err := gotelemetry.New(testRuntime{
		tracer:     sdktrace.NewTracerProvider(),
		meter:      sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	for _, count := range []int{-1, 0, 2} {
		telemetry.Observe(context.Background(), outbox.Event{
			Operation: outbox.OperationPublish,
			Outcome:   outbox.OutcomeSuccess,
			Count:     count,
		})
	}

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	var total int64
	for _, scope := range metrics.ScopeMetrics {
		for _, measurement := range scope.Metrics {
			if measurement.Name != "outbox.operations" {
				continue
			}
			for _, point := range measurement.Data.(metricdata.Sum[int64]).DataPoints {
				total += point.Value
			}
		}
	}
	if total != 4 {
		t.Fatalf("operation count = %d, want 4", total)
	}
}

func TestBacklogAgeNeverBecomesNegative(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		oldest time.Time
		now    time.Time
		want   float64
	}{
		"future": {oldest: time.Unix(11, 0), now: time.Unix(10, 0), want: 0},
		"equal":  {oldest: time.Unix(10, 0), now: time.Unix(10, 0), want: 0},
		"past":   {oldest: time.Unix(5, 0), now: time.Unix(10, 0), want: 5},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reader := sdkmetric.NewManualReader()
			telemetry, err := gotelemetry.New(testRuntime{
				tracer:     sdktrace.NewTracerProvider(),
				meter:      sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
				propagator: propagation.TraceContext{},
			})
			if err != nil {
				t.Fatalf("new telemetry: %v", err)
			}
			telemetry.RecordBacklog(context.Background(), outbox.BacklogStats{
				OldestPendingAt: &test.oldest,
			}, test.now)
			var metrics metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &metrics); err != nil {
				t.Fatalf("collect metrics: %v", err)
			}
			for _, scope := range metrics.ScopeMetrics {
				for _, measurement := range scope.Metrics {
					if measurement.Name != "outbox.backlog.oldest_pending_age" {
						continue
					}
					points := measurement.Data.(metricdata.Gauge[float64]).DataPoints
					if len(points) != 1 || points[0].Value != test.want {
						t.Fatalf("age points = %#v, want %g", points, test.want)
					}
				}
			}
		})
	}
}

func TestPublishRetryStateUsesFixedBoundaryBuckets(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	telemetry, err := gotelemetry.New(testRuntime{
		tracer: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)),
		meter:  metricnoop.NewMeterProvider(), propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	publisher, err := telemetry.WrapPublisher(&recordingPublisher{})
	if err != nil {
		t.Fatalf("wrap publisher: %v", err)
	}
	for _, attempts := range []int{-1, 0, 1, 2, 5, 6} {
		if err := publisher.Publish(context.Background(), outbox.Envelope{Attempts: attempts}); err != nil {
			t.Fatalf("publish attempt %d: %v", attempts, err)
		}
	}

	spans := recorder.Ended()
	want := []string{"none", "none", "first", "repeated", "repeated", "many"}
	if len(spans) != len(want) {
		t.Fatalf("ended spans = %d, want %d", len(spans), len(want))
	}
	for index, span := range spans {
		attributes := attribute.NewSet(span.Attributes()...)
		value, ok := attributes.Value("outbox.retry.state")
		if !ok || value.AsString() != want[index] {
			t.Fatalf("span %d retry state = %v/%t, want %q", index, value, ok, want[index])
		}
	}
}

func TestTelemetryScopesAreVersioned(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	telemetry, err := gotelemetry.New(testRuntime{
		tracer:     sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)),
		meter:      sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	publisher, err := telemetry.WrapPublisher(&recordingPublisher{})
	if err != nil {
		t.Fatalf("wrap publisher: %v", err)
	}
	if err := publisher.Publish(context.Background(), outbox.Envelope{}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	telemetry.Observe(context.Background(), outbox.Event{
		Operation: outbox.OperationPublish, Outcome: outbox.OutcomeSuccess, Count: 1,
	})

	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].InstrumentationScope().Version != gotelemetry.InstrumentationVersion {
		t.Fatalf("span scope = %#v", spans)
	}
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	if len(metrics.ScopeMetrics) != 1 ||
		metrics.ScopeMetrics[0].Scope.Version != gotelemetry.InstrumentationVersion {
		t.Fatalf("metric scope = %#v", metrics.ScopeMetrics)
	}
}

func TestWrappedPublisherPreservesCompletionSemantics(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	telemetry, err := gotelemetry.New(testRuntime{
		tracer: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)),
		meter:  metricnoop.NewMeterProvider(), propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	envelope := outbox.Envelope{
		ID: "id", Topic: "topic", Payload: []byte("payload"),
		Metadata: map[string]string{"key": "value"}, Attempts: 2,
	}

	success := &recordingPublisher{}
	wrapper, err := telemetry.WrapPublisher(success)
	if err != nil {
		t.Fatalf("wrap success publisher: %v", err)
	}
	if err := wrapper.Publish(context.Background(), envelope); err != nil {
		t.Fatalf("success publish: %v", err)
	}
	if success.calls != 1 || !reflect.DeepEqual(success.envelope, envelope) {
		t.Fatalf("success calls/envelope = %d/%#v", success.calls, success.envelope)
	}

	wantError := errors.New("publisher failure")
	failure := &recordingPublisher{err: wantError}
	wrapper, err = telemetry.WrapPublisher(failure)
	if err != nil {
		t.Fatalf("wrap failure publisher: %v", err)
	}
	if err := wrapper.Publish(context.Background(), envelope); err != wantError {
		t.Fatalf("failure error = %v, want exact %v", err, wantError)
	}

	wantPanic := &struct{ secret string }{secret: "panic"}
	panicking := &recordingPublisher{panicValue: wantPanic}
	wrapper, err = telemetry.WrapPublisher(panicking)
	if err != nil {
		t.Fatalf("wrap panicking publisher: %v", err)
	}
	func() {
		defer func() {
			if got := recover(); got != wantPanic {
				t.Fatalf("panic = %#v, want exact %#v", got, wantPanic)
			}
		}()
		_ = wrapper.Publish(context.Background(), envelope)
	}()

	spans := recorder.Ended()
	if len(spans) != 3 {
		t.Fatalf("ended spans = %d, want 3", len(spans))
	}
	for index, want := range []string{"success", "failure", "panic"} {
		attributes := attribute.NewSet(spans[index].Attributes()...)
		value, ok := attributes.Value("outbox.outcome")
		if !ok || value.AsString() != want {
			t.Fatalf("span %d outcome = %v/%t, want %q", index, value, ok, want)
		}
		wantStatus := codes.Error
		if want == "success" {
			wantStatus = codes.Unset
		}
		if spans[index].Status().Code != wantStatus {
			t.Fatalf("span %d status = %v, want %v", index, spans[index].Status().Code, wantStatus)
		}
		if spans[index].Status().Description == wantError.Error() ||
			spans[index].Status().Description == wantPanic.secret {
			t.Fatalf("span %d leaked failure text: %#v", index, spans[index].Status())
		}
	}
}

func TestWrappedPublisherPreservesOptionalHealthContract(t *testing.T) {
	t.Parallel()

	telemetry, err := gotelemetry.New(testRuntime{
		tracer: sdktrace.NewTracerProvider(), meter: metricnoop.NewMeterProvider(), propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	want := errors.New("health failure")
	downstream := &healthyRecordingPublisher{healthErr: want}
	publisher, err := telemetry.WrapPublisher(downstream)
	if err != nil {
		t.Fatalf("wrap publisher: %v", err)
	}
	health, ok := publisher.(interface{ Health(context.Context) error })
	if !ok {
		t.Fatal("wrapped publisher lost Health contract")
	}
	ctx := context.WithValue(context.Background(), contextKey{}, "context-value")
	if err := health.Health(ctx); err != want {
		t.Fatalf("health error = %v, want exact %v", err, want)
	}
	if downstream.healthCalls != 1 || downstream.healthContext.Value(contextKey{}) != "context-value" {
		t.Fatalf("health calls/context = %d/%v", downstream.healthCalls, downstream.healthContext)
	}
}

func assertBoundedOperationAttributes(t *testing.T, attributes attribute.Set) {
	t.Helper()

	operation, ok := attributes.Value("outbox.operation")
	if !ok || operation.AsString() != "unknown" {
		t.Fatalf("operation attribute = %v/%t, want unknown", operation, ok)
	}
	outcome, ok := attributes.Value("outbox.outcome")
	if !ok || outcome.AsString() != "unknown" {
		t.Fatalf("outcome attribute = %v/%t, want unknown", outcome, ok)
	}
	retry, ok := attributes.Value("outbox.retry.state")
	if !ok || retry.AsString() != "repeated" {
		t.Fatalf("retry attribute = %v/%t, want repeated", retry, ok)
	}
}

func TestWrappedPublisherContainsTracerStartPanic(t *testing.T) {
	t.Parallel()

	base := sdktrace.NewTracerProvider()
	telemetry, err := gotelemetry.New(testRuntime{
		tracer: panickingTracerProvider{TracerProvider: base},
		meter:  metricnoop.NewMeterProvider(), propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	want := errors.New("publisher failure")
	publisher, err := telemetry.WrapPublisher(&recordingPublisher{err: want})
	if err != nil {
		t.Fatalf("wrap publisher: %v", err)
	}

	if err := publisher.Publish(context.Background(), outbox.Envelope{}); err != want {
		t.Fatalf("publish error = %v, want exact %v", err, want)
	}
}

func TestWrappedPublisherContainsSpanCompletionPanic(t *testing.T) {
	t.Parallel()

	base := sdktrace.NewTracerProvider()
	telemetry, err := gotelemetry.New(testRuntime{
		tracer: completingPanicTracerProvider{TracerProvider: base},
		meter:  metricnoop.NewMeterProvider(), propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	publisher, err := telemetry.WrapPublisher(&recordingPublisher{})
	if err != nil {
		t.Fatalf("wrap publisher: %v", err)
	}

	if err := publisher.Publish(context.Background(), outbox.Envelope{}); err != nil {
		t.Fatalf("publish: %v", err)
	}
}

func TestWrappedPublisherContainsSpanStatusPanic(t *testing.T) {
	t.Parallel()

	base := sdktrace.NewTracerProvider()
	telemetry, err := gotelemetry.New(testRuntime{
		tracer: completingPanicTracerProvider{TracerProvider: base},
		meter:  metricnoop.NewMeterProvider(), propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	want := errors.New("publisher failure")
	publisher, err := telemetry.WrapPublisher(&recordingPublisher{err: want})
	if err != nil {
		t.Fatalf("wrap publisher: %v", err)
	}

	if err := publisher.Publish(context.Background(), outbox.Envelope{}); err != want {
		t.Fatalf("publish error = %v, want exact %v", err, want)
	}
}

func TestTelemetryRejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	if _, err := gotelemetry.New(nil); !errors.Is(err, gotelemetry.ErrRuntimeRequired) {
		t.Fatalf("nil runtime error = %v", err)
	}
	if _, err := gotelemetry.New(panickingRuntime{}); !errors.Is(err, gotelemetry.ErrRuntimeRequired) {
		t.Fatalf("panicking runtime error = %v", err)
	}
	for name, runtime := range map[string]testRuntime{
		"tracer":     {meter: metricnoop.NewMeterProvider(), propagator: propagation.TraceContext{}},
		"meter":      {tracer: sdktrace.NewTracerProvider(), propagator: propagation.TraceContext{}},
		"propagator": {tracer: sdktrace.NewTracerProvider(), meter: metricnoop.NewMeterProvider()},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := gotelemetry.New(runtime); !errors.Is(err, gotelemetry.ErrRuntimeRequired) {
				t.Fatalf("incomplete runtime error = %v", err)
			}
		})
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
	var missing *gotelemetry.Telemetry
	if _, err := missing.WrapPublisher(&recordingPublisher{}); !errors.Is(err, gotelemetry.ErrRuntimeRequired) {
		t.Fatalf("nil telemetry error = %v", err)
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
			if _, err := gotelemetry.New(runtime); !errors.Is(err, failure) ||
				!errors.Is(err, gotelemetry.ErrInstrumentCreation) {
				t.Fatalf("new error = %v, want provider cause and instrument category", err)
			}
		})
	}
}

func TestTelemetryContainsProviderConstructionPanic(t *testing.T) {
	t.Parallel()

	_, err := gotelemetry.New(testRuntime{
		tracer:     sdktrace.NewTracerProvider(),
		meter:      constructorPanicMeterProvider{MeterProvider: metricnoop.NewMeterProvider()},
		propagator: propagation.TraceContext{},
	})
	if !errors.Is(err, gotelemetry.ErrInstrumentCreation) {
		t.Fatalf("new error = %v", err)
	}
}

func TestTelemetryRejectsNilConstructedInstrument(t *testing.T) {
	t.Parallel()

	_, err := gotelemetry.New(testRuntime{
		tracer: nilTracerProvider{TracerProvider: sdktrace.NewTracerProvider()},
		meter:  metricnoop.NewMeterProvider(), propagator: propagation.TraceContext{},
	})
	if !errors.Is(err, gotelemetry.ErrInstrumentCreation) {
		t.Fatalf("new error = %v", err)
	}
}

type testRuntime struct {
	tracer     trace.TracerProvider
	meter      metric.MeterProvider
	propagator propagation.TextMapPropagator
}

type panickingRuntime struct{}

func (panickingRuntime) TracerProvider() trace.TracerProvider {
	panic("secret runtime panic")
}

func (panickingRuntime) MeterProvider() metric.MeterProvider { return nil }

func (panickingRuntime) Propagator() propagation.TextMapPropagator { return nil }

func (runtime testRuntime) TracerProvider() trace.TracerProvider      { return runtime.tracer }
func (runtime testRuntime) MeterProvider() metric.MeterProvider       { return runtime.meter }
func (runtime testRuntime) Propagator() propagation.TextMapPropagator { return runtime.propagator }

type recordingPublisher struct {
	err        error
	panicValue any
	span       trace.SpanContext
	context    context.Context
	calls      int
	envelope   outbox.Envelope
}

type contextKey struct{}

type healthyRecordingPublisher struct {
	recordingPublisher
	healthErr     error
	healthCalls   int
	healthContext context.Context
}

func (publisher *healthyRecordingPublisher) Health(ctx context.Context) error {
	publisher.healthCalls++
	publisher.healthContext = ctx

	return publisher.healthErr
}

type panickingPropagator struct{}

func (panickingPropagator) Inject(context.Context, propagation.TextMapCarrier) {
	panic("secret propagation panic")
}

func (panickingPropagator) Extract(context.Context, propagation.TextMapCarrier) context.Context {
	panic("secret propagation panic")
}

func (panickingPropagator) Fields() []string { return nil }

type replacingPropagator struct{}

func (replacingPropagator) Inject(context.Context, propagation.TextMapCarrier) {}

func (replacingPropagator) Extract(context.Context, propagation.TextMapCarrier) context.Context {
	return context.Background()
}

func (replacingPropagator) Fields() []string { return nil }

type nilContextPropagator struct{}

func (nilContextPropagator) Inject(context.Context, propagation.TextMapCarrier) {}

func (nilContextPropagator) Extract(context.Context, propagation.TextMapCarrier) context.Context {
	return nil
}

func (nilContextPropagator) Fields() []string { return nil }

type recordingPropagator struct {
	extracted map[string]string
}

func (*recordingPropagator) Inject(_ context.Context, carrier propagation.TextMapCarrier) {
	carrier.Set("traceparent", "provider-traceparent")
	carrier.Set("tracestate", "provider-tracestate")
	carrier.Set("baggage", "secret-provider-baggage")
	carrier.Set("authorization", "secret-provider-credential")
}

func (propagator *recordingPropagator) Extract(
	ctx context.Context,
	carrier propagation.TextMapCarrier,
) context.Context {
	propagator.extracted = map[string]string{}
	for _, key := range carrier.Keys() {
		propagator.extracted[key] = carrier.Get(key)
	}

	return ctx
}

func (*recordingPropagator) Fields() []string {
	return []string{"traceparent", "tracestate", "baggage", "authorization"}
}

type panickingTracerProvider struct{ trace.TracerProvider }

type nilTracerProvider struct{ trace.TracerProvider }

func (nilTracerProvider) Tracer(string, ...trace.TracerOption) trace.Tracer { return nil }

type invalidStartTracerProvider struct {
	trace.TracerProvider
	nilContext bool
	nilSpan    bool
}

func (provider invalidStartTracerProvider) Tracer(
	name string,
	options ...trace.TracerOption,
) trace.Tracer {
	return invalidStartTracer{
		Tracer:     provider.TracerProvider.Tracer(name, options...),
		nilContext: provider.nilContext,
		nilSpan:    provider.nilSpan,
	}
}

type invalidStartTracer struct {
	trace.Tracer
	nilContext bool
	nilSpan    bool
}

func (tracer invalidStartTracer) Start(
	ctx context.Context,
	name string,
	options ...trace.SpanStartOption,
) (context.Context, trace.Span) {
	ctx, span := tracer.Tracer.Start(ctx, name, options...)
	if tracer.nilContext {
		ctx = nil
	}
	if tracer.nilSpan {
		ctx = context.Background()
		span = nil
	}

	return ctx, span
}

func (provider panickingTracerProvider) Tracer(
	name string,
	options ...trace.TracerOption,
) trace.Tracer {
	return panickingTracer{Tracer: provider.TracerProvider.Tracer(name, options...)}
}

type panickingTracer struct{ trace.Tracer }

func (panickingTracer) Start(
	context.Context,
	string,
	...trace.SpanStartOption,
) (context.Context, trace.Span) {
	panic("secret tracer panic")
}

type completingPanicTracerProvider struct{ trace.TracerProvider }

func (provider completingPanicTracerProvider) Tracer(
	name string,
	options ...trace.TracerOption,
) trace.Tracer {
	return completingPanicTracer{Tracer: provider.TracerProvider.Tracer(name, options...)}
}

type completingPanicTracer struct{ trace.Tracer }

func (tracer completingPanicTracer) Start(
	ctx context.Context,
	name string,
	options ...trace.SpanStartOption,
) (context.Context, trace.Span) {
	ctx, span := tracer.Tracer.Start(ctx, name, options...)

	return ctx, completingPanicSpan{Span: span}
}

type completingPanicSpan struct{ trace.Span }

func (completingPanicSpan) End(...trace.SpanEndOption) {
	panic("secret span completion panic")
}

func (completingPanicSpan) SetStatus(codes.Code, string) {
	panic("secret span status panic")
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

type panickingMeterProvider struct {
	metric.MeterProvider
	meter metric.Meter
}

type constructorPanicMeterProvider struct{ metric.MeterProvider }

func (constructorPanicMeterProvider) Meter(string, ...metric.MeterOption) metric.Meter {
	panic("secret provider construction panic")
}

func (provider panickingMeterProvider) Meter(string, ...metric.MeterOption) metric.Meter {
	return provider.meter
}

type panickingMeter struct{ metric.Meter }

func (meter panickingMeter) Int64Counter(
	name string,
	options ...metric.Int64CounterOption,
) (metric.Int64Counter, error) {
	instrument, err := meter.Meter.Int64Counter(name, options...)

	return panickingInt64Counter{Int64Counter: instrument}, err
}

func (meter panickingMeter) Float64Histogram(
	name string,
	options ...metric.Float64HistogramOption,
) (metric.Float64Histogram, error) {
	instrument, err := meter.Meter.Float64Histogram(name, options...)

	return panickingFloat64Histogram{Float64Histogram: instrument}, err
}

func (meter panickingMeter) Int64Gauge(
	name string,
	options ...metric.Int64GaugeOption,
) (metric.Int64Gauge, error) {
	instrument, err := meter.Meter.Int64Gauge(name, options...)

	return panickingInt64Gauge{Int64Gauge: instrument}, err
}

func (meter panickingMeter) Float64Gauge(
	name string,
	options ...metric.Float64GaugeOption,
) (metric.Float64Gauge, error) {
	instrument, err := meter.Meter.Float64Gauge(name, options...)

	return panickingFloat64Gauge{Float64Gauge: instrument}, err
}

type panickingInt64Counter struct{ metric.Int64Counter }

func (panickingInt64Counter) Add(context.Context, int64, ...metric.AddOption) {
	panic("secret counter panic")
}

type panickingFloat64Histogram struct{ metric.Float64Histogram }

func (panickingFloat64Histogram) Record(context.Context, float64, ...metric.RecordOption) {
	panic("secret histogram panic")
}

type panickingInt64Gauge struct{ metric.Int64Gauge }

func (panickingInt64Gauge) Record(context.Context, int64, ...metric.RecordOption) {
	panic("secret gauge panic")
}

type panickingFloat64Gauge struct{ metric.Float64Gauge }

func (panickingFloat64Gauge) Record(context.Context, float64, ...metric.RecordOption) {
	panic("secret gauge panic")
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

func (publisher *recordingPublisher) Publish(ctx context.Context, envelope outbox.Envelope) error {
	publisher.calls++
	publisher.envelope = envelope
	publisher.context = ctx
	publisher.span = trace.SpanContextFromContext(ctx)
	if publisher.panicValue != nil {
		panic(publisher.panicValue)
	}

	return publisher.err
}
