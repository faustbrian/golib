package gokafka

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
)

func FuzzDeadLetterPolicy(f *testing.F) {
	f.Add(
		"accounts.events",
		int32(0),
		int64(1),
		int64(1_753_443_296),
		"traceparent",
		[]byte("value"),
	)
	f.Add(
		"accounts.events.dead-letter",
		int32(-1),
		int64(-1),
		int64(0),
		HeaderDeadLetterSourceOffset,
		[]byte{0xff},
	)

	policy, err := NewDeadLetterPolicy(
		discardPublisher{},
		DeadLetterPolicyConfig{Topic: "accounts.events.dead-letter"},
	)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(
		t *testing.T,
		topic string,
		partition int32,
		offset int64,
		unixTime int64,
		headerKey string,
		value []byte,
	) {
		record := kafka.ConsumedMessage{
			Topic:     topic,
			Key:       []byte("aggregate-1"),
			Value:     value,
			Headers:   []kafka.Header{{Key: headerKey, Value: value}},
			Timestamp: time.Unix(unixTime, 0),
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

			return
		}
		if disposition != FailureRetry || err.Error() == "" {
			t.Fatalf("failed disposition = %v, error = %v", disposition, err)
		}
	})
}
