package eventoutbox_test

import (
	"errors"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/adapters/outbox"
	"github.com/faustbrian/golib/pkg/outbox"
)

func TestEnvelopeCodecRoundTripsEveryMessageField(t *testing.T) {
	t.Parallel()

	codec, err := eventoutbox.NewEnvelopeCodec(
		eventoutbox.FixedTopic("account-events"),
		outbox.Limits{
			MaxIDBytes:             255,
			MaxTopicBytes:          255,
			MaxPayloadBytes:        eventsourcing.MaxPayloadBytes,
			MaxMetadataEntries:     32,
			MaxMetadataBytes:       eventsourcing.MaxMetadataBytes + 4096,
			MaxOrderingKeyBytes:    eventsourcing.MaxAggregateIDBytes,
			MaxIdempotencyKeyBytes: eventsourcing.MaxMessageIDBytes,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	message := testMessage(t)

	envelope, err := codec.Encode(message)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := codec.Decode(envelope)
	if err != nil {
		t.Fatal(err)
	}

	if !decoded.Equal(message) {
		t.Fatalf("decoded message = %s, want %s", decoded, message)
	}
	if envelope.ID != message.ID().String() ||
		envelope.Topic != "account-events" ||
		envelope.OrderingKey != message.Stream().AggregateID() ||
		envelope.IdempotencyKey != message.ID().String() ||
		envelope.PayloadVersion != eventoutbox.EnvelopePayloadVersion {
		t.Fatalf("envelope routing identity = %#v", envelope)
	}

	envelope.Payload[0] ^= 0xff
	envelope.Metadata[eventoutbox.MetadataAggregateType] = "changed"
	if !message.Equal(testMessage(t)) {
		t.Fatal("mutating envelope changed source message")
	}
}

func TestEnvelopeCodecRejectsTamperedIdentity(t *testing.T) {
	t.Parallel()

	codec, err := eventoutbox.NewEnvelopeCodec(
		eventoutbox.FixedTopic("account-events"),
		outbox.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := codec.Encode(testMessage(t))
	if err != nil {
		t.Fatal(err)
	}
	envelope.IdempotencyKey = "different"

	if _, err := codec.Decode(envelope); !errors.Is(
		err,
		eventoutbox.ErrEnvelopeCorrupt,
	) {
		t.Fatalf("decode error = %v, want ErrEnvelopeCorrupt", err)
	}
}

func testMessage(t *testing.T) eventsourcing.Message {
	t.Helper()

	stream, err := eventsourcing.NewStreamID("account", "account-1")
	if err != nil {
		t.Fatal(err)
	}
	event, err := eventsourcing.NewEncodedEvent(
		eventsourcing.EncodedEventInput{
			Name:        "account.opened",
			Version:     3,
			ContentType: "application/json",
			Payload:     []byte(`{"owner":"Ada"}`),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:            "message-1",
			Stream:        stream,
			Event:         event,
			Metadata:      map[string]string{"source": "test"},
			RecordedAt:    time.Date(2026, 7, 25, 12, 34, 56, 123456000, time.UTC),
			CorrelationID: "correlation-1",
			CausationID:   "causation-1",
			Tenant:        "tenant-1",
			Partition:     "partition-1",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:        pending,
		StreamVersion:  7,
		GlobalPosition: 19,
	})
	if err != nil {
		t.Fatal(err)
	}

	return message
}
