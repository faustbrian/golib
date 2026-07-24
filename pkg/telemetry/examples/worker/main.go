// Command worker demonstrates a bounded worker telemetry lifecycle.
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	telemetry "github.com/faustbrian/golib/pkg/telemetry"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	config := telemetry.DefaultConfig("example-worker", "dev")
	if endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); endpoint != "" {
		config.Traces.Exporter.Endpoint = endpoint
		config.Metrics.Exporter.Endpoint = endpoint
	}
	config.Environment = "local"
	runtime, err := telemetry.Init(ctx, config)
	if err != nil {
		return err
	}
	counter, err := runtime.Meter("example-worker").Int64Counter("worker.jobs.processed")
	if err != nil {
		return errors.Join(err, runtime.Shutdown(context.Background()))
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return runtime.Shutdown(shutdownCtx)
		case <-ticker.C:
			jobCtx, span := runtime.Tracer("example-worker").Start(ctx, "worker.process")
			counter.Add(jobCtx, 1)
			span.End()
		}
	}
}
