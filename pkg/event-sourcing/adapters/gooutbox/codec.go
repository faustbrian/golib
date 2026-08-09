// Package gooutbox atomically stages event-sourcing messages and transactional
// outbox envelopes through one caller-owned PostgreSQL transaction.
package gooutbox

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/outbox"
)

const (
	// EnvelopePayloadVersion identifies the raw encoded-event payload layout.
	EnvelopePayloadVersion uint16 = 1

	// MetadataMessageID identifies the original event message.
	MetadataMessageID = "es.message_id"
	// MetadataAggregateType identifies the stable aggregate type.
	MetadataAggregateType = "es.aggregate_type"
	// MetadataAggregateID identifies the aggregate root.
	MetadataAggregateID = "es.aggregate_id"
	// MetadataStreamVersion carries the one-based aggregate stream version.
	MetadataStreamVersion = "es.stream_version"
	// MetadataEventName identifies the stable persisted event type.
	MetadataEventName = "es.event_name"
	// MetadataEventSchemaVersion carries the event payload schema version.
	MetadataEventSchemaVersion = "es.event_schema_version"
	// MetadataContentType carries the encoded event media type.
	MetadataContentType = "es.content_type"
	// MetadataRecordedAt carries the canonical event recording time.
	MetadataRecordedAt = "es.recorded_at"
	// MetadataCorrelationID carries the optional correlation identifier.
	MetadataCorrelationID = "es.correlation_id"
	// MetadataCausationID carries the optional causation identifier.
	MetadataCausationID = "es.causation_id"
	// MetadataTenant carries optional application tenant routing data.
	MetadataTenant = "es.tenant"
	// MetadataPartition carries optional application partition routing data.
	MetadataPartition = "es.partition"
	// MetadataGlobalPosition carries an optional store-wide ordering position.
	MetadataGlobalPosition = "es.global_position"
	// MetadataApplication contains canonical JSON application metadata.
	MetadataApplication = "es.metadata"
	// MetadataDeliveryMode distinguishes live publication from replay.
	MetadataDeliveryMode = "es.delivery_mode"

	deliveryModeLive = "live"

	maxOutboxOrderingKeyBytes = 255
)

var (
	// ErrResolverRequired reports a missing topic resolver.
	ErrResolverRequired = errors.New(
		"event-sourcing/gooutbox: topic resolver is required",
	)
	// ErrResolverPanic reports a contained topic resolver panic.
	ErrResolverPanic = errors.New(
		"event-sourcing/gooutbox: topic resolver panicked",
	)
	// ErrEnvelopeInvalid reports a message that cannot fit the configured
	// outbox envelope contract.
	ErrEnvelopeInvalid = errors.New(
		"event-sourcing/gooutbox: envelope is invalid",
	)
	// ErrEnvelopeCorrupt reports malformed or inconsistent envelope data.
	ErrEnvelopeCorrupt = errors.New(
		"event-sourcing/gooutbox: envelope is corrupt",
	)
)

// EnvelopeError redacts an envelope conversion failure while preserving its
// stable category and original cause for errors.Is and errors.As.
type EnvelopeError struct {
	category error
	cause    error
}

// Error implements error without exposing resolver or driver diagnostics.
func (err *EnvelopeError) Error() string {
	return err.category.Error()
}

// Unwrap exposes the stable category and original cause.
func (err *EnvelopeError) Unwrap() []error {
	return []error{err.category, err.cause}
}

// TopicMessage exposes only immutable pre-persistence fields so routing can be
// resolved before PostgreSQL stream locks are acquired. PendingMessage and
// Message both implement this contract.
type TopicMessage interface {
	ID() eventsourcing.MessageID
	Stream() eventsourcing.StreamID
	Event() eventsourcing.EncodedEvent
	Metadata() map[string]string
	RecordedAt() time.Time
	CorrelationID() (eventsourcing.MessageID, bool)
	CausationID() (eventsourcing.MessageID, bool)
	Tenant() (string, bool)
	Partition() (string, bool)
}

