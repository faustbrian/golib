package golib

import (
	"encoding/base64"
	"fmt"
	"mime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/faustbrian/golib/pkg/cloudevents"
	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/tenancy"
	"github.com/faustbrian/golib/pkg/workflow"
)

const eventSchemaExtension = "eventschema"

// EventSourcingOptions owns the application boundary decisions that are not
// implied by a persisted event-sourcing message.
type EventSourcingOptions struct {
	Source     string
	DataSchema string
	OccurredAt *time.Time
}

// EventSourcingState retains canonical event-store state outside CloudEvents.
type EventSourcingState struct {
	Stream         eventsourcing.StreamID
	StreamVersion  uint64
	GlobalPosition eventsourcing.GlobalPosition
	EventVersion   eventsourcing.SchemaVersion
	Metadata       map[string]string
	RecordedAt     time.Time
	CorrelationID  string
	CausationID    string
	Tenant         string
	Partition      string
	TrustMetadata  bool
}

// EventSourcingToCloudEvent maps the domain event payload and portable context
// while returning every event-store-owned field as explicit state.
func EventSourcingToCloudEvent(
	message eventsourcing.Message,
	options EventSourcingOptions,
) (cloudevents.Event, EventSourcingState, Report, error) {
	if options.Source == "" || message.ID().IsZero() {
		return cloudevents.Event{}, EventSourcingState{}, Report{}, fmt.Errorf("%w: event-sourcing mapping", ErrInvalidAdapterInput)
	}
	encoded := message.Event()
	data, err := dataFromPayload(encoded.ContentType(), encoded.Payload())
	if err != nil {
		return cloudevents.Event{}, EventSourcingState{}, Report{}, err
	}
	extensions := map[string]cloudevents.Attribute{}
	extensions[eventSchemaExtension], _ = cloudevents.NewStringAttribute(strconv.FormatUint(uint64(encoded.Version()), 10))
	state := EventSourcingState{
		Stream: message.Stream(), StreamVersion: message.StreamVersion(), EventVersion: encoded.Version(),
		Metadata: message.Metadata(), RecordedAt: message.RecordedAt(), TrustMetadata: true,
	}
	if position, present := message.GlobalPosition(); present {
		state.GlobalPosition = position
	}
	if value, present := message.CorrelationID(); present {
		state.CorrelationID = value.String()
		extensions[correlationIDExtension], _ = cloudevents.NewStringAttribute(state.CorrelationID)
	}
	if value, present := message.CausationID(); present {
		state.CausationID = value.String()
		extensions[causationIDExtension], _ = cloudevents.NewStringAttribute(state.CausationID)
	}
	if value, present := message.Tenant(); present {
		state.Tenant, err = validatedTenant(value)
		if err != nil {
			return cloudevents.Event{}, EventSourcingState{}, Report{}, err
		}
		extensions[tenantIDExtension], _ = cloudevents.NewStringAttribute(state.Tenant)
	}
	if value, present := message.Partition(); present {
		state.Partition = value
		extensions["partitionkey"], _ = cloudevents.NewStringAttribute(value)
	}
	state.Metadata = cloneStrings(state.Metadata)
	occurredAt := cloneTimePointer(options.OccurredAt)
	event, err := cloudevents.NewEvent(cloudevents.Attributes{
		ID: message.ID().String(), Source: options.Source, Type: encoded.Name().String(),
		DataContentType: encoded.ContentType(), DataSchema: options.DataSchema,
		Subject: encodeStreamSubject(message.Stream()), Time: occurredAt, Extensions: extensions,
	}, data)
	if err != nil {
		return cloudevents.Event{}, EventSourcingState{}, Report{}, err
	}
	return event, state, Report{}, nil
}

