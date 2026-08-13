package main

import (
	"testing"

	telemetry "github.com/faustbrian/golib/pkg/telemetry"
)

func TestApplyEnvironmentNormalizesStandardOTLPEndpointURL(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector.example:4317")

	config := telemetry.DefaultConfig("example-service", "test")
	if err := applyEnvironment(&config); err != nil {
		t.Fatalf("applyEnvironment() error = %v", err)
	}

	if config.Traces.Exporter.Endpoint != "collector.example:4317" {
		t.Fatalf("trace endpoint = %q, want collector.example:4317", config.Traces.Exporter.Endpoint)
	}
	if config.Metrics.Exporter.Endpoint != "collector.example:4317" {
		t.Fatalf("metric endpoint = %q, want collector.example:4317", config.Metrics.Exporter.Endpoint)
	}
}
