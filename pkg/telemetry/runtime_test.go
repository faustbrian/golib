package telemetry

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	telemetrymetric "github.com/faustbrian/golib/pkg/telemetry/metric"
	telemetrypropagation "github.com/faustbrian/golib/pkg/telemetry/propagation"
	telemetrytrace "github.com/faustbrian/golib/pkg/telemetry/trace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestRuntimeProvidesStandardAPIsAndShutsDownOnce(t *testing.T) {
	exporter := &recordingSpanExporter{}
	config := DefaultConfig("orders", "1.2.3")
	config.RegisterGlobal = false
	config.Metrics.Enabled = false
	config.Traces.Sampler.Ratio = 1

	runtime, err := Init(context.Background(), config, WithTraceExporter(exporter))
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if runtime.TracerProvider() == nil {
		t.Fatal("TracerProvider() = nil, want standard provider")
	}
	if runtime.Tracer("test") == nil {
		t.Fatal("Tracer() = nil, want standard tracer")
	}
	if runtime.Propagator() == nil {
		t.Fatal("Propagator() = nil, want standard propagator")
	}

	_, span := runtime.Tracer("test").Start(context.Background(), "operation")
	span.End()
	if err := runtime.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush() error = %v", err)
	}
	if exporter.exportCount() != 1 {
		t.Fatalf("exported spans = %d, want 1", exporter.exportCount())
	}

	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("first Shutdown() error = %v", err)
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown() error = %v", err)
	}
	if exporter.shutdownCount() != 1 {
		t.Fatalf("exporter shutdowns = %d, want 1", exporter.shutdownCount())
	}
}

func TestTraceQueueBoundDoesNotBlockBusinessWork(t *testing.T) {
	exporter := newBlockingSpanExporter()
	config := DefaultConfig("orders", "1.2.3")
	config.RegisterGlobal = false
	config.Metrics.Enabled = false
	config.Traces.Sampler.Ratio = 1
	config.Traces.Batch.MaxQueueSize = 2
	config.Traces.Batch.MaxExportBatchSize = 1
	config.Traces.Batch.BatchTimeout = time.Hour

	runtime, err := Init(context.Background(), config, WithTraceExporter(exporter))
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	_, first := runtime.Tracer("test").Start(context.Background(), "blocked-export")
	first.End()
	select {
	case <-exporter.started:
	case <-time.After(time.Second):
		t.Fatal("export did not start")
	}

	produced := make(chan struct{})
	go func() {
		defer close(produced)
		for range 100 {
			_, span := runtime.Tracer("test").Start(context.Background(), "business-work")
			span.End()
		}
	}()
	select {
	case <-produced:
	case <-time.After(time.Second):
		t.Fatal("business work blocked on a saturated trace exporter")
	}

	close(exporter.release)
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if got := exporter.exportCount(); got > 3 {
		t.Fatalf("exported spans = %d, want at most active export plus queue bound", got)
	}
}

func TestRuntimeRestoresGlobalsItRegistered(t *testing.T) {
	previous := otel.GetTracerProvider()
	previousMeter := otel.GetMeterProvider()
	previousPropagator := otel.GetTextMapPropagator()
	config := DefaultConfig("orders", "1.2.3")
	config.RegisterGlobal = true
	config.Metrics.ExportInterval = time.Hour

	runtime, err := Init(
		context.Background(),
		config,
		WithTraceExporter(&recordingSpanExporter{}),
		WithMetricExporter(&recordingMetricExporter{}),
	)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if otel.GetTracerProvider() != runtime.TracerProvider() {
		t.Fatal("Init() did not register its tracer provider")
	}
	if otel.GetTextMapPropagator() != runtime.Propagator() {
		t.Fatal("Init() did not register its propagator")
	}
	if otel.GetMeterProvider() != runtime.MeterProvider() {
		t.Fatal("Init() did not register its meter provider")
	}

	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if otel.GetTracerProvider() != previous {
		t.Fatal("Shutdown() did not restore the previous tracer provider")
	}
	if otel.GetTextMapPropagator() != previousPropagator {
		t.Fatal("Shutdown() did not restore the previous propagator")
	}
	if otel.GetMeterProvider() != previousMeter {
		t.Fatal("Shutdown() did not restore the previous meter provider")
	}
}

func TestRuntimeReturnsShutdownFailuresOnEveryCall(t *testing.T) {
	want := errors.New("exporter shutdown failed")
	exporter := &recordingSpanExporter{shutdownErr: want}
	config := DefaultConfig("orders", "1.2.3")
	config.RegisterGlobal = false
	config.Metrics.Enabled = false

	runtime, err := Init(context.Background(), config, WithTraceExporter(exporter))
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	for range 2 {
		if err := runtime.Shutdown(context.Background()); !errors.Is(err, want) {
			t.Fatalf("Shutdown() error = %v, want error wrapping %v", err, want)
		}
	}
}

