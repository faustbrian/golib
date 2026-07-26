package runner_test

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	migrations "github.com/faustbrian/golib/pkg/migrations"
	migrationpostgres "github.com/faustbrian/golib/pkg/migrations/postgres"
	outboxpostgres "github.com/faustbrian/golib/pkg/outbox/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestGoMigrationsConcurrentCleanInstall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	version := os.Getenv("OUTBOX_POSTGRES_VERSION")
	if version == "" {
		version = "18"
	}
	container, err := tcpostgres.Run(ctx, "postgres:"+version+"-alpine",
		tcpostgres.WithDatabase("outbox"),
		tcpostgres.WithUsername("outbox"),
		tcpostgres.WithPassword("outbox"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL %s: %v", version, err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Errorf("terminate PostgreSQL: %v", err)
		}
	})

	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL connection string: %v", err)
	}
	database, err := sql.Open("pgx", connectionString)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.PingContext(ctx); err != nil {
		t.Fatalf("ping PostgreSQL: %v", err)
	}

	source, err := migrations.NewFSSource(outboxpostgres.Migrations(), ".")
	if err != nil {
		t.Fatalf("create migration source: %v", err)
	}
	runners := make([]*migrations.Runner, 2)
	for index := range runners {
		backend, err := migrationpostgres.New(database,
			migrationpostgres.WithLockRetryInterval(10*time.Millisecond))
		if err != nil {
			t.Fatalf("create migration backend %d: %v", index, err)
		}
		runners[index], err = migrations.NewRunner(source, backend)
		if err != nil {
			t.Fatalf("create migration runner %d: %v", index, err)
		}
	}

	start := make(chan struct{})
	results := make([]migrations.Result, len(runners))
	errorsByRunner := make([]error, len(runners))
	var wait sync.WaitGroup
	for index, runner := range runners {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results[index], errorsByRunner[index] = runner.Up(ctx)
		}()
	}
	close(start)
	wait.Wait()

	applied := 0
	for index, result := range results {
		if errorsByRunner[index] != nil {
			t.Fatalf("concurrent runner %d: %v", index, errorsByRunner[index])
		}
		applied += len(result.Records())
	}
	if applied != 1 {
		t.Fatalf("concurrent runners applied %d migrations, want 1", applied)
	}

	var ledgerRows int
	var dirtyRows int
	if err := database.QueryRowContext(ctx, `
SELECT count(*), count(*) FILTER (WHERE dirty)
FROM go_schema_migrations
WHERE version = 1 AND name = 'create_outbox'`).Scan(&ledgerRows, &dirtyRows); err != nil {
		t.Fatalf("read migration ledger: %v", err)
	}
	if ledgerRows != 1 || dirtyRows != 0 {
		t.Fatalf("ledger rows/dirty = %d/%d, want 1/0", ledgerRows, dirtyRows)
	}
	var messageTable string
	var auditTable string
	if err := database.QueryRowContext(ctx, `
SELECT to_regclass('public.outbox_messages')::text,
       to_regclass('public.outbox_replay_audit')::text`).Scan(&messageTable, &auditTable); err != nil {
		t.Fatalf("inspect migrated tables: %v", err)
	}
	if messageTable != "outbox_messages" || auditTable != "outbox_replay_audit" {
		t.Fatalf("migrated tables = %q/%q", messageTable, auditTable)
	}

	result, err := runners[0].Up(ctx)
	if err != nil {
		t.Fatalf("rerun migration: %v", err)
	}
	if len(result.Records()) != 0 {
		t.Fatalf("rerun applied records: %#v", result.Records())
	}
}
