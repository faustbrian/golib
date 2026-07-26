package telemetry

import (
	"math"
	"strings"
	"testing"
	"time"

	telemetrytrace "github.com/faustbrian/golib/pkg/telemetry/trace"
)

func TestDefaultConfigIsSafeAndInspectable(t *testing.T) {
	t.Parallel()

	config := DefaultConfig("orders", "1.2.3")

	if config.Service.Name != "orders" {
		t.Fatalf("service name = %q, want orders", config.Service.Name)
	}
	if config.Service.Version != "1.2.3" {
		t.Fatalf("service version = %q, want 1.2.3", config.Service.Version)
	}
	if !config.Traces.Enabled || !config.Metrics.Enabled {
		t.Fatal("traces and metrics must be enabled by default")
	}
	if config.Traces.Exporter.Protocol != ProtocolGRPC {
		t.Fatalf("trace protocol = %q, want %q", config.Traces.Exporter.Protocol, ProtocolGRPC)
	}
	if config.Traces.Exporter.Endpoint != "localhost:4317" {
		t.Fatalf("trace endpoint = %q, want localhost:4317", config.Traces.Exporter.Endpoint)
	}
	if config.Metrics.Exporter.Endpoint != "localhost:4317" {
		t.Fatalf("metric endpoint = %q, want localhost:4317", config.Metrics.Exporter.Endpoint)
	}
	if config.Traces.Exporter.Timeout != 10*time.Second {
		t.Fatalf("trace timeout = %s, want 10s", config.Traces.Exporter.Timeout)
	}
	if config.ShutdownTimeout != 10*time.Second {
		t.Fatalf("shutdown timeout = %s, want 10s", config.ShutdownTimeout)
	}
	if config.Traces.Sampler.Mode != telemetrytrace.ModeRatio || !config.Traces.Sampler.ParentBased {
		t.Fatalf("default sampler = %+v, want parent-based ratio", config.Traces.Sampler)
	}
	if config.Metrics.CardinalityLimit != 1_000 {
		t.Fatalf("metric cardinality limit = %d, want 1000", config.Metrics.CardinalityLimit)
	}
	if config.Propagation.MaxHeaderBytes != 8*1_024 || config.Propagation.BaggageEnabled {
		t.Fatalf("default propagation = %+v, want bounded trace context only", config.Propagation)
	}
}

func TestConfigValidationRejectsUnsafeValues(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*Config){
		"missing service name":  func(config *Config) { config.Service.Name = "" },
		"invalid protocol":      func(config *Config) { config.Traces.Exporter.Protocol = Protocol("udp") },
		"empty endpoint":        func(config *Config) { config.Metrics.Exporter.Endpoint = "" },
		"negative timeout":      func(config *Config) { config.ShutdownTimeout = -time.Second },
		"unbounded queue":       func(config *Config) { config.Traces.Batch.MaxQueueSize = 0 },
		"zero batch size":       func(config *Config) { config.Traces.Batch.MaxExportBatchSize = 0 },
		"oversized batch":       func(config *Config) { config.Traces.Batch.MaxExportBatchSize = 3_000 },
		"batch timeout":         func(config *Config) { config.Traces.Batch.BatchTimeout = 0 },
		"invalid sampler":       func(config *Config) { config.Traces.Sampler.Mode = telemetrytrace.Mode("invalid") },
		"NaN sampler":           func(config *Config) { config.Traces.Sampler.Ratio = math.NaN() },
		"metric interval":       func(config *Config) { config.Metrics.ExportInterval = 0 },
		"metric cardinality":    func(config *Config) { config.Metrics.CardinalityLimit = 0 },
		"propagation bounds":    func(config *Config) { config.Propagation.MaxHeaderBytes = 0 },
		"invalid compression":   func(config *Config) { config.Metrics.Exporter.Compression = Compression("brotli") },
		"export timeout":        func(config *Config) { config.Traces.Exporter.Timeout = 0 },
		"retry interval":        func(config *Config) { config.Metrics.Exporter.Retry.MaxInterval = 0 },
		"incomplete client TLS": func(config *Config) { config.Traces.Exporter.TLS.CertificateFile = "client.pem" },
		"plaintext with TLS": func(config *Config) {
			config.Traces.Exporter.TLS.CAFile = "collector-ca.pem"
		},
		"empty resource key": func(config *Config) { config.Resource[""] = "value" },
		"long resource key":  func(config *Config) { config.Resource[strings.Repeat("k", 256)] = "value" },
		"invalid resource value": func(config *Config) {
			config.Resource["invalid"] = string([]byte{0xff})
		},
		"long resource value": func(config *Config) {
			config.Resource["long"] = strings.Repeat("v", 4_097)
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			config := DefaultConfig("orders", "1.2.3")
			mutate(&config)

			if err := config.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want validation error")
			}
		})
	}
}

func TestConfigValidationRejectsInvalidResourceIdentity(t *testing.T) {
	t.Parallel()

	identityFields := map[string]func(*Config, string){
		"service name":      func(config *Config, value string) { config.Service.Name = value },
		"service version":   func(config *Config, value string) { config.Service.Version = value },
		"service namespace": func(config *Config, value string) { config.Service.Namespace = value },
		"service instance":  func(config *Config, value string) { config.Service.Instance = value },
		"environment":       func(config *Config, value string) { config.Environment = value },
	}
	invalidValues := map[string]string{
		"invalid UTF-8": string([]byte{0xff}),
		"too long":      strings.Repeat("x", 256),
	}
	for field, set := range identityFields {
		for problem, value := range invalidValues {
			t.Run(field+"/"+problem, func(t *testing.T) {
				t.Parallel()
				config := DefaultConfig("orders", "1.2.3")
				set(&config, value)
				if err := config.Validate(); err == nil {
					t.Fatal("Validate() error = nil, want invalid identity error")
				}
			})
		}
	}
}

func TestDefaultExporterHeadersAreIndependent(t *testing.T) {
	t.Parallel()

	config := DefaultConfig("orders", "1.2.3")
	config.Traces.Exporter.Headers["authorization"] = "trace-token"
	if _, exists := config.Metrics.Exporter.Headers["authorization"]; exists {
		t.Fatal("trace and metric exporter headers share mutable state")
	}
}
