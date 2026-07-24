//go:build disasterrecovery

package postgres_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	gopostgres "github.com/faustbrian/golib/pkg/postgres"
	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/control"
	"github.com/faustbrian/golib/pkg/queue-control-plane/history"
	controlpostgres "github.com/faustbrian/golib/pkg/queue-control-plane/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresDisasterRecoveryIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dsn := os.Getenv("RESTORED_DATABASE_URL")
	if dsn == "" {
		t.Fatal("RESTORED_DATABASE_URL is required for disaster-recovery tests")
	}

	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open restored migration database: %v", err)
	}
	defer database.Close()
	runner, err := controlpostgres.NewMigrationRunner(database)
	if err != nil {
		t.Fatalf("build restored migration runner: %v", err)
	}
	result, err := runner.Up(ctx)
	if err != nil || len(result.Records()) != 0 {
		t.Fatalf("restored migration result = (%d records, %v)", len(result.Records()), err)
	}

	pool, err := gopostgres.New(ctx, gopostgres.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("open restored runtime pool: %v", err)
	}
	t.Cleanup(func() {
		if err := pool.Close(context.Background()); err != nil {
			t.Errorf("close restored runtime pool: %v", err)
		}
	})
	runtime, err := controlpostgres.NewRuntime(pool)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	if err := runtime.Readiness.Ready(ctx); err != nil {
		t.Fatalf("restored Ready() error = %v", err)
	}

	command, err := runtime.Commands.Get(ctx, "tenant-1", "pause-critical-1")
	if err != nil || command.Status != controlplane.CommandSucceeded {
		t.Fatalf("restored Commands.Get() = (%+v, %v)", command, err)
	}
	target := controlplane.Target{Kind: controlplane.TargetQueue, Name: "critical"}
	desired, err := runtime.Desired.Get(ctx, "tenant-1", target)
	if err != nil || desired.State != control.DesiredPaused || desired.Revision != 1 ||
		desired.CommandKey != command.CommandID {
		t.Fatalf("restored Desired.Get() = (%+v, %v)", desired, err)
	}

	report, err := runtime.Audit.VerifyTenant(ctx, "tenant-1", 1)
	if err != nil || report.Events != 0 || report.HeadSequence != 4 ||
		report.HeadHash == (history.Hash{}) {
		t.Fatalf("restored Audit.VerifyTenant() = (%+v, %v)", report, err)
	}
	page, err := runtime.Audit.ListTenant(ctx, "tenant-1", 0, 10)
	if err != nil || len(page.Entries) != 0 || page.NextSequence != 0 {
		t.Fatalf("restored Audit.ListTenant() = (%+v, %v)", page, err)
	}
}
