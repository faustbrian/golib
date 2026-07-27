//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	schedulerlease "github.com/faustbrian/golib/pkg/scheduler/lease"
	"github.com/faustbrian/golib/pkg/scheduler/lease/conformance"
	schedulerpostgres "github.com/faustbrian/golib/pkg/scheduler/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresConformance(t *testing.T) {
	databaseURL := os.Getenv("POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("POSTGRES_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	migration := schedulerpostgres.SchemaMigration()
	if _, err := pool.Exec(t.Context(), "DROP TABLE IF EXISTS scheduler_leases"); err != nil {
		t.Fatalf("drop existing table error = %v", err)
	}
	if _, err := pool.Exec(t.Context(), migration.Up); err != nil {
		t.Fatalf("apply migration error = %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), migration.Down) })
	store, err := schedulerpostgres.New(pool)
	if err != nil {
		t.Fatalf("postgres.New() error = %v", err)
	}

	conformance.TestStore(t, func(t *testing.T) conformance.Harness {
		if _, err := pool.Exec(t.Context(), "TRUNCATE scheduler_leases"); err != nil {
			t.Fatalf("truncate leases error = %v", err)
		}
		return conformance.Harness{
			Store: store,
			Now:   time.Now,
			Advance: func(duration time.Duration) {
				time.Sleep(duration + 50*time.Millisecond)
			},
		}
	})
}

func TestPostgresCallerOwnedSchema(t *testing.T) {
	databaseURL := os.Getenv("POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("POSTGRES_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)

	const schema = "scheduler_integration"
	if _, err := pool.Exec(
		t.Context(),
		"CREATE SCHEMA IF NOT EXISTS "+schema,
	); err != nil {
		t.Fatalf("create schema error = %v", err)
	}
	migration, err := schedulerpostgres.SchemaMigrationFor(schema)
	if err != nil {
		t.Fatalf("SchemaMigrationFor() error = %v", err)
	}
	if _, err := pool.Exec(
		t.Context(),
		"DROP TABLE IF EXISTS "+schema+".scheduler_leases",
	); err != nil {
		t.Fatalf("drop existing table error = %v", err)
	}
	if _, err := pool.Exec(t.Context(), migration.Up); err != nil {
		t.Fatalf("apply migration error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), migration.Down)
		_, _ = pool.Exec(
			context.Background(),
			"DROP SCHEMA IF EXISTS "+schema,
		)
	})
	store, err := schedulerpostgres.NewWithSchema(pool, schema)
	if err != nil {
		t.Fatalf("postgres.NewWithSchema() error = %v", err)
	}

	owned, err := store.Acquire(
		t.Context(),
		"schema:acquire",
		"replica-a",
		time.Minute,
		time.Time{},
	)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if owned.FencingToken != 1 {
		t.Fatalf("Acquire() fencing token = %d, want 1", owned.FencingToken)
	}
	owned, err = store.Heartbeat(t.Context(), owned, time.Minute, time.Time{})
	if err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	inspected, err := store.Inspect(t.Context(), owned.Key)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if inspected.FencingToken != owned.FencingToken {
		t.Fatalf(
			"Inspect() fencing token = %d, want %d",
			inspected.FencingToken,
			owned.FencingToken,
		)
	}
	if err := store.Release(t.Context(), owned); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if _, err := store.Heartbeat(
		t.Context(),
		owned,
		time.Minute,
		time.Time{},
	); !errors.Is(err, schedulerlease.ErrNotFound) {
		t.Fatalf("Heartbeat(released) error = %v, want ErrNotFound", err)
	}
	owned, err = store.Acquire(
		t.Context(),
		owned.Key,
		"replica-b",
		time.Minute,
		time.Time{},
	)
	if err != nil {
		t.Fatalf("Acquire(takeover) error = %v", err)
	}
	if owned.FencingToken != 2 {
		t.Fatalf("Acquire(takeover) fencing token = %d, want 2", owned.FencingToken)
	}
	if err := store.Recover(
		t.Context(),
		owned.Key,
		owned.FencingToken,
	); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}

	var publicTableExists bool
	if err := pool.QueryRow(t.Context(), `
SELECT to_regclass('public.scheduler_leases') IS NOT NULL
`).Scan(&publicTableExists); err != nil {
		t.Fatalf("inspect public table error = %v", err)
	}
	if publicTableExists {
		t.Fatal("caller-owned schema migration created public.scheduler_leases")
	}
}

func TestPostgresFaultRecovery(t *testing.T) {
	databaseURL := os.Getenv("POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("POSTGRES_URL is not set")
	}
	pool, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	migration := schedulerpostgres.SchemaMigration()
	if _, err := pool.Exec(t.Context(), "DROP TABLE IF EXISTS scheduler_leases"); err != nil {
		t.Fatalf("drop existing table error = %v", err)
	}
	if _, err := pool.Exec(t.Context(), migration.Up); err != nil {
		t.Fatalf("apply migration error = %v", err)
	}
	store, err := schedulerpostgres.New(pool)
	if err != nil {
		t.Fatalf("postgres.New() error = %v", err)
	}

	t.Run("server time overrides replica clock", func(t *testing.T) {
		before := time.Now().UTC()
		owned, err := store.Acquire(
			t.Context(), "fault:clock", "replica-a", time.Minute,
			time.Date(1900, time.January, 1, 0, 0, 0, 0, time.UTC),
		)
		after := time.Now().UTC()
		if err != nil {
			t.Fatalf("Acquire() error = %v", err)
		}
		if owned.AcquiredAt.Before(before.Add(-time.Second)) || owned.AcquiredAt.After(after.Add(time.Second)) {
			t.Fatalf("AcquiredAt = %v, local interval [%v, %v]", owned.AcquiredAt, before, after)
		}
	})

	t.Run("latency cancellation leaves no partial lease", func(t *testing.T) {
		transaction, err := pool.Begin(t.Context())
		if err != nil {
			t.Fatalf("Begin() error = %v", err)
		}
		defer func() { _ = transaction.Rollback(context.Background()) }()
		if _, err := transaction.Exec(t.Context(), "LOCK TABLE scheduler_leases IN ACCESS EXCLUSIVE MODE"); err != nil {
			t.Fatalf("LOCK TABLE error = %v", err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		_, err = store.Acquire(ctx, "fault:partial", "replica-a", time.Minute, time.Time{})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Acquire(blocked) error = %v, want deadline exceeded", err)
		}
		if err := transaction.Rollback(t.Context()); err != nil {
			t.Fatalf("Rollback() error = %v", err)
		}
		if _, err := store.Inspect(t.Context(), "fault:partial"); !errors.Is(err, schedulerlease.ErrNotFound) {
			t.Fatalf("Inspect(partial) error = %v, want ErrNotFound", err)
		}
	})

	t.Run("closed connection pool fails and a new pool recovers", func(t *testing.T) {
		outagePool, err := pgxpool.New(t.Context(), databaseURL)
		if err != nil {
			t.Fatalf("pgxpool.New(outage) error = %v", err)
		}
		outageStore, _ := schedulerpostgres.New(outagePool)
		owned, err := outageStore.Acquire(t.Context(), "fault:reconnect", "replica-a", time.Minute, time.Time{})
		if err != nil {
			outagePool.Close()
			t.Fatalf("Acquire() error = %v", err)
		}
		outagePool.Close()
		if _, err := outageStore.Inspect(t.Context(), owned.Key); err == nil {
			t.Fatal("Inspect(closed pool) error = nil")
		}
		reconnectedPool, err := pgxpool.New(t.Context(), databaseURL)
		if err != nil {
			t.Fatalf("pgxpool.New(reconnected) error = %v", err)
		}
		defer reconnectedPool.Close()
		reconnectedStore, _ := schedulerpostgres.New(reconnectedPool)
		current, err := reconnectedStore.Inspect(t.Context(), owned.Key)
		if err != nil {
			t.Fatalf("Inspect(reconnected) error = %v", err)
		}
		if current.Owner != owned.Owner || current.FencingToken != owned.FencingToken {
			t.Fatalf("reconnected lease = %+v, want %+v", current, owned)
		}
	})
}
