// Package telemetry provides an explicit, vendor-neutral OpenTelemetry runtime.
package telemetry

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	telemetrymetric "github.com/faustbrian/golib/pkg/telemetry/metric"
	telemetrypropagation "github.com/faustbrian/golib/pkg/telemetry/propagation"
	telemetrytrace "github.com/faustbrian/golib/pkg/telemetry/trace"
)

// Protocol selects an OTLP transport.
type Protocol string

const (
	// ProtocolGRPC exports OTLP using gRPC.
	ProtocolGRPC Protocol = "grpc"
	// ProtocolHTTPProtobuf exports OTLP using HTTP and protobuf.
	ProtocolHTTPProtobuf Protocol = "http/protobuf"
)

// Compression selects exporter payload compression.
type Compression string

const (
	// CompressionNone disables payload compression.
	CompressionNone Compression = "none"
	// CompressionGZIP enables gzip payload compression.
	CompressionGZIP Compression = "gzip"
)

// Config describes the complete runtime. Constructing it has no side effects.
type Config struct {
	Service         ServiceConfig
	Environment     string
	Resource        map[string]string
	Traces          TraceConfig
	Metrics         MetricConfig
	Propagation     telemetrypropagation.Config
	RegisterGlobal  bool
	ShutdownTimeout time.Duration
}

// ServiceConfig identifies the process producing telemetry.
type ServiceConfig struct {
	Name      string
	Version   string
	Namespace string
	Instance  string
}

// TraceConfig controls the trace provider and exporter.
type TraceConfig struct {
	Enabled  bool
	Exporter ExporterConfig
	Batch    BatchConfig
	Sampler  SamplerConfig
}

// MetricConfig controls the metric provider and exporter.
type MetricConfig struct {
	Enabled          bool
	Exporter         ExporterConfig
	ExportInterval   time.Duration
	ExportTimeout    time.Duration
	CardinalityLimit int
	Views            []telemetrymetric.ViewConfig
}

// ExporterConfig controls an OTLP exporter.
type ExporterConfig struct {
	Protocol    Protocol
	Endpoint    string
	URLPath     string
	Headers     map[string]string
	Compression Compression
	TLS         TLSConfig
	Retry       RetryConfig
	Timeout     time.Duration
}

// TLSConfig controls transport security. Insecure is intended for a local or
// same-cluster Collector connection and must be selected explicitly.
type TLSConfig struct {
	Insecure           bool
	CAFile             string
	CertificateFile    string
	PrivateKeyFile     string
	ServerName         string
	InsecureSkipVerify bool
}

// RetryConfig bounds exporter retry behavior.
type RetryConfig struct {
	Enabled         bool
	InitialInterval time.Duration
	MaxInterval     time.Duration
	MaxElapsedTime  time.Duration
}

// BatchConfig bounds trace batching and its memory use.
type BatchConfig struct {
	MaxQueueSize       int
	MaxExportBatchSize int
	BatchTimeout       time.Duration
	ExportTimeout      time.Duration
}

// SamplerConfig controls parent-based ratio sampling.
type SamplerConfig struct {
	Mode        telemetrytrace.Mode
	Ratio       float64
	ParentBased bool
}

// DefaultConfig returns inspectable defaults suitable for exporting to a
// Collector sidecar or cluster-local agent.
func DefaultConfig(serviceName, serviceVersion string) Config {
	exporter := ExporterConfig{
		Protocol:    ProtocolGRPC,
		Endpoint:    "localhost:4317",
		Headers:     make(map[string]string),
		Compression: CompressionGZIP,
		TLS:         TLSConfig{Insecure: true},
		Retry: RetryConfig{
			Enabled:         true,
			InitialInterval: 5 * time.Second,
			MaxInterval:     30 * time.Second,
			MaxElapsedTime:  time.Minute,
		},
		Timeout: 10 * time.Second,
	}
	metricExporter := exporter
	metricExporter.Headers = make(map[string]string)

	return Config{
		Service:  ServiceConfig{Name: serviceName, Version: serviceVersion},
		Resource: make(map[string]string),
		Traces: TraceConfig{
			Enabled:  true,
			Exporter: exporter,
			Batch: BatchConfig{
				MaxQueueSize:       2_048,
				MaxExportBatchSize: 512,
				BatchTimeout:       5 * time.Second,
				ExportTimeout:      10 * time.Second,
			},
			Sampler: SamplerConfig{
				Mode:        telemetrytrace.ModeRatio,
				Ratio:       0.1,
				ParentBased: true,
			},
		},
		Metrics: MetricConfig{
			Enabled:          true,
			Exporter:         metricExporter,
			ExportInterval:   time.Minute,
			ExportTimeout:    30 * time.Second,
			CardinalityLimit: 1_000,
		},
		Propagation:     telemetrypropagation.DefaultConfig(),
		RegisterGlobal:  true,
		ShutdownTimeout: 10 * time.Second,
	}
}