// TopicResolver selects one bounded outbox topic from immutable message data
// available before persistence. It cannot observe store-assigned positions.
// Implementations must be deterministic for the same TopicMessage, bounded,
// side-effect-free, concurrency-safe, and must not perform blocking IO.
type TopicResolver interface {
	ResolveTopic(TopicMessage) (string, error)
}

// TopicResolverFunc adapts a function to TopicResolver.
type TopicResolverFunc func(TopicMessage) (string, error)

// ResolveTopic implements TopicResolver.
func (resolver TopicResolverFunc) ResolveTopic(
	message TopicMessage,
) (string, error) {
	if resolver == nil {
		return "", ErrResolverRequired
	}

	return resolver(message)
}

// FixedTopic routes every message to one topic.
func FixedTopic(topic string) TopicResolver {
	return TopicResolverFunc(func(TopicMessage) (string, error) {
		return topic, nil
	})
}

// EnvelopeCodec maps persisted event messages to bounded outbox envelopes and
// back without reflection. It is immutable and safe for concurrent use.
type EnvelopeCodec struct {
	resolver TopicResolver
	limits   outbox.Limits
}

// DefaultLimits returns outbox limits that cover the adapter's fixed envelope
// fields and the core event payload bound. The outbox writer must use the same
// limits.
func DefaultLimits() outbox.Limits {
	return outbox.Limits{
		MaxIDBytes:             eventsourcing.MaxMessageIDBytes,
		MaxTopicBytes:          255,
		MaxPayloadBytes:        eventsourcing.MaxPayloadBytes,
		MaxMetadataEntries:     32,
		MaxMetadataBytes:       eventsourcing.MaxMetadataBytes + 4096,
		MaxOrderingKeyBytes:    maxOutboxOrderingKeyBytes,
		MaxIdempotencyKeyBytes: eventsourcing.MaxMessageIDBytes,
	}
}

// NewEnvelopeCodec validates a resolver and the exact limits shared with the
// outbox writer.
func NewEnvelopeCodec(
	resolver TopicResolver,
	limits outbox.Limits,
) (*EnvelopeCodec, error) {
	if resolver == nil {
		return nil, ErrResolverRequired
	}
	if err := limits.Validate(); err != nil {
		return nil, envelopeFailure(ErrEnvelopeInvalid, err)
	}

	return &EnvelopeCodec{resolver: resolver, limits: limits}, nil
}

// Encode creates one live outbox envelope. The event payload and application
// metadata are defensively copied into the returned value.
func (codec *EnvelopeCodec) Encode(
	message eventsourcing.Message,
) (outbox.Envelope, error) {
	if codec == nil || codec.resolver == nil || message.ID().IsZero() {
		return outbox.Envelope{}, ErrEnvelopeInvalid
	}
	topic, err := codec.resolveTopic(message)
	if err != nil {
		return outbox.Envelope{}, err
	}

	return codec.encodeResolved(message, topic)
}

func (codec *EnvelopeCodec) resolveTopic(message TopicMessage) (string, error) {
	topic, err := resolveTopic(codec.resolver, message)
	if err != nil {
		return "", envelopeFailure(ErrEnvelopeInvalid, err)
	}
	if !validTopic(topic, codec.limits.MaxTopicBytes) {
		return "", ErrEnvelopeInvalid
	}

	return topic, nil
}

