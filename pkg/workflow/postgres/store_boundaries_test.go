package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
	"github.com/jackc/pgx/v5"
)

func TestSchemaMigrationUsesDefaultSchema(t *testing.T) {
	t.Parallel()

	migration := SchemaMigration()
	if migration.Version != 1 || !strings.Contains(migration.Up, `"workflow"."workflow_instances"`) {
		t.Fatalf("default migration = %#v", migration)
	}
}

func TestStoreRejectsSequencesOutsidePostgreSQLBigint(t *testing.T) {
	t.Parallel()

	transition := mustLargeSequenceTransition(t)
	database := &fakeDatabase{}
	err := newStore(database, "workflow").Commit(context.Background(), transition)
	if !errors.Is(err, workflow.ErrInvalidStoreRequest) || workflow.StoreCommitOutcomeOf(err) != workflow.StoreCommitNotCommitted {
		t.Fatalf("large transition sequence = %v", err)
	}
	query := mustHistoryQuery(t, uint64(^uint64(0)>>1)+1, 1)
	if _, err := newStore(database, "workflow").History(context.Background(), query); !errors.Is(err, workflow.ErrInvalidStoreRequest) {
		t.Fatalf("large history cursor = %v", err)
	}
	maximum := uint64(^uint64(0) >> 1)
	if !transitionSequenceFits(mustSequenceTransition(t, maximum)) {
		t.Fatal("exact PostgreSQL sequence maximum was rejected")
	}
	maximumQuery := mustHistoryQuery(t, maximum, 1)
	missing := newStore(&fakeDatabase{row: &fakeRow{values: []any{false}}}, "workflow")
	if _, err := missing.History(context.Background(), maximumQuery); !errors.Is(err, workflow.ErrStoreNotFound) {
		t.Fatalf("exact maximum history cursor = %v", err)
	}
}

func TestCommitHandlesConcurrentTransitionDecisions(t *testing.T) {
	t.Parallel()

	create := mustCreateTransition(t)
	existing := mustExistingTransition(t)
	lookupFailure := errors.New("lookup failed")
	tests := []struct {
		name string
		plan workflow.Transition
		tx   *fakeTransaction
		want error
	}{
		{
			name: "create became exact", plan: create,
			tx: &fakeTransaction{rows: []rowScanner{
				&fakeRow{err: pgx.ErrNoRows}, &fakeRow{values: []any{create.Fingerprint()}},
			}, execResults: []commandResult{fakeCommandResult(0)}},
		},
		{
			name: "create conflicted", plan: create,
			tx: &fakeTransaction{rows: []rowScanner{
				&fakeRow{err: pgx.ErrNoRows}, &fakeRow{err: pgx.ErrNoRows},
			}, execResults: []commandResult{fakeCommandResult(0)}}, want: workflow.ErrStoreConflict,
		},
		{
			name: "existing sequence became exact", plan: existing,
			tx: &fakeTransaction{rows: []rowScanner{
				&fakeRow{err: pgx.ErrNoRows},
				&fakeRow{values: []any{int64(2), existing.Definition().Name(), existing.Definition().Version(), existing.Definition().Fingerprint()}},
				&fakeRow{values: []any{existing.Fingerprint()}},
			}},
		},
		{
			name: "existing definition conflicted", plan: existing,
			tx: &fakeTransaction{rows: []rowScanner{
				&fakeRow{err: pgx.ErrNoRows},
				&fakeRow{values: []any{int64(1), "other", existing.Definition().Version(), existing.Definition().Fingerprint()}},
				&fakeRow{err: pgx.ErrNoRows},
			}}, want: workflow.ErrStoreConflict,
		},
		{
			name: "insert became exact", plan: create,
			tx: &fakeTransaction{rows: []rowScanner{
				&fakeRow{err: pgx.ErrNoRows}, &fakeRow{err: pgx.ErrNoRows},
				&fakeRow{values: []any{create.Fingerprint()}},
			}, execResults: []commandResult{fakeCommandResult(1)}},
		},
		{
			name: "insert lookup failed", plan: create,
			tx: &fakeTransaction{rows: []rowScanner{
				&fakeRow{err: pgx.ErrNoRows}, &fakeRow{err: pgx.ErrNoRows},
				&fakeRow{err: lookupFailure},
			}, execResults: []commandResult{fakeCommandResult(1)}}, want: lookupFailure,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := newStore(&fakeDatabase{tx: test.tx}, "workflow").Commit(context.Background(), test.plan)
			if test.want == nil {
				if err != nil {
					t.Fatalf("commit = %v", err)
				}
				return
			}
			if !errors.Is(err, test.want) || workflow.StoreCommitOutcomeOf(err) != workflow.StoreCommitNotCommitted {
				t.Fatalf("commit = %v outcome = %d", err, workflow.StoreCommitOutcomeOf(err))
			}
		})
	}
}

