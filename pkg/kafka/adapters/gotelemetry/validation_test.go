package gotelemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	kafka "github.com/faustbrian/golib/pkg/kafka"
)

func TestObserverRejectsImpossibleSettlementCounts(t *testing.T) {
	t.Parallel()

	instrumentation, err := New(Config{Runtime: completeTestRuntime()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	base := kafka.Observation{
		Kind:        kafka.ObservationConsumePoll,
		StartedAt:   time.Unix(1, 0),
		Duration:    time.Millisecond,
		RecordCount: 2,
		Succeeded:   true,
	}
	tests := []kafka.Observation{
		func() kafka.Observation {
			value := base
			value.ProcessedCount = 3

			return value
		}(),
		func() kafka.Observation {
			value := base
			value.ProcessedCount = 1
			value.CommittedCount = 2

			return value
		}(),
	}
	for _, observation := range tests {
		if err := instrumentation.Observer()(
			context.Background(),
			observation,
		); !errors.Is(err, ErrInvalidObservation) {
			t.Fatalf("Observer(%#v) error = %v", observation, err)
		}
	}
}

func TestMessagingOperationRejectsUnknownKind(t *testing.T) {
	t.Parallel()

	if descriptor := messagingOperation(
		kafka.Observation{Kind: kafka.ObservationKind(255)},
	); descriptor != (operationDescriptor{}) {
		t.Fatalf("unknown operation descriptor = %#v", descriptor)
	}
}

func TestObserverRejectsInvalidMetadataAndContexts(t *testing.T) {
	t.Parallel()

	instrumentation, err := New(Config{Runtime: completeTestRuntime()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	base := kafka.Observation{
		Kind:        kafka.ObservationConsumePoll,
		StartedAt:   time.Unix(1, 0),
		Duration:    time.Millisecond,
		RecordCount: 2,
		Succeeded:   true,
	}
	mutations := []func(*kafka.Observation){
		func(value *kafka.Observation) { value.Kind = 255 },
		func(value *kafka.Observation) { value.StartedAt = time.Time{} },
		func(value *kafka.Observation) { value.Duration = -1 },
		func(value *kafka.Observation) { value.RecordCount = -1 },
		func(value *kafka.Observation) { value.PartitionCount = -1 },
		func(value *kafka.Observation) { value.ProcessedCount = -1 },
		func(value *kafka.Observation) { value.CommittedCount = -1 },
		func(value *kafka.Observation) { value.RecordBytes = -1 },
		func(value *kafka.Observation) { value.RequestBytes = -1 },
		func(value *kafka.Observation) { value.ResponseBytes = -1 },
		func(value *kafka.Observation) { value.QueueDuration = -1 },
		func(value *kafka.Observation) { value.ThrottleDuration = -1 },
		func(value *kafka.Observation) {
			value.PartitionKnown = true
			value.Partition = -1
		},
		func(value *kafka.Observation) {
			value.OffsetKnown = true
			value.Offset = -1
		},
		func(value *kafka.Observation) {
			value.BrokerKnown = true
			value.BrokerID = -1
		},
		func(value *kafka.Observation) {
			value.APIKeyKnown = true
			value.APIKey = -1
		},
		func(value *kafka.Observation) {
			value.Category = kafka.ErrorRetryable
		},
		func(value *kafka.Observation) {
			value.Succeeded = false
			value.Category = kafka.ErrorUnknown
		},
		func(value *kafka.Observation) {
			value.Succeeded = false
			value.Category = kafka.ErrorCategory(255)
		},
	}
	for index, mutate := range mutations {
		observation := base
		mutate(&observation)
		if err := instrumentation.Observer()(
			context.Background(),
			observation,
		); !errors.Is(err, ErrInvalidObservation) {
			t.Fatalf("mutation %d error = %v", index, err)
		}
	}

	if err := instrumentation.Observer()(nil, base); !errors.Is(
		err,
		ErrContextRequired,
	) {
		t.Fatalf("nil context error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := instrumentation.Observer()(ctx, base); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("canceled context error = %v", err)
	}
	var nilInstrumentation *Instrumentation
	if err := nilInstrumentation.Observer()(
		context.Background(),
		base,
	); !errors.Is(err, ErrRuntimeRequired) {
		t.Fatalf("nil instrumentation error = %v", err)
	}
	if err := (&Instrumentation{}).Observer()(
		context.Background(),
		base,
	); !errors.Is(err, ErrRuntimeRequired) {
		t.Fatalf("empty instrumentation error = %v", err)
	}
}

func TestObserverEnforcesObservationRecordCardinality(t *testing.T) {
	t.Parallel()

	instrumentation, err := New(Config{Runtime: completeTestRuntime()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	base := kafka.Observation{
		StartedAt: time.Unix(1, 0),
		Duration:  time.Millisecond,
		Succeeded: true,
	}
	tests := []kafka.Observation{
		func() kafka.Observation {
			value := base
			value.Kind = kafka.ObservationProduceRecord
			value.RecordCount = 2

			return value
		}(),
		func() kafka.Observation {
			value := base
			value.Kind = kafka.ObservationProduceAsync

			return value
		}(),
		func() kafka.Observation {
			value := base
			value.Kind = kafka.ObservationProduceBatch

			return value
		}(),
		func() kafka.Observation {
			value := base
			value.Kind = kafka.ObservationConsumeRecord
			value.RecordCount = 2

			return value
		}(),
		func() kafka.Observation {
			value := base
			value.Kind = kafka.ObservationConsumeBatch

			return value
		}(),
		func() kafka.Observation {
			value := base
			value.Kind = kafka.ObservationConsumeCommit

			return value
		}(),
		func() kafka.Observation {
			value := base
			value.Kind = kafka.ObservationTransactionCommit
			value.RecordCount = 1

			return value
		}(),
	}
	for _, observation := range tests {
		if err := instrumentation.Observer()(
			context.Background(),
			observation,
		); !errors.Is(err, ErrInvalidObservation) {
			t.Fatalf("Observer(%s) error = %v", observation.Kind, err)
		}
	}
}
