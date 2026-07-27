package kafka

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestReplayEmitsPlanRecordRunAndShutdownObservations(t *testing.T) {
	t.Parallel()

	backend := &recordingReplayBackend{fetches: []kgo.Fetches{recordFetches(
		&kgo.Record{
			Topic: "events", Partition: 1, Offset: 1,
			Key: []byte("key"), Value: []byte("value"),
		},
	)}}
	reader := replayReaderWithBackend(backend, []ReplayRange{{
		Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 2,
	}})
	var observations []Observation
	reader.clientID = "track-replay"
	reader.observers = replayObserverDispatcher(t, &observations)

	plan, err := reader.PlanAgainstBroker(context.Background())
	if err != nil || plan.TotalRemaining != 1 {
		t.Fatalf("PlanAgainstBroker() = %#v, %v", plan, err)
	}
	result, err := reader.Replay(
		context.Background(),
		HandlerFunc(func(context.Context, ConsumedMessage) error { return nil }),
	)
	if err != nil || result.Processed != 1 {
		t.Fatalf("Replay() = %#v, %v", result, err)
	}
	if err := reader.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	if len(observations) != 4 {
		t.Fatalf("observations = %#v", observations)
	}
	for index, observation := range observations {
		if err := observation.Validate(); err != nil {
			t.Fatalf("observation %d invalid: %#v: %v", index, observation, err)
		}
	}
	if got := observations[0]; got.Kind != ObservationReplayPlan ||
		got.ClientID != "track-replay" || got.PartitionCount != 1 ||
		got.ReplayRemaining != 1 || !got.Succeeded {
		t.Fatalf("plan observation = %#v", got)
	}
	if got := observations[1]; got.Kind != ObservationReplayRecord ||
		got.Topic != "events" || !got.PartitionKnown || got.Partition != 1 ||
		!got.OffsetKnown || got.Offset != 1 || got.RecordCount != 1 ||
		got.ProcessedCount != 1 || got.ReplayProcessed != 1 ||
		got.RecordBytes == 0 || !got.Succeeded {
		t.Fatalf("record observation = %#v", got)
	}
	if got := observations[2]; got.Kind != ObservationReplayRun ||
		got.PartitionCount != 1 || got.ReplayProcessed != 1 ||
		got.ReplayRemaining != 0 || !got.Succeeded {
		t.Fatalf("run observation = %#v", got)
	}
	if got := observations[3]; got.Kind != ObservationReplayShutdown ||
		!got.Succeeded {
		t.Fatalf("shutdown observation = %#v", got)
	}
}

func TestReplayObservesGapFailureAndExactProgress(t *testing.T) {
	t.Parallel()

	backend := &recordingReplayBackend{fetches: []kgo.Fetches{recordFetches(
		&kgo.Record{Topic: "events", Partition: 1, Offset: 2},
	)}}
	reader := replayReaderWithBackend(backend, []ReplayRange{{
		Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 3,
	}})
	var observations []Observation
	reader.observers = replayObserverDispatcher(t, &observations)

	result, err := reader.Replay(
		context.Background(),
		HandlerFunc(func(context.Context, ConsumedMessage) error { return nil }),
	)

	if !errors.Is(err, ErrReplayOffsetGap) ||
		result.Failed != 1 || result.Ranges[0].NextOffset != 1 {
		t.Fatalf("Replay() = %#v, %v", result, err)
	}
	if len(observations) != 2 {
		t.Fatalf("observations = %#v", observations)
	}
	if got := observations[0]; got.Kind != ObservationReplayRecord ||
		got.Succeeded || got.Category != ErrorPermanent ||
		got.ReplayFailed != 1 || got.Offset != 2 {
		t.Fatalf("record observation = %#v", got)
	}
	if got := observations[1]; got.Kind != ObservationReplayRun ||
		got.Succeeded || got.Category != ErrorPermanent ||
		got.ReplayFailed != 1 || got.ReplayRemaining != 2 {
		t.Fatalf("run observation = %#v", got)
	}
}

