package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gopostgres "github.com/faustbrian/golib/pkg/postgres"
	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/apihttp"
	"github.com/faustbrian/golib/pkg/queue-control-plane/control"
	"github.com/faustbrian/golib/pkg/queue-control-plane/fleet"
	controlpostgres "github.com/faustbrian/golib/pkg/queue-control-plane/postgres"
	"github.com/faustbrian/golib/pkg/queue-control-plane/server"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestRunProcessRejectsConfigurationBeforeDependencies(t *testing.T) {
	t.Parallel()

	called := false
	err := runProcess(context.Background(), mapEnvironment(nil), processDependencies{
		loadAccess: func(string, int64) (*server.StaticAccess, error) {
			called = true

			return nil, nil
		},
	})
	if !errors.Is(err, ErrInvalidRuntimeConfiguration) || called {
		t.Fatalf("runProcess() = %v, dependency called = %t", err, called)
	}
}

func TestRunProcessMigratesAndExitsWithoutServingDependencies(t *testing.T) {
	t.Parallel()

	migrated := false
	err := runProcess(context.Background(), mapEnvironment(map[string]string{
		"DATABASE_URL":               "postgres://database/control",
		"QUEUE_CONTROL_MIGRATE_ONLY": "true",
	}), processDependencies{
		migrate: func(_ context.Context, dsn string) error {
			if dsn != "postgres://database/control" {
				t.Fatalf("migration DSN = %q", dsn)
			}
			migrated = true

			return nil
		},
	})
	if err != nil || !migrated {
		t.Fatalf("runProcess() = %v, migrated = %t", err, migrated)
	}
}

func TestRunProcessMigrationOnlyPropagatesFailure(t *testing.T) {
	t.Parallel()

	stageErr := errors.New("migration failed")
	err := runProcess(context.Background(), mapEnvironment(map[string]string{
		"DATABASE_URL":               "postgres://database/control",
		"QUEUE_CONTROL_MIGRATE_ONLY": "true",
	}), processDependencies{
		migrate: func(context.Context, string) error { return stageErr },
	})
	if !errors.Is(err, stageErr) {
		t.Fatalf("runProcess() error = %v, want %v", err, stageErr)
	}
}

func TestRunProcessMigrationOnlyRequiresMigrator(t *testing.T) {
	t.Parallel()

	err := runProcess(context.Background(), mapEnvironment(map[string]string{
		"DATABASE_URL":               "postgres://database/control",
		"QUEUE_CONTROL_MIGRATE_ONLY": "true",
	}), processDependencies{})
	if !errors.Is(err, ErrInvalidProcessDependencies) {
		t.Fatalf("runProcess() error = %v, want %v", err, ErrInvalidProcessDependencies)
	}
}

func TestRunProcessRetainsAndExitsWithoutServingDependencies(t *testing.T) {
	t.Parallel()

	retained := false
	err := runProcess(context.Background(), mapEnvironment(map[string]string{
		"DATABASE_URL":                 "postgres://database/control",
		"QUEUE_CONTROL_RETENTION_ONLY": "true",
		"QUEUE_CONTROL_RETENTION_FILE": "/etc/control/retention.json",
	}), processDependencies{
		retain: func(_ context.Context, dsn, path string, maxBytes int64) error {
			if dsn != "postgres://database/control" ||
				path != "/etc/control/retention.json" || maxBytes != 1<<20 {
				t.Fatalf("retention input = (%q, %q, %d)", dsn, path, maxBytes)
			}
			retained = true

			return nil
		},
	})
	if err != nil || !retained {
		t.Fatalf("runProcess() = %v, retained = %t", err, retained)
	}
}

func TestRunProcessRetentionOnlyRequiresRetainer(t *testing.T) {
	t.Parallel()

	err := runProcess(context.Background(), mapEnvironment(map[string]string{
		"DATABASE_URL":                 "postgres://database/control",
		"QUEUE_CONTROL_RETENTION_ONLY": "true",
		"QUEUE_CONTROL_RETENTION_FILE": "/etc/control/retention.json",
	}), processDependencies{})
	if !errors.Is(err, ErrInvalidProcessDependencies) {
		t.Fatalf("runProcess() error = %v, want %v", err, ErrInvalidProcessDependencies)
	}
}