// CloudEventToEventSourcing constructs one persisted message from portable
// CloudEvents fields and caller-retained event-store state.
func CloudEventToEventSourcing(
	event cloudevents.Event,
	state EventSourcingState,
) (eventsourcing.Message, Report, error) {
	if err := event.Validate(); err != nil || state.Stream.IsZero() || state.StreamVersion == 0 || state.EventVersion == 0 || state.RecordedAt.IsZero() {
		return eventsourcing.Message{}, Report{}, fmt.Errorf("%w: event-sourcing target", ErrInvalidAdapterInput)
	}
	if subject, present := event.Subject(); !present || subject != encodeStreamSubject(state.Stream) {
		return eventsourcing.Message{}, Report{}, fmt.Errorf("%w: subject", ErrMetadataCollision)
	}
	version, _, _ := stringExtension(event, eventSchemaExtension)
	if version != strconv.FormatUint(uint64(state.EventVersion), 10) {
		return eventsourcing.Message{}, Report{}, fmt.Errorf("%w: eventschema", ErrMetadataCollision)
	}
	correlationID, err := mappedString(event, correlationIDExtension, state.CorrelationID, state.TrustMetadata)
	if err != nil {
		return eventsourcing.Message{}, Report{}, err
	}
	causationID, err := mappedString(event, causationIDExtension, state.CausationID, state.TrustMetadata)
	if err != nil {
		return eventsourcing.Message{}, Report{}, err
	}
	tenant, err := mappedString(event, tenantIDExtension, state.Tenant, state.TrustMetadata)
	if err != nil {
		return eventsourcing.Message{}, Report{}, err
	}
	tenant, err = validatedTenant(tenant)
	if err != nil {
		return eventsourcing.Message{}, Report{}, err
	}
	partition, err := mappedString(event, "partitionkey", state.Partition, state.TrustMetadata)
	if err != nil {
		return eventsourcing.Message{}, Report{}, err
	}
	contentType, present := event.DataContentType()
	if !present || !event.Data().Present() {
		return eventsourcing.Message{}, Report{}, fmt.Errorf("%w: event data", ErrInvalidAdapterInput)
	}
	encoded, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name: event.Type(), Version: state.EventVersion, ContentType: contentType, Payload: event.Data().Bytes(),
	})
	if err != nil {
		return eventsourcing.Message{}, Report{}, err
	}
	pending, err := eventsourcing.NewPendingMessage(eventsourcing.PendingMessageInput{
		ID: event.ID(), Stream: state.Stream, Event: encoded, Metadata: cloneStrings(state.Metadata),
		RecordedAt: state.RecordedAt, CorrelationID: correlationID, CausationID: causationID,
		Tenant: tenant, Partition: partition,
	})
	if err != nil {
		return eventsourcing.Message{}, Report{}, err
	}
	message, _ := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending: pending, StreamVersion: state.StreamVersion, GlobalPosition: state.GlobalPosition,
	})
	report := Report{Losses: []Loss{{Field: "source", Reason: "not represented by event-sourcing"}}}
	appendDataKindLoss(event, &report, "event-sourcing", true)
	if _, present := event.DataSchema(); present {
		report.Losses = append(report.Losses, Loss{Field: "dataschema", Reason: "not represented by event-sourcing"})
	}
	if _, present := event.Time(); present {
		report.Losses = append(report.Losses, Loss{Field: "time", Reason: "recorded_at is not occurrence time"})
	}
	appendExtensionLosses(event, &report, "event-sourcing", map[string]struct{}{
		eventSchemaExtension: {}, correlationIDExtension: {}, causationIDExtension: {},
		tenantIDExtension: {}, "partitionkey": {},
	})
	return message, report, nil
}

// OutboxOptions owns the application mapping. Golib outbox has no official
// CloudEvents protocol binding.
type OutboxOptions struct {
	Source          string
	Type            string
	DataContentType string
	DataSchema      string
	Subject         string
	OccurredAt      *time.Time
}

// OutboxToCloudEvent performs the documented Golib outbox mapping and returns
// a deep copy of relay-owned state for exact reconstruction.
func OutboxToCloudEvent(
	envelope outbox.Envelope,
	options OutboxOptions,
) (cloudevents.Event, outbox.Envelope, Report, error) {
	if envelope.ID == "" || options.Source == "" || options.Type == "" {
		return cloudevents.Event{}, outbox.Envelope{}, Report{}, fmt.Errorf("%w: outbox mapping", ErrInvalidAdapterInput)
	}
	data, err := dataFromPayload(options.DataContentType, envelope.Payload)
	if err != nil {
		return cloudevents.Event{}, outbox.Envelope{}, Report{}, err
	}
	event, err := cloudevents.NewEvent(cloudevents.Attributes{
		ID: envelope.ID, Source: options.Source, Type: options.Type,
		DataContentType: options.DataContentType, DataSchema: options.DataSchema,
		Subject: options.Subject, Time: cloneTimePointer(options.OccurredAt),
	}, data)
	if err != nil {
		return cloudevents.Event{}, outbox.Envelope{}, Report{}, err
	}
	return event, cloneEnvelope(envelope), Report{}, nil
}

