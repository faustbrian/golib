package golib_test

import (
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/cloudevents"
	golib "github.com/faustbrian/golib/pkg/cloudevents/adapters/golib"
	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/tenancy"
	"github.com/faustbrian/golib/pkg/workflow"
)

func TestEventSourcingAdapterRejectsEveryUnrepresentableBoundary(t *testing.T) {
	t.Parallel()

	if _, _, _, err := golib.EventSourcingToCloudEvent(eventsourcing.Message{}, golib.EventSourcingOptions{Source: "/source"}); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("zero source message error = %v", err)
	}
	invalidJSON := eventSourcingMessage(t, "application/json", []byte("{"), false)
	if _, _, _, err := golib.EventSourcingToCloudEvent(invalidJSON, golib.EventSourcingOptions{Source: "/source"}); err == nil {
		t.Fatal("invalid JSON source payload error = nil")
	}
	message := eventSourcingMessage(t, "application/octet-stream", []byte("body"), true)
	invalidTenantMessage := eventSourcingMessage(t, "application/octet-stream", []byte("body"), false)
	invalidTenantMessage = eventSourcingMessageWithTenant(t, invalidTenantMessage, "bad?tenant")
	if _, _, _, err := golib.EventSourcingToCloudEvent(invalidTenantMessage, golib.EventSourcingOptions{Source: "/source"}); !errors.Is(err, golib.ErrInvalidAdapterInput) || !errors.Is(err, tenancy.ErrInvalidTenantID) {
		t.Fatalf("invalid outbound tenant error = %v", err)
	}
	if _, _, _, err := golib.EventSourcingToCloudEvent(message, golib.EventSourcingOptions{Source: "bad\nsource"}); err == nil {
		t.Fatal("invalid CloudEvents source error = nil")
	}

	event, state, _, err := golib.EventSourcingToCloudEvent(message, golib.EventSourcingOptions{Source: "/source"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := golib.CloudEventToEventSourcing(cloudevents.Event{}, state); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("invalid target event error = %v", err)
	}
	if _, _, err := golib.CloudEventToEventSourcing(eventForEnvelope(t, event.ID(), event.Type(), "other", "application/octet-stream", event.Extensions(), cloudevents.NewBinaryData([]byte("body"))), state); !errors.Is(err, golib.ErrMetadataCollision) {
		t.Fatalf("subject collision error = %v", err)
	}
	withoutSchema := eventForEnvelope(t, event.ID(), event.Type(), eventSubject(event), "application/octet-stream", nil, cloudevents.NewBinaryData([]byte("body")))
	if _, _, err := golib.CloudEventToEventSourcing(withoutSchema, state); !errors.Is(err, golib.ErrMetadataCollision) {
		t.Fatalf("missing event schema error = %v", err)
	}

	for _, test := range []struct {
		name   string
		mutate func(*golib.EventSourcingState)
	}{
		{"correlation", func(value *golib.EventSourcingState) { value.CorrelationID = "different" }},
		{"causation", func(value *golib.EventSourcingState) { value.CausationID = "different" }},
		{"tenant", func(value *golib.EventSourcingState) { value.Tenant = "different" }},
		{"partition", func(value *golib.EventSourcingState) { value.Partition = "different" }},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			changed := state
			test.mutate(&changed)
			if _, _, err := golib.CloudEventToEventSourcing(event, changed); !errors.Is(err, golib.ErrMetadataCollision) {
				t.Fatalf("metadata collision error = %v", err)
			}
		})
	}

	withoutData := eventForEnvelope(t, event.ID(), event.Type(), eventSubject(event), "application/octet-stream", event.Extensions(), cloudevents.Data{})
	if _, _, err := golib.CloudEventToEventSourcing(withoutData, state); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("absent event data error = %v", err)
	}
	invalidTenant, _ := cloudevents.NewStringAttribute("bad?tenant")
	invalidTenantEvent := eventForEnvelope(
		t, event.ID(), event.Type(), eventSubject(event), "application/octet-stream",
		map[string]cloudevents.Attribute{"eventschema": event.Extensions()["eventschema"], "tenantid": invalidTenant},
		cloudevents.NewBinaryData([]byte("body")),
	)
	invalidTenantState := state
	invalidTenantState.Tenant = ""
	if _, _, err := golib.CloudEventToEventSourcing(invalidTenantEvent, invalidTenantState); !errors.Is(err, golib.ErrInvalidAdapterInput) || !errors.Is(err, tenancy.ErrInvalidTenantID) {
		t.Fatalf("invalid inbound tenant error = %v", err)
	}
	emptyData := eventForEnvelope(t, event.ID(), event.Type(), eventSubject(event), "application/octet-stream", event.Extensions(), cloudevents.NewBinaryData(nil))
	if _, _, err := golib.CloudEventToEventSourcing(emptyData, state); err == nil {
		t.Fatal("empty persisted payload error = nil")
	}
	badID := eventForEnvelope(t, "bad id", event.Type(), eventSubject(event), "application/octet-stream", event.Extensions(), cloudevents.NewBinaryData([]byte("body")))
	if _, _, err := golib.CloudEventToEventSourcing(badID, state); err == nil {
		t.Fatal("invalid persisted message ID error = nil")
	}
}

