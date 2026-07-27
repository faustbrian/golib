//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestPostgreSQLSnapshotStoreLifecycle(t *testing.T) {
	ctx, pool := newDerivedIntegrationPool(t)
	store, err := eventpostgres.NewSnapshotStore(
		pool,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	stream := mustStream(t, "account", "snapshot-account")
	createdAt := time.Date(
		2026,
		time.July,
		25,
		18,
		0,
		0,
		987654321,
		time.FixedZone("EEST", 3*60*60),
	)
	current := mustPostgreSQLSnapshot(
		t,
		stream,
		7,
		2,
		`{"owner":"Ada"}`,
		createdAt,
	)
	if err := store.Save(ctx, current); err != nil {
		t.Fatalf("Save(new) error = %v", err)
	}
	loaded, err := store.Load(ctx, stream)
	if err != nil || !loaded.Equal(current) {
		t.Fatalf("Load() = %#v, %v", loaded, err)
	}
	if err := store.Save(ctx, current); err != nil {
		t.Fatalf("Save(idempotent) error = %v", err)
	}

	for name, snapshot := range map[string]eventsourcing.Snapshot{
		"aggregate regression": mustPostgreSQLSnapshot(
			t,
			stream,
			6,
			2,
			`{"owner":"Ada"}`,
			createdAt,
		),
		"schema regression": mustPostgreSQLSnapshot(
			t,
			stream,
			8,
			1,
			`{"owner":"Ada"}`,
			createdAt,
		),
	} {
		err := store.Save(ctx, snapshot)
		var versionError *eventsourcing.SnapshotVersionError
		if !errors.Is(err, eventsourcing.ErrSnapshotStale) ||
			!errors.As(err, &versionError) ||
			versionError.StoredAggregateVersion != 7 ||
			versionError.StoredSchemaVersion != 2 {
			t.Fatalf("%s error = %#v, %v", name, versionError, err)
		}
	}
	conflict := mustPostgreSQLSnapshot(
		t,
		stream,
		7,
		2,
		`{"owner":"Grace"}`,
		createdAt,
	)
	err = store.Save(ctx, conflict)
	var conflictError *eventsourcing.SnapshotConflictError
	if !errors.Is(err, eventsourcing.ErrSnapshotConflict) ||
		!errors.As(err, &conflictError) ||
		conflictError.AggregateVersion != 7 ||
		conflictError.SchemaVersion != 2 {
		t.Fatalf("Save(conflict) = %#v, %v", conflictError, err)
	}

	next := mustPostgreSQLSnapshot(
		t,
		stream,
		8,
		3,
		`{"owner":"Ada","closed":true}`,
		createdAt.Add(time.Second),
	)
	if err := store.Save(ctx, next); err != nil {
		t.Fatalf("Save(newer) error = %v", err)
	}
	loaded, err = store.Load(ctx, stream)
	if err != nil || !loaded.Equal(next) {
		t.Fatalf("Load(newer) = %#v, %v", loaded, err)
	}
	if err := store.Delete(ctx, stream); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := store.Delete(ctx, stream); err != nil {
		t.Fatalf("Delete(idempotent) error = %v", err)
	}
	if _, err := store.Load(
		ctx,
		stream,
	); !errors.Is(err, eventsourcing.ErrSnapshotNotFound) {
		t.Fatalf("Load(deleted) error = %v", err)
	}
}

