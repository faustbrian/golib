package audit_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
)

func TestRecordPreservesCompleteContextAndIdentitySemantics(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 12, 0, 0, 123, time.FixedZone("offset", 2*60*60))
	delegated := &audit.ActorInput{Kind: audit.ActorService, ID: "gateway", AuthenticationMethod: "mTLS"}
	builder, err := audit.NewBuilder(audit.BuilderConfig{
		Clock:       func() time.Time { return now },
		IDGenerator: func() (string, error) { return "record-complete", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := builder.Build(audit.RecordInput{
		OccurredAt: now.Add(-time.Second), Action: "invoice.deleted", Outcome: audit.OutcomeDenied,
		ReasonCode: "policy_denied", Description: "safe summary",
		Actor:   audit.ActorInput{Kind: audit.ActorHuman, ID: "user-1", AuthenticationMethod: "webauthn", DelegatedBy: delegated},
		Subject: audit.SubjectInput{Type: "invoice", ID: "invoice-1", Deleted: true},
		Context: audit.ContextInput{
			TenantID: "tenant-1", CorrelationID: "correlation-1", CausationID: "causation-1",
			RequestID: "request-1", TraceID: "trace-1", IdempotencyID: "idempotency-1",
			SourceService: "billing", SourceVersion: "v1.2.3", Environment: "production",
			NetworkOrigin: "192.0.2.1", UserAgent: "client/1.0",
		},
		Changes:    audit.ChangeSetInput{Before: map[string]string{"status": "open"}, After: map[string]string{"status": "deleted"}},
		Policy:     audit.PolicyMetadata{PolicyID: "audit-policy", Version: "2026-08"},
		Attributes: map[string]string{"app.channel": "api"},
	})
	if err != nil {
		t.Fatal(err)
	}
	actor, subject, ctx := record.Actor(), record.Subject(), record.Context()
	if record.ID() != "record-complete" || record.Action() != "invoice.deleted" || record.Outcome() != audit.OutcomeDenied ||
		record.ReasonCode() != "policy_denied" || record.Description() != "safe summary" ||
		actor.Kind() != audit.ActorHuman || actor.ID() != "user-1" || actor.AuthenticationMethod() != "webauthn" ||
		actor.DelegatedBy().Kind() != audit.ActorService || actor.DelegatedBy().ID() != "gateway" ||
		subject.Type() != "invoice" || subject.ID() != "invoice-1" || !subject.Deleted() ||
		ctx.TenantID() != "tenant-1" || ctx.CorrelationID() != "correlation-1" || ctx.CausationID() != "causation-1" ||
		ctx.RequestID() != "request-1" || ctx.TraceID() != "trace-1" || ctx.IdempotencyID() != "idempotency-1" ||
		ctx.SourceService() != "billing" || ctx.SourceVersion() != "v1.2.3" || ctx.Environment() != "production" ||
		ctx.NetworkOrigin() != "192.0.2.1" || ctx.UserAgent() != "client/1.0" ||
		record.Changes().NoChange() || record.Policy().PolicyID != "audit-policy" || record.Policy().Version != "2026-08" ||
		record.Integrity().Enabled() {
		t.Fatalf("complete record contract was not preserved: %#v", record)
	}
	if (audit.Actor{}).DelegatedBy().Kind() != 0 {
		t.Fatal("zero delegated actor was not explicit")
	}
	if record.OccurredAt().Location() != time.UTC || record.RecordedAt().Location() != time.UTC {
		t.Fatal("record times were not canonicalized to UTC")
	}
}

func TestBuilderValidationRejectsAmbiguousOrOversizedRecords(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	base := securityInput(audit.IntegrityInput{}, nil)
	tests := map[string]func(*audit.RecordInput){
		"missing occurrence": func(value *audit.RecordInput) { value.OccurredAt = time.Time{} },
		"missing action":     func(value *audit.RecordInput) { value.Action = "" },
		"unknown outcome":    func(value *audit.RecordInput) { value.Outcome = audit.Outcome(99) },
		"unknown actor":      func(value *audit.RecordInput) { value.Actor.Kind = 0 },
		"human without ID":   func(value *audit.RecordInput) { value.Actor = audit.ActorInput{Kind: audit.ActorHuman} },
		"system without ID":  func(value *audit.RecordInput) { value.Actor = audit.ActorInput{Kind: audit.ActorSystem} },
		"anonymous with ID": func(value *audit.RecordInput) {
			value.Actor = audit.ActorInput{Kind: audit.ActorAnonymous, ID: "unexpected"}
		},
		"missing subject type": func(value *audit.RecordInput) { value.Subject.Type = "" },
		"missing subject ID":   func(value *audit.RecordInput) { value.Subject.ID = "" },
		"invalid UTF-8 field": func(value *audit.RecordInput) {
			value.Subject.Type = string([]byte{0xff})
		},
		"ambiguous no change": func(value *audit.RecordInput) { value.Changes = audit.ChangeSetInput{} },
		"contradictory no change": func(value *audit.RecordInput) {
			value.Changes = audit.ChangeSetInput{NoChange: true, After: map[string]string{"status": "paid"}}
		},
		"empty attribute key": func(value *audit.RecordInput) { value.Attributes = map[string]string{"": "value"} },
		"invalid UTF-8 attribute key": func(value *audit.RecordInput) {
			value.Attributes = map[string]string{string([]byte{0xff}): "value"}
		},
		"invalid UTF-8 attribute value": func(value *audit.RecordInput) {
			value.Attributes = map[string]string{"app.safe": string([]byte{0xff})}
		},
		"reserved attribute": func(value *audit.RecordInput) { value.Attributes = map[string]string{"audit.internal": "value"} },
		"nested delegation": func(value *audit.RecordInput) {
			value.Actor.DelegatedBy = &audit.ActorInput{Kind: audit.ActorSystem, ID: "one", DelegatedBy: &audit.ActorInput{Kind: audit.ActorSystem, ID: "two"}}
		},
	}
	for name, mutate := range tests {
		mutate := mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input := base
			mutate(&input)
			_, err := securityBuilder(t).Build(input)
			if !errors.Is(err, audit.ErrInvalidArgument) {
				t.Fatalf("Build() error = %v", err)
			}
		})
	}

	if builder, err := audit.NewBuilder(audit.BuilderConfig{Clock: func() time.Time { return now }, IDGenerator: func() (string, error) { return "", nil }}); err != nil {
		t.Fatal(err)
	} else if _, err := builder.Build(base); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("empty generated ID error = %v", err)
	}
	generationFailure := errors.New("entropy unavailable")
	if builder, err := audit.NewBuilder(audit.BuilderConfig{Clock: func() time.Time { return now }, IDGenerator: func() (string, error) { return "", generationFailure }}); err != nil {
		t.Fatal(err)
	} else if _, err := builder.Build(base); !errors.Is(err, generationFailure) {
		t.Fatalf("ID generation error = %v", err)
	}
	var nilBuilder *audit.Builder
	if _, err := nilBuilder.Build(base); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("nil Builder.Build() error = %v", err)
	}
}

