package postgres

import (
	"context"
	"errors"
	"testing"

	gopostgres "github.com/faustbrian/golib/pkg/postgres"
)

func TestNewRuntimeRejectsMissingPool(t *testing.T) {
	t.Parallel()

	for _, pool := range []*gopostgres.Pool{nil, {}} {
		runtime, err := NewRuntime(pool)
		if runtime != nil || !errors.Is(err, ErrInvalidRuntimePool) {
			t.Fatalf("NewRuntime(invalid) = (%v, %v), want nil and ErrInvalidRuntimePool", runtime, err)
		}
	}
}

func TestNewRuntimeWiresControlPlanePersistence(t *testing.T) {
	t.Parallel()

	pool, err := gopostgres.New(context.Background(), gopostgres.Config{
		DSN:           "postgres://localhost/control_plane",
		StartupPolicy: gopostgres.StartupLazy,
	})
	if err != nil {
		t.Fatalf("postgres.New() error = %v", err)
	}
	t.Cleanup(func() { _ = pool.Close(context.Background()) })

	runtime, err := NewRuntime(pool)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	if runtime.Journal == nil || runtime.Audit == nil || runtime.Commands == nil ||
		runtime.Desired == nil || runtime.Readiness == nil {
		t.Fatalf("runtime = %+v, want every persistence service", runtime)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runtime.Readiness.Ready(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Ready(cancelled) error = %v, want context canceled", err)
	}
}
