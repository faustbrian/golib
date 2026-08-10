package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNewRejectsMissingPoolAndUnsafeSchema(t *testing.T) {
	t.Parallel()

	if _, err := New(nil, Config{}); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
		t.Fatalf("nil pool error = %v", err)
	}
	if _, err := New(nil, Config{Schema: "unsafe-schema"}); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
		t.Fatalf("unsafe schema error = %v", err)
	}
	store, err := New(&pgxpool.Pool{}, Config{})
	if err != nil || !strings.Contains(store.queries.instanceExists, `"workflow"."workflow_instances"`) {
		t.Fatalf("default store = %#v, %v", store, err)
	}
}

func TestSchemaMigrationIsVersionedAndOwnsAtomicStoreTables(t *testing.T) {
	t.Parallel()

	migration, err := SchemaMigrationFor("tenant_workflow")
	if err != nil {
		t.Fatalf("construct migration: %v", err)
	}
	if migration.Version != 1 || migration.Name != "create_workflow_store" {
		t.Fatalf("migration identity = %d %q", migration.Version, migration.Name)
	}
	for _, table := range []string{"workflow_instances", "workflow_transitions", "workflow_history", "workflow_work"} {
		if !strings.Contains(migration.Up, `"tenant_workflow"."`+table+`"`) {
			t.Fatalf("migration missing qualified table %s", table)
		}
	}
	if !strings.Contains(migration.Up, "workflow_work_due_idx") {
		t.Fatal("migration does not provide the due-work claim index")
	}
	if strings.Contains(migration.Up, "data bytea NOT NULL") || strings.Contains(migration.Up, "payload bytea NOT NULL") {
		t.Fatal("migration rejects valid nil event or work payloads")
	}
	if _, err := SchemaMigrationFor("unsafe-schema"); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
		t.Fatalf("unsafe migration schema error = %v", err)
	}
	migrations, err := SchemaMigrationsFor("tenant_workflow")
	if err != nil || len(migrations) != 2 || migrations[0] != migration ||
		migrations[1].Version != 2 || migrations[1].Name != "add_workflow_dead_letter_resolutions" ||
		!strings.Contains(migrations[1].Up, `"tenant_workflow"."workflow_work_resolutions"`) ||
		!strings.Contains(migrations[1].Up, `CREATE INDEX "workflow_work_dead_letter_idx"`) ||
		!strings.Contains(migrations[1].Down, `DROP INDEX "tenant_workflow"."workflow_work_dead_letter_idx"`) {
		t.Fatalf("schema migrations = %#v, %v", migrations, err)
	}
	if defaults := SchemaMigrations(); len(defaults) != 2 ||
		!strings.Contains(defaults[1].Up, `"workflow"."workflow_work_resolutions"`) {
		t.Fatalf("default schema migrations = %#v", defaults)
	}
	if _, err := SchemaMigrationsFor("unsafe-schema"); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
		t.Fatalf("unsafe migration list error = %v", err)
	}
}

func TestCommitAtomicallyPersistsHistoryAndDueWork(t *testing.T) {
	t.Parallel()

	transition := mustCreateTransition(t)
	tx := &fakeTransaction{
		rows: []rowScanner{
			&fakeRow{err: pgx.ErrNoRows},
			&fakeRow{values: []any{transition.Fingerprint()}},
		},
		execResults: []commandResult{
			fakeCommandResult(1), fakeCommandResult(1), fakeCommandResult(1),
			fakeCommandResult(1), fakeCommandResult(1),
		},
	}
	store := newStore(&fakeDatabase{tx: tx}, "workflow")
	if err := store.Commit(context.Background(), transition); err != nil {
		t.Fatalf("commit transition: %v", err)
	}
	if !tx.committed || len(tx.execQueries) != 5 || len(tx.rowQueries) != 2 {
		t.Fatalf("transaction calls = commit %t exec %d rows %d", tx.committed, len(tx.execQueries), len(tx.rowQueries))
	}
	if !strings.Contains(tx.execQueries[2], "workflow_history") || !strings.Contains(tx.execQueries[3], "workflow_work") {
		t.Fatal("history and due work were not staged in the commit transaction")
	}
}