func TestOutboxAdapterRejectsEveryInvalidBoundary(t *testing.T) {
	t.Parallel()

	envelope := outbox.Envelope{ID: "outbox-1", Topic: "topic", Payload: []byte("body"), PayloadVersion: 1}
	if _, _, _, err := golib.OutboxToCloudEvent(outbox.Envelope{}, golib.OutboxOptions{Source: "/source", Type: "type"}); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("invalid source envelope error = %v", err)
	}
	if _, _, _, err := golib.OutboxToCloudEvent(envelope, golib.OutboxOptions{Source: "/source", Type: "type", DataContentType: ";"}); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("invalid source content type error = %v", err)
	}
	if _, _, _, err := golib.OutboxToCloudEvent(envelope, golib.OutboxOptions{Source: "bad\nsource", Type: "type"}); err == nil {
		t.Fatal("invalid source event error = nil")
	}
	if _, _, err := golib.CloudEventToOutbox(cloudevents.Event{}, envelope); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("invalid target event error = %v", err)
	}
	event := eventForEnvelope(t, "event-1", "type", "", "", nil, cloudevents.NewBinaryData([]byte("body")))
	colliding := envelope
	colliding.ID = "other"
	if _, _, err := golib.CloudEventToOutbox(event, colliding); !errors.Is(err, golib.ErrMetadataCollision) {
		t.Fatalf("target ID collision error = %v", err)
	}
	withoutData := eventForEnvelope(t, "event-1", "type", "", "", nil, cloudevents.Data{})
	withoutID := envelope
	withoutID.ID = ""
	if _, _, err := golib.CloudEventToOutbox(withoutData, withoutID); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("absent target payload error = %v", err)
	}
}

