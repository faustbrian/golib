package nethttp

import (
	"net/http"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func BenchmarkTransport(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		config := ClientConfig{Operation: "benchmark.request"}
		if enabled {
			name = "enabled"
			config.TracerProvider = sdktrace.NewTracerProvider()
			config.MeterProvider = sdkmetric.NewMeterProvider()
		}
		b.Run(name, func(b *testing.B) {
			transport, err := NewTransport(roundTripperFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusNoContent,
					Header:     make(http.Header),
					Body:       http.NoBody,
					Request:    request,
				}, nil
			}), config)
			if err != nil {
				b.Fatalf("NewTransport() error = %v", err)
			}
			request, err := http.NewRequest(http.MethodGet, "https://example.test/resource", nil)
			if err != nil {
				b.Fatalf("NewRequest() error = %v", err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				response, err := transport.RoundTrip(request)
				if err != nil {
					b.Fatalf("RoundTrip() error = %v", err)
				}
				_ = response.Body.Close()
			}
		})
	}
}
