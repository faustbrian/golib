package eventsourcing

import (
	"bytes"
	"fmt"
	"maps"
	"mime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxAggregateTypeBytes is the maximum encoded aggregate type length.
	MaxAggregateTypeBytes = 255
	// MaxAggregateIDBytes is the maximum encoded aggregate identifier length.
	MaxAggregateIDBytes = 512
	// MaxEventNameBytes is the maximum encoded event name length.
	MaxEventNameBytes = 255
	// MaxMessageIDBytes is the maximum encoded message identifier length.
	MaxMessageIDBytes = 128
	// MaxContentTypeBytes is the maximum encoded content type length.
	MaxContentTypeBytes = 128
	// MaxPayloadBytes is the maximum encoded event payload length.
	MaxPayloadBytes = 1 << 20
	// MaxMetadataEntries is the maximum number of application metadata entries.
	MaxMetadataEntries = 64
	// MaxMetadataKeyBytes is the maximum encoded metadata key length.
	MaxMetadataKeyBytes = 128
	// MaxMetadataValueBytes is the maximum encoded metadata value length.
	MaxMetadataValueBytes = 4 << 10
	// MaxMetadataBytes is the maximum combined metadata key and value length.
	MaxMetadataBytes = 64 << 10
	// MaxTenantBytes is the maximum encoded tenant value length.
	MaxTenantBytes = 255
	// MaxPartitionBytes is the maximum encoded partition value length.
	MaxPartitionBytes = 255
)

const reservedMetadataPrefix = "es."

// MessageID is a stable application-supplied message identifier.
//
// Its zero value represents an absent optional identifier and is invalid as a
// message's primary ID.
type MessageID struct {
	value string
}

// NewMessageID validates a stable message identifier.
func NewMessageID(value string) (MessageID, error) {
	if !validToken(value, MaxMessageIDBytes) {
		return MessageID{}, invalid("message_id", "must be a non-empty canonical token")
	}

	return MessageID{value: value}, nil
}

// String returns the canonical identifier.
func (id MessageID) String() string {
	return id.value
}

// IsZero reports whether the identifier is absent.
func (id MessageID) IsZero() bool {
	return id.value == ""
}

// StreamID identifies one aggregate event stream.
type StreamID struct {
	aggregateType string
	aggregateID   string
}

// NewStreamID validates an aggregate type and application-defined identifier.
func NewStreamID(aggregateType, aggregateID string) (StreamID, error) {
	if !validName(aggregateType, MaxAggregateTypeBytes) {
		return StreamID{}, invalid("aggregate_type", "must be a non-empty canonical name")
	}
	if !validText(aggregateID, MaxAggregateIDBytes, false) {
		return StreamID{}, invalid("aggregate_id", "must be bounded UTF-8 without control characters")
	}

	return StreamID{aggregateType: aggregateType, aggregateID: aggregateID}, nil
}

// AggregateType returns the stable aggregate type.
func (id StreamID) AggregateType() string {
	return id.aggregateType
}

// AggregateID returns the application-defined aggregate identifier.
func (id StreamID) AggregateID() string {
	return id.aggregateID
}

// IsZero reports whether the stream has not been assigned.
func (id StreamID) IsZero() bool {
	return id.aggregateType == "" && id.aggregateID == ""
}

// String returns a diagnostic stream identity without event data or metadata.
func (id StreamID) String() string {
	return id.aggregateType + "/" + id.aggregateID
}

// EventName is a stable persisted event identity.
type EventName struct {
	value string
}

// String returns the stable persisted name.
func (name EventName) String() string {
	return name.value
}

// SchemaVersion is a positive event schema version.
type SchemaVersion uint32

// GlobalPosition is a one-based store-assigned global ordering position.
//
// Its zero value means that a store does not provide global ordering.
type GlobalPosition uint64

// EncodedEventInput supplies immutable encoded event data.
type EncodedEventInput struct {
	Name        string
	Version     SchemaVersion
	ContentType string
	Payload     []byte
}

// EncodedEvent is immutable-by-contract event data ready for storage.
type EncodedEvent struct {
	name        EventName
	version     SchemaVersion
	contentType string
	payload     []byte
}