func TestCommitTreatsExactTransitionReplayAsIdempotent(t *testing.T) {
	t.Parallel()

	transition := mustCreateTransition(t)
	tx := &fakeTransaction{rows: []rowScanner{&fakeRow{values: []any{transition.Fingerprint()}}}}
	store := newStore(&fakeDatabase{tx: tx}, "workflow")
	if err := store.Commit(context.Background(), transition); err != nil {
		t.Fatalf("replay exact transition: %v", err)
	}
	if tx.committed || len(tx.execQueries) != 0 || !tx.rolledBack {
		t.Fatal("idempotent replay performed a second durable write")
	}
	remaining := time.Until(tx.rollbackDeadline)
	if remaining < 4*time.Second || remaining > rollbackTimeout {
		t.Fatalf("rollback deadline remaining = %s", remaining)
	}
}

func TestCommitClassifiesValidationAndTransactionFailures(t *testing.T) {
	t.Parallel()

	transition := mustCreateTransition(t)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	beginFailure := errors.New("begin failure containing secret")
	tests := []struct {
		name  string
		store *Store
		ctx   context.Context
		plan  workflow.Transition
		cause error
	}{
		{name: "nil store", ctx: context.Background(), plan: transition, cause: workflow.ErrInvalidStoreRequest},
		{name: "nil database", store: &Store{}, ctx: context.Background(), plan: transition, cause: workflow.ErrInvalidStoreRequest},
		{name: "nil context", store: newStore(&fakeDatabase{}, "workflow"), plan: transition, cause: workflow.ErrInvalidStoreRequest},
		{name: "invalid transition", store: newStore(&fakeDatabase{}, "workflow"), ctx: context.Background(), cause: workflow.ErrInvalidStoreRequest},
		{name: "canceled", store: newStore(&fakeDatabase{}, "workflow"), ctx: canceled, plan: transition, cause: context.Canceled},
		{name: "begin", store: newStore(&fakeDatabase{beginErr: beginFailure}, "workflow"), ctx: context.Background(), plan: transition, cause: beginFailure},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.store.Commit(test.ctx, test.plan)
			if !errors.Is(err, test.cause) || workflow.StoreCommitOutcomeOf(err) != workflow.StoreCommitNotCommitted {
				t.Fatalf("error = %v outcome = %d", err, workflow.StoreCommitOutcomeOf(err))
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("commit error exposed driver details")
			}
		})
	}
}

func TestStageRejectsInvalidCallerOwnedTransactionRequests(t *testing.T) {
	t.Parallel()

	transition := mustCreateTransition(t)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name       string
		store      *Store
		ctx        context.Context
		tx         pgx.Tx
		transition workflow.Transition
		want       error
	}{
		{name: "nil store", ctx: context.Background(), tx: &inertPGXTx{}, transition: transition, want: workflow.ErrInvalidStoreRequest},
		{name: "nil database", store: &Store{}, ctx: context.Background(), tx: &inertPGXTx{}, transition: transition, want: workflow.ErrInvalidStoreRequest},
		{name: "nil context", store: newStore(&fakeDatabase{}, "workflow"), tx: &inertPGXTx{}, transition: transition, want: workflow.ErrInvalidStoreRequest},
		{name: "nil transaction", store: newStore(&fakeDatabase{}, "workflow"), ctx: context.Background(), transition: transition, want: workflow.ErrInvalidStoreRequest},
		{name: "invalid transition", store: newStore(&fakeDatabase{}, "workflow"), ctx: context.Background(), tx: &inertPGXTx{}, want: workflow.ErrInvalidStoreRequest},
		{name: "canceled", store: newStore(&fakeDatabase{}, "workflow"), ctx: canceled, tx: &inertPGXTx{}, transition: transition, want: context.Canceled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.store.Stage(test.ctx, test.tx, test.transition)
			if !errors.Is(err, test.want) ||
				workflow.StoreCommitOutcomeOf(err) != workflow.StoreCommitNotCommitted {
				t.Fatalf("stage error = %v outcome %d", err, workflow.StoreCommitOutcomeOf(err))
			}
		})
	}
}

type inertPGXTx struct{ pgx.Tx }

