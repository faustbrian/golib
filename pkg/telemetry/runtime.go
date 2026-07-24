package telemetry

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	telemetrymetric "github.com/faustbrian/golib/pkg/telemetry/metric"
	telemetryotlp "github.com/faustbrian/golib/pkg/telemetry/otlp"
	telemetrypropagation "github.com/faustbrian/golib/pkg/telemetry/propagation"
	telemetrytrace "github.com/faustbrian/golib/pkg/telemetry/trace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	otelpropagation "go.opentelemetry.io/otel/propagation"
	metricexport "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	traceapi "go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

var (
	// ErrAlreadyInitialized indicates that a globally registered runtime is
	// already active in this process.
	ErrAlreadyInitialized = errors.New("telemetry runtime already initialized")
)

var globalRuntime struct {
	sync.Mutex
	active *Runtime
}

// Option customizes runtime construction. Options are primarily useful for
// deterministic testing and custom Collector transports.
type Option interface {
	apply(*options)
}

type optionFunc func(*options)

func (f optionFunc) apply(options *options) {
	f(options)
}

type options struct {
	traceExporter      trace.SpanExporter
	metricExporter     metricexport.Exporter
	buildResource      func(context.Context, Config) (*resource.Resource, error)
	buildPropagator    func(telemetrypropagation.Config) (*telemetrypropagation.Policy, error)
	buildSampler       func(telemetrytrace.Config) (trace.Sampler, error)
	buildMetricOptions func(telemetrymetric.Config) ([]metricexport.Option, error)
}

// WithTraceExporter supplies a standard OpenTelemetry span exporter.
func WithTraceExporter(exporter trace.SpanExporter) Option {
	return optionFunc(func(options *options) {
		options.traceExporter = exporter
	})
}

// WithMetricExporter supplies a standard OpenTelemetry metric exporter.
func WithMetricExporter(exporter metricexport.Exporter) Option {
	return optionFunc(func(options *options) {
		options.metricExporter = exporter
	})
}

// Runtime owns the providers and their complete lifecycle.
type Runtime struct {
	config Config

	tracerProvider traceapi.TracerProvider
	meterProvider  metric.MeterProvider
	sdkTracer      *trace.TracerProvider
	sdkMeter       *metricexport.MeterProvider
	propagator     *telemetrypropagation.Policy
	traceExporter  *shutdownRecordingSpanExporter
	metricExporter *shutdownRecordingMetricExporter

	previousTracer     traceapi.TracerProvider
	previousMeter      metric.MeterProvider
	previousPropagator otelpropagation.TextMapPropagator
	registered         bool

	shutdownOnce sync.Once
	shutdownErr  error
}

