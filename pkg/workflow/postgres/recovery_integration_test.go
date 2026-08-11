//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestPostgreSQLRestartAndSnapshotRestorePreserveAuthoritativeHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	container, err := tcpostgres.Run(ctx, "postgres:18-alpine",
		tcpostgres.WithDatabase("workflow"),
		tcpostgres.WithUsername("workflow"),
		tcpostgres.WithPassword("workflow"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start recovery PostgreSQL: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if terminateErr := container.Terminate(cleanupCtx); terminateErr != nil {
			t.Errorf("terminate recovery PostgreSQL: %v", terminateErr)
		}
	})
	connection, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("recovery PostgreSQL connection string: %v", err)
	}
	pool := mustRecoveryPool(t, ctx, connection)
	if _, err := pool.Exec(ctx, "CREATE SCHEMA workflow"); err != nil {
		t.Fatalf("create workflow schema: %v", err)
	}
	for _, migration := range SchemaMigrations() {
		if _, err := pool.Exec(ctx, migration.Up); err != nil {
			t.Fatalf("apply workflow migration %d: %v", migration.Version, err)
		}
	}
	store, err := New(pool, Config{})
	if err != nil {
		t.Fatalf("construct recovery store: %v", err)
	}
	created := mustCreateTransition(t)
	if err := store.Commit(ctx, created); err != nil {
		t.Fatalf("commit snapshot baseline: %v", err)
	}
	pool.Close()
	if err := container.Snapshot(ctx); err != nil {
		t.Fatalf("snapshot workflow database: %v", err)
	}

	pool = mustRecoveryPool(t, ctx, connection)
	store, err = New(pool, Config{})
	if err != nil {
		t.Fatalf("reconstruct recovery store: %v", err)
	}
	afterSnapshot := mustAttemptTransition(t, created.Definition())
	if err := store.Commit(ctx, afterSnapshot); err != nil {
		t.Fatalf("commit post-snapshot transition: %v", err)
	}
	pool.Close()

	stopTimeout := 15 * time.Second
	if err := container.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("stop PostgreSQL for restart drill: %v", err)
	}
	if err := container.Start(ctx); err != nil {
		t.Fatalf("restart PostgreSQL: %v", err)
	}
	connection, err = container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("restarted PostgreSQL connection string: %v", err)
	}
	pool = mustRecoveryPool(t, ctx, connection)
	store, err = New(pool, Config{})
	if err != nil {
		t.Fatalf("construct restarted store: %v", err)
	}
	assertRecoveryHistory(t, ctx, store, 3)
	pool.Close()

	if err := container.Restore(ctx); err != nil {
		t.Fatalf("restore workflow database snapshot: %v", err)
	}
	pool = mustRecoveryPool(t, ctx, connection)
	defer pool.Close()
	store, err = New(pool, Config{})
	if err != nil {
		t.Fatalf("construct restored store: %v", err)
	}
	assertRecoveryHistory(t, ctx, store, 2)
	baseline := mustReconciliation(t, created)
	if outcome, reconcileErr := store.ReconcileTransition(ctx, baseline); reconcileErr != nil || outcome != workflow.TransitionCommitted {
		t.Fatalf("reconcile restored baseline = %d, %v", outcome, reconcileErr)
	}
	removed := mustReconciliation(t, afterSnapshot)
	if outcome, reconcileErr := store.ReconcileTransition(ctx, removed); reconcileErr != nil || outcome != workflow.TransitionMissing {
		t.Fatalf("reconcile transition newer than snapshot = %d, %v", outcome, reconcileErr)
	}
}

func mustRecoveryPool(t *testing.T, ctx context.Context, connection string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, connection)
	if err != nil {
		t.Fatalf("connect recovery PostgreSQL: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping recovery PostgreSQL: %v", err)
	}
	return pool
}

func assertRecoveryHistory(t *testing.T, ctx context.Context, store *Store, want int) {
	t.Helper()
	page, err := store.History(ctx, mustHistoryQuery(t, 0, uint32(want+1)))
	if err != nil || len(page.Events()) != want || page.HasMore() {
		t.Fatalf("recovery history count = %d, has more %t, error %v", len(page.Events()), page.HasMore(), err)
	}
}

func mustReconciliation(t *testing.T, transition workflow.Transition) workflow.TransitionReconciliation {
	t.Helper()
	reconciliation, err := workflow.NewTransitionReconciliation(workflow.TransitionReconciliationSpec{
		TransitionID: transition.ID(), Fingerprint: transition.Fingerprint(),
	})
	if err != nil {
		t.Fatalf("construct recovery reconciliation: %v", err)
	}
	return reconciliation
}
