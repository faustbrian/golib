package gokafka_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/gokafka"
)

func FuzzPublisherEnvelopeMapping(f *testing.F) {
	f.Add(
		"event-1", "events", "aggregate-1", "event-1",
		"traceparent", "trace-1", "application/json", uint16(1), []byte("{}"), false,
		uint8(kafka.ErrorRetryable), "broker restart",
	)
	f.Add("", "", "", "", "", "", "", uint16(0), []byte(nil), false, uint8(0), "")
	f.Add("event-1", "events", "", "", "event-id", "forged", "", uint16(1), []byte("x"), false, uint8(kafka.ErrorAuthorization), "authorization")
	f.Add("event-1", "events", "", "", "", "", "", uint16(1), []byte("x"), true, uint8(kafka.ErrorAmbiguous), "callback panic")

	f.Fuzz(func(
		t *testing.T,
		id string,
		topic string,
		orderingKey string,
		idempotencyKey string,
		metadataKey string,
		metadataValue string,
		contentType string,
		version uint16,
		payload []byte,
		panicClient bool,
		producerCategory uint8,
		producerResult string,
	) {
		if len(id)+len(topic)+len(orderingKey)+len(idempotencyKey)+
			len(metadataKey)+len(metadataValue)+len(contentType)+len(payload)+
			len(producerResult) > 1<<16 {
			return
		}
		metadata := map[string]string{metadataKey: metadataValue}
		if contentType != "" {
			metadata["es.content_type"] = contentType
		}
		envelope := outbox.Envelope{
			ID: id, Topic: topic, OrderingKey: orderingKey,
			IdempotencyKey: idempotencyKey, PayloadVersion: version,
			Payload: payload, Metadata: metadata,
		}
		if panicClient {
			publisher, err := gokafka.New(panickingClient{})
			if err != nil {
				t.Fatal(err)
			}
			_ = publisher.Publish(context.Background(), envelope)
			return
		}

		first := &recordingClient{}
		publisher, err := gokafka.New(first)
		if err != nil {
			t.Fatal(err)
		}
		publishErr := publisher.Publish(context.Background(), envelope)
		if publishErr != nil {
			if first.calls != 0 {
				t.Fatalf("rejected envelope reached client %d times", first.calls)
			}
			return
		}
		if first.calls != 1 || first.message.Topic != topic ||
			!slices.Equal(first.message.Value, payload) {
			t.Fatalf("mapped message = %#v", first.message)
		}
		wantKey := orderingKey
		if wantKey == "" {
			wantKey = idempotencyKey
		}
		if wantKey == "" {
			wantKey = id
		}
		if string(first.message.Key) != wantKey {
			t.Fatalf("mapped key = %q, want %q", first.message.Key, wantKey)
		}

		second := &recordingClient{}
		secondPublisher, err := gokafka.New(second)
		if err != nil {
			t.Fatal(err)
		}
		if err := secondPublisher.Publish(context.Background(), envelope); err != nil {
			t.Fatalf("repeat Publish() error = %v", err)
		}
		if first.message.Topic != second.message.Topic ||
			!slices.Equal(first.message.Key, second.message.Key) ||
			!slices.Equal(first.message.Value, second.message.Value) ||
			!slices.EqualFunc(first.message.Headers, second.message.Headers, func(left, right kafka.Header) bool {
				return left.Key == right.Key && slices.Equal(left.Value, right.Value)
			}) {
			t.Fatalf("mapping was not deterministic: %#v / %#v", first.message, second.message)
		}
		if len(payload) != 0 {
			first.message.Value[0] ^= 0xff
			if !slices.Equal(second.message.Value, payload) {
				t.Fatal("one client mutation changed another mapped payload")
			}
		}

		category := kafka.ErrorCategory(producerCategory)
		diagnostic := "producer-secret[" + producerResult + "]"
		cause := categorizedError{category: category, message: diagnostic}
		failingPublisher, err := gokafka.New(&recordingClient{publishErr: cause})
		if err != nil {
			t.Fatal(err)
		}
		publishErr = failingPublisher.Publish(context.Background(), envelope)
		var categorized interface{ Category() kafka.ErrorCategory }
		if !errors.Is(publishErr, cause) || !errors.As(publishErr, &categorized) ||
			categorized.Category() != category {
			t.Fatalf("producer result was not preserved: %v", publishErr)
		}
		if strings.Contains(publishErr.Error(), diagnostic) {
			t.Fatal("producer diagnostic was rendered")
		}
	})
}