func TestCommitRejectsRemainingAtomicWriteFailures(t *testing.T) {
	t.Parallel()

	transition := mustCreateTransition(t)
	failure := errors.New("write failure")
	tests := []struct {
		name string
		tx   *fakeTransaction
		want error
	}{
		{
			name: "unexpected inserted fingerprint",
			tx: &fakeTransaction{rows: []rowScanner{
				&fakeRow{err: pgx.ErrNoRows}, &fakeRow{values: []any{strings.Repeat("f", 64)}},
			}, execResults: []commandResult{fakeCommandResult(1)}}, want: workflow.ErrDuplicateTransition,
		},
		{
			name: "second history",
			tx: &fakeTransaction{rows: []rowScanner{
				&fakeRow{err: pgx.ErrNoRows}, &fakeRow{values: []any{transition.Fingerprint()}},
			}, execResults: []commandResult{fakeCommandResult(1), fakeCommandResult(1)}, execErrors: []error{nil, nil, failure}}, want: failure,
		},
		{
			name: "work",
			tx: &fakeTransaction{rows: []rowScanner{
				&fakeRow{err: pgx.ErrNoRows}, &fakeRow{values: []any{transition.Fingerprint()}},
			}, execResults: []commandResult{fakeCommandResult(1), fakeCommandResult(1), fakeCommandResult(1)}, execErrors: []error{nil, nil, nil, failure}}, want: failure,
		},
		{
			name: "update",
			tx: &fakeTransaction{rows: []rowScanner{
				&fakeRow{err: pgx.ErrNoRows}, &fakeRow{values: []any{transition.Fingerprint()}},
			}, execResults: []commandResult{fakeCommandResult(1), fakeCommandResult(1), fakeCommandResult(1), fakeCommandResult(1)}, execErrors: []error{nil, nil, nil, nil, failure}}, want: failure,
		},
		{
			name: "stale update",
			tx: &fakeTransaction{rows: []rowScanner{
				&fakeRow{err: pgx.ErrNoRows}, &fakeRow{values: []any{transition.Fingerprint()}},
			}, execResults: []commandResult{
				fakeCommandResult(1), fakeCommandResult(1), fakeCommandResult(1),
				fakeCommandResult(1), fakeCommandResult(0),
			}}, want: workflow.ErrStoreConflict,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := newStore(&fakeDatabase{tx: test.tx}, "workflow").Commit(context.Background(), transition)
			if !errors.Is(err, test.want) || workflow.StoreCommitOutcomeOf(err) != workflow.StoreCommitNotCommitted {
				t.Fatalf("commit = %v outcome = %d", err, workflow.StoreCommitOutcomeOf(err))
			}
		})
	}
}

func TestCommitPersistsMigrationIdentityAndDueTimes(t *testing.T) {
	t.Parallel()

	transition := mustMigrationTransition(t)
	tx := &fakeTransaction{
		rows: []rowScanner{
			&fakeRow{err: pgx.ErrNoRows},
			&fakeRow{values: []any{int64(2), transition.Definition().Name(), transition.Definition().Version(), transition.Definition().Fingerprint()}},
			&fakeRow{values: []any{transition.Fingerprint()}},
		},
		execResults: []commandResult{fakeCommandResult(1), fakeCommandResult(1), fakeCommandResult(1)},
	}
	if err := newStore(&fakeDatabase{tx: tx}, "workflow").Commit(context.Background(), transition); err != nil {
		t.Fatalf("commit migration: %v", err)
	}
	next := transition.Events()[0].Definition()
	update := tx.execArguments[len(tx.execArguments)-1]
	if update[0] != next.Name() || update[1] != next.Version() || update[2] != next.Fingerprint() {
		t.Fatalf("migration update identity = %#v", update[:3])
	}
}

func TestHistoryRejectsInvalidRequestsAndDatabaseFailures(t *testing.T) {
	t.Parallel()

	query := mustHistoryQuery(t, 0, 1)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	failure := errors.New("database failure containing secret")
	tests := []struct {
		name  string
		store *Store
		ctx   context.Context
		query workflow.HistoryQuery
		want  error
	}{
		{name: "nil store", ctx: context.Background(), query: query, want: workflow.ErrInvalidStoreRequest},
		{name: "nil database", store: &Store{}, ctx: context.Background(), query: query, want: workflow.ErrInvalidStoreRequest},
		{name: "nil context", store: newStore(&fakeDatabase{}, "workflow"), query: query, want: workflow.ErrInvalidStoreRequest},
		{name: "invalid query", store: newStore(&fakeDatabase{}, "workflow"), ctx: context.Background(), want: workflow.ErrInvalidStoreRequest},
		{name: "canceled", store: newStore(&fakeDatabase{}, "workflow"), ctx: canceled, query: query, want: context.Canceled},
		{name: "exists query", store: newStore(&fakeDatabase{row: &fakeRow{err: failure}}, "workflow"), ctx: context.Background(), query: query, want: failure},
		{name: "missing", store: newStore(&fakeDatabase{row: &fakeRow{values: []any{false}}}, "workflow"), ctx: context.Background(), query: query, want: workflow.ErrStoreNotFound},
		{name: "history query", store: newStore(&fakeDatabase{row: &fakeRow{values: []any{true}}, queryErr: failure}, "workflow"), ctx: context.Background(), query: query, want: failure},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.store.History(test.ctx, test.query)
			if !errors.Is(err, test.want) {
				t.Fatalf("history = %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("history error exposed driver details")
			}
		})
	}
}

