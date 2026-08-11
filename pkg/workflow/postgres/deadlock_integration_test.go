//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type deadlockTransactionResult struct {
	err          error
	committed    []workflow.Transition
	notCommitted []workflow.Transition
}

func TestPostgreSQLDeadlockAbortsOneCompleteTransitionSet(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := integrationPool(t, ctx)

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
		t.Fatalf("construct store: %v", err)
	}
	definition := mustCreateTransition(t).Definition()
	for _, instanceID := range []string{"deadlock-a", "deadlock-b"} {
		if err := store.Commit(ctx, mustDeadlockStart(t, definition, instanceID)); err != nil {
			t.Fatalf("create %s: %v", instanceID, err)
		}
	}
	first := []workflow.Transition{
		mustDeadlockPause(t, definition, "deadlock-a", "deadlock-a-first"),
		mustDeadlockPause(t, definition, "deadlock-b", "deadlock-b-first"),
	}
	second := []workflow.Transition{
		mustDeadlockPause(t, definition, "deadlock-b", "deadlock-b-second"),
		mustDeadlockPause(t, definition, "deadlock-a", "deadlock-a-second"),
	}

	ready := make(chan error, 2)
	proceed := make(chan struct{})
	results := make(chan deadlockTransactionResult, 2)
	go runDeadlockTransaction(ctx, pool, store, first, ready, proceed, results)
	go runDeadlockTransaction(ctx, pool, store, second, ready, proceed, results)
	var firstStageErrors []error
	for range 2 {
		if readyErr := <-ready; readyErr != nil {
			firstStageErrors = append(firstStageErrors, readyErr)
		}
	}
	close(proceed)
	if len(firstStageErrors) != 0 {
		for range 2 {
			<-results
		}
		t.Fatalf("stage first deadlock transitions: %v", firstStageErrors)
	}

	committed := 0
	deadlocked := 0
	var winner, loser []workflow.Transition
	for range 2 {
		result := <-results
		if result.err == nil {
			committed++
			winner = result.committed
			continue
		}
		var databaseError *pgconn.PgError
		if !errors.As(result.err, &databaseError) || databaseError.Code != "40P01" {
			t.Fatalf("deadlock transaction error = %v", result.err)
		}
		deadlocked++
		loser = result.notCommitted
	}
	if committed != 1 || deadlocked != 1 {
		t.Fatalf("deadlock outcomes = committed %d deadlocked %d", committed, deadlocked)
	}
	for _, transition := range winner {
		assertDeadlockReconciliation(t, ctx, store, transition, workflow.TransitionCommitted)
	}
	for _, transition := range loser {
		assertDeadlockReconciliation(t, ctx, store, transition, workflow.TransitionMissing)
	}
	for _, instanceID := range []string{"deadlock-a", "deadlock-b"} {
		query, queryErr := workflow.NewHistoryQuery(workflow.HistoryQuerySpec{InstanceID: instanceID, Limit: 3})
		if queryErr != nil {
			t.Fatalf("construct %s history query: %v", instanceID, queryErr)
		}
		page, historyErr := store.History(ctx, query)
		if historyErr != nil || len(page.Events()) != 2 || page.HasMore() {
			t.Fatalf("%s history after deadlock = %#v, %v", instanceID, page, historyErr)
		}
	}
}

func runDeadlockTransaction(
	ctx context.Context,
	pool *pgxpool.Pool,
	store *Store,
	transitions []workflow.Transition,
	ready chan<- error,
	proceed <-chan struct{},
	results chan<- deadlockTransactionResult,
) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		ready <- err
		results <- deadlockTransactionResult{err: err, notCommitted: transitions}
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, "SET LOCAL deadlock_timeout = '100ms'"); err == nil {
		err = store.Stage(ctx, tx, transitions[0])
	}
	ready <- err
	if err != nil {
		results <- deadlockTransactionResult{err: err, notCommitted: transitions}
		return
	}
	<-proceed
	if err = store.Stage(ctx, tx, transitions[1]); err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		results <- deadlockTransactionResult{err: err, notCommitted: transitions}
		return
	}
	results <- deadlockTransactionResult{committed: transitions}
}

func mustDeadlockStart(
	t *testing.T,
	definition workflow.DefinitionReference,
	instanceID string,
) workflow.Transition {
	t.Helper()
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	event, err := workflow.NewHistoryEvent(workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: instanceID, Kind: workflow.EventInstanceStarted,
		OccurredAt: now, Definition: definition,
	})
	if err != nil {
		t.Fatalf("construct %s start event: %v", instanceID, err)
	}
	transition, err := workflow.NewTransition(workflow.TransitionSpec{
		ID: "start-" + instanceID, InstanceID: instanceID, Definition: definition,
		Events: []workflow.HistoryEvent{event},
	})
	if err != nil {
		t.Fatalf("construct %s start transition: %v", instanceID, err)
	}
	return transition
}

func mustDeadlockPause(
	t *testing.T,
	definition workflow.DefinitionReference,
	instanceID string,
	transitionID string,
) workflow.Transition {
	t.Helper()
	event, err := workflow.NewHistoryEvent(workflow.HistoryEventSpec{
		Sequence: 2, InstanceID: instanceID, Kind: workflow.EventInstancePaused,
		OccurredAt: time.Date(2026, 8, 11, 12, 0, 1, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("construct %s pause event: %v", instanceID, err)
	}
	transition, err := workflow.NewTransition(workflow.TransitionSpec{
		ID: transitionID, InstanceID: instanceID, ExpectedSequence: 1,
		Definition: definition, Events: []workflow.HistoryEvent{event},
	})
	if err != nil {
		t.Fatalf("construct %s pause transition: %v", instanceID, err)
	}
	return transition
}

func assertDeadlockReconciliation(
	t *testing.T,
	ctx context.Context,
	store *Store,
	transition workflow.Transition,
	want workflow.TransitionReconciliationOutcome,
) {
	t.Helper()
	outcome, err := store.ReconcileTransition(ctx, mustReconciliation(t, transition))
	if err != nil || outcome != want {
		t.Fatalf("reconcile %s = %d, %v; want %d", transition.ID(), outcome, err, want)
	}
}
