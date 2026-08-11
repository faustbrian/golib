package goqueue

import (
	"bytes"
	"errors"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/queue/job"
)

func TestCodecRoundTripsCanonicalCompleteDelivery(t *testing.T) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	delivery := queueDelivery(t, eventsourcing.DeliveryReplay)
	encoded, err := codec.Encode(delivery)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	const want = `{"format":"golib.event-sourcing.queue.v1","delivery_mode":"replay","message_id":"message-1","aggregate_type":"account","aggregate_id":"tenant/42","stream_version":7,"event_name":"account.opened","event_schema_version":2,"content_type":"application/octet-stream","payload":"AAEC/w==","metadata":{"a":"first","z":"last"},"recorded_at":"2026-07-25T12:34:56.123456Z","correlation_id":"correlation-1","causation_id":"causation-1","tenant":"tenant-1","partition":"eu-north","global_position":11}`
	if string(encoded) != want {
		t.Fatalf("Encode() = %s", encoded)
	}

	decoded, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if decoded.Mode() != eventsourcing.DeliveryReplay ||
		!decoded.Message().Equal(delivery.Message()) {
		t.Fatalf("Decode() = %#v", decoded)
	}
	encoded[0] = 'X'
	if decoded.Message().Event().Payload()[0] != 0 {
		t.Fatal("Decode() retained encoded input storage")
	}
}

func TestCodecRoundTripsAbsentOptionalFields(t *testing.T) {
	codec, err := NewCodec(CodecConfig{MaxEnvelopeBytes: 2_048})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	delivery := minimalQueueDelivery(t)
	encoded, err := codec.Encode(delivery)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	const want = `{"format":"golib.event-sourcing.queue.v1","delivery_mode":"live","message_id":"message-1","aggregate_type":"account","aggregate_id":"42","stream_version":1,"event_name":"account.opened","event_schema_version":1,"content_type":"application/json","payload":"e30=","recorded_at":"2026-07-25T12:34:56Z"}`
	if string(encoded) != want {
		t.Fatalf("Encode() = %s", encoded)
	}
	decoded, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !decoded.Message().Equal(delivery.Message()) ||
		decoded.Mode() != eventsourcing.DeliveryLive {
		t.Fatalf("Decode() = %#v", decoded)
	}
	for iteration := 0; iteration < 100; iteration++ {
		reencoded, encodeErr := codec.Encode(decoded)
		if encodeErr != nil || !bytes.Equal(reencoded, encoded) {
			t.Fatalf("Encode(iteration %d) = %s, %v", iteration, reencoded, encodeErr)
		}
	}
}

func TestCodecRejectsEveryUnsupportedWireVersion(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	const current = `{"format":"golib.event-sourcing.queue.v1","delivery_mode":"live","message_id":"message-1","aggregate_type":"account","aggregate_id":"42","stream_version":1,"event_name":"account.opened","event_schema_version":1,"content_type":"application/json","payload":"e30=","recorded_at":"2026-07-25T12:34:56Z"}`
	for _, version := range []string{
		"golib.event-sourcing.queue.v0",
		"golib.event-sourcing.queue.v2",
		"golib.event-sourcing.queue.v999999999999999999999999",
	} {
		encoded := strings.Replace(current, envelopeFormat, version, 1)
		if _, decodeErr := codec.Decode([]byte(encoded)); !errors.Is(
			decodeErr,
			ErrEnvelopeInvalid,
		) {
			t.Fatalf("Decode(%q) error = %v", version, decodeErr)
		}
	}
}

func TestCodecAcceptsExactEnvelopeLimit(t *testing.T) {
	delivery := minimalQueueDelivery(t)
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	encoded, err := codec.Encode(delivery)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	codec, err = NewCodec(CodecConfig{MaxEnvelopeBytes: len(encoded)})
	if err != nil {
		t.Fatalf("NewCodec(boundary) error = %v", err)
	}
	bounded, err := codec.Encode(delivery)
	if err != nil {
		t.Fatalf("Encode(boundary) error = %v", err)
	}
	if !bytes.Equal(bounded, encoded) {
		t.Fatalf("Encode(boundary) = %s, want %s", bounded, encoded)
	}
	if _, err := codec.Decode(bounded); err != nil {
		t.Fatalf("Decode(boundary) error = %v", err)
	}
}

