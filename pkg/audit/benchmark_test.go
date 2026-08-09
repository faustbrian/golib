package audit_test

import (
	"context"
	"crypto/sha256"
	"strconv"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
	"github.com/faustbrian/golib/pkg/audit/memory"
)

func BenchmarkCanonicalJSON(b *testing.B) {
	record := benchmarkRecord(b)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := audit.CanonicalJSON(record); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCanonicalSHA256Baseline(b *testing.B) {
	record := benchmarkRecord(b)
	b.ReportAllocs()
	for b.Loop() {
		encoded, err := audit.CanonicalJSON(record)
		if err != nil {
			b.Fatal(err)
		}
		_ = sha256.Sum256(encoded)
	}
}

func BenchmarkRedaction(b *testing.B) {
	record := benchmarkRecord(b)
	redactor, _ := audit.NewRedactor(audit.RedactionRules{AllowedAttributes: []string{"app.safe"}, AllowedChanges: []string{"status"}})
	b.ReportAllocs()
	for b.Loop() {
		if _, err := redactor.Redact(context.Background(), record); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMemoryAppend(b *testing.B) {
	record := benchmarkRecord(b)
	b.ReportAllocs()
	for b.Loop() {
		store, err := memory.New(memory.Config{MaxRecords: 1, MaxBytes: 1 << 20, MaxBatchRecords: 1})
		if err != nil {
			b.Fatal(err)
		}
		if _, err := store.Append(context.Background(), record); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMemoryBatchAppend(b *testing.B) {
	records := benchmarkRecords(b, 100)
	b.ReportAllocs()
	for b.Loop() {
		store, err := memory.New(memory.Config{MaxRecords: len(records), MaxBytes: 100 << 20, MaxBatchRecords: len(records)})
		if err != nil {
			b.Fatal(err)
		}
		if _, err := store.AppendBatch(context.Background(), records); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMemoryFilteredPagination(b *testing.B) {
	store := benchmarkStore(b, 1_000)
	tenant, _ := audit.Tenant("tenant-1")
	query, _ := audit.NewQuery(audit.QueryInput{
		Tenant: tenant, ActorID: "user-1", SubjectType: "invoice",
		SubjectID: "invoice-1", Action: "invoice.updated", CorrelationID: "correlation-1",
		Outcome: audit.OutcomeSucceeded, Limit: 100,
	})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := store.Query(context.Background(), query); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMemoryExport(b *testing.B) {
	store := benchmarkStore(b, 1_000)
	query, _ := audit.NewQuery(audit.QueryInput{Tenant: audit.AllTenants(), Limit: 1_000})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := store.Export(context.Background(), query, func(audit.Record) error { return nil }); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIntegrityVerification(b *testing.B) {
	records := benchmarkRecords(b, 100)
	chain, _ := audit.NewChain(audit.ChainConfig{Algorithm: audit.IntegritySHA256})
	previous := []byte(nil)
	for index := range records {
		sealed, err := chain.Seal(context.Background(), records[index], audit.ChainLink{
			Partition: "tenant-1", Sequence: uint64(index + 1), PreviousDigest: previous,
		})
		if err != nil {
			b.Fatal(err)
		}
		records[index] = sealed
		previous = sealed.Integrity().Digest()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := chain.Verify(context.Background(), records); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkRecord(tb testing.TB) audit.Record {
	tb.Helper()
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	builder, err := audit.NewBuilder(audit.BuilderConfig{Clock: func() time.Time { return now }, IDGenerator: func() (string, error) { return "benchmark-record", nil }})
	if err != nil {
		tb.Fatal(err)
	}
	record, err := builder.Build(audit.RecordInput{
		OccurredAt: now, Action: "invoice.updated", Outcome: audit.OutcomeSucceeded,
		Actor:      audit.ActorInput{Kind: audit.ActorHuman, ID: "user-1"},
		Subject:    audit.SubjectInput{Type: "invoice", ID: "invoice-1"},
		Changes:    audit.ChangeSetInput{Before: map[string]string{"status": "open"}, After: map[string]string{"status": "paid"}},
		Attributes: map[string]string{"app.safe": "value"},
	})
	if err != nil {
		tb.Fatal(err)
	}
	return record
}

func benchmarkRecords(tb testing.TB, count int) []audit.Record {
	tb.Helper()
	records := make([]audit.Record, count)
	for index := range records {
		record := benchmarkRecord(tb)
		builder, err := audit.NewBuilder(audit.BuilderConfig{
			Clock:       func() time.Time { return record.RecordedAt() },
			IDGenerator: func() (string, error) { return "benchmark-record-" + strconv.Itoa(index), nil },
		})
		if err != nil {
			tb.Fatal(err)
		}
		records[index], err = builder.Build(audit.RecordInput{
			OccurredAt: record.OccurredAt(), Action: record.Action(), Outcome: record.Outcome(),
			Actor:   audit.ActorInput{Kind: record.Actor().Kind(), ID: record.Actor().ID()},
			Subject: audit.SubjectInput{Type: record.Subject().Type(), ID: record.Subject().ID()},
			Context: audit.ContextInput{TenantID: "tenant-1", CorrelationID: "correlation-1"},
			Changes: audit.ChangeSetInput{NoChange: true},
		})
		if err != nil {
			tb.Fatal(err)
		}
	}
	return records
}

func benchmarkStore(tb testing.TB, count int) *memory.Store {
	tb.Helper()
	records := benchmarkRecords(tb, count)
	store, err := memory.New(memory.Config{MaxRecords: count, MaxBytes: count << 20, MaxBatchRecords: count})
	if err != nil {
		tb.Fatal(err)
	}
	if _, err := store.AppendBatch(context.Background(), records); err != nil {
		tb.Fatal(err)
	}
	return store
}
