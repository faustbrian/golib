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

func TestListDeadLettersReturnsStableUnresolvedPages(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 21, 0, 0, 0, time.UTC)
	query := mustDeadLetterQuery(t, workflow.DeadLetterCursor{}, 1)
	database := &fakeDatabase{queryRows: &fakeRows{rows: [][]any{
		deadLetterRow(now, "work-1", 1, 1),
		deadLetterRow(now, "work-2", 2, 2),
	}}}
	page, err := newStore(database, "workflow").ListDeadLetters(context.Background(), query)
	if err != nil || len(page.Items()) != 1 || !page.HasMore() ||
		page.Items()[0].Work().ID() != "work-1" || page.NextCursor().WorkID() != "work-1" ||
		database.lastQueryLimit != 2 ||
		!strings.Contains(database.lastQuery, "workflow_work_resolutions") {
		t.Fatalf("dead-letter page = %#v, %v query %q", page, err, database.lastQuery)
	}
	exactDatabase := &fakeDatabase{queryRows: &fakeRows{rows: [][]any{
		deadLetterRow(now, "work-1", 1, 1),
	}}}
	exactPage, err := newStore(exactDatabase, "workflow").ListDeadLetters(
		context.Background(), query,
	)
	if err != nil || exactPage.HasMore() || len(exactPage.Items()) != 1 {
		t.Fatalf("exact dead-letter page = %#v, %v", exactPage, err)
	}
	next := mustDeadLetterQuery(t, page.NextCursor(), 1)
	nextDatabase := &fakeDatabase{queryRows: &fakeRows{}}
	nextPage, err := newStore(nextDatabase, "workflow").ListDeadLetters(context.Background(), next)
	if err != nil || len(nextPage.Items()) != 0 {
		t.Fatalf("next dead-letter page = %#v, %v", nextPage, err)
	}
}

func TestListDeadLettersRejectsInvalidFailuresAndCorruptRows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 21, 0, 0, 0, time.UTC)
	query := mustDeadLetterQuery(t, workflow.DeadLetterCursor{}, 2)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	failure := errors.New("driver failure")
	tests := []struct {
		name  string
		store *Store
		ctx   context.Context
		query workflow.DeadLetterQuery
		want  error
	}{
		{name: "nil store", ctx: context.Background(), query: query, want: workflow.ErrInvalidStoreRequest},
		{name: "nil database", store: &Store{}, ctx: context.Background(), query: query, want: workflow.ErrInvalidStoreRequest},
		{name: "nil context", store: newStore(&fakeDatabase{}, "workflow"), query: query, want: workflow.ErrInvalidStoreRequest},
		{name: "invalid query", store: newStore(&fakeDatabase{}, "workflow"), ctx: context.Background(), want: workflow.ErrInvalidStoreRequest},
		{name: "cancelled", store: newStore(&fakeDatabase{}, "workflow"), ctx: cancelled, query: query, want: context.Canceled},
		{name: "query", store: newStore(&fakeDatabase{queryErr: failure}, "workflow"), ctx: context.Background(), query: query, want: failure},
		{name: "scan", store: newStore(&fakeDatabase{queryRows: &fakeRows{rows: [][]any{{}}, scanErr: failure}}, "workflow"), ctx: context.Background(), query: query, want: failure},
		{name: "iterate", store: newStore(&fakeDatabase{queryRows: &fakeRows{err: failure}}, "workflow"), ctx: context.Background(), query: query, want: failure},
		{name: "page order", store: newStore(&fakeDatabase{queryRows: &fakeRows{rows: [][]any{
			deadLetterRow(now, "work-2", 1, 1), deadLetterRow(now, "work-1", 1, 1),
		}}}, "workflow"), ctx: context.Background(), query: query, want: ErrCorruptStore},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.store.ListDeadLetters(test.ctx, test.query)
			if !errors.Is(err, test.want) {
				t.Fatalf("list error = %v", err)
			}
		})
	}
}