// CloudEventToOutbox restores payload and stable ID into caller-retained outbox
// state. Portable context without an outbox owner is reported as loss.
func CloudEventToOutbox(
	event cloudevents.Event,
	state outbox.Envelope,
) (outbox.Envelope, Report, error) {
	if err := event.Validate(); err != nil || state.Topic == "" || state.PayloadVersion == 0 {
		return outbox.Envelope{}, Report{}, fmt.Errorf("%w: outbox target", ErrInvalidAdapterInput)
	}
	if state.ID != "" && state.ID != event.ID() {
		return outbox.Envelope{}, Report{}, fmt.Errorf("%w: outbox id", ErrMetadataCollision)
	}
	if !event.Data().Present() {
		return outbox.Envelope{}, Report{}, fmt.Errorf("%w: outbox payload", ErrInvalidAdapterInput)
	}
	state = cloneEnvelope(state)
	state.ID = event.ID()
	state.Payload = restoreRetainedNil(event.Data(), state.Payload == nil)
	report := Report{Losses: []Loss{
		{Field: "source", Reason: "not represented by outbox"},
		{Field: "type", Reason: "outbox topic is transport-owned"},
	}}
	if _, present := event.DataContentType(); present {
		report.Losses = append(report.Losses, Loss{Field: "datacontenttype", Reason: "not represented by outbox"})
	}
	appendDataKindLoss(event, &report, "outbox", false)
	appendOptionalContextLosses(event, &report, "outbox")
	appendExtensionLosses(event, &report, "outbox", nil)
	return state, report, nil
}

// QueueOptions owns required CloudEvents values absent from Golib queue jobs.
type QueueOptions struct {
	Source     string
	StableID   string
	Type       string
	OccurredAt *time.Time
}

// QueueToCloudEvent performs the documented Golib queue mapping and retains
// execution, retry, settlement, and operational metadata out of band.
func QueueToCloudEvent(
	message job.Message,
	options QueueOptions,
) (cloudevents.Event, job.Message, Report, error) {
	if message.Metadata != nil {
		if _, err := validatedTenant(message.Metadata.TenantID); err != nil {
			return cloudevents.Event{}, job.Message{}, Report{}, err
		}
	}
	if err := message.Validate(); err != nil || options.Source == "" {
		return cloudevents.Event{}, job.Message{}, Report{}, fmt.Errorf("%w: queue mapping", ErrInvalidAdapterInput)
	}
	id, eventType, contentType := options.StableID, options.Type, ""
	if message.Metadata != nil {
		if id == "" {
			id = message.Metadata.OriginalID
		}
		if eventType == "" {
			eventType = message.Metadata.JobType
		}
		contentType = message.Metadata.ContentType
	}
	if id == "" || eventType == "" {
		return cloudevents.Event{}, job.Message{}, Report{}, fmt.Errorf("%w: queue identity", ErrInvalidAdapterInput)
	}
	data, err := dataFromPayload(contentType, message.Body)
	if err != nil {
		return cloudevents.Event{}, job.Message{}, Report{}, err
	}
	extensions, err := queueExtensions(message.Metadata)
	if err != nil {
		return cloudevents.Event{}, job.Message{}, Report{}, err
	}
	event, err := cloudevents.NewEvent(cloudevents.Attributes{
		ID: id, Source: options.Source, Type: eventType, DataContentType: contentType,
		Time: cloneTimePointer(options.OccurredAt), Extensions: extensions,
	}, data)
	if err != nil {
		return cloudevents.Event{}, job.Message{}, Report{}, err
	}
	return event, cloneJob(message), Report{}, nil
}