// Init validates config and explicitly initializes the telemetry runtime.
func Init(ctx context.Context, config Config, opts ...Option) (*Runtime, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	settings := options{
		buildResource:      BuildResource,
		buildPropagator:    telemetrypropagation.New,
		buildSampler:       telemetrytrace.NewSampler,
		buildMetricOptions: telemetrymetric.Options,
	}
	for _, option := range opts {
		if option != nil {
			option.apply(&settings)
		}
	}

	res, err := settings.buildResource(ctx, config)
	if err != nil {
		return nil, err
	}
	propagator, err := settings.buildPropagator(config.Propagation)
	if err != nil {
		return nil, err
	}

	runtime := &Runtime{
		config:         config,
		tracerProvider: tracenoop.NewTracerProvider(),
		meterProvider:  metricnoop.NewMeterProvider(),
		propagator:     propagator,
	}
	if config.Traces.Enabled {
		if settings.traceExporter == nil {
			settings.traceExporter, err = telemetryotlp.NewTraceExporter(ctx, otlpConfig(config.Traces.Exporter))
			if err != nil {
				return nil, fmt.Errorf("construct trace exporter: %w", err)
			}
		}
		sampler, err := settings.buildSampler(telemetrytrace.Config{
			Mode:        config.Traces.Sampler.Mode,
			Ratio:       config.Traces.Sampler.Ratio,
			ParentBased: config.Traces.Sampler.ParentBased,
		})
		if err != nil {
			cleanup, cancel := boundedCleanupContext(ctx, config.ShutdownTimeout)
			defer cancel()
			return nil, errors.Join(err, settings.traceExporter.Shutdown(cleanup))
		}
		runtime.traceExporter = &shutdownRecordingSpanExporter{SpanExporter: settings.traceExporter}
		runtime.sdkTracer = trace.NewTracerProvider(
			trace.WithResource(res),
			trace.WithSampler(sampler),
			trace.WithBatcher(
				runtime.traceExporter,
				trace.WithMaxQueueSize(config.Traces.Batch.MaxQueueSize),
				trace.WithMaxExportBatchSize(config.Traces.Batch.MaxExportBatchSize),
				trace.WithBatchTimeout(config.Traces.Batch.BatchTimeout),
				trace.WithExportTimeout(config.Traces.Batch.ExportTimeout),
			),
		)
		runtime.tracerProvider = runtime.sdkTracer
	}
	if config.Metrics.Enabled {
		providerOptions, err := settings.buildMetricOptions(telemetrymetric.Config{
			CardinalityLimit: config.Metrics.CardinalityLimit,
			Views:            config.Metrics.Views,
		})
		if err != nil {
			return nil, errors.Join(err, runtime.cleanup(ctx))
		}
		if settings.metricExporter == nil {
			settings.metricExporter, err = telemetryotlp.NewMetricExporter(ctx, otlpConfig(config.Metrics.Exporter))
			if err != nil {
				return nil, errors.Join(fmt.Errorf("construct metric exporter: %w", err), runtime.cleanup(ctx))
			}
		}
		runtime.metricExporter = &shutdownRecordingMetricExporter{Exporter: settings.metricExporter}
		reader := metricexport.NewPeriodicReader(
			runtime.metricExporter,
			metricexport.WithInterval(config.Metrics.ExportInterval),
			metricexport.WithTimeout(config.Metrics.ExportTimeout),
		)
		providerOptions = append(providerOptions, metricexport.WithResource(res), metricexport.WithReader(reader))
		runtime.sdkMeter = metricexport.NewMeterProvider(providerOptions...)
		runtime.meterProvider = runtime.sdkMeter
	}

	if config.RegisterGlobal {
		globalRuntime.Lock()
		if globalRuntime.active != nil {
			globalRuntime.Unlock()
			return nil, errors.Join(ErrAlreadyInitialized, runtime.cleanup(ctx))
		}
		runtime.previousTracer = otel.GetTracerProvider()
		runtime.previousMeter = otel.GetMeterProvider()
		runtime.previousPropagator = otel.GetTextMapPropagator()
		otel.SetTracerProvider(runtime.tracerProvider)
		otel.SetMeterProvider(runtime.meterProvider)
		otel.SetTextMapPropagator(runtime.propagator)
		runtime.registered = true
		globalRuntime.active = runtime
		globalRuntime.Unlock()
	}

	return runtime, nil
}

func (r *Runtime) cleanup(ctx context.Context) error {
	bounded, cancel := boundedCleanupContext(ctx, r.config.ShutdownTimeout)
	defer cancel()
	var errs []error
	if r.sdkMeter != nil {
		errs = append(errs, r.shutdownMeter(bounded))
	}
	if r.sdkTracer != nil {
		errs = append(errs, r.shutdownTracer(bounded))
	}
	return errors.Join(errs...)
}

func boundedCleanupContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), timeout)
}

func otlpConfig(config ExporterConfig) telemetryotlp.Config {
	return telemetryotlp.Config{
		Protocol:    telemetryotlp.Protocol(config.Protocol),
		Endpoint:    config.Endpoint,
		URLPath:     config.URLPath,
		Headers:     config.Headers,
		Compression: telemetryotlp.Compression(config.Compression),
		TLS: telemetryotlp.TLSConfig{
			Insecure:           config.TLS.Insecure,
			CAFile:             config.TLS.CAFile,
			CertificateFile:    config.TLS.CertificateFile,
			PrivateKeyFile:     config.TLS.PrivateKeyFile,
			ServerName:         config.TLS.ServerName,
			InsecureSkipVerify: config.TLS.InsecureSkipVerify,
		},
		Retry: telemetryotlp.RetryConfig{
			Enabled:         config.Retry.Enabled,
			InitialInterval: config.Retry.InitialInterval,
			MaxInterval:     config.Retry.MaxInterval,
			MaxElapsedTime:  config.Retry.MaxElapsedTime,
		},
		Timeout: config.Timeout,
	}
}

// TracerProvider returns the standard OpenTelemetry tracer provider API.
func (r *Runtime) TracerProvider() traceapi.TracerProvider {
	return r.tracerProvider
}

// MeterProvider returns the standard OpenTelemetry meter provider API.
func (r *Runtime) MeterProvider() metric.MeterProvider {
	return r.meterProvider
}

