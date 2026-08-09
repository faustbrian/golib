//go:build integration

package postgres_test

import (
	"context"
	"io/fs"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