func TestRuntimeReturnsMetricShutdownFailures(t *testing.T) {
	want := errors.New("metric exporter shutdown failed")
	exporter := &recordingMetricExporter{shutdownErr: want}
	config := DefaultConfig("orders", "1.2.3")
	config.RegisterGlobal = false
	config.Traces.Enabled = false
	config.Metrics.ExportInterval = time.Hour

	runtime, err := Init(context.Background(), config, WithMetricExporter(exporter))
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if err := runtime.Shutdown(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Shutdown() error = %v, want %v", err, want)
	}
}

func TestJoinDistinctAggregatesIndependentFailures(t *testing.T) {
	t.Parallel()

	first := errors.New("first")
	second := errors.New("second")
	err := joinDistinct(first, second)
	if !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("joinDistinct() error = %v, want both failures", err)
	}
}

func TestRuntimeAggregatesTraceAndMetricFlushFailures(t *testing.T) {
	traceFailure := errors.New("trace export failed")
	metricFailure := errors.New("metric flush failed")
	traceExporter := &recordingSpanExporter{exportErr: traceFailure}
	metricExporter := &recordingMetricExporter{flushErr: metricFailure}
	config := DefaultConfig("orders", "1.2.3")
	config.RegisterGlobal = false
	config.Traces.Sampler.Ratio = 1
	config.Metrics.ExportInterval = time.Hour
	runtime, err := Init(
		context.Background(),
		config,
		WithTraceExporter(traceExporter),
		WithMetricExporter(metricExporter),
	)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	_, span := runtime.Tracer("test").Start(context.Background(), "operation")
	span.End()
	if err := runtime.ForceFlush(context.Background()); !errors.Is(err, traceFailure) || !errors.Is(err, metricFailure) {
		t.Fatalf("ForceFlush() error = %v, want both failures", err)
	}
	_ = runtime.Shutdown(context.Background())
}

func TestRuntimeOwnsMetricExportAndShutdown(t *testing.T) {
	exporter := &recordingMetricExporter{}
	config := DefaultConfig("orders", "1.2.3")
	config.RegisterGlobal = false
	config.Traces.Enabled = false
	config.Metrics.ExportInterval = time.Hour

	runtime, err := Init(context.Background(), config, WithMetricExporter(exporter))
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if runtime.MeterProvider() == nil {
		t.Fatal("MeterProvider() = nil, want standard provider")
	}
	counter, err := runtime.Meter("test").Int64Counter("jobs.processed")
	if err != nil {
		t.Fatalf("Int64Counter() error = %v", err)
	}
	counter.Add(context.Background(), 1)

	if err := runtime.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush() error = %v", err)
	}
	if exporter.exportCount() != 1 {
		t.Fatalf("metric exports = %d, want 1", exporter.exportCount())
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown() error = %v", err)
	}
	if exporter.shutdownCount() != 1 {
		t.Fatalf("metric exporter shutdowns = %d, want 1", exporter.shutdownCount())
	}
}

func TestInitCleansUpTraceProviderAfterMetricConstructionFailure(t *testing.T) {
	exporter := &recordingSpanExporter{}
	config := DefaultConfig("orders", "1.2.3")
	config.RegisterGlobal = false
	config.Metrics.Exporter.TLS.Insecure = false
	config.Metrics.Exporter.TLS.CAFile = "missing-ca.pem"

	if _, err := Init(context.Background(), config, WithTraceExporter(exporter)); err == nil {
		t.Fatal("Init() error = nil, want metric construction error")
	}
	if exporter.shutdownCount() != 1 {
		t.Fatalf("trace exporter shutdowns = %d, want partial initialization cleanup", exporter.shutdownCount())
	}
}

func TestInitRejectsDuplicateGlobalRuntimeAndCleansUp(t *testing.T) {
	config := DefaultConfig("orders", "1.2.3")
	config.Metrics.ExportInterval = time.Hour
	first, err := Init(
		context.Background(),
		config,
		WithTraceExporter(&recordingSpanExporter{}),
		WithMetricExporter(&recordingMetricExporter{}),
	)
	if err != nil {
		t.Fatalf("first Init() error = %v", err)
	}
	defer func() { _ = first.Shutdown(context.Background()) }()
	secondExporter := &recordingSpanExporter{}
	secondMetricExporter := &recordingMetricExporter{}
	if _, err := Init(
		context.Background(),
		config,
		WithTraceExporter(secondExporter),
		WithMetricExporter(secondMetricExporter),
	); !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("second Init() error = %v, want %v", err, ErrAlreadyInitialized)
	}
	if secondExporter.shutdownCount() != 1 {
		t.Fatalf("second exporter shutdowns = %d, want cleanup", secondExporter.shutdownCount())
	}
	if secondMetricExporter.shutdownCount() != 1 {
		t.Fatalf("second metric exporter shutdowns = %d, want cleanup", secondMetricExporter.shutdownCount())
	}
}

