package gokafka_test

import (
	"reflect"
	"testing"

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
				Payload: []byte(`{"name":"hyvää päivää"}`),
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