// CloudEventToQueue restores an event into caller-retained queue execution
// state. Settlement callbacks remain attached to the retained message.
func CloudEventToQueue(
	event cloudevents.Event,
	state job.Message,
) (job.Message, Report, error) {
	if err := event.Validate(); err != nil || !event.Data().Present() {
		return job.Message{}, Report{}, fmt.Errorf("%w: queue target", ErrInvalidAdapterInput)
	}
	state = cloneJob(state)
	if state.Metadata == nil {
		state.Metadata = &job.Metadata{}
	}
	if state.Metadata.OriginalID != "" && state.Metadata.OriginalID != event.ID() {
		return job.Message{}, Report{}, fmt.Errorf("%w: queue original id", ErrMetadataCollision)
	}
	if state.Metadata.JobType != "" && state.Metadata.JobType != event.Type() {
		return job.Message{}, Report{}, fmt.Errorf("%w: queue job type", ErrMetadataCollision)
	}
	state.Body = restoreRetainedNil(event.Data(), state.Body == nil)
	state.Metadata.OriginalID = event.ID()
	state.Metadata.JobType = event.Type()
	contentType, _ := event.DataContentType()
	if state.Metadata.ContentType != "" && state.Metadata.ContentType != contentType {
		return job.Message{}, Report{}, fmt.Errorf("%w: queue content type", ErrMetadataCollision)
	}
	state.Metadata.ContentType = contentType
	if err := verifyQueueExtensions(event, state.Metadata); err != nil {
		return job.Message{}, Report{}, err
	}
	report := Report{Losses: []Loss{{Field: "source", Reason: "not represented by queue"}}}
	appendDataKindLoss(event, &report, "queue", true)
	appendOptionalContextLosses(event, &report, "queue")
	appendExtensionLosses(event, &report, "queue", map[string]struct{}{
		correlationIDExtension: {}, requestIDExtension: {}, causationIDExtension: {},
		"traceparent": {}, "tracestate": {}, tenantIDExtension: {},
	})
	return state, report, nil
}

// WorkflowOptions supplies the stable event ID absent from workflow history
// and the explicit producer source.
type WorkflowOptions struct {
	StableID string
	Source   string
}

// WorkflowState retains durable workflow fields not represented by the
// CloudEvents information model.
type WorkflowState struct {
	StableID       string
	Sequence       uint64
	Definition     workflow.DefinitionReference
	SuccessorID    string
	StepName       string
	Attempt        uint32
	IdempotencyKey string
	DueAt          time.Time
	Code           string
	Retryable      bool
	// DataWasNil preserves nil versus present-empty workflow data across the
	// portable CloudEvents representation, where both have zero wire bytes.
	DataWasNil bool
}

// WorkflowToCloudEvent maps one durable decision without inventing an ID.
func WorkflowToCloudEvent(
	history workflow.HistoryEvent,
	options WorkflowOptions,
) (cloudevents.Event, WorkflowState, Report, error) {
	if options.StableID == "" || options.Source == "" || history.Sequence() == 0 {
		return cloudevents.Event{}, WorkflowState{}, Report{}, fmt.Errorf("%w: workflow mapping", ErrInvalidAdapterInput)
	}
	event, err := cloudevents.NewEvent(cloudevents.Attributes{
		ID: options.StableID, Source: options.Source, Type: workflowEventType(history.Kind()),
		Subject: history.InstanceID(), Time: timePointer(history.OccurredAt()),
	}, cloudevents.NewBinaryData(history.Data()))
	if err != nil {
		return cloudevents.Event{}, WorkflowState{}, Report{}, err
	}
	return event, WorkflowState{
		StableID: options.StableID, Sequence: history.Sequence(), Definition: history.Definition(),
		SuccessorID: history.SuccessorID(), StepName: history.StepName(), Attempt: history.Attempt(),
		IdempotencyKey: history.IdempotencyKey(), DueAt: history.DueAt(), Code: history.Code(),
		Retryable: history.Retryable(), DataWasNil: history.Data() == nil,
	}, Report{}, nil
}

// CloudEventToWorkflow restores one decision from portable fields and retained
// workflow state.
func CloudEventToWorkflow(
	event cloudevents.Event,
	state WorkflowState,
) (workflow.HistoryEvent, Report, error) {
	if state.StableID == "" || state.Sequence == 0 || event.ID() != state.StableID {
		return workflow.HistoryEvent{}, Report{}, fmt.Errorf("%w: workflow target", ErrInvalidAdapterInput)
	}
	kind, err := parseWorkflowEventType(event.Type())
	if err != nil {
		return workflow.HistoryEvent{}, Report{}, err
	}
	instanceID, present := event.Subject()
	occurredAt, hasTime := event.Time()
	if !present || !hasTime || !event.Data().Present() {
		return workflow.HistoryEvent{}, Report{}, fmt.Errorf("%w: workflow portable fields", ErrInvalidAdapterInput)
	}
	history, err := workflow.NewHistoryEvent(workflow.HistoryEventSpec{
		Sequence: state.Sequence, InstanceID: instanceID, Kind: kind, OccurredAt: occurredAt,
		Definition: state.Definition, SuccessorID: state.SuccessorID, StepName: state.StepName,
		Attempt: state.Attempt, IdempotencyKey: state.IdempotencyKey, DueAt: state.DueAt,
		Code: state.Code, Retryable: state.Retryable, Data: restoreRetainedNil(event.Data(), state.DataWasNil),
	})
	if err != nil {
		return workflow.HistoryEvent{}, Report{}, err
	}
	report := Report{Losses: []Loss{{Field: "source", Reason: "not represented by workflow history"}}}
	if _, present := event.DataContentType(); present {
		report.Losses = append(report.Losses, Loss{
			Field: "datacontenttype", Reason: "not represented by workflow history",
		})
	}
	if _, present := event.DataSchema(); present {
		report.Losses = append(report.Losses, Loss{
			Field: "dataschema", Reason: "not represented by workflow history",
		})
	}
	appendDataKindLoss(event, &report, "workflow history", false)
	appendExtensionLosses(event, &report, "workflow history", nil)
	return history, report, nil
}

