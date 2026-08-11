package golib_test

import (
	"bytes"
	"context"
	"errors"
	"mime"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
	"github.com/faustbrian/golib/pkg/cloudevents"
	golib "github.com/faustbrian/golib/pkg/cloudevents/adapters/golib"
	"github.com/faustbrian/golib/pkg/correlation"
	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/tenancy"
	"github.com/faustbrian/golib/pkg/workflow"
)

const fuzzAdapterPayloadLimit = 64 << 10

func FuzzDecodeKafka(f *testing.F) {
	f.Add([]byte("body"), "content-type", []byte(cloudevents.JSONMediaType))
	f.Add([]byte("{}"), "ce_specversion", []byte("1.0"))
	f.Fuzz(func(t *testing.T, value []byte, headerKey string, headerValue []byte) {
		if len(value) > 1<<20 || len(headerKey) > 256 || len(headerValue) > 8192 {
			t.Skip()
		}
		record := kafka.ConsumedRecord{
			Topic: "events", Value: append([]byte(nil), value...),
			Headers: []kafka.Header{{Key: headerKey, Value: append([]byte(nil), headerValue...)}},
		}
		message, _, err := golib.DecodeKafka(record, cloudevents.DefaultLimits())
		if err != nil {
			return
		}
		if err := message.Event.Validate(); err != nil {
			t.Fatalf("successful Kafka decode produced invalid event: %v", err)
		}
		before := message.Event.Data().Bytes()
		if len(record.Value) > 0 {
			record.Value[0] ^= 0xff
		}
		if !bytes.Equal(before, message.Event.Data().Bytes()) {
			t.Fatal("decoded Kafka event aliases the input record")
		}
	})
}

func FuzzQueuePayloadRoundTrip(f *testing.F) {
	f.Add([]byte("body"), "tenant-a")
	f.Add([]byte{}, "")
	f.Add([]byte("body"), "bad?tenant")
	f.Fuzz(func(t *testing.T, payload []byte, tenantValue string) {
		if len(payload) > 1<<20 || len(tenantValue) > tenancy.MaxTenantIDBytes+1 {
			t.Skip()
		}
		message := job.Message{Timeout: time.Second, Body: append([]byte(nil), payload...)}
		if tenantValue != "" {
			message.Metadata = &job.Metadata{TenantID: tenantValue}
		}
		event, state, _, err := golib.QueueToCloudEvent(message, golib.QueueOptions{
			Source: "/queue", StableID: "job-1", Type: "example.job",
		})
		if _, tenantErr := tenancy.ParseTenantID(tenantValue); tenantValue != "" && tenantErr != nil {
			if !errors.Is(err, golib.ErrInvalidAdapterInput) || !errors.Is(err, tenancy.ErrInvalidTenantID) {
				t.Fatalf("malformed tenant error = %v", err)
			}
			return
		}
		if err != nil {
			t.Fatalf("queue to CloudEvent: %v", err)
		}
		roundTrip, _, err := golib.CloudEventToQueue(event, state)
		if err != nil {
			t.Fatalf("CloudEvent to queue: %v", err)
		}
		if !bytes.Equal(roundTrip.Body, payload) {
			t.Fatal("queue payload changed during round trip")
		}
	})
}