func (codec *EnvelopeCodec) encodeResolved(
	message eventsourcing.Message,
	topic string,
) (outbox.Envelope, error) {
	metadataJSON, _ := json.Marshal(message.Metadata())
	event := message.Event()
	metadata := map[string]string{
		MetadataMessageID:          message.ID().String(),
		MetadataAggregateType:      message.Stream().AggregateType(),
		MetadataAggregateID:        message.Stream().AggregateID(),
		MetadataStreamVersion:      strconv.FormatUint(message.StreamVersion(), 10),
		MetadataEventName:          event.Name().String(),
		MetadataEventSchemaVersion: strconv.FormatUint(uint64(event.Version()), 10),
		MetadataContentType:        event.ContentType(),
		MetadataRecordedAt:         message.RecordedAt().Format(time.RFC3339Nano),
		MetadataApplication:        string(metadataJSON),
		MetadataDeliveryMode:       deliveryModeLive,
	}
	if id, exists := message.CorrelationID(); exists {
		metadata[MetadataCorrelationID] = id.String()
	}
	if id, exists := message.CausationID(); exists {
		metadata[MetadataCausationID] = id.String()
	}
	if tenant, exists := message.Tenant(); exists {
		metadata[MetadataTenant] = tenant
	}
	if partition, exists := message.Partition(); exists {
		metadata[MetadataPartition] = partition
	}
	if position, exists := message.GlobalPosition(); exists {
		metadata[MetadataGlobalPosition] = strconv.FormatUint(
			uint64(position),
			10,
		)
	}

	envelope := outbox.Envelope{
		ID:             message.ID().String(),
		Topic:          topic,
		Payload:        event.Payload(),
		PayloadVersion: EnvelopePayloadVersion,
		Metadata:       metadata,
		OrderingKey: orderingKey(
			message.Stream().AggregateID(),
			codec.limits.MaxOrderingKeyBytes,
		),
		IdempotencyKey: message.ID().String(),
		AvailableAt:    message.RecordedAt(),
		CreatedAt:      message.RecordedAt(),
	}
	if err := envelope.ValidateForInsert(codec.limits); err != nil {
		return outbox.Envelope{}, envelopeFailure(ErrEnvelopeInvalid, err)
	}

	return envelope, nil
}

// Decode reconstructs one persisted message from an unclaimed live envelope.
// Unknown event-sourcing metadata keys and inconsistent routing identity fail
// closed.
func (codec *EnvelopeCodec) Decode(
	envelope outbox.Envelope,
) (eventsourcing.Message, error) {
	if codec == nil {
		return eventsourcing.Message{}, ErrEnvelopeCorrupt
	}
	if err := envelope.ValidateForInsert(codec.limits); err != nil {
		return eventsourcing.Message{}, envelopeFailure(ErrEnvelopeCorrupt, err)
	}
	if envelope.PayloadVersion != EnvelopePayloadVersion ||
		envelope.ID != envelope.IdempotencyKey ||
		envelope.ID != envelope.Metadata[MetadataMessageID] ||
		envelope.OrderingKey != orderingKey(
			envelope.Metadata[MetadataAggregateID],
			codec.limits.MaxOrderingKeyBytes,
		) {
		return eventsourcing.Message{}, ErrEnvelopeCorrupt
	}
	if err := validateMetadataKeys(envelope.Metadata); err != nil {
		return eventsourcing.Message{}, err
	}

	streamVersion, err := requiredUint64(
		envelope.Metadata,
		MetadataStreamVersion,
	)
	if err != nil {
		return eventsourcing.Message{}, err
	}
	schemaVersion, err := requiredUint32(
		envelope.Metadata,
		MetadataEventSchemaVersion,
	)
	if err != nil {
		return eventsourcing.Message{}, err
	}
	globalPosition, err := optionalUint64(
		envelope.Metadata,
		MetadataGlobalPosition,
	)
	if err != nil {
		return eventsourcing.Message{}, err
	}
	recordedAt, err := time.Parse(
		time.RFC3339Nano,
		envelope.Metadata[MetadataRecordedAt],
	)
	if err != nil {
		return eventsourcing.Message{}, ErrEnvelopeCorrupt
	}
	var applicationMetadata map[string]string
	if err := json.Unmarshal(
		[]byte(envelope.Metadata[MetadataApplication]),
		&applicationMetadata,
	); err != nil {
		return eventsourcing.Message{}, ErrEnvelopeCorrupt
	}
	stream, err := eventsourcing.NewStreamID(
		envelope.Metadata[MetadataAggregateType],
		envelope.Metadata[MetadataAggregateID],
	)
	if err != nil {
		return eventsourcing.Message{}, ErrEnvelopeCorrupt
	}
	event, err := eventsourcing.NewEncodedEvent(
		eventsourcing.EncodedEventInput{
			Name:        envelope.Metadata[MetadataEventName],
			Version:     eventsourcing.SchemaVersion(schemaVersion),
			ContentType: envelope.Metadata[MetadataContentType],
			Payload:     envelope.Payload,
		},
	)
	if err != nil {
		return eventsourcing.Message{}, ErrEnvelopeCorrupt
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:            envelope.ID,
			Stream:        stream,
			Event:         event,
			Metadata:      applicationMetadata,
			RecordedAt:    recordedAt,
			CorrelationID: envelope.Metadata[MetadataCorrelationID],
			CausationID:   envelope.Metadata[MetadataCausationID],
			Tenant:        envelope.Metadata[MetadataTenant],
			Partition:     envelope.Metadata[MetadataPartition],
		},
	)
	if err != nil {
		return eventsourcing.Message{}, ErrEnvelopeCorrupt
	}
	message, _ := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:        pending,
		StreamVersion:  streamVersion,
		GlobalPosition: eventsourcing.GlobalPosition(globalPosition),
	})

	return message, nil
}