func TestRunProcessReturnsTelemetryShutdownFailure(t *testing.T) {
	t.Parallel()

	shutdownErr := errors.New("telemetry shutdown failed")
	stopped := false
	dependencies := validProcessDependencies(t)
	dependencies.buildTelemetry = func(context.Context, Config, apihttp.BuildInfo) (processTelemetry, error) {
		return &processTelemetryStub{stopped: &stopped, shutdownErr: shutdownErr}, nil
	}
	err := runProcess(context.Background(), mapEnvironment(map[string]string{
		"DATABASE_URL":              "postgres://database/control",
		"QUEUE_CONTROL_ACCESS_FILE": "/run/secrets/access.json",
	}), dependencies)
	if !errors.Is(err, shutdownErr) || !stopped {
		t.Fatalf("runProcess() = %v, telemetry stopped = %t", err, stopped)
	}
}

func TestRunProcessComposesAndClosesRuntime(t *testing.T) {
	t.Parallel()

	pool := lazyProcessPool(t)
	served := false
	migrated := false
	telemetryStopped := false
	listener := &processListener{}
	err := runProcess(context.Background(), mapEnvironment(map[string]string{
		"DATABASE_URL":                               "postgres://database/control",
		"QUEUE_CONTROL_ACCESS_FILE":                  "/run/secrets/access.json",
		"QUEUE_CONTROL_RUN_MIGRATIONS":               "true",
		"QUEUE_CONTROL_ALLOWED_ORIGINS":              "https://control.example",
		"QUEUE_CONTROL_LISTEN_ADDRESS":               "127.0.0.1:9090",
		"QUEUE_CONTROL_ACCESS_MAX_BYTES":             "2048",
		"QUEUE_CONTROL_KUBERNETES_TENANTS_FILE":      "/etc/control/tenants.json",
		"QUEUE_CONTROL_KUBERNETES_TENANTS_MAX_BYTES": "4096",
		"QUEUE_CONTROL_MANAGEMENT_TENANTS_FILE":      "/etc/control/management.json",
		"QUEUE_CONTROL_MANAGEMENT_TENANTS_MAX_BYTES": "8192",
	}), processDependencies{
		buildInfo: func() (apihttp.BuildInfo, error) {
			return apihttp.BuildInfo{Version: "v1.2.3", Commit: "abcdef"}, nil
		},
		buildTelemetry: func(context.Context, Config, apihttp.BuildInfo) (processTelemetry, error) {
			return &processTelemetryStub{stopped: &telemetryStopped}, nil
		},
		loadAccess: func(path string, maxBytes int64) (*server.StaticAccess, error) {
			if path != "/run/secrets/access.json" || maxBytes != 2048 {
				t.Fatalf("access input = (%q, %d)", path, maxBytes)
			}

			return applicationAccess(t), nil
		},
		migrate: func(context.Context, string) error {
			migrated = true

			return nil
		},
		retain: func(context.Context, string, string, int64) error { return nil },
		loadWorkloads: func(path string, maxBytes int64) (workloadRuntime, error) {
			if path != "/etc/control/tenants.json" || maxBytes != 4096 {
				t.Fatalf("workload input = (%q, %d)", path, maxBytes)
			}

			return workloadRuntime{
				Source:     applicationWorkloadSource{},
				Dispatcher: applicationDispatcher{},
			}, nil
		},
		loadManagement: func(path string, maxBytes int64) (managementRuntime, error) {
			if path != "/etc/control/management.json" || maxBytes != 8192 {
				t.Fatalf("management input = (%q, %d)", path, maxBytes)
			}
			return managementRuntime{
				Workers:    applicationRemoteWorkerSource{},
				Queues:     applicationQueueSource{},
				Records:    applicationRecordSource{},
				Dispatcher: managementProcessDispatcher{},
			}, nil
		},
		routeDispatchers: func(dataPlane, workloads control.Dispatcher) (control.Dispatcher, error) {
			if _, ok := dataPlane.(managementProcessDispatcher); !ok {
				t.Fatalf("data-plane dispatcher = %T, want management dispatcher", dataPlane)
			}
			return control.NewRoutingDispatcher(dataPlane, workloads)
		},
		openPool: func(context.Context, gopostgres.Config) (*gopostgres.Pool, error) {
			return pool, nil
		},
		buildPersistence: controlpostgres.NewRuntime,
		buildRateLimiter: func() (apihttp.RateLimiter, error) {
			return applicationRateLimiter{}, nil
		},
		listen: func(network, address string) (net.Listener, error) {
			if network != "tcp" || address != "127.0.0.1:9090" {
				t.Fatalf("Listen(%q, %q)", network, address)
			}

			return listener, nil
		},
		buildServer: func(got net.Listener, handler http.Handler, _ server.Config) (processServer, error) {
			if got != listener || handler == nil {
				t.Fatalf("buildServer(%v, %v)", got, handler)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/version", nil))
			var build apihttp.BuildInfo
			if err := json.Unmarshal(response.Body.Bytes(), &build); err != nil ||
				build.Version != "v1.2.3" || build.Commit != "abcdef" {
				t.Fatalf("version response = (%+v, %v)", build, err)
			}
			capabilities := httptest.NewRecorder()
			handler.ServeHTTP(capabilities, httptest.NewRequest(http.MethodGet, "/v1/capabilities", nil))
			var discovered struct {
				Capabilities []string `json:"capabilities"`
			}
			if err := json.Unmarshal(capabilities.Body.Bytes(), &discovered); err != nil ||
				!containsString(discovered.Capabilities, "queues") ||
				!containsString(discovered.Capabilities, "workers") ||
				!containsString(discovered.Capabilities, "records") {
				t.Fatalf("capabilities response = (%+v, %v)", discovered, err)
			}

			return processServerFunc(func(context.Context) error {
				served = true

				return nil
			}), nil
		},
		dispatcher: control.UnavailableDispatcher{},
	})
	if err != nil {
		t.Fatalf("runProcess() error = %v", err)
	}
	if !migrated || !served || !telemetryStopped {
		t.Fatalf(
			"migrated = %t, served = %t, telemetry stopped = %t, want true",
			migrated,
			served,
			telemetryStopped,
		)
	}
	if !errors.Is(pool.Liveness().Err, gopostgres.ErrPoolClosed) {
		t.Fatalf("pool liveness = %+v, want closed", pool.Liveness())
	}
}

type applicationRemoteWorkerSource struct{}

func (applicationRemoteWorkerSource) SnapshotTenant(
	context.Context,
	string,
	time.Time,
	time.Duration,
) (fleet.RegistrySnapshot, error) {
	return fleet.RegistrySnapshot{}, nil
}

func TestRunProcessStopsAtEveryStartupFailure(t *testing.T) {
	t.Parallel()

	stageErr := errors.New("stage failed")
	tests := map[string]struct {
		environment map[string]string
		mutate      func(*processDependencies)
		want        error
	}{
		"dependencies": {
			mutate: func(deps *processDependencies) { deps.listen = nil },
			want:   ErrInvalidProcessDependencies,
		},
		"build metadata": {
			mutate: func(deps *processDependencies) {
				deps.buildInfo = func() (apihttp.BuildInfo, error) {
					return apihttp.BuildInfo{}, stageErr
				}
			},
			want: stageErr,
		},
		"telemetry": {
			mutate: func(deps *processDependencies) {
				deps.buildTelemetry = func(context.Context, Config, apihttp.BuildInfo) (processTelemetry, error) {
					return nil, stageErr
				}
			},
			want: stageErr,
		},
		"access": {
			mutate: func(deps *processDependencies) {
				deps.loadAccess = func(string, int64) (*server.StaticAccess, error) {
					return nil, stageErr
				}
			},
			want: stageErr,
		},
		"migration": {
			environment: map[string]string{"QUEUE_CONTROL_RUN_MIGRATIONS": "true"},
			mutate: func(deps *processDependencies) {
				deps.migrate = func(context.Context, string) error { return stageErr }
			},
			want: stageErr,
		},
		"workloads": {
			environment: map[string]string{
				"QUEUE_CONTROL_KUBERNETES_TENANTS_FILE": "/etc/control/tenants.json",
			},
			mutate: func(deps *processDependencies) {
				deps.loadWorkloads = func(string, int64) (workloadRuntime, error) {
					return workloadRuntime{}, stageErr
				}
			},
			want: stageErr,
		},
		"management": {
			environment: map[string]string{
				"QUEUE_CONTROL_MANAGEMENT_TENANTS_FILE": "/etc/control/management.json",
			},
			mutate: func(deps *processDependencies) {
				deps.loadManagement = func(string, int64) (managementRuntime, error) {
					return managementRuntime{}, stageErr
				}
			},
			want: stageErr,
		},
		"invalid management": {
			environment: map[string]string{
				"QUEUE_CONTROL_MANAGEMENT_TENANTS_FILE": "/etc/control/management.json",
			},
			mutate: func(deps *processDependencies) {
				deps.loadManagement = func(string, int64) (managementRuntime, error) {
					return managementRuntime{}, nil
				}
			},
			want: ErrInvalidManagementRuntime,
		},
		"invalid workloads": {
			environment: map[string]string{
				"QUEUE_CONTROL_KUBERNETES_TENANTS_FILE": "/etc/control/tenants.json",
			},
			mutate: func(deps *processDependencies) {
				deps.loadWorkloads = func(string, int64) (workloadRuntime, error) {
					return workloadRuntime{}, nil
				}
			},
			want: ErrInvalidWorkloadRuntime,
		},
		"workload routing": {
			environment: map[string]string{
				"QUEUE_CONTROL_KUBERNETES_TENANTS_FILE": "/etc/control/tenants.json",
			},
			mutate: func(deps *processDependencies) {
				deps.loadWorkloads = func(string, int64) (workloadRuntime, error) {
					return workloadRuntime{
						Source:     applicationWorkloadSource{},
						Dispatcher: applicationDispatcher{},
					}, nil
				}
				deps.routeDispatchers = func(control.Dispatcher, control.Dispatcher) (control.Dispatcher, error) {
					return nil, stageErr
				}
			},
			want: stageErr,
		},
		"pool": {
			mutate: func(deps *processDependencies) {
				deps.openPool = func(context.Context, gopostgres.Config) (*gopostgres.Pool, error) {
					return nil, stageErr
				}
			},
			want: stageErr,
		},
		"persistence": {
			mutate: func(deps *processDependencies) {
				deps.buildPersistence = func(*gopostgres.Pool) (*controlpostgres.Runtime, error) {
					return nil, stageErr
				}
			},
			want: stageErr,
		},
		"rate limiter": {
			mutate: func(deps *processDependencies) {
				deps.buildRateLimiter = func() (apihttp.RateLimiter, error) { return nil, stageErr }
			},
			want: stageErr,
		},
		"application": {
			mutate: func(deps *processDependencies) {
				deps.loadAccess = func(string, int64) (*server.StaticAccess, error) { return nil, nil }
			},
			want: ErrInvalidApplicationDependencies,
		},
		"listener": {
			mutate: func(deps *processDependencies) {
				deps.listen = func(string, string) (net.Listener, error) { return nil, stageErr }
			},
			want: stageErr,
		},
		"server": {
			mutate: func(deps *processDependencies) {
				deps.buildServer = func(net.Listener, http.Handler, server.Config) (processServer, error) {
					return nil, stageErr
				}
			},
			want: stageErr,
		},
		"serve": {
			mutate: func(deps *processDependencies) {
				deps.buildServer = func(net.Listener, http.Handler, server.Config) (processServer, error) {
					return processServerFunc(func(context.Context) error { return stageErr }), nil
				}
			},
			want: stageErr,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			environment := map[string]string{
				"DATABASE_URL":              "postgres://database/control",
				"QUEUE_CONTROL_ACCESS_FILE": "/run/secrets/access.json",
			}
			for key, value := range test.environment {
				environment[key] = value
			}
			dependencies := validProcessDependencies(t)
			test.mutate(&dependencies)
			err := runProcess(context.Background(), mapEnvironment(environment), dependencies)
			if !errors.Is(err, test.want) {
				t.Fatalf("runProcess() error = %v, want %v", err, test.want)
			}
		})
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}

	return false
}