// FuzzEventSourcingRoundTrip protects exact payload and retained-state
// reconstruction at the externally supplied event-store envelope boundary.
func FuzzEventSourcingRoundTrip(f *testing.F) {
	f.Add([]byte("body"), "tenant-a", "metadata")
	f.Add([]byte{0, 0xff, '\n'}, "", "")
	f.Add([]byte("body"), "bad?tenant", "value")
	f.Fuzz(func(t *testing.T, payload []byte, tenantValue, metadataValue string) {
		if len(payload) == 0 || len(payload) > fuzzAdapterPayloadLimit ||
			len(tenantValue) > tenancy.MaxTenantIDBytes+1 || len(metadataValue) > 1024 {
			t.Skip()
		}
		stream, err := eventsourcing.NewStreamID("order", "A-123")
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
			Name: "order.created", Version: 2, ContentType: "application/octet-stream",
			Payload: payload,
		})
		if err != nil {
			return
		}
		pending, err := eventsourcing.NewPendingMessage(eventsourcing.PendingMessageInput{
			ID: "message-1", Stream: stream, Event: encoded,
			Metadata:      map[string]string{"external": metadataValue},
			RecordedAt:    time.Date(2026, 8, 11, 1, 2, 3, 0, time.UTC),
			CorrelationID: "correlation-1", CausationID: "cause-1",
			Tenant: tenantValue, Partition: "partition-1",
		})
		if err != nil {
			return
		}
		message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
			Pending: pending, StreamVersion: 4, GlobalPosition: 9,
		})
		if err != nil {
			t.Fatal(err)
		}

		event, state, report, err := golib.EventSourcingToCloudEvent(message, golib.EventSourcingOptions{Source: "/event-store"})
		if _, tenantErr := tenancy.ParseTenantID(tenantValue); tenantValue != "" && tenantErr != nil {
			if !errors.Is(err, golib.ErrInvalidAdapterInput) || !errors.Is(err, tenancy.ErrInvalidTenantID) {
				t.Fatalf("malformed event-sourcing tenant error = %v", err)
			}
			return
		}
		if err != nil || len(report.Losses) != 0 {
			t.Fatalf("event-sourcing to CloudEvent = %#v, %v", report, err)
		}
		wantPayload := append([]byte(nil), payload...)
		if len(payload) > 0 {
			payload[0] ^= 0xff
		}
		if !bytes.Equal(event.Data().Bytes(), wantPayload) || state.Metadata["external"] != metadataValue {
			t.Fatal("event-sourcing conversion aliases caller-owned input")
		}
		roundTrip, reverseReport, err := golib.CloudEventToEventSourcing(event, state)
		if err != nil || len(reverseReport.Losses) != 1 || !roundTrip.Equal(message) {
			t.Fatalf("event-sourcing round trip = %#v, %#v, %v", roundTrip, reverseReport, err)
		}
	})
}

// FuzzOutboxRoundTrip protects byte-exact payload and relay-owned metadata
// reconstruction without treating the Golib outbox as a standard binding.
func FuzzOutboxRoundTrip(f *testing.F) {
	f.Add([]byte("body"), "metadata")
	f.Add([]byte{}, "")
	f.Add([]byte{0, 0xff, '\n'}, "\u2603")
	f.Fuzz(func(t *testing.T, payload []byte, metadataValue string) {
		if len(payload) > fuzzAdapterPayloadLimit || len(metadataValue) > 1024 {
			t.Skip()
		}
		now := time.Date(2026, 8, 11, 2, 3, 4, 0, time.UTC)
		envelope := outbox.Envelope{
			ID: "outbox-1", Topic: "orders", Payload: payload, PayloadVersion: 3,
			Metadata: map[string]string{"external": metadataValue}, OrderingKey: "tenant-a",
			IdempotencyKey: "operation-1", AvailableAt: now, CreatedAt: now,
		}
		want := append([]byte(nil), envelope.CanonicalJSON()...)
		event, state, report, err := golib.OutboxToCloudEvent(envelope, golib.OutboxOptions{
			Source: "/outbox", Type: "order.created",
		})
		if err != nil || len(report.Losses) != 0 {
			t.Fatalf("outbox to CloudEvent = %#v, %v", report, err)
		}
		if len(payload) > 0 {
			payload[0] ^= 0xff
		}
		envelope.Metadata["external"] = "changed"
		if bytes.Equal(event.Data().Bytes(), payload) && len(payload) > 0 {
			t.Fatal("outbox event data aliases caller-owned payload")
		}
		roundTrip, reverseReport, err := golib.CloudEventToOutbox(event, state)
		if err != nil || len(reverseReport.Losses) != 2 || !bytes.Equal(roundTrip.CanonicalJSON(), want) {
			t.Fatalf("outbox round trip = %#v, %#v, %v", roundTrip, reverseReport, err)
		}
	})
}

