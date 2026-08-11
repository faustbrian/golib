// Package goqueue maps event-sourcing deliveries to compatible queue
// backends without coupling queue behavior into the event-sourcing core.
package goqueue

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/queue/job"
)

const envelopeFormat = "golib.event-sourcing.queue.v1"

var (
	// ErrInvalidConfig reports an invalid queue envelope bound.
	ErrInvalidConfig = errors.New(
		"event-sourcing/goqueue: configuration is invalid",
	)
	// ErrEnvelopeInvalid reports malformed, non-canonical, or incompatible
	// queue input.
	ErrEnvelopeInvalid = errors.New(
		"event-sourcing/goqueue: envelope is invalid",
	)
	// ErrEnvelopeTooLarge reports an envelope outside the configured bound.
	ErrEnvelopeTooLarge = errors.New(
		"event-sourcing/goqueue: envelope exceeds configured limit",
	)
)

// CodecConfig defines one immutable queue envelope bound. A zero value selects
// queue/job.DefaultMaxMessageBytes.
type CodecConfig struct {
	MaxEnvelopeBytes int
}

// EnvelopeError exposes stable categories and causes without disclosing event
// identity, payload, metadata, or hostile input.
type EnvelopeError struct {
	category error
	cause    error
}

// Error implements error with a stable redacted diagnostic.
func (err *EnvelopeError) Error() string {
	return err.category.Error()
}

// Format keeps wrapped input and parser diagnostics redacted for every fmt
// representation, including Go-syntax formatting.
func (err *EnvelopeError) Format(state fmt.State, verb rune) {
	formatRedactedError(state, verb, err.Error())
}

// Unwrap preserves the stable category and original cause.
func (err *EnvelopeError) Unwrap() []error {
	if err.cause == nil {
		return []error{err.category}
	}
	return []error{err.category, err.cause}
}

// Codec maps complete persisted deliveries to canonical bounded JSON.
type Codec struct {
	maxEnvelopeBytes int
}

// NewCodec validates and constructs an immutable concurrency-safe codec.
func NewCodec(config CodecConfig) (*Codec, error) {
	if config.MaxEnvelopeBytes == 0 {
		config.MaxEnvelopeBytes = job.DefaultMaxMessageBytes
	}
	if config.MaxEnvelopeBytes < 1 ||
		config.MaxEnvelopeBytes > job.DefaultMaxMessageBytes {
		return nil, ErrInvalidConfig
	}
	return &Codec{maxEnvelopeBytes: config.MaxEnvelopeBytes}, nil
}

// Encode returns a canonical owned queue envelope.
func (codec *Codec) Encode(
	delivery eventsourcing.Delivery,
) ([]byte, error) {
	if codec == nil || codec.maxEnvelopeBytes < 1 {
		return nil, ErrInvalidConfig
	}
	if delivery.IsZero() {
		return nil, ErrEnvelopeInvalid
	}
	message := delivery.Message()
	event := message.Event()
	correlationID, _ := message.CorrelationID()
	causationID, _ := message.CausationID()
	tenant, _ := message.Tenant()
	partition, _ := message.Partition()
	globalPosition, _ := message.GlobalPosition()
	envelope := wireEnvelope{
		Format:             envelopeFormat,
		DeliveryMode:       delivery.Mode().String(),
		MessageID:          message.ID().String(),
		AggregateType:      message.Stream().AggregateType(),
		AggregateID:        message.Stream().AggregateID(),
		StreamVersion:      message.StreamVersion(),
		EventName:          event.Name().String(),
		EventSchemaVersion: uint64(event.Version()),
		ContentType:        event.ContentType(),
		Payload:            event.Payload(),
		Metadata:           message.Metadata(),
		RecordedAt:         message.RecordedAt().Format(time.RFC3339Nano),
		CorrelationID:      correlationID.String(),
		CausationID:        causationID.String(),
		Tenant:             tenant,
		Partition:          partition,
		GlobalPosition:     uint64(globalPosition),
	}
	encoded := encodeWireEnvelope(envelope)
	if len(encoded) > codec.maxEnvelopeBytes {
		return nil, envelopeFailure(ErrEnvelopeTooLarge, nil)
	}
	return encoded, nil
}

