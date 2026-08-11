package gokafka

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/kafka"
)

type wireGoldenRecord struct {
	Topic     string             `json:"topic"`
	Key       string             `json:"key"`
	Value     string             `json:"value"`
	Timestamp string             `json:"timestamp"`
	Headers   []wireGoldenHeader `json:"headers"`
}

type wireGoldenHeader struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func TestRecordCodecMatchesFrozenWireVersionOneRecords(t *testing.T) {
	t.Parallel()

	codec, err := NewRecordCodec(RecordCodecConfig{
		Resolver:      FixedTopic("accounts.events.v1"),
		AllowedTopics: []string{"accounts.events.v1"},
	})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}

	tests := []struct {
		name    string
		golden  string
		message eventsourcing.Message
		mode    eventsourcing.DeliveryMode
	}{
		{
			name:    "complete replay",
			golden:  "testdata/wire/v1_complete_replay.json",
			message: testMessage(t),
			mode:    eventsourcing.DeliveryReplay,
		},
		{
			name:    "minimal live",
			golden:  "testdata/wire/v1_minimal_live.json",
			message: minimalWireMessage(t),
			mode:    eventsourcing.DeliveryLive,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			golden := readWireGoldenRecord(t, test.golden)
			delivery, err := eventsourcing.NewDelivery(test.message, test.mode)
			if err != nil {
				t.Fatalf("construct delivery: %v", err)
			}
			encoded, err := codec.Encode(delivery)
			if err != nil {
				t.Fatalf("encode delivery: %v", err)
			}
			want := golden.kafkaMessage(t)
			if !equalKafkaMessage(encoded, want) {
				t.Fatalf("encoded record = %#v, want %#v", encoded, want)
			}

			decoded, err := codec.Decode(consumedRecord(want))
			if err != nil {
				t.Fatalf("decode frozen record: %v", err)
			}
			if decoded.Mode() != test.mode || !decoded.Message().Equal(test.message) {
				t.Fatalf("decoded delivery = %#v", decoded)
			}
		})
	}
}

func readWireGoldenRecord(t testing.TB, path string) wireGoldenRecord {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden record: %v", err)
	}
	var record wireGoldenRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("decode golden record: %v", err)
	}

	return record
}

func (record wireGoldenRecord) kafkaMessage(t testing.TB) kafka.Message {
	t.Helper()

	timestamp, err := time.Parse(time.RFC3339Nano, record.Timestamp)
	if err != nil {
		t.Fatalf("parse golden timestamp: %v", err)
	}
	headers := make([]kafka.Header, len(record.Headers))
	for index, header := range record.Headers {
		headers[index] = kafka.Header{Key: header.Key, Value: []byte(header.Value)}
	}

	return kafka.Message{
		Topic:     record.Topic,
		Key:       []byte(record.Key),
		Value:     []byte(record.Value),
		Headers:   headers,
		Timestamp: timestamp,
	}
}

func equalKafkaMessage(left, right kafka.Message) bool {
	return left.Topic == right.Topic &&
		slices.Equal(left.Key, right.Key) &&
		slices.Equal(left.Value, right.Value) &&
		left.Timestamp.Equal(right.Timestamp) &&
		slices.EqualFunc(left.Headers, right.Headers, func(
			leftHeader kafka.Header,
			rightHeader kafka.Header,
		) bool {
			return leftHeader.Key == rightHeader.Key &&
				slices.Equal(leftHeader.Value, rightHeader.Value)
		})
}

func minimalWireMessage(t testing.TB) eventsourcing.Message {
	t.Helper()

	stream, err := eventsourcing.NewStreamID("account", "account-minimal")
	if err != nil {
		t.Fatalf("construct stream: %v", err)
	}
	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "account.opened",
		Version:     1,
		ContentType: "application/json",
		Payload:     []byte("{}"),
	})
	if err != nil {
		t.Fatalf("construct event: %v", err)
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:         "msg-minimal",
			Stream:     stream,
			Event:      event,
			RecordedAt: time.Date(2026, time.July, 25, 10, 11, 12, 0, time.UTC),
		},
	)
	if err != nil {
		t.Fatalf("construct pending message: %v", err)
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:       pending,
		StreamVersion: 1,
	})
	if err != nil {
		t.Fatalf("construct message: %v", err)
	}

	return message
}