func TestCommitRejectsConflictingTransitionIdentity(t *testing.T) {
	t.Parallel()

	transition := mustCreateTransition(t)
	tx := &fakeTransaction{rows: []rowScanner{&fakeRow{values: []any{strings.Repeat("f", 64)}}}}
	err := newStore(&fakeDatabase{tx: tx}, "workflow").Commit(context.Background(), transition)
	if !errors.Is(err, workflow.ErrDuplicateTransition) || workflow.StoreCommitOutcomeOf(err) != workflow.StoreCommitNotCommitted {
		t.Fatalf("conflicting identity error = %v", err)
	}
}

func TestCommitExistingInstanceUsesOptimisticDefinitionAndSequence(t *testing.T) {
	t.Parallel()

	transition := mustExistingTransition(t)
	tx := &fakeTransaction{
		rows: []rowScanner{
			&fakeRow{err: pgx.ErrNoRows},
			&fakeRow{values: []any{int64(1), transition.Definition().Name(), transition.Definition().Version(), transition.Definition().Fingerprint()}},
			&fakeRow{values: []any{transition.Fingerprint()}},
		},
		execResults: []commandResult{fakeCommandResult(1), fakeCommandResult(1), fakeCommandResult(1)},
	}
	if err := newStore(&fakeDatabase{tx: tx}, "workflow").Commit(context.Background(), transition); err != nil {
		t.Fatalf("commit existing transition: %v", err)
	}
	if !tx.committed || !strings.Contains(tx.rowQueries[1], "FOR UPDATE") {
		t.Fatal("existing instance was not fenced by a row lock")
	}
}

func TestCommitFailureBoundariesRemainNotCommittedUntilCommit(t *testing.T) {
	t.Parallel()

	failure := errors.New("database failure")
	tests := []struct {
		name string
		tx   *fakeTransaction
		plan workflow.Transition
		want error
	}{
		{name: "lookup", plan: mustCreateTransition(t), tx: &fakeTransaction{rows: []rowScanner{&fakeRow{err: failure}}}, want: failure},
		{name: "create instance", plan: mustCreateTransition(t), tx: &fakeTransaction{rows: []rowScanner{&fakeRow{err: pgx.ErrNoRows}}, execErrors: []error{failure}}, want: failure},
		{name: "missing existing", plan: mustExistingTransition(t), tx: &fakeTransaction{rows: []rowScanner{&fakeRow{err: pgx.ErrNoRows}, &fakeRow{err: pgx.ErrNoRows}}}, want: workflow.ErrStoreNotFound},
		{name: "lock existing", plan: mustExistingTransition(t), tx: &fakeTransaction{rows: []rowScanner{&fakeRow{err: pgx.ErrNoRows}, &fakeRow{err: failure}}}, want: failure},
		{name: "insert transition", plan: mustCreateTransition(t), tx: &fakeTransaction{rows: []rowScanner{&fakeRow{err: pgx.ErrNoRows}, &fakeRow{err: failure}}, execResults: []commandResult{fakeCommandResult(1)}}, want: failure},
		{name: "history", plan: mustCreateTransition(t), tx: &fakeTransaction{rows: []rowScanner{&fakeRow{err: pgx.ErrNoRows}, &fakeRow{values: []any{mustCreateTransition(t).Fingerprint()}}}, execResults: []commandResult{fakeCommandResult(1)}, execErrors: []error{nil, failure}}, want: failure},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := newStore(&fakeDatabase{tx: test.tx}, "workflow").Commit(context.Background(), test.plan)
			if !errors.Is(err, test.want) || workflow.StoreCommitOutcomeOf(err) != workflow.StoreCommitNotCommitted {
				t.Fatalf("error = %v outcome = %d", err, workflow.StoreCommitOutcomeOf(err))
			}
		})
	}
}

func TestCommitFailureAfterCommitAttemptIsUnknown(t *testing.T) {
	t.Parallel()

	transition := mustCreateTransition(t)
	commitFailure := errors.New("commit transport failure")
	tx := &fakeTransaction{
		rows: []rowScanner{&fakeRow{err: pgx.ErrNoRows}, &fakeRow{values: []any{transition.Fingerprint()}}},
		execResults: []commandResult{
			fakeCommandResult(1), fakeCommandResult(1), fakeCommandResult(1),
			fakeCommandResult(1), fakeCommandResult(1),
		},
		commitErr: commitFailure,
	}
	err := newStore(&fakeDatabase{tx: tx}, "workflow").Commit(context.Background(), transition)
	if !errors.Is(err, commitFailure) || workflow.StoreCommitOutcomeOf(err) != workflow.StoreCommitUnknown {
		t.Fatalf("commit failure = %v outcome = %d", err, workflow.StoreCommitOutcomeOf(err))
	}
}