func dataFromPayload(contentType string, payload []byte) (cloudevents.Data, error) {
	if contentType == "" {
		return cloudevents.NewBinaryData(payload), nil
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return cloudevents.Data{}, fmt.Errorf("%w: content type: %w", ErrInvalidAdapterInput, err)
	}
	mediaType = strings.ToLower(mediaType)
	if mediaType == "application/json" || strings.HasSuffix(mediaType, "+json") {
		data, err := cloudevents.NewJSONData(payload)
		if err != nil {
			return cloudevents.Data{}, err
		}
		return data, nil
	}
	if strings.HasPrefix(mediaType, "text/") {
		data, err := cloudevents.NewTextData(string(payload))
		if err != nil {
			return cloudevents.Data{}, err
		}
		return data, nil
	}
	return cloudevents.NewBinaryData(payload), nil
}

func restoreRetainedNil(data cloudevents.Data, retainedNil bool) []byte {
	value := data.Bytes()
	if retainedNil && len(value) == 0 {
		return nil
	}
	return value
}

func putStringExtension(extensions map[string]cloudevents.Attribute, name, value string) error {
	attribute, err := cloudevents.NewStringAttribute(value)
	if err != nil {
		return fmt.Errorf("%w: extension %s: %w", ErrInvalidAdapterInput, name, err)
	}
	extensions[name] = attribute
	return nil
}

func mappedString(event cloudevents.Event, name, retained string, trusted bool) (string, error) {
	value, present, err := stringExtension(event, name)
	if err != nil {
		return "", err
	}
	if present && retained != "" && value != retained {
		return "", fmt.Errorf("%w: %s", ErrMetadataCollision, name)
	}
	if present {
		if retained == "" && !trusted {
			return "", fmt.Errorf("%w: %s", ErrUntrustedMetadata, name)
		}
		return value, nil
	}
	if retained != "" {
		return "", fmt.Errorf("%w: missing %s", ErrMetadataCollision, name)
	}
	return retained, nil
}

func encodeStreamSubject(stream eventsourcing.StreamID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(stream.AggregateType())) + "." +
		base64.RawURLEncoding.EncodeToString([]byte(stream.AggregateID()))
}

