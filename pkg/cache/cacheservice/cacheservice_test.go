package cacheservice_test

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/cache/cacheservice"
)

type resource struct {
	starts int
	stops  int
}

func TestTransferredResourceUsesExplicitLifecycle(t *testing.T) {
	value := &resource{}
	adapter, err := cacheservice.New(cacheservice.Options[*resource]{
		Name:     "cache",
		Resource: value,
		Startup: func(context.Context, *resource) error {
			value.starts++

			return nil
		},
		Shutdown: func(context.Context, *resource) error {
			value.stops++

			return nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	component := adapter.Component()
	if err := component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if adapter.Resource() != value {
		t.Fatal("Resource() did not preserve the concrete resource")
	}
	if err := component.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if value.starts != 1 || value.stops != 1 {
		t.Fatalf("calls = (start %d, stop %d), want (1, 1)", value.starts, value.stops)
	}
}

func TestReadinessIsExplicitAndRequiresAnActiveResource(t *testing.T) {
	value := &resource{}
	probeErr := errors.New("probe failed")
	adapter, err := cacheservice.New(cacheservice.Options[*resource]{
		Name:     "cache",
		Resource: value,
		Readiness: func(context.Context, *resource) error {
			return probeErr
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	check, ok := adapter.Readiness()
	if !ok {
		t.Fatal("Readiness() did not expose the configured check")
	}
	if err := check.Run(context.Background()); !errors.Is(err, cacheservice.ErrUnavailable) {
		t.Fatalf("readiness before start = %v, want ErrUnavailable", err)
	}

	component := adapter.Component()
	if err := component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := check.Run(context.Background()); !errors.Is(err, probeErr) {
		t.Fatalf("readiness error = %v, want probe failure", err)
	}
	if err := component.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := check.Run(context.Background()); !errors.Is(err, cacheservice.ErrUnavailable) {
		t.Fatalf("readiness after stop = %v, want ErrUnavailable", err)
	}
}

func TestStartupFailureCleansTransferredResourceAndPreservesFailures(t *testing.T) {
	value := &resource{}
	startupErr := errors.New("startup failed")
	cleanupErr := errors.New("cleanup failed")
	adapter, err := cacheservice.New(cacheservice.Options[*resource]{
		Name:     "cache",
		Resource: value,
		Startup: func(context.Context, *resource) error {
			return startupErr
		},
		Shutdown: func(context.Context, *resource) error {
			value.stops++

			return cleanupErr
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = adapter.Component().Start(context.Background())
	if !errors.Is(err, startupErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("Start() error = %v, want startup and cleanup failures", err)
	}
	if value.stops != 1 {
		t.Fatalf("Shutdown() calls = %d, want 1", value.stops)
	}
}

func TestSharedResourceIsNeverClosed(t *testing.T) {
	value := &resource{}
	adapter, err := cacheservice.New(cacheservice.Options[*resource]{
		Name:     "cache",
		Resource: value,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	component := adapter.Component()
	if err := component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := component.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if value.stops != 0 {
		t.Fatalf("Shutdown() calls = %d, want 0", value.stops)
	}
	if _, ok := adapter.Readiness(); ok {
		t.Fatal("Readiness() exposed an unconfigured check")
	}
}

func TestTransferredResourceShutsDownOnlyOnce(t *testing.T) {
	value := &resource{}
	shutdownErr := errors.New("shutdown failed")
	adapter, err := cacheservice.New(cacheservice.Options[*resource]{
		Name:     "cache",
		Resource: value,
		Shutdown: func(context.Context, *resource) error {
			value.stops++

			return shutdownErr
		},
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
			t.Fatalf("Stop() error = %v, want shutdown failure", err)
		}
	}
	if value.stops != 1 {
		t.Fatalf("Shutdown() calls = %d, want 1", value.stops)
	}
}

func TestNewRejectsInvalidOptions(t *testing.T) {
	var nilResource *resource
	tests := []cacheservice.Options[*resource]{
		{},
		{Name: "cache", Resource: nilResource},
	}
	for _, options := range tests {
		_, err := cacheservice.New(options)
		if !errors.Is(err, cacheservice.ErrInvalidOptions) {
			t.Fatalf("New() error = %v, want ErrInvalidOptions", err)
		}
		var optionsError *cacheservice.OptionsError
		if !errors.As(err, &optionsError) || optionsError.Error() == "" {
			t.Fatalf("New() error = %v, want OptionsError", err)
		}
	}
}

func TestNewRejectsNilInterfaceAndAcceptsValueResource(t *testing.T) {
	if _, err := cacheservice.New(cacheservice.Options[any]{
		Name: "cache", Resource: nil,
	}); !errors.Is(err, cacheservice.ErrInvalidOptions) {
		t.Fatalf("New(nil) error = %v, want ErrInvalidOptions", err)
	}
	adapter, err := cacheservice.New(cacheservice.Options[int]{
		Name: "cache", Resource: 42,
	})
	if err != nil {
		t.Fatalf("New(value) error = %v", err)
	}
	if adapter.Resource() != 42 {
		t.Fatalf("Resource() = %d, want 42", adapter.Resource())
	}
	text, err := cacheservice.New(cacheservice.Options[string]{
		Name: "cache", Resource: "shared",
	})
	if err != nil {
		t.Fatalf("New(string) error = %v", err)
	}
	if text.Resource() != "shared" {
		t.Fatalf("Resource() = %q, want shared", text.Resource())
	}
}

func TestStartupErrorUsesSafeDiagnostics(t *testing.T) {
	validation := errors.New("validation")
	cleanup := errors.New("cleanup")
	withoutCleanup := &cacheservice.StartupError{Validation: validation}
	if withoutCleanup.Error() != "cache service startup validation failed" {
		t.Fatalf("Error() = %q", withoutCleanup.Error())
	}
	withCleanup := &cacheservice.StartupError{Validation: validation, Cleanup: cleanup}
	if withCleanup.Error() != "cache service startup validation and cleanup failed" {
		t.Fatalf("Error() = %q", withCleanup.Error())
	}
	if !errors.Is(withCleanup, validation) || !errors.Is(withCleanup, cleanup) {
		t.Fatalf("StartupError does not preserve causes: %v", withCleanup)
	}
}