// Validate reports every invalid setting in the configuration.
func (c Config) Validate() error {
	var errs []error
	if c.Service.Name == "" {
		errs = append(errs, errors.New("service name is required"))
	}
	identity := []struct {
		name  string
		value string
	}{
		{name: "service name", value: c.Service.Name},
		{name: "service version", value: c.Service.Version},
		{name: "service namespace", value: c.Service.Namespace},
		{name: "service instance", value: c.Service.Instance},
		{name: "environment", value: c.Environment},
	}
	for _, field := range identity {
		if len(field.value) > 255 || !utf8.ValidString(field.value) {
			errs = append(errs, fmt.Errorf("%s must be valid UTF-8 and at most 255 bytes", field.name))
		}
	}
	if c.ShutdownTimeout <= 0 {
		errs = append(errs, errors.New("shutdown timeout must be positive"))
	}
	for key, value := range c.Resource {
		if _, reserved := reservedResourceKeys[key]; reserved {
			errs = append(errs, fmt.Errorf("resource attribute %q is reserved", key))
		}
		if key == "" || len(key) > 255 || !utf8.ValidString(key) {
			errs = append(errs, fmt.Errorf("resource attribute key %q is invalid", key))
		}
		if len(value) > 4_096 || !utf8.ValidString(value) {
			errs = append(errs, fmt.Errorf("resource attribute %q value is invalid", key))
		}
	}
	if c.Traces.Enabled {
		errs = append(errs, c.Traces.validate()...)
	}
	if c.Metrics.Enabled {
		errs = append(errs, c.Metrics.validate()...)
	}
	if _, err := telemetrypropagation.New(c.Propagation); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

func (c TraceConfig) validate() []error {
	errs := c.Exporter.validate("trace")
	if c.Batch.MaxQueueSize <= 0 {
		errs = append(errs, errors.New("trace batch max queue size must be positive"))
	}
	if c.Batch.MaxExportBatchSize <= 0 || c.Batch.MaxExportBatchSize > c.Batch.MaxQueueSize {
		errs = append(errs, errors.New("trace batch size must be positive and no greater than queue size"))
	}
	if c.Batch.BatchTimeout <= 0 || c.Batch.ExportTimeout <= 0 {
		errs = append(errs, errors.New("trace batch timeouts must be positive"))
	}
	if _, err := telemetrytrace.NewSampler(telemetrytrace.Config{
		Mode:        c.Sampler.Mode,
		Ratio:       c.Sampler.Ratio,
		ParentBased: c.Sampler.ParentBased,
	}); err != nil {
		errs = append(errs, err)
	}
	return errs
}

func (c MetricConfig) validate() []error {
	errs := c.Exporter.validate("metric")
	if c.ExportInterval <= 0 || c.ExportTimeout <= 0 {
		errs = append(errs, errors.New("metric export interval and timeout must be positive"))
	}
	if _, err := telemetrymetric.Options(telemetrymetric.Config{
		CardinalityLimit: c.CardinalityLimit,
		Views:            c.Views,
	}); err != nil {
		errs = append(errs, err)
	}
	return errs
}

func (c ExporterConfig) validate(signal string) []error {
	var errs []error
	if c.Protocol != ProtocolGRPC && c.Protocol != ProtocolHTTPProtobuf {
		errs = append(errs, fmt.Errorf("%s exporter protocol %q is unsupported", signal, c.Protocol))
	}
	if c.Endpoint == "" {
		errs = append(errs, fmt.Errorf("%s exporter endpoint is required", signal))
	}
	if c.Compression != CompressionNone && c.Compression != CompressionGZIP {
		errs = append(errs, fmt.Errorf("%s exporter compression %q is unsupported", signal, c.Compression))
	}
	if c.Timeout <= 0 {
		errs = append(errs, fmt.Errorf("%s exporter timeout must be positive", signal))
	}
	if c.Retry.Enabled && (c.Retry.InitialInterval <= 0 || c.Retry.MaxInterval <= 0 || c.Retry.MaxElapsedTime <= 0) {
		errs = append(errs, fmt.Errorf("%s exporter retry intervals must be positive", signal))
	}
	if (c.TLS.CertificateFile == "") != (c.TLS.PrivateKeyFile == "") {
		errs = append(errs, fmt.Errorf("%s exporter client certificate and private key must be configured together", signal))
	}
	if c.TLS.Insecure && (c.TLS.CAFile != "" || c.TLS.CertificateFile != "" ||
		c.TLS.PrivateKeyFile != "" || c.TLS.ServerName != "" || c.TLS.InsecureSkipVerify) {
		errs = append(errs, fmt.Errorf("%s exporter plaintext mode cannot include TLS settings", signal))
	}
	return errs
}
