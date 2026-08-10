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
	listQuery, err := workflow.NewInstanceListQuery(workflow.InstanceListQuerySpec{
		Selection: workflow.ListActiveInstances, Limit: 10,
	})
	if err != nil {
		t.Fatalf("construct instance list: %v", err)
	}
	listed, err := store.ListInstances(ctx, listQuery)
	if err != nil || len(listed.Items()) != 1 || listed.Items()[0].InstanceID() != created.InstanceID() {
		t.Fatalf("list durable instance = %#v, %v", listed, err)
	}
	reconciliation, err := workflow.NewTransitionReconciliation(workflow.TransitionReconciliationSpec{
		TransitionID: created.ID(), Fingerprint: created.Fingerprint(),
	})
	if err != nil {
		t.Fatalf("construct transition reconciliation: %v", err)
	}
	if outcome, err := store.ReconcileTransition(ctx, reconciliation); err != nil || outcome != workflow.TransitionCommitted {
		t.Fatalf("reconcile committed transition = %d, %v", outcome, err)
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

	claimTime := time.Date(2026, 8, 9, 12, 0, 2, 0, time.UTC)
	claim, err := workflow.NewWorkClaimRequest(workflow.WorkClaimRequestSpec{
		Owner: "worker-1", Now: claimTime, LeaseDuration: 30 * time.Second, Limit: 10,
	})
	if err != nil {
		t.Fatalf("construct work claim: %v", err)
	}
	leases, err := store.Claim(ctx, claim)
	if err != nil {
		t.Fatalf("claim due work: %v", err)
	}
	if len(leases) != 1 || leases[0].Work().ID() != "work-1" ||
		leases[0].Attempt() != 1 || leases[0].Token() != 1 {
		t.Fatalf("claimed leases = %#v", leases)
	}
	earlyClaim, err := workflow.NewWorkClaimRequest(workflow.WorkClaimRequestSpec{
		Owner: "worker-2", Now: leases[0].ExpiresAt().Add(-time.Nanosecond),
		LeaseDuration: time.Minute, Limit: 10,
	})
	if err != nil {
		t.Fatalf("construct competing claim: %v", err)
	}
	if competing, err := store.Claim(ctx, earlyClaim); err != nil || len(competing) != 0 {
		t.Fatalf("live lease was reclaimed = %#v, %v", competing, err)
	}
	recoveryClaim, err := workflow.NewWorkClaimRequest(workflow.WorkClaimRequestSpec{
		Owner: "worker-2", Now: leases[0].ExpiresAt(), LeaseDuration: 30 * time.Second, Limit: 10,
	})
	if err != nil {
		t.Fatalf("construct recovery claim: %v", err)
	}
	recovered, err := store.Claim(ctx, recoveryClaim)
	if err != nil || len(recovered) != 1 || recovered[0].Attempt() != 2 || recovered[0].Token() != 2 {
		t.Fatalf("recovered claim = %#v, %v", recovered, err)
	}
	staleCompletion, err := workflow.NewWorkCompletion(workflow.WorkCompletionSpec{
		WorkID: "work-1", Owner: "worker-1", Token: 1, CompletedAt: recoveryClaim.Now(),
	})
	if err != nil {
		t.Fatalf("construct stale completion: %v", err)
	}
	if err := store.Complete(ctx, staleCompletion); !errors.Is(err, workflow.ErrStaleWorkLease) {
		t.Fatalf("stale completion = %v", err)
	}
	renewal, err := workflow.NewWorkLeaseRenewal(workflow.WorkLeaseRenewalSpec{
		WorkID: "work-1", Owner: "worker-2", Token: 2,
		Now: recoveryClaim.Now().Add(10 * time.Second), ExtendBy: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("construct renewal: %v", err)
	}
	renewed, err := store.Renew(ctx, renewal)
	if err != nil || renewed.Token() != 2 || renewed.ExpiresAt() != renewal.Now().Add(10*time.Second) {
		t.Fatalf("renew work = %#v, %v", renewed, err)
	}
	retryAt := renewed.ExpiresAt().Add(time.Second)
	failure, err := workflow.NewWorkFailure(workflow.WorkFailureSpec{
		WorkID: "work-1", Owner: "worker-2", Token: 2, FailedAt: renewal.Now(),
		Code: "temporary", Disposition: workflow.WorkRetry, RetryAt: retryAt,
	})
	if err != nil {
		t.Fatalf("construct retry failure: %v", err)
	}
	if err := store.Fail(ctx, failure); err != nil {
		t.Fatalf("retry work: %v", err)
	}
	beforeRetry, err := workflow.NewWorkClaimRequest(workflow.WorkClaimRequestSpec{
		Owner: "worker-3", Now: retryAt.Add(-time.Nanosecond), LeaseDuration: time.Minute, Limit: 10,
	})
	if err != nil {
		t.Fatalf("construct early claim: %v", err)
	}
	if leases, err := store.Claim(ctx, beforeRetry); err != nil || len(leases) != 0 {
		t.Fatalf("early retry claim = %#v, %v", leases, err)
	}
	afterRetry, err := workflow.NewWorkClaimRequest(workflow.WorkClaimRequestSpec{
		Owner: "worker-3", Now: retryAt, LeaseDuration: time.Minute, Limit: 10,
	})
	if err != nil {
		t.Fatalf("construct retry claim: %v", err)
	}
	retried, err := store.Claim(ctx, afterRetry)
	if err != nil || len(retried) != 1 || retried[0].Attempt() != 3 || retried[0].Token() != 3 {
		t.Fatalf("retried claim = %#v, %v", retried, err)
	}
	completion, err := workflow.NewWorkCompletion(workflow.WorkCompletionSpec{
		WorkID: "work-1", Owner: "worker-3", Token: 3, CompletedAt: retryAt,
	})
	if err != nil {
		t.Fatalf("construct completion: %v", err)
	}
	if err := store.Complete(ctx, completion); err != nil {
		t.Fatalf("complete work: %v", err)
	}
	if leases, err := store.Claim(ctx, afterRetry); err != nil || len(leases) != 0 {
		t.Fatalf("completed work was claimable = %#v, %v", leases, err)
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

	completedAt := retryAt.Add(time.Minute)
	completedEvent, err := workflow.NewHistoryEvent(workflow.HistoryEventSpec{
		Sequence: 4, InstanceID: created.InstanceID(), Kind: workflow.EventInstanceCompleted,
		OccurredAt: completedAt, Data: []byte("complete"),
	})
	if err != nil {
		t.Fatalf("construct completion event: %v", err)
	}
	completedTransition, err := workflow.NewTransition(workflow.TransitionSpec{
		ID: "transition-completed", InstanceID: created.InstanceID(), ExpectedSequence: 3,
		Definition: created.Definition(), Events: []workflow.HistoryEvent{completedEvent},
	})
	if err != nil {
		t.Fatalf("construct completion transition: %v", err)
	}
	if err := store.Commit(ctx, completedTransition); err != nil {
		t.Fatalf("commit completion: %v", err)
	}
	if active, err := store.ListInstances(ctx, listQuery); err != nil || len(active.Items()) != 0 {
		t.Fatalf("list active after completion = %#v, %v", active, err)
	}
	archivedQuery, err := workflow.NewInstanceListQuery(workflow.InstanceListQuerySpec{
		Selection: workflow.ListArchivedInstances, Limit: 10,
	})
	if err != nil {
		t.Fatalf("construct archived list query: %v", err)
	}
	archived, err := store.ListInstances(ctx, archivedQuery)
	if err != nil || len(archived.Items()) != 1 ||
		archived.Items()[0].InstanceID() != created.InstanceID() ||
		!archived.Items()[0].ArchivedAt().Equal(completedAt) {
		t.Fatalf("list archived after completion = %#v, %v", archived, err)
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

func TestPostgreSQLDeadLetterResolutionIsAuditedAndFenced(t *testing.T) {
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
	created := mustCreateTransition(t)
	if err := store.Commit(ctx, created); err != nil {
		t.Fatalf("commit workflow: %v", err)
	}
	claimAt := created.Work()[0].AvailableAt()
	claim, err := workflow.NewWorkClaimRequest(workflow.WorkClaimRequestSpec{
		Owner: "worker-1", Now: claimAt, LeaseDuration: 30 * time.Second, Limit: 1,
	})
	if err != nil {
		t.Fatalf("construct claim: %v", err)
	}
	leases, err := store.Claim(ctx, claim)
	if err != nil || len(leases) != 1 {
		t.Fatalf("claim work = %#v, %v", leases, err)
	}
	deadAt := claimAt.Add(time.Second)
	dead, err := workflow.NewWorkFailure(workflow.WorkFailureSpec{
		WorkID: leases[0].Work().ID(), Owner: leases[0].Owner(), Token: leases[0].Token(),
		FailedAt: deadAt, Code: "poison", Disposition: workflow.WorkDeadLetter,
	})
	if err != nil {
		t.Fatalf("construct dead letter: %v", err)
	}
	if err := store.Fail(ctx, dead); err != nil {
		t.Fatalf("dead-letter work: %v", err)
	}
	query, err := workflow.NewDeadLetterQuery(workflow.DeadLetterQuerySpec{Limit: 10})
	if err != nil {
		t.Fatalf("construct dead-letter query: %v", err)
	}
	page, err := store.ListDeadLetters(ctx, query)
	if err != nil || len(page.Items()) != 1 || page.Items()[0].Token() != 1 ||
		page.Items()[0].Attempt() != 1 || page.Items()[0].FailureCode() != "poison" {
		t.Fatalf("list first dead letter = %#v, %v", page, err)
	}
	retryAt := deadAt.Add(2 * time.Second)
	retry, err := workflow.NewDeadLetterResolution(workflow.DeadLetterResolutionSpec{
		CommandID: "resolve-work-1", WorkID: "work-1", Token: 1,
		Action: workflow.DeadLetterRetry, Actor: "operator-1", Reason: "payload-repaired",
		OccurredAt: deadAt.Add(time.Second), RetryAt: retryAt, Deadline: retryAt.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("construct retry resolution: %v", err)
	}
	if err := store.ResolveDeadLetter(ctx, retry); err != nil {
		t.Fatalf("retry dead letter: %v", err)
	}
	if err := store.ResolveDeadLetter(ctx, retry); err != nil {
		t.Fatalf("replay retry resolution: %v", err)
	}
	stale, _ := workflow.NewDeadLetterResolution(workflow.DeadLetterResolutionSpec{
		CommandID: "resolve-stale", WorkID: "work-1", Token: 1,
		Action: workflow.DeadLetterDiscard, Actor: "operator-1", Reason: "discard-work",
		OccurredAt: retryAt,
	})
	if err := store.ResolveDeadLetter(ctx, stale); !errors.Is(err, workflow.ErrStaleWorkLease) {
		t.Fatalf("stale resolution = %v", err)
	}
	reclaim, _ := workflow.NewWorkClaimRequest(workflow.WorkClaimRequestSpec{
		Owner: "worker-2", Now: retryAt, LeaseDuration: 30 * time.Second, Limit: 1,
	})
	retried, err := store.Claim(ctx, reclaim)
	if err != nil || len(retried) != 1 || retried[0].Token() != 2 || retried[0].Attempt() != 2 {
		t.Fatalf("claim retried dead letter = %#v, %v", retried, err)
	}
	deadAgain, _ := workflow.NewWorkFailure(workflow.WorkFailureSpec{
		WorkID: "work-1", Owner: "worker-2", Token: 2, FailedAt: retryAt.Add(time.Second),
		Code: "still-poison", Disposition: workflow.WorkDeadLetter,
	})
	if err := store.Fail(ctx, deadAgain); err != nil {
		t.Fatalf("dead-letter retried work: %v", err)
	}
	page, err = store.ListDeadLetters(ctx, query)
	if err != nil || len(page.Items()) != 1 || page.Items()[0].Token() != 2 {
		t.Fatalf("list second dead letter = %#v, %v", page, err)
	}
	discard, _ := workflow.NewDeadLetterResolution(workflow.DeadLetterResolutionSpec{
		CommandID: "discard-work-1", WorkID: "work-1", Token: 2,
		Action: workflow.DeadLetterDiscard, Actor: "operator-1", Reason: "manual-discard",
		OccurredAt: retryAt.Add(2 * time.Second),
	})
	if err := store.ResolveDeadLetter(ctx, discard); err != nil {
		t.Fatalf("discard dead letter: %v", err)
	}
	if page, err = store.ListDeadLetters(ctx, query); err != nil || len(page.Items()) != 0 {
		t.Fatalf("list resolved dead letters = %#v, %v", page, err)
	}
	var resolutionCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM workflow.workflow_work_resolutions").Scan(&resolutionCount); err != nil || resolutionCount != 2 {
		t.Fatalf("resolution audit count = %d, %v", resolutionCount, err)
	}
}

func TestPostgreSQLStageComposesWithCallerOwnedTransaction(t *testing.T) {
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
	if _, err := pool.Exec(ctx, "CREATE TABLE workflow.effects (id text PRIMARY KEY)"); err != nil {
		t.Fatalf("create companion effect table: %v", err)
	}
	store, err := New(pool, Config{})
	if err != nil {
		t.Fatalf("construct store: %v", err)
	}
	transition := mustCreateTransition(t)

	stage := func(commit bool) {
		t.Helper()
		tx, beginErr := pool.Begin(ctx)
		if beginErr != nil {
			t.Fatalf("begin caller transaction: %v", beginErr)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if stageErr := store.Stage(ctx, tx, transition); stageErr != nil {
			t.Fatalf("stage workflow transition: %v", stageErr)
		}
		if _, execErr := tx.Exec(ctx, "INSERT INTO workflow.effects (id) VALUES ('effect-1')"); execErr != nil {
			t.Fatalf("stage companion effect: %v", execErr)
		}
		if commit {
			if commitErr := tx.Commit(ctx); commitErr != nil {
				t.Fatalf("commit caller transaction: %v", commitErr)
			}
		}
	}
	stage(false)
	for table, want := range map[string]int{"workflow_instances": 0, "effects": 0} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM workflow."+table).Scan(&count); err != nil || count != want {
			t.Fatalf("rolled-back %s count = %d, %v", table, count, err)
		}
	}
	stage(true)
	for table, want := range map[string]int{"workflow_instances": 1, "effects": 1} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM workflow."+table).Scan(&count); err != nil || count != want {
			t.Fatalf("committed %s count = %d, %v", table, count, err)
		}
	}
	replay, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin replay transaction: %v", err)
	}
	defer func() { _ = replay.Rollback(ctx) }()
	if err := store.Stage(ctx, replay, transition); err != nil {
		t.Fatalf("stage exact replay: %v", err)
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