func TestHistoryReturnsStableValidatedPages(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	database := &fakeDatabase{
		row: &fakeRow{values: []any{true}},
		queryRows: &fakeRows{rows: [][]any{
			{int64(1), int16(workflow.EventInstancePaused), now, "", "", "", "", "", int64(0), "", (*time.Time)(nil), "", false, []byte(nil)},
			{int64(2), int16(workflow.EventInstanceResumed), now.Add(time.Second), "", "", "", "", "", int64(0), "", (*time.Time)(nil), "", false, []byte(nil)},
		}},
	}
	store := newStore(database, "workflow")
	query, err := workflow.NewHistoryQuery(workflow.HistoryQuerySpec{InstanceID: "instance-1", Limit: 1})
	if err != nil {
		t.Fatalf("construct query: %v", err)
	}
	page, err := store.History(context.Background(), query)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if len(page.Events()) != 1 || page.Events()[0].Sequence() != 1 ||
		page.NextAfterSequence() != 1 || !page.HasMore() {
		t.Fatal("history page was not stable or bounded")
	}
	if database.lastQueryLimit != int32(2) {
		t.Fatalf("database page limit = %d", database.lastQueryLimit)
	}
}

func TestHistoryDoesNotClaimMoreForAnExactlyFullPage(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	database := &fakeDatabase{
		row: &fakeRow{values: []any{true}},
		queryRows: &fakeRows{rows: [][]any{
			{int64(1), int16(workflow.EventInstancePaused), now, "", "", "", "", "", int64(0), "", (*time.Time)(nil), "", false, []byte(nil)},
			{int64(2), int16(workflow.EventInstanceResumed), now.Add(time.Second), "", "", "", "", "", int64(0), "", (*time.Time)(nil), "", false, []byte(nil)},
		}},
	}
	page, err := newStore(database, "workflow").History(context.Background(), mustHistoryQuery(t, 0, 2))
	if err != nil {
		t.Fatalf("read exactly full history: %v", err)
	}
	if page.HasMore() || len(page.Events()) != 2 {
		t.Fatal("exactly full page claimed an unobserved successor")
	}
}

func TestListInstancesReturnsStableValidatedCreationPages(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	fingerprint := strings.Repeat("a", 64)
	database := &fakeDatabase{queryRows: &fakeRows{rows: [][]any{
		{"instance-1", "orders", "1", fingerprint, int64(1), now, now, (*time.Time)(nil)},
		{"instance-2", "orders", "1", fingerprint, int64(2), now, now.Add(time.Second), (*time.Time)(nil)},
		{"instance-3", "orders", "1", fingerprint, int64(3), now.Add(time.Second), now.Add(time.Second), (*time.Time)(nil)},
	}}}
	query, err := workflow.NewInstanceListQuery(workflow.InstanceListQuerySpec{
		Selection: workflow.ListActiveInstances, Limit: 2,
	})
	if err != nil {
		t.Fatalf("construct list query: %v", err)
	}
	page, err := newStore(database, "workflow").ListInstances(context.Background(), query)
	if err != nil {
		t.Fatalf("list instances: %v", err)
	}
	if len(page.Items()) != 2 || page.Items()[1].InstanceID() != "instance-2" ||
		!page.HasMore() || page.NextCursor().InstanceID() != "instance-2" ||
		database.lastQueryLimit != 3 || !strings.Contains(database.lastQuery, "archived_at IS NULL") {
		t.Fatalf("instance page = %#v query %q limit %d", page, database.lastQuery, database.lastQueryLimit)
	}
}

