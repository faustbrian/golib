package golib_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/cloudevents"
	golib "github.com/faustbrian/golib/pkg/cloudevents/adapters/golib"
	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/workflow"
)

func TestEventSourcingMappingPreservesCanonicalEnvelopeOutOfBand(t *testing.T) {
	t.Parallel()

	recordedAt := time.Date(2026, 8, 9, 1, 2, 3, 0, time.UTC)
	occurredAt := recordedAt.Add(-time.Minute)
	stream, err := eventsourcing.NewStreamID("order", "A/123")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name: "order.created", Version: 2, ContentType: "application/json",
		Payload: []byte(`{"order":"A/123"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := eventsourcing.NewPendingMessage(eventsourcing.PendingMessageInput{
		ID: "message-1", Stream: stream, Event: encoded, RecordedAt: recordedAt,
		Metadata: map[string]string{"owner": "domain"}, CorrelationID: "correlation-1",
		CausationID: "cause-1", Tenant: "tenant-a", Partition: "tenant-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending: pending, StreamVersion: 4, GlobalPosition: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	event, state, report, err := golib.EventSourcingToCloudEvent(message, golib.EventSourcingOptions{
		Source: "/event-store/orders", DataSchema: "https://schemas.example/order-created-v2.json",
		OccurredAt: &occurredAt,
	})
	if err != nil || len(report.Losses) != 0 {
		t.Fatalf("to CloudEvent = %#v, %v", report, err)
	}
	if event.ID() != "message-1" || event.Type() != "order.created" || string(event.Data().Bytes()) != `{"order":"A/123"}` {
		t.Fatalf("CloudEvent = %#v", event)
	}
	roundTrip, reverseReport, err := golib.CloudEventToEventSourcing(event, state)
	if err != nil || len(reverseReport.Losses) != 3 || !roundTrip.Equal(message) {
		t.Fatalf("event-sourcing round trip = %#v, %#v, %v", roundTrip, reverseReport, err)
	}
}

func TestGolibOutboxAndQueueMappingsRetainUnrepresentableState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 2, 3, 4, 0, time.UTC)
	envelope := outbox.Envelope{
		ID: "outbox-1", Topic: "orders", Payload: []byte(`{"ok":true}`), PayloadVersion: 3,
		Metadata: map[string]string{"content-owner": "application"}, OrderingKey: "tenant-a",
		IdempotencyKey: "operation-1", AvailableAt: now, CreatedAt: now,
	}
	event, state, report, err := golib.OutboxToCloudEvent(envelope, golib.OutboxOptions{
		Source: "/outbox/orders", Type: "order.created", DataContentType: "application/json",
	})
	if err != nil || len(report.Losses) != 0 {
		t.Fatalf("outbox mapping = %#v, %v", report, err)
	}
	roundTrip, reverseReport, err := golib.CloudEventToOutbox(event, state)
	if err != nil || len(reverseReport.Losses) != 3 || !bytes.Equal(roundTrip.CanonicalJSON(), envelope.CanonicalJSON()) {
		t.Fatalf("outbox round trip = %#v, %#v, %v", roundTrip, reverseReport, err)
	}

	queueMessage := job.Message{
		Timeout: time.Minute, Body: []byte("payload"), RetryCount: 2, RetryDelay: time.Second,
		Metadata: &job.Metadata{OriginalID: "job-1", JobType: "order.notify", ContentType: "text/plain", TenantID: "tenant-a"},
	}
	queueEvent, queueState, queueReport, err := golib.QueueToCloudEvent(queueMessage, golib.QueueOptions{Source: "/queue/jobs"})
	if err != nil || len(queueReport.Losses) != 0 {
		t.Fatalf("queue mapping = %#v, %v", queueReport, err)
	}
	queueRoundTrip, queueReverseReport, err := golib.CloudEventToQueue(queueEvent, queueState)
	if err != nil || len(queueReverseReport.Losses) != 1 || !bytes.Equal(queueRoundTrip.Body, queueMessage.Body) ||
		queueRoundTrip.RetryCount != queueMessage.RetryCount || queueRoundTrip.Metadata.OriginalID != "job-1" {
		t.Fatalf("queue round trip = %#v, %#v, %v", queueRoundTrip, queueReverseReport, err)
	}
}

func TestWorkflowMappingRequiresCallerOwnedStableID(t *testing.T) {
	t.Parallel()

	reference, err := workflow.NewDefinitionReference("orders", "v1", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	history, err := workflow.NewHistoryEvent(workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "workflow-1", Kind: workflow.EventInstanceStarted,
		OccurredAt: time.Date(2026, 8, 9, 3, 4, 5, 0, time.UTC), Definition: reference,
		Data: []byte(`{"order":"A-123"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := golib.WorkflowToCloudEvent(history, golib.WorkflowOptions{Source: "/workflow"}); err == nil {
		t.Fatal("missing stable workflow event ID error = nil")
	}
	event, state, report, err := golib.WorkflowToCloudEvent(history, golib.WorkflowOptions{
		StableID: "workflow-event-1", Source: "/workflow",
	})
	if err != nil || len(report.Losses) != 0 {
		t.Fatalf("workflow mapping = %#v, %v", report, err)
	}
	roundTrip, reverseReport, err := golib.CloudEventToWorkflow(event, state)
	if err != nil || len(reverseReport.Losses) != 1 || roundTrip.Sequence() != history.Sequence() ||
		roundTrip.InstanceID() != history.InstanceID() || !bytes.Equal(roundTrip.Data(), history.Data()) {
		t.Fatalf("workflow round trip = %#v, %#v, %v", roundTrip, reverseReport, err)
	}
}

var _ = cloudevents.JSONMediaType