func TestScanDeadLetterRecordRejectsDriverAndDurableCorruption(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 21, 0, 0, 0, time.UTC)
	failure := errors.New("scan failure")
	if _, err := scanDeadLetterRecord(&fakeRow{err: failure}); !errors.Is(err, failure) {
		t.Fatalf("scan failure = %v", err)
	}
	valid := deadLetterRow(now, "work-1", 1, 1)
	mutations := map[string]func([]any){
		"kind below":    func(row []any) { row[1] = int16(workflow.WorkActivity - 1) },
		"kind above":    func(row []any) { row[1] = int16(workflow.WorkCompensation + 1) },
		"sequence zero": func(row []any) { row[3] = int64(0) },
		"attempt zero":  func(row []any) { row[9] = int64(0) },
		"attempt above": func(row []any) { row[9] = int64(^uint32(0)) + 1 },
		"token zero":    func(row []any) { row[10] = int64(0) },
		"work":          func(row []any) { row[0] = " spaces " },
		"resolution":    func(row []any) { row[11] = " spaces " },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			row := append([]any(nil), valid...)
			mutate(row)
			if _, err := scanDeadLetterRecord(&fakeRow{values: row}); !errors.Is(err, ErrCorruptStore) {
				t.Fatalf("corrupt row error = %v", err)
			}
		})
	}
	if record, err := scanDeadLetterRecord(&fakeRow{values: valid}); err != nil ||
		record.Work().ID() != "work-1" {
		t.Fatalf("valid dead letter = %#v, %v", record, err)
	}
	boundaries := []struct {
		kind     workflow.WorkKind
		sequence int64
		attempts uint32
	}{
		{kind: workflow.WorkActivity, sequence: 1, attempts: 1},
		{kind: workflow.WorkCompensation, sequence: 2, attempts: ^uint32(0)},
	}
	for _, boundary := range boundaries {
		row := append([]any(nil), valid...)
		row[1] = int16(boundary.kind)
		row[3] = boundary.sequence
		row[9] = int64(boundary.attempts)
		if _, err := scanDeadLetterRecord(&fakeRow{values: row}); err != nil {
			t.Fatalf("valid numeric boundary kind %d attempt %d: %v", boundary.kind, boundary.attempts, err)
		}
	}
}

func TestResolveDeadLetterClassifiesValidationAndTransactionFailures(t *testing.T) {
	t.Parallel()

	resolution := mustDeadLetterResolution(t, workflow.DeadLetterDiscard)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	failure := errors.New("database failure")
	tests := []struct {
		name       string
		store      *Store
		ctx        context.Context
		resolution workflow.DeadLetterResolution
		want       error
	}{
		{name: "nil store", ctx: context.Background(), resolution: resolution, want: workflow.ErrInvalidStoreRequest},
		{name: "nil database", store: &Store{}, ctx: context.Background(), resolution: resolution, want: workflow.ErrInvalidStoreRequest},
		{name: "nil context", store: newStore(&fakeDatabase{}, "workflow"), resolution: resolution, want: workflow.ErrInvalidStoreRequest},
		{name: "invalid resolution", store: newStore(&fakeDatabase{}, "workflow"), ctx: context.Background(), want: workflow.ErrInvalidStoreRequest},
		{name: "cancelled", store: newStore(&fakeDatabase{}, "workflow"), ctx: cancelled, resolution: resolution, want: context.Canceled},
		{name: "begin", store: newStore(&fakeDatabase{beginErr: failure}, "workflow"), ctx: context.Background(), resolution: resolution, want: failure},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.store.ResolveDeadLetter(test.ctx, test.resolution)
			if !errors.Is(err, test.want) || workflow.StoreCommitOutcomeOf(err) != workflow.StoreCommitNotCommitted {
				t.Fatalf("resolution error = %v outcome %d", err, workflow.StoreCommitOutcomeOf(err))
			}
		})
	}
}