func TestReconcileTransitionDistinguishesMissingExactAndConflict(t *testing.T) {
	t.Parallel()

	fingerprint := strings.Repeat("a", 64)
	reconciliation, err := workflow.NewTransitionReconciliation(workflow.TransitionReconciliationSpec{
		TransitionID: "transition-1", Fingerprint: fingerprint,
	})
	if err != nil {
		t.Fatalf("construct reconciliation: %v", err)
	}
	for _, test := range []struct {
		name string
		row  rowScanner
		want workflow.TransitionReconciliationOutcome
	}{
		{name: "missing", row: &fakeRow{err: pgx.ErrNoRows}, want: workflow.TransitionMissing},
		{name: "exact", row: &fakeRow{values: []any{fingerprint}}, want: workflow.TransitionCommitted},
		{name: "conflict", row: &fakeRow{values: []any{strings.Repeat("b", 64)}}, want: workflow.TransitionConflicting},
	} {
		t.Run(test.name, func(t *testing.T) {
			outcome, err := newStore(&fakeDatabase{row: test.row}, "workflow").ReconcileTransition(
				context.Background(), reconciliation,
			)
			if err != nil || outcome != test.want {
				t.Fatalf("reconciliation = %d, %v", outcome, err)
			}
		})
	}
}

func TestClaimReturnsStableFencedDueWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 13, 0, 0, 0, time.UTC)
	rows := &fakeRows{rows: [][]any{
		{"work-1", int16(workflow.WorkActivity), "instance-1", int64(2), now.Add(-time.Second), now.Add(time.Hour), []byte("one"), "tenant-1", "correlation-1", int64(2), int64(4), now.Add(time.Minute)},
		{"work-2", int16(workflow.WorkTimer), "instance-2", int64(3), now, now.Add(time.Hour), []byte(nil), "", "", int64(1), int64(1), now.Add(time.Minute)},
	}}
	database := &fakeDatabase{queryRows: rows}
	request, err := workflow.NewWorkClaimRequest(workflow.WorkClaimRequestSpec{
		Owner: "worker-1", Now: now, LeaseDuration: time.Minute, Limit: 2,
	})
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}
	leases, err := newStore(database, "workflow").Claim(context.Background(), request)
	if err != nil {
		t.Fatalf("claim work: %v", err)
	}
	if len(leases) != 2 || leases[0].Work().ID() != "work-1" || leases[0].Token() != 4 ||
		leases[1].Work().Kind() != workflow.WorkTimer || database.lastQueryLimit != 2 {
		t.Fatalf("claimed leases = %#v", leases)
	}
	if !strings.Contains(database.lastQuery, "FOR UPDATE OF work SKIP LOCKED") ||
		!strings.Contains(database.lastQuery, "lease_expires_at <= $1") ||
		!strings.Contains(database.lastQuery, "PARTITION BY tenant_id") {
		t.Fatal("claim does not atomically recover expired leases")
	}
}

