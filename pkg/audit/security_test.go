package audit_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
)

type rejectingSink struct{}

func (rejectingSink) Append(context.Context, audit.Record) (audit.AppendResult, error) {
	return audit.AppendResult{}, nil
}

func (rejectingSink) AppendBatch(context.Context, []audit.Record) (audit.BatchResult, error) {
	return audit.BatchResult{}, nil
}

func TestBuilderRejectsPartialIntegrityMetadata(t *testing.T) {
	t.Parallel()

	tests := map[string]audit.IntegrityInput{
		"algorithm only": {Algorithm: audit.IntegritySHA256},
		"partition only": {Partition: "tenant-1"},
		"key only":       {KeyID: "key-1"},
		"sequence only":  {Sequence: 1},
	}
	for name, integrity := range tests {
		integrity := integrity
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := securityBuilder(t).Build(securityInput(integrity, nil))
			if !errors.Is(err, audit.ErrInvalidArgument) {
				t.Fatalf("Build() error = %v", err)
			}
		})
	}
}

func TestBuilderRejectsUnrestrictedBodyFields(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"request_body", "response.body", "raw-body", "http.request.body", "password_hint"} {
		key := key
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			_, err := securityBuilder(t).Build(securityInput(audit.IntegrityInput{}, map[string]string{key: "private payload"}))
			if !errors.Is(err, audit.ErrSensitiveData) {
				t.Fatalf("Build() error = %v", err)
			}
		})
	}
}

func TestRecorderDoesNotExposeRedactionDiagnostics(t *testing.T) {
	t.Parallel()

	cause := errors.New("request body contained password=hunter2")
	recorder, err := audit.NewRecorder(audit.RecorderConfig{
		Sink: rejectingSink{},
		Redactor: audit.RedactorFunc(func(context.Context, audit.Record) (audit.Record, error) {
			return audit.Record{}, cause
		}),
		Mode: audit.DeliveryFailClosed,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = recorder.Submit(context.Background(), mustSecurityRecord(t))
	if errors.Is(err, cause) {
		t.Fatalf("Submit() retained secret-bearing cause: %v", err)
	}
	if strings.Contains(err.Error(), "hunter2") || strings.Contains(err.Error(), "password") {
		t.Fatalf("Submit() exposed redaction diagnostic: %v", err)
	}
	if unwrapped := errors.Unwrap(err); unwrapped != nil {
		t.Fatalf("Submit() exposed unwrap diagnostic: %v", unwrapped)
	}
}

func securityBuilder(t *testing.T) *audit.Builder {
	t.Helper()
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	builder, err := audit.NewBuilder(audit.BuilderConfig{
		Clock:       func() time.Time { return now },
		IDGenerator: func() (string, error) { return "security-record", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return builder
}

func securityInput(integrity audit.IntegrityInput, attributes map[string]string) audit.RecordInput {
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	return audit.RecordInput{
		OccurredAt: now,
		Action:     "account.viewed",
		Outcome:    audit.OutcomeSucceeded,
		Actor:      audit.ActorInput{Kind: audit.ActorSystem, ID: "billing"},
		Subject:    audit.SubjectInput{Type: "account", ID: "account-1"},
		Changes:    audit.ChangeSetInput{NoChange: true},
		Attributes: attributes,
		Integrity:  integrity,
	}
}

func mustSecurityRecord(t *testing.T) audit.Record {
	t.Helper()
	record, err := securityBuilder(t).Build(securityInput(audit.IntegrityInput{}, nil))
	if err != nil {
		t.Fatal(err)
	}
	return record
}
