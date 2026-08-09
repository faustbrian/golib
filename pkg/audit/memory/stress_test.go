package memory_test

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
	"github.com/faustbrian/golib/pkg/audit/memory"
)

func TestStoreConcurrentDuplicateStressIsBoundedAndIdempotent(t *testing.T) {
	t.Parallel()

	const workers = 32
	store, err := memory.New(memory.Config{MaxRecords: 1, MaxBytes: 1 << 20, MaxBatchRecords: 1})
	if err != nil {
		t.Fatal(err)
	}
	record := testRecord(t, "concurrent-record", "invoice.created")
	var accepted atomic.Int32
	var duplicates atomic.Int32
	errorsByWorker := make(chan error, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			result, err := store.Append(context.Background(), record)
			if err != nil {
				errorsByWorker <- err
				return
			}
			switch result.Status {
			case audit.AppendAccepted:
				accepted.Add(1)
			case audit.AppendDuplicate:
				duplicates.Add(1)
			default:
				errorsByWorker <- audit.ErrInvalidArgument
			}
		}()
	}
	group.Wait()
	close(errorsByWorker)
	for err := range errorsByWorker {
		t.Fatalf("concurrent Append() error = %v", err)
	}
	if accepted.Load() != 1 || duplicates.Load() != workers-1 {
		t.Fatalf("concurrent statuses: accepted=%d duplicates=%d", accepted.Load(), duplicates.Load())
	}
}

func TestStoreLargeTenantHighCardinalitySoakRemainsBounded(t *testing.T) {
	t.Parallel()

	const count = audit.MaxAppendBatchRecords
	store, err := memory.New(memory.Config{MaxRecords: count, MaxBytes: count << 20, MaxBatchRecords: count})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	records := make([]audit.Record, count)
	for index := range records {
		id := strconv.Itoa(index)
		builder, err := audit.NewBuilder(audit.BuilderConfig{
			Clock:       func() time.Time { return now },
			IDGenerator: func() (string, error) { return "soak-record-" + id, nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		records[index], err = builder.Build(audit.RecordInput{
			OccurredAt: now, Action: "resource.observed", Outcome: audit.OutcomeSucceeded,
			Actor:   audit.ActorInput{Kind: audit.ActorService, ID: "soak-writer"},
			Subject: audit.SubjectInput{Type: "resource", ID: "subject-" + id},
			Context: audit.ContextInput{TenantID: "large-tenant"},
			Changes: audit.ChangeSetInput{NoChange: true},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if result, err := store.AppendBatch(context.Background(), records); err != nil || len(result.Results) != count {
		t.Fatalf("large AppendBatch() results/error = %d, %v", len(result.Results), err)
	}
	tenant, _ := audit.Tenant("large-tenant")
	query, _ := audit.NewQuery(audit.QueryInput{Tenant: tenant, Limit: count})
	page, err := store.Query(context.Background(), query)
	if err != nil || len(page.Records) != count || !page.Next.IsZero() {
		t.Fatalf("large Query() records/cursor/error = %d, %q, %v", len(page.Records), page.Next.String(), err)
	}
	if _, err := store.Append(context.Background(), records[0]); err != nil {
		t.Fatalf("duplicate at capacity error = %v", err)
	}
}
