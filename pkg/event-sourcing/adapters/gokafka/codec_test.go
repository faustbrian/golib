package gokafka

import (
	"errors"
	"slices"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/kafka"
)

func TestNewRecordCodecRejectsKafkaInvalidAllowedTopics(t *testing.T) {
	t.Parallel()

	for _, topic := range []string{".", "..", "bad/topic", "töpïc"} {
		t.Run(topic, func(t *testing.T) {
			t.Parallel()

			_, err := NewRecordCodec(RecordCodecConfig{
				Resolver:      FixedTopic(topic),
				AllowedTopics: []string{topic},
			})
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("error = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestRecordCodecRoundTripsCompleteDelivery(t *testing.T) {
	t.Parallel()

	message := testMessage(t)
	codec, err := NewRecordCodec(RecordCodecConfig{
		Resolver:      FixedTopic("accounts.events.v1"),
		AllowedTopics: []string{"accounts.events.v1"},
	})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}
	delivery, err := eventsourcing.NewDelivery(
		message,
		eventsourcing.DeliveryReplay,
	)
	if err != nil {
		t.Fatalf("construct delivery: %v", err)
	}

	record, err := codec.Encode(delivery)
	if err != nil {
		t.Fatalf("encode delivery: %v", err)
	}

	if record.Topic != "accounts.events.v1" {
		t.Fatalf("topic = %q", record.Topic)
	}
	if string(record.Key) != "account-42" {
		t.Fatalf("key = %q", record.Key)
	}
	if string(record.Value) != `{"amount":9007199254740993}` {
		t.Fatalf("value = %q", record.Value)
	}
	wantTimestamp := message.RecordedAt().Truncate(time.Millisecond)
	if !record.Timestamp.Equal(wantTimestamp) {
		t.Fatalf("timestamp = %s, want %s", record.Timestamp, wantTimestamp)
	}
	expectedHeaders := []kafka.Header{
		{Key: HeaderWireVersion, Value: []byte("1")},
		{Key: HeaderMessageID, Value: []byte("msg-42")},
		{Key: HeaderAggregateType, Value: []byte("account")},
		{Key: HeaderAggregateID, Value: []byte("account-42")},
		{Key: HeaderStreamVersion, Value: []byte("7")},
		{Key: HeaderEventName, Value: []byte("account.credited")},
		{Key: HeaderEventSchemaVersion, Value: []byte("3")},
		{Key: HeaderContentType, Value: []byte("application/json")},
		{
			Key:   HeaderRecordedAt,
			Value: []byte("2026-07-25T10:11:12.123456Z"),
		},
		{Key: HeaderCorrelationID, Value: []byte("correlation-42")},
		{Key: HeaderCausationID, Value: []byte("causation-42")},
		{Key: HeaderTenant, Value: []byte("tenant-a")},
		{Key: HeaderPartition, Value: []byte("region-eu")},
		{Key: HeaderGlobalPosition, Value: []byte("19")},
		{
			Key:   HeaderApplicationMetadata,
			Value: []byte(`{"region":"eu","source":"test"}`),
		},
		{Key: HeaderDeliveryMode, Value: []byte("replay")},
	}
	if !slices.EqualFunc(
		record.Headers,
		expectedHeaders,
		func(left, right kafka.Header) bool {
			return left.Key == right.Key && slices.Equal(left.Value, right.Value)
		},
	) {
		t.Fatalf("headers = %#v", record.Headers)
	}

	decoded, err := codec.Decode(kafka.ConsumedMessage{
		Topic:     record.Topic,
		Key:       record.Key,
		Value:     record.Value,
		Headers:   record.Headers,
		Timestamp: record.Timestamp,
	})
	if err != nil {
		t.Fatalf("decode record: %v", err)
	}
	if decoded.Mode() != eventsourcing.DeliveryReplay {
		t.Fatalf("mode = %s", decoded.Mode())
	}
	if !decoded.Message().Equal(message) {
		t.Fatalf("decoded message = %s", decoded.Message())
	}
}

func testMessage(t testing.TB) eventsourcing.Message {
	t.Helper()

	stream, err := eventsourcing.NewStreamID("account", "account-42")
	if err != nil {
		t.Fatalf("construct stream: %v", err)
	}
	event, err := eventsourcing.NewEncodedEvent(
		eventsourcing.EncodedEventInput{
			Name:        "account.credited",
			Version:     3,
			ContentType: "application/json",
			Payload:     []byte(`{"amount":9007199254740993}`),
		},
	)
	if err != nil {
		t.Fatalf("construct event: %v", err)
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:       "msg-42",
			Stream:   stream,
			Event:    event,
			Metadata: map[string]string{"source": "test", "region": "eu"},
			RecordedAt: time.Date(
				2026,
				time.July,
				25,
				10,
				11,
				12,
				123456000,
				time.UTC,
			),
			CorrelationID: "correlation-42",
			CausationID:   "causation-42",
			Tenant:        "tenant-a",
			Partition:     "region-eu",
		},
	)
	if err != nil {
		t.Fatalf("construct pending message: %v", err)
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:        pending,
		StreamVersion:  7,
		GlobalPosition: 19,
	})
	if err != nil {
		t.Fatalf("construct message: %v", err)
	}

	return message
}
