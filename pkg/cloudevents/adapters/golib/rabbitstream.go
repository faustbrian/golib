package golib

import (
	"bytes"
	"fmt"
	"mime"
	"strings"
	"time"

	"github.com/faustbrian/golib/pkg/cloudevents"
	"github.com/faustbrian/golib/pkg/rabbitstream"
)

// RabbitStreamTransport is caller-owned routing and correlation metadata for
// one structured CloudEvent. Exactly one stream target is required.
type RabbitStreamTransport struct {
	// Stream selects one direct RabbitMQ Stream.
	Stream string
	// SuperStream selects one logical RabbitMQ Super Stream.
	SuperStream string
	// RoutingKey selects a Super Stream partition and must agree with any event partition key.
	RoutingKey string
	// CorrelationID is transported outside the CloudEvents context.
	CorrelationID string
	// Properties are copied into RabbitMQ application properties.
	Properties []rabbitstream.MetadataEntry
}

// RabbitStreamMessage contains a decoded CloudEvent and independently owned
// transport application properties.
type RabbitStreamMessage struct {
	// Event is the decoded structured CloudEvent.
	Event cloudevents.Event
	// TransportProperties are independently owned RabbitMQ application properties.
	TransportProperties []rabbitstream.MetadataEntry
}

// RabbitStreamState is broker and routing state that is not promoted into the
// CloudEvents context.
type RabbitStreamState struct {
	// Stream is the direct or backing stream recorded by the transport.
	Stream string
	// SuperStream is the logical stream recorded by the transport.
	SuperStream string
	// Partition is the backing stream for a consumed Super Stream message.
	Partition string
	// RoutingKey is the transport routing key.
	RoutingKey string
	// CorrelationID is the transport correlation identifier.
	CorrelationID string
	// PublishingID is the RabbitMQ publishing identifier when present.
	PublishingID uint64
	// HasPublishingID distinguishes an absent identifier from identifier zero.
	HasPublishingID bool
	// Offset is the broker delivery offset when present.
	Offset uint64
	// HasOffset distinguishes an absent offset from offset zero.
	HasOffset bool
	// Timestamp is the transport timestamp.
	Timestamp time.Time
}

// EncodeRabbitStream uses the normative structured JSON event format. It does
// not claim or invent a CloudEvents binary AMQP binding. Rabbitstream's
// conservative default limits are applied before transport metadata is copied.
func EncodeRabbitStream(
	event cloudevents.Event,
	transport RabbitStreamTransport,
) (rabbitstream.Message, error) {
	if (transport.Stream == "") == (transport.SuperStream == "") {
		return rabbitstream.Message{}, fmt.Errorf("%w: RabbitStream target", ErrMetadataCollision)
	}
	routingKey := transport.RoutingKey
	if partitionKey, present := cloudevents.KafkaPartitionKey(event); present {
		if routingKey != "" && routingKey != string(partitionKey) {
			return rabbitstream.Message{}, fmt.Errorf("%w: RabbitStream routing key", ErrMetadataCollision)
		}
		routingKey = string(partitionKey)
	}
	payload, err := cloudevents.EncodeJSON(event)
	if err != nil {
		return rabbitstream.Message{}, err
	}
	timestamp, _ := event.Time()
	message := rabbitstream.Message{
		Stream: transport.Stream, SuperStream: transport.SuperStream,
		RoutingKey: routingKey, MessageID: event.ID(), CorrelationID: transport.CorrelationID,
		Timestamp: timestamp, ContentType: cloudevents.JSONMediaType,
		Payload: payload, Properties: transport.Properties,
	}
	if err := message.Validate(rabbitstream.DefaultLimits()); err != nil {
		return rabbitstream.Message{}, err
	}
	message.Properties = cloneRabbitStreamMetadata(transport.Properties)
	return message, nil
}

// DecodeRabbitStream decodes one structured JSON CloudEvent while retaining
// broker state and application properties outside the event context. It
// applies rabbitstream's conservative default limits before copying transport
// metadata; configured consumers can enforce stricter limits before calling.
func DecodeRabbitStream(
	message rabbitstream.Message,
	limits cloudevents.Limits,
) (RabbitStreamMessage, RabbitStreamState, error) {
	if err := validateRabbitStreamMessage(message); err != nil {
		return RabbitStreamMessage{}, RabbitStreamState{}, err
	}
	mediaType, _, err := mime.ParseMediaType(message.ContentType)
	if err != nil || !strings.EqualFold(mediaType, cloudevents.JSONMediaType) {
		return RabbitStreamMessage{}, RabbitStreamState{}, cloudevents.ErrUnsupportedMode
	}
	event, err := cloudevents.DecodeJSON(message.Payload, limits)
	if err != nil {
		return RabbitStreamMessage{}, RabbitStreamState{}, err
	}
	if message.MessageID != "" && message.MessageID != event.ID() {
		return RabbitStreamMessage{}, RabbitStreamState{}, fmt.Errorf("%w: RabbitStream message ID", ErrMetadataCollision)
	}
	if partitionKey, present := cloudevents.KafkaPartitionKey(event); present &&
		message.RoutingKey != "" && !bytes.Equal(partitionKey, []byte(message.RoutingKey)) {
		return RabbitStreamMessage{}, RabbitStreamState{}, fmt.Errorf("%w: RabbitStream routing key", ErrMetadataCollision)
	}
	decoded := RabbitStreamMessage{
		Event: event, TransportProperties: cloneRabbitStreamMetadata(message.Properties),
	}
	state := RabbitStreamState{
		Stream: message.Stream, SuperStream: message.SuperStream, Partition: message.Partition,
		RoutingKey: message.RoutingKey, CorrelationID: message.CorrelationID,
		PublishingID: message.PublishingID, HasPublishingID: message.HasPublishingID,
		Offset: message.Offset, HasOffset: message.HasOffset, Timestamp: message.Timestamp,
	}
	return decoded, state, nil
}

func validateRabbitStreamMessage(message rabbitstream.Message) error {
	limits := rabbitstream.DefaultLimits()
	if message.Partition != "" || message.HasOffset || message.Offset != 0 {
		return message.ValidateDelivery(limits)
	}
	return message.Validate(limits)
}

func cloneRabbitStreamMetadata(entries []rabbitstream.MetadataEntry) []rabbitstream.MetadataEntry {
	if entries == nil {
		return nil
	}
	cloned := make([]rabbitstream.MetadataEntry, len(entries))
	for index, entry := range entries {
		cloned[index] = rabbitstream.MetadataEntry{Key: entry.Key, Value: cloneBytes(entry.Value)}
	}
	return cloned
}
