package goqueue_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/goqueue"
)

func FuzzPublisherEnvelope(f *testing.F) {
	f.Add("message-id", "topic", []byte(`{"value":1}`), "key", "value")
	f.Add("", "", []byte{}, "", "")

	f.Fuzz(func(t *testing.T, id, topic string, payload []byte, metadataKey, metadataValue string) {
		queue := &recordingQueue{}
		publisher, err := goqueue.New(queue)
		if err != nil {
			t.Fatalf("create publisher: %v", err)
		}
		if err := publisher.Publish(context.Background(), outbox.Envelope{
			ID: id, Topic: topic, Payload: payload,
			Metadata: map[string]string{metadataKey: metadataValue},
		}); err != nil {
			t.Fatalf("publish: %v", err)
		}
		if !json.Valid(queue.message.Bytes()) {
			t.Fatalf("invalid queued JSON: %q", queue.message.Bytes())
		}
	})
}

func BenchmarkPublisher(b *testing.B) {
	b.ReportAllocs()
	publisher, err := goqueue.New(&recordingQueue{})
	if err != nil {
		b.Fatalf("create publisher: %v", err)
	}
	envelope := outbox.Envelope{ID: "benchmark", Topic: "topic", Payload: []byte("payload")}

	for b.Loop() {
		if err := publisher.Publish(context.Background(), envelope); err != nil {
			b.Fatalf("publish: %v", err)
		}
	}
}
