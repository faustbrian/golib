package audit_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
	"github.com/faustbrian/golib/pkg/audit/memory"
)

func TestRecorderRedactsBeforePersistence(t *testing.T) {
	t.Parallel()

	sink, err := memory.New(memory.Config{MaxRecords: 2, MaxBytes: 1 << 20, MaxBatchRecords: 2})
	if err != nil {
		t.Fatal(err)
	}
	redactor, err := audit.NewRedactor(audit.RedactionRules{
		DropDescription:   true,
		DropNetworkOrigin: true,
		DropUserAgent:     true,
		AllowedAttributes: []string{"app.safe"},
		AllowedChanges:    []string{"status"},
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewRecorder(audit.RecorderConfig{
		Sink:     sink,
		Redactor: redactor,
		Mode:     audit.DeliveryFailClosed,
	})
	if err != nil {
		t.Fatal(err)
	}

	record := deliveryRecord(t, "delivery-record")
	result, err := recorder.Submit(context.Background(), record)
	if err != nil || result.Disposition != audit.DeliveryPersisted {
		t.Fatalf("Submit() = %#v, %v", result, err)
	}
	query, err := audit.NewQuery(audit.QueryInput{Tenant: audit.NoTenant(), Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	page, err := sink.Query(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 {
		t.Fatalf("persisted records = %d", len(page.Records))
	}
	persisted := page.Records[0]
	if persisted.Description() != "" || persisted.Context().NetworkOrigin() != "" ||
		persisted.Context().UserAgent() != "" || persisted.Attributes()["app.safe"] != "kept" ||
		len(persisted.Attributes()) != 1 || persisted.Changes().After()["status"] != "paid" ||
		len(persisted.Changes().After()) != 1 {
		t.Fatalf("persisted record was not redacted: %#v", persisted)
	}
}

func TestRecorderBuffersFailedBatchAtomically(t *testing.T) {
	t.Parallel()

	primary, err := memory.New(memory.Config{MaxRecords: 1, MaxBytes: 1 << 20, MaxBatchRecords: 2})
	if err != nil {
		t.Fatal(err)
	}
	buffer, err := memory.New(memory.Config{MaxRecords: 4, MaxBytes: 1 << 20, MaxBatchRecords: 2})
	if err != nil {
		t.Fatal(err)
	}
	redactor, err := audit.NewRedactor(audit.RedactionRules{AllowedChanges: []string{"status"}})
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewRecorder(audit.RecorderConfig{
		Sink: primary, Redactor: redactor, Mode: audit.DeliveryDurableBuffer, Buffer: buffer,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := recorder.SubmitBatch(context.Background(), []audit.Record{
		deliveryRecord(t, "delivery-1"),
		deliveryRecord(t, "delivery-2"),
	})
	if err != nil || result.Disposition != audit.DeliveryBuffered || len(result.Append.Results) != 2 {
		t.Fatalf("SubmitBatch() = %#v, %v", result, err)
	}
	query, err := audit.NewQuery(audit.QueryInput{Tenant: audit.NoTenant(), Limit: 4})
	if err != nil {
		t.Fatal(err)
	}
	page, err := buffer.Query(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 2 || page.Records[0].ID() != "delivery-1" || page.Records[1].ID() != "delivery-2" {
		t.Fatalf("buffered records = %#v", page.Records)
	}
}

func TestRedactorValidatesRulesCancellationAndExplicitNoChange(t *testing.T) {
	t.Parallel()
	var nilRedactor audit.RedactorFunc
	if _, err := nilRedactor.Redact(context.Background(), audit.Record{}); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("nil RedactorFunc error = %v", err)
	}
	var nilAlert audit.AlertFunc
	if err := nilAlert.Alert(context.Background(), audit.DeliveryAlert{}); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("nil AlertFunc error = %v", err)
	}

	for _, rules := range []audit.RedactionRules{
		{AllowedAttributes: []string{""}},
		{AllowedChanges: []string{""}},
		{AllowedAttributes: []string{string([]byte{0xff})}},
		{AllowedChanges: []string{string([]byte{0xff})}},
		{AllowedAttributes: make([]string, audit.DefaultLimits().MaxAttributeEntries+1)},
		{AllowedChanges: make([]string, audit.DefaultLimits().MaxChangeEntries+1)},
	} {
		if _, err := audit.NewRedactor(rules); !errors.Is(err, audit.ErrInvalidArgument) {
			t.Fatalf("NewRedactor(%#v) error = %v", rules, err)
		}
	}
	attributeBoundary := make([]string, audit.DefaultLimits().MaxAttributeEntries)
	changeBoundary := make([]string, audit.DefaultLimits().MaxChangeEntries)
	for index := range attributeBoundary {
		attributeBoundary[index] = fmt.Sprintf("app.attribute.%03d", index)
	}
	for index := range changeBoundary {
		changeBoundary[index] = fmt.Sprintf("field.%03d", index)
	}
	if _, err := audit.NewRedactor(audit.RedactionRules{
		AllowedAttributes: attributeBoundary,
		AllowedChanges:    changeBoundary,
	}); err != nil {
		t.Fatalf("NewRedactor(exact boundaries) error = %v", err)
	}
	redactor, err := audit.NewRedactor(audit.RedactionRules{})
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := redactor.Redact(canceled, deliveryRecord(t, "canceled-redaction")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Redact() error = %v", err)
	}
	redacted, err := redactor.Redact(context.Background(), deliveryRecord(t, "empty-changes"))
	if err != nil {
		t.Fatal(err)
	}
	if !redacted.Changes().NoChange() || len(redacted.Attributes()) != 0 {
		t.Fatalf("default-deny redaction = %#v", redacted)
	}
}

func deliveryRecord(t *testing.T, id string) audit.Record {
	t.Helper()
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	builder, err := audit.NewBuilder(audit.BuilderConfig{
		Clock:       func() time.Time { return now },
		IDGenerator: func() (string, error) { return id, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := builder.Build(audit.RecordInput{
		OccurredAt:  now,
		Action:      "invoice.paid",
		Outcome:     audit.OutcomeSucceeded,
		Description: "contains caller-provided detail",
		Actor:       audit.ActorInput{Kind: audit.ActorHuman, ID: "user-1"},
		Subject:     audit.SubjectInput{Type: "invoice", ID: "invoice-1"},
		Context: audit.ContextInput{
			NetworkOrigin: "192.0.2.1",
			UserAgent:     "client/1.0",
		},
		Changes: audit.ChangeSetInput{
			Before: map[string]string{"status": "pending", "internal_note": "private"},
			After:  map[string]string{"status": "paid", "internal_note": "private"},
		},
		Attributes: map[string]string{"app.safe": "kept", "app.private": "removed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return record
}
