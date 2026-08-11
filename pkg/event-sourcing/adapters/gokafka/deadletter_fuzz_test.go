package gokafka

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
)

func FuzzDeadLetterPolicy(f *testing.F) {
	f.Add(
		"accounts.events",
		[]byte("aggregate-1"),
		[]byte("value"),
		int32(0),
		int64(1),
		int64(1_753_443_296_000_000_000),
		encodeFuzzHeaders([]kafka.Header{{
			Key:   "traceparent",
			Value: []byte("value"),
		}}),
	)
	f.Add(
		"accounts.events.dead-letter",
		[]byte{},
		[]byte{0xff},
		int32(-1),
		int64(-1),
		int64(0),
		encodeFuzzHeaders([]kafka.Header{{
			Key:   HeaderDeadLetterSourceOffset,
			Value: []byte{0xff},
		}}),
	)

	f.Fuzz(func(
		t *testing.T,
		topic string,
		key []byte,
		value []byte,
		partition int32,
		offset int64,
		unixNano int64,
		headerBytes []byte,
	) {
		publisher := &fuzzCapturingPublisher{}
		policy, err := NewDeadLetterPolicy(
			publisher,
			DeadLetterPolicyConfig{Topic: "accounts.events.dead-letter"},
		)
		if err != nil {
			t.Fatal(err)
		}
		record := kafka.ConsumedMessage{
			Topic:     topic,
			Key:       slices.Clone(key),
			Value:     slices.Clone(value),
			Headers:   decodeFuzzHeaders(headerBytes),
			Timestamp: time.Unix(0, unixNano).UTC(),
			Partition: partition,
			Offset:    offset,
		}
		disposition, err := policy.HandleFailure(
			context.Background(),
			record,
			errors.New("failed"),
		)
		if err == nil {
			if disposition != FailureHandled {
				t.Fatalf("successful disposition = %v", disposition)
			}
			if len(publisher.messages) != 1 {
				t.Fatalf("successful publications = %d", len(publisher.messages))
			}
			published := publisher.messages[0]
			if published.Topic != "accounts.events.dead-letter" ||
				!slices.Equal(published.Key, key) ||
				!slices.Equal(published.Value, value) ||
				!published.Timestamp.Equal(record.Timestamp) {
				t.Fatalf("published record changed source identity: %#v", published)
			}

			return
		}
		if disposition != FailureRetry || err.Error() == "" {
			t.Fatalf("failed disposition = %v, error = %v", disposition, err)
		}
	})
}

type fuzzCapturingPublisher struct {
	messages []kafka.Message
}

func (publisher *fuzzCapturingPublisher) Publish(
	_ context.Context,
	message kafka.Message,
) error {
	publisher.messages = append(publisher.messages, message)

	return nil
}
