package outboxotel_test

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"weak"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/otel"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestPublicationSurvivesSamplingAndSDKShutdown(t *testing.T) {
	t.Parallel()

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.NeverSample()),
	)
	meterProvider := sdkmetric.NewMeterProvider()
	instrumentation, err := outboxotel.New(testRuntime{
		tracer: tracerProvider, meter: meterProvider, propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	if err := tracerProvider.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown tracer provider: %v", err)
	}
	if err := meterProvider.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown meter provider: %v", err)
	}

	want := errors.New("publisher failure")
	downstream := &recordingPublisher{err: want}
	publisher, err := instrumentation.WrapPublisher(downstream)
	if err != nil {
		t.Fatalf("wrap publisher: %v", err)
	}
	if err := publisher.Publish(context.Background(), outbox.Envelope{}); err != want {
		t.Fatalf("publish error = %v, want exact %v", err, want)
	}
	if downstream.calls != 1 {
		t.Fatalf("downstream calls = %d, want 1", downstream.calls)
	}
	instrumentation.Observe(context.Background(), outbox.Event{
		Operation: outbox.OperationPublish,
		Outcome:   outbox.OutcomeFailure,
		Count:     1,
	})
	instrumentation.RecordBacklog(context.Background(), outbox.BacklogStats{}, time.Now())
}