// FuzzWorkflowRoundTrip protects durable workflow history fields and payload
// bytes across the CloudEvents conversion boundary.
func FuzzWorkflowRoundTrip(f *testing.F) {
	reference, err := workflow.NewDefinitionReference(
		"orders", "v1", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	)
	if err != nil {
		f.Fatal(err)
	}
	f.Add([]byte(`{"order":"A-123"}`))
	f.Add([]byte{})
	f.Add([]byte{0, 0xff, '\n'})
	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > fuzzAdapterPayloadLimit {
			t.Skip()
		}
		history, err := workflow.NewHistoryEvent(workflow.HistoryEventSpec{
			Sequence: 1, InstanceID: "workflow-1", Kind: workflow.EventInstanceStarted,
			OccurredAt: time.Date(2026, 8, 11, 3, 4, 5, 0, time.UTC), Definition: reference,
			Data: payload,
		})
		if err != nil {
			t.Fatal(err)
		}
		event, state, report, err := golib.WorkflowToCloudEvent(history, golib.WorkflowOptions{
			StableID: "workflow-event-1", Source: "/workflow",
		})
		if err != nil || len(report.Losses) != 0 {
			t.Fatalf("workflow to CloudEvent = %#v, %v", report, err)
		}
		wantPayload := history.Data()
		if len(payload) > 0 {
			payload[0] ^= 0xff
		}
		if !bytes.Equal(event.Data().Bytes(), wantPayload) {
			t.Fatal("workflow event data aliases caller-owned payload")
		}
		roundTrip, reverseReport, err := golib.CloudEventToWorkflow(event, state)
		if err != nil || len(reverseReport.Losses) != 1 ||
			roundTrip.Sequence() != history.Sequence() || roundTrip.InstanceID() != history.InstanceID() ||
			roundTrip.Kind() != history.Kind() || !roundTrip.OccurredAt().Equal(history.OccurredAt()) ||
			roundTrip.Definition() != history.Definition() || !bytes.Equal(roundTrip.Data(), wantPayload) {
			t.Fatalf("workflow round trip = %#v, %#v, %v", roundTrip, reverseReport, err)
		}
	})
}

