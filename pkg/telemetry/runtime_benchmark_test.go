package telemetry

import (
	"context"
	"fmt"
	"testing"
)

func BenchmarkRuntimeTracing(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		b.Run(name, func(b *testing.B) {
			config := DefaultConfig("benchmark", "1.0.0")
			config.RegisterGlobal = false
			config.Metrics.Enabled = false
			config.Traces.Enabled = enabled
			config.Traces.Sampler.Ratio = 1
			options := []Option(nil)
			if enabled {
				options = append(options, WithTraceExporter(&recordingSpanExporter{}))
			}
			runtime, err := Init(context.Background(), config, options...)
			if err != nil {
				b.Fatalf("Init() error = %v", err)
			}
			defer func() { _ = runtime.Shutdown(context.Background()) }()
			tracer := runtime.Tracer("benchmark")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_, span := tracer.Start(context.Background(), "operation")
				span.End()
			}
		})
	}
}

func BenchmarkTraceExporterBatching(b *testing.B) {
	for _, batchSize := range []int{1, 64, 512} {
		b.Run(fmt.Sprintf("batch-%d", batchSize), func(b *testing.B) {
			exporter := &recordingSpanExporter{}
			config := DefaultConfig("benchmark", "1.0.0")
			config.RegisterGlobal = false
			config.Metrics.Enabled = false
			config.Traces.Sampler.Ratio = 1
			config.Traces.Batch.MaxQueueSize = 2_048
			config.Traces.Batch.MaxExportBatchSize = batchSize
			runtime, err := Init(context.Background(), config, WithTraceExporter(exporter))
			if err != nil {
				b.Fatalf("Init() error = %v", err)
			}
			defer func() { _ = runtime.Shutdown(context.Background()) }()
			tracer := runtime.Tracer("benchmark")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_, span := tracer.Start(context.Background(), "operation")
				span.End()
			}
			b.StopTimer()
			if err := runtime.ForceFlush(context.Background()); err != nil {
				b.Fatalf("ForceFlush() error = %v", err)
			}
		})
	}
}