func TestBuilderCanonicalizesDurableTimestampPrecisionAndRejectsUnencodableYears(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 12, 0, 0, 123456789, time.FixedZone("source", 2*60*60))
	builder, err := audit.NewBuilder(audit.BuilderConfig{
		Clock:       func() time.Time { return now },
		IDGenerator: func() (string, error) { return "precision-record", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	input := securityInput(audit.IntegrityInput{}, nil)
	input.OccurredAt = now
	record, err := builder.Build(input)
	if err != nil {
		t.Fatal(err)
	}
	want := now.UTC().Truncate(time.Microsecond)
	if !record.OccurredAt().Equal(want) || !record.RecordedAt().Equal(want) {
		t.Fatalf("canonical times = %s/%s, want %s", record.OccurredAt(), record.RecordedAt(), want)
	}

	for _, invalidTime := range []time.Time{
		time.Date(-1, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC),
	} {
		input.OccurredAt = invalidTime
		if _, err := builder.Build(input); !errors.Is(err, audit.ErrInvalidArgument) {
			t.Fatalf("Build(%s) error = %v", invalidTime, err)
		}
	}
	for _, boundaryTime := range []time.Time{
		time.Date(0, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(9999, time.December, 31, 23, 59, 59, 999999000, time.UTC),
	} {
		input.OccurredAt = boundaryTime
		if _, err := builder.Build(input); err != nil {
			t.Fatalf("Build(%s) boundary error = %v", boundaryTime, err)
		}
	}
}

func TestBuilderBoundsRecordIdentityConsistentlyWithDurableAdapters(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	oversizedID := strings.Repeat("r", audit.DefaultLimits().MaxFieldBytes+1)
	builder, err := audit.NewBuilder(audit.BuilderConfig{
		Clock:       func() time.Time { return now },
		IDGenerator: func() (string, error) { return oversizedID, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Build(securityInput(audit.IntegrityInput{}, nil)); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("oversized record ID Build() error = %v", err)
	}
}

func TestLimitsBoundEveryCollection(t *testing.T) {
	t.Parallel()

	limits := audit.DefaultLimits()
	for name, mutate := range map[string]func(*audit.Limits){
		"record":            func(value *audit.Limits) { value.MaxRecordBytes = 0 },
		"field":             func(value *audit.Limits) { value.MaxFieldBytes = 0 },
		"description":       func(value *audit.Limits) { value.MaxDescriptionBytes = 0 },
		"attribute entries": func(value *audit.Limits) { value.MaxAttributeEntries = 0 },
		"attribute bytes":   func(value *audit.Limits) { value.MaxAttributeBytes = 0 },
		"change entries":    func(value *audit.Limits) { value.MaxChangeEntries = 0 },
		"change bytes":      func(value *audit.Limits) { value.MaxChangeBytes = 0 },
		"integrity":         func(value *audit.Limits) { value.MaxIntegrityBytes = 0 },
	} {
		value := limits
		mutate(&value)
		if err := value.Validate(); !errors.Is(err, audit.ErrInvalidArgument) {
			t.Fatalf("%s Validate() error = %v", name, err)
		}
	}
	if err := limits.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{31, 33} {
		value := limits
		value.MaxIntegrityBytes = size
		if err := value.Validate(); !errors.Is(err, audit.ErrInvalidArgument) {
			t.Fatalf("MaxIntegrityBytes=%d Validate() error = %v", size, err)
		}
	}

	small := limits
	small.MaxFieldBytes = 8
	small.MaxDescriptionBytes = 8
	small.MaxAttributeEntries = 1
	small.MaxAttributeBytes = 8
	small.MaxChangeEntries = 1
	small.MaxChangeBytes = 8
	builder, err := audit.NewBuilder(audit.BuilderConfig{Limits: small, Clock: time.Now, IDGenerator: func() (string, error) { return "record", nil }})
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*audit.RecordInput){
		"action bytes":      func(value *audit.RecordInput) { value.Action = strings.Repeat("a", 9) },
		"description bytes": func(value *audit.RecordInput) { value.Description = strings.Repeat("d", 9) },
		"attribute entries": func(value *audit.RecordInput) { value.Attributes = map[string]string{"a": "1", "b": "2"} },
		"attribute bytes":   func(value *audit.RecordInput) { value.Attributes = map[string]string{"a": strings.Repeat("v", 8)} },
		"change entries": func(value *audit.RecordInput) {
			value.Changes = audit.ChangeSetInput{After: map[string]string{"a": "1", "b": "2"}}
		},
		"change bytes": func(value *audit.RecordInput) {
			value.Changes = audit.ChangeSetInput{After: map[string]string{"a": strings.Repeat("v", 8)}}
		},
	} {
		input := securityInput(audit.IntegrityInput{}, nil)
		input.Action = "viewed"
		input.Actor.ID = "system"
		input.Subject.Type = "account"
		input.Subject.ID = "acct-1"
		mutate(&input)
		if _, err := builder.Build(input); !errors.Is(err, audit.ErrInvalidArgument) {
			t.Fatalf("%s Build() error = %v", name, err)
		}
	}
}

func TestCanonicalDecoderRejectsHostileOrNonCanonicalRecords(t *testing.T) {
	t.Parallel()

	record := mustSecurityRecord(t)
	encoded, err := audit.CanonicalJSON(record)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := audit.ParseCanonicalJSON(encoded, audit.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	for _, hostile := range [][]byte{
		nil,
		[]byte("{"),
		append(append([]byte(nil), encoded...), []byte("{}")...),
		[]byte(`{"schema_version":2}`),
	} {
		if _, err := audit.ParseCanonicalJSON(hostile, audit.DefaultLimits()); !errors.Is(err, audit.ErrInvalidArgument) {
			t.Fatalf("ParseCanonicalJSON(%q) error = %v", hostile, err)
		}
	}

	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	mutations := []func(map[string]any){
		func(value map[string]any) { value["occurred_at"] = "not-a-time" },
		func(value map[string]any) { value["recorded_at"] = "not-a-time" },
		func(value map[string]any) { value["unknown"] = true },
		func(value map[string]any) { value["action"] = "" },
		func(value map[string]any) { value["integrity"].(map[string]any)["previous_digest"] = "not-hex" },
		func(value map[string]any) { value["integrity"].(map[string]any)["digest"] = "not-hex" },
		func(value map[string]any) {
			value["integrity"].(map[string]any)["digest"] = strings.Repeat("ab", audit.DefaultLimits().MaxIntegrityBytes+1)
		},
	}
	for index, mutate := range mutations {
		copyObject := cloneJSONMap(t, object)
		mutate(copyObject)
		candidate, err := json.Marshal(copyObject)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := audit.ParseCanonicalJSON(candidate, audit.DefaultLimits()); !errors.Is(err, audit.ErrInvalidArgument) {
			t.Fatalf("mutation %d error = %v", index, err)
		}
	}
}

func cloneJSONMap(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
