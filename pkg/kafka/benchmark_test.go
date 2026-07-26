package kafka

import (
	"context"
	"testing"
)

func BenchmarkMessageValidation(b *testing.B) {
	message := Message{
		Topic: "track.tracking-event.v1",
		Key:   []byte("tracked-item-1"),
		Value: []byte(`{"event_id":"event-1","schema_version":1}`),
		Headers: []Header{
			{Key: "content-type", Value: []byte("application/json")},
			{Key: "schema-version", Value: []byte("1")},
		},
	}
	limits := DefaultMessageLimits()

	b.ReportAllocs()
	for b.Loop() {
		if err := message.validate(limits); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFailureHandlerSuccess(b *testing.B) {
	ctx := context.Background()
	record := ConsumedMessage{
		Topic: "track.tracking-event.v1",
		Key:   []byte("tracked-item-1"),
		Value: []byte(`{"event_id":"event-1","schema_version":1}`),
		Headers: []Header{
			{Key: "content-type", Value: []byte("application/json")},
			{Key: "schema-version", Value: []byte("1")},
		},
	}
	direct := HandlerFunc(func(context.Context, ConsumedMessage) error {
		return nil
	})
	decorated, err := NewFailureHandler(FailureHandlerConfig{Handler: direct})
	if err != nil {
		b.Fatal(err)
	}

	b.Run("direct-handler", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := direct.Handle(ctx, record); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("failure-policy", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := decorated.Handle(ctx, record); err != nil {
				b.Fatal(err)
			}
		}
	})
}
