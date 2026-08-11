// Package gokafka maps event-sourcing deliveries to bounded Kafka records.
package gokafka

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/kafka"
)

const (
	// HeaderWireVersion identifies the canonical event-sourcing Kafka mapping.
	HeaderWireVersion = "es.wire_version"
	// HeaderMessageID identifies the event message.
	HeaderMessageID = "es.message_id"
	// HeaderAggregateType identifies the stable aggregate type.
	HeaderAggregateType = "es.aggregate_type"
	// HeaderAggregateID identifies the aggregate root.
	HeaderAggregateID = "es.aggregate_id"
	// HeaderStreamVersion carries the one-based aggregate stream version.
	HeaderStreamVersion = "es.stream_version"
	// HeaderEventName identifies the stable persisted event type.
	HeaderEventName = "es.event_name"
	// HeaderEventSchemaVersion carries the event payload schema version.
	HeaderEventSchemaVersion = "es.event_schema_version"
	// HeaderContentType carries the encoded event media type.
	HeaderContentType = "es.content_type"
	// HeaderRecordedAt carries the canonical event recording time.
	HeaderRecordedAt = "es.recorded_at"
	// HeaderCorrelationID carries the optional correlation identifier.
	HeaderCorrelationID = "es.correlation_id"
	// HeaderCausationID carries the optional causation identifier.
	HeaderCausationID = "es.causation_id"
	// HeaderTenant carries optional application tenant routing data.
	HeaderTenant = "es.tenant"
	// HeaderPartition carries optional application partition routing data.
	HeaderPartition = "es.partition"
	// HeaderGlobalPosition carries an optional store-wide ordering position.
	HeaderGlobalPosition = "es.global_position"
	// HeaderApplicationMetadata contains canonical JSON application metadata.
	HeaderApplicationMetadata = "es.metadata"
	// HeaderDeliveryMode distinguishes live publication from replay.
	HeaderDeliveryMode = "es.delivery_mode"

	maxAllowedTopics = 64
	reservedPrefix   = "es."
	wireVersionV1    = "1"

	canonicalHeaderSequence = "\x00" +
		HeaderWireVersion + "\x00" +
		HeaderMessageID + "\x00" +
		HeaderAggregateType + "\x00" +
		HeaderAggregateID + "\x00" +
		HeaderStreamVersion + "\x00" +
		HeaderEventName + "\x00" +
		HeaderEventSchemaVersion + "\x00" +
		HeaderContentType + "\x00" +
		HeaderRecordedAt + "\x00" +
		HeaderCorrelationID + "\x00" +
		HeaderCausationID + "\x00" +
		HeaderTenant + "\x00" +
		HeaderPartition + "\x00" +
		HeaderGlobalPosition + "\x00" +
		HeaderApplicationMetadata + "\x00" +
		HeaderDeliveryMode + "\x00"
)

var (
	// ErrResolverRequired reports a missing topic resolver.
	ErrResolverRequired = errors.New(
		"event-sourcing/gokafka: topic resolver is required",
	)
	// ErrResolverPanic reports a contained topic resolver panic.
	ErrResolverPanic = errors.New(
		"event-sourcing/gokafka: topic resolver panicked",
	)
	// ErrInvalidConfig reports invalid bounds or topic allowlist entries.
	ErrInvalidConfig = errors.New(
		"event-sourcing/gokafka: configuration is invalid",
	)
	// ErrTopicDenied reports a topic outside the configured allowlist.
	ErrTopicDenied = errors.New(
		"event-sourcing/gokafka: topic is not allowed",
	)
	// ErrRecordInvalid reports an outbound delivery that cannot fit the
	// configured Kafka record contract.
	ErrRecordInvalid = errors.New(
		"event-sourcing/gokafka: record is invalid",
	)
	// ErrRecordCorrupt reports malformed or inconsistent inbound record data.
	ErrRecordCorrupt = errors.New(
		"event-sourcing/gokafka: record is corrupt",
	)
)