// NewEncodedEvent validates and owns encoded event data.
func NewEncodedEvent(input EncodedEventInput) (EncodedEvent, error) {
	if !validName(input.Name, MaxEventNameBytes) {
		return EncodedEvent{}, invalid("event_name", "must be a non-empty canonical name")
	}
	if input.Version == 0 {
		return EncodedEvent{}, invalid("event_schema_version", "must be greater than zero")
	}
	if !validContentType(input.ContentType) {
		return EncodedEvent{}, invalid("content_type", "must be a bounded canonical media type")
	}
	if len(input.Payload) == 0 || len(input.Payload) > MaxPayloadBytes {
		return EncodedEvent{}, invalid("payload", "must be non-empty and within the size limit")
	}

	return EncodedEvent{
		name:        EventName{value: input.Name},
		version:     input.Version,
		contentType: input.ContentType,
		payload:     cloneBytes(input.Payload),
	}, nil
}

// Name returns the stable persisted event identity.
func (event EncodedEvent) Name() EventName {
	return event.name
}

// Version returns the event schema version.
func (event EncodedEvent) Version() SchemaVersion {
	return event.version
}

// ContentType returns the payload media type.
func (event EncodedEvent) ContentType() string {
	return event.contentType
}

// Payload returns a defensive copy of the encoded payload.
func (event EncodedEvent) Payload() []byte {
	return cloneBytes(event.payload)
}

// IsZero reports whether the event has not been assigned.
func (event EncodedEvent) IsZero() bool {
	return event.name.value == "" && event.version == 0 && event.contentType == "" && len(event.payload) == 0
}

// PendingMessageInput supplies message data before store-assigned positions.
type PendingMessageInput struct {
	ID            string
	Stream        StreamID
	Event         EncodedEvent
	Metadata      map[string]string
	RecordedAt    time.Time
	CorrelationID string
	CausationID   string
	Tenant        string
	Partition     string
}

// PendingMessage is an immutable-by-contract message awaiting persistence.
type PendingMessage struct {
	id            MessageID
	stream        StreamID
	event         EncodedEvent
	metadata      map[string]string
	recordedAt    time.Time
	correlationID MessageID
	causationID   MessageID
	tenant        string
	partition     string
}

// NewPendingMessage validates and owns pending message data.
func NewPendingMessage(input PendingMessageInput) (PendingMessage, error) {
	id, err := NewMessageID(input.ID)
	if err != nil {
		return PendingMessage{}, err
	}
	if input.Stream.IsZero() {
		return PendingMessage{}, invalid("stream", "must be assigned")
	}
	if input.Event.IsZero() {
		return PendingMessage{}, invalid("event", "must be assigned")
	}
	if input.RecordedAt.IsZero() {
		return PendingMessage{}, invalid("recorded_at", "must be assigned")
	}

	metadata, err := copyMetadata(input.Metadata)
	if err != nil {
		return PendingMessage{}, err
	}
	correlationID, err := optionalMessageID("correlation_id", input.CorrelationID)
	if err != nil {
		return PendingMessage{}, err
	}
	causationID, err := optionalMessageID("causation_id", input.CausationID)
	if err != nil {
		return PendingMessage{}, err
	}
	if input.Tenant != "" &&
		(strings.TrimSpace(input.Tenant) == "" ||
			!validText(input.Tenant, MaxTenantBytes, true)) {
		return PendingMessage{}, invalid("tenant", "must be bounded UTF-8 without control characters")
	}
	if input.Partition != "" &&
		(strings.TrimSpace(input.Partition) == "" ||
			!validText(input.Partition, MaxPartitionBytes, true)) {
		return PendingMessage{}, invalid("partition", "must be bounded UTF-8 without control characters")
	}

	return PendingMessage{
		id:            id,
		stream:        input.Stream,
		event:         cloneEvent(input.Event),
		metadata:      metadata,
		recordedAt:    normalizeTime(input.RecordedAt),
		correlationID: correlationID,
		causationID:   causationID,
		tenant:        input.Tenant,
		partition:     input.Partition,
	}, nil
}

// ID returns the message identifier.
func (message PendingMessage) ID() MessageID {
	return message.id
}

// Stream returns the aggregate stream identity.
func (message PendingMessage) Stream() StreamID {
	return message.stream
}

// Event returns an immutable-by-contract copy of the encoded event.
func (message PendingMessage) Event() EncodedEvent {
	return cloneEvent(message.event)
}

// Metadata returns a defensive copy of application metadata.
func (message PendingMessage) Metadata() map[string]string {
	return cloneMetadata(message.metadata)
}

