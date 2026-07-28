package telemetryservice_test

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/telemetry"
	"github.com/faustbrian/golib/pkg/telemetry/telemetryservice"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
)

func TestRequiredRuntimeStartsAndStopsExplicitProviders(t *testing.T) {
	config := localConfig()
	adapter, err := telemetryservice.New(telemetryservice.Options{
		Name:    "telemetry",
		Config:  config,
		Failure: telemetryservice.FailureRequired,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	component := adapter.Component()
	if err := component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	runtime, active := adapter.Runtime()
	if !active || runtime == nil {
		t.Fatal("Runtime() did not expose the active runtime")
	}
	if err := component.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if _, active := adapter.Runtime(); active {
		t.Fatal("Runtime() remained active after shutdown")
	}
}

func TestRequiredInitializationFailurePreventsStartup(t *testing.T) {
	config := localConfig()
	config.ShutdownTimeout = -1
	adapter, err := telemetryservice.New(telemetryservice.Options{
		Name:    "telemetry",
		Config:  config,
		Failure: telemetryservice.FailureRequired,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = adapter.Component().Start(context.Background())
	var initializationError *telemetryservice.InitializationError
	if !errors.As(err, &initializationError) {
		t.Fatalf("Start() error = %v, want InitializationError", err)
	}
	if initializationError.Error() != "telemetry service initialization failed" {
		t.Fatalf("InitializationError.Error() = %q", initializationError.Error())
	}
	if initializationError.Cause == nil ||
		!errors.Is(err, initializationError.Cause) {
		t.Fatal("InitializationError did not preserve its cause")
	}
}

func TestBestEffortInitializationFailureRemainsObservable(t *testing.T) {
	config := localConfig()
	config.ShutdownTimeout = -1
	adapter, err := telemetryservice.New(telemetryservice.Options{
		Name:    "telemetry",
		Config:  config,
		Failure: telemetryservice.FailureBestEffort,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	component := adapter.Component()
	if err := component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, active := adapter.Runtime(); active {
		t.Fatal("Runtime() exposed a failed best-effort runtime")
	}
	if err := adapter.InitializationError(); err == nil {
		t.Fatal("InitializationError() = nil, want retained failure")
	}
	if err := component.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestGlobalProviderPolicyIsCallerOwnedAndRestored(t *testing.T) {
	previous := otel.GetTracerProvider()
	config := localConfig()
	config.RegisterGlobal = true
	adapter, err := telemetryservice.New(telemetryservice.Options{
		Name:    "telemetry",
		Config:  config,
		Failure: telemetryservice.FailureRequired,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	component := adapter.Component()
	if err := component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	runtime, active := adapter.Runtime()
	if !active || runtime == nil {
		t.Fatal("Runtime() did not expose the registered runtime")
	}
	if otel.GetTracerProvider() != runtime.TracerProvider() {
		t.Fatal("caller-selected global registration was not applied")
	}
	if err := component.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if otel.GetTracerProvider() != previous {
		t.Fatal("caller-selected global registration was not restored")
	}
}

func TestShutdownFailureIsStableAndRunsOnce(t *testing.T) {
	shutdownErr := errors.New("exporter shutdown failed")
	exporter := &failingSpanExporter{err: shutdownErr}
	config := telemetry.DefaultConfig("orders", "1.2.3")
	config.RegisterGlobal = false
	config.Metrics.Enabled = false
	adapter, err := telemetryservice.New(telemetryservice.Options{
		Name:           "telemetry",
		Config:         config,
		RuntimeOptions: []telemetry.Option{telemetry.WithTraceExporter(exporter)},
		Failure:        telemetryservice.FailureRequired,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	component := adapter.Component()
	if err := component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	for range 2 {
		if err := component.Stop(context.Background()); !errors.Is(err, shutdownErr) {
			t.Fatalf("Stop() error = %v, want exporter failure", err)
		}
	}
	if exporter.shutdowns != 1 {
		t.Fatalf("exporter shutdowns = %d, want 1", exporter.shutdowns)
	}
}

func TestNewRejectsInvalidOptions(t *testing.T) {
	tests := []telemetryservice.Options{
		{Failure: telemetryservice.FailureRequired},
		{Name: "telemetry"},
		{Name: "telemetry", Failure: telemetryservice.FailurePolicy("unknown")},
	}
	for _, options := range tests {
		_, err := telemetryservice.New(options)
		if !errors.Is(err, telemetryservice.ErrInvalidOptions) {
			t.Fatalf("New(%+v) error = %v, want ErrInvalidOptions", options, err)
		}
		var optionsError *telemetryservice.OptionsError
		if !errors.As(err, &optionsError) || optionsError.Error() == "" {
			t.Fatalf("New(%+v) error = %v, want OptionsError", options, err)
		}
	}
}

func localConfig() telemetry.Config {
	config := telemetry.DefaultConfig("orders", "1.2.3")
	config.RegisterGlobal = false
	config.Traces.Enabled = false
	config.Metrics.Enabled = false

	return config
}

type failingSpanExporter struct {
	err       error
	shutdowns int
}

func (*failingSpanExporter) ExportSpans(context.Context, []trace.ReadOnlySpan) error {
	return nil
}

func (exporter *failingSpanExporter) Shutdown(context.Context) error {
	exporter.shutdowns++

	return exporter.err
}