func TestInitBoundsCleanupWithoutCallerCancellation(t *testing.T) {
	exporter := newCleanupProbeSpanExporter()
	config := DefaultConfig("orders", "1.2.3")
	config.RegisterGlobal = false
	config.Metrics.Enabled = false
	config.ShutdownTimeout = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	started := time.Now()
	_, err := Init(
		ctx,
		config,
		WithTraceExporter(exporter),
		optionFunc(func(options *options) {
			options.buildSampler = func(telemetrytrace.Config) (trace.Sampler, error) {
				return nil, errors.New("sampler construction failed")
			}
		}),
	)
	if err == nil {
		t.Fatal("Init() error = nil, want sampler construction failure")
	}
	if exporter.contextWasCanceled.Load() {
		t.Fatal("cleanup inherited caller cancellation")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Init() error = %v, want bounded cleanup deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Init() cleanup took %s, want bounded return", elapsed)
	}
}

func TestDuplicateInitCleanupDoesNotHoldGlobalLock(t *testing.T) {
	config := DefaultConfig("orders", "1.2.3")
	config.Metrics.Enabled = false
	active, err := Init(context.Background(), config, WithTraceExporter(&recordingSpanExporter{}))
	if err != nil {
		t.Fatalf("first Init() error = %v", err)
	}

	blocked := newBlockingShutdownSpanExporter()
	duplicateDone := make(chan error, 1)
	go func() {
		_, duplicateErr := Init(context.Background(), config, WithTraceExporter(blocked))
		duplicateDone <- duplicateErr
	}()
	select {
	case <-blocked.started:
	case <-time.After(time.Second):
		t.Fatal("duplicate cleanup did not start")
	}

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- active.Shutdown(context.Background()) }()
	select {
	case shutdownErr := <-shutdownDone:
		if shutdownErr != nil {
			t.Fatalf("active Shutdown() error = %v", shutdownErr)
		}
	case <-time.After(100 * time.Millisecond):
		close(blocked.release)
		<-duplicateDone
		<-shutdownDone
		t.Fatal("duplicate cleanup held the global runtime lock")
	}

	close(blocked.release)
	if duplicateErr := <-duplicateDone; !errors.Is(duplicateErr, ErrAlreadyInitialized) {
		t.Fatalf("duplicate Init() error = %v, want %v", duplicateErr, ErrAlreadyInitialized)
	}
}

func TestShutdownDoesNotReplaceExternallyChangedGlobal(t *testing.T) {
	previous := otel.GetTracerProvider()
	config := DefaultConfig("orders", "1.2.3")
	config.Metrics.Enabled = false
	runtime, err := Init(context.Background(), config, WithTraceExporter(&recordingSpanExporter{}))
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	external := trace.NewTracerProvider()
	otel.SetTracerProvider(external)
	defer func() {
		otel.SetTracerProvider(previous)
		_ = external.Shutdown(context.Background())
	}()

	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if otel.GetTracerProvider() != external {
		t.Fatal("Shutdown() replaced an externally installed tracer provider")
	}
}

