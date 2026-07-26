package main

import (
	"testing"

	"github.com/faustbrian/golib/pkg/queue-control-plane/apihttp"
	telemetry "github.com/faustbrian/golib/pkg/telemetry"
)

func TestProductionTelemetryConfigUsesExplicitSecureRuntime(t *testing.T) {
	t.Parallel()

	config := productionTelemetryConfig(Config{
		TelemetryEndpoint:        "collector.telemetry.svc:4317",
		TelemetryProtocol:        "http/protobuf",
		TelemetryInsecure:        false,
		TelemetryEnvironment:     "production",
		TelemetryInstance:        "control-plane-1",
		TelemetryCAFile:          "/var/run/telemetry/ca.pem",
		TelemetryCertificateFile: "/var/run/telemetry/client.pem",
		TelemetryPrivateKeyFile:  "/var/run/telemetry/client-key.pem",
		TelemetryServerName:      "collector.telemetry.svc",
	}, apihttp.BuildInfo{Version: "v1.2.3"})

	if config.Service.Name != "go-queue-control-plane" ||
		config.Service.Version != "v1.2.3" ||
		config.Service.Instance != "control-plane-1" ||
		config.Environment != "production" ||
		config.RegisterGlobal ||
		!config.Traces.Enabled || !config.Metrics.Enabled ||
		config.Traces.Exporter.Endpoint != "collector.telemetry.svc:4317" ||
		config.Metrics.Exporter.Endpoint != "collector.telemetry.svc:4317" ||
		config.Traces.Exporter.Protocol != telemetry.ProtocolHTTPProtobuf ||
		config.Metrics.Exporter.Protocol != telemetry.ProtocolHTTPProtobuf ||
		config.Traces.Exporter.TLS.Insecure || config.Metrics.Exporter.TLS.Insecure ||
		config.Traces.Exporter.TLS.CAFile != "/var/run/telemetry/ca.pem" ||
		config.Metrics.Exporter.TLS.CertificateFile != "/var/run/telemetry/client.pem" ||
		config.Traces.Exporter.TLS.PrivateKeyFile != "/var/run/telemetry/client-key.pem" ||
		config.Metrics.Exporter.TLS.ServerName != "collector.telemetry.svc" {
		t.Fatalf("productionTelemetryConfig() = %+v", config)
	}
}