func TestQueueAdapterRejectsEveryInvalidBoundary(t *testing.T) {
	t.Parallel()

	valid := job.Message{Timeout: time.Second, Body: []byte("body")}
	if _, _, _, err := golib.QueueToCloudEvent(job.Message{}, golib.QueueOptions{Source: "/source", StableID: "id", Type: "type"}); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("invalid source message error = %v", err)
	}
	if _, _, _, err := golib.QueueToCloudEvent(valid, golib.QueueOptions{Source: "/source"}); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("missing source identity error = %v", err)
	}
	for name, options := range map[string]golib.QueueOptions{
		"missing stable ID": {Source: "/source", Type: "type"},
		"missing type":      {Source: "/source", StableID: "id"},
	} {
		if _, _, _, err := golib.QueueToCloudEvent(valid, options); !errors.Is(err, golib.ErrInvalidAdapterInput) {
			t.Fatalf("%s error = %v", name, err)
		}
	}
	invalidJSON := valid
	invalidJSON.Metadata = &job.Metadata{OriginalID: "id", JobType: "type", ContentType: "application/json"}
	invalidJSON.Body = []byte("{")
	if _, _, _, err := golib.QueueToCloudEvent(invalidJSON, golib.QueueOptions{Source: "/source"}); err == nil {
		t.Fatal("invalid source payload error = nil")
	}
	invalidExtension := valid
	invalidExtension.Metadata = &job.Metadata{OriginalID: "id", JobType: "type", TenantID: "bad?tenant"}
	if _, _, _, err := golib.QueueToCloudEvent(invalidExtension, golib.QueueOptions{Source: "/source"}); !errors.Is(err, golib.ErrInvalidAdapterInput) || !errors.Is(err, tenancy.ErrInvalidTenantID) {
		t.Fatalf("invalid source tenant error = %v", err)
	}
	invalidExtension.Metadata = &job.Metadata{
		OriginalID: "id", JobType: "type", Correlation: map[string]string{"correlationid": "bad\nvalue"},
	}
	if _, _, _, err := golib.QueueToCloudEvent(invalidExtension, golib.QueueOptions{Source: "/source"}); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("invalid source correlation error = %v", err)
	}
	if _, _, _, err := golib.QueueToCloudEvent(valid, golib.QueueOptions{Source: "bad\nsource", StableID: "id", Type: "type"}); err == nil {
		t.Fatal("invalid source event error = nil")
	}

	event := eventForEnvelope(t, "id", "type", "", "", nil, cloudevents.NewBinaryData([]byte("body")))
	if _, _, err := golib.CloudEventToQueue(cloudevents.Event{}, valid); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("invalid target event error = %v", err)
	}
	withoutQueueData := eventForEnvelope(t, "id", "type", "", "", nil, cloudevents.Data{})
	if _, _, err := golib.CloudEventToQueue(withoutQueueData, valid); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("absent target data error = %v", err)
	}
	state, _, err := golib.CloudEventToQueue(event, valid)
	if err != nil || state.Metadata == nil {
		t.Fatalf("nil target metadata = %#v, %v", state, err)
	}
	for _, colliding := range []job.Message{
		{Timeout: time.Second, Metadata: &job.Metadata{OriginalID: "other"}},
		{Timeout: time.Second, Metadata: &job.Metadata{JobType: "other"}},
	} {
		if _, _, err := golib.CloudEventToQueue(event, colliding); !errors.Is(err, golib.ErrMetadataCollision) {
			t.Fatalf("target identity collision error = %v", err)
		}
	}
	tenant, _ := cloudevents.NewStringAttribute("tenant-a")
	withTenant := eventForEnvelope(t, "id", "type", "", "", map[string]cloudevents.Attribute{"tenantid": tenant}, cloudevents.NewBinaryData([]byte("body")))
	if _, _, err := golib.CloudEventToQueue(withTenant, valid); !errors.Is(err, golib.ErrMetadataCollision) {
		t.Fatalf("target extension collision error = %v", err)
	}
	invalidTenant, _ := cloudevents.NewStringAttribute("bad?tenant")
	invalidTenantEvent := eventForEnvelope(t, "id", "type", "", "", map[string]cloudevents.Attribute{
		"tenantid": invalidTenant,
	}, cloudevents.NewBinaryData([]byte("body")))
	if _, _, err := golib.CloudEventToQueue(invalidTenantEvent, valid); !errors.Is(err, golib.ErrInvalidAdapterInput) || !errors.Is(err, tenancy.ErrInvalidTenantID) {
		t.Fatalf("invalid wire tenant error = %v", err)
	}
	invalidTenantState := valid
	invalidTenantState.Metadata = &job.Metadata{TenantID: "bad?tenant"}
	if _, _, err := golib.CloudEventToQueue(invalidTenantEvent, invalidTenantState); !errors.Is(err, golib.ErrInvalidAdapterInput) || !errors.Is(err, tenancy.ErrInvalidTenantID) {
		t.Fatalf("invalid replay tenant error = %v", err)
	}
}

