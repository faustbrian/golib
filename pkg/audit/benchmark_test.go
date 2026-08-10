package audit_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"slices"
	"strconv"
	"strings"
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
	record := benchmarkUnredactedRecord(b)
	redactor, _ := audit.NewRedactor(audit.RedactionRules{AllowedAttributes: []string{"app.safe"}, AllowedChanges: []string{"status"}})
	b.ReportAllocs()
	for b.Loop() {
		if _, err := redactor.Redact(context.Background(), record); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReferenceRedaction(b *testing.B) {
	record := benchmarkUnredactedRecord(b)
	expectedRedactor, _ := audit.NewRedactor(audit.RedactionRules{AllowedAttributes: []string{"app.safe"}, AllowedChanges: []string{"status"}})
	expected, err := expectedRedactor.Redact(context.Background(), record)
	if err != nil {
		b.Fatal(err)
	}
	reference := referenceRedact(record)
	if expected.Attributes()["app.safe"] != reference.attributes["app.safe"] || expected.Changes().After()["status"] != reference.after["status"] {
		b.Fatal("reference redaction does not match the library workload")
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = referenceRedact(record)
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

func BenchmarkReferenceAppend(b *testing.B) {
	record := benchmarkRecord(b)
	b.ReportAllocs()
	for b.Loop() {
		store := make(map[string][]byte, 1)
		if err := referenceAppend(store, []audit.Record{record}); err != nil {
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

func BenchmarkReferenceBatchAppend(b *testing.B) {
	records := benchmarkRecords(b, 100)
	b.ReportAllocs()
	for b.Loop() {
		store := make(map[string][]byte, len(records))
		if err := referenceAppend(store, records); err != nil {
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

func BenchmarkReferenceFilteredPagination(b *testing.B) {
	records := benchmarkRecords(b, 1_000)
	if result := referenceFilteredPage(records, 100); len(result) != 100 {
		b.Fatalf("reference filtered records = %d", len(result))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = referenceFilteredPage(records, 100)
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

func BenchmarkReferenceExport(b *testing.B) {
	records := benchmarkRecords(b, 1_000)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		exported := referenceFilteredPage(records, len(records))
		count := 0
		for range exported {
			count++
		}
		if count != len(records) {
			b.Fatal("reference export was truncated")
		}
	}
}

func BenchmarkIntegrityVerification(b *testing.B) {
	records, chain := benchmarkIntegrityRecords(b, 100)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := chain.Verify(context.Background(), records); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReferenceIntegrityVerification(b *testing.B) {
	records, _ := benchmarkIntegrityRecords(b, 100)
	if !referenceVerifyIntegrity(records) {
		b.Fatal("reference integrity verifier rejected the valid chain")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if !referenceVerifyIntegrity(records) {
			b.Fatal("reference integrity verifier rejected the valid chain")
		}
	}
}

type referenceRedactedRecord struct {
	attributes, before, after map[string]string
}

func referenceRedact(record audit.Record) referenceRedactedRecord {
	result := referenceRedactedRecord{attributes: make(map[string]string), before: make(map[string]string), after: make(map[string]string)}
	if value, exists := record.Attributes()["app.safe"]; exists {
		result.attributes["app.safe"] = value
	}
	if value, exists := record.Changes().Before()["status"]; exists {
		result.before["status"] = value
	}
	if value, exists := record.Changes().After()["status"]; exists {
		result.after["status"] = value
	}
	return result
}

func referenceAppend(store map[string][]byte, records []audit.Record) error {
	prepared := make(map[string][]byte, len(records))
	for _, record := range records {
		canonical, err := audit.CanonicalJSON(record)
		if err != nil {
			return err
		}
		if existing, exists := store[record.ID()]; exists && !bytes.Equal(existing, canonical) {
			return audit.ErrDuplicateConflict
		}
		if existing, exists := prepared[record.ID()]; exists && !bytes.Equal(existing, canonical) {
			return audit.ErrDuplicateConflict
		}
		prepared[record.ID()] = append([]byte(nil), canonical...)
	}
	for id, canonical := range prepared {
		store[id] = canonical
	}
	return nil
}

func referenceFilteredPage(records []audit.Record, limit int) []audit.Record {
	ordered := append([]audit.Record(nil), records...)
	slices.SortFunc(ordered, compareBenchmarkRecords)
	result := make([]audit.Record, 0, limit)
	for _, record := range ordered {
		if record.Context().TenantID() != "tenant-1" || record.Actor().ID() != "user-1" ||
			record.Subject().Type() != "invoice" || record.Subject().ID() != "invoice-1" ||
			record.Action() != "invoice.updated" || record.Context().CorrelationID() != "correlation-1" ||
			record.Outcome() != audit.OutcomeSucceeded {
			continue
		}
		result = append(result, record)
		if len(result) == limit {
			break
		}
	}
	return result
}

func compareBenchmarkRecords(left, right audit.Record) int {
	if comparison := left.RecordedAt().Compare(right.RecordedAt()); comparison != 0 {
		return comparison
	}
	return strings.Compare(left.ID(), right.ID())
}

func benchmarkIntegrityRecords(tb testing.TB, count int) ([]audit.Record, *audit.Chain) {
	tb.Helper()
	records := benchmarkRecords(tb, count)
	chain, err := audit.NewChain(audit.ChainConfig{Algorithm: audit.IntegritySHA256})
	if err != nil {
		tb.Fatal(err)
	}
	previous := []byte(nil)
	for index := range records {
		sealed, err := chain.Seal(context.Background(), records[index], audit.ChainLink{
			Partition: "tenant-1", Sequence: uint64(index + 1), PreviousDigest: previous,
		})
		if err != nil {
			tb.Fatal(err)
		}
		records[index] = sealed
		previous = sealed.Integrity().Digest()
	}
	return records, chain
}

func referenceVerifyIntegrity(records []audit.Record) bool {
	var previous []byte
	for index, record := range records {
		integrity := record.Integrity()
		if integrity.Algorithm() != audit.IntegritySHA256 || integrity.Partition() != "tenant-1" ||
			integrity.Sequence() != uint64(index+1) || subtle.ConstantTimeCompare(integrity.PreviousDigest(), previous) != 1 {
			return false
		}
		canonical, err := audit.CanonicalJSON(record)
		if err != nil {
			return false
		}
		marker := bytes.LastIndex(canonical, []byte(`,"digest":"`))
		if marker < 0 || marker+len(`,"digest":"`)+sha256.Size*2 >= len(canonical) {
			return false
		}
		unsigned := append([]byte(nil), canonical[:marker]...)
		unsigned = append(unsigned, canonical[marker+len(`,"digest":"`)+sha256.Size*2+1:]...)
		digest := sha256.Sum256(unsigned)
		if subtle.ConstantTimeCompare(digest[:], integrity.Digest()) != 1 {
			return false
		}
		previous = integrity.Digest()
	}
	return len(records) != 0
}

func benchmarkRecord(tb testing.TB) audit.Record {
	tb.Helper()
	return redactBenchmarkRecord(tb, benchmarkUnredactedRecord(tb))
}

func benchmarkUnredactedRecord(tb testing.TB) audit.Record {
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

func redactBenchmarkRecord(tb testing.TB, record audit.Record) audit.Record {
	tb.Helper()
	redactor := audit.RedactorFunc(func(_ context.Context, record audit.Record) (audit.Record, error) {
		return record, nil
	})
	redacted, err := redactor.Redact(context.Background(), record)
	if err != nil {
		tb.Fatal(err)
	}
	return redacted
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
		records[index] = redactBenchmarkRecord(tb, records[index])
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
