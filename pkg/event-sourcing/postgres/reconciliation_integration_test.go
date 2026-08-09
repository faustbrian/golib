//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type reconciliationResult struct {
	messages []eventsourcing.Message
	outcome  eventsourcing.CommitOutcome
	err      error
}

func TestPostgreSQLReconcilesAmbiguousAppendWithoutDuplicates(t *testing.T) {
	ctx, pool := newDerivedIntegrationPool(t)
	store, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	stream := mustStream(t, "account", "reconcile-append")
	pending := []eventsourcing.PendingMessage{
		mustPending(t, stream, "reconcile-message-1", 1),
		mustPending(t, stream, "reconcile-message-2", 2),
	}
	expected := eventsourcing.ExpectNewStream()

	messages, outcome, err := store.ReconcileAppend(
		ctx,
		stream,
		expected,
		pending,
	)
	if err != nil || outcome != eventsourcing.CommitNotCommitted || messages != nil {
		t.Fatalf("absent reconciliation = %#v, %d, %v", messages, outcome, err)
	}

	committed, err := store.Append(ctx, stream, expected, pending)
	if err != nil {
		t.Fatal(err)
	}
	messages, outcome, err = store.ReconcileAppend(
		ctx,
		stream,
		expected,
		pending,
	)
	if err != nil || outcome != eventsourcing.CommitCommitted {
		t.Fatalf("committed reconciliation = %#v, %d, %v", messages, outcome, err)
	}
	if len(messages) != len(committed) {
		t.Fatalf("reconciled messages = %d, want %d", len(messages), len(committed))
	}
	for index := range committed {
		if !messages[index].Equal(committed[index]) {
			t.Fatalf("reconciled message %d differs from committed append", index)
		}
	}

	partial := []eventsourcing.PendingMessage{
		pending[0],
		mustPending(t, stream, "reconcile-message-missing", 3),
	}
	messages, outcome, err = store.ReconcileAppend(
		ctx,
		stream,
		expected,
		partial,
	)
	if messages != nil || outcome != eventsourcing.CommitUnknown || !errors.Is(
		err,
		eventpostgres.ErrAppendReconciliationMismatch,
	) {
		t.Fatalf("partial reconciliation = %#v, %d, %v", messages, outcome, err)
	}

	var count int
	if err := pool.QueryRow(
		ctx,
		"SELECT count(*) FROM event_sourcing.messages WHERE aggregate_type = $1 AND aggregate_id = $2",
		stream.AggregateType(),
		stream.AggregateID(),
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != len(pending) {
		t.Fatalf("durable message count = %d, want %d", count, len(pending))
	}
}

func TestPostgreSQLReconciliationWaitsForTransactionResolution(t *testing.T) {
	ctx, pool := newDerivedIntegrationPool(t)
	store, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}

	tests := map[string]struct {
		resolve func(context.Context, pgx.Tx) error
		want    eventsourcing.CommitOutcome
	}{
		"commit": {
			resolve: func(ctx context.Context, tx pgx.Tx) error {
				return tx.Commit(ctx)
			},
			want: eventsourcing.CommitCommitted,
		},
		"rollback": {
			resolve: func(ctx context.Context, tx pgx.Tx) error {
				return tx.Rollback(ctx)
			},
			want: eventsourcing.CommitNotCommitted,
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			stream := mustStream(t, "account", "reconcile-resolution-"+name)
			pending := []eventsourcing.PendingMessage{
				mustPending(t, stream, "reconcile-resolution-message-"+name, 1),
			}
			expected := eventsourcing.ExpectNewStream()
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(
					context.Background(),
					5*time.Second,
				)
				defer cleanupCancel()
				_ = tx.Rollback(cleanupCtx)
			}()
			writer, err := eventpostgres.NewTx(tx, eventpostgres.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Stage(ctx, stream, expected, pending); err != nil {
				t.Fatal(err)
			}

			reconcileCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			result := make(chan reconciliationResult, 1)
			go func() {
				messages, outcome, reconcileErr := store.ReconcileAppend(
					reconcileCtx,
					stream,
					expected,
					pending,
				)
				result <- reconciliationResult{
					messages: messages,
					outcome:  outcome,
					err:      reconcileErr,
				}
			}()

			waitForReconciliationLock(t, reconcileCtx, pool)
			select {
			case early := <-result:
				t.Fatalf("reconciliation returned before resolution: %#v", early)
			default:
			}
			if err := test.resolve(ctx, tx); err != nil {
				t.Fatal(err)
			}
			select {
			case resolved := <-result:
				if resolved.err != nil || resolved.outcome != test.want {
					t.Fatalf("resolved reconciliation = %#v", resolved)
				}
				if test.want == eventsourcing.CommitCommitted &&
					len(resolved.messages) != len(pending) {
					t.Fatalf("committed messages = %d", len(resolved.messages))
				}
				if test.want == eventsourcing.CommitNotCommitted &&
					resolved.messages != nil {
					t.Fatalf("rolled-back messages = %#v", resolved.messages)
				}
			case <-reconcileCtx.Done():
				t.Fatalf("reconciliation did not resolve: %v", reconcileCtx.Err())
			}
		})
	}
}

func waitForReconciliationLock(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()

	deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		err := pool.QueryRow(
			deadline,
			`SELECT EXISTS (
	SELECT 1
	FROM pg_stat_activity
	WHERE wait_event_type = 'Lock'
		AND query LIKE 'SELECT last_position FROM %positions%FOR UPDATE'
)`,
		).Scan(&waiting)
		if err == nil && waiting {
			return
		}
		select {
		case <-deadline.Done():
			t.Fatalf(
				"wait for reconciliation lock: %v: %v",
				deadline.Err(),
				err,
			)
		case <-ticker.C:
		}
	}
}