func validProcessDependencies(t *testing.T) processDependencies {
	t.Helper()

	return processDependencies{
		buildInfo: func() (apihttp.BuildInfo, error) {
			return apihttp.BuildInfo{Version: "dev", Commit: "unknown"}, nil
		},
		buildTelemetry: func(context.Context, Config, apihttp.BuildInfo) (processTelemetry, error) {
			return nil, nil
		},
		loadAccess: func(string, int64) (*server.StaticAccess, error) {
			return applicationAccess(t), nil
		},
		migrate: func(context.Context, string) error { return nil },
		retain:  func(context.Context, string, string, int64) error { return nil },
		loadWorkloads: func(string, int64) (workloadRuntime, error) {
			return workloadRuntime{}, nil
		},
		loadManagement: func(string, int64) (managementRuntime, error) {
			return managementRuntime{}, nil
		},
		routeDispatchers: func(dataPlane, workloads control.Dispatcher) (control.Dispatcher, error) {
			return control.NewRoutingDispatcher(dataPlane, workloads)
		},
		openPool: func(context.Context, gopostgres.Config) (*gopostgres.Pool, error) {
			return lazyProcessPool(t), nil
		},
		buildPersistence: controlpostgres.NewRuntime,
		buildRateLimiter: func() (apihttp.RateLimiter, error) {
			return applicationRateLimiter{}, nil
		},
		listen: func(string, string) (net.Listener, error) { return &processListener{}, nil },
		buildServer: func(net.Listener, http.Handler, server.Config) (processServer, error) {
			return processServerFunc(func(context.Context) error { return nil }), nil
		},
		dispatcher: control.UnavailableDispatcher{},
	}
}