func TestRenewCompleteAndFailRejectStaleFences(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 13, 0, 0, 0, time.UTC)
	renewal, err := workflow.NewWorkLeaseRenewal(workflow.WorkLeaseRenewalSpec{
		WorkID: "work-1", Owner: "worker-1", Token: 3, Now: now, ExtendBy: time.Minute,
	})
	if err != nil {
		t.Fatalf("construct renewal: %v", err)
	}
	row := &fakeRow{values: []any{
		"work-1", int16(workflow.WorkActivity), "instance-1", int64(2), now.Add(-time.Second),
		now.Add(time.Hour), []byte(nil), "", "", int64(2), int64(3), now.Add(time.Minute),
	}}
	renewed, err := newStore(&fakeDatabase{row: row}, "workflow").Renew(context.Background(), renewal)
	if err != nil || renewed.Token() != 3 || renewed.ExpiresAt() != now.Add(time.Minute) {
		t.Fatalf("renewed lease = %#v, %v", renewed, err)
	}
	if _, err := newStore(&fakeDatabase{row: &fakeRow{err: pgx.ErrNoRows}}, "workflow").Renew(context.Background(), renewal); !errors.Is(err, workflow.ErrStaleWorkLease) {
		t.Fatalf("stale renewal = %v", err)
	}

	completion, err := workflow.NewWorkCompletion(workflow.WorkCompletionSpec{
		WorkID: "work-1", Owner: "worker-1", Token: 3, CompletedAt: now,
	})
	if err != nil {
		t.Fatalf("construct completion: %v", err)
	}
	completedTx := &fakeTransaction{execResults: []commandResult{fakeCommandResult(1)}}
	if err := newStore(&fakeDatabase{tx: completedTx}, "workflow").Complete(context.Background(), completion); err != nil || !completedTx.committed {
		t.Fatalf("complete work = %v commit %t", err, completedTx.committed)
	}
	staleTx := &fakeTransaction{execResults: []commandResult{fakeCommandResult(0)}}
	if err := newStore(&fakeDatabase{tx: staleTx}, "workflow").Complete(context.Background(), completion); !errors.Is(err, workflow.ErrStaleWorkLease) {
		t.Fatalf("stale completion = %v", err)
	}

	retry, err := workflow.NewWorkFailure(workflow.WorkFailureSpec{
		WorkID: "work-1", Owner: "worker-1", Token: 3, FailedAt: now,
		Code: "temporary", Disposition: workflow.WorkRetry, RetryAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("construct retry: %v", err)
	}
	retryTx := &fakeTransaction{execResults: []commandResult{fakeCommandResult(1)}}
	if err := newStore(&fakeDatabase{tx: retryTx}, "workflow").Fail(context.Background(), retry); err != nil ||
		!strings.Contains(retryTx.execQueries[0], "state = 1") {
		t.Fatalf("retry work = %v", err)
	}
	dead, err := workflow.NewWorkFailure(workflow.WorkFailureSpec{
		WorkID: "work-1", Owner: "worker-1", Token: 3, FailedAt: now,
		Code: "poison", Disposition: workflow.WorkDeadLetter,
	})
	if err != nil {
		t.Fatalf("construct dead letter: %v", err)
	}
	deadTx := &fakeTransaction{execResults: []commandResult{fakeCommandResult(1)}}
	if err := newStore(&fakeDatabase{tx: deadTx}, "workflow").Fail(context.Background(), dead); err != nil ||
		!strings.Contains(deadTx.execQueries[0], "state = 4") {
		t.Fatalf("dead-letter work = %v", err)
	}
}

func mustCreateTransition(t *testing.T) workflow.Transition {
	t.Helper()
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	definition, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "orders", Version: "1", Mode: workflow.Orchestration,
		Steps: []workflow.StepSpec{{
			Name: "execute", Kind: workflow.StepActivity, Target: "orders.execute",
			Timeout: time.Minute, InputLimit: 8, ResultLimit: 8,
			Retry: workflow.RetryPolicy{MaxAttempts: 2, InitialDelay: time.Second, MaxDelay: time.Second},
		}},
	})
	if err != nil {
		t.Fatalf("construct definition: %v", err)
	}
	start, err := workflow.NewHistoryEvent(workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted,
		OccurredAt: now, Definition: definition.Reference(),
	})
	if err != nil {
		t.Fatalf("construct start: %v", err)
	}
	scheduled, err := workflow.NewHistoryEvent(workflow.HistoryEventSpec{
		Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventActivityScheduled,
		OccurredAt: now.Add(time.Second), StepName: "execute", Data: []byte("input"),
	})
	if err != nil {
		t.Fatalf("construct schedule: %v", err)
	}
	work, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
		ID: "work-1", Kind: workflow.WorkActivity, InstanceID: "instance-1", Sequence: 2,
		AvailableAt: now.Add(time.Second), Deadline: now.Add(time.Minute), Payload: []byte("input"),
	})
	if err != nil {
		t.Fatalf("construct work: %v", err)
	}
	transition, err := workflow.NewTransition(workflow.TransitionSpec{
		ID: "transition-1", InstanceID: "instance-1", Definition: definition.Reference(),
		Events: []workflow.HistoryEvent{start, scheduled}, Work: []workflow.PendingWork{work},
	})
	if err != nil {
		t.Fatalf("construct transition: %v", err)
	}
	return transition
}