// RecordError redacts conversion diagnostics while preserving stable error
// categories and original causes for errors.Is and errors.As.
type RecordError struct {
	category error
	cause    error
}

// Error implements error without exposing resolver or transport diagnostics.
func (err *RecordError) Error() string {
	return err.category.Error()
}

// Unwrap exposes the stable category and original cause.
func (err *RecordError) Unwrap() []error {
	return []error{err.category, err.cause}
}

// TopicResolver selects one Kafka topic for a persisted event message.
type TopicResolver interface {
	ResolveTopic(eventsourcing.Message) (string, error)
}

// TopicResolverFunc adapts a function to TopicResolver.
type TopicResolverFunc func(eventsourcing.Message) (string, error)

// ResolveTopic implements TopicResolver.
func (resolver TopicResolverFunc) ResolveTopic(
	message eventsourcing.Message,
) (string, error) {
	if resolver == nil {
		return "", ErrResolverRequired
	}

	return resolver(message)
}

// FixedTopic routes every message to one topic.
func FixedTopic(topic string) TopicResolver {
	return TopicResolverFunc(func(eventsourcing.Message) (string, error) {
		return topic, nil
	})
}

// RecordCodecConfig defines immutable topic routing and Kafka record bounds.
type RecordCodecConfig struct {
	Resolver      TopicResolver
	AllowedTopics []string
	Limits        kafka.MessageLimits
}

// RecordCodec maps complete deliveries to Kafka records and back without
// reflection. It is immutable and safe for concurrent use when its resolver
// is safe for concurrent use.
type RecordCodec struct {
	resolver TopicResolver
	allowed  map[string]struct{}
	limits   kafka.MessageLimits
}

// DefaultRecordLimits returns bounds that keep the encoded payload and event
// headers below a typical one-megabyte Kafka record limit.
func DefaultRecordLimits() kafka.MessageLimits {
	return kafka.MessageLimits{
		MaxTopicBytes:       249,
		MaxKeyBytes:         eventsourcing.MaxAggregateIDBytes,
		MaxValueBytes:       900 << 10,
		MaxHeaders:          32,
		MaxHeaderKeyBytes:   eventsourcing.MaxMetadataKeyBytes,
		MaxHeaderValueBytes: eventsourcing.MaxMetadataBytes,
		MaxHeaderBytes:      72 << 10,
	}
}

// NewRecordCodec validates the resolver, exact topic allowlist, and bounds.
func NewRecordCodec(config RecordCodecConfig) (*RecordCodec, error) {
	if config.Resolver == nil {
		return nil, ErrResolverRequired
	}
	if config.Limits == (kafka.MessageLimits{}) {
		config.Limits = DefaultRecordLimits()
	}
	if err := config.Limits.Validate(); err != nil {
		return nil, recordFailure(ErrInvalidConfig, err)
	}
	if len(config.AllowedTopics) == 0 ||
		len(config.AllowedTopics) > maxAllowedTopics {
		return nil, ErrInvalidConfig
	}

	allowed := make(map[string]struct{}, len(config.AllowedTopics))
	for _, topic := range config.AllowedTopics {
		if !validTopic(topic, config.Limits.MaxTopicBytes) {
			return nil, ErrInvalidConfig
		}
		if _, duplicate := allowed[topic]; duplicate {
			return nil, ErrInvalidConfig
		}
		allowed[topic] = struct{}{}
	}

	return &RecordCodec{
		resolver: config.Resolver,
		allowed:  allowed,
		limits:   config.Limits,
	}, nil
}