func resolveTopic(
	resolver TopicResolver,
	message TopicMessage,
) (topic string, err error) {
	defer func() {
		if recover() != nil {
			topic = ""
			err = ErrResolverPanic
		}
	}()

	return resolver.ResolveTopic(message)
}

func orderingKey(aggregateID string, maximum int) string {
	if len(aggregateID) <= maximum {
		return aggregateID
	}
	digest := sha256.Sum256([]byte(aggregateID))

	return "sha256:" + string(hex.AppendEncode(nil, digest[:]))
}

func validTopic(topic string, maximum int) bool {
	if topic == "" || len(topic) > maximum || !utf8.ValidString(topic) {
		return false
	}
	for _, character := range topic {
		if unicode.IsControl(character) {
			return false
		}
	}

	return true
}

func validateMetadataKeys(metadata map[string]string) error {
	if metadata[MetadataDeliveryMode] != deliveryModeLive {
		return ErrEnvelopeCorrupt
	}
	for key := range metadata {
		if strings.HasPrefix(key, "es.") && !knownMetadataKey(key) {
			return ErrEnvelopeCorrupt
		}
	}

	return nil
}

func knownMetadataKey(key string) bool {
	switch key {
	case MetadataMessageID,
		MetadataAggregateType,
		MetadataAggregateID,
		MetadataStreamVersion,
		MetadataEventName,
		MetadataEventSchemaVersion,
		MetadataContentType,
		MetadataRecordedAt,
		MetadataCorrelationID,
		MetadataCausationID,
		MetadataTenant,
		MetadataPartition,
		MetadataGlobalPosition,
		MetadataApplication,
		MetadataDeliveryMode:
		return true
	default:
		return false
	}
}

func requiredUint64(
	metadata map[string]string,
	key string,
) (uint64, error) {
	value, err := strconv.ParseUint(metadata[key], 10, 64)
	if err != nil || value == 0 {
		return 0, ErrEnvelopeCorrupt
	}

	return value, nil
}

func requiredUint32(
	metadata map[string]string,
	key string,
) (uint32, error) {
	value, err := strconv.ParseUint(metadata[key], 10, 32)
	if err != nil || value == 0 {
		return 0, ErrEnvelopeCorrupt
	}

	return uint32(value), nil
}

func optionalUint64(
	metadata map[string]string,
	key string,
) (uint64, error) {
	_, exists := metadata[key]
	if !exists {
		return 0, nil
	}

	return requiredUint64(metadata, key)
}

func envelopeFailure(category, cause error) error {
	return &EnvelopeError{category: category, cause: cause}
}

var _ error = (*EnvelopeError)(nil)
