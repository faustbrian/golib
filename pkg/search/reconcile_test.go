package search_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/faustbrian/golib/pkg/search"
)

func TestReconcilerDetectsAndRepairsMissingStaleAndOrphanedDerivedState(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	missing, _ := search.NewDocument("tenant-a", "events", "a", 2, json.RawMessage(`{"value":"a"}`), limits)
	stale, _ := search.NewDocument("tenant-a", "events", "b", 5, json.RawMessage(`{"value":"new"}`), limits)
	consistent, _ := search.NewDocument("tenant-a", "events", "c", 1, json.RawMessage(`{"value":"same"}`), limits)
	source := &reconciliationReader{records: []search.ReconciliationRecord{
		search.SourceRecord(missing), search.SourceRecord(stale), search.SourceRecord(consistent),
	}}
	indexed := &reconciliationReader{records: []search.ReconciliationRecord{
		search.IndexRecord("b", 3, search.SourceDigest(stale.Source)),
		search.IndexRecord("c", 1, search.SourceDigest(consistent.Source)),
		search.IndexRecord("d", 8, "orphan-digest"),
	}}
	repair := &repairIndexer{}
	reconciler, err := search.NewReconciler(source, indexed, repair, limits)
	if err != nil {
		t.Fatal(err)
	}

	report, err := reconciler.Run(t.Context(), search.ReconciliationRequest{Tenant: "tenant-a", Index: "events", PageSize: 2, MaxRecords: 10, Repair: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.SourceRecords != 3 || report.IndexRecords != 3 || len(report.Drift) != 3 || report.Repaired != 3 || !report.Complete {
		t.Fatalf("report = %#v", report)
	}
	if len(repair.requests) != 1 || len(repair.requests[0].Operations) != 3 || repair.requests[0].Operations[2].Action != search.ActionDelete || repair.requests[0].Operations[2].Version != 9 {
		t.Fatalf("repair requests = %#v", repair.requests)
	}
}

func TestReconcilerReportsSameVersionDivergenceWithoutUnsafeOverwrite(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	document, _ := search.NewDocument("tenant-a", "events", "a", 2, json.RawMessage(`{"value":"source"}`), limits)
	source := &reconciliationReader{records: []search.ReconciliationRecord{search.SourceRecord(document)}}
	indexed := &reconciliationReader{records: []search.ReconciliationRecord{search.IndexRecord("a", 2, "different")}}
	repair := &repairIndexer{}
	reconciler, _ := search.NewReconciler(source, indexed, repair, limits)

	report, err := reconciler.Run(t.Context(), search.ReconciliationRequest{Tenant: "tenant-a", Index: "events", PageSize: 1, MaxRecords: 2, Repair: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Drift) != 1 || report.Drift[0].Kind != search.DriftDivergent || report.Repaired != 0 || len(repair.requests) != 0 {
		t.Fatalf("report/repairs = %#v/%#v", report, repair.requests)
	}
}

func TestReconcilerRejectsReaderPagesLargerThanRequested(t *testing.T) {
	t.Parallel()
	limits := search.DefaultLimits()
	page := search.ReconciliationPage{Done: true, Records: []search.ReconciliationRecord{
		search.IndexRecord("a", 1, "digest-a"),
		search.IndexRecord("b", 1, "digest-b"),
	}}
	reconciler, err := search.NewReconciler(oversizedReader{page: search.ReconciliationPage{Done: true}}, oversizedReader{page: page}, &repairIndexer{}, limits)
	if err != nil {
		t.Fatal(err)
	}
	_, err = reconciler.Run(t.Context(), search.ReconciliationRequest{Tenant: "tenant", Index: "events", PageSize: 1, MaxRecords: 2})
	if !errors.Is(err, search.ErrMalformedReconciliation) {
		t.Fatalf("Run() error = %v, want ErrMalformedReconciliation", err)
	}
}

type oversizedReader struct{ page search.ReconciliationPage }

func (reader oversizedReader) Read(context.Context, string, string, string, int) (search.ReconciliationPage, error) {
	return reader.page, nil
}

type reconciliationReader struct{ records []search.ReconciliationRecord }

func (r *reconciliationReader) Read(_ context.Context, _ string, _ string, cursor string, limit int) (search.ReconciliationPage, error) {
	start := 0
	if cursor != "" {
		_, _ = fmt.Sscanf(cursor, "%d", &start)
	}
	end := min(start+limit, len(r.records))
	return search.ReconciliationPage{Records: append([]search.ReconciliationRecord(nil), r.records[start:end]...), Cursor: fmt.Sprint(end), Done: end == len(r.records)}, nil
}

type repairIndexer struct{ requests []search.BulkRequest }

func (*repairIndexer) Write(context.Context, search.WriteOperation, search.RefreshPolicy) (search.ItemOutcome, error) {
	panic("unexpected Write")
}
func (r *repairIndexer) Bulk(_ context.Context, request search.BulkRequest) (search.BulkResult, error) {
	r.requests = append(r.requests, request)
	items := make([]search.ItemOutcome, len(request.Operations))
	for index, operation := range request.Operations {
		items[index] = search.ItemOutcome{Position: index, ID: operation.ID, Action: operation.Action, State: search.OutcomeApplied, Version: operation.Version}
	}
	return search.NewBulkResult(items)
}
