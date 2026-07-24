package telemetry

import (
	"context"
	"testing"
	"time"
)

func FuzzResourceAttributes(f *testing.F) {
	f.Add("region", "eu-north-1")
	f.Add("service.name", "untrusted")
	f.Add(string([]byte{0xff}), string([]byte{0xfe}))
	f.Fuzz(func(t *testing.T, key, value string) {
		config := DefaultConfig("fuzz-service", "1.0.0")
		config.Resource[key] = value
		if err := config.Validate(); err == nil {
			if _, err := BuildResource(context.Background(), config); err != nil {
				t.Fatalf("validated resource failed to build: %v", err)
			}
		}
	})
}

func FuzzConfiguration(f *testing.F) {
	f.Add("service", "localhost:4317", "grpc", float64(0.1), int64(time.Second))
	f.Add("", "", "udp", float64(-1), int64(-1))
	f.Fuzz(func(_ *testing.T, service, endpoint, protocol string, ratio float64, timeoutNanos int64) {
		config := DefaultConfig(service, "fuzz")
		config.Traces.Exporter.Endpoint = endpoint
		config.Traces.Exporter.Protocol = Protocol(protocol)
		config.Traces.Sampler.Ratio = ratio
		config.ShutdownTimeout = time.Duration(timeoutNanos)
		_ = config.Validate()
	})
}
