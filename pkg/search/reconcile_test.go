package search_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
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
	guard := &deletionGuard{version: 9}
	reconciler, err := search.NewReconcilerWithDeletionGuard(source, indexed, repair, guard, limits)
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
	if len(guard.calls) != 1 || guard.calls[0] != (search.ReconciliationDeletion{Tenant: "tenant-a", Index: "events", ID: "d", ObservedIndexVersion: 8}) {
		t.Fatalf("deletion guard calls = %#v", guard.calls)
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

func TestSourceDigestCanonicalizesEquivalentJSONObjects(t *testing.T) {
	t.Parallel()

	first := search.SourceDigest(json.RawMessage(`{"a":1,"b":{"c":2}}`))
	second := search.SourceDigest(json.RawMessage("{\n \"b\": {\"c\": 2}, \"a\": 1\n}"))
	if first == "" || first != second {
		t.Fatalf("equivalent source digests = %q/%q", first, second)
	}
	if digest := search.SourceDigest(json.RawMessage([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'})); digest != "" {
		t.Fatalf("invalid source digest = %q, want empty", digest)
	}
}

func TestReconcilerBoundsCombinedRetainedRecordBytes(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	limits.MaxResultBytes = 32
	document, err := search.NewDocument("tenant", "events", "a", 1, json.RawMessage(`{"value":"payload larger than result budget"}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	reconciler, err := search.NewReconciler(
		&reconciliationReader{records: []search.ReconciliationRecord{search.SourceRecord(document)}},
		&reconciliationReader{}, &repairIndexer{}, limits,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.Run(t.Context(), search.ReconciliationRequest{Tenant: "tenant", Index: "events", PageSize: 1, MaxRecords: 2}); !errors.Is(err, search.ErrReconciliationLimit) {
		t.Fatalf("Run() error = %v, want ErrReconciliationLimit", err)
	}
}

func TestReconcilerRefusesOrphanDeletionWithoutDurableSourceGuard(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	repair := &repairIndexer{}
	reconciler, err := search.NewReconciler(
		&reconciliationReader{},
		&reconciliationReader{records: []search.ReconciliationRecord{
			search.IndexRecord("concurrent", 7, search.SourceDigest(json.RawMessage(`{"value":"new"}`))),
		}},
		repair,
		limits,
	)
	if err != nil {
		t.Fatal(err)
	}

	report, err := reconciler.Run(t.Context(), search.ReconciliationRequest{
		Tenant: "tenant-a", Index: "events", PageSize: 10, MaxRecords: 10, Repair: true,
	})
	if !errors.Is(err, search.ErrReconciliationDeletionGuard) || report.Complete || len(report.Drift) != 1 || report.Drift[0].Kind != search.DriftOrphaned || len(repair.requests) != 0 {
		t.Fatalf("Run() report/error/repairs = %#v/%v/%#v", report, err, repair.requests)
	}
}

func TestReconcilerRejectsUnsafeDeletionGuardOutcomes(t *testing.T) {
	t.Parallel()

	guardFailure := errors.New("source reservation failed with private-source-token")
	cancelFailure := fmt.Errorf("private-source-token: %w", context.Canceled)
	deadlineFailure := fmt.Errorf("private-source-token: %w", context.DeadlineExceeded)
	tests := map[string]struct {
		observed  uint64
		version   uint64
		guardErr  error
		calls     int
		wantIs    error
		forbidden string
	}{
		"guard failure":           {observed: 7, version: 8, guardErr: guardFailure, calls: 1, forbidden: "private-source-token"},
		"cancellation":            {observed: 7, version: 8, guardErr: cancelFailure, calls: 1, wantIs: context.Canceled, forbidden: "private-source-token"},
		"deadline":                {observed: 7, version: 8, guardErr: deadlineFailure, calls: 1, wantIs: context.DeadlineExceeded, forbidden: "private-source-token"},
		"non-increasing version":  {observed: 7, version: 7, calls: 1},
		"terminal version":        {observed: 7, version: math.MaxUint64, calls: 1},
		"exhausted index version": {observed: math.MaxUint64, version: math.MaxUint64, calls: 0},
	}
	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			limits := search.DefaultLimits()
			repair := &repairIndexer{}
			guard := &deletionGuard{version: test.version, err: test.guardErr}
			reconciler, err := search.NewReconcilerWithDeletionGuard(
				&reconciliationReader{},
				&reconciliationReader{records: []search.ReconciliationRecord{search.IndexRecord("orphan", test.observed, "digest")}},
				repair,
				guard,
				limits,
			)
			if err != nil {
				t.Fatal(err)
			}

			report, err := reconciler.Run(t.Context(), search.ReconciliationRequest{
				Tenant: "tenant-a", Index: "events", PageSize: 1, MaxRecords: 1, Repair: true,
			})
			if !errors.Is(err, search.ErrReconciliationDeletionGuard) || report.Complete || report.Repaired != 0 || len(repair.requests) != 0 {
				t.Fatalf("Run() report/error/repairs = %#v/%v/%#v", report, err, repair.requests)
			}
			if test.wantIs != nil && !errors.Is(err, test.wantIs) {
				t.Fatalf("Run() error = %v, want classification %v", err, test.wantIs)
			}
			if test.guardErr != nil && test.wantIs == nil && errors.Is(err, test.guardErr) {
				t.Fatalf("Run() retained private guard cause: %v", err)
			}
			if strings.Contains(err.Error(), test.forbidden) && test.forbidden != "" {
				t.Fatalf("Run() leaked private guard error: %v", err)
			}
			if len(guard.calls) != test.calls {
				t.Fatalf("ReserveDeletion() calls = %d, want %d", len(guard.calls), test.calls)
			}
		})
	}
}

func TestReconcilerDoesNotDispatchOtherRepairsWhenOrphanDeletionIsUnguarded(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	document, err := search.NewDocument("tenant-a", "events", "a", 1, json.RawMessage(`{"value":"missing"}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	repair := &repairIndexer{}
	reconciler, err := search.NewReconciler(
		&reconciliationReader{records: []search.ReconciliationRecord{search.SourceRecord(document)}},
		&reconciliationReader{records: []search.ReconciliationRecord{search.IndexRecord("b", 1, "digest")}},
		repair,
		limits,
	)
	if err != nil {
		t.Fatal(err)
	}

	report, err := reconciler.Run(t.Context(), search.ReconciliationRequest{
		Tenant: "tenant-a", Index: "events", PageSize: 2, MaxRecords: 2, Repair: true,
	})
	if !errors.Is(err, search.ErrReconciliationDeletionGuard) || report.Complete || len(report.Drift) != 2 || report.Repaired != 0 || len(repair.requests) != 0 {
		t.Fatalf("Run() report/error/repairs = %#v/%v/%#v", report, err, repair.requests)
	}
}

func TestReconcilerRejectsSourceDigestThatDoesNotMatchDocument(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	document, err := search.NewDocument("tenant-a", "events", "a", 1, json.RawMessage(`{"value":"trusted"}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	forged := search.SourceRecord(document)
	forged.Digest = search.SourceDigest(json.RawMessage(`{"value":"poisoned"}`))
	reconciler, err := search.NewReconciler(
		&reconciliationReader{records: []search.ReconciliationRecord{forged}},
		&reconciliationReader{records: []search.ReconciliationRecord{search.IndexRecord("a", 1, forged.Digest)}},
		&repairIndexer{},
		limits,
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := reconciler.Run(t.Context(), search.ReconciliationRequest{Tenant: "tenant-a", Index: "events", PageSize: 1, MaxRecords: 2}); !errors.Is(err, search.ErrMalformedReconciliation) {
		t.Fatalf("Run() error = %v, want ErrMalformedReconciliation", err)
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

func TestReconcilerBoundsAndValidatesReaderTraversal(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	limits.MaxIDBytes = 4
	limits.MaxQueryBytes = 4
	limits.MaxPageItems = 2
	limits.MaxPages = 2
	request := search.ReconciliationRequest{Tenant: "tenant", Index: "events", PageSize: 1, MaxRecords: 4}

	t.Run("page count", func(t *testing.T) {
		reader := &endlessReconciliationReader{}
		reconciler, err := search.NewReconciler(&reconciliationReader{}, reader, &repairIndexer{}, limits)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := reconciler.Run(t.Context(), request); !errors.Is(err, search.ErrReconciliationLimit) {
			t.Fatalf("Run() error = %v, want ErrReconciliationLimit", err)
		}
		if reader.calls != limits.MaxPages {
			t.Fatalf("Read() calls = %d, want %d", reader.calls, limits.MaxPages)
		}
	})

	tests := map[string]search.ReconciliationPage{
		"done page retains cursor": {
			Done:   true,
			Cursor: "next",
		},
		"oversized cursor": {
			Records: []search.ReconciliationRecord{search.IndexRecord("a", 1, "d")},
			Cursor:  "12345",
		},
		"oversized record ID": {
			Done:    true,
			Records: []search.ReconciliationRecord{search.IndexRecord("12345", 1, "d")},
		},
		"oversized record digest": {
			Done:    true,
			Records: []search.ReconciliationRecord{search.IndexRecord("a", 1, "12345")},
		},
		"invalid UTF-8 record ID": {
			Done:    true,
			Records: []search.ReconciliationRecord{search.IndexRecord(string([]byte{0xff}), 1, "d")},
		},
		"invalid UTF-8 record digest": {
			Done:    true,
			Records: []search.ReconciliationRecord{search.IndexRecord("a", 1, string([]byte{0xff}))},
		},
		"unexpected index document": {
			Done: true,
			Records: []search.ReconciliationRecord{{
				ID:      "a",
				Version: 1,
				Digest:  "d",
				Document: &search.Document{
					Tenant:  "tenant",
					Index:   "events",
					ID:      "a",
					Version: 1,
					Source:  json.RawMessage(`{"value":"untrusted"}`),
				},
			}},
		},
	}
	for name, page := range tests {
		name, page := name, page
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reader := &countingReconciliationReader{page: page}
			reconciler, err := search.NewReconciler(&reconciliationReader{}, reader, &repairIndexer{}, limits)
			if err != nil {
				t.Fatal(err)
			}

			if _, err := reconciler.Run(t.Context(), request); !errors.Is(err, search.ErrMalformedReconciliation) {
				t.Fatalf("Run() error = %v, want ErrMalformedReconciliation", err)
			}
			if reader.calls != 1 {
				t.Fatalf("Read() calls = %d, want 1", reader.calls)
			}
		})
	}
}

func TestReconcilerRejectsInvalidUTF8ScopeBeforeReading(t *testing.T) {
	t.Parallel()

	invalid := string([]byte{0xff})
	reader := &countingReconciliationReader{page: search.ReconciliationPage{Done: true}}
	reconciler, err := search.NewReconciler(reader, reader, &repairIndexer{}, search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []search.ReconciliationRequest{
		{Tenant: invalid, Index: "events", PageSize: 1, MaxRecords: 1},
		{Tenant: "tenant", Index: invalid, PageSize: 1, MaxRecords: 1},
	} {
		if _, err := reconciler.Run(t.Context(), request); !errors.Is(err, search.ErrInvalidReconciliation) {
			t.Fatalf("Run(%#v) error = %v, want ErrInvalidReconciliation", request, err)
		}
	}
	if reader.calls != 0 {
		t.Fatalf("reader calls = %d, want 0", reader.calls)
	}
}

func TestReconcilerBoundsIndexTraversalByRemainingTotal(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	sourceDocument, err := search.NewDocument("tenant", "events", "a", 1, json.RawMessage(`{"id":"a"}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	indexed := &endlessReconciliationReader{offset: 1}
	reconciler, err := search.NewReconciler(
		&reconciliationReader{records: []search.ReconciliationRecord{search.SourceRecord(sourceDocument)}},
		indexed,
		&repairIndexer{},
		limits,
	)
	if err != nil {
		t.Fatal(err)
	}

	request := search.ReconciliationRequest{Tenant: "tenant", Index: "events", PageSize: 1, MaxRecords: 2}
	if _, err := reconciler.Run(t.Context(), request); !errors.Is(err, search.ErrReconciliationLimit) {
		t.Fatalf("Run() error = %v, want ErrReconciliationLimit", err)
	}
	if indexed.calls != 1 {
		t.Fatalf("index Read() calls = %d, want 1", indexed.calls)
	}
}

func TestReconcilerBatchesRepairsWithinTheConfiguredItemLimit(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	limits.MaxBulkItems = 2
	records := make([]search.ReconciliationRecord, 0, 5)
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		document, err := search.NewDocument("tenant-a", "events", id, 1, json.RawMessage(`{"id":"`+id+`"}`), limits)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, search.SourceRecord(document))
	}
	repair := &repairIndexer{}
	reconciler, err := search.NewReconciler(
		&reconciliationReader{records: records},
		&reconciliationReader{},
		repair,
		limits,
	)
	if err != nil {
		t.Fatal(err)
	}

	report, err := reconciler.Run(t.Context(), search.ReconciliationRequest{Tenant: "tenant-a", Index: "events", PageSize: 5, MaxRecords: 5, Repair: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.Repaired != 5 || !report.Complete {
		t.Fatalf("report = %#v", report)
	}
	if len(repair.requests) != 3 {
		t.Fatalf("repair requests = %#v", repair.requests)
	}
	wantIDs := [][]string{{"a", "b"}, {"c", "d"}, {"e"}}
	for batch, request := range repair.requests {
		if len(request.Operations) != len(wantIDs[batch]) {
			t.Fatalf("repair batch %d operation count = %d, want %d", batch, len(request.Operations), len(wantIDs[batch]))
		}
		for position, operation := range request.Operations {
			if operation.ID != wantIDs[batch][position] {
				t.Fatalf("repair batch %d operation %d ID = %q, want %q", batch, position, operation.ID, wantIDs[batch][position])
			}
		}
	}
}

func TestReconcilerRejectsMisattributedRepairResults(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	documents := make([]search.ReconciliationRecord, 0, 2)
	for _, id := range []string{"a", "b"} {
		document, err := search.NewDocument("tenant-a", "events", id, 1, json.RawMessage(`{"id":"`+id+`"}`), limits)
		if err != nil {
			t.Fatal(err)
		}
		documents = append(documents, search.SourceRecord(document))
	}

	tests := map[string][]search.ItemOutcome{
		"missing outcome": {
			{Position: 0, ID: "a", Action: search.ActionIndex, State: search.OutcomeApplied},
		},
		"wrong ID": {
			{Position: 0, ID: "other", Action: search.ActionIndex, State: search.OutcomeApplied},
			{Position: 1, ID: "b", Action: search.ActionIndex, State: search.OutcomeApplied},
		},
		"wrong action": {
			{Position: 0, ID: "a", Action: search.ActionDelete, State: search.OutcomeApplied},
			{Position: 1, ID: "b", Action: search.ActionIndex, State: search.OutcomeApplied},
		},
		"wrong applied version": {
			{Position: 0, ID: "a", Action: search.ActionIndex, State: search.OutcomeApplied, Version: 2},
			{Position: 1, ID: "b", Action: search.ActionIndex, State: search.OutcomeApplied, Version: 1},
		},
	}
	for name, outcomes := range tests {
		name, outcomes := name, outcomes
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result, err := search.NewBulkResult(outcomes)
			if err != nil {
				t.Fatal(err)
			}
			repair := &fixedRepairIndexer{result: result}
			reconciler, err := search.NewReconciler(
				&reconciliationReader{records: documents},
				&reconciliationReader{},
				repair,
				limits,
			)
			if err != nil {
				t.Fatal(err)
			}

			report, err := reconciler.Run(t.Context(), search.ReconciliationRequest{Tenant: "tenant-a", Index: "events", PageSize: 2, MaxRecords: 2, Repair: true})
			if !errors.Is(err, search.ErrRepairPartial) {
				t.Fatalf("Run() error = %v, want ErrRepairPartial", err)
			}
			if report.Complete {
				t.Fatalf("report.Complete = true for misattributed repair: %#v", report)
			}
		})
	}
}

func TestReconcilerStopsAfterAPartialRepairBatch(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	limits.MaxBulkItems = 2
	records := make([]search.ReconciliationRecord, 0, 5)
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		document, err := search.NewDocument("tenant-a", "events", id, 1, json.RawMessage(`{"id":"`+id+`"}`), limits)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, search.SourceRecord(document))
	}
	first, err := search.NewBulkResult([]search.ItemOutcome{
		{Position: 0, ID: "a", Action: search.ActionIndex, State: search.OutcomeApplied, Version: 1},
		{Position: 1, ID: "b", Action: search.ActionIndex, State: search.OutcomeApplied, Version: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	partial, err := search.NewBulkResult([]search.ItemOutcome{
		{Position: 0, ID: "c", Action: search.ActionIndex, State: search.OutcomeApplied, Version: 1},
		{Position: 1, ID: "d", Action: search.ActionIndex, State: search.OutcomeUnknown},
	})
	if err != nil {
		t.Fatal(err)
	}
	repair := &scriptedRepairIndexer{results: []search.BulkResult{first, partial}}
	reconciler, err := search.NewReconciler(
		&reconciliationReader{records: records},
		&reconciliationReader{},
		repair,
		limits,
	)
	if err != nil {
		t.Fatal(err)
	}

	report, err := reconciler.Run(t.Context(), search.ReconciliationRequest{Tenant: "tenant-a", Index: "events", PageSize: 5, MaxRecords: 5, Repair: true})
	if !errors.Is(err, search.ErrRepairPartial) {
		t.Fatalf("Run() error = %v, want ErrRepairPartial", err)
	}
	if report.Repaired != 3 || report.Complete {
		t.Fatalf("report = %#v", report)
	}
	if len(repair.requests) != 2 {
		t.Fatalf("repair request count = %d, want 2", len(repair.requests))
	}
}

type oversizedReader struct{ page search.ReconciliationPage }

func (reader oversizedReader) Read(context.Context, string, string, string, int) (search.ReconciliationPage, error) {
	return reader.page, nil
}

type countingReconciliationReader struct {
	page  search.ReconciliationPage
	calls int
}

func (reader *countingReconciliationReader) Read(context.Context, string, string, string, int) (search.ReconciliationPage, error) {
	reader.calls++
	return reader.page, nil
}

type endlessReconciliationReader struct {
	calls  int
	offset int
}

func (reader *endlessReconciliationReader) Read(context.Context, string, string, string, int) (search.ReconciliationPage, error) {
	reader.calls++
	id := fmt.Sprint(reader.calls + reader.offset)
	return search.ReconciliationPage{Records: []search.ReconciliationRecord{search.IndexRecord(id, 1, "d")}, Cursor: id}, nil
}

type reconciliationReader struct{ records []search.ReconciliationRecord }

func (r *reconciliationReader) Read(_ context.Context, _ string, _ string, cursor string, limit int) (search.ReconciliationPage, error) {
	start := 0
	if cursor != "" {
		_, _ = fmt.Sscanf(cursor, "%d", &start)
	}
	end := min(start+limit, len(r.records))
	done := end == len(r.records)
	next := ""
	if !done {
		next = fmt.Sprint(end)
	}
	return search.ReconciliationPage{Records: append([]search.ReconciliationRecord(nil), r.records[start:end]...), Cursor: next, Done: done}, nil
}

type repairIndexer struct{ requests []search.BulkRequest }

type deletionGuard struct {
	calls   []search.ReconciliationDeletion
	version uint64
	err     error
}

func (guard *deletionGuard) ReserveDeletion(_ context.Context, deletion search.ReconciliationDeletion) (uint64, error) {
	guard.calls = append(guard.calls, deletion)
	return guard.version, guard.err
}

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

type fixedRepairIndexer struct{ result search.BulkResult }

func (*fixedRepairIndexer) Write(context.Context, search.WriteOperation, search.RefreshPolicy) (search.ItemOutcome, error) {
	panic("unexpected Write")
}
func (r *fixedRepairIndexer) Bulk(context.Context, search.BulkRequest) (search.BulkResult, error) {
	return r.result, nil
}

type scriptedRepairIndexer struct {
	requests []search.BulkRequest
	results  []search.BulkResult
}

func (*scriptedRepairIndexer) Write(context.Context, search.WriteOperation, search.RefreshPolicy) (search.ItemOutcome, error) {
	panic("unexpected Write")
}
func (r *scriptedRepairIndexer) Bulk(_ context.Context, request search.BulkRequest) (search.BulkResult, error) {
	position := len(r.requests)
	r.requests = append(r.requests, request)
	return r.results[position], nil
}
