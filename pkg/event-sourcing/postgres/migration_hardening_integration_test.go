//go:build integration

package postgres_test

import (
	"context"
	"io/fs"
	"sort"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	postgreSQLDeploymentJobs    = 8
	postgreSQLDeploymentLockKey = int64(0x6576656e7473746f)
)

type postgreSQLDeploymentMigration struct {
	name string
	up   string
}

type postgreSQLDeploymentResult struct {
	applied int
	err     error
}

func TestPostgreSQLUpgradeFromEveryPriorSchemaPreservesRollingWriters(
	t *testing.T,
) {
	ctx, pool := newDerivedIntegrationPool(t)
	if _, err := pool.Exec(
		ctx,
		`DROP TABLE event_sourcing.projections, event_sourcing.snapshots`,
	); err != nil {
		t.Fatal(err)
	}
	assertPostgreSQLNoOptionalExtensions(t, ctx, pool)

	eventStore, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	stream := mustStream(t, "account", "rolling-upgrade")
	first := mustPending(t, stream, "rolling-upgrade-1", 1)
	if _, err := eventStore.Append(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{first},
	); err != nil {
		t.Fatalf("append on version 1 schema: %v", err)
	}
	applyPostgreSQLMigration(
		t,
		ctx,
		pool,
		"000002_create_snapshots_and_projections.sql",
	)

	second := mustPending(t, stream, "rolling-upgrade-2", 2)
	stored, err := eventStore.Append(
		ctx,
		stream,
		eventsourcing.ExpectExactVersion(1),
		[]eventsourcing.PendingMessage{second},
	)
	if err != nil || len(stored) != 1 || stored[0].StreamVersion() != 2 {
		t.Fatalf("append after version 2 migration = %#v, %v", stored, err)
	}
	options, err := eventsourcing.NewReadStreamOptions(
		eventsourcing.ReadStreamOptionsInput{FromVersion: 1, Limit: 2},
	)
	if err != nil {
		t.Fatal(err)
	}
	iterator, err := eventStore.ReadStream(ctx, stream, options)
	messages := collectMessages(t, ctx, mustIterator(t, iterator, err))
	if len(messages) != 2 ||
		messages[0].ID() != first.ID() ||
		messages[1].ID() != second.ID() {
		t.Fatalf("history after version 2 migration = %#v", messages)
	}

	snapshotStore, err := eventpostgres.NewSnapshotStore(
		pool,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := mustPostgreSQLSnapshot(
		t,
		stream,
		2,
		1,
		`{"balance":2}`,
		time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC),
	)
	if err := snapshotStore.Save(ctx, snapshot); err != nil {
		t.Fatalf("save snapshot after version 2 migration: %v", err)
	}
	loadedSnapshot, err := snapshotStore.Load(ctx, stream)
	if err != nil || !loadedSnapshot.Equal(snapshot) {
		t.Fatalf(
			"load snapshot after version 2 migration = %#v, %v",
			loadedSnapshot,
			err,
		)
	}
	projectionStore, err := eventpostgres.NewProjectionStore(
		pool,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := projectionStore.Save(ctx, "rolling-upgrade", 0, 2); err != nil {
		t.Fatalf("save checkpoint after version 2 migration: %v", err)
	}
	assertPostgreSQLStatus(
		t,
		loadPostgreSQLStatus(t, ctx, projectionStore, "rolling-upgrade"),
		projection.StateRunning,
		2,
		true,
	)
	assertPostgreSQLNoOptionalExtensions(t, ctx, pool)
}

func TestPostgreSQLConcurrentDeploymentJobsApplyEachMigrationOnce(
	t *testing.T,
) {
	ctx, observerPool := newDerivedIntegrationPool(t)
	if _, err := observerPool.Exec(
		ctx,
		"DROP SCHEMA event_sourcing CASCADE",
	); err != nil {
		t.Fatal(err)
	}
	migrations := loadPostgreSQLDeploymentMigrations(t)

	config, err := pgxpool.ParseConfig(observerPool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = postgreSQLDeploymentJobs
	config.MinConns = 0
	config.MinIdleConns = 0
	jobPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(jobPool.Close)

	holder, err := observerPool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(holder.Release)
	if _, err := holder.Exec(
		ctx,
		"SELECT pg_advisory_lock($1)",
		postgreSQLDeploymentLockKey,
	); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()
		_, _ = holder.Exec(
			cleanupCtx,
			"SELECT pg_advisory_unlock($1)",
			postgreSQLDeploymentLockKey,
		)
	})

	results := make(chan postgreSQLDeploymentResult, postgreSQLDeploymentJobs)
	for range postgreSQLDeploymentJobs {
		go func() {
			applied, jobErr := runPostgreSQLDeploymentJob(
				ctx,
				jobPool,
				migrations,
			)
			results <- postgreSQLDeploymentResult{applied: applied, err: jobErr}
		}()
	}
	waitForPostgreSQLDeploymentWaiters(
		t,
		ctx,
		observerPool,
		postgreSQLDeploymentJobs,
	)
	select {
	case early := <-results:
		t.Fatalf("deployment job completed while lock held: %#v", early)
	default:
	}
	if _, err := holder.Exec(
		ctx,
		"SELECT pg_advisory_unlock($1)",
		postgreSQLDeploymentLockKey,
	); err != nil {
		t.Fatal(err)
	}

	totalApplied := 0
	applyingJobs := 0
	for range postgreSQLDeploymentJobs {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent deployment job: %v", result.err)
		}
		totalApplied += result.applied
		if result.applied > 0 {
			applyingJobs++
		}
	}
	if totalApplied != len(migrations) || applyingJobs != 1 {
		t.Fatalf(
			"deployment applications/jobs = %d/%d",
			totalApplied,
			applyingJobs,
		)
	}

	var ledgerRows, dirtyRows int
	if err := observerPool.QueryRow(
		ctx,
		`SELECT count(*), count(*) FILTER (WHERE NOT applied)
		 FROM public.event_sourcing_test_migrations`,
	).Scan(&ledgerRows, &dirtyRows); err != nil {
		t.Fatal(err)
	}
	if ledgerRows != len(migrations) || dirtyRows != 0 {
		t.Fatalf("deployment ledger = %d/%d", ledgerRows, dirtyRows)
	}
	var schemaTables int
	if err := observerPool.QueryRow(
		ctx,
		`SELECT count(*)
		 FROM information_schema.tables
		 WHERE table_schema = 'event_sourcing'
			AND table_name = ANY($1)`,
		[]string{"positions", "streams", "messages", "snapshots", "projections"},
	).Scan(&schemaTables); err != nil {
		t.Fatal(err)
	}
	if schemaTables != 5 {
		t.Fatalf("event-sourcing schema tables = %d", schemaTables)
	}

	store, err := eventpostgres.New(observerPool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	stream := mustStream(t, "account", "concurrent-deployment")
	stored, err := store.Append(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			mustPending(t, stream, "concurrent-deployment-message", 1),
		},
	)
	if err != nil || len(stored) != 1 || stored[0].StreamVersion() != 1 {
		t.Fatalf("append after concurrent deployment = %#v, %v", stored, err)
	}
}