// Decode reconstructs one delivery only from this version's exact canonical
// encoding.
func (codec *Codec) Decode(encoded []byte) (eventsourcing.Delivery, error) {
	if codec == nil || codec.maxEnvelopeBytes < 1 {
		return eventsourcing.Delivery{}, ErrInvalidConfig
	}
	if len(encoded) == 0 {
		return eventsourcing.Delivery{}, ErrEnvelopeInvalid
	}
	if len(encoded) > codec.maxEnvelopeBytes {
		return eventsourcing.Delivery{}, envelopeFailure(
			ErrEnvelopeTooLarge,
			nil,
		)
	}

	var envelope wireEnvelope
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return eventsourcing.Delivery{}, envelopeFailure(
			ErrEnvelopeInvalid,
			err,
		)
	}
	if err := requireJSONEnd(decoder); err != nil {
		return eventsourcing.Delivery{}, envelopeFailure(
			ErrEnvelopeInvalid,
			err,
		)
	}
	canonical := encodeWireEnvelope(envelope)
	if !bytes.Equal(canonical, encoded) {
		return eventsourcing.Delivery{}, envelopeFailure(
			ErrEnvelopeInvalid,
			nil,
		)
	}
	return envelope.delivery()
}

type wireEnvelope struct {
	Format             string            `json:"format"`
	DeliveryMode       string            `json:"delivery_mode"`
	MessageID          string            `json:"message_id"`
	AggregateType      string            `json:"aggregate_type"`
	AggregateID        string            `json:"aggregate_id"`
	StreamVersion      uint64            `json:"stream_version"`
	EventName          string            `json:"event_name"`
	EventSchemaVersion uint64            `json:"event_schema_version"`
	ContentType        string            `json:"content_type"`
	Payload            []byte            `json:"payload"`
	Metadata           map[string]string `json:"metadata,omitempty"`
	RecordedAt         string            `json:"recorded_at"`
	CorrelationID      string            `json:"correlation_id,omitempty"`
	CausationID        string            `json:"causation_id,omitempty"`
	Tenant             string            `json:"tenant,omitempty"`
	Partition          string            `json:"partition,omitempty"`
	GlobalPosition     uint64            `json:"global_position,omitempty"`
}

