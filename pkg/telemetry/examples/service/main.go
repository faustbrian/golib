// Command service demonstrates HTTP service lifecycle and instrumentation.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	telemetry "github.com/faustbrian/golib/pkg/telemetry"
	"github.com/faustbrian/golib/pkg/telemetry/instrumentation/nethttp"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	config := telemetry.DefaultConfig("example-service", "dev")
	applyEnvironment(&config)
	runtime, err := telemetry.Init(ctx, config)
	if err != nil {
		return fmt.Errorf("initialize telemetry: %w", err)
	}

	handler, err := nethttp.NewHandler(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("content-type", "application/json")
		_, _ = writer.Write([]byte("{\"status\":\"ok\"}\n"))
	}), nethttp.ServerConfig{
		Operation:      "health.show",
		Route:          "/health",
		TracerProvider: runtime.TracerProvider(),
		MeterProvider:  runtime.MeterProvider(),
		Propagator:     runtime.Propagator(),
	})
	if err != nil {
		return errors.Join(err, runtime.Shutdown(context.Background()))
	}

	server := &http.Server{
		Addr:              environment("HTTP_ADDR", ":8080"),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	shutdown := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		shutdown <- errors.Join(server.Shutdown(shutdownCtx), runtime.Shutdown(shutdownCtx))
	}()

	listenErr := server.ListenAndServe()
	if !errors.Is(listenErr, http.ErrServerClosed) {
		return errors.Join(listenErr, runtime.Shutdown(context.Background()))
	}
	return <-shutdown
}

func applyEnvironment(config *telemetry.Config) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint != "" {
		config.Traces.Exporter.Endpoint = endpoint
		config.Metrics.Exporter.Endpoint = endpoint
	}
	if os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL") == string(telemetry.ProtocolHTTPProtobuf) {
		config.Traces.Exporter.Protocol = telemetry.ProtocolHTTPProtobuf
		config.Metrics.Exporter.Protocol = telemetry.ProtocolHTTPProtobuf
		config.Traces.Exporter.URLPath = "/v1/traces"
		config.Metrics.Exporter.URLPath = "/v1/metrics"
	}
	config.Environment = environment("DEPLOYMENT_ENVIRONMENT", "local")
}

func environment(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
