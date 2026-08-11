package golib_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/cloudevents"
	golib "github.com/faustbrian/golib/pkg/cloudevents/adapters/golib"
	"github.com/faustbrian/golib/pkg/kafka"
)

func TestKafkaAdapterPreservesTransportOwnershipAndBindingRoundTrip(t *testing.T) {
	t.Parallel()

	partitionKey, err := cloudevents.NewPartitionKeyAttribute("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	data, err := cloudevents.NewJSONData([]byte(`{"ok":true}`))
	if err != nil {
		t.Fatal(err)
	}
	event, err := cloudevents.NewEvent(cloudevents.Attributes{
		ID: "event-1", Source: "/source", Type: "example.created", DataContentType: "application/json",
		Extensions: map[string]cloudevents.Attribute{"partitionkey": partitionKey},
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	timestamp := time.Date(2026, 8, 9, 4, 5, 6, 0, time.UTC)
	producer, err := golib.EncodeKafka(event, cloudevents.BinaryMode, golib.KafkaTransport{
		Topic: "events", Partition: kafka.ExplicitPartition(3), Timestamp: timestamp,
		Headers: []kafka.Header{{Key: "transport-attempt", Value: []byte("1")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if producer.Topic != "events" || producer.Partition != kafka.ExplicitPartition(3) ||
		!producer.Timestamp.Equal(timestamp) || string(producer.Key) != "tenant-a" {
		t.Fatalf("producer transport = %#v", producer)
	}

	consumed := kafka.ConsumedRecord{
		Topic: producer.Topic, Key: producer.Key, Value: producer.Value, Headers: producer.Headers,
		Timestamp: producer.Timestamp, TimestampType: kafka.TimestampCreateTime,
		Partition: 3, Offset: 42, LeaderEpoch: 7,
	}
	message, state, err := golib.DecodeKafka(consumed, cloudevents.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if message.Event.ID() != event.ID() || message.Mode != cloudevents.BinaryMode ||
		state.Topic != "events" || state.Partition != 3 || state.Offset != 42 ||
		len(message.TransportHeaders) != 1 || message.TransportHeaders[0].Key != "transport-attempt" {
		t.Fatalf("decoded Kafka mapping = %#v, %#v", message, state)
	}
	producer.Value[0] = 'X'
	if bytes.Equal(message.Event.Data().Bytes(), producer.Value) {
		t.Fatal("decoded event aliases producer value")
	}
}

func TestKafkaAdapterRejectsTransportCollisions(t *testing.T) {
	t.Parallel()

	event := baseEvent(t)
	if _, err := golib.EncodeKafka(event, cloudevents.BinaryMode, golib.KafkaTransport{
		Headers: []kafka.Header{{Key: "ce_id", Value: []byte("other")}},
	}); !errors.Is(err, golib.ErrMetadataCollision) {
		t.Fatalf("header collision error = %v", err)
	}
	partitionKey, err := cloudevents.NewPartitionKeyAttribute("event-key")
	if err != nil {
		t.Fatal(err)
	}
	withKey, err := cloudevents.NewEvent(cloudevents.Attributes{
		ID: "event-1", Source: "/source", Type: "example.created",
		Extensions: map[string]cloudevents.Attribute{"partitionkey": partitionKey},
	}, cloudevents.NewBinaryData([]byte("body")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := golib.EncodeKafka(withKey, cloudevents.BinaryMode, golib.KafkaTransport{Key: []byte("transport-key")}); !errors.Is(err, golib.ErrMetadataCollision) {
		t.Fatalf("key collision error = %v", err)
	}
}