func TestCodecValidatesConfigurationAndValues(t *testing.T) {
	for _, limit := range []int{-1, job.DefaultMaxMessageBytes + 1} {
		if _, err := NewCodec(CodecConfig{MaxEnvelopeBytes: limit}); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("NewCodec(%d) error = %v", limit, err)
		}
	}
	codec, err := NewCodec(CodecConfig{MaxEnvelopeBytes: 1})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	if _, err := codec.Encode(eventsourcing.Delivery{}); !errors.Is(err, ErrEnvelopeInvalid) {
		t.Fatalf("zero delivery error = %v", err)
	}
	if _, err := codec.Encode(minimalQueueDelivery(t)); !errors.Is(err, ErrEnvelopeTooLarge) {
		t.Fatalf("oversized encode error = %v", err)
	}
	if _, err := codec.Decode([]byte("{}")); !errors.Is(err, ErrEnvelopeTooLarge) {
		t.Fatalf("oversized decode error = %v", err)
	}

	var nilCodec *Codec
	if _, err := nilCodec.Encode(minimalQueueDelivery(t)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil Encode() error = %v", err)
	}
	if _, err := nilCodec.Decode(nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil Decode() error = %v", err)
	}
	codec, err = NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	if _, err := codec.Decode(nil); !errors.Is(err, ErrEnvelopeInvalid) {
		t.Fatalf("empty Decode() error = %v", err)
	}
}

func TestCodecRejectsNonCanonicalAndMalformedInput(t *testing.T) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	valid, err := codec.Encode(queueDelivery(t, eventsourcing.DeliveryLive))
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	tests := map[string][]byte{
		"malformed":      []byte("{"),
		"unknown field":  bytes.Replace(valid, []byte(`"format":`), []byte(`"unknown":1,"format":`), 1),
		"trailing value": append(append([]byte(nil), valid...), []byte(`{}`)...),
		"trailing bad":   append(append([]byte(nil), valid...), byte('{')),
		"whitespace":     append([]byte(" "), valid...),
		"duplicate": bytes.Replace(
			valid,
			[]byte(`"format":"golib.event-sourcing.queue.v1"`),
			[]byte(`"format":"golib.event-sourcing.queue.v1","format":"golib.event-sourcing.queue.v1"`),
			1,
		),
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := codec.Decode(input)
			if !errors.Is(err, ErrEnvelopeInvalid) {
				t.Fatalf("Decode() error = %v", err)
			}
			var envelopeErr *EnvelopeError
			if !errors.As(err, &envelopeErr) {
				t.Fatalf("Decode() error type = %T", err)
			}
			if strings.Contains(err.Error(), string(input)) {
				t.Fatalf("Decode() disclosed input: %v", err)
			}
		})
	}
}

func TestCodecPreservesTrailingSyntaxCause(t *testing.T) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	encoded, err := codec.Encode(minimalQueueDelivery(t))
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	_, err = codec.Decode(append(encoded, '{'))
	if !errors.Is(err, ErrEnvelopeInvalid) ||
		!errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Decode() error = %v, want preserved JSON syntax cause", err)
	}
}

func TestWireEnvelopeRejectsIncompatibleFields(t *testing.T) {
	valid := wireEnvelopeFromDelivery(t)
	tests := map[string]func(*wireEnvelope){
		"format": func(envelope *wireEnvelope) {
			envelope.Format = "other"
		},
		"schema zero": func(envelope *wireEnvelope) {
			envelope.EventSchemaVersion = 0
		},
		"schema overflow": func(envelope *wireEnvelope) {
			envelope.EventSchemaVersion = math.MaxUint32 + 1
		},
		"recorded malformed": func(envelope *wireEnvelope) {
			envelope.RecordedAt = "bad"
		},
		"recorded offset": func(envelope *wireEnvelope) {
			envelope.RecordedAt = "2026-07-25T15:34:56.123456+03:00"
		},
		"recorded nanoseconds": func(envelope *wireEnvelope) {
			envelope.RecordedAt = "2026-07-25T12:34:56.123456789Z"
		},
		"stream": func(envelope *wireEnvelope) {
			envelope.AggregateType = ""
		},
		"event": func(envelope *wireEnvelope) {
			envelope.Payload = nil
		},
		"pending": func(envelope *wireEnvelope) {
			envelope.MessageID = ""
		},
		"message": func(envelope *wireEnvelope) {
			envelope.StreamVersion = 0
		},
		"mode": func(envelope *wireEnvelope) {
			envelope.DeliveryMode = "unknown"
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			envelope := valid
			mutate(&envelope)
			if _, err := envelope.delivery(); !errors.Is(err, ErrEnvelopeInvalid) {
				t.Fatalf("delivery() error = %v", err)
			}
		})
	}
}