func mustExistingTransition(t *testing.T) workflow.Transition {
	t.Helper()
	created := mustCreateTransition(t)
	now := time.Date(2026, 8, 9, 12, 1, 0, 0, time.UTC)
	event, err := workflow.NewHistoryEvent(workflow.HistoryEventSpec{
		Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventActivityScheduled,
		OccurredAt: now, StepName: "execute", Data: []byte("input"),
	})
	if err != nil {
		t.Fatalf("construct event: %v", err)
	}
	work, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
		ID: "work-2", Kind: workflow.WorkActivity, InstanceID: "instance-1", Sequence: 2,
		AvailableAt: now, Deadline: now.Add(time.Minute), Payload: []byte("input"),
	})
	if err != nil {
		t.Fatalf("construct work: %v", err)
	}
	transition, err := workflow.NewTransition(workflow.TransitionSpec{
		ID: "transition-2", InstanceID: "instance-1", ExpectedSequence: 1,
		Definition: created.Definition(), Events: []workflow.HistoryEvent{event},
		Work: []workflow.PendingWork{work},
	})
	if err != nil {
		t.Fatalf("construct existing transition: %v", err)
	}
	return transition
}

type fakeDatabase struct {
	tx             *fakeTransaction
	beginErr       error
	row            rowScanner
	queryRows      rowSet
	queryErr       error
	lastQueryLimit int32
	lastQuery      string
}

func (database *fakeDatabase) Begin(context.Context) (transaction, error) {
	if database.beginErr != nil {
		return nil, database.beginErr
	}
	return database.tx, nil
}

func (database *fakeDatabase) Query(_ context.Context, query string, arguments ...any) (rowSet, error) {
	if database.queryErr != nil {
		return nil, database.queryErr
	}
	database.lastQuery = query
	for _, argument := range arguments {
		if limit, ok := argument.(int32); ok {
			database.lastQueryLimit = limit
		}
	}
	return database.queryRows, nil
}

func (database *fakeDatabase) QueryRow(context.Context, string, ...any) rowScanner {
	return database.row
}

type fakeTransaction struct {
	rows             []rowScanner
	execResults      []commandResult
	execErrors       []error
	rowQueries       []string
	execQueries      []string
	execArguments    [][]any
	committed        bool
	rolledBack       bool
	commitErr        error
	rollbackDeadline time.Time
}

func (tx *fakeTransaction) QueryRow(_ context.Context, query string, _ ...any) rowScanner {
	tx.rowQueries = append(tx.rowQueries, query)
	row := tx.rows[0]
	tx.rows = tx.rows[1:]
	return row
}

func (tx *fakeTransaction) Exec(_ context.Context, query string, arguments ...any) (commandResult, error) {
	tx.execQueries = append(tx.execQueries, query)
	tx.execArguments = append(tx.execArguments, append([]any(nil), arguments...))
	if len(tx.execErrors) > 0 {
		err := tx.execErrors[0]
		tx.execErrors = tx.execErrors[1:]
		if err != nil {
			return nil, err
		}
	}
	result := tx.execResults[0]
	tx.execResults = tx.execResults[1:]
	return result, nil
}

func (tx *fakeTransaction) Commit(context.Context) error {
	tx.committed = true
	return tx.commitErr
}

func (tx *fakeTransaction) Rollback(ctx context.Context) error {
	tx.rolledBack = true
	tx.rollbackDeadline, _ = ctx.Deadline()
	return nil
}

type fakeCommandResult int64

func (result fakeCommandResult) RowsAffected() int64 { return int64(result) }

type fakeRow struct {
	values []any
	err    error
}

func (row *fakeRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	for index, value := range row.values {
		switch destination := destinations[index].(type) {
		case *string:
			*destination = value.(string)
		case *int64:
			*destination = value.(int64)
		case *int16:
			*destination = value.(int16)
		case *bool:
			*destination = value.(bool)
		case *time.Time:
			*destination = value.(time.Time)
		case **time.Time:
			*destination = value.(*time.Time)
		case *[]byte:
			*destination = append([]byte(nil), value.([]byte)...)
		default:
			panic("unsupported fake row destination")
		}
	}
	return nil
}

type fakeRows struct {
	rows    [][]any
	index   int
	scanErr error
	err     error
}

func (rows *fakeRows) Next() bool {
	if rows.index >= len(rows.rows) {
		return false
	}
	rows.index++
	return true
}

func (rows *fakeRows) Scan(destinations ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	return (&fakeRow{values: rows.rows[rows.index-1]}).Scan(destinations...)
}

func (rows *fakeRows) Err() error { return rows.err }
func (*fakeRows) Close()          {}