func lazyProcessPool(t *testing.T) *gopostgres.Pool {
	t.Helper()

	pool, err := gopostgres.New(context.Background(), gopostgres.Config{
		DSN:           "postgres://localhost/control_plane",
		StartupPolicy: gopostgres.StartupLazy,
	})
	if err != nil {
		t.Fatalf("postgres.New() error = %v", err)
	}

	return pool
}

type processServerFunc func(context.Context) error

func (run processServerFunc) Run(ctx context.Context) error { return run(ctx) }

type processListener struct{}

type managementProcessDispatcher struct{}

func (managementProcessDispatcher) Dispatch(context.Context, controlplane.Command) error {
	return nil
}

func (*processListener) Accept() (net.Conn, error) { return nil, errors.New("not used") }
func (*processListener) Close() error              { return nil }
func (*processListener) Addr() net.Addr            { return processAddress("test") }

type processAddress string

func (address processAddress) Network() string { return string(address) }
func (address processAddress) String() string  { return string(address) }

type processTelemetryStub struct {
	stopped     *bool
	shutdownErr error
}

func (*processTelemetryStub) TracerProvider() trace.TracerProvider {
	return tracenoop.NewTracerProvider()
}

func (*processTelemetryStub) MeterProvider() metric.MeterProvider {
	return metricnoop.NewMeterProvider()
}

func (*processTelemetryStub) Propagator() propagation.TextMapPropagator {
	return propagation.TraceContext{}
}

func (runtime *processTelemetryStub) Shutdown(context.Context) error {
	*runtime.stopped = true

	return runtime.shutdownErr
}
