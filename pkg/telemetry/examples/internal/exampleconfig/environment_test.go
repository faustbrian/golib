package exampleconfig

import (
	"errors"
	"net/url"
	"testing"

	telemetry "github.com/faustbrian/golib/pkg/telemetry"
)

func TestApplyEnvironment(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		if err := ApplyEnvironment(nil); !errors.Is(err, errNilConfig) {
			t.Fatalf("ApplyEnvironment() error = %v, want nil-config error", err)
		}
	})

	t.Run("defaults remain explicit", func(t *testing.T) {
		config := telemetry.DefaultConfig("example", "test")
		if err := ApplyEnvironment(&config); err != nil {
			t.Fatalf("ApplyEnvironment() error = %v", err)
		}
		assertExporter(t, config.Traces.Exporter, telemetry.ProtocolGRPC, "localhost:4317", "", true)
	})

	t.Run("grpc HTTP endpoint", func(t *testing.T) {
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector.example:4317")
		t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
		config := telemetry.DefaultConfig("example", "test")
		if err := ApplyEnvironment(&config); err != nil {
			t.Fatalf("ApplyEnvironment() error = %v", err)
		}
		assertExporter(t, config.Traces.Exporter, telemetry.ProtocolGRPC, "collector.example:4317", "", true)
		assertExporter(t, config.Metrics.Exporter, telemetry.ProtocolGRPC, "collector.example:4317", "", true)
	})

	t.Run("secure HTTP protobuf prefix", func(t *testing.T) {
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://collector.example:4318/tenant/")
		t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
		config := telemetry.DefaultConfig("example", "test")
		if err := ApplyEnvironment(&config); err != nil {
			t.Fatalf("ApplyEnvironment() error = %v", err)
		}
		assertExporter(t, config.Traces.Exporter, telemetry.ProtocolHTTPProtobuf, "collector.example:4318", "/tenant/v1/traces", false)
		assertExporter(t, config.Metrics.Exporter, telemetry.ProtocolHTTPProtobuf, "collector.example:4318", "/tenant/v1/metrics", false)
	})

	for name, endpoint := range map[string]string{
		"relative":    "collector.example:4317",
		"unsupported": "ftp://collector.example:4317",
		"credentials": (&url.URL{Scheme: "https", User: url.User("identity"), Host: "collector.example:4317"}).String(),
		"query":       "https://collector.example:4317?tenant=one",
		"fragment":    "https://collector.example:4317/#secret",
	} {
		t.Run("invalid endpoint "+name, func(t *testing.T) {
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
			config := telemetry.DefaultConfig("example", "test")
			if err := ApplyEnvironment(&config); !errors.Is(err, errInvalidEndpoint) {
				t.Fatalf("ApplyEnvironment() error = %v, want invalid-endpoint error", err)
			}
		})
	}

	t.Run("invalid protocol", func(t *testing.T) {
		t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "json")
		config := telemetry.DefaultConfig("example", "test")
		if err := ApplyEnvironment(&config); !errors.Is(err, errInvalidProtocol) {
			t.Fatalf("ApplyEnvironment() error = %v, want invalid-protocol error", err)
		}
	})

	t.Run("gRPC path", func(t *testing.T) {
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://collector.example:4317/tenant")
		config := telemetry.DefaultConfig("example", "test")
		if err := ApplyEnvironment(&config); !errors.Is(err, errGRPCPath) {
			t.Fatalf("ApplyEnvironment() error = %v, want gRPC-path error", err)
		}
	})
}

func assertExporter(
	t *testing.T,
	exporter telemetry.ExporterConfig,
	protocol telemetry.Protocol,
	endpoint string,
	urlPath string,
	insecure bool,
) {
	t.Helper()
	if exporter.Protocol != protocol || exporter.Endpoint != endpoint || exporter.URLPath != urlPath ||
		exporter.TLS.Insecure != insecure {
		t.Fatalf("exporter = %#v, want protocol=%q endpoint=%q path=%q insecure=%t", exporter, protocol, endpoint, urlPath, insecure)
	}
}