// Encode creates one owned Kafka record. The aggregate ID is the default key,
// preserving per-aggregate order for records routed to one topic.
func (codec *RecordCodec) Encode(
	delivery eventsourcing.Delivery,
) (kafka.Message, error) {
	if codec == nil || codec.resolver == nil || delivery.IsZero() {
		return kafka.Message{}, ErrRecordInvalid
	}

	message := delivery.Message()
	recordedAt := message.RecordedAt()
	recordedAtValue := recordedAt.Format(time.RFC3339Nano)
	if _, err := canonicalTime(recordedAtValue); err != nil {
		return kafka.Message{}, recordFailure(ErrRecordInvalid, err)
	}
	topic, err := resolveTopic(codec.resolver, message)
	if err != nil {
		return kafka.Message{}, recordFailure(ErrRecordInvalid, err)
	}
	if _, allowed := codec.allowed[topic]; !allowed {
		return kafka.Message{}, recordFailure(ErrRecordInvalid, ErrTopicDenied)
	}

	metadata, _ := json.Marshal(message.Metadata())
	event := message.Event()
	headers := []kafka.Header{
		header(HeaderWireVersion, wireVersionV1),
		header(HeaderMessageID, message.ID().String()),
		header(HeaderAggregateType, message.Stream().AggregateType()),
		header(HeaderAggregateID, message.Stream().AggregateID()),
		headerUint64(HeaderStreamVersion, message.StreamVersion()),
		header(HeaderEventName, event.Name().String()),
		headerUint64(HeaderEventSchemaVersion, uint64(event.Version())),
		header(HeaderContentType, event.ContentType()),
		header(
			HeaderRecordedAt,
			recordedAtValue,
		),
	}
	if id, exists := message.CorrelationID(); exists {
		headers = append(headers, header(HeaderCorrelationID, id.String()))
	}
	if id, exists := message.CausationID(); exists {
		headers = append(headers, header(HeaderCausationID, id.String()))
	}
	if tenant, exists := message.Tenant(); exists {
		headers = append(headers, header(HeaderTenant, tenant))
	}
	if partition, exists := message.Partition(); exists {
		headers = append(headers, header(HeaderPartition, partition))
	}
	if position, exists := message.GlobalPosition(); exists {
		headers = append(
			headers,
			headerUint64(HeaderGlobalPosition, uint64(position)),
		)
	}
	headers = append(
		headers,
		kafka.Header{
			Key:   HeaderApplicationMetadata,
			Value: bytes.Clone(metadata),
		},
		header(HeaderDeliveryMode, delivery.Mode().String()),
	)

	record := kafka.Message{
		Topic:     topic,
		Key:       []byte(message.Stream().AggregateID()),
		Value:     event.Payload(),
		Headers:   headers,
		Timestamp: recordedAt.Truncate(time.Millisecond),
	}
	if err := validateRecord(
		record.Topic,
		record.Key,
		record.Value,
		record.Headers,
		codec.limits,
	); err != nil {
		return kafka.Message{}, recordFailure(ErrRecordInvalid, err)
	}

	return record, nil
}

