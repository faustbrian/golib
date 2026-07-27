package golog

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	kafka "github.com/faustbrian/golib/pkg/kafka"
)

func FuzzIdentityPolicyValidate(f *testing.F) {
	f.Add("client", "orders", "workers")
	f.Add("", ".", "\x00")
	f.Fuzz(func(t *testing.T, clientID, topic, groupID string) {
		t.Helper()

		err := (IdentityPolicy{
			AllowedClientIDs:      []string{clientID},
			AllowedTopics:         []string{topic},
			AllowedConsumerGroups: []string{groupID},
		}).Validate()
		if err != nil && !errors.Is(err, ErrInvalidIdentityPolicy) {
			t.Fatalf("unexpected validation error = %v", err)
		}
	})
}

func FuzzObserverValidation(f *testing.F) {
	f.Add(uint8(kafka.ObservationProduceRecord), int64(1), true, uint8(0))
	f.Add(uint8(0), int64(-1), false, uint8(255))

	adapter, err := New(Config{
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	if err != nil {
		f.Fatalf("New() error = %v", err)
	}
	f.Fuzz(func(
		t *testing.T,
		kind uint8,
		durationNanos int64,
		succeeded bool,
		category uint8,
	) {
		t.Helper()

		observation := kafka.Observation{
			Kind:        kafka.ObservationKind(kind),
			StartedAt:   time.Unix(1, 0),
			Duration:    time.Duration(durationNanos),
			RecordCount: recordCount(kafka.ObservationKind(kind)),
			Succeeded:   succeeded,
			Category:    kafka.ErrorCategory(category),
		}
		err := adapter.Observer()(context.Background(), observation)
		if err != nil && !errors.Is(err, ErrInvalidObservation) {
			t.Fatalf("unexpected observer error = %v", err)
		}
	})
}

func recordCount(kind kafka.ObservationKind) int {
	switch kind {
	case kafka.ObservationProduceRecord,
		kafka.ObservationProduceAsync,
		kafka.ObservationConsumeRecord,
		kafka.ObservationProduceBatch,
		kafka.ObservationConsumeBatch,
		kafka.ObservationConsumeCommit:
		return 1
	default:
		return 0
	}
}