func TestHistoryRejectsCorruptRowsAndPagination(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	valid := []any{int64(1), int16(workflow.EventInstancePaused), now, "", "", "", "", "", int64(0), "", (*time.Time)(nil), "", false, []byte(nil)}
	failure := errors.New("row failure")
	tests := []struct {
		name  string
		rows  *fakeRows
		query workflow.HistoryQuery
		want  error
	}{
		{name: "scan", rows: &fakeRows{rows: [][]any{valid}, scanErr: failure}, query: mustHistoryQuery(t, 0, 1), want: failure},
		{name: "iterate", rows: &fakeRows{err: failure}, query: mustHistoryQuery(t, 0, 1), want: failure},
		{name: "negative sequence", rows: &fakeRows{rows: [][]any{replaceHistoryValue(valid, 0, int64(-1))}}, query: mustHistoryQuery(t, 0, 1), want: ErrCorruptStore},
		{name: "negative attempt", rows: &fakeRows{rows: [][]any{replaceHistoryValue(valid, 8, int64(-1))}}, query: mustHistoryQuery(t, 0, 1), want: ErrCorruptStore},
		{name: "oversized attempt", rows: &fakeRows{rows: [][]any{replaceHistoryValue(valid, 8, int64(1)<<40)}}, query: mustHistoryQuery(t, 0, 1), want: ErrCorruptStore},
		{name: "invalid kind", rows: &fakeRows{rows: [][]any{replaceHistoryValue(valid, 1, int16(256))}}, query: mustHistoryQuery(t, 0, 1), want: ErrCorruptStore},
		{name: "invalid definition", rows: &fakeRows{rows: [][]any{replaceHistoryValues(valid, map[int]any{3: "orders", 4: "1", 5: "bad"})}}, query: mustHistoryQuery(t, 0, 1), want: ErrCorruptStore},
		{name: "invalid event", rows: &fakeRows{rows: [][]any{replaceHistoryValue(valid, 7, "unexpected-step")}}, query: mustHistoryQuery(t, 0, 1), want: ErrCorruptStore},
		{name: "gap", rows: &fakeRows{rows: [][]any{replaceHistoryValue(valid, 0, int64(2))}}, query: mustHistoryQuery(t, 0, 1), want: ErrCorruptStore},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newStore(&fakeDatabase{row: &fakeRow{values: []any{true}}, queryRows: test.rows}, "workflow")
			_, err := store.History(context.Background(), test.query)
			if !errors.Is(err, test.want) {
				t.Fatalf("history = %v", err)
			}
		})
	}
}

func TestScanHistoryEventRejectsExactNumericAndPartialDefinitionBoundaries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	valid := []any{int64(1), int16(workflow.EventInstancePaused), now, "", "", "", "", "", int64(0), "", (*time.Time)(nil), "", false, []byte(nil)}
	tests := [][]any{
		replaceHistoryValue(valid, 0, int64(0)),
		replaceHistoryValue(valid, 0, int64(-1)),
		replaceHistoryValue(valid, 8, int64(^uint32(0))+1),
		replaceHistoryValue(valid, 1, int16(0)),
		replaceHistoryValue(valid, 1, int16(workflow.EventActivityRetryScheduled+1)),
		replaceHistoryValue(valid, 3, "orders"),
		replaceHistoryValue(valid, 4, "1"),
		replaceHistoryValue(valid, 5, strings.Repeat("a", 64)),
	}
	for index, values := range tests {
		if _, err := scanHistoryEvent(&fakeRow{values: values}, "instance-1"); !errors.Is(err, ErrCorruptStore) {
			t.Fatalf("case %d error = %v", index, err)
		}
	}
}

