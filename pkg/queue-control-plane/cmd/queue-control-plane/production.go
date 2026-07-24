package main

import (
	"context"
	"database/sql"
	"net"
	"net/http"
	"time"

	gopostgres "github.com/faustbrian/golib/pkg/postgres"
	"github.com/faustbrian/golib/pkg/queue-control-plane/apihttp"
	"github.com/faustbrian/golib/pkg/queue-control-plane/control"
	controlpostgres "github.com/faustbrian/golib/pkg/queue-control-plane/postgres"
	"github.com/faustbrian/golib/pkg/queue-control-plane/server"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	productionRateLimit  uint32 = 120
	productionRateWindow        = time.Minute
	productionRateKeys          = 10_000
)

var (
	buildVersion = "dev"
	buildCommit  = "unknown"
	buildTime    string
)

func productionDependencies() processDependencies {
	return processDependencies{
		buildInfo: func() (apihttp.BuildInfo, error) {
			return parseBuildInfo(buildVersion, buildCommit, buildTime)
		},
		buildTelemetry: func(
			ctx context.Context,
			config Config,
			build apihttp.BuildInfo,
		) (processTelemetry, error) {
			runtime, err := buildProductionTelemetry(ctx, config, build)
			if runtime == nil {
				return nil, err
			}

			return runtime, err
		},
		loadAccess:     server.LoadStaticAccessFile,
		loadWorkloads:  loadProductionWorkloads,
		loadManagement: loadProductionManagement,
		routeDispatchers: func(dataPlane, workloads control.Dispatcher) (control.Dispatcher, error) {
			return control.NewRoutingDispatcher(dataPlane, workloads)
		},
		migrate: func(ctx context.Context, dsn string) error {
			return executeMigrations(ctx, dsn, sql.Open, func(database *sql.DB) (migrationApplier, error) {
				return controlpostgres.NewMigrationRunner(database)
			})
		},
		retain:           executeProductionRetention,
		openPool:         gopostgres.New,
		buildPersistence: controlpostgres.NewRuntime,
		buildRateLimiter: func() (apihttp.RateLimiter, error) {
			return apihttp.NewFixedWindowRateLimiter(
				productionRateLimit,
				productionRateWindow,
				productionRateKeys,
				time.Now,
			)
		},
		listen: net.Listen,
		buildServer: func(
			listener net.Listener,
			handler http.Handler,
			config server.Config,
		) (processServer, error) {
			return server.New(listener, handler, config)
		},
		dispatcher: control.UnavailableDispatcher{},
	}
}
