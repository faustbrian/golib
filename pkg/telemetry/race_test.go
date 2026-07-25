package telemetry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestConcurrentGlobalInitialization(t *testing.T) {
	config := DefaultConfig("race-test", "1.0.0")
	config.Metrics.Enabled = false
	const attempts = 32
	type result struct {
		runtime *Runtime
		err     error
	}
	results := make(chan result, attempts)
	var wait sync.WaitGroup
	for range attempts {
		wait.Add(1)
		go func() {
			defer wait.Done()
			runtime, err := Init(context.Background(), config, WithTraceExporter(&recordingSpanExporter{}))
			results <- result{runtime: runtime, err: err}
		}()
	}
	wait.Wait()
	close(results)

	initialized := 0
	for result := range results {
		switch {
		case result.err == nil:
			initialized++
			if err := result.runtime.Shutdown(context.Background()); err != nil {
				t.Fatalf("Shutdown() error = %v", err)
			}
		case !errors.Is(result.err, ErrAlreadyInitialized):
			t.Fatalf("Init() error = %v, want duplicate initialization", result.err)
		}
	}
	if initialized != 1 {
		t.Fatalf("successful initializations = %d, want 1", initialized)
	}
}

func TestConcurrentInstrumentationFlushAndShutdown(t *testing.T) {
	traceExporter := &recordingSpanExporter{}
	metricExporter := &recordingMetricExporter{}
	config := DefaultConfig("race-test", "1.0.0")
	config.RegisterGlobal = false
	config.Traces.Sampler.Ratio = 1
	config.Metrics.ExportInterval = time.Hour
	runtime, err := Init(
		context.Background(),
		config,
		WithTraceExporter(traceExporter),
		WithMetricExporter(metricExporter),
	)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	counter, err := runtime.Meter("race").Int64Counter("race.operations")
	if err != nil {
		t.Fatalf("Int64Counter() error = %v", err)
	}

	start := make(chan struct{})
	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			for range 100 {
				ctx, span := runtime.Tracer("race").Start(context.Background(), "operation")
				counter.Add(ctx, 1)
				span.End()
				_ = runtime.ForceFlush(ctx)
			}
		}()
	}
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_ = runtime.Shutdown(context.Background())
		}()
	}
	close(start)
	wait.Wait()
	_ = runtime.Shutdown(context.Background())
}
