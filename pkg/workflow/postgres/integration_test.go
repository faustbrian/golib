//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestPostgreSQLAtomicTransitionsAndStableHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := integrationPool(t, ctx)

	if _, err := pool.Exec(ctx, "CREATE SCHEMA workflow"); err != nil {
		t.Fatalf("create workflow schema: %v", err)
	}
	migration := SchemaMigration()
	if _, err := pool.Exec(ctx, migration.Up); err != nil {
		t.Fatalf("apply workflow migration: %v", err)
	}
	store, err := New(pool, Config{})
	if err != nil {
		t.Fatalf("construct store: %v", err)
	}

	created := mustCreateTransition(t)
	if err := store.Commit(ctx, created); err != nil {
		var databaseError *pgconn.PgError
		if errors.As(err, &databaseError) {
			t.Fatalf("commit create: SQLSTATE %s constraint %q table %q column %q", databaseError.Code, databaseError.ConstraintName, databaseError.TableName, databaseError.ColumnName)
		}
		t.Fatalf("commit create: %v", err)
	}
	if err := store.Commit(ctx, created); err != nil {
		t.Fatalf("replay create: %v", err)
	}

	query := mustHistoryQuery(t, 0, 1)
	first, err := store.History(ctx, query)
	if err != nil {
		t.Fatalf("first history page: %v", err)
	}
	if len(first.Events()) != 1 || first.Events()[0].Sequence() != 1 || !first.HasMore() {
		t.Fatalf("first history page = %#v", first)
	}
	secondQuery := mustHistoryQuery(t, first.NextAfterSequence(), 2)
	second, err := store.History(ctx, secondQuery)
	if err != nil {
		t.Fatalf("second history page: %v", err)
	}
	if len(second.Events()) != 1 || second.Events()[0].Sequence() != 2 || second.HasMore() {
		t.Fatalf("second history page = %#v", second)
	}

	attempt := mustAttemptTransition(t, created.Definition())
	if err := store.Commit(ctx, attempt); err != nil {
		t.Fatalf("commit attempt: %v", err)
	}
	attemptPage, err := store.History(ctx, mustHistoryQuery(t, 2, 1))
	if err != nil {
		t.Fatalf("read attempt: %v", err)
	}
	if len(attemptPage.Events()) != 1 || attemptPage.Events()[0].DueAt().IsZero() {
		t.Fatal("activity attempt deadline was not persisted")
	}

	failed := mustDuplicateWorkTransition(t, created.Definition())
	if err := store.Commit(ctx, failed); err == nil || workflow.StoreCommitOutcomeOf(err) != workflow.StoreCommitNotCommitted {
		t.Fatalf("duplicate-work transition = %v", err)
	}
	var historyCount, transitionCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM workflow.workflow_history WHERE instance_id = $1", created.InstanceID()).Scan(&historyCount); err != nil {
		t.Fatalf("count history: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM workflow.workflow_transitions WHERE transition_id = $1", failed.ID()).Scan(&transitionCount); err != nil {
		t.Fatalf("count failed transition: %v", err)
	}
	if historyCount != 3 || transitionCount != 0 {
		t.Fatalf("failed transaction visibility = history %d transition %d", historyCount, transitionCount)
	}

	missing, err := workflow.NewHistoryQuery(workflow.HistoryQuerySpec{InstanceID: "missing", Limit: 1})
	if err != nil {
		t.Fatalf("construct missing query: %v", err)
	}
	if _, err := store.History(ctx, missing); !errors.Is(err, workflow.ErrStoreNotFound) {
		t.Fatalf("missing history = %v", err)
	}

	if _, err := pool.Exec(ctx, migration.Down); err != nil {
		t.Fatalf("roll back workflow migration: %v", err)
	}
	var historyTableExists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('workflow.workflow_history') IS NOT NULL").Scan(&historyTableExists); err != nil {
		t.Fatalf("inspect rolled-back migration: %v", err)
	}
	if historyTableExists {
		t.Fatal("workflow migration rollback left the history table behind")
	}
}

func integrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	connection := os.Getenv("WORKFLOW_POSTGRES_URL")
	if connection == "" {
		container, err := tcpostgres.Run(ctx, "postgres:18-alpine",
			tcpostgres.WithDatabase("workflow"),
			tcpostgres.WithUsername("workflow"),
			tcpostgres.WithPassword("workflow"),
			tcpostgres.BasicWaitStrategies(),
		)
		if err != nil {
			t.Fatalf("start PostgreSQL: %v", err)
		}
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := container.Terminate(cleanupCtx); err != nil {
				t.Errorf("terminate PostgreSQL: %v", err)
			}
		})
		connection, err = container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			t.Fatalf("PostgreSQL connection string: %v", err)
		}
	}
	pool, err := pgxpool.New(ctx, connection)
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping PostgreSQL: %v", err)
	}
	return pool
}

func mustAttemptTransition(t *testing.T, definition workflow.DefinitionReference) workflow.Transition {
	t.Helper()
	now := time.Date(2026, 8, 9, 12, 3, 0, 0, time.UTC)
	event, err := workflow.NewHistoryEvent(workflow.HistoryEventSpec{
		Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptStarted,
		OccurredAt: now, StepName: "execute", Attempt: 1,
		IdempotencyKey: "attempt-1", DueAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("construct attempt event: %v", err)
	}
	transition, err := workflow.NewTransition(workflow.TransitionSpec{
		ID: "transition-attempt", InstanceID: "instance-1", ExpectedSequence: 2,
		Definition: definition, Events: []workflow.HistoryEvent{event},
	})
	if err != nil {
		t.Fatalf("construct attempt transition: %v", err)
	}
	return transition
}

func mustDuplicateWorkTransition(t *testing.T, definition workflow.DefinitionReference) workflow.Transition {
	t.Helper()
	now := time.Date(2026, 8, 9, 12, 4, 0, 0, time.UTC)
	event, err := workflow.NewHistoryEvent(workflow.HistoryEventSpec{
		Sequence: 4, InstanceID: "instance-1", Kind: workflow.EventActivityRetryScheduled,
		OccurredAt: now, StepName: "execute", Attempt: 2, DueAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("construct retry event: %v", err)
	}
	work, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
		ID: "work-1", Kind: workflow.WorkActivity, InstanceID: "instance-1", Sequence: 4,
		AvailableAt: now.Add(time.Second), Deadline: now.Add(time.Minute), Payload: []byte("input"),
	})
	if err != nil {
		t.Fatalf("construct duplicate work: %v", err)
	}
	transition, err := workflow.NewTransition(workflow.TransitionSpec{
		ID: "transition-failed", InstanceID: "instance-1", ExpectedSequence: 3,
		Definition: definition, Events: []workflow.HistoryEvent{event}, Work: []workflow.PendingWork{work},
	})
	if err != nil {
		t.Fatalf("construct duplicate-work transition: %v", err)
	}
	return transition
}
