package gokafka_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/gokafka"
)

func BenchmarkPublisherMapping(b *testing.B) {
	publisher, err := gokafka.New(&recordingClient{})
	if err != nil {
		b.Fatal(err)
	}
	envelope := outbox.Envelope{
		ID: "event-1", Topic: "track.tracking-event.v1",
		OrderingKey: "tracked-item-1", IdempotencyKey: "event-1",
		PayloadVersion: 1, Payload: []byte(`{"event_id":"event-1"}`),
	}

	b.ReportAllocs()
	for b.Loop() {
		if err := publisher.Publish(context.Background(), envelope); err != nil {
			b.Fatal(err)
		}
	}
}
