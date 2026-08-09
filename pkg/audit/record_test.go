package audit_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
)

func TestBuilderCreatesImmutableRecordWithExplicitIdentityContext(t *testing.T) {
	t.Parallel()

	recordedAt := time.Date(2026, time.August, 9, 9, 30, 0, 0, time.UTC)
	attributes := map[string]string{"app.order_source": "api"}
	before := map[string]string{"status": "pending"}
	after := map[string]string{"status": "paid"}

	builder, err := audit.NewBuilder(audit.BuilderConfig{
		Clock:       func() time.Time { return recordedAt },
		IDGenerator: func() (string, error) { return "018f4f7c-4e50-7a00-8000-000000000001", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := builder.Build(audit.RecordInput{
		OccurredAt:  recordedAt.Add(-time.Second),
		Action:      "invoice.payment_captured",
		Outcome:     audit.OutcomeSucceeded,
		ReasonCode:  "payment_confirmed",
		Description: "payment was captured",
		Actor: audit.ActorInput{
			Kind:                 audit.ActorHuman,
			ID:                   "user-42",
			AuthenticationMethod: "webauthn",
			DelegatedBy: &audit.ActorInput{
				Kind: audit.ActorService,
				ID:   "support-console",
			},
		},
		Subject: audit.SubjectInput{Type: "invoice", ID: "invoice-99"},
		Context: audit.ContextInput{
			TenantID:      "tenant-7",
			CorrelationID: "correlation-1",
			CausationID:   "command-3",
			RequestID:     "request-4",
			TraceID:       "trace-5",
			IdempotencyID: "capture-6",
			SourceService: "billing",
			SourceVersion: "2026.08.09",
			Environment:   "production",
			NetworkOrigin: "192.0.2.10",
			UserAgent:     "example-client/1.0",
		},
		Changes:    audit.ChangeSetInput{Before: before, After: after},
		Policy:     audit.PolicyMetadata{PolicyID: "audit-v2", Version: "2"},
		Attributes: attributes,
	})
	if err != nil {
		t.Fatal(err)
	}

	attributes["app.order_source"] = "mutated"
	before["status"] = "mutated"
	after["status"] = "mutated"

	if record.ID() != "018f4f7c-4e50-7a00-8000-000000000001" ||
		record.OccurredAt() != recordedAt.Add(-time.Second) ||
		record.RecordedAt() != recordedAt ||
		record.Action() != "invoice.payment_captured" ||
		record.Outcome() != audit.OutcomeSucceeded ||
		record.Actor().DelegatedBy().ID() != "support-console" ||
		record.Subject().ID() != "invoice-99" ||
		record.Context().TenantID() != "tenant-7" ||
		record.Changes().Before()["status"] != "pending" ||
		record.Changes().After()["status"] != "paid" ||
		record.Attributes()["app.order_source"] != "api" {
		t.Fatalf("record did not preserve its original immutable values: %#v", record)
	}

	returned := record.Attributes()
	returned["app.order_source"] = "caller mutation"
	if record.Attributes()["app.order_source"] != "api" {
		t.Fatal("record returned caller-owned attributes")
	}
}

func TestCanonicalEncodingDoesNotDependOnMapInsertionOrder(t *testing.T) {
	t.Parallel()

	recordedAt := time.Date(2026, time.August, 9, 9, 30, 0, 123, time.UTC)
	build := func(attributes, before, after map[string]string) audit.Record {
		t.Helper()
		builder, err := audit.NewBuilder(audit.BuilderConfig{
			Clock:       func() time.Time { return recordedAt },
			IDGenerator: func() (string, error) { return "record-1", nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		record, err := builder.Build(audit.RecordInput{
			OccurredAt: recordedAt.Add(-time.Second),
			Action:     "account.updated",
			Outcome:    audit.OutcomeSucceeded,
			Actor:      audit.ActorInput{Kind: audit.ActorSystem, ID: "billing"},
			Subject:    audit.SubjectInput{Type: "account", ID: "account-1"},
			Changes:    audit.ChangeSetInput{Before: before, After: after},
			Attributes: attributes,
		})
		if err != nil {
			t.Fatal(err)
		}
		return record
	}

	first := build(
		map[string]string{"app.z": "last", "app.a": "first"},
		map[string]string{"z": "old-z", "a": "old-a"},
		map[string]string{"z": "new-z", "a": "new-a"},
	)
	second := build(
		map[string]string{"app.a": "first", "app.z": "last"},
		map[string]string{"a": "old-a", "z": "old-z"},
		map[string]string{"a": "new-a", "z": "new-z"},
	)

	firstEncoding, err := audit.CanonicalJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	secondEncoding, err := audit.CanonicalJSON(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstEncoding, secondEncoding) {
		t.Fatalf("canonical encodings differ:\n%s\n%s", firstEncoding, secondEncoding)
	}
}

func TestCanonicalEncodingRoundTripsWithoutMutableAliases(t *testing.T) {
	t.Parallel()

	record := canonicalRecordFixture(t)
	encoded, err := audit.CanonicalJSON(record)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := audit.ParseCanonicalJSON(encoded, audit.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	reencoded, err := audit.CanonicalJSON(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, reencoded) {
		t.Fatalf("round-trip changed canonical bytes:\n%s\n%s", encoded, reencoded)
	}
	changes := decoded.Changes().After()
	changes["status"] = "mutated"
	if decoded.Changes().After()["status"] != "paid" {
		t.Fatal("decoded record exposed a mutable alias")
	}
}

func TestBuilderRejectsRecordAboveTotalCanonicalByteLimit(t *testing.T) {
	t.Parallel()
	limits := audit.DefaultLimits()
	limits.MaxRecordBytes = 128
	builder, err := audit.NewBuilder(audit.BuilderConfig{
		Limits:      limits,
		Clock:       func() time.Time { return time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC) },
		IDGenerator: func() (string, error) { return "bounded-record", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = builder.Build(audit.RecordInput{
		OccurredAt: time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC),
		Action:     "invoice.created", Outcome: audit.OutcomeSucceeded,
		Actor:   audit.ActorInput{Kind: audit.ActorSystem, ID: "billing"},
		Subject: audit.SubjectInput{Type: "invoice", ID: "invoice-1"},
		Changes: audit.ChangeSetInput{NoChange: true},
	})
	if !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("Build() error = %v", err)
	}
}

func canonicalRecordFixture(t *testing.T) audit.Record {
	t.Helper()
	now := time.Date(2026, time.August, 9, 15, 0, 0, 0, time.UTC)
	builder, err := audit.NewBuilder(audit.BuilderConfig{
		Clock:       func() time.Time { return now },
		IDGenerator: func() (string, error) { return "canonical-record", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := builder.Build(audit.RecordInput{
		OccurredAt: now.Add(-time.Second), Action: "invoice.paid", Outcome: audit.OutcomeSucceeded,
		Actor:      audit.ActorInput{Kind: audit.ActorHuman, ID: "user-1", AuthenticationMethod: "webauthn"},
		Subject:    audit.SubjectInput{Type: "invoice", ID: "invoice-1"},
		Context:    audit.ContextInput{TenantID: "tenant-1", CorrelationID: "correlation-1"},
		Changes:    audit.ChangeSetInput{Before: map[string]string{"status": "pending"}, After: map[string]string{"status": "paid"}},
		Attributes: map[string]string{"app.source": "api"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return record
}