func TestResolveDeadLetterHandlesReplayConflictAndPersistenceBoundaries(t *testing.T) {
	t.Parallel()

	retry := mustDeadLetterResolution(t, workflow.DeadLetterRetry)
	discard := mustDeadLetterResolution(t, workflow.DeadLetterDiscard)
	failure := errors.New("driver failure")
	tests := []struct {
		name       string
		resolution workflow.DeadLetterResolution
		tx         *fakeTransaction
		want       error
		unknown    bool
	}{
		{name: "exact replay", resolution: discard, tx: &fakeTransaction{rows: []rowScanner{&fakeRow{values: []any{discard.Fingerprint()}}}}},
		{name: "conflicting replay", resolution: discard, tx: &fakeTransaction{rows: []rowScanner{&fakeRow{values: []any{strings.Repeat("f", 64)}}}}, want: workflow.ErrStoreConflict},
		{name: "lookup", resolution: discard, tx: &fakeTransaction{rows: []rowScanner{&fakeRow{err: failure}}}, want: failure},
		{name: "missing fence", resolution: discard, tx: &fakeTransaction{rows: []rowScanner{&fakeRow{err: pgx.ErrNoRows}, &fakeRow{err: pgx.ErrNoRows}}}, want: workflow.ErrStaleWorkLease},
		{name: "lock", resolution: discard, tx: &fakeTransaction{rows: []rowScanner{&fakeRow{err: pgx.ErrNoRows}, &fakeRow{err: failure}}}, want: failure},
		{name: "insert", resolution: discard, tx: &fakeTransaction{rows: []rowScanner{&fakeRow{err: pgx.ErrNoRows}, &fakeRow{values: []any{"work-1"}}, &fakeRow{err: failure}}}, want: failure},
		{name: "insert fingerprint", resolution: discard, tx: &fakeTransaction{rows: []rowScanner{&fakeRow{err: pgx.ErrNoRows}, &fakeRow{values: []any{"work-1"}}, &fakeRow{values: []any{strings.Repeat("f", 64)}}}}, want: workflow.ErrStoreConflict},
		{name: "insert race exact", resolution: discard, tx: &fakeTransaction{rows: []rowScanner{&fakeRow{err: pgx.ErrNoRows}, &fakeRow{values: []any{"work-1"}}, &fakeRow{err: pgx.ErrNoRows}, &fakeRow{values: []any{discard.Fingerprint()}}}}},
		{name: "insert race lookup", resolution: discard, tx: &fakeTransaction{rows: []rowScanner{&fakeRow{err: pgx.ErrNoRows}, &fakeRow{values: []any{"work-1"}}, &fakeRow{err: pgx.ErrNoRows}, &fakeRow{err: failure}}}, want: failure},
		{name: "insert race conflict", resolution: discard, tx: &fakeTransaction{rows: []rowScanner{&fakeRow{err: pgx.ErrNoRows}, &fakeRow{values: []any{"work-1"}}, &fakeRow{err: pgx.ErrNoRows}, &fakeRow{err: pgx.ErrNoRows}}}, want: workflow.ErrStoreConflict},
		{name: "discard", resolution: discard, tx: &fakeTransaction{rows: []rowScanner{&fakeRow{err: pgx.ErrNoRows}, &fakeRow{values: []any{"work-1"}}, &fakeRow{values: []any{discard.Fingerprint()}}}}},
		{name: "retry exec", resolution: retry, tx: &fakeTransaction{rows: []rowScanner{&fakeRow{err: pgx.ErrNoRows}, &fakeRow{values: []any{"work-1"}}, &fakeRow{values: []any{retry.Fingerprint()}}}, execErrors: []error{failure}}, want: failure},
		{name: "retry stale", resolution: retry, tx: &fakeTransaction{rows: []rowScanner{&fakeRow{err: pgx.ErrNoRows}, &fakeRow{values: []any{"work-1"}}, &fakeRow{values: []any{retry.Fingerprint()}}}, execResults: []commandResult{fakeCommandResult(0)}}, want: workflow.ErrStaleWorkLease},
		{name: "retry", resolution: retry, tx: &fakeTransaction{rows: []rowScanner{&fakeRow{err: pgx.ErrNoRows}, &fakeRow{values: []any{"work-1"}}, &fakeRow{values: []any{retry.Fingerprint()}}}, execResults: []commandResult{fakeCommandResult(1)}}},
		{name: "commit unknown", resolution: discard, tx: &fakeTransaction{rows: []rowScanner{&fakeRow{err: pgx.ErrNoRows}, &fakeRow{values: []any{"work-1"}}, &fakeRow{values: []any{discard.Fingerprint()}}}, commitErr: failure}, want: failure, unknown: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := newStore(&fakeDatabase{tx: test.tx}, "workflow").ResolveDeadLetter(context.Background(), test.resolution)
			if test.want == nil {
				if err != nil {
					t.Fatalf("resolve dead letter: %v", err)
				}
				return
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("resolution error = %v", err)
			}
			wantOutcome := workflow.StoreCommitNotCommitted
			if test.unknown {
				wantOutcome = workflow.StoreCommitUnknown
			}
			if workflow.StoreCommitOutcomeOf(err) != wantOutcome {
				t.Fatalf("resolution outcome = %d", workflow.StoreCommitOutcomeOf(err))
			}
		})
	}
}

func deadLetterRow(failedAt time.Time, workID string, attempts uint32, token uint64) []any {
	return []any{
		workID, int16(workflow.WorkActivity), "instance-1", int64(2), failedAt.Add(-time.Minute),
		failedAt.Add(time.Hour), []byte("payload"), "tenant-1", "correlation-1",
		int64(attempts), int64(token), "poison", failedAt,
	}
}

func mustDeadLetterQuery(
	t *testing.T,
	after workflow.DeadLetterCursor,
	limit uint32,
) workflow.DeadLetterQuery {
	t.Helper()
	query, err := workflow.NewDeadLetterQuery(workflow.DeadLetterQuerySpec{After: after, Limit: limit})
	if err != nil {
		t.Fatalf("construct dead-letter query: %v", err)
	}
	return query
}

func mustDeadLetterResolution(
	t *testing.T,
	action workflow.DeadLetterResolutionAction,
) workflow.DeadLetterResolution {
	t.Helper()
	now := time.Date(2026, 8, 10, 21, 0, 0, 0, time.UTC)
	spec := workflow.DeadLetterResolutionSpec{
		CommandID: "command-1", WorkID: "work-1", Token: 1, Action: action,
		Actor: "operator-1", Reason: "manual-action", OccurredAt: now,
	}
	if action == workflow.DeadLetterRetry {
		spec.RetryAt = now.Add(time.Second)
		spec.Deadline = now.Add(time.Hour)
	}
	resolution, err := workflow.NewDeadLetterResolution(spec)
	if err != nil {
		t.Fatalf("construct resolution: %v", err)
	}
	return resolution
}
