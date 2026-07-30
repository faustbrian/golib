package kafka

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestProducerShutdownObserversReportAttemptsAndFenceReentry(
	t *testing.T,
) {

	backend := &recordingProducerBackend{flushErr: context.Canceled}
	var observations []Observation
	var reentryErr error
	producer := &Producer{
		client:          backend,
		clientID:        "orders-producer",
		shutdownTimeout: time.Second,
	}
	producer.observers = shutdownObserverDispatcher(t, func(
		observation Observation,
	) {
		observations = append(observations, observation)
		if observation.Kind == ObservationProducerShutdown {
			reentryErr = producer.Shutdown(context.Background())
		}
	})

	if err := producer.Shutdown(context.Background()); !errors.Is(
		err,
		ErrDrainIncomplete,
	) {
		t.Fatalf("first Shutdown() error = %v", err)
	}
	backend.flushErr = nil
	if err := producer.Shutdown(context.Background()); err != nil {
		t.Fatalf("retry Shutdown() error = %v", err)
	}
	if err := producer.Shutdown(context.Background()); err != nil {
		t.Fatalf("idempotent Shutdown() error = %v", err)
	}

	checked := assertShutdownObservations(
		t,
		observations,
		ObservationProducerShutdown,
		"orders-producer",
		"",
	)
	if checked[0].Succeeded ||
		checked[0].Category != ErrorCanceled ||
		!checked[1].Succeeded ||
		checked[1].Category != ErrorUnknown {
		t.Fatalf("shutdown observations = %#v", checked)
	}
	if !errors.Is(reentryErr, ErrObserverReentry) {
		t.Fatalf("observer Shutdown() error = %v", reentryErr)
	}
	if backend.closes != 1 {
		t.Fatalf("backend closes = %d, want 1", backend.closes)
	}
}

func TestConsumerShutdownObserversReportAttemptsAndFenceReentry(
	t *testing.T,
) {

	backend := &recordingConsumerBackend{leaveErr: context.Canceled}
	consumer := consumerWithBackend(
		backend,
		10,
		time.Second,
		time.Second,
	)
	consumer.clientID = "orders-consumer"
	consumer.groupID = "orders-projection"
	var observations []Observation
	var reentryErr error
	consumer.observers = shutdownObserverDispatcher(t, func(
		observation Observation,
	) {
		observations = append(observations, observation)
		if observation.Kind == ObservationConsumerShutdown {
			reentryErr = consumer.Shutdown(context.Background())
		}
	})

	if err := consumer.Shutdown(context.Background()); !errors.Is(
		err,
		ErrConsumerShutdownIncomplete,
	) {
		t.Fatalf("first Shutdown() error = %v", err)
	}
	backend.leaveErr = nil
	if err := consumer.Shutdown(context.Background()); err != nil {
		t.Fatalf("retry Shutdown() error = %v", err)
	}
	if err := consumer.Shutdown(context.Background()); err != nil {
		t.Fatalf("idempotent Shutdown() error = %v", err)
	}

	checked := assertShutdownObservations(
		t,
		observations,
		ObservationConsumerShutdown,
		"orders-consumer",
		"orders-projection",
	)
	if checked[0].Succeeded ||
		checked[0].Category != ErrorCanceled ||
		!checked[1].Succeeded ||
		checked[1].Category != ErrorUnknown {
		t.Fatalf("shutdown observations = %#v", checked)
	}
	if !errors.Is(reentryErr, ErrObserverReentry) {
		t.Fatalf("observer Shutdown() error = %v", reentryErr)
	}
	if backend.closed != 1 || backend.leaveCalls != 2 {
		t.Fatalf("backend close/leave = %d/%d", backend.closed, backend.leaveCalls)
	}
}

func TestTransactionProcessorShutdownObserversReportAttemptsAndFenceReentry(
	t *testing.T,
) {

	backend := &recordingTransactionProcessorBackend{
		leaveErr: context.Canceled,
	}
	processor := transactionProcessorForTest(t, backend)
	var observations []Observation
	var reentryErr error
	processor.observers = shutdownObserverDispatcher(t, func(
		observation Observation,
	) {
		observations = append(observations, observation)
		if observation.Kind == ObservationTransactionProcessorShutdown {
			reentryErr = processor.Shutdown(context.Background())
		}
	})

	if err := processor.Shutdown(context.Background()); !errors.Is(
		err,
		ErrTransactionProcessorShutdownIncomplete,
	) {
		t.Fatalf("first Shutdown() error = %v", err)
	}
	backend.leaveErr = nil
	if err := processor.Shutdown(context.Background()); err != nil {
		t.Fatalf("retry Shutdown() error = %v", err)
	}
	if err := processor.Shutdown(context.Background()); err != nil {
		t.Fatalf("idempotent Shutdown() error = %v", err)
	}

	checked := assertShutdownObservations(
		t,
		observations,
		ObservationTransactionProcessorShutdown,
		"transaction-worker",
		"transaction-worker",
	)
	if checked[0].Succeeded ||
		checked[0].Category != ErrorCanceled ||
		!checked[1].Succeeded ||
		checked[1].Category != ErrorUnknown {
		t.Fatalf("shutdown observations = %#v", checked)
	}
	if !errors.Is(reentryErr, ErrObserverReentry) {
		t.Fatalf("observer Shutdown() error = %v", reentryErr)
	}
	if !backend.closed || backend.leaveCalls != 2 {
		t.Fatalf("backend close/leave = %t/%d", backend.closed, backend.leaveCalls)
	}
}

func assertShutdownObservations(
	t *testing.T,
	observations []Observation,
	kind ObservationKind,
	clientID string,
	groupID string,
) [2]Observation {
	t.Helper()

	if len(observations) != 2 {
		t.Fatalf("shutdown observations = %#v", observations)

		return [2]Observation{}
	}
	for index, observation := range observations {
		if observation.Kind != kind ||
			observation.ClientID != clientID ||
			observation.GroupID != groupID ||
			observation.StartedAt.IsZero() ||
			observation.Duration < 0 {
			t.Fatalf("shutdown observation %d = %#v", index, observation)
		}
		if err := observation.Validate(); err != nil {
			t.Fatalf("shutdown observation %d invalid: %v", index, err)
		}
	}

	return [2]Observation{observations[0], observations[1]}
}

func shutdownObserverDispatcher(
	t *testing.T,
	observe func(Observation),
) observerDispatcher {
	t.Helper()

	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(_ context.Context, observation Observation) error {
				observe(observation)

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}

	return newObserverDispatcher(policy)
}
