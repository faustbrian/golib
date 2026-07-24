package main

import (
	"context"
	"errors"
	"net"
	"net/http"

	gopostgres "github.com/faustbrian/golib/pkg/postgres"
	"github.com/faustbrian/golib/pkg/queue-control-plane/apihttp"
	"github.com/faustbrian/golib/pkg/queue-control-plane/control"
	controlpostgres "github.com/faustbrian/golib/pkg/queue-control-plane/postgres"
	"github.com/faustbrian/golib/pkg/queue-control-plane/server"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// ErrInvalidProcessDependencies reports an incomplete startup graph.
var ErrInvalidProcessDependencies = errors.New("queue-control-plane: invalid process dependencies")

// ErrInvalidWorkloadRuntime reports incomplete Kubernetes startup wiring.
var ErrInvalidWorkloadRuntime = errors.New("queue-control-plane: invalid workload runtime")

type processServer interface {
	Run(context.Context) error
}

type processTelemetry interface {
	TracerProvider() trace.TracerProvider
	MeterProvider() metric.MeterProvider
	Propagator() propagation.TextMapPropagator
	Shutdown(context.Context) error
}

type workloadRuntime struct {
	Source     apihttp.WorkloadSource
	Dispatcher control.Dispatcher
}

type processDependencies struct {
	buildInfo        func() (apihttp.BuildInfo, error)
	buildTelemetry   func(context.Context, Config, apihttp.BuildInfo) (processTelemetry, error)
	loadAccess       func(string, int64) (*server.StaticAccess, error)
	migrate          func(context.Context, string) error
	retain           func(context.Context, string, string, int64) error
	loadWorkloads    func(string, int64) (workloadRuntime, error)
	loadManagement   func(string, int64) (managementRuntime, error)
	routeDispatchers func(control.Dispatcher, control.Dispatcher) (control.Dispatcher, error)
	openPool         func(context.Context, gopostgres.Config) (*gopostgres.Pool, error)
	buildPersistence func(*gopostgres.Pool) (*controlpostgres.Runtime, error)
	buildRateLimiter func() (apihttp.RateLimiter, error)
	listen           func(string, string) (net.Listener, error)
	buildServer      func(net.Listener, http.Handler, server.Config) (processServer, error)
	dispatcher       control.Dispatcher
}

func runProcess(
	ctx context.Context,
	getenv func(string) string,
	dependencies processDependencies,
) (runErr error) {
	config, err := LoadConfig(getenv)
	if err != nil {
		return err
	}
	if config.MigrateOnly {
		if dependencies.migrate == nil {
			return ErrInvalidProcessDependencies
		}

		return dependencies.migrate(ctx, config.DatabaseURL)
	}
	if config.RetentionOnly {
		if dependencies.retain == nil {
			return ErrInvalidProcessDependencies
		}

		return dependencies.retain(
			ctx,
			config.DatabaseURL,
			config.RetentionDocumentPath,
			config.RetentionDocumentSize,
		)
	}
	if invalidProcessDependencies(dependencies) {
		return ErrInvalidProcessDependencies
	}
	build, err := dependencies.buildInfo()
	if err != nil {
		return err
	}
	telemetryRuntime, err := dependencies.buildTelemetry(ctx, config, build)
	if err != nil {
		return err
	}
	if telemetryRuntime != nil {
		defer func() {
			runErr = errors.Join(runErr, telemetryRuntime.Shutdown(context.Background()))
		}()
	}

	access, err := dependencies.loadAccess(config.AccessDocumentPath, config.AccessDocumentSize)
	if err != nil {
		return err
	}
	if config.RunMigrations {
		if err := dependencies.migrate(ctx, config.DatabaseURL); err != nil {
			return err
		}
	}
	dispatcher := dependencies.dispatcher
	var workloads apihttp.WorkloadSource
	var workers apihttp.RemoteWorkerSource
	var queues apihttp.QueueSource
	var records apihttp.RecordSource
	if config.ManagementTenantPath != "" {
		management, err := dependencies.loadManagement(
			config.ManagementTenantPath,
			config.ManagementTenantSize,
		)
		if err != nil {
			return err
		}
		if missingDependency(management.Workers) || missingDependency(management.Queues) ||
			missingDependency(management.Records) ||
			missingDependency(management.Dispatcher) {
			return ErrInvalidManagementRuntime
		}
		workers = management.Workers
		queues = management.Queues
		records = management.Records
		dispatcher = management.Dispatcher
	}
	if config.KubernetesTenantPath != "" {
		workload, err := dependencies.loadWorkloads(
			config.KubernetesTenantPath,
			config.KubernetesTenantSize,
		)
		if err != nil {
			return err
		}
		if missingDependency(workload.Source) || missingDependency(workload.Dispatcher) {
			return ErrInvalidWorkloadRuntime
		}
		dispatcher, err = dependencies.routeDispatchers(dispatcher, workload.Dispatcher)
		if err != nil {
			return err
		}
		workloads = workload.Source
	}

	pool, err := dependencies.openPool(ctx, gopostgres.Config{DSN: config.DatabaseURL})
	if err != nil {
		return err
	}
	defer func() {
		runErr = errors.Join(runErr, pool.Close(context.Background()))
	}()
	persistence, err := dependencies.buildPersistence(pool)
	if err != nil {
		return err
	}
	limiter, err := dependencies.buildRateLimiter()
	if err != nil {
		return err
	}
	application := applicationDependencies{
		Access:         access,
		Journal:        persistence.Journal,
		Dispatcher:     dispatcher,
		RateLimiter:    limiter,
		Readiness:      persistence.Readiness,
		Audit:          persistence.Audit,
		CommandResults: persistence.Commands,
		Workloads:      workloads,
		RemoteWorkers:  workers,
		Queues:         queues,
		Records:        records,
		DesiredState:   persistence.Desired,
		Build:          build,
	}
	if telemetryRuntime != nil {
		application.Meter = telemetryRuntime.MeterProvider().Meter(telemetryServiceName)
		application.Telemetry = &apihttp.TelemetryConfig{
			TracerProvider: telemetryRuntime.TracerProvider(),
			MeterProvider:  telemetryRuntime.MeterProvider(),
			Propagator:     telemetryRuntime.Propagator(),
			TrustedInbound: config.TelemetryTrustedInbound,
		}
	}
	handler, err := buildApplication(config, application)
	if err != nil {
		return err
	}
	listener, err := dependencies.listen("tcp", config.ListenAddress)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()
	process, err := dependencies.buildServer(listener, handler, server.Config{})
	if err != nil {
		return err
	}

	return process.Run(ctx)
}

func invalidProcessDependencies(dependencies processDependencies) bool {
	return dependencies.buildInfo == nil || dependencies.buildTelemetry == nil ||
		dependencies.loadAccess == nil || dependencies.migrate == nil || dependencies.retain == nil ||
		dependencies.loadWorkloads == nil || dependencies.routeDispatchers == nil ||
		dependencies.loadManagement == nil ||
		dependencies.openPool == nil || dependencies.buildPersistence == nil ||
		dependencies.buildRateLimiter == nil || dependencies.listen == nil ||
		dependencies.buildServer == nil || missingDependency(dependencies.dispatcher)
}
