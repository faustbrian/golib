package audit_test

import (
	"context"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
)

func FuzzCanonicalRecord(f *testing.F) {
	record := mustFuzzRecord(f)
	encoded, err := audit.CanonicalJSON(record)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(encoded)
	f.Add([]byte(`{"schema_version":1}`))
	f.Add([]byte{0xff, 0x00, '{'})
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > audit.DefaultLimits().MaxRecordBytes+1 {
			t.Skip()
		}
		decoded, err := audit.ParseCanonicalJSON(input, audit.DefaultLimits())
		if err != nil {
			return
		}
		roundTrip, err := audit.CanonicalJSON(decoded)
		if err != nil {
			t.Fatal(err)
		}
		second, err := audit.ParseCanonicalJSON(roundTrip, audit.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		stable, err := audit.CanonicalJSON(second)
		if err != nil {
			t.Fatal(err)
		}
		if string(roundTrip) != string(stable) {
			t.Fatal("canonical encoding was not stable")
		}
	})
}

func FuzzHostileRecordConstruction(f *testing.F) {
	f.Add("invoice.viewed", "actor-1", "invoice", "invoice-1", "app.safe", "value")
	f.Add("request_body", "", "", "", "authorization", "Bearer secret")
	f.Fuzz(func(t *testing.T, action, actorID, subjectType, subjectID, key, value string) {
		limits := audit.DefaultLimits()
		for _, candidate := range []string{action, actorID, subjectType, subjectID, key, value} {
			if len(candidate) > limits.MaxRecordBytes+1 {
				t.Skip()
			}
		}
		now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
		builder, err := audit.NewBuilder(audit.BuilderConfig{
			Clock:       func() time.Time { return now },
			IDGenerator: func() (string, error) { return "fuzz-hostile-record", nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		record, err := builder.Build(audit.RecordInput{
			OccurredAt: now, Action: action, Outcome: audit.OutcomeSucceeded,
			Actor:   audit.ActorInput{Kind: audit.ActorHuman, ID: actorID},
			Subject: audit.SubjectInput{Type: subjectType, ID: subjectID},
			Changes: audit.ChangeSetInput{NoChange: true}, Attributes: map[string]string{key: value},
		})
		if err != nil {
			return
		}
		redactor, err := audit.NewRedactor(audit.RedactionRules{AllowedAttributes: []string{key}})
		if err != nil {
			t.Fatal(err)
		}
		record, err = redactor.Redact(context.Background(), record)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := audit.CanonicalJSON(record)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := audit.ParseCanonicalJSON(encoded, limits); err != nil {
			t.Fatal(err)
		}
	})
}

func FuzzCursor(f *testing.F) {
	f.Add("")
	f.Add("djEKMjAyNi0wOC0wOVQxMjowMDowMFoKcmVjb3JkLTE")
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 8*audit.DefaultLimits().MaxFieldBytes {
			t.Skip()
		}
		cursor, err := audit.ParseCursor(input)
		if err != nil {
			return
		}
		parsed, err := audit.ParseCursor(cursor.String())
		if err != nil {
			t.Fatal(err)
		}
		if parsed.RecordID() != cursor.RecordID() || !parsed.RecordedAt().Equal(cursor.RecordedAt()) {
			t.Fatal("cursor round trip changed position")
		}
	})
}

func mustFuzzRecord(f *testing.F) audit.Record {
	f.Helper()
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	builder, err := audit.NewBuilder(audit.BuilderConfig{Clock: func() time.Time { return now }, IDGenerator: func() (string, error) { return "fuzz-record", nil }})
	if err != nil {
		f.Fatal(err)
	}
	record, err := builder.Build(audit.RecordInput{
		OccurredAt: now, Action: "account.viewed", Outcome: audit.OutcomeSucceeded,
		Actor:   audit.ActorInput{Kind: audit.ActorSystem, ID: "fuzzer"},
		Subject: audit.SubjectInput{Type: "account", ID: "account-1"},
		Changes: audit.ChangeSetInput{NoChange: true},
	})
	if err != nil {
		f.Fatal(err)
	}
	return record
}