// Decode reconstructs a complete delivery from one consumed Kafka record.
// Unknown reserved headers, duplicate identities, and routing mismatches fail
// closed. Non-reserved headers remain available to transport instrumentation.
func (codec *RecordCodec) Decode(
	record kafka.ConsumedMessage,
) (eventsourcing.Delivery, error) {
	if codec == nil {
		return eventsourcing.Delivery{}, ErrRecordCorrupt
	}
	if _, allowed := codec.allowed[record.Topic]; !allowed {
		return eventsourcing.Delivery{},
			recordFailure(ErrRecordCorrupt, ErrTopicDenied)
	}
	if err := validateRecord(
		record.Topic,
		record.Key,
		record.Value,
		record.Headers,
		codec.limits,
	); err != nil {
		return eventsourcing.Delivery{},
			recordFailure(ErrRecordCorrupt, err)
	}

	headers, err := parseHeaders(record.Headers)
	if err != nil {
		return eventsourcing.Delivery{}, err
	}
	if !bytes.Equal(record.Key, []byte(headers[HeaderAggregateID])) {
		return eventsourcing.Delivery{}, ErrRecordCorrupt
	}

	streamVersion, err := requiredUint64(headers, HeaderStreamVersion)
	if err != nil {
		return eventsourcing.Delivery{}, err
	}
	schemaVersion, err := requiredUint32(
		headers,
		HeaderEventSchemaVersion,
	)
	if err != nil {
		return eventsourcing.Delivery{}, err
	}
	globalPosition, err := optionalUint64(headers, HeaderGlobalPosition)
	if err != nil {
		return eventsourcing.Delivery{}, err
	}
	recordedAt, err := canonicalTime(headers[HeaderRecordedAt])
	if err != nil {
		return eventsourcing.Delivery{}, err
	}
	if record.TimestampType != kafka.TimestampCreateTime ||
		record.Timestamp.IsZero() ||
		!record.Timestamp.Equal(recordedAt.Truncate(time.Millisecond)) {
		return eventsourcing.Delivery{}, ErrRecordCorrupt
	}
	metadata, err := canonicalMetadata(
		[]byte(headers[HeaderApplicationMetadata]),
	)
	if err != nil {
		return eventsourcing.Delivery{}, err
	}

	stream, err := eventsourcing.NewStreamID(
		headers[HeaderAggregateType],
		headers[HeaderAggregateID],
	)
	if err != nil {
		return eventsourcing.Delivery{}, ErrRecordCorrupt
	}
	event, err := eventsourcing.NewEncodedEvent(
		eventsourcing.EncodedEventInput{
			Name:        headers[HeaderEventName],
			Version:     eventsourcing.SchemaVersion(schemaVersion),
			ContentType: headers[HeaderContentType],
			Payload:     record.Value,
		},
	)
	if err != nil {
		return eventsourcing.Delivery{}, ErrRecordCorrupt
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:            headers[HeaderMessageID],
			Stream:        stream,
			Event:         event,
			Metadata:      metadata,
			RecordedAt:    recordedAt,
			CorrelationID: headers[HeaderCorrelationID],
			CausationID:   headers[HeaderCausationID],
			Tenant:        headers[HeaderTenant],
			Partition:     headers[HeaderPartition],
		},
	)
	if err != nil {
		return eventsourcing.Delivery{}, ErrRecordCorrupt
	}
	message, _ := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:        pending,
		StreamVersion:  streamVersion,
		GlobalPosition: eventsourcing.GlobalPosition(globalPosition),
	})

	mode, err := parseDeliveryMode(headers[HeaderDeliveryMode])
	if err != nil {
		return eventsourcing.Delivery{}, err
	}
	delivery, _ := eventsourcing.NewDelivery(message, mode)

	return delivery, nil
}

func resolveTopic(
	resolver TopicResolver,
	message eventsourcing.Message,
) (topic string, err error) {
	defer func() {
		if recover() != nil {
			topic = ""
			err = ErrResolverPanic
		}
	}()

	return resolver.ResolveTopic(message)
}

func header(key, value string) kafka.Header {
	return kafka.Header{Key: key, Value: []byte(value)}
}

func headerUint64(key string, value uint64) kafka.Header {
	return header(key, strconv.FormatUint(value, 10))
}

func parseHeaders(input []kafka.Header) (map[string]string, error) {
	headers := make(map[string]string, len(input))
	lastReservedOrder := -1
	for _, item := range input {
		if !strings.HasPrefix(item.Key, reservedPrefix) {
			continue
		}
		if strings.ContainsRune(item.Key, '\x00') {
			return nil, ErrRecordCorrupt
		}
		order := canonicalHeaderOrder(item.Key)
		if order < 0 || order <= lastReservedOrder || len(item.Value) == 0 {
			return nil, ErrRecordCorrupt
		}
		headers[item.Key] = string(item.Value)
		lastReservedOrder = order
	}
	for _, required := range []string{
		HeaderWireVersion,
		HeaderMessageID,
		HeaderAggregateType,
		HeaderAggregateID,
		HeaderStreamVersion,
		HeaderEventName,
		HeaderEventSchemaVersion,
		HeaderContentType,
		HeaderRecordedAt,
		HeaderApplicationMetadata,
		HeaderDeliveryMode,
	} {
		if headers[required] == "" {
			return nil, ErrRecordCorrupt
		}
	}
	if headers[HeaderWireVersion] != wireVersionV1 {
		return nil, ErrRecordCorrupt
	}

	return headers, nil
}

