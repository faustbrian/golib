// Package exampleconfig maps standard OpenTelemetry environment variables to
// the explicit telemetry configuration used by the examples.
package exampleconfig

import (
	"errors"
	"net/url"
	"os"
	"strings"

	telemetry "github.com/faustbrian/golib/pkg/telemetry"
)

var (
	errNilConfig       = errors.New("telemetry config is nil")
	errInvalidEndpoint = errors.New("OTLP endpoint must be an absolute HTTP or HTTPS URL without credentials, query, or fragment")
	errInvalidProtocol = errors.New("OTLP protocol must be grpc or http/protobuf")
	errGRPCPath        = errors.New("OTLP gRPC endpoint must not contain a path")
)

// ApplyEnvironment applies the standard generic OTLP endpoint and protocol.
func ApplyEnvironment(config *telemetry.Config) error {
	if config == nil {
		return errNilConfig
	}

	endpoint, err := parseEndpoint(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if err != nil {
		return err
	}
	protocol, err := parseProtocol(os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"), config.Traces.Exporter.Protocol)
	if err != nil {
		return err
	}
	if endpoint == nil {
		return applyProtocol(config, protocol, "")
	}

	config.Traces.Exporter.Endpoint = endpoint.Host
	config.Metrics.Exporter.Endpoint = endpoint.Host
	config.Traces.Exporter.TLS.Insecure = endpoint.Scheme == "http"
	config.Metrics.Exporter.TLS.Insecure = endpoint.Scheme == "http"

	return applyProtocol(config, protocol, strings.TrimSuffix(endpoint.Path, "/"))
}

func parseEndpoint(value string) (*url.URL, error) {
	if value == "" {
		return nil, nil
	}
	endpoint, err := url.Parse(value)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errInvalidEndpoint
	}
	return endpoint, nil
}

func parseProtocol(value string, fallback telemetry.Protocol) (telemetry.Protocol, error) {
	if value == "" {
		return fallback, nil
	}
	switch telemetry.Protocol(value) {
	case telemetry.ProtocolGRPC, telemetry.ProtocolHTTPProtobuf:
		return telemetry.Protocol(value), nil
	default:
		return "", errInvalidProtocol
	}
}

func applyProtocol(config *telemetry.Config, protocol telemetry.Protocol, pathPrefix string) error {
	config.Traces.Exporter.Protocol = protocol
	config.Metrics.Exporter.Protocol = protocol
	if protocol == telemetry.ProtocolGRPC {
		if pathPrefix != "" {
			return errGRPCPath
		}
		config.Traces.Exporter.URLPath = ""
		config.Metrics.Exporter.URLPath = ""
		return nil
	}
	config.Traces.Exporter.URLPath = pathPrefix + "/v1/traces"
	config.Metrics.Exporter.URLPath = pathPrefix + "/v1/metrics"
	return nil
}
