package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestListInstancesSupportsArchiveSelectionsAndStableCursor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 19, 0, 0, 0, time.UTC)
	fingerprint := strings.Repeat("a", 64)
	archivedAt := now.Add(time.Second)
	archivedQuery := mustInstanceListQuery(t, workflow.ListArchivedInstances, workflow.InstanceListCursor{}, 1)
	archivedDatabase := &fakeDatabase{queryRows: &fakeRows{rows: [][]any{
		{"instance-1", "orders", "1", fingerprint, int64(1), now, now, &archivedAt},
	}}}
	archivedPage, err := newStore(archivedDatabase, "workflow").ListInstances(context.Background(), archivedQuery)
	if err != nil || len(archivedPage.Items()) != 1 || archivedPage.Items()[0].ArchivedAt() != archivedAt ||
		archivedPage.HasMore() ||
		!strings.Contains(archivedDatabase.lastQuery, "archived_at IS NOT NULL") {
		t.Fatalf("archived instances = %#v, %v query %q", archivedPage, err, archivedDatabase.lastQuery)
	}
	next := mustInstanceListQuery(t, workflow.ListAllInstances, archivedPage.NextCursor(), 1)
	allDatabase := &fakeDatabase{queryRows: &fakeRows{}}
	allPage, err := newStore(allDatabase, "workflow").ListInstances(context.Background(), next)
	if err != nil || len(allPage.Items()) != 0 ||
		strings.Contains(allDatabase.lastQuery, "archived_at IS NULL") ||
		strings.Contains(allDatabase.lastQuery, "archived_at IS NOT NULL") {
		t.Fatalf("all instances = %#v, %v query %q", allPage, err, allDatabase.lastQuery)
	}
}

func TestListInstancesRejectsFailuresAndCorruptRows(t *testing.T) {
	t.Parallel()

	query := mustInstanceListQuery(t, workflow.ListActiveInstances, workflow.InstanceListCursor{}, 1)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	failure := errors.New("database failure")
	now := time.Date(2026, 8, 9, 19, 0, 0, 0, time.UTC)
	fingerprint := strings.Repeat("a", 64)
	archivedAt := now.Add(time.Second)
	tests := []struct {
		name  string
		store *Store
		ctx   context.Context
		query workflow.InstanceListQuery
		want  error
	}{
		{name: "nil store", ctx: context.Background(), query: query, want: workflow.ErrInvalidStoreRequest},
		{name: "nil database", store: &Store{}, ctx: context.Background(), query: query, want: workflow.ErrInvalidStoreRequest},
		{name: "nil context", store: newStore(&fakeDatabase{}, "workflow"), query: query, want: workflow.ErrInvalidStoreRequest},
		{name: "invalid query", store: newStore(&fakeDatabase{}, "workflow"), ctx: context.Background(), want: workflow.ErrInvalidStoreRequest},
		{name: "cancelled", store: newStore(&fakeDatabase{}, "workflow"), ctx: cancelled, query: query, want: context.Canceled},
		{name: "query", store: newStore(&fakeDatabase{queryErr: failure}, "workflow"), ctx: context.Background(), query: query, want: failure},
		{name: "scan", store: newStore(&fakeDatabase{queryRows: &fakeRows{rows: [][]any{{}}, scanErr: failure}}, "workflow"), ctx: context.Background(), query: query, want: failure},
		{name: "sequence", store: newStore(&fakeDatabase{queryRows: &fakeRows{rows: [][]any{{"instance-1", "orders", "1", fingerprint, int64(0), now, now, (*time.Time)(nil)}}}}, "workflow"), ctx: context.Background(), query: query, want: ErrCorruptStore},
		{name: "definition", store: newStore(&fakeDatabase{queryRows: &fakeRows{rows: [][]any{{"instance-1", "orders", "1", "short", int64(1), now, now, (*time.Time)(nil)}}}}, "workflow"), ctx: context.Background(), query: query, want: ErrCorruptStore},
		{name: "record", store: newStore(&fakeDatabase{queryRows: &fakeRows{rows: [][]any{{" spaces ", "orders", "1", fingerprint, int64(1), now, now, (*time.Time)(nil)}}}}, "workflow"), ctx: context.Background(), query: query, want: ErrCorruptStore},
		{name: "iterate", store: newStore(&fakeDatabase{queryRows: &fakeRows{err: failure}}, "workflow"), ctx: context.Background(), query: query, want: failure},
		{name: "selection", store: newStore(&fakeDatabase{queryRows: &fakeRows{rows: [][]any{{"instance-1", "orders", "1", fingerprint, int64(1), now, now, &archivedAt}}}}, "workflow"), ctx: context.Background(), query: query, want: ErrCorruptStore},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.store.ListInstances(test.ctx, test.query)
			if !errors.Is(err, test.want) {
				t.Fatalf("list error = %v", err)
			}
		})
	}
}

func TestReconcileTransitionRejectsInvalidAndDatabaseFailure(t *testing.T) {
	t.Parallel()

	reconciliation, err := workflow.NewTransitionReconciliation(workflow.TransitionReconciliationSpec{
		TransitionID: "transition-1", Fingerprint: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatalf("construct reconciliation: %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	failure := errors.New("database failure")
	tests := []struct {
		name  string
		store *Store
		ctx   context.Context
		value workflow.TransitionReconciliation
		want  error
	}{
		{name: "nil store", ctx: context.Background(), value: reconciliation, want: workflow.ErrInvalidStoreRequest},
		{name: "nil database", store: &Store{}, ctx: context.Background(), value: reconciliation, want: workflow.ErrInvalidStoreRequest},
		{name: "nil context", store: newStore(&fakeDatabase{}, "workflow"), value: reconciliation, want: workflow.ErrInvalidStoreRequest},
		{name: "invalid value", store: newStore(&fakeDatabase{}, "workflow"), ctx: context.Background(), want: workflow.ErrInvalidStoreRequest},
		{name: "cancelled", store: newStore(&fakeDatabase{}, "workflow"), ctx: cancelled, value: reconciliation, want: context.Canceled},
		{name: "database", store: newStore(&fakeDatabase{row: &fakeRow{err: failure}}, "workflow"), ctx: context.Background(), value: reconciliation, want: failure},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outcome, err := test.store.ReconcileTransition(test.ctx, test.value)
			if outcome != 0 || !errors.Is(err, test.want) {
				t.Fatalf("reconciliation = %d, %v", outcome, err)
			}
		})
	}
}

func mustInstanceListQuery(
	t *testing.T,
	selection workflow.InstanceListSelection,
	after workflow.InstanceListCursor,
	limit uint32,
) workflow.InstanceListQuery {
	t.Helper()
	query, err := workflow.NewInstanceListQuery(workflow.InstanceListQuerySpec{
		Selection: selection, After: after, Limit: limit,
	})
	if err != nil {
		t.Fatalf("construct instance list query: %v", err)
	}
	return query
}
