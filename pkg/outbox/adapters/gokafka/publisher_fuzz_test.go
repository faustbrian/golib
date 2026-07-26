package gokafka_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/gokafka"
)

func FuzzPublisherEnvelopeMapping(f *testing.F) {
	f.Add("event-1", "events", "aggregate-1", "event-1", uint16(1), []byte("{}"))
	f.Add("", "", "", "", uint16(0), []byte(nil))

	f.Fuzz(func(
		t *testing.T,
		id string,
		topic string,
		orderingKey string,
		idempotencyKey string,
		version uint16,
		payload []byte,
	) {
		if len(id)+len(topic)+len(orderingKey)+len(idempotencyKey)+len(payload) > 1<<20 {
			return
		}
		client := &recordingClient{}
		publisher, err := gokafka.New(client)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		_ = publisher.Publish(context.Background(), outbox.Envelope{
			ID: id, Topic: topic, OrderingKey: orderingKey,
			IdempotencyKey: idempotencyKey, PayloadVersion: version,
			Payload: payload,
		})
	})
}
