package audit_test

import (
	"context"
	"fmt"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
	"github.com/faustbrian/golib/pkg/audit/memory"
)

func ExampleBuilder() {
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	builder, _ := audit.NewBuilder(audit.BuilderConfig{
		Clock:       func() time.Time { return now },
		IDGenerator: func() (string, error) { return "record-1", nil },
	})
	record, err := builder.Build(audit.RecordInput{
		OccurredAt: now,
		Action:     "invoice.approved",
		Outcome:    audit.OutcomeSucceeded,
		Actor:      audit.ActorInput{Kind: audit.ActorHuman, ID: "user-1"},
		Subject:    audit.SubjectInput{Type: "invoice", ID: "invoice-1"},
		Changes:    audit.ChangeSetInput{NoChange: true},
	})
	fmt.Println(record.ID(), record.Action(), err)
	// Output: record-1 invoice.approved <nil>
}

func ExampleRecorder() {
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	builder, _ := audit.NewBuilder(audit.BuilderConfig{
		Clock:       func() time.Time { return now },
		IDGenerator: func() (string, error) { return "record-1", nil },
	})
	record, _ := builder.Build(audit.RecordInput{
		OccurredAt: now, Action: "invoice.viewed", Outcome: audit.OutcomeSucceeded,
		Actor:   audit.ActorInput{Kind: audit.ActorService, ID: "billing-api"},
		Subject: audit.SubjectInput{Type: "invoice", ID: "invoice-1"},
		Changes: audit.ChangeSetInput{NoChange: true},
	})
	sink, _ := memory.New(memory.Config{MaxRecords: 10, MaxBytes: 1 << 20, MaxBatchRecords: 10})
	redactor, _ := audit.NewRedactor(audit.RedactionRules{})
	recorder, _ := audit.NewRecorder(audit.RecorderConfig{Sink: sink, Redactor: redactor, Mode: audit.DeliveryFailClosed})
	result, err := recorder.Submit(context.Background(), record)
	fmt.Println(result.Disposition == audit.DeliveryPersisted, err)
	// Output: true <nil>
}

func ExampleChain() {
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	builder, _ := audit.NewBuilder(audit.BuilderConfig{
		Clock:       func() time.Time { return now },
		IDGenerator: func() (string, error) { return "record-1", nil },
	})
	record, _ := builder.Build(audit.RecordInput{
		OccurredAt: now, Action: "invoice.approved", Outcome: audit.OutcomeSucceeded,
		Actor:   audit.ActorInput{Kind: audit.ActorService, ID: "billing"},
		Subject: audit.SubjectInput{Type: "invoice", ID: "invoice-1"},
		Changes: audit.ChangeSetInput{NoChange: true},
	})
	chain, _ := audit.NewChain(audit.ChainConfig{Algorithm: audit.IntegritySHA256})
	sealed, err := chain.Seal(context.Background(), record, audit.ChainLink{Partition: "tenant-1", Sequence: 1})
	verifyErr := chain.Verify(context.Background(), []audit.Record{sealed})
	fmt.Println(sealed.Integrity().Enabled(), len(sealed.Integrity().Digest()), err, verifyErr)
	// Output: true 32 <nil> <nil>
}

func ExampleExporter() {
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	builder, _ := audit.NewBuilder(audit.BuilderConfig{
		Clock:       func() time.Time { return now },
		IDGenerator: func() (string, error) { return "record-1", nil },
	})
	record, _ := builder.Build(audit.RecordInput{
		OccurredAt: now, Action: "invoice.viewed", Outcome: audit.OutcomeSucceeded,
		Actor:   audit.ActorInput{Kind: audit.ActorHuman, ID: "user-1"},
		Subject: audit.SubjectInput{Type: "invoice", ID: "invoice-1"},
		Context: audit.ContextInput{TenantID: "tenant-1"},
		Changes: audit.ChangeSetInput{NoChange: true},
	})
	store, _ := memory.New(memory.Config{MaxRecords: 1, MaxBytes: 1 << 20, MaxBatchRecords: 1})
	redactor, _ := audit.NewRedactor(audit.RedactionRules{})
	record, _ = redactor.Redact(context.Background(), record)
	_, _ = store.Append(context.Background(), record)
	tenant, _ := audit.Tenant("tenant-1")
	query, _ := audit.NewQuery(audit.QueryInput{Tenant: tenant, Limit: 1})
	err := store.Export(context.Background(), query, func(record audit.Record) error {
		fmt.Println(record.ID(), record.Action())
		return nil
	})
	fmt.Println(err)
	// Output:
	// record-1 invoice.viewed
	// <nil>
}
