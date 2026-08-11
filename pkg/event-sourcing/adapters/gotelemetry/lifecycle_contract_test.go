package gotelemetry

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestBoundedBatchExporterBackpressureDoesNotBlockEventProcessing(t *testing.T) {
	exporter := &blockingSpanExporter{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	processor := sdktrace.NewBatchSpanProcessor(
		exporter,
		sdktrace.WithMaxQueueSize(8),
		sdktrace.WithMaxExportBatchSize(1),
		sdktrace.WithBatchTimeout(time.Hour),
	)
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(processor))
	t.Cleanup(func() {
		close(exporter.release)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tracerProvider.Shutdown(shutdownCtx)
	})
	instrumentation, err := New(testRuntime{
		tracer:     tracerProvider,
		meter:      metricnoop.NewMeterProvider(),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	var calls atomic.Int64
	dispatcher, err := instrumentation.WrapDispatcher(dispatcherFunc(func(
		context.Context,
		[]eventsourcing.Delivery,
	) error {
		calls.Add(1)
		return nil
	}))
	if err != nil {
		t.Fatalf("WrapDispatcher() error = %v", err)
	}
	if err := dispatcher.Dispatch(context.Background(), nil); err != nil {
		t.Fatalf("first Dispatch() error = %v", err)
	}
	select {
	case <-exporter.entered:
	case <-time.After(time.Second):
		t.Fatal("batch exporter did not enter blocked export")
	}

	done := make(chan error, 1)
	go func() {
		for range 1024 {
			if err := dispatcher.Dispatch(context.Background(), nil); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Dispatch() under backpressure error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("bounded batch exporter backpressure blocked event processing")
	}
	if got := calls.Load(); got != 1025 {
		t.Fatalf("downstream calls = %d, want 1025", got)
	}
}

func TestBoundedMetricExporterBackpressureDoesNotBlockEventProcessing(t *testing.T) {
	exporter := &blockingMetricExporter{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	reader := sdkmetric.NewPeriodicReader(
		exporter,
		sdkmetric.WithInterval(time.Millisecond),
		sdkmetric.WithTimeout(time.Hour),
	)
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		close(exporter.release)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = meterProvider.Shutdown(shutdownCtx)
	})
	instrumentation, err := New(testRuntime{
		tracer:     sdktrace.NewTracerProvider(),
		meter:      meterProvider,
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	var calls atomic.Int64
	dispatcher, err := instrumentation.WrapDispatcher(dispatcherFunc(func(
		context.Context,
		[]eventsourcing.Delivery,
	) error {
		calls.Add(1)
		return nil
	}))
	if err != nil {
		t.Fatalf("WrapDispatcher() error = %v", err)
	}
	if err := dispatcher.Dispatch(context.Background(), nil); err != nil {
		t.Fatalf("first Dispatch() error = %v", err)
	}
	select {
	case <-exporter.entered:
	case <-time.After(time.Second):
		t.Fatal("periodic metric exporter did not enter blocked export")
	}

	done := make(chan error, 1)
	go func() {
		for range 1024 {
			if err := dispatcher.Dispatch(context.Background(), nil); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Dispatch() under metric backpressure error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("bounded metric exporter backpressure blocked event processing")
	}
	if got := calls.Load(); got != 1025 {
		t.Fatalf("downstream calls = %d, want 1025", got)
	}
}

func TestSDKSamplingAndShutdownCannotChangeDownstreamResult(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.NeverSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instrumentation, err := New(testRuntime{
		tracer:     tracerProvider,
		meter:      meterProvider,
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	want := errors.New("downstream result")
	dispatcher, err := instrumentation.WrapDispatcher(
		dispatcherFunc(func(context.Context, []eventsourcing.Delivery) error { return want }),
	)
	if err != nil {
		t.Fatalf("WrapDispatcher() error = %v", err)
	}
	if got := dispatcher.Dispatch(context.Background(), nil); got != want {
		t.Fatalf("sampled-out Dispatch() error = %v, want exact %v", got, want)
	}
	if len(recorder.Ended()) != 0 {
		t.Fatalf("sampled-out ended spans = %d, want 0", len(recorder.Ended()))
	}
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	assertOperationMetric(t, metrics, "dispatch", "error", 1)

	if err := tracerProvider.Shutdown(context.Background()); err != nil {
		t.Fatalf("tracer Shutdown() error = %v", err)
	}
	if err := meterProvider.Shutdown(context.Background()); err != nil {
		t.Fatalf("meter Shutdown() error = %v", err)
	}
	for range 1024 {
		if got := dispatcher.Dispatch(context.Background(), nil); got != want {
			t.Fatalf("post-shutdown Dispatch() error = %v, want exact %v", got, want)
		}
	}
}

func TestExporterFailureCannotReplaceDownstreamResult(t *testing.T) {
	exporter := &failingSpanExporter{err: errors.New("export failure")}
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(
		sdktrace.NewSimpleSpanProcessor(exporter),
	))
	t.Cleanup(func() { _ = tracerProvider.Shutdown(context.Background()) })
	instrumentation, err := New(testRuntime{
		tracer:     tracerProvider,
		meter:      metricnoop.NewMeterProvider(),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	want := errors.New("downstream result")
	dispatcher, err := instrumentation.WrapDispatcher(
		dispatcherFunc(func(context.Context, []eventsourcing.Delivery) error { return want }),
	)
	if err != nil {
		t.Fatalf("WrapDispatcher() error = %v", err)
	}
	if got := dispatcher.Dispatch(context.Background(), nil); got != want {
		t.Fatalf("Dispatch() error = %v, want exact %v", got, want)
	}
	if got := exporter.calls.Load(); got != 1 {
		t.Fatalf("export calls = %d, want 1", got)
	}
}

func TestIteratorSpanCompletesExactlyOnce(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	instrumentation, err := New(testRuntime{
		tracer: sdktrace.NewTracerProvider(
			sdktrace.WithSpanProcessor(recorder),
		),
		meter:      metricnoop.NewMeterProvider(),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	store, err := instrumentation.WrapEventStore(
		&telemetryStore{iterator: &telemetryIterator{}},
	)
	if err != nil {
		t.Fatalf("WrapEventStore() error = %v", err)
	}
	iterator, err := store.ReadStream(
		context.Background(),
		eventsourcing.StreamID{},
		eventsourcing.ReadStreamOptions{},
	)
	if err != nil {
		t.Fatalf("ReadStream() error = %v", err)
	}
	if err := iterator.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := iterator.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if got := len(recorder.Ended()); got != 1 {
		t.Fatalf("ended spans = %d, want 1", got)
	}
}

type blockingSpanExporter struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

type blockingMetricExporter struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (*blockingMetricExporter) Temporality(
	kind sdkmetric.InstrumentKind,
) metricdata.Temporality {
	return sdkmetric.DefaultTemporalitySelector(kind)
}

func (*blockingMetricExporter) Aggregation(
	kind sdkmetric.InstrumentKind,
) sdkmetric.Aggregation {
	return sdkmetric.DefaultAggregationSelector(kind)
}

func (exporter *blockingMetricExporter) Export(
	context.Context,
	*metricdata.ResourceMetrics,
) error {
	exporter.once.Do(func() { close(exporter.entered) })
	<-exporter.release
	return nil
}

func (*blockingMetricExporter) ForceFlush(context.Context) error {
	return nil
}

func (*blockingMetricExporter) Shutdown(context.Context) error {
	return nil
}

func (exporter *blockingSpanExporter) ExportSpans(
	context.Context,
	[]sdktrace.ReadOnlySpan,
) error {
	exporter.once.Do(func() { close(exporter.entered) })
	<-exporter.release
	return nil
}

func (*blockingSpanExporter) Shutdown(context.Context) error {
	return nil
}

type failingSpanExporter struct {
	err   error
	calls atomic.Int64
}

func (exporter *failingSpanExporter) ExportSpans(
	context.Context,
	[]sdktrace.ReadOnlySpan,
) error {
	exporter.calls.Add(1)
	return exporter.err
}

func (*failingSpanExporter) Shutdown(context.Context) error {
	return nil
}