// Propagator returns the runtime's standard OpenTelemetry propagator.
func (r *Runtime) Propagator() otelpropagation.TextMapPropagator {
	return r.propagator
}

// Tracer returns a standard OpenTelemetry tracer.
func (r *Runtime) Tracer(name string, options ...traceapi.TracerOption) traceapi.Tracer {
	return r.tracerProvider.Tracer(name, options...)
}

// Meter returns a standard OpenTelemetry meter.
func (r *Runtime) Meter(name string, options ...metric.MeterOption) metric.Meter {
	return r.meterProvider.Meter(name, options...)
}

// ForceFlush exports all telemetry queued by the runtime.
func (r *Runtime) ForceFlush(ctx context.Context) error {
	bounded, cancel := context.WithTimeout(ctx, r.config.ShutdownTimeout)
	defer cancel()
	var errs []error
	if r.sdkTracer != nil {
		errs = append(errs, r.sdkTracer.ForceFlush(bounded))
	}
	if r.sdkMeter != nil {
		errs = append(errs, r.sdkMeter.ForceFlush(bounded))
	}
	return errors.Join(errs...)
}

// Shutdown unregisters globals owned by this runtime, flushes providers, and
// shuts them down. Every call returns the same aggregate result.
func (r *Runtime) Shutdown(ctx context.Context) error {
	r.shutdownOnce.Do(func() {
		bounded, cancel := context.WithTimeout(ctx, r.config.ShutdownTimeout)
		defer cancel()

		if r.registered {
			globalRuntime.Lock()
			if globalRuntime.active == r {
				if otel.GetTracerProvider() == r.tracerProvider {
					otel.SetTracerProvider(r.previousTracer)
				}
				if otel.GetMeterProvider() == r.meterProvider {
					otel.SetMeterProvider(r.previousMeter)
				}
				if otel.GetTextMapPropagator() == r.propagator {
					otel.SetTextMapPropagator(r.previousPropagator)
				}
				globalRuntime.active = nil
			}
			globalRuntime.Unlock()
		}

		var errs []error
		if r.sdkMeter != nil {
			errs = append(errs, r.sdkMeter.ForceFlush(bounded))
			errs = append(errs, r.shutdownMeter(bounded))
		}
		if r.sdkTracer != nil {
			errs = append(errs, r.sdkTracer.ForceFlush(bounded))
			errs = append(errs, r.shutdownTracer(bounded))
		}
		r.shutdownErr = errors.Join(errs...)
	})

	return r.shutdownErr
}

func (r *Runtime) shutdownTracer(ctx context.Context) error {
	providerErr := r.sdkTracer.Shutdown(ctx)
	return joinDistinct(providerErr, r.traceExporter.err())
}

func (r *Runtime) shutdownMeter(ctx context.Context) error {
	providerErr := r.sdkMeter.Shutdown(ctx)
	return joinDistinct(providerErr, r.metricExporter.err())
}

func joinDistinct(primary, secondary error) error {
	if secondary == nil || errors.Is(primary, secondary) {
		return primary
	}
	return errors.Join(primary, secondary)
}

type shutdownRecordingSpanExporter struct {
	trace.SpanExporter
	mu          sync.Mutex
	shutdownErr error
}

func (exporter *shutdownRecordingSpanExporter) Shutdown(ctx context.Context) error {
	err := exporter.SpanExporter.Shutdown(ctx)
	exporter.mu.Lock()
	exporter.shutdownErr = err
	exporter.mu.Unlock()
	return err
}

func (exporter *shutdownRecordingSpanExporter) err() error {
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	return exporter.shutdownErr
}

type shutdownRecordingMetricExporter struct {
	metricexport.Exporter
	mu          sync.Mutex
	shutdownErr error
}

func (exporter *shutdownRecordingMetricExporter) Temporality(kind metricexport.InstrumentKind) metricdata.Temporality {
	return exporter.Exporter.Temporality(kind)
}

func (exporter *shutdownRecordingMetricExporter) Aggregation(kind metricexport.InstrumentKind) metricexport.Aggregation {
	return exporter.Exporter.Aggregation(kind)
}

func (exporter *shutdownRecordingMetricExporter) Shutdown(ctx context.Context) error {
	err := exporter.Exporter.Shutdown(ctx)
	exporter.mu.Lock()
	exporter.shutdownErr = err
	exporter.mu.Unlock()
	return err
}

func (exporter *shutdownRecordingMetricExporter) err() error {
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	return exporter.shutdownErr
}