func cloneStrings(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func timePointer(value time.Time) *time.Time { return &value }

func cloneEnvelope(envelope outbox.Envelope) outbox.Envelope {
	envelope.Payload = cloneBytes(envelope.Payload)
	envelope.Metadata = cloneStrings(envelope.Metadata)
	return envelope
}

func cloneJob(message job.Message) job.Message {
	message.Body = cloneBytes(message.Body)
	if message.Metadata == nil {
		return message
	}
	metadata := *message.Metadata
	metadata.Tags = cloneStrings(metadata.Tags)
	metadata.Correlation = cloneStrings(metadata.Correlation)
	metadata.TraceContext = cloneStrings(metadata.TraceContext)
	if metadata.EnqueuedAt != nil {
		enqueuedAt := *metadata.EnqueuedAt
		metadata.EnqueuedAt = &enqueuedAt
	}
	message.Metadata = &metadata
	return message
}

func queueExtensions(metadata *job.Metadata) (map[string]cloudevents.Attribute, error) {
	extensions := map[string]cloudevents.Attribute{}
	if metadata == nil {
		return extensions, nil
	}
	if _, err := validatedTenant(metadata.TenantID); err != nil {
		return nil, err
	}
	for _, mapping := range []struct{ target, source string }{
		{correlationIDExtension, metadata.Correlation[correlationIDExtension]},
		{requestIDExtension, metadata.Correlation[requestIDExtension]},
		{causationIDExtension, metadata.Correlation[causationIDExtension]},
		{"traceparent", metadata.TraceContext["traceparent"]},
		{"tracestate", metadata.TraceContext["tracestate"]},
		{tenantIDExtension, metadata.TenantID},
	} {
		if mapping.source != "" {
			attribute, err := cloudevents.NewStringAttribute(mapping.source)
			if err != nil {
				return nil, fmt.Errorf("%w: queue extension %s: %w", ErrInvalidAdapterInput, mapping.target, err)
			}
			extensions[mapping.target] = attribute
		}
	}
	return extensions, nil
}

func appendOptionalContextLosses(event cloudevents.Event, report *Report, target string) {
	if _, present := event.DataSchema(); present {
		report.Losses = append(report.Losses, Loss{Field: "dataschema", Reason: "not represented by " + target})
	}
	if _, present := event.Subject(); present {
		report.Losses = append(report.Losses, Loss{Field: "subject", Reason: "not represented by " + target})
	}
	if _, present := event.Time(); present {
		report.Losses = append(report.Losses, Loss{Field: "time", Reason: "not represented by " + target})
	}
}

func appendDataKindLoss(event cloudevents.Event, report *Report, target string, contentTypePreserved bool) {
	data := event.Data()
	if !data.Present() {
		return
	}
	targetKind := cloudevents.DataBinary
	if contentTypePreserved {
		if contentType, present := event.DataContentType(); present {
			mediaType, _, err := mime.ParseMediaType(contentType)
			if err == nil {
				mediaType = strings.ToLower(mediaType)
				switch {
				case mediaType == "application/json" || strings.HasSuffix(mediaType, "+json"):
					targetKind = cloudevents.DataJSON
				case strings.HasPrefix(mediaType, "text/"):
					targetKind = cloudevents.DataText
				}
			}
		}
	}
	if data.Kind() != targetKind {
		report.Losses = append(report.Losses, Loss{
			Field: "data.kind", Reason: "not represented by " + target,
		})
	}
}

func appendExtensionLosses(
	event cloudevents.Event,
	report *Report,
	target string,
	represented map[string]struct{},
) {
	extensions := event.Extensions()
	names := make([]string, 0, len(extensions))
	for name := range extensions {
		if _, present := represented[name]; !present {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		report.Losses = append(report.Losses, Loss{
			Field: "extensions." + name, Reason: "not represented by " + target,
		})
	}
}

func verifyQueueExtensions(event cloudevents.Event, metadata *job.Metadata) error {
	if _, err := validatedTenant(metadata.TenantID); err != nil {
		return err
	}
	expected := []struct{ name, retained string }{
		{correlationIDExtension, metadata.Correlation[correlationIDExtension]},
		{requestIDExtension, metadata.Correlation[requestIDExtension]},
		{causationIDExtension, metadata.Correlation[causationIDExtension]},
		{"traceparent", metadata.TraceContext["traceparent"]},
		{"tracestate", metadata.TraceContext["tracestate"]},
		{tenantIDExtension, metadata.TenantID},
	}
	for _, mapping := range expected {
		value, present, err := stringExtension(event, mapping.name)
		if err != nil {
			return err
		}
		if mapping.name == tenantIDExtension && present {
			if _, err := validatedTenant(value); err != nil {
				return err
			}
		}
		if (!present && mapping.retained != "") || (present && mapping.retained != value) {
			return fmt.Errorf("%w: queue extension %s", ErrMetadataCollision, mapping.name)
		}
	}
	return nil
}

func validatedTenant(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	tenant, err := tenancy.ParseTenantID(value)
	if err != nil {
		return "", fmt.Errorf("%w: tenant: %w", ErrInvalidAdapterInput, err)
	}
	return tenant.Value(), nil
}

func workflowEventType(kind workflow.EventKind) string {
	return "golib.workflow.history." + strconv.FormatUint(uint64(kind), 10)
}

func parseWorkflowEventType(value string) (workflow.EventKind, error) {
	const prefix = "golib.workflow.history."
	if !strings.HasPrefix(value, prefix) {
		return 0, fmt.Errorf("%w: workflow type", ErrInvalidAdapterInput)
	}
	parsed, err := strconv.ParseUint(strings.TrimPrefix(value, prefix), 10, 8)
	if err != nil {
		return 0, fmt.Errorf("%w: workflow type: %w", ErrInvalidAdapterInput, err)
	}
	kind := workflow.EventKind(parsed)
	if value != workflowEventType(kind) {
		return 0, fmt.Errorf("%w: non-canonical workflow type", ErrInvalidAdapterInput)
	}
	return kind, nil
}