func TestPostgreSQLSnapshotStoreSerializesConcurrentCreation(t *testing.T) {
	ctx, pool := newDerivedIntegrationPool(t)
	store, err := eventpostgres.NewSnapshotStore(
		pool,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	stream := mustStream(t, "account", "snapshot-race")
	createdAt := time.Date(2026, time.July, 25, 16, 0, 0, 0, time.UTC)
	first := mustPostgreSQLSnapshot(
		t,
		stream,
		1,
		1,
		`{"winner":"first"}`,
		createdAt,
	)
	second := mustPostgreSQLSnapshot(
		t,
		stream,
		1,
		1,
		`{"winner":"second"}`,
		createdAt,
	)

	start := make(chan struct{})
	results := make(chan error, 2)
	for _, snapshot := range []eventsourcing.Snapshot{first, second} {
		snapshot := snapshot
		go func() {
			<-start
			results <- store.Save(ctx, snapshot)
		}()
	}
	close(start)
	var successes, conflicts int
	for range 2 {
		switch err := <-results; {
		case err == nil:
			successes++
		case errors.Is(err, eventsourcing.ErrSnapshotConflict):
			conflicts++
		default:
			t.Fatalf("concurrent Save() error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestPostgreSQLProjectionControlAndTransactionalCheckpoint(t *testing.T) {
	ctx, pool := newDerivedIntegrationPool(t)
	store, err := eventpostgres.NewProjectionStore(
		pool,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	const name = "account-summary"
	assertPostgreSQLStatus(
		t,
		loadPostgreSQLStatus(t, ctx, store, name),
		projection.StateRunning,
		0,
		false,
	)
	if err := store.Save(ctx, name, 0, 5); err != nil {
		t.Fatalf("Save(new) error = %v", err)
	}
	assertPostgreSQLStatus(
		t,
		loadPostgreSQLStatus(t, ctx, store, name),
		projection.StateRunning,
		5,
		true,
	)
	err = store.Save(ctx, name, 4, 6)
	var checkpointConflict *projection.CheckpointConflictError
	if !errors.Is(err, projection.ErrCheckpointConflict) ||
		!errors.As(err, &checkpointConflict) ||
		checkpointConflict.Expected != 4 ||
		checkpointConflict.Actual != 5 ||
		!checkpointConflict.ActualExists {
		t.Fatalf("Save(stale) = %#v, %v", checkpointConflict, err)
	}

	status, err := store.Pause(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	assertPostgreSQLStatus(t, status, projection.StatePaused, 5, true)
	if err := store.Save(
		ctx,
		name,
		5,
		6,
	); !errors.Is(err, projection.ErrProjectionPaused) {
		t.Fatalf("Save(paused) error = %v", err)
	}
	status, err = store.Pause(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	assertPostgreSQLStatus(t, status, projection.StatePaused, 5, true)
	if _, err := store.ResetCheckpoint(
		ctx,
		name,
		4,
	); !errors.Is(err, projection.ErrCheckpointConflict) {
		t.Fatalf("ResetCheckpoint(stale) error = %v", err)
	}
	status, err = store.ResetCheckpoint(ctx, name, 5)
	if err != nil {
		t.Fatal(err)
	}
	assertPostgreSQLStatus(t, status, projection.StatePaused, 0, false)
	status, err = store.ResetCheckpoint(ctx, name, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertPostgreSQLStatus(t, status, projection.StatePaused, 0, false)
	status, err = store.Resume(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	assertPostgreSQLStatus(t, status, projection.StateRunning, 0, false)
	status, err = store.Resume(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	assertPostgreSQLStatus(t, status, projection.StateRunning, 0, false)

	if _, err := pool.Exec(
		ctx,
		"CREATE TABLE account_summary (id integer PRIMARY KEY, total integer NOT NULL)",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(
		ctx,
		"INSERT INTO account_summary (id, total) VALUES (1, 0)",
	); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := eventpostgres.NewTxCheckpointWriter(
		tx,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(
		ctx,
		"UPDATE account_summary SET total = 1 WHERE id = 1",
	); err != nil {
		t.Fatal(err)
	}
	if err := writer.Stage(ctx, name, 0, 1); err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	assertProjectionAndReadModel(t, ctx, pool, store, name, 0, 0, false)

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	writer, err = eventpostgres.NewTxCheckpointWriter(
		tx,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(
		ctx,
		"UPDATE account_summary SET total = 1 WHERE id = 1",
	); err != nil {
		t.Fatal(err)
	}
	if err := writer.Stage(ctx, name, 0, 1); err != nil {
		t.Fatalf("Stage(commit) error = %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertProjectionAndReadModel(t, ctx, pool, store, name, 1, 1, true)
}

func TestPostgreSQLProjectionStoreAllowsOneConcurrentWinner(t *testing.T) {
	ctx, pool := newDerivedIntegrationPool(t)
	store, err := eventpostgres.NewProjectionStore(
		pool,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	const writers = 16
	start := make(chan struct{})
	var successes atomic.Uint32
	var conflicts atomic.Uint32
	var wait sync.WaitGroup
	wait.Add(writers)
	for range writers {
		go func() {
			defer wait.Done()
			<-start
			err := store.Save(ctx, "concurrent-summary", 0, 1)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, projection.ErrCheckpointConflict):
				conflicts.Add(1)
			default:
				t.Errorf("Save() error = %v", err)
			}
		}()
	}
	close(start)
	wait.Wait()
	if successes.Load() != 1 || conflicts.Load() != writers-1 {
		t.Fatalf(
			"successes=%d conflicts=%d",
			successes.Load(),
			conflicts.Load(),
		)
	}
}

func TestPostgreSQLDerivedMigrationRollsBackIndependently(t *testing.T) {
	ctx, pool := newDerivedIntegrationPool(t)
	contents, err := fs.ReadFile(
		eventpostgres.Migrations(),
		"000002_create_snapshots_and_projections.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, migrationSection(t, contents, false)); err != nil {
		t.Fatalf("roll back derived migration: %v", err)
	}
	for _, table := range []string{"snapshots", "projections"} {
		var exists bool
		if err := pool.QueryRow(
			ctx,
			"SELECT to_regclass($1) IS NOT NULL",
			"event_sourcing."+table,
		).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatalf("%s remains after migration rollback", table)
		}
	}
	var messagesExist bool
	if err := pool.QueryRow(
		ctx,
		"SELECT to_regclass('event_sourcing.messages') IS NOT NULL",
	).Scan(&messagesExist); err != nil {
		t.Fatal(err)
	}
	if !messagesExist {
		t.Fatal("base event-store migration was rolled back")
	}
}

func TestPostgreSQLProjectionStoreRejectsUnsupportedPosition(t *testing.T) {
	ctx, pool := newDerivedIntegrationPool(t)
	store, err := eventpostgres.NewProjectionStore(
		pool,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	unsupported := eventsourcing.GlobalPosition(uint64(math.MaxInt64) + 1)
	if err := store.Save(
		ctx,
		"account-summary",
		0,
		unsupported,
	); !errors.Is(err, eventsourcing.ErrVersionOverflow) {
		t.Fatalf("Save(unsupported) error = %v", err)
	}
}

func newDerivedIntegrationPool(
	t testing.TB,
) (context.Context, *pgxpool.Pool) {
	t.Helper()

	ctx, pool, _ := newPostgreSQLIntegrationDatabase(t)

	return ctx, pool
}

func newPostgreSQLIntegrationDatabase(
	t testing.TB,
) (context.Context, *pgxpool.Pool, *tcpostgres.PostgresContainer) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	version := os.Getenv("EVENT_SOURCING_POSTGRES_VERSION")
	if version == "" {
		version = "18"
	}
	container, err := tcpostgres.Run(
		ctx,
		postgresIntegrationImage(t, version),
		tcpostgres.WithDatabase("event_sourcing"),
		tcpostgres.WithUsername("event_sourcing"),
		tcpostgres.WithPassword("event_sourcing"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL %s: %v", version, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			30*time.Second,
		)
		defer cleanupCancel()
		if err := container.Terminate(cleanupCtx); err != nil {
			t.Errorf("terminate PostgreSQL: %v", err)
		}
	})
	connectionString, err := container.ConnectionString(
		ctx,
		"sslmode=disable",
	)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	entries, err := fs.ReadDir(eventpostgres.Migrations(), ".")
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(entries, func(left, right int) bool {
		return entries[left].Name() < entries[right].Name()
	})
	for _, entry := range entries {
		contents, err := fs.ReadFile(eventpostgres.Migrations(), entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(
			ctx,
			migrationSection(t, contents, true),
		); err != nil {
			t.Fatalf("apply migration %s: %v", entry.Name(), err)
		}
	}

	return ctx, pool, container
}

func migrationSection(t testing.TB, contents []byte, up bool) string {
	t.Helper()

	const upMarker = "-- +migrations Up\n"
	const downMarker = "-- +migrations Down\n"
	down := strings.Index(string(contents), downMarker)
	if !strings.HasPrefix(string(contents), upMarker) || down < 0 {
		t.Fatal("migration directives are incomplete")
	}
	if up {
		return string(contents[len(upMarker):down])
	}

	return string(contents[down+len(downMarker):])
}

func mustPostgreSQLSnapshot(
	t testing.TB,
	stream eventsourcing.StreamID,
	aggregateVersion uint64,
	schemaVersion eventsourcing.SchemaVersion,
	state string,
	createdAt time.Time,
) eventsourcing.Snapshot {
	t.Helper()

	snapshot, err := eventsourcing.NewSnapshot(eventsourcing.SnapshotInput{
		Stream:           stream,
		AggregateVersion: aggregateVersion,
		SchemaVersion:    schemaVersion,
		State:            []byte(state),
		Metadata:         map[string]string{"codec": "json"},
		CreatedAt:        createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}

	return snapshot
}

func loadPostgreSQLStatus(
	t testing.TB,
	ctx context.Context,
	store *eventpostgres.ProjectionStore,
	name string,
) projection.Status {
	t.Helper()
	status, err := store.Status(ctx, name)
	if err != nil {
		t.Fatal(err)
	}

	return status
}

func assertPostgreSQLStatus(
	t testing.TB,
	status projection.Status,
	state projection.RunState,
	checkpoint eventsourcing.GlobalPosition,
	hasCheckpoint bool,
) {
	t.Helper()

	actual, exists := status.Checkpoint()
	if status.State() != state ||
		actual != checkpoint ||
		exists != hasCheckpoint {
		t.Fatalf("status = %#v", status)
	}
}

func assertProjectionAndReadModel(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *eventpostgres.ProjectionStore,
	name string,
	checkpoint eventsourcing.GlobalPosition,
	total int,
	hasCheckpoint bool,
) {
	t.Helper()

	status, err := store.Status(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	assertPostgreSQLStatus(
		t,
		status,
		projection.StateRunning,
		checkpoint,
		hasCheckpoint,
	)
	var actualTotal int
	if err := pool.QueryRow(
		ctx,
		"SELECT total FROM account_summary WHERE id = 1",
	).Scan(&actualTotal); err != nil {
		t.Fatal(err)
	}
	if actualTotal != total {
		t.Fatalf("read-model total = %d", actualTotal)
	}
}