func canonicalHeaderOrder(key string) int {
	return strings.Index(canonicalHeaderSequence, "\x00"+key+"\x00")
}

func requiredUint64(headers map[string]string, key string) (uint64, error) {
	value := headers[key]
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 || strconv.FormatUint(parsed, 10) != value {
		return 0, ErrRecordCorrupt
	}

	return parsed, nil
}

func requiredUint32(headers map[string]string, key string) (uint32, error) {
	value, err := requiredUint64(headers, key)
	if err != nil || value > uint64(^uint32(0)) {
		return 0, ErrRecordCorrupt
	}

	return uint32(value), nil
}

func optionalUint64(headers map[string]string, key string) (uint64, error) {
	if headers[key] == "" {
		return 0, nil
	}

	return requiredUint64(headers, key)
}

func canonicalTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil ||
		parsed.IsZero() ||
		parsed.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano) !=
			value {
		return time.Time{}, ErrRecordCorrupt
	}

	return parsed, nil
}

func canonicalMetadata(input []byte) (map[string]string, error) {
	var metadata map[string]string
	if err := json.Unmarshal(input, &metadata); err != nil ||
		metadata == nil {
		return nil, ErrRecordCorrupt
	}
	canonical, err := json.Marshal(metadata)
	if err != nil || !bytes.Equal(canonical, input) {
		return nil, ErrRecordCorrupt
	}

	return metadata, nil
}

func parseDeliveryMode(value string) (eventsourcing.DeliveryMode, error) {
	switch value {
	case "live":
		return eventsourcing.DeliveryLive, nil
	case "replay":
		return eventsourcing.DeliveryReplay, nil
	default:
		return 0, ErrRecordCorrupt
	}
}

func validateRecord(
	topic string,
	key []byte,
	value []byte,
	headers []kafka.Header,
	limits kafka.MessageLimits,
) error {
	if !validTopic(topic, limits.MaxTopicBytes) ||
		len(key) == 0 ||
		len(key) > limits.MaxKeyBytes ||
		len(value) == 0 ||
		len(value) > limits.MaxValueBytes ||
		len(headers) > limits.MaxHeaders {
		return ErrRecordCorrupt
	}

	total := 0
	for _, item := range headers {
		next, valid := nextHeaderByteTotal(total, item, limits)
		if !valid {
			return ErrRecordCorrupt
		}
		total = next
	}

	return nil
}

func nextHeaderByteTotal(
	total int,
	item kafka.Header,
	limits kafka.MessageLimits,
) (int, bool) {
	if item.Key == "" ||
		len(item.Key) > limits.MaxHeaderKeyBytes ||
		len(item.Value) > limits.MaxHeaderValueBytes {
		return 0, false
	}
	size := len(item.Key) + len(item.Value)
	if size > limits.MaxHeaderBytes-total {
		return 0, false
	}

	return total + size, true
}

func validTopic(topic string, maximum int) bool {
	if topic == "" ||
		len(topic) > maximum ||
		topic == "." ||
		topic == ".." {
		return false
	}
	for index := range len(topic) {
		character := topic[index]
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') &&
			character != '.' &&
			character != '_' &&
			character != '-' {
			return false
		}
	}

	return true
}

func recordFailure(category, cause error) error {
	if cause == nil || errors.Is(category, cause) {
		return category
	}

	return &RecordError{category: category, cause: cause}
}
