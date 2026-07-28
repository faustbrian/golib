package postgresservice_test

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/postgres/postgresservice"
)

type resource struct {
	pings    int
	closes   int
	pingErr  error
	closeErr error
}

func (pool *resource) Ping(context.Context) error {
	pool.pings++

	return pool.pingErr
}

func (pool *resource) Close(context.Context) error {
	pool.closes++

	return pool.closeErr
}

func TestConstructedPoolIsValidatedExposedAndClosed(t *testing.T) {
	pool := &resource{}
	adapter, err := postgresservice.New(postgresservice.Options{
		Name: "database",
		Construct: func(context.Context) (postgresservice.Resource, error) {
			return pool, nil
		},
		StartupPing: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	component := adapter.Component()
	if err := component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if current, ok := adapter.Pool(); !ok || current != pool {
		t.Fatalf("Pool() = (%v, %v), want constructed pool", current, ok)
	}
	if pool.pings != 1 {
		t.Fatalf("Ping() calls = %d, want 1", pool.pings)
	}
	if err := component.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if pool.closes != 1 {
		t.Fatalf("Close() calls = %d, want 1", pool.closes)
	}
}

func TestSharedPoolIsValidatedWithoutBeingClosed(t *testing.T) {
	pool := &resource{}
	adapter, err := postgresservice.New(postgresservice.Options{
		Name:        "database",
		Pool:        pool,
		StartupPing: true,
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
	if pool.pings != 1 || pool.closes != 0 {
		t.Fatalf("calls = (ping %d, close %d), want (1, 0)", pool.pings, pool.closes)
	}
}

func TestTransferredPoolClosesOnlyOnce(t *testing.T) {
	pool := &resource{}
	adapter, err := postgresservice.New(postgresservice.Options{
		Name:              "database",
		Pool:              pool,
		TransferOwnership: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	component := adapter.Component()
	if err := component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := component.Stop(context.Background()); err != nil {
		t.Fatalf("first Stop() error = %v", err)
	}
	if err := component.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}
	if pool.closes != 1 {
		t.Fatalf("Close() calls = %d, want 1", pool.closes)
	}
}

func TestReadinessUsesOnlyAnActivePool(t *testing.T) {
	pool := &resource{}
	adapter, err := postgresservice.New(postgresservice.Options{
		Name: "database",
		Pool: pool,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	check := adapter.Readiness()
	if err := check.Run(context.Background()); !errors.Is(err, postgresservice.ErrUnavailable) {
		t.Fatalf("Readiness() before start = %v, want ErrUnavailable", err)
	}

	component := adapter.Component()
	if err := component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	probeErr := errors.New("probe failed")
	pool.pingErr = probeErr
	if err := check.Run(context.Background()); !errors.Is(err, probeErr) {
		t.Fatalf("Readiness() error = %v, want probe error", err)
	}
	if err := component.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := check.Run(context.Background()); !errors.Is(err, postgresservice.ErrUnavailable) {
		t.Fatalf("Readiness() after stop = %v, want ErrUnavailable", err)
	}
}

func TestStartupPingFailureClosesOwnedPoolAndPreservesFailures(t *testing.T) {
	pingErr := errors.New("ping failed")
	closeErr := errors.New("close failed")
	pool := &resource{pingErr: pingErr, closeErr: closeErr}
	adapter, err := postgresservice.New(postgresservice.Options{
		Name: "database",
		Construct: func(context.Context) (postgresservice.Resource, error) {
			return pool, nil
		},
		StartupPing: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = adapter.Component().Start(context.Background())
	if !errors.Is(err, pingErr) || !errors.Is(err, closeErr) {
		t.Fatalf("Start() error = %v, want ping and cleanup failures", err)
	}
	if _, ok := adapter.Pool(); ok {
		t.Fatal("Pool() reported a failed startup pool")
	}
	if pool.closes != 1 {
		t.Fatalf("Close() calls = %d, want 1", pool.closes)
	}
}

func TestSharedPoolIsNotClosedAfterStartupPingFailure(t *testing.T) {
	pingErr := errors.New("ping failed")
	pool := &resource{pingErr: pingErr}
	adapter, err := postgresservice.New(postgresservice.Options{
		Name:        "database",
		Pool:        pool,
		StartupPing: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := adapter.Component().Start(context.Background()); !errors.Is(err, pingErr) {
		t.Fatalf("Start() error = %v, want ping failure", err)
	}
	if pool.closes != 0 {
		t.Fatalf("Close() calls = %d, want 0", pool.closes)
	}
}

func TestNewRejectsInvalidOptions(t *testing.T) {
	var nilPool *resource
	tests := []struct {
		name    string
		options postgresservice.Options
	}{
		{name: "blank name", options: postgresservice.Options{}},
		{name: "missing resource", options: postgresservice.Options{Name: "database"}},
		{
			name: "both resources",
			options: postgresservice.Options{
				Name: "database", Pool: &resource{},
				Construct: func(context.Context) (postgresservice.Resource, error) {
					return &resource{}, nil
				},
			},
		},
		{name: "typed nil pool", options: postgresservice.Options{Name: "database", Pool: nilPool}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := postgresservice.New(test.options)
			if !errors.Is(err, postgresservice.ErrInvalidOptions) {
				t.Fatalf("New() error = %v, want ErrInvalidOptions", err)
			}
			var optionsError *postgresservice.OptionsError
			if !errors.As(err, &optionsError) || optionsError.Error() == "" {
				t.Fatalf("New() error = %v, want OptionsError", err)
			}
		})
	}
}

func TestConstructorFailureIsPreserved(t *testing.T) {
	cause := errors.New("construct failed")
	adapter, err := postgresservice.New(postgresservice.Options{
		Name: "database",
		Construct: func(context.Context) (postgresservice.Resource, error) {
			return nil, cause
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := adapter.Component().Start(context.Background()); !errors.Is(err, cause) {
		t.Fatalf("Start() error = %v, want constructor failure", err)
	}
}

func TestStartupPingIsOptIn(t *testing.T) {
	pool := &resource{}
	adapter, err := postgresservice.New(postgresservice.Options{Name: "database", Pool: pool})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := adapter.Component().Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if pool.pings != 0 {
		t.Fatalf("Ping() calls = %d, want 0", pool.pings)
	}
}

func TestConstructorMustReturnANonNilPool(t *testing.T) {
	adapter, err := postgresservice.New(postgresservice.Options{
		Name: "database",
		Construct: func(context.Context) (postgresservice.Resource, error) {
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := adapter.Component().Start(context.Background()); !errors.Is(err, postgresservice.ErrUnavailable) {
		t.Fatalf("Start() error = %v, want ErrUnavailable", err)
	}
}

func TestStartupErrorUsesSafeDiagnostics(t *testing.T) {
	validation := errors.New("validation")
	cleanup := errors.New("cleanup")
	withoutCleanup := &postgresservice.StartupError{Validation: validation}
	if withoutCleanup.Error() != "postgres service startup validation failed" {
		t.Fatalf("Error() = %q", withoutCleanup.Error())
	}
	withCleanup := &postgresservice.StartupError{Validation: validation, Cleanup: cleanup}
	if withCleanup.Error() != "postgres service startup validation and cleanup failed" {
		t.Fatalf("Error() = %q", withCleanup.Error())
	}
	if !errors.Is(withCleanup, validation) || !errors.Is(withCleanup, cleanup) {
		t.Fatalf("StartupError does not preserve causes: %v", withCleanup)
	}
}

func TestShutdownFailureIsStableAcrossRepeatedCalls(t *testing.T) {
	closeErr := errors.New("close failed")
	pool := &resource{closeErr: closeErr}
	adapter, err := postgresservice.New(postgresservice.Options{
		Name: "database", Pool: pool, TransferOwnership: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	component := adapter.Component()
	if err := component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := component.Stop(context.Background()); !errors.Is(err, closeErr) {
		t.Fatalf("first Stop() error = %v, want close failure", err)
	}
	if err := component.Stop(context.Background()); !errors.Is(err, closeErr) {
		t.Fatalf("second Stop() error = %v, want close failure", err)
	}
	if pool.closes != 1 {
		t.Fatalf("Close() calls = %d, want 1", pool.closes)
	}
}