func encodeWireEnvelope(envelope wireEnvelope) []byte {
	encoded := make([]byte, 0, 1_024)
	encoded = append(encoded, `{"format":`...)
	encoded = strconv.AppendQuoteToASCII(encoded, envelope.Format)
	encoded = append(encoded, `,"delivery_mode":`...)
	encoded = strconv.AppendQuoteToASCII(encoded, envelope.DeliveryMode)
	encoded = append(encoded, `,"message_id":`...)
	encoded = strconv.AppendQuoteToASCII(encoded, envelope.MessageID)
	encoded = append(encoded, `,"aggregate_type":`...)
	encoded = strconv.AppendQuoteToASCII(encoded, envelope.AggregateType)
	encoded = append(encoded, `,"aggregate_id":`...)
	encoded = strconv.AppendQuoteToASCII(encoded, envelope.AggregateID)
	encoded = append(encoded, `,"stream_version":`...)
	encoded = strconv.AppendUint(encoded, envelope.StreamVersion, 10)
	encoded = append(encoded, `,"event_name":`...)
	encoded = strconv.AppendQuoteToASCII(encoded, envelope.EventName)
	encoded = append(encoded, `,"event_schema_version":`...)
	encoded = strconv.AppendUint(encoded, envelope.EventSchemaVersion, 10)
	encoded = append(encoded, `,"content_type":`...)
	encoded = strconv.AppendQuoteToASCII(encoded, envelope.ContentType)
	encoded = append(encoded, `,"payload":"`...)
	encoded = base64.StdEncoding.AppendEncode(encoded, envelope.Payload)
	encoded = append(encoded, '"')
	if len(envelope.Metadata) != 0 {
		encoded = append(encoded, `,"metadata":{`...)
		keys := make([]string, 0, len(envelope.Metadata))
		for key := range envelope.Metadata {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for index, key := range keys {
			if index != 0 {
				encoded = append(encoded, ',')
			}
			encoded = strconv.AppendQuoteToASCII(encoded, key)
			encoded = append(encoded, ':')
			encoded = strconv.AppendQuoteToASCII(
				encoded,
				envelope.Metadata[key],
			)
		}
		encoded = append(encoded, '}')
	}
	encoded = append(encoded, `,"recorded_at":`...)
	encoded = strconv.AppendQuoteToASCII(encoded, envelope.RecordedAt)
	encoded = appendOptionalString(
		encoded,
		"correlation_id",
		envelope.CorrelationID,
	)
	encoded = appendOptionalString(
		encoded,
		"causation_id",
		envelope.CausationID,
	)
	encoded = appendOptionalString(encoded, "tenant", envelope.Tenant)
	encoded = appendOptionalString(encoded, "partition", envelope.Partition)
	if envelope.GlobalPosition != 0 {
		encoded = append(encoded, `,"global_position":`...)
		encoded = strconv.AppendUint(encoded, envelope.GlobalPosition, 10)
	}
	return append(encoded, '}')
}

func appendOptionalString(encoded []byte, key string, value string) []byte {
	if value == "" {
		return encoded
	}
	encoded = append(encoded, ',')
	encoded = strconv.AppendQuoteToASCII(encoded, key)
	encoded = append(encoded, ':')
	return strconv.AppendQuoteToASCII(encoded, value)
}

func (envelope wireEnvelope) delivery() (eventsourcing.Delivery, error) {
	if envelope.Format != envelopeFormat ||
		envelope.EventSchemaVersion == 0 ||
		envelope.EventSchemaVersion > math.MaxUint32 {
		return eventsourcing.Delivery{}, ErrEnvelopeInvalid
	}
	recordedAt, err := time.Parse(time.RFC3339Nano, envelope.RecordedAt)
	if err != nil ||
		recordedAt.Location() != time.UTC ||
		recordedAt.Nanosecond()%1_000 != 0 ||
		recordedAt.Format(time.RFC3339Nano) != envelope.RecordedAt {
		return eventsourcing.Delivery{}, ErrEnvelopeInvalid
	}
	stream, err := eventsourcing.NewStreamID(
		envelope.AggregateType,
		envelope.AggregateID,
	)
	if err != nil {
		return eventsourcing.Delivery{}, ErrEnvelopeInvalid
	}
	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        envelope.EventName,
		Version:     eventsourcing.SchemaVersion(envelope.EventSchemaVersion),
		ContentType: envelope.ContentType,
		Payload:     envelope.Payload,
	})
	if err != nil {
		return eventsourcing.Delivery{}, ErrEnvelopeInvalid
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:            envelope.MessageID,
			Stream:        stream,
			Event:         event,
			Metadata:      envelope.Metadata,
			RecordedAt:    recordedAt,
			CorrelationID: envelope.CorrelationID,
			CausationID:   envelope.CausationID,
			Tenant:        envelope.Tenant,
			Partition:     envelope.Partition,
		},
	)
	if err != nil {
		return eventsourcing.Delivery{}, ErrEnvelopeInvalid
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:        pending,
		StreamVersion:  envelope.StreamVersion,
		GlobalPosition: eventsourcing.GlobalPosition(envelope.GlobalPosition),
	})
	if err != nil {
		return eventsourcing.Delivery{}, ErrEnvelopeInvalid
	}
	mode, err := deliveryMode(envelope.DeliveryMode)
	if err != nil {
		return eventsourcing.Delivery{}, err
	}
	delivery, _ := eventsourcing.NewDelivery(message, mode)
	return delivery, nil
}

func deliveryMode(value string) (eventsourcing.DeliveryMode, error) {
	switch value {
	case "live":
		return eventsourcing.DeliveryLive, nil
	case "replay":
		return eventsourcing.DeliveryReplay, nil
	default:
		return 0, ErrEnvelopeInvalid
	}
}

func requireJSONEnd(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return ErrEnvelopeInvalid
		}
		return err
	}
	return nil
}

func envelopeFailure(category error, cause error) error {
	return &EnvelopeError{category: category, cause: cause}
}
