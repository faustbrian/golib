package gokafka_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/gokafka"
)

func TestPublisherGoldenKafkaRecords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		envelope outbox.Envelope
		want     kafka.Message
	}{
		{
			name: "all fields and sorted event sourcing metadata",
			envelope: outbox.Envelope{
				ID: "event-1", Topic: "events.v1", OrderingKey: "stream-1",
				IdempotencyKey: "command-1", PayloadVersion: 42,
				Payload:  []byte(`{"name":"hyvää päivää"}`),
				Attempts: 7, AvailableAt: time.Unix(1_234, 567).UTC(),
				CreatedAt: time.Unix(1_000, 123).UTC(),
				Metadata: map[string]string{
					"z-last":          "雪",
					"es.content_type": "application/vnd.events+json",
					"a-first":         "first",
				},
			},
			want: kafka.Message{
				Topic: "events.v1", Key: []byte("stream-1"),
				Value: []byte(`{"name":"hyvää päivää"}`),
				Headers: []kafka.Header{
					{Key: "content-type", Value: []byte("application/vnd.events+json")},
					{Key: "event-id", Value: []byte("event-1")},
					{Key: "schema-version", Value: []byte("42")},
					{Key: "idempotency-key", Value: []byte("command-1")},
					{Key: "a-first", Value: []byte("first")},
					{Key: "es.content_type", Value: []byte("application/vnd.events+json")},
					{Key: "z-last", Value: []byte("雪")},
				},
			},
		},
		{
			name: "nil payload and event ID fallback",
			envelope: outbox.Envelope{
				ID: "event-2", Topic: "events.v1", PayloadVersion: 1,
			},
			want: kafka.Message{
				Topic: "events.v1", Key: []byte("event-2"), Value: nil,
				Headers: []kafka.Header{
					{Key: "content-type", Value: []byte("application/json")},
					{Key: "event-id", Value: []byte("event-2")},
					{Key: "schema-version", Value: []byte("1")},
				},
			},
		},
		{
			name: "empty payload metadata value and idempotency fallback",
			envelope: outbox.Envelope{
				ID: "event-3", Topic: "events.v1", IdempotencyKey: "command-3",
				PayloadVersion: 1, Payload: []byte{},
				Metadata: map[string]string{"empty": "", "es.content_type": ""},
			},
			want: kafka.Message{
				Topic: "events.v1", Key: []byte("command-3"), Value: []byte{},
				Headers: []kafka.Header{
					{Key: "content-type", Value: []byte("application/json")},
					{Key: "event-id", Value: []byte("event-3")},
					{Key: "schema-version", Value: []byte("1")},
					{Key: "idempotency-key", Value: []byte("command-3")},
					{Key: "empty", Value: []byte{}},
					{Key: "es.content_type", Value: []byte{}},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			client := &recordingClient{}
			publisher, err := gokafka.New(client)
			if err != nil {
				t.Fatal(err)
			}
			if err := publisher.Publish(t.Context(), test.envelope); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(client.message, test.want) {
				t.Fatalf("Kafka record = %#v, want %#v", client.message, test.want)
			}
		})
	}
}

func TestPublisherGoldenExactAcceptedBoundaries(t *testing.T) {
	t.Parallel()

	want := kafka.Message{
		Topic: "topic", Key: []byte("key1"), Value: []byte("data"),
		Headers: []kafka.Header{
			{Key: "content-type", Value: []byte("application/json")},
			{Key: "event-id", Value: []byte("id-1")},
			{Key: "schema-version", Value: []byte("1")},
			{Key: "idempotency-key", Value: []byte("idem")},
			{Key: "a", Value: []byte("12")},
			{Key: "b", Value: []byte("34")},
		},
	}
	headerBytes := 0
	for _, header := range want.Headers {
		headerBytes += len(header.Key) + len(header.Value)
	}
	limits := gokafka.Limits{
		Envelope: outbox.Limits{
			MaxIDBytes: 4, MaxTopicBytes: 5, MaxPayloadBytes: 4,
			MaxMetadataEntries: 2, MaxMetadataBytes: 6,
			MaxOrderingKeyBytes: 4, MaxIdempotencyKeyBytes: 4,
		},
		Kafka: kafka.MessageLimits{
			MaxTopicBytes: 5, MaxKeyBytes: 4, MaxValueBytes: 4,
			MaxHeaders: len(want.Headers), MaxHeaderKeyBytes: len("idempotency-key"),
			MaxHeaderValueBytes: len("application/json"), MaxHeaderBytes: headerBytes,
		},
	}
	client := &recordingClient{}
	publisher, err := gokafka.New(client, gokafka.WithLimits(limits))
	if err != nil {
		t.Fatal(err)
	}
	envelope := outbox.Envelope{
		ID: "id-1", Topic: "topic", Payload: []byte("data"), PayloadVersion: 1,
		Metadata:       map[string]string{"b": "34", "a": "12"},
		OrderingKey:    strings.Repeat("k", limits.Envelope.MaxOrderingKeyBytes),
		IdempotencyKey: strings.Repeat("i", limits.Envelope.MaxIdempotencyKeyBytes),
		Attempts:       99, AvailableAt: time.Unix(2_000, 1).UTC(),
		CreatedAt: time.Unix(1_000, 1).UTC(),
	}
	want.Key = []byte(envelope.OrderingKey)
	want.Headers[3].Value = []byte(envelope.IdempotencyKey)

	if err := publisher.Publish(t.Context(), envelope); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(client.message, want) {
		t.Fatalf("Kafka boundary record = %#v, want %#v", client.message, want)
	}
}