// WithMetadata returns an immutable copy with validated replacement metadata.
func (message PendingMessage) WithMetadata(
	metadata map[string]string,
) (PendingMessage, error) {
	if message.id.IsZero() ||
		message.stream.IsZero() ||
		message.event.IsZero() ||
		message.recordedAt.IsZero() {
		return PendingMessage{}, invalid("pending_message", "must be assigned")
	}

	owned, err := copyMetadata(metadata)
	if err != nil {
		return PendingMessage{}, err
	}
	decorated := clonePendingMessage(message)
	decorated.metadata = owned

	return decorated, nil
}

// RecordedAt returns the UTC, microsecond-precision recording time.
func (message PendingMessage) RecordedAt() time.Time {
	return message.recordedAt
}

// CorrelationID returns the optional correlation identifier.
func (message PendingMessage) CorrelationID() (MessageID, bool) {
	return message.correlationID, !message.correlationID.IsZero()
}

// CausationID returns the optional causation identifier.
func (message PendingMessage) CausationID() (MessageID, bool) {
	return message.causationID, !message.causationID.IsZero()
}

// Tenant returns optional application-defined tenant metadata.
func (message PendingMessage) Tenant() (string, bool) {
	return message.tenant, message.tenant != ""
}

// Partition returns optional application-defined partition metadata.
func (message PendingMessage) Partition() (string, bool) {
	return message.partition, message.partition != ""
}

// String returns diagnostics that omit payload and metadata values.
func (message PendingMessage) String() string {
	return fmt.Sprintf(
		"pending message %s stream=%s event=%s payload_bytes=%d metadata_entries=%d",
		message.id.String(),
		message.stream.String(),
		message.event.Name().String(),
		len(message.event.payload),
		len(message.metadata),
	)
}

// MessageInput supplies store-assigned positions for a pending message.
type MessageInput struct {
	Pending        PendingMessage
	StreamVersion  uint64
	GlobalPosition GlobalPosition
}

// Message is an immutable-by-contract persisted event message.
type Message struct {
	pending        PendingMessage
	streamVersion  uint64
	globalPosition GlobalPosition
}

// NewMessage validates store-assigned positions and owns the pending message.
func NewMessage(input MessageInput) (Message, error) {
	if input.Pending.id.IsZero() ||
		input.Pending.stream.IsZero() ||
		input.Pending.event.IsZero() ||
		input.Pending.recordedAt.IsZero() {
		return Message{}, invalid("pending_message", "must be assigned")
	}
	if input.StreamVersion == 0 {
		return Message{}, invalid("stream_version", "must be greater than zero")
	}

	return Message{
		pending:        clonePendingMessage(input.Pending),
		streamVersion:  input.StreamVersion,
		globalPosition: input.GlobalPosition,
	}, nil
}

// ID returns the message identifier.
func (message Message) ID() MessageID {
	return message.pending.ID()
}

// Stream returns the aggregate stream identity.
func (message Message) Stream() StreamID {
	return message.pending.Stream()
}

// StreamVersion returns the one-based stream position.
func (message Message) StreamVersion() uint64 {
	return message.streamVersion
}

// Event returns an immutable-by-contract copy of the encoded event.
func (message Message) Event() EncodedEvent {
	return message.pending.Event()
}

// Metadata returns a defensive copy of application metadata.
func (message Message) Metadata() map[string]string {
	return message.pending.Metadata()
}

// RecordedAt returns the UTC, microsecond-precision recording time.
func (message Message) RecordedAt() time.Time {
	return message.pending.RecordedAt()
}

// CorrelationID returns the optional correlation identifier.
func (message Message) CorrelationID() (MessageID, bool) {
	return message.pending.CorrelationID()
}

// CausationID returns the optional causation identifier.
func (message Message) CausationID() (MessageID, bool) {
	return message.pending.CausationID()
}

// Tenant returns optional application-defined tenant metadata.
func (message Message) Tenant() (string, bool) {
	return message.pending.Tenant()
}

// Partition returns optional application-defined partition metadata.
func (message Message) Partition() (string, bool) {
	return message.pending.Partition()
}

// GlobalPosition returns the optional one-based global ordering position.
func (message Message) GlobalPosition() (GlobalPosition, bool) {
	return message.globalPosition, message.globalPosition != 0
}