func TestReplayValidatesCopiesAndFencesObservers(t *testing.T) {
	t.Parallel()

	config := validReplayConfig()
	config.Observers = ObserverPolicy{
		Observers: []ObserverFunc{
			func(context.Context, Observation) error { return nil },
		},
	}
	if err := config.Validate(); !errors.Is(
		err,
		ErrObserverFailureHandlerRequired,
	) {
		t.Fatalf("ReplayConfig.Validate() error = %v", err)
	}
	factoryCalled := false
	_, err := newReplayReader(config, func(...kgo.Opt) (*kgo.Client, error) {
		factoryCalled = true

		return nil, errors.New("unexpected allocation")
	})
	if !errors.Is(err, ErrObserverFailureHandlerRequired) || factoryCalled {
		t.Fatalf("newReplayReader() error/called = %v/%v", err, factoryCalled)
	}

	backend := &recordingReplayBackend{fetches: []kgo.Fetches{recordFetches(
		&kgo.Record{Topic: "events", Partition: 1, Offset: 1},
	)}}
	reader := replayReaderWithBackend(backend, validReplayConfig().Ranges)
	var reentryErrors []error
	observer := ObserverFunc(func(ctx context.Context, _ Observation) error {
		_, planErr := reader.PlanAgainstBroker(context.Background())
		_, replayErr := reader.Replay(
			context.Background(),
			HandlerFunc(func(context.Context, ConsumedMessage) error { return nil }),
		)
		shutdownErr := reader.Shutdown(context.Background())
		closeErr := reader.Close()
		reentryErrors = append(
			reentryErrors,
			planErr,
			replayErr,
			shutdownErr,
			closeErr,
		)

		return nil
	})
	policy := ObserverPolicy{
		Observers:      []ObserverFunc{observer},
		FailureHandler: func(context.Context, ObservationFailure) {},
	}
	normalized, err := normalizeObserverPolicy(policy)
	if err != nil {
		t.Fatalf("normalizeObserverPolicy() error = %v", err)
	}
	reader.observers = newObserverDispatcher(normalized)

	if _, err := reader.Replay(
		context.Background(),
		HandlerFunc(func(context.Context, ConsumedMessage) error { return nil }),
	); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}

	if len(reentryErrors) != 8 {
		t.Fatalf("reentry errors = %#v", reentryErrors)
	}
	for index, reentryErr := range reentryErrors {
		if !errors.Is(reentryErr, ErrObserverReentry) {
			t.Fatalf("reentry error %d = %v", index, reentryErr)
		}
	}
	if backend.closed != 0 {
		t.Fatal("observer closed replay backend")
	}
	observerCtx := context.WithValue(
		context.Background(),
		observerContextKey{},
		true,
	)
	if _, err := reader.PlanAgainstBroker(observerCtx); !errors.Is(
		err,
		ErrObserverReentry,
	) {
		t.Fatalf("PlanAgainstBroker(observer context) error = %v", err)
	}
	if _, err := reader.Replay(
		observerCtx,
		HandlerFunc(func(context.Context, ConsumedMessage) error { return nil }),
	); !errors.Is(err, ErrObserverReentry) {
		t.Fatalf("Replay(observer context) error = %v", err)
	}
	if err := reader.Shutdown(observerCtx); !errors.Is(
		err,
		ErrObserverReentry,
	) {
		t.Fatalf("Shutdown(observer context) error = %v", err)
	}

	policy.Observers[0] = nil
	if reflect.ValueOf(normalized.Observers[0]).IsNil() {
		t.Fatal("normalized observer policy aliases caller slice")
	}
}

func TestReplayWiresBrokerObserversAndObservesFailures(t *testing.T) {
	t.Parallel()

	var observations []Observation
	config := validReplayConfig()
	config.Observers = ObserverPolicy{
		Observers: []ObserverFunc{
			func(_ context.Context, observation Observation) error {
				observations = append(observations, observation)

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	}
	factoryErr := errors.New("factory failed")
	_, err := newReplayReader(config, func(options ...kgo.Opt) (*kgo.Client, error) {
		client, clientErr := kgo.NewClient(options...)
		if clientErr != nil {
			t.Fatalf("apply replay options: %v", clientErr)
		}
		defer client.Close()

		hooks := reflect.ValueOf(client.OptValue(kgo.WithHooks))
		if hooks.Kind() != reflect.Slice || hooks.Len() != 1 {
			t.Fatalf("replay hooks = %#v", hooks)
		}
		hook, ok := hooks.Index(0).Interface().(*franzObserverHook)
		if !ok ||
			hook.clientID != "track-replay" ||
			hook.groupID != "" ||
			hook.before == nil ||
			hook.after == nil {
			t.Fatalf("replay observer hook = %#v", hooks.Index(0).Interface())
		}

		return nil, factoryErr
	})
	if !errors.Is(err, factoryErr) {
		t.Fatalf("newReplayReader() error = %v", err)
	}

	reader := replayReaderWithBackend(
		&recordingReplayBackend{},
		validReplayConfig().Ranges,
	)
	reader.observers = replayObserverDispatcher(t, &observations)
	reader.bounds = &recordingReplayBoundsBackend{
		err: errors.New("bounds failed"),
	}
	if _, err := reader.PlanAgainstBroker(context.Background()); !errors.Is(
		err,
		ErrReplayBoundsUnavailable,
	) {
		t.Fatalf("PlanAgainstBroker() error = %v", err)
	}
	if len(observations) != 1 ||
		observations[0].Kind != ObservationReplayPlan ||
		observations[0].Succeeded ||
		observations[0].Category != ErrorPermanent {
		t.Fatalf("plan observations = %#v", observations)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	shutdownReader := replayReaderWithBackend(
		&recordingReplayBackend{},
		validReplayConfig().Ranges,
	)
	shutdownReader.observers = replayObserverDispatcher(t, &observations)
	if err := shutdownReader.Shutdown(canceled); !errors.Is(
		err,
		ErrReplayShutdownIncomplete,
	) {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if len(observations) != 2 ||
		observations[1].Kind != ObservationReplayShutdown ||
		observations[1].Succeeded ||
		observations[1].Category != ErrorCanceled {
		t.Fatalf("shutdown observations = %#v", observations)
	}
}

func replayObserverDispatcher(
	t *testing.T,
	observations *[]Observation,
) observerDispatcher {
	t.Helper()

	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(_ context.Context, observation Observation) error {
				*observations = append(*observations, observation)

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalizeObserverPolicy() error = %v", err)
	}

	return newObserverDispatcher(policy)
}
