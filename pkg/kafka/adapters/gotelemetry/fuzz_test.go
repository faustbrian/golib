package gotelemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	kafka "github.com/faustbrian/golib/pkg/kafka"
)

func FuzzAttributePolicyValidate(f *testing.F) {
	f.Add("client", "orders", "workers")
	f.Add("", ".", "\x00")
	f.Add(string([]byte{0xff}), "orders/created", " group")

	f.Fuzz(func(t *testing.T, clientID string, topic string, groupID string) {
		policy := AttributePolicy{
			AllowedClientIDs:      []string{clientID},
			AllowedTopics:         []string{topic},
			AllowedConsumerGroups: []string{groupID},
		}
		err := policy.Validate()
		if err != nil && !errors.Is(err, ErrInvalidAttributePolicy) {
			t.Fatalf("Validate() error = %v", err)
		}
	})
}

func FuzzObserverValidation(f *testing.F) {
	f.Add(uint8(kafka.ObservationProduceRecord), int64(time.Millisecond), 1, 0, true, uint8(0))
	f.Add(uint8(255), int64(-1), -1, -1, false, uint8(255))

	instrumentation, err := New(Config{Runtime: completeTestRuntime()})
	if err != nil {
		f.Fatalf("New() error = %v", err)
	}
	observer := instrumentation.Observer()
	f.Fuzz(func(
		t *testing.T,
		kind uint8,
		duration int64,
		recordCount int,
		processedCount int,
		succeeded bool,
		category uint8,
	) {
		err := observer(context.Background(), kafka.Observation{
			Kind:           kafka.ObservationKind(kind),
			StartedAt:      time.Unix(1, 0),
			Duration:       time.Duration(duration),
			RecordCount:    recordCount,
			ProcessedCount: processedCount,
			Succeeded:      succeeded,
			Category:       kafka.ErrorCategory(category),
		})
		if err != nil && !errors.Is(err, ErrInvalidObservation) {
			t.Fatalf("Observer() error = %v", err)
		}
	})
}