func TestWorkflowAdapterRejectsEveryInvalidBoundary(t *testing.T) {
	t.Parallel()

	history := workflowHistory(t)
	if _, _, _, err := golib.WorkflowToCloudEvent(history, golib.WorkflowOptions{StableID: "id"}); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("missing workflow source error = %v", err)
	}
	if _, _, _, err := golib.WorkflowToCloudEvent(workflow.HistoryEvent{}, golib.WorkflowOptions{StableID: "id", Source: "/source"}); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("zero workflow history error = %v", err)
	}
	if _, _, _, err := golib.WorkflowToCloudEvent(history, golib.WorkflowOptions{StableID: "id", Source: "bad\nsource"}); err == nil {
		t.Fatal("invalid source event error = nil")
	}
	event, state, _, err := golib.WorkflowToCloudEvent(history, golib.WorkflowOptions{StableID: "id", Source: "/source"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := golib.CloudEventToWorkflow(cloudevents.Event{}, state); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("invalid target event error = %v", err)
	}
	for name, changed := range map[string]golib.WorkflowState{
		"missing stable ID": func() golib.WorkflowState { value := state; value.StableID = ""; return value }(),
		"missing sequence":  func() golib.WorkflowState { value := state; value.Sequence = 0; return value }(),
		"different ID":      func() golib.WorkflowState { value := state; value.StableID = "other"; return value }(),
	} {
		if _, _, err := golib.CloudEventToWorkflow(event, changed); !errors.Is(err, golib.ErrInvalidAdapterInput) {
			t.Fatalf("%s target state error = %v", name, err)
		}
	}
	wrongType := eventForEnvelope(t, event.ID(), "other", eventSubject(event), "", nil, event.Data())
	if _, _, err := golib.CloudEventToWorkflow(wrongType, state); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("invalid target type error = %v", err)
	}
	withoutPortableFields := eventForEnvelope(t, event.ID(), event.Type(), "", "", nil, cloudevents.Data{})
	if _, _, err := golib.CloudEventToWorkflow(withoutPortableFields, state); !errors.Is(err, golib.ErrInvalidAdapterInput) {
		t.Fatalf("missing portable fields error = %v", err)
	}
	occurredAt, _ := event.Time()
	for name, candidate := range map[string]cloudevents.Event{
		"missing subject": workflowPortableEvent(t, event.ID(), event.Type(), "", &occurredAt, event.Data()),
		"missing time":    workflowPortableEvent(t, event.ID(), event.Type(), eventSubject(event), nil, event.Data()),
		"missing data":    workflowPortableEvent(t, event.ID(), event.Type(), eventSubject(event), &occurredAt, cloudevents.Data{}),
	} {
		if _, _, err := golib.CloudEventToWorkflow(candidate, state); !errors.Is(err, golib.ErrInvalidAdapterInput) {
			t.Fatalf("%s portable field error = %v", name, err)
		}
	}
	invalidKind, err := cloudevents.NewEvent(cloudevents.Attributes{
		ID: event.ID(), Source: "/source", Type: "golib.workflow.history.0",
		Subject: eventSubject(event), Time: &occurredAt,
	}, event.Data())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := golib.CloudEventToWorkflow(invalidKind, state); err == nil {
		t.Fatal("invalid target workflow history error = nil")
	}
}

func workflowPortableEvent(t *testing.T, id, eventType, subject string, occurredAt *time.Time, data cloudevents.Data) cloudevents.Event {
	t.Helper()
	event, err := cloudevents.NewEvent(cloudevents.Attributes{
		ID: id, Source: "/source", Type: eventType, Subject: subject, Time: occurredAt,
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func eventSourcingMessage(t *testing.T, contentType string, payload []byte, metadata bool) eventsourcing.Message {
	t.Helper()
	stream, err := eventsourcing.NewStreamID("order", "1")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{Name: "order.created", Version: 1, ContentType: contentType, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	input := eventsourcing.PendingMessageInput{ID: "message-1", Stream: stream, Event: encoded, RecordedAt: time.Now()}
	if metadata {
		input.CorrelationID = "correlation-1"
		input.CausationID = "causation-1"
		input.Tenant = "tenant-a"
		input.Partition = "partition-a"
	}
	pending, err := eventsourcing.NewPendingMessage(input)
	if err != nil {
		t.Fatal(err)
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{Pending: pending, StreamVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func eventSourcingMessageWithTenant(
	t *testing.T,
	message eventsourcing.Message,
	tenant string,
) eventsourcing.Message {
	t.Helper()
	pending, err := eventsourcing.NewPendingMessage(eventsourcing.PendingMessageInput{
		ID: message.ID().String(), Stream: message.Stream(), Event: message.Event(),
		RecordedAt: message.RecordedAt(), Tenant: tenant,
	})
	if err != nil {
		t.Fatal(err)
	}
	converted, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending: pending, StreamVersion: message.StreamVersion(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return converted
}

func eventForEnvelope(t *testing.T, id, eventType, subject, contentType string, extensions map[string]cloudevents.Attribute, data cloudevents.Data) cloudevents.Event {
	t.Helper()
	event, err := cloudevents.NewEvent(cloudevents.Attributes{
		ID: id, Source: "/source", Type: eventType, Subject: subject,
		DataContentType: contentType, Extensions: extensions,
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func eventSubject(event cloudevents.Event) string {
	value, _ := event.Subject()
	return value
}

func workflowHistory(t *testing.T) workflow.HistoryEvent {
	t.Helper()
	reference, err := workflow.NewDefinitionReference("orders", "v1", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	history, err := workflow.NewHistoryEvent(workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "workflow-1", Kind: workflow.EventInstanceStarted,
		OccurredAt: time.Now(), Definition: reference, Data: []byte("body"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return history
}