func TestDisabledRuntimeNeedsNoExporters(t *testing.T) {
	config := DefaultConfig("orders", "1.2.3")
	config.RegisterGlobal = false
	config.Traces.Enabled = false
	config.Metrics.Enabled = false

	runtime, err := Init(context.Background(), config, nil)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if _, ok := runtime.TracerProvider().(tracenoop.TracerProvider); !ok {
		t.Fatalf("TracerProvider() = %T, want no-op provider", runtime.TracerProvider())
	}
	if err := runtime.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush() error = %v", err)
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestInitRejectsInvalidConfig(t *testing.T) {
	config := DefaultConfig("", "1.2.3")
	if _, err := Init(context.Background(), config); err == nil {
		t.Fatal("Init() error = nil, want validation error")
	}
}

func TestInitReportsTraceExporterConstructionFailure(t *testing.T) {
	config := DefaultConfig("orders", "1.2.3")
	config.RegisterGlobal = false
	config.Metrics.Enabled = false
	config.Traces.Exporter.TLS.Insecure = false
	config.Traces.Exporter.TLS.CAFile = "missing-ca.pem"
	if _, err := Init(context.Background(), config); err == nil {
		t.Fatal("Init() error = nil, want trace exporter construction error")
	}
}

func TestInitPropagatesInternalConstructionFailures(t *testing.T) {
	want := errors.New("construction failed")
	config := DefaultConfig("orders", "1.2.3")
	config.RegisterGlobal = false

	t.Run("resource", func(t *testing.T) {
		_, err := Init(context.Background(), config, optionFunc(func(options *options) {
			options.buildResource = func(context.Context, Config) (*resource.Resource, error) {
				return nil, want
			}
		}))
		if !errors.Is(err, want) {
			t.Fatalf("Init() error = %v, want %v", err, want)
		}
	})

	t.Run("propagator", func(t *testing.T) {
		_, err := Init(context.Background(), config, optionFunc(func(options *options) {
			options.buildPropagator = func(telemetrypropagation.Config) (*telemetrypropagation.Policy, error) {
				return nil, want
			}
		}))
		if !errors.Is(err, want) {
			t.Fatalf("Init() error = %v, want %v", err, want)
		}
	})

	t.Run("sampler", func(t *testing.T) {
		exporter := &recordingSpanExporter{}
		samplerConfig := config
		samplerConfig.Metrics.Enabled = false
		_, err := Init(
			context.Background(),
			samplerConfig,
			WithTraceExporter(exporter),
			optionFunc(func(options *options) {
				options.buildSampler = func(telemetrytrace.Config) (trace.Sampler, error) {
					return nil, want
				}
			}),
		)
		if !errors.Is(err, want) || exporter.shutdownCount() != 1 {
			t.Fatalf("Init() error/shutdowns = %v/%d, want failure and cleanup", err, exporter.shutdownCount())
		}
	})

	t.Run("metric options", func(t *testing.T) {
		exporter := &recordingSpanExporter{}
		_, err := Init(
			context.Background(),
			config,
			WithTraceExporter(exporter),
			WithMetricExporter(&recordingMetricExporter{}),
			optionFunc(func(options *options) {
				options.buildMetricOptions = func(telemetrymetric.Config) ([]metric.Option, error) {
					return nil, want
				}
			}),
		)
		if !errors.Is(err, want) || exporter.shutdownCount() != 1 {
			t.Fatalf("Init() error/shutdowns = %v/%d, want failure and cleanup", err, exporter.shutdownCount())
		}
	})
}

type recordingSpanExporter struct {
	mu          sync.Mutex
	exports     int
	shutdowns   int
	exportErr   error
	shutdownErr error
}

type blockingSpanExporter struct {
	recordingSpanExporter
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

type cleanupProbeSpanExporter struct {
	recordingSpanExporter
	contextWasCanceled atomic.Bool
}

func newCleanupProbeSpanExporter() *cleanupProbeSpanExporter {
	return &cleanupProbeSpanExporter{}
}

func (e *cleanupProbeSpanExporter) Shutdown(ctx context.Context) error {
	if ctx.Err() != nil {
		e.contextWasCanceled.Store(true)
	}
	<-ctx.Done()
	return ctx.Err()
}

type blockingShutdownSpanExporter struct {
	recordingSpanExporter
	started chan struct{}
	release chan struct{}
}

func newBlockingShutdownSpanExporter() *blockingShutdownSpanExporter {
	return &blockingShutdownSpanExporter{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (e *blockingShutdownSpanExporter) Shutdown(ctx context.Context) error {
	close(e.started)
	select {
	case <-e.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func newBlockingSpanExporter() *blockingSpanExporter {
	return &blockingSpanExporter{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (e *blockingSpanExporter) ExportSpans(ctx context.Context, spans []trace.ReadOnlySpan) error {
	e.once.Do(func() { close(e.started) })
	select {
	case <-e.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return e.recordingSpanExporter.ExportSpans(ctx, spans)
}

type recordingMetricExporter struct {
	mu          sync.Mutex
	exports     int
	shutdowns   int
	exportErr   error
	flushErr    error
	shutdownErr error
}

func (e *recordingMetricExporter) Temporality(metric.InstrumentKind) metricdata.Temporality {
	return metricdata.CumulativeTemporality
}

func (e *recordingMetricExporter) Aggregation(kind metric.InstrumentKind) metric.Aggregation {
	return metric.DefaultAggregationSelector(kind)
}

func (e *recordingMetricExporter) Export(context.Context, *metricdata.ResourceMetrics) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.exports++
	return e.exportErr
}

func (e *recordingMetricExporter) ForceFlush(context.Context) error {
	return e.flushErr
}

func (e *recordingMetricExporter) Shutdown(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.shutdowns++
	return e.shutdownErr
}

func (e *recordingMetricExporter) exportCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.exports
}

func (e *recordingMetricExporter) shutdownCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.shutdowns
}

func (e *recordingSpanExporter) ExportSpans(_ context.Context, spans []trace.ReadOnlySpan) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.exports += len(spans)
	return e.exportErr
}

func (e *recordingSpanExporter) Shutdown(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.shutdowns++
	return e.shutdownErr
}

func (e *recordingSpanExporter) exportCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.exports
}

func (e *recordingSpanExporter) shutdownCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.shutdowns
}