func loadPostgreSQLDeploymentMigrations(
	t testing.TB,
) []postgreSQLDeploymentMigration {
	t.Helper()

	entries, err := fs.ReadDir(eventpostgres.Migrations(), ".")
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(entries, func(left, right int) bool {
		return entries[left].Name() < entries[right].Name()
	})
	migrations := make([]postgreSQLDeploymentMigration, 0, len(entries))
	for _, entry := range entries {
		contents, readErr := fs.ReadFile(eventpostgres.Migrations(), entry.Name())
		if readErr != nil {
			t.Fatal(readErr)
		}
		migrations = append(migrations, postgreSQLDeploymentMigration{
			name: entry.Name(),
			up:   migrationUpSection(t, contents),
		})
	}

	return migrations
}

func runPostgreSQLDeploymentJob(
	ctx context.Context,
	pool *pgxpool.Pool,
	migrations []postgreSQLDeploymentMigration,
) (int, error) {
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return 0, err
	}
	defer connection.Release()
	if _, err := connection.Exec(
		ctx,
		"SELECT pg_advisory_lock($1)",
		postgreSQLDeploymentLockKey,
	); err != nil {
		return 0, err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()
		_, _ = connection.Exec(
			cleanupCtx,
			"SELECT pg_advisory_unlock($1)",
			postgreSQLDeploymentLockKey,
		)
	}()
	if _, err := connection.Exec(
		ctx,
		`CREATE TABLE IF NOT EXISTS public.event_sourcing_test_migrations (
			name text PRIMARY KEY,
			applied boolean NOT NULL
		)`,
	); err != nil {
		return 0, err
	}

	applied := 0
	for _, migration := range migrations {
		var exists bool
		if err := connection.QueryRow(
			ctx,
			`SELECT EXISTS (
				SELECT 1 FROM public.event_sourcing_test_migrations
				WHERE name = $1 AND applied
			)`,
			migration.name,
		).Scan(&exists); err != nil {
			return applied, err
		}
		if exists {
			continue
		}
		tx, err := connection.Begin(ctx)
		if err != nil {
			return applied, err
		}
		if _, err := tx.Exec(ctx, migration.up); err != nil {
			_ = tx.Rollback(ctx)
			return applied, err
		}
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO public.event_sourcing_test_migrations (name, applied)
			 VALUES ($1, true)`,
			migration.name,
		); err != nil {
			_ = tx.Rollback(ctx)
			return applied, err
		}
		if err := tx.Commit(ctx); err != nil {
			return applied, err
		}
		applied++
	}

	return applied, nil
}

func waitForPostgreSQLDeploymentWaiters(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	want int,
) {
	t.Helper()

	deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	var lastWaiters int
	for {
		err := pool.QueryRow(
			deadline,
			`SELECT count(*)
			 FROM pg_stat_activity
			 WHERE datname = current_database()
				AND wait_event_type = 'Lock'
				AND wait_event = 'advisory'
				AND query LIKE 'SELECT pg_advisory_lock%'`,
		).Scan(&lastWaiters)
		if err == nil && lastWaiters == want {
			return
		}
		select {
		case <-deadline.Done():
			t.Fatalf(
				"deployment lock waiters = %d, want %d: %v: %v",
				lastWaiters,
				want,
				deadline.Err(),
				err,
			)
		case <-ticker.C:
		}
	}
}

func applyPostgreSQLMigration(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	name string,
) {
	t.Helper()

	contents, err := fs.ReadFile(eventpostgres.Migrations(), name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, migrationUpSection(t, contents)); err != nil {
		t.Fatalf("apply migration %s: %v", name, err)
	}
}

func assertPostgreSQLNoOptionalExtensions(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()

	var extensions int
	if err := pool.QueryRow(
		ctx,
		"SELECT count(*) FROM pg_extension WHERE extname <> 'plpgsql'",
	).Scan(&extensions); err != nil {
		t.Fatal(err)
	}
	if extensions != 0 {
		t.Fatalf("optional PostgreSQL extensions = %d", extensions)
	}
}
