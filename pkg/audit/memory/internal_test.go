package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
)

type cancelAfterContext struct{ calls int }

func (*cancelAfterContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*cancelAfterContext) Done() <-chan struct{}       { return nil }
func (ctx *cancelAfterContext) Err() error {
	ctx.calls++
	if ctx.calls > 1 {
		return context.Canceled
	}
	return nil
}
func (*cancelAfterContext) Value(any) any { return nil }

func TestQueryCancellationDuringIterationAndCorruptCursorAreRejected(t *testing.T) {
	t.Parallel()

	store, err := New(Config{MaxRecords: 2, MaxBytes: 1 << 20, MaxBatchRecords: 2})
	if err != nil {
		t.Fatal(err)
	}
	record := internalMemoryRecord("record-1", time.Now())
	encoded, _ := audit.CanonicalJSON(record)
	store.records[record.ID()] = entry{record: record, encoded: encoded}
	query, _ := audit.NewQuery(audit.QueryInput{Tenant: audit.NoTenant(), Limit: 1})
	if _, err := store.Query(&cancelAfterContext{}, query); !errors.Is(err, context.Canceled) {
		t.Fatalf("mid-query cancellation error = %v", err)
	}

	corrupt := internalMemoryRecord("", record.RecordedAt())
	store.records = map[string]entry{
		"":         {record: corrupt},
		"record-1": {record: record, encoded: encoded},
	}
	if _, err := store.Query(context.Background(), query); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("corrupt cursor Query() error = %v", err)
	}
}

func TestExportPropagatesQueryValidation(t *testing.T) {
	t.Parallel()

	store, _ := New(Config{MaxRecords: 1, MaxBytes: 1 << 20, MaxBatchRecords: 1})
	if err := store.Export(context.Background(), audit.Query{}, func(audit.Record) error { return nil }); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("invalid-query Export() error = %v", err)
	}
}

func internalMemoryRecord(id string, now time.Time) audit.Record {
	builder, _ := audit.NewBuilder(audit.BuilderConfig{Clock: func() time.Time { return now }, IDGenerator: func() (string, error) { return id, nil }})
	record, _ := builder.Build(audit.RecordInput{
		OccurredAt: now, Action: "test", Outcome: audit.OutcomeSucceeded,
		Actor:   audit.ActorInput{Kind: audit.ActorSystem, ID: "test"},
		Subject: audit.SubjectInput{Type: "test", ID: "test"},
		Changes: audit.ChangeSetInput{NoChange: true},
	})
	if id == "" {
		return audit.Record{}
	}
	return record
}
