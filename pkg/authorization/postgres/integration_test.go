package postgres_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	"github.com/faustbrian/golib/pkg/authorization/policy"
	store "github.com/faustbrian/golib/pkg/authorization/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationAtomicManifestUpdates(t *testing.T) {
	connectionString := os.Getenv("POSTGRES_URL")
	if connectionString == "" {
		t.Skip("POSTGRES_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	migration := store.SchemaMigration()
	_, _ = pool.Exec(ctx, migration.Down)
	if _, err := pool.Exec(ctx, migration.Up); err != nil {
		t.Fatalf("apply migration error = %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), migration.Down) })

	repository, err := store.New(pool)
	if err != nil {
		t.Fatalf("postgres.New() error = %v", err)
	}
	if _, err := repository.Load(ctx); !errors.Is(err, store.ErrNotInitialized) {
		t.Fatalf("initial Load() error = %v, want ErrNotInitialized", err)
	}
	if _, err := repository.Update(ctx, 99, integrationManifest(100)); !errors.Is(err, store.ErrRevisionConflict) {
		t.Fatalf("invalid initialization error = %v, want ErrRevisionConflict", err)
	}

	first := integrationManifest(1)
	stored, err := repository.Update(ctx, 0, first)
	if err != nil || stored.Revision != 1 {
		t.Fatalf("initial Update() = (%+v, %v)", stored, err)
	}
	loaded, err := repository.Load(ctx)
	if err != nil || loaded.Revision != 1 {
		t.Fatalf("Load() = (%+v, %v)", loaded, err)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := repository.Update(canceled, 1, integrationManifest(2)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Update() error = %v, want context.Canceled", err)
	}
	loaded, err = repository.Load(ctx)
	if err != nil || loaded.Revision != 1 {
		t.Fatalf("Load() after canceled update = (%+v, %v), want revision 1", loaded, err)
	}

	type updateResult struct {
		revision authorization.Revision
		err      error
	}
	results := make(chan updateResult, 2)
	var updates sync.WaitGroup
	for revision := authorization.Revision(2); revision <= 3; revision++ {
		updates.Add(1)
		go func() {
			defer updates.Done()
			stored, updateErr := repository.Update(ctx, 1, integrationManifest(revision))
			results <- updateResult{revision: stored.Revision, err: updateErr}
		}()
	}
	updates.Wait()
	close(results)
	successes := 0
	conflicts := 0
	winner := authorization.Revision(0)
	for result := range results {
		switch {
		case result.err == nil:
			successes++
			winner = result.revision
		case errors.Is(result.err, store.ErrRevisionConflict):
			conflicts++
		default:
			t.Fatalf("concurrent Update() error = %v", result.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent updates = %d successes, %d conflicts; want 1 each", successes, conflicts)
	}
	loaded, err = repository.Load(ctx)
	if err != nil || loaded.Revision != winner {
		t.Fatalf("Load() after concurrent updates = (%+v, %v), want revision %d", loaded, err, winner)
	}

	victim, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire backend connection: %v", err)
	}
	backendPID := victim.Conn().PgConn().PID()
	var terminated bool
	if err := pool.QueryRow(ctx, "SELECT pg_terminate_backend($1)", backendPID).Scan(&terminated); err != nil {
		victim.Release()
		t.Fatalf("terminate backend: %v", err)
	}
	victim.Release()
	if !terminated {
		t.Fatal("pg_terminate_backend() = false")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		loaded, err = repository.Load(ctx)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Load() did not recover after backend termination: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if loaded.Revision != winner {
		t.Fatalf("reconnected Load().Revision = %d, want %d", loaded.Revision, winner)
	}

	if _, err := repository.Update(ctx, 0, integrationManifest(2)); !errors.Is(err, store.ErrRevisionConflict) {
		t.Fatalf("conflicting Update() error = %v, want ErrRevisionConflict", err)
	}
	if _, err := repository.Update(ctx, 1, first); !errors.Is(err, store.ErrRevisionNotMonotonic) {
		t.Fatalf("stale Update() error = %v, want ErrRevisionNotMonotonic", err)
	}
}

func integrationManifest(revision authorization.Revision) policy.Manifest {
	return policy.Manifest{
		Format: policy.FormatV1, Revision: revision,
		Algorithm: policy.AlgorithmDenyOverrides,
		Policies:  []policy.Record{},
	}
}