func TestScanHistoryEventAcceptsExactPersistedNumericBoundaries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	maximumAttempt := []any{
		int64(1), int16(workflow.EventActivityAttemptFailed), now,
		"", "", "", "", "execute", int64(^uint32(0)), "", (*time.Time)(nil),
		"failed", false, []byte(nil),
	}
	if _, err := scanHistoryEvent(&fakeRow{values: maximumAttempt}, "instance-1"); err != nil {
		t.Fatalf("maximum attempt: %v", err)
	}
	due := now.Add(time.Second)
	maximumKind := []any{
		int64(1), int16(workflow.EventActivityRetryScheduled), now,
		"", "", "", "", "execute", int64(1), "", &due, "", false, []byte(nil),
	}
	if _, err := scanHistoryEvent(&fakeRow{values: maximumKind}, "instance-1"); err != nil {
		t.Fatalf("maximum event kind: %v", err)
	}
}

func TestHistoryDecodesDefinitionAndDueTime(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	due := now.Add(time.Minute)
	definition := mustCreateTransition(t).Definition()
	rows := &fakeRows{rows: [][]any{
		{int64(1), int16(workflow.EventInstanceStarted), now,
			definition.Name(), definition.Version(), definition.Fingerprint(), "", "", int64(0), "", (*time.Time)(nil), "", false, []byte("input")},
		{int64(2), int16(workflow.EventActivityAttemptStarted), now.Add(time.Second),
			"", "", "", "", "execute", int64(1), "activity-key", &due, "", false, []byte(nil)},
	}}
	query := mustHistoryQuery(t, 0, 2)
	page, err := newStore(&fakeDatabase{row: &fakeRow{values: []any{true}}, queryRows: rows}, "workflow").History(context.Background(), query)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(page.Events()) != 2 || !page.Events()[1].DueAt().Equal(due) {
		t.Fatal("definition or due time was not decoded")
	}
}

func TestOperationErrorPreservesCauseWithoutExposingIt(t *testing.T) {
	t.Parallel()

	cause := errors.New("password=secret")
	err := newOperationError("query history", cause)
	if !errors.Is(err, cause) || strings.Contains(err.Error(), "secret") || err.Error() != "workflow/postgres: query history failed" {
		t.Fatalf("operation error = %v", err)
	}
}

func mustHistoryQuery(t *testing.T, after uint64, limit uint32) workflow.HistoryQuery {
	t.Helper()
	query, err := workflow.NewHistoryQuery(workflow.HistoryQuerySpec{
		InstanceID: "instance-1", AfterSequence: after, Limit: limit,
	})
	if err != nil {
		t.Fatalf("construct history query: %v", err)
	}
	return query
}

func mustMigrationTransition(t *testing.T) workflow.Transition {
	t.Helper()
	old := mustCreateTransition(t).Definition()
	next, err := workflow.NewDefinitionReference(old.Name(), "2", strings.Repeat("a", 64))
	if err != nil {
		t.Fatalf("construct migration definition: %v", err)
	}
	now := time.Date(2026, 8, 9, 12, 2, 0, 0, time.UTC)
	event, err := workflow.NewHistoryEvent(workflow.HistoryEventSpec{
		Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventDefinitionMigrated,
		OccurredAt: now, Definition: next, Data: []byte("migrated"),
	})
	if err != nil {
		t.Fatalf("construct migration event: %v", err)
	}
	transition, err := workflow.NewTransition(workflow.TransitionSpec{
		ID: "transition-migration", InstanceID: "instance-1", ExpectedSequence: 2,
		Definition: old, Events: []workflow.HistoryEvent{event},
	})
	if err != nil {
		t.Fatalf("construct migration transition: %v", err)
	}
	return transition
}

func mustLargeSequenceTransition(t *testing.T) workflow.Transition {
	t.Helper()
	maximum := uint64(^uint64(0) >> 1)
	return mustSequenceTransition(t, maximum+1)
}

func mustSequenceTransition(t *testing.T, sequence uint64) workflow.Transition {
	t.Helper()
	definition := mustCreateTransition(t).Definition()
	event, err := workflow.NewHistoryEvent(workflow.HistoryEventSpec{
		Sequence: sequence, InstanceID: "instance-1", Kind: workflow.EventInstancePaused,
		OccurredAt: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("construct large-sequence event: %v", err)
	}
	transition, err := workflow.NewTransition(workflow.TransitionSpec{
		ID: "large-sequence", InstanceID: "instance-1", ExpectedSequence: sequence - 1,
		Definition: definition, Events: []workflow.HistoryEvent{event},
	})
	if err != nil {
		t.Fatalf("construct large-sequence transition: %v", err)
	}
	return transition
}

func replaceHistoryValue(values []any, index int, value any) []any {
	return replaceHistoryValues(values, map[int]any{index: value})
}

func replaceHistoryValues(values []any, replacements map[int]any) []any {
	cloned := append([]any(nil), values...)
	for index, value := range replacements {
		cloned[index] = value
	}
	return cloned
}