// FuzzAuditCorrelationTenantMetadata protects trusted extraction, rejection of
// malformed identity metadata, and the deliberately loss-aware audit subset.
func FuzzAuditCorrelationTenantMetadata(f *testing.F) {
	f.Add("order.create", "correlation-1", "cause-1", "tenant-a")
	f.Add("order.read", "bad.value", "cause-1", "bad?tenant")
	f.Add("\u2603", "", "", "")
	f.Fuzz(func(t *testing.T, action, correlationValue, causationValue, tenantValue string) {
		if len(action) > 1024 || len(correlationValue) > 1025 || len(causationValue) > 1025 ||
			len(tenantValue) > tenancy.MaxTenantIDBytes+1 {
			t.Skip()
		}
		now := time.Date(2026, 8, 11, 4, 5, 6, 0, time.UTC)
		builder, err := audit.NewBuilder(audit.BuilderConfig{
			Clock:       func() time.Time { return now },
			IDGenerator: func() (string, error) { return "audit-1", nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		record, err := builder.Build(audit.RecordInput{
			OccurredAt: now, Action: action, Outcome: audit.OutcomeSucceeded,
			Actor:   audit.ActorInput{Kind: audit.ActorService, ID: "orders"},
			Subject: audit.SubjectInput{Type: "order", ID: "A-123"},
			Context: audit.ContextInput{
				TenantID: tenantValue, CorrelationID: correlationValue, CausationID: causationValue,
			},
			Changes: audit.ChangeSetInput{NoChange: true},
			Policy:  audit.PolicyMetadata{PolicyID: "audit", Version: "v1"},
		})
		if err != nil {
			return
		}
		converted, report, err := golib.AddAuditMetadata(fuzzBaseEvent(t), record)
		if _, tenantErr := tenancy.ParseTenantID(tenantValue); tenantValue != "" && tenantErr != nil {
			if !errors.Is(err, golib.ErrInvalidAdapterInput) || !errors.Is(err, tenancy.ErrInvalidTenantID) {
				t.Fatalf("malformed audit tenant error = %v", err)
			}
			return
		}
		if err != nil {
			return // Invalid CloudEvents string attributes are a rejected boundary.
		}
		if len(report.Losses) != 19 {
			t.Fatalf("audit loss report = %#v", report)
		}
		if _, err := golib.ExtractAuditMetadata(converted, false); !errors.Is(err, golib.ErrUntrustedMetadata) {
			t.Fatalf("untrusted audit metadata error = %v", err)
		}
		_, correlationErr := correlation.ParseCorrelationID(correlationValue, correlation.Policy{})
		_, causationErr := correlation.ParseCausationID(causationValue, correlation.Policy{})
		metadata, err := golib.ExtractAuditMetadata(converted, true)
		if (correlationValue != "" && correlationErr != nil) || (causationValue != "" && causationErr != nil) {
			if !errors.Is(err, golib.ErrInvalidAdapterInput) {
				t.Fatalf("malformed audit correlation error = %v", err)
			}
			return
		}
		if err != nil || metadata.RecordID != "audit-1" || metadata.Action != action ||
			metadata.Outcome != audit.OutcomeSucceeded || metadata.Tenant.Value() != tenantValue ||
			metadata.Correlation.CorrelationID.String() != correlationValue ||
			metadata.Correlation.CausationID.String() != causationValue {
			t.Fatalf("audit metadata = %#v, %v", metadata, err)
		}
	})
}

// FuzzDirectJSONSchemaValidation protects the explicit, provider-free schema
// boundary and checks that validation failures cannot be mistaken for mapping
// failures or successful conformance.
func FuzzDirectJSONSchemaValidation(f *testing.F) {
	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		f.Fatal(err)
	}
	compiled, err := compiler.Compile(context.Background(), []byte(
		`{"type":"object","required":["id"],"properties":{"id":{"type":"string"}},"additionalProperties":false}`,
	))
	if err != nil {
		f.Fatal(err)
	}
	const schemaURI = "https://schemas.example/order.json"
	validator := golib.JSONSchemaValidator{URI: schemaURI, Schema: compiled}
	f.Add([]byte(`{"id":"A-123"}`), schemaURI, "application/json")
	f.Add([]byte(`{}`), schemaURI, "application/json; charset=utf-8")
	f.Add([]byte{0xff}, "https://schemas.example/other.json", "application/problem+json")
	f.Fuzz(func(t *testing.T, payload []byte, uri, contentType string) {
		if len(payload) > fuzzAdapterPayloadLimit || len(uri) > 2048 || len(contentType) > 1024 {
			t.Skip()
		}
		err := validator.Validate(context.Background(), uri, contentType, payload)
		if uri != schemaURI || !fuzzJSONContentType(contentType) {
			if !errors.Is(err, golib.ErrSchemaMapping) {
				t.Fatalf("schema mapping error = %v", err)
			}
			return
		}
		result, validationErr := compiled.Validate(context.Background(), payload)
		switch {
		case validationErr != nil:
			if err == nil || errors.Is(err, golib.ErrSchemaMapping) || errors.Is(err, golib.ErrSchemaViolation) {
				t.Fatalf("schema parse/limit error = %v, direct = %v", err, validationErr)
			}
		case result.Valid:
			if err != nil {
				t.Fatalf("valid schema instance error = %v", err)
			}
		default:
			if !errors.Is(err, golib.ErrSchemaViolation) {
				t.Fatalf("schema violation error = %v", err)
			}
		}
	})
}

func fuzzJSONContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

func fuzzBaseEvent(t *testing.T) cloudevents.Event {
	t.Helper()
	event, err := cloudevents.NewEvent(cloudevents.Attributes{
		ID: "event-1", Source: "/fuzz", Type: "example.event",
	}, cloudevents.NewBinaryData([]byte("body")))
	if err != nil {
		t.Fatal(err)
	}
	return event
}
