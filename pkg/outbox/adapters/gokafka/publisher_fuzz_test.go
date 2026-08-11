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
		"z-trace", "trace-1", "a-name", "åpnet", "application/json", uint16(1), []byte("{}"), false,
		uint8(kafka.ErrorRetryable), "broker restart",
	)
	f.Add("", "", "", "", "", "", "", "", "", uint16(0), []byte(nil), false, uint8(0), "")
	f.Add("event-1", "events", "", "", "event-id", "forged", "x", "y", "", uint16(1), []byte("x"), false, uint8(kafka.ErrorAuthorization), "authorization")
	f.Add("事件-1", "événements", "鍵", "冪等", "β", "雪", "α", "🙂", "application/例+json", uint16(1), []byte("payload-雪"), true, uint8(kafka.ErrorAmbiguous), "callback panic")

	f.Fuzz(func(
		t *testing.T,
		id string,
		topic string,
		orderingKey string,
		idempotencyKey string,
		metadataKey string,
		metadataValue string,
		secondMetadataKey string,
		secondMetadataValue string,
		contentType string,
		version uint16,
		payload []byte,
		panicClient bool,
		producerCategory uint8,
		producerResult string,
	) {
		limits := gokafka.DefaultLimits().Envelope
		if len(id) > limits.MaxIDBytes+1 || len(topic) > limits.MaxTopicBytes+1 ||
			len(orderingKey) > limits.MaxOrderingKeyBytes+1 ||
			len(idempotencyKey) > limits.MaxIdempotencyKeyBytes+1 ||
			len(metadataKey)+len(metadataValue) > limits.MaxMetadataBytes+1 ||
			len(secondMetadataKey)+len(secondMetadataValue) > limits.MaxMetadataBytes+1 ||
			len(contentType) > limits.MaxMetadataBytes+1 ||
			len(payload) > limits.MaxPayloadBytes+1 || len(producerResult) > 16<<10 {
			return
		}
		metadata := map[string]string{
			metadataKey:       metadataValue,
			secondMetadataKey: secondMetadataValue,
		}
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
		secondKey := slices.Clone(second.message.Key)
		secondValue := slices.Clone(second.message.Value)
		secondHeaders := make([]kafka.Header, len(second.message.Headers))
		for index, header := range second.message.Headers {
			secondHeaders[index] = kafka.Header{Key: header.Key, Value: slices.Clone(header.Value)}
		}
		if len(first.message.Key) != 0 {
			first.message.Key[0] ^= 0xff
		}
		if len(first.message.Value) != 0 {
			first.message.Value[0] ^= 0xff
		}
		for index := range first.message.Headers {
			if len(first.message.Headers[index].Value) != 0 {
				first.message.Headers[index].Value[0] ^= 0xff
			}
		}
		if !slices.Equal(second.message.Key, secondKey) ||
			!slices.Equal(second.message.Value, secondValue) ||
			!slices.EqualFunc(second.message.Headers, secondHeaders, func(left, right kafka.Header) bool {
				return left.Key == right.Key && slices.Equal(left.Value, right.Value)
			}) {
			t.Fatal("one client mutation changed another mapped record")
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