// Equal reports value equality across every observable message field.
func (message Message) Equal(other Message) bool {
	return pendingMessagesEqual(message.pending, other.pending) &&
		message.streamVersion == other.streamVersion &&
		message.globalPosition == other.globalPosition
}

// String returns diagnostics that omit payload and metadata values.
func (message Message) String() string {
	return fmt.Sprintf(
		"message %s stream=%s version=%d event=%s payload_bytes=%d metadata_entries=%d",
		message.ID().String(),
		message.Stream().String(),
		message.streamVersion,
		message.Event().Name().String(),
		len(message.pending.event.payload),
		len(message.pending.metadata),
	)
}

func optionalMessageID(field, value string) (MessageID, error) {
	if value == "" {
		return MessageID{}, nil
	}

	id, err := NewMessageID(value)
	if err != nil {
		return MessageID{}, invalid(field, "must be a canonical message identifier")
	}

	return id, nil
}

func copyMetadata(input map[string]string) (map[string]string, error) {
	if len(input) > MaxMetadataEntries {
		return nil, invalid("metadata", "contains too many entries")
	}

	total := 0
	output := make(map[string]string, len(input))

	for key, value := range input {
		if !validMetadataKey(key) {
			return nil, invalid("metadata", "contains an invalid or reserved key")
		}
		if !validText(value, MaxMetadataValueBytes, true) {
			return nil, invalid("metadata", "contains an invalid value")
		}

		total += len(key) + len(value)
		if total > MaxMetadataBytes {
			return nil, invalid("metadata", "exceeds the combined size limit")
		}

		output[key] = value
	}

	return output, nil
}

func validMetadataKey(value string) bool {
	return (len(value) < len(reservedMetadataPrefix) ||
		!strings.EqualFold(value[:len(reservedMetadataPrefix)], reservedMetadataPrefix)) &&
		validToken(value, MaxMetadataKeyBytes)
}

func validContentType(value string) bool {
	if len(value) == 0 ||
		len(value) > MaxContentTypeBytes ||
		strings.Count(value, "/") != 1 {
		return false
	}

	mediaType, parameters, err := mime.ParseMediaType(value)
	if err != nil {
		return false
	}

	return mime.FormatMediaType(mediaType, parameters) == value
}

func validName(value string, limit int) bool {
	if len(value) == 0 || len(value) > limit {
		return false
	}

	segmentStart := true

	for _, character := range value {
		switch {
		case segmentStart && character >= 'a' && character <= 'z':
			segmentStart = false
		case !segmentStart && character == '.':
			segmentStart = true
		case !segmentStart &&
			((character >= 'a' && character <= 'z') ||
				(character >= '0' && character <= '9') ||
				character == '-' ||
				character == '_'):
		default:
			return false
		}
	}

	return !segmentStart
}

func validToken(value string, limit int) bool {
	if len(value) == 0 || len(value) > limit {
		return false
	}

	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '.' ||
			character == '_' ||
			character == ':' ||
			character == '-' {
			continue
		}

		return false
	}

	return true
}

func validText(value string, limit int, allowEmpty bool) bool {
	if (!allowEmpty && strings.TrimSpace(value) == "") ||
		len(value) > limit ||
		!utf8.ValidString(value) {
		return false
	}

	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}

	return true
}

func normalizeTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

func cloneEvent(event EncodedEvent) EncodedEvent {
	event.payload = cloneBytes(event.payload)

	return event
}

func clonePendingMessage(message PendingMessage) PendingMessage {
	message.event = cloneEvent(message.event)
	message.metadata = cloneMetadata(message.metadata)

	return message
}

func pendingMessagesEqual(left, right PendingMessage) bool {
	return left.id == right.id &&
		left.stream == right.stream &&
		left.event.name == right.event.name &&
		left.event.version == right.event.version &&
		left.event.contentType == right.event.contentType &&
		bytes.Equal(left.event.payload, right.event.payload) &&
		maps.Equal(left.metadata, right.metadata) &&
		left.recordedAt.Equal(right.recordedAt) &&
		left.correlationID == right.correlationID &&
		left.causationID == right.causationID &&
		left.tenant == right.tenant &&
		left.partition == right.partition
}

func cloneBytes(input []byte) []byte {
	if input == nil {
		return nil
	}

	output := make([]byte, len(input))
	copy(output, input)

	return output
}

func cloneMetadata(input map[string]string) map[string]string {
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}

	return output
}
