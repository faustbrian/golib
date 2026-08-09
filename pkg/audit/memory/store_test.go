package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
	"github.com/faustbrian/golib/pkg/audit/memory"
)

func TestStoreAppendIsIdempotentByRecordIDAndRejectsConflicts(t *testing.T) {
	t.Parallel()

	store, err := memory.New(memory.Config{
		MaxRecords:      2,
		MaxBytes:        1 << 20,
		MaxBatchRecords: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	first := testRecord(t, "record-1", "invoice.created")
	accepted, err := store.Append(context.Background(), first)
	if err != nil || accepted.RecordID != first.ID() || accepted.Status != audit.AppendAccepted {
		t.Fatalf("first append = %#v, %v", accepted, err)
	}

	duplicate, err := store.Append(context.Background(), first)
	if err != nil || duplicate.RecordID != first.ID() || duplicate.Status != audit.AppendDuplicate {
		t.Fatalf("duplicate append = %#v, %v", duplicate, err)
	}

	conflict := testRecord(t, "record-1", "invoice.deleted")
	if _, err := store.Append(context.Background(), conflict); !errors.Is(err, audit.ErrDuplicateConflict) ||
		audit.AppendOutcomeOf(err) != audit.AppendRejected {
		t.Fatalf("conflicting append error = %v", err)
	}
}

func TestStoreQueryUsesExplicitTenantScopeAndStableCursorOrder(t *testing.T) {
	t.Parallel()

	store, err := memory.New(memory.Config{MaxRecords: 10, MaxBytes: 1 << 20, MaxBatchRecords: 10})
	if err != nil {
		t.Fatal(err)
	}
	recordedAt := time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC)
	records := []audit.Record{
		testRecordAt(t, "record-b", "invoice.viewed", "tenant-1", recordedAt),
		testRecordAt(t, "record-c", "invoice.viewed", "tenant-2", recordedAt.Add(time.Second)),
		testRecordAt(t, "record-a", "invoice.created", "tenant-1", recordedAt),
	}
	if _, err := store.AppendBatch(context.Background(), records); err != nil {
		t.Fatal(err)
	}

	tenant, err := audit.Tenant("tenant-1")
	if err != nil {
		t.Fatal(err)
	}
	firstQuery, err := audit.NewQuery(audit.QueryInput{
		Tenant: tenant,
		Action: "invoice.created",
		Limit:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstPage, err := store.Query(context.Background(), firstQuery)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage.Records) != 1 || firstPage.Records[0].ID() != "record-a" || !firstPage.Next.IsZero() {
		t.Fatalf("filtered first page = %#v", firstPage)
	}

	allTenantRecords, err := audit.NewQuery(audit.QueryInput{Tenant: tenant, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.Query(context.Background(), allTenantRecords)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.Records[0].ID() != "record-a" || page.Next.IsZero() {
		t.Fatalf("first tenant page = %#v", page)
	}
	nextQuery, err := audit.NewQuery(audit.QueryInput{Tenant: tenant, Limit: 1, After: page.Next})
	if err != nil {
		t.Fatal(err)
	}
	nextPage, err := store.Query(context.Background(), nextQuery)
	if err != nil {
		t.Fatal(err)
	}
	if len(nextPage.Records) != 1 || nextPage.Records[0].ID() != "record-b" || !nextPage.Next.IsZero() {
		t.Fatalf("second tenant page = %#v", nextPage)
	}
}

func TestStoreExportStreamsOutsideLockAndHonorsCancellation(t *testing.T) {
	t.Parallel()
	store, err := memory.New(memory.Config{MaxRecords: 4, MaxBytes: 1 << 20, MaxBatchRecords: 4})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC)
	if _, err := store.AppendBatch(context.Background(), []audit.Record{
		testRecordAt(t, "record-a", "invoice.created", "tenant-1", base),
		testRecordAt(t, "record-b", "invoice.viewed", "tenant-1", base.Add(time.Second)),
	}); err != nil {
		t.Fatal(err)
	}
	tenant, err := audit.Tenant("tenant-1")
	if err != nil {
		t.Fatal(err)
	}
	query, err := audit.NewQuery(audit.QueryInput{Tenant: tenant, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}

	exported := make([]string, 0, 2)
	err = store.Export(context.Background(), query, func(record audit.Record) error {
		exported = append(exported, record.ID())
		if record.ID() == "record-a" {
			_, appendErr := store.Append(context.Background(), testRecordAt(t, "record-c", "tenant-2", "invoice.created", base))
			return appendErr
		}
		return nil
	})
	if err != nil || len(exported) != 2 || exported[0] != "record-a" || exported[1] != "record-b" {
		t.Fatalf("Export() = %#v, %v", exported, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	count := 0
	err = store.Export(ctx, query, func(audit.Record) error {
		count++
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || count != 1 {
		t.Fatalf("canceled Export() count/error = %d, %v", count, err)
	}
}

func TestStoreEnforcesAllCapacityAndAtomicityBoundaries(t *testing.T) {
	t.Parallel()

	for _, config := range []memory.Config{
		{},
		{MaxRecords: 1, MaxBytes: 1, MaxBatchRecords: audit.MaxAppendBatchRecords + 1},
	} {
		if _, err := memory.New(config); !errors.Is(err, audit.ErrInvalidArgument) {
			t.Fatalf("New(%#v) error = %v", config, err)
		}
	}
	store, err := memory.New(memory.Config{MaxRecords: 1, MaxBytes: 1 << 20, MaxBatchRecords: 2})
	if err != nil {
		t.Fatal(err)
	}
	first := testRecord(t, "record-1", "invoice.created")
	second := testRecord(t, "record-2", "invoice.created")
	if _, err := store.AppendBatch(context.Background(), []audit.Record{first, second}); !errors.Is(err, audit.ErrBackpressure) || audit.AppendOutcomeOf(err) != audit.AppendRejected {
		t.Fatalf("capacity AppendBatch() error = %v", err)
	}
	query, _ := audit.NewQuery(audit.QueryInput{Tenant: audit.NoTenant(), Limit: 2})
	page, err := store.Query(context.Background(), query)
	if err != nil || len(page.Records) != 0 {
		t.Fatalf("failed batch was partially committed: %#v, %v", page, err)
	}

	for _, records := range [][]audit.Record{nil, {first, second, first}} {
		if _, err := store.AppendBatch(context.Background(), records); !errors.Is(err, audit.ErrBatchTooLarge) {
			t.Fatalf("bounded AppendBatch(%d) error = %v", len(records), err)
		}
	}
	if _, err := store.AppendBatch(context.Background(), []audit.Record{{}}); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("zero record AppendBatch() error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Append(canceled, first); !errors.Is(err, context.Canceled) || audit.AppendOutcomeOf(err) != audit.AppendRejected {
		t.Fatalf("canceled Append() error = %v", err)
	}
	var nilStore *memory.Store
	if _, err := nilStore.Append(context.Background(), first); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("nil store Append() error = %v", err)
	}
	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if _, err := store.Append(nil, first); !errors.Is(err, audit.ErrInvalidArgument) { //nolint:staticcheck // Explicit nil-context validation is under test.
		t.Fatalf("nil context Append() error = %v", err)
	}

	tiny, err := memory.New(memory.Config{MaxRecords: 2, MaxBytes: 1, MaxBatchRecords: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tiny.Append(context.Background(), first); !errors.Is(err, audit.ErrBackpressure) {
		t.Fatalf("byte-bound Append() error = %v", err)
	}
}

func TestStoreBatchDeduplicatesWithinCallAndQueriesEveryFilter(t *testing.T) {
	t.Parallel()

	store, err := memory.New(memory.Config{MaxRecords: 4, MaxBytes: 1 << 20, MaxBatchRecords: 4})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC)
	record := detailedRecord(t, "record-a", "tenant-1", base)
	result, err := store.AppendBatch(context.Background(), []audit.Record{record, record})
	if err != nil || len(result.Results) != 2 || result.Results[0].Status != audit.AppendAccepted || result.Results[1].Status != audit.AppendDuplicate {
		t.Fatalf("duplicate-in-batch AppendBatch() = %#v, %v", result, err)
	}
	other := detailedRecord(t, "record-b", "", base.Add(2*time.Hour))
	if _, err := store.Append(context.Background(), other); err != nil {
		t.Fatal(err)
	}

	tenant, _ := audit.Tenant("tenant-1")
	query, err := audit.NewQuery(audit.QueryInput{
		Tenant: tenant, From: base, Through: base.Add(time.Hour), ActorID: "actor-1",
		SubjectType: "invoice", SubjectID: "invoice-1", Action: "invoice.denied",
		CorrelationID: "correlation-1", Outcome: audit.OutcomeDenied, Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.Query(context.Background(), query)
	if err != nil || len(page.Records) != 1 || page.Records[0].ID() != "record-a" {
		t.Fatalf("filtered Query() = %#v, %v", page, err)
	}
	for name, mutate := range map[string]func(*audit.QueryInput){
		"from":         func(input *audit.QueryInput) { input.From = base.Add(time.Second) },
		"through":      func(input *audit.QueryInput) { input.Through = base.Add(-time.Second) },
		"actor":        func(input *audit.QueryInput) { input.ActorID = "other" },
		"subject type": func(input *audit.QueryInput) { input.SubjectType = "account" },
		"subject ID":   func(input *audit.QueryInput) { input.SubjectID = "other" },
		"action":       func(input *audit.QueryInput) { input.Action = "other" },
		"correlation":  func(input *audit.QueryInput) { input.CorrelationID = "other" },
		"outcome":      func(input *audit.QueryInput) { input.Outcome = audit.OutcomeSucceeded },
	} {
		input := audit.QueryInput{Tenant: tenant, Limit: 2}
		mutate(&input)
		filtered, err := audit.NewQuery(input)
		if err != nil {
			t.Fatalf("%s NewQuery() error = %v", name, err)
		}
		page, err := store.Query(context.Background(), filtered)
		if err != nil || len(page.Records) != 0 {
			t.Fatalf("%s Query() = %#v, %v", name, page, err)
		}
	}
}

func TestStoreQueryAndExportValidateCancellationAndCallbackFailures(t *testing.T) {
	t.Parallel()

	store, _ := memory.New(memory.Config{MaxRecords: 2, MaxBytes: 1 << 20, MaxBatchRecords: 2})
	record := testRecord(t, "record-1", "invoice.created")
	_, _ = store.Append(context.Background(), record)
	query, _ := audit.NewQuery(audit.QueryInput{Tenant: audit.NoTenant(), Limit: 1})
	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if _, err := store.Query(nil, query); !errors.Is(err, audit.ErrInvalidArgument) { //nolint:staticcheck // Explicit nil-context validation is under test.
		t.Fatalf("nil-context Query() error = %v", err)
	}
	if _, err := store.Query(context.Background(), audit.Query{}); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("invalid Query() error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Query(canceled, query); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Query() error = %v", err)
	}
	if err := store.Export(context.Background(), query, nil); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("nil callback Export() error = %v", err)
	}
	callbackFailure := errors.New("archive failed")
	if err := store.Export(context.Background(), query, func(audit.Record) error { return callbackFailure }); !errors.Is(err, callbackFailure) {
		t.Fatalf("callback-failed Export() error = %v", err)
	}
}

func testRecord(t *testing.T, id, action string) audit.Record {
	t.Helper()
	clock := time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC)
	return testRecordAt(t, id, action, "", clock)
}

func testRecordAt(t *testing.T, id, action, tenant string, clock time.Time) audit.Record {
	t.Helper()
	builder, err := audit.NewBuilder(audit.BuilderConfig{
		Clock:       func() time.Time { return clock },
		IDGenerator: func() (string, error) { return id, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := builder.Build(audit.RecordInput{
		OccurredAt: clock,
		Action:     action,
		Outcome:    audit.OutcomeSucceeded,
		Actor:      audit.ActorInput{Kind: audit.ActorSystem, ID: "test"},
		Subject:    audit.SubjectInput{Type: "invoice", ID: "invoice-1"},
		Context:    audit.ContextInput{TenantID: tenant},
		Changes:    audit.ChangeSetInput{NoChange: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func detailedRecord(t *testing.T, id, tenant string, clock time.Time) audit.Record {
	t.Helper()
	builder, err := audit.NewBuilder(audit.BuilderConfig{Clock: func() time.Time { return clock }, IDGenerator: func() (string, error) { return id, nil }})
	if err != nil {
		t.Fatal(err)
	}
	record, err := builder.Build(audit.RecordInput{
		OccurredAt: clock, Action: "invoice.denied", Outcome: audit.OutcomeDenied,
		Actor:   audit.ActorInput{Kind: audit.ActorHuman, ID: "actor-1"},
		Subject: audit.SubjectInput{Type: "invoice", ID: "invoice-1"},
		Context: audit.ContextInput{TenantID: tenant, CorrelationID: "correlation-1"},
		Changes: audit.ChangeSetInput{NoChange: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return record
}
