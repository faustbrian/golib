package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
)

func TestConfigurationAndCapacityAcceptExactCeilings(t *testing.T) {
	t.Parallel()

	for _, config := range []Config{
		{MaxBytes: 1, MaxBatchRecords: 1},
		{MaxRecords: 1, MaxBatchRecords: 1},
		{MaxRecords: 1, MaxBytes: 1},
		{MaxRecords: 1, MaxBytes: 1, MaxBatchRecords: audit.MaxAppendBatchRecords + 1},
	} {
		if _, err := New(config); !errors.Is(err, audit.ErrInvalidArgument) {
			t.Fatalf("New(%#v) error = %v", config, err)
		}
	}
	if _, err := New(Config{MaxRecords: 1, MaxBytes: 1, MaxBatchRecords: audit.MaxAppendBatchRecords}); err != nil {
		t.Fatalf("exact-limit New() error = %v", err)
	}
	record := internalMemoryRecord("exact-capacity", time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC))
	encoded, _ := audit.CanonicalJSON(record)
	store, err := New(Config{MaxRecords: 1, MaxBytes: len(encoded), MaxBatchRecords: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := store.Append(context.Background(), record); err != nil || result.Status != audit.AppendAccepted || store.usedBytes != len(encoded) {
		t.Fatalf("exact-capacity Append() = %#v, used=%d, %v", result, store.usedBytes, err)
	}
	store, _ = New(Config{MaxRecords: 1, MaxBytes: len(encoded) - 1, MaxBatchRecords: 1})
	if _, err := store.Append(context.Background(), record); !errors.Is(err, audit.ErrBackpressure) {
		t.Fatalf("over-capacity Append() error = %v", err)
	}
	second := internalMemoryRecord("second-capacity", record.RecordedAt())
	secondEncoded, _ := audit.CanonicalJSON(second)
	store, _ = New(Config{MaxRecords: 2, MaxBytes: len(encoded) + len(secondEncoded) - 1, MaxBatchRecords: 1})
	if _, err := store.Append(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(context.Background(), second); !errors.Is(err, audit.ErrBackpressure) {
		t.Fatalf("cumulative over-capacity Append() error = %v", err)
	}
}

func TestQuerySeparatesValidationAndExactPagination(t *testing.T) {
	t.Parallel()

	query, _ := audit.NewQuery(audit.QueryInput{Tenant: audit.NoTenant(), Limit: 1})
	var nilStore *Store
	if _, err := nilStore.Query(context.Background(), query); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("nil-store Query() error = %v", err)
	}
	store, _ := New(Config{MaxRecords: 2, MaxBytes: 1 << 20, MaxBatchRecords: 2})
	base := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	if _, err := store.AppendBatch(context.Background(), []audit.Record{
		internalMemoryRecord("record-b", base),
		internalMemoryRecord("record-a", base),
	}); err != nil {
		t.Fatal(err)
	}
	page, err := store.Query(context.Background(), query)
	if err != nil || len(page.Records) != 1 || page.Records[0].ID() != "record-a" || page.Next.IsZero() {
		t.Fatalf("first page = %#v, %v", page, err)
	}
	next, _ := audit.NewQuery(audit.QueryInput{Tenant: audit.NoTenant(), Limit: 1, After: page.Next})
	page, err = store.Query(context.Background(), next)
	if err != nil || len(page.Records) != 1 || page.Records[0].ID() != "record-b" || !page.Next.IsZero() {
		t.Fatalf("final page = %#v, %v", page, err)
	}
}
