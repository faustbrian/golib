package search

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

var errReconcileBranch = errors.New("reconcile branch")

type branchReader struct {
	pages []ReconciliationPage
	err   error
	calls int
}

func (r *branchReader) Read(context.Context, string, string, string, int) (ReconciliationPage, error) {
	if r.err != nil {
		return ReconciliationPage{}, r.err
	}
	page := r.pages[r.calls]
	r.calls++
	return page, nil
}

type branchRepair struct {
	result BulkResult
	err    error
}

func (r branchRepair) Write(context.Context, WriteOperation, RefreshPolicy) (ItemOutcome, error) {
	return ItemOutcome{}, r.err
}
func (r branchRepair) Bulk(context.Context, BulkRequest) (BulkResult, error) { return r.result, r.err }

func validBranchRecord(t *testing.T, id string, version uint64) ReconciliationRecord {
	t.Helper()
	doc, err := NewDocument("t", "i", id, version, json.RawMessage(`{"id":"`+id+`"}`), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return SourceRecord(doc)
}

func TestInternalReconcilerValidationAndReaderFailures(t *testing.T) {
	limits := DefaultLimits()
	reader := &branchReader{}
	repair := branchRepair{}
	for _, args := range []struct {
		s, i ReconciliationReader
		r    Indexer
		l    Limits
	}{{nil, reader, repair, limits}, {reader, nil, repair, limits}, {reader, reader, nil, limits}, {reader, reader, repair, Limits{}}} {
		if _, err := NewReconciler(args.s, args.i, args.r, args.l); err == nil {
			t.Fatal("invalid reconciler accepted")
		}
	}
	source := &branchReader{pages: []ReconciliationPage{{Done: true}}}
	index := &branchReader{pages: []ReconciliationPage{{Done: true}}}
	reconciler, _ := NewReconciler(source, index, repair, limits)
	for _, request := range []ReconciliationRequest{{}, {Tenant: "t", Index: "i", PageSize: 0, MaxRecords: 1}, {Tenant: "t", Index: "i", PageSize: limits.MaxPageItems + 1, MaxRecords: 1}} {
		if _, err := reconciler.Run(t.Context(), request); !errors.Is(err, ErrInvalidReconciliation) {
			t.Fatal(err)
		}
	}
	request := ReconciliationRequest{Tenant: "t", Index: "i", PageSize: 1, MaxRecords: 10}
	boundaryRequest := ReconciliationRequest{
		Tenant: strings.Repeat("t", limits.MaxTenantBytes), Index: strings.Repeat("i", limits.MaxIndexBytes),
		PageSize: limits.MaxPageItems, MaxRecords: limits.MaxPages * limits.MaxPageItems,
	}
	if _, err := reconciler.Run(t.Context(), boundaryRequest); err != nil {
		t.Fatal("exact reconciliation request boundary rejected", err)
	}
	for _, invalid := range []ReconciliationRequest{
		{Tenant: strings.Repeat("t", limits.MaxTenantBytes+1), Index: "i", PageSize: 1, MaxRecords: 1},
		{Tenant: "t", Index: strings.Repeat("i", limits.MaxIndexBytes+1), PageSize: 1, MaxRecords: 1},
		{Tenant: "t", Index: "i", PageSize: 1, MaxRecords: limits.MaxPages*limits.MaxPageItems + 1},
	} {
		if _, err := reconciler.Run(t.Context(), invalid); !errors.Is(err, ErrInvalidReconciliation) {
			t.Fatal("invalid reconciliation boundary accepted", err)
		}
	}
	source.err = errReconcileBranch
	if _, err := reconciler.Run(t.Context(), request); !errors.Is(err, errReconcileBranch) {
		t.Fatal(err)
	}
	source.err = nil
	source.calls = 0
	source.pages = []ReconciliationPage{{Done: true}}
	index.err = errReconcileBranch
	if _, err := reconciler.Run(t.Context(), request); !errors.Is(err, errReconcileBranch) {
		t.Fatal(err)
	}

	malformed := []ReconciliationPage{{Done: false}, {Records: []ReconciliationRecord{{}}, Done: true}, {Records: []ReconciliationRecord{{ID: "id", Version: 1, Digest: "d", Document: &Document{Tenant: "wrong", Index: "i", ID: "id", Version: 1}}}, Done: true}, {Records: []ReconciliationRecord{validBranchRecord(t, "b", 1), validBranchRecord(t, "a", 1)}, Done: true}, {Records: []ReconciliationRecord{validBranchRecord(t, "a", 1), validBranchRecord(t, "a", 1)}, Done: true}}
	for _, page := range malformed {
		source = &branchReader{pages: []ReconciliationPage{page}}
		_, err := readReconciliation(t.Context(), source, request, true)
		if !errors.Is(err, ErrMalformedReconciliation) {
			t.Fatalf("page %#v error=%v", page, err)
		}
	}
	progress := &branchReader{pages: []ReconciliationPage{{Records: []ReconciliationRecord{validBranchRecord(t, "a", 1)}, Cursor: "same"}, {Records: []ReconciliationRecord{validBranchRecord(t, "b", 1)}, Cursor: "same"}}}
	if _, err := readReconciliation(t.Context(), progress, request, true); !errors.Is(err, ErrMalformedReconciliation) {
		t.Fatal(err)
	}
	tooMany := &branchReader{pages: []ReconciliationPage{{Records: []ReconciliationRecord{validBranchRecord(t, "a", 1), validBranchRecord(t, "b", 1)}, Done: true}}}
	limited := request
	limited.PageSize = 2
	limited.MaxRecords = 1
	if _, err := readReconciliation(t.Context(), tooMany, limited, true); !errors.Is(err, ErrReconciliationLimit) {
		t.Fatal(err)
	}
	single := &branchReader{pages: []ReconciliationPage{{Records: []ReconciliationRecord{validBranchRecord(t, "a", 1)}, Done: true}}}
	if records, err := readReconciliation(t.Context(), single, request, true); err != nil || len(records) != 1 {
		t.Fatal(records, err)
	}
	twoPages := []ReconciliationPage{
		{Records: []ReconciliationRecord{validBranchRecord(t, "a", 1)}, Cursor: "1"},
		{Records: []ReconciliationRecord{validBranchRecord(t, "b", 1)}, Done: true},
	}
	exactTotal := request
	exactTotal.MaxRecords = 2
	if records, err := readReconciliation(t.Context(), &branchReader{pages: twoPages}, exactTotal, true); err != nil || len(records) != 2 {
		t.Fatal(records, err)
	}
	overTotal := request
	overTotal.MaxRecords = 1
	if _, err := readReconciliation(t.Context(), &branchReader{pages: twoPages}, overTotal, true); !errors.Is(err, ErrReconciliationLimit) {
		t.Fatal(err)
	}
	duplicateRequest := request
	duplicateRequest.PageSize = 2
	duplicates := &branchReader{pages: []ReconciliationPage{{Records: []ReconciliationRecord{validBranchRecord(t, "a", 1), validBranchRecord(t, "a", 1)}, Done: true}}}
	if _, err := readReconciliation(t.Context(), duplicates, duplicateRequest, true); !errors.Is(err, ErrMalformedReconciliation) {
		t.Fatal(err)
	}
}

func TestInternalReconcilerRepairBranches(t *testing.T) {
	limits := DefaultLimits()
	request := ReconciliationRequest{Tenant: "t", Index: "i", PageSize: 10, MaxRecords: 20, Repair: true}
	sourceRecords := []ReconciliationRecord{validBranchRecord(t, "a", 3), validBranchRecord(t, "c", 2), validBranchRecord(t, "d", 1)}
	indexRecords := []ReconciliationRecord{IndexRecord("b", math.MaxUint64, "x"), IndexRecord("c", 1, "old"), IndexRecord("d", 2, "new")}
	source := &branchReader{pages: []ReconciliationPage{{Records: sourceRecords, Done: true}}}
	index := &branchReader{pages: []ReconciliationPage{{Records: indexRecords, Done: true}}}
	applied, _ := NewBulkResult([]ItemOutcome{{Position: 0, ID: "a", Action: ActionIndex, State: OutcomeApplied}, {Position: 1, ID: "c", Action: ActionIndex, State: OutcomeApplied}})
	reconciler, _ := NewReconciler(source, index, branchRepair{result: applied}, limits)
	report, err := reconciler.Run(t.Context(), request)
	if err != nil || report.Repaired != 2 || len(report.Drift) != 4 {
		t.Fatalf("report=%#v err=%v", report, err)
	}

	newReaders := func() (*branchReader, *branchReader) {
		return &branchReader{pages: []ReconciliationPage{{Records: []ReconciliationRecord{validBranchRecord(t, "a", 2)}, Done: true}}}, &branchReader{pages: []ReconciliationPage{{Done: true}}}
	}
	source, index = newReaders()
	reconciler, _ = NewReconciler(source, index, branchRepair{err: errReconcileBranch}, limits)
	if _, err := reconciler.Run(t.Context(), request); !errors.Is(err, errReconcileBranch) {
		t.Fatal(err)
	}
	partial, _ := NewBulkResult([]ItemOutcome{{Position: 0, ID: "a", Action: ActionIndex, State: OutcomeUnknown}})
	source, index = newReaders()
	reconciler, _ = NewReconciler(source, index, branchRepair{result: partial}, limits)
	if _, err := reconciler.Run(t.Context(), request); !errors.Is(err, ErrRepairPartial) {
		t.Fatal(err)
	}
	request.Repair = false
	source, index = newReaders()
	reconciler, _ = NewReconciler(source, index, branchRepair{}, limits)
	if report, err := reconciler.Run(t.Context(), request); err != nil || report.Repaired != 0 {
		t.Fatal(err)
	}

	source = &branchReader{pages: []ReconciliationPage{{Records: []ReconciliationRecord{validBranchRecord(t, "a", 1)}, Done: true}}}
	index = &branchReader{pages: []ReconciliationPage{{Records: []ReconciliationRecord{IndexRecord("b", 1, "x")}, Done: true}}}
	request.MaxRecords = 1
	reconciler, _ = NewReconciler(source, index, branchRepair{}, limits)
	if _, err := reconciler.Run(t.Context(), request); !errors.Is(err, ErrReconciliationLimit) {
		t.Fatal(err)
	}
	bounded := limits
	bounded.MaxBulkItems = 1
	request.MaxRecords = 10
	request.Repair = true
	source = &branchReader{pages: []ReconciliationPage{{Records: []ReconciliationRecord{validBranchRecord(t, "a", 1), validBranchRecord(t, "b", 1)}, Done: true}}}
	index = &branchReader{pages: []ReconciliationPage{{Done: true}}}
	reconciler, _ = NewReconciler(source, index, branchRepair{}, bounded)
	if _, err := reconciler.Run(t.Context(), request); !errors.Is(err, ErrBulkLimit) {
		t.Fatal(err)
	}
}