func TestWireEnvelopeAcceptsMaximumSchemaVersion(t *testing.T) {
	envelope := wireEnvelopeFromDelivery(t)
	envelope.EventSchemaVersion = math.MaxUint32
	delivery, err := envelope.delivery()
	if err != nil {
		t.Fatalf("delivery() error = %v", err)
	}
	if delivery.Message().Event().Version() !=
		eventsourcing.SchemaVersion(math.MaxUint32) {
		t.Fatalf(
			"schema version = %d, want %d",
			delivery.Message().Event().Version(),
			uint64(math.MaxUint32),
		)
	}
}

func TestEnvelopeErrorPreservesCategoryAndCauseWithoutDisclosure(t *testing.T) {
	cause := errors.New("secret")
	err := envelopeFailure(ErrEnvelopeInvalid, cause)
	if !errors.Is(err, ErrEnvelopeInvalid) ||
		!errors.Is(err, cause) ||
		strings.Contains(err.Error(), "secret") {
		t.Fatalf("envelopeFailure() = %v", err)
	}
	var envelopeErr *EnvelopeError
	if !errors.As(err, &envelopeErr) {
		t.Fatalf("envelopeFailure() type = %T", err)
	}
}

func queueDelivery(
	t testing.TB,
	mode eventsourcing.DeliveryMode,
) eventsourcing.Delivery {
	t.Helper()
	stream, err := eventsourcing.NewStreamID("account", "tenant/42")
	if err != nil {
		t.Fatal(err)
	}
	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "account.opened",
		Version:     2,
		ContentType: "application/octet-stream",
		Payload:     []byte{0, 1, 2, 255},
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:            "message-1",
			Stream:        stream,
			Event:         event,
			Metadata:      map[string]string{"z": "last", "a": "first"},
			RecordedAt:    time.Date(2026, 7, 25, 12, 34, 56, 123456000, time.UTC),
			CorrelationID: "correlation-1",
			CausationID:   "causation-1",
			Tenant:        "tenant-1",
			Partition:     "eu-north",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:        pending,
		StreamVersion:  7,
		GlobalPosition: 11,
	})
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := eventsourcing.NewDelivery(message, mode)
	if err != nil {
		t.Fatal(err)
	}
	return delivery
}

func minimalQueueDelivery(t testing.TB) eventsourcing.Delivery {
	t.Helper()
	stream, err := eventsourcing.NewStreamID("account", "42")
	if err != nil {
		t.Fatal(err)
	}
	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "account.opened",
		Version:     1,
		ContentType: "application/json",
		Payload:     []byte("{}"),
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:         "message-1",
			Stream:     stream,
			Event:      event,
			RecordedAt: time.Date(2026, 7, 25, 12, 34, 56, 0, time.UTC),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:       pending,
		StreamVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := eventsourcing.NewDelivery(
		message,
		eventsourcing.DeliveryLive,
	)
	if err != nil {
		t.Fatal(err)
	}
	return delivery
}

func queueDeliveryWithPayload(
	t testing.TB,
	payloadBytes int,
) eventsourcing.Delivery {
	return queueDeliveryWithPayloadAndTenant(t, payloadBytes, "")
}

func queueDeliveryWithPayloadAndTenant(
	t testing.TB,
	payloadBytes int,
	tenant string,
) eventsourcing.Delivery {
	t.Helper()
	stream, err := eventsourcing.NewStreamID("account", "42")
	if err != nil {
		t.Fatal(err)
	}
	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "account.opened",
		Version:     1,
		ContentType: "application/octet-stream",
		Payload:     make([]byte, payloadBytes),
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := eventsourcing.NewPendingMessage(eventsourcing.PendingMessageInput{
		ID:         "message-large",
		Stream:     stream,
		Event:      event,
		Tenant:     tenant,
		RecordedAt: time.Date(2026, 7, 25, 12, 34, 56, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:       pending,
		StreamVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := eventsourcing.NewDelivery(
		message,
		eventsourcing.DeliveryLive,
	)
	if err != nil {
		t.Fatal(err)
	}
	return delivery
}

func wireEnvelopeFromDelivery(t testing.TB) wireEnvelope {
	t.Helper()
	delivery := queueDelivery(t, eventsourcing.DeliveryLive)
	message := delivery.Message()
	event := message.Event()
	correlation, _ := message.CorrelationID()
	causation, _ := message.CausationID()
	tenant, _ := message.Tenant()
	partition, _ := message.Partition()
	position, _ := message.GlobalPosition()
	return wireEnvelope{
		Format:             envelopeFormat,
		DeliveryMode:       "live",
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
		CorrelationID:      correlation.String(),
		CausationID:        causation.String(),
		Tenant:             tenant,
		Partition:          partition,
		GlobalPosition:     uint64(position),
	}
}