func TestTelemetryIsSafeForConcurrentPublicationAndObservation(t *testing.T) {
	t.Parallel()

	tracerProvider := sdktrace.NewTracerProvider()
	meterProvider := sdkmetric.NewMeterProvider()
	instrumentation, err := outboxotel.New(testRuntime{
		tracer: tracerProvider, meter: meterProvider, propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	downstream := &atomicPublisher{}
	publisher, err := instrumentation.WrapPublisher(downstream)
	if err != nil {
		t.Fatalf("wrap publisher: %v", err)
	}

	const publications = 64
	var wait sync.WaitGroup
	wait.Add(publications)
	for range publications {
		go func() {
			defer wait.Done()
			if err := publisher.Publish(context.Background(), outbox.Envelope{Attempts: 2}); err != nil {
				t.Errorf("publish: %v", err)
			}
			instrumentation.Observe(context.Background(), outbox.Event{
				Operation: outbox.OperationPublish,
				Outcome:   outbox.OutcomeSuccess,
				Count:     1,
				Attempts:  2,
			})
		}()
	}
	wait.Wait()
	if got := downstream.calls.Load(); got != publications {
		t.Fatalf("downstream calls = %d, want %d", got, publications)
	}
}

func TestPublicationCompletionIsExactlyOnceDuringCancellationAndSDKShutdown(t *testing.T) {
	t.Parallel()

	base := sdktrace.NewTracerProvider()
	meterProvider := sdkmetric.NewMeterProvider()
	var ended atomic.Int64
	instrumentation, err := outboxotel.New(testRuntime{
		tracer: countingTracerProvider{TracerProvider: base, ended: &ended},
		meter:  meterProvider, propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	downstream := &atomicPublisher{}
	publisher, err := instrumentation.WrapPublisher(downstream)
	if err != nil {
		t.Fatalf("wrap publisher: %v", err)
	}

	const publications = 128
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(publications + 2)
	for index := range publications {
		go func() {
			defer wait.Done()
			<-start
			ctx := context.Background()
			if index%2 == 0 {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			if publishErr := publisher.Publish(ctx, outbox.Envelope{Attempts: index}); publishErr != nil {
				t.Errorf("publish: %v", publishErr)
			}
			instrumentation.Observe(ctx, outbox.Event{
				Operation: outbox.OperationPublish,
				Outcome:   outbox.OutcomeSuccess,
				Count:     1,
				Attempts:  index,
			})
			instrumentation.RecordBacklog(ctx, outbox.BacklogStats{Pending: int64(index)}, time.Now())
		}()
	}
	go func() {
		defer wait.Done()
		<-start
		if shutdownErr := base.Shutdown(context.Background()); shutdownErr != nil {
			t.Errorf("shutdown tracer provider: %v", shutdownErr)
		}
	}()
	go func() {
		defer wait.Done()
		<-start
		if shutdownErr := meterProvider.Shutdown(context.Background()); shutdownErr != nil {
			t.Errorf("shutdown meter provider: %v", shutdownErr)
		}
	}()
	close(start)
	wait.Wait()

	if got := downstream.calls.Load(); got != publications {
		t.Fatalf("downstream calls = %d, want %d", got, publications)
	}
	if got := ended.Load(); got != publications {
		t.Fatalf("span completions = %d, want %d", got, publications)
	}
}

func TestBatchExporterBudgetBoundsPublishAndShutdown(t *testing.T) {
	t.Parallel()

	exporter := &deadlineExporter{called: make(chan struct{}, 1)}
	processor := sdktrace.NewBatchSpanProcessor(
		exporter,
		sdktrace.WithMaxQueueSize(8),
		sdktrace.WithMaxExportBatchSize(1),
		sdktrace.WithBatchTimeout(time.Millisecond),
		sdktrace.WithExportTimeout(20*time.Millisecond),
	)
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(processor))
	instrumentation, err := outboxotel.New(testRuntime{
		tracer: provider, meter: sdkmetric.NewMeterProvider(), propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	downstream := &atomicPublisher{}
	publisher, err := instrumentation.WrapPublisher(downstream)
	if err != nil {
		t.Fatalf("wrap publisher: %v", err)
	}

	published := make(chan error, 1)
	go func() {
		published <- publisher.Publish(context.Background(), outbox.Envelope{})
	}()
	select {
	case publishErr := <-published:
		if publishErr != nil {
			t.Fatalf("publish: %v", publishErr)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("publish waited for the slow exporter")
	}
	if got := downstream.calls.Load(); got != 1 {
		t.Fatalf("downstream calls = %d, want 1", got)
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if shutdownErr := provider.Shutdown(shutdownContext); shutdownErr != nil {
		t.Fatalf("shutdown tracer provider: %v", shutdownErr)
	}
	select {
	case <-exporter.called:
	default:
		t.Fatal("shutdown did not attempt to flush the completed span")
	}
}

func TestCallerDeadlineBoundsCooperativeSlowProvider(t *testing.T) {
	t.Parallel()

	propagator := deadlinePropagator{called: make(chan struct{}, 1)}
	instrumentation, err := outboxotel.New(testRuntime{
		tracer: tracenoop.NewTracerProvider(), meter: metricnoop.NewMeterProvider(), propagator: propagator,
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	downstream := &recordingPublisher{}
	publisher, err := instrumentation.WrapPublisher(downstream)
	if err != nil {
		t.Fatalf("wrap publisher: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	published := make(chan error, 1)
	go func() {
		published <- publisher.Publish(ctx, outbox.Envelope{})
	}()
	select {
	case publishErr := <-published:
		if publishErr != nil {
			t.Fatalf("publish: %v", publishErr)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("publish exceeded the caller's provider budget")
	}
	select {
	case <-propagator.called:
	default:
		t.Fatal("slow propagator was not exercised")
	}
	if downstream.calls != 1 || !errors.Is(downstream.context.Err(), context.DeadlineExceeded) {
		t.Fatalf("downstream calls/context = %d/%v", downstream.calls, downstream.context.Err())
	}
}

func TestNoOpProviderStartsNoGoroutines(t *testing.T) {
	instrumentation, err := outboxotel.New(testRuntime{
		tracer: tracenoop.NewTracerProvider(), meter: metricnoop.NewMeterProvider(), propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	publisher, err := instrumentation.WrapPublisher(&atomicPublisher{})
	if err != nil {
		t.Fatalf("wrap publisher: %v", err)
	}
	runtime.GC()
	before := runtime.NumGoroutine()
	for range 1_000 {
		if publishErr := publisher.Publish(context.Background(), outbox.Envelope{}); publishErr != nil {
			t.Fatalf("publish: %v", publishErr)
		}
	}
	runtime.GC()
	if after := runtime.NumGoroutine(); after != before {
		t.Fatalf("goroutines after publication = %d, want baseline %d", after, before)
	}
}

func TestWrappedPublisherDoesNotRetainEnvelopePayload(t *testing.T) {
	instrumentation, err := outboxotel.New(testRuntime{
		tracer: sdktrace.NewTracerProvider(), meter: sdkmetric.NewMeterProvider(), propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}
	publisher, err := instrumentation.WrapPublisher(&atomicPublisher{})
	if err != nil {
		t.Fatalf("wrap publisher: %v", err)
	}

	reference := publishPayload(t, publisher)
	for range 10 {
		runtime.GC()
		if reference.Value() == nil {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("wrapped publisher retained the completed envelope payload")
}

type retainedPayload [64 << 10]byte

func publishPayload(t *testing.T, publisher outboxotel.Publisher) weak.Pointer[retainedPayload] {
	t.Helper()

	payload := new(retainedPayload)
	reference := weak.Make(payload)
	if err := publisher.Publish(context.Background(), outbox.Envelope{Payload: payload[:]}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	return reference
}

type atomicPublisher struct {
	calls atomic.Int64
}

func (publisher *atomicPublisher) Publish(context.Context, outbox.Envelope) error {
	publisher.calls.Add(1)

	return nil
}

type countingTracerProvider struct {
	trace.TracerProvider
	ended *atomic.Int64
}

func (provider countingTracerProvider) Tracer(name string, options ...trace.TracerOption) trace.Tracer {
	return countingTracer{
		Tracer: provider.TracerProvider.Tracer(name, options...),
		ended:  provider.ended,
	}
}

type countingTracer struct {
	trace.Tracer
	ended *atomic.Int64
}

func (tracer countingTracer) Start(
	ctx context.Context,
	name string,
	options ...trace.SpanStartOption,
) (context.Context, trace.Span) {
	spanContext, span := tracer.Tracer.Start(ctx, name, options...)

	return spanContext, countingSpan{Span: span, ended: tracer.ended}
}

type countingSpan struct {
	trace.Span
	ended *atomic.Int64
}

func (span countingSpan) End(options ...trace.SpanEndOption) {
	span.ended.Add(1)
	span.Span.End(options...)
}

type deadlineExporter struct {
	called chan struct{}
}

func (exporter *deadlineExporter) ExportSpans(ctx context.Context, _ []sdktrace.ReadOnlySpan) error {
	select {
	case exporter.called <- struct{}{}:
	default:
	}
	<-ctx.Done()

	return ctx.Err()
}

func (*deadlineExporter) Shutdown(context.Context) error { return nil }

type deadlinePropagator struct {
	called chan struct{}
}

func (deadlinePropagator) Inject(context.Context, propagation.TextMapCarrier) {}

func (propagator deadlinePropagator) Extract(
	ctx context.Context,
	_ propagation.TextMapCarrier,
) context.Context {
	select {
	case propagator.called <- struct{}{}:
	default:
	}
	<-ctx.Done()

	return ctx
}

func (deadlinePropagator) Fields() []string { return nil }
