package main

import (
	"context"

	"github.com/faustbrian/golib/pkg/queue-control-plane/apihttp"
	telemetry "github.com/faustbrian/golib/pkg/telemetry"
)

const telemetryServiceName = "go-queue-control-plane"

func productionTelemetryConfig(config Config, build apihttp.BuildInfo) telemetry.Config {
	settings := telemetry.DefaultConfig(telemetryServiceName, build.Version)
	settings.Service.Instance = config.TelemetryInstance
	settings.Environment = config.TelemetryEnvironment
	settings.RegisterGlobal = false
	settings.Traces.Exporter.Endpoint = config.TelemetryEndpoint
	settings.Metrics.Exporter.Endpoint = config.TelemetryEndpoint
	settings.Traces.Exporter.TLS.Insecure = config.TelemetryInsecure
	settings.Metrics.Exporter.TLS.Insecure = config.TelemetryInsecure
	settings.Traces.Exporter.TLS.CAFile = config.TelemetryCAFile
	settings.Metrics.Exporter.TLS.CAFile = config.TelemetryCAFile
	settings.Traces.Exporter.TLS.CertificateFile = config.TelemetryCertificateFile
	settings.Metrics.Exporter.TLS.CertificateFile = config.TelemetryCertificateFile
	settings.Traces.Exporter.TLS.PrivateKeyFile = config.TelemetryPrivateKeyFile
	settings.Metrics.Exporter.TLS.PrivateKeyFile = config.TelemetryPrivateKeyFile
	settings.Traces.Exporter.TLS.ServerName = config.TelemetryServerName
	settings.Metrics.Exporter.TLS.ServerName = config.TelemetryServerName
	protocol := telemetry.Protocol(config.TelemetryProtocol)
	settings.Traces.Exporter.Protocol = protocol
	settings.Metrics.Exporter.Protocol = protocol

	return settings
}

func buildProductionTelemetry(
	ctx context.Context,
	config Config,
	build apihttp.BuildInfo,
) (*telemetry.Runtime, error) {
	if !config.TelemetryEnabled {
		return nil, nil
	}

	return telemetry.Init(ctx, productionTelemetryConfig(config, build))
}
