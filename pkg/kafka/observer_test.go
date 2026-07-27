package kafka

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestProducerObserversReceiveOrderedDeliveryMetadata(t *testing.T) {
	t.Parallel()

	var order []int
	var observations []Observation
	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Timeout: time.Second,
		Observers: []ObserverFunc{
			func(_ context.Context, observation Observation) error {
				order = append(order, 1)
				observations = append(observations, observation)

				return nil
			},
			func(_ context.Context, observation Observation) error {
				order = append(order, 2)
				observations = append(observations, observation)

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}
	deliveredAt := time.Unix(1_700_000_123, 456)
	backend := &recordingProducerBackend{
		prepareDelivery: func(record *kgo.Record) {
			record.Partition = 7
			record.Offset = 42
			record.Timestamp = deliveredAt
		},
	}
	producer := &Producer{
		client:    backend,
		clientID:  "observer-producer",
		limits:    DefaultMessageLimits(),
		observers: newObserverDispatcher(policy),
	}

	result := producer.PublishRecord(context.Background(), ProducerRecord{
		Topic: "events",
		Key:   []byte("aggregate-1"),
		Value: []byte("payload"),
	})

	if result.Err != nil {
		t.Fatalf("PublishRecord() error = %v", result.Err)
	}
	if !reflect.DeepEqual(order, []int{1, 2}) {
		t.Fatalf("observer order = %v", order)
	}
	if len(observations) != 2 {
		t.Fatalf("observations = %d, want 2", len(observations))
	}
	for _, observation := range observations {
		if observation.Kind != ObservationProduceRecord ||
			observation.ClientID != "observer-producer" ||
			observation.Topic != "events" ||
			observation.Partition != 7 ||
			!observation.PartitionKnown ||
			observation.Offset != 42 ||
			!observation.OffsetKnown ||
			!observation.Timestamp.Equal(deliveredAt) ||
			observation.RecordCount != 1 ||
			observation.RecordBytes != int64(
				len("events")+len("aggregate-1")+len("payload")+32,
			) ||
			!observation.Succeeded ||
			observation.Category != ErrorUnknown ||
			observation.StartedAt.IsZero() ||
			observation.Duration < 0 {
			t.Fatalf("observation = %#v", observation)
		}
	}
}

func TestProducerObserverReportsBoundedBatchOutcome(t *testing.T) {
	t.Parallel()

	var got []Observation
	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(_ context.Context, observation Observation) error {
				got = append(got, observation)

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}
	backend := &recordingProducerBackend{
		deliveryErrors: []error{nil, kerr.NotLeaderForPartition},
	}
	producer := &Producer{
		client:          backend,
		limits:          DefaultMessageLimits(),
		maxBatchRecords: 10,
		maxBatchBytes:   1 << 20,
		observers:       newObserverDispatcher(policy),
	}
	records := []ProducerRecord{
		{Topic: "events", Key: []byte("one"), Value: []byte("first")},
		{Topic: "events", Key: []byte("two"), Value: []byte("second")},
	}

	results, err := producer.PublishBatch(context.Background(), records)

	if len(results) != 2 || !errors.Is(err, ErrBatchDeliveryFailed) {
		t.Fatalf("PublishBatch() = %#v, %v", results, err)
	}
	if len(got) != 1 {
		t.Fatalf("observations = %d, want 1", len(got))
	}
	wantBytes := recordSize(records[0]) + recordSize(records[1])
	if got[0].Kind != ObservationProduceBatch ||
		got[0].Topic != "events" ||
		got[0].PartitionKnown ||
		got[0].RecordCount != 2 ||
		got[0].RecordBytes != wantBytes ||
		got[0].Succeeded ||
		got[0].Category != ErrorRetryable {
		t.Fatalf("observation = %#v", got[0])
	}
}

func TestProducerObserverReportsAsyncOutcomeAfterCallerCancellation(t *testing.T) {
	t.Parallel()

	var got []Observation
	var callbackContextErr error
	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(ctx context.Context, observation Observation) error {
				callbackContextErr = ctx.Err()
				got = append(got, observation)

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}
	backend := &recordingProducerBackend{}
	producer := &Producer{
		client:    backend,
		limits:    DefaultMessageLimits(),
		observers: newObserverDispatcher(policy),
	}
	ctx, cancel := context.WithCancel(context.Background())
	delivery, err := producer.PublishAsync(ctx, ProducerRecord{
		Topic: "events",
		Key:   []byte("aggregate-1"),
		Value: []byte("payload"),
	})
	if err != nil {
		t.Fatalf("PublishAsync() error = %v", err)
	}
	cancel()

	backend.completeAsync(0, 4, 21, nil)
	result := <-delivery

	if result.Err != nil {
		t.Fatalf("async delivery error = %v", result.Err)
	}
	if callbackContextErr != nil {
		t.Fatalf("observer context error = %v", callbackContextErr)
	}
	if len(got) != 1 {
		t.Fatalf("observations = %d, want 1", len(got))
	}
	if got[0].Kind != ObservationProduceAsync ||
		got[0].Topic != "events" ||
		got[0].Partition != 4 ||
		!got[0].PartitionKnown ||
		got[0].RecordCount != 1 ||
		!got[0].Succeeded {
		t.Fatalf("observation = %#v", got[0])
	}
}

func TestProducerObserverContextFencesProducerReentry(t *testing.T) {
	t.Parallel()

	var producer *Producer
	var reentryErrors []error
	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(ctx context.Context, _ Observation) error {
				recordResult := producer.PublishRecord(ctx, ProducerRecord{
					Topic: "events",
					Key:   []byte("nested"),
				})
				reentryErrors = append(reentryErrors, recordResult.Err)
				_, batchErr := producer.PublishBatch(ctx, []ProducerRecord{{
					Topic: "events",
					Key:   []byte("nested"),
				}})
				reentryErrors = append(reentryErrors, batchErr)
				_, asyncErr := producer.PublishAsync(ctx, ProducerRecord{
					Topic: "events",
					Key:   []byte("nested"),
				})
				reentryErrors = append(reentryErrors, asyncErr)
				reentryErrors = append(
					reentryErrors,
					producer.Health(ctx),
					producer.Drain(ctx),
					producer.Abort(ctx),
					producer.Shutdown(ctx),
					producer.RunTransaction(ctx, func(Transaction) error {
						return nil
					}),
				)

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}
	producer = &Producer{
		client:          &recordingProducerBackend{},
		limits:          DefaultMessageLimits(),
		maxBatchRecords: 10,
		maxBatchBytes:   1 << 20,
		observers:       newObserverDispatcher(policy),
	}

	result := producer.PublishRecord(context.Background(), ProducerRecord{
		Topic: "events",
		Key:   []byte("aggregate-1"),
	})

	if result.Err != nil {
		t.Fatalf("PublishRecord() error = %v", result.Err)
	}
	if len(reentryErrors) != 8 {
		t.Fatalf("reentry errors = %d, want 8", len(reentryErrors))
	}
	for index, err := range reentryErrors {
		if !errors.Is(err, ErrObserverReentry) {
			t.Fatalf("reentry error %d = %v", index, err)
		}
	}
}

func TestAsyncProducerObserverCannotCloseItsProducer(t *testing.T) {
	t.Parallel()

	var producer *Producer
	var closeErr error
	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(context.Context, Observation) error {
				closeErr = producer.Close()

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}
	backend := &recordingProducerBackend{}
	producer = &Producer{
		client:          backend,
		limits:          DefaultMessageLimits(),
		shutdownTimeout: time.Second,
		observers:       newObserverDispatcher(policy),
	}
	delivery, err := producer.PublishAsync(
		context.Background(),
		ProducerRecord{Topic: "events", Key: []byte("aggregate-1")},
	)
	if err != nil {
		t.Fatalf("PublishAsync() error = %v", err)
	}

	backend.completeAsync(0, 1, 1, nil)
	result := <-delivery

	if result.Err != nil {
		t.Fatalf("async delivery error = %v", result.Err)
	}
	if !errors.Is(closeErr, ErrObserverReentry) {
		t.Fatalf("observer Close() error = %v", closeErr)
	}
	if backend.closes != 0 {
		t.Fatalf("observer closed backend %d times", backend.closes)
	}
}

func TestObserverPolicyValidationAndOwnership(t *testing.T) {
	t.Parallel()

	noopObserver := ObserverFunc(func(context.Context, Observation) error {
		return nil
	})
	noopFailure := ObservationFailureFunc(func(
		context.Context,
		ObservationFailure,
	) {
	})
	if err := (ObserverPolicy{}).Validate(); err != nil {
		t.Fatalf("zero observer policy error = %v", err)
	}
	if _, err := normalizeProducerConfig(ProducerConfig{
		Brokers:       []string{"broker.internal:9092"},
		ClientID:      "observer-validation",
		AllowedTopics: []string{"events"},
		Observers: ObserverPolicy{
			Observers: []ObserverFunc{noopObserver},
		},
	}); !errors.Is(err, ErrObserverFailureHandlerRequired) {
		t.Fatalf("producer observer policy error = %v", err)
	}

	for name, test := range map[string]struct {
		policy ObserverPolicy
		want   error
	}{
		"failure handler without observers": {
			policy: ObserverPolicy{FailureHandler: noopFailure},
			want:   ErrInvalidObserverPolicy,
		},
		"timeout without observers": {
			policy: ObserverPolicy{Timeout: time.Second},
			want:   ErrInvalidObserverPolicy,
		},
		"too many observers": {
			policy: ObserverPolicy{
				Observers:      repeatedObservers(noopObserver, 17),
				FailureHandler: noopFailure,
			},
			want: ErrInvalidObserverPolicy,
		},
		"nil observer": {
			policy: ObserverPolicy{
				Observers:      []ObserverFunc{nil},
				FailureHandler: noopFailure,
			},
			want: ErrInvalidObserverPolicy,
		},
		"missing failure handler": {
			policy: ObserverPolicy{Observers: []ObserverFunc{noopObserver}},
			want:   ErrObserverFailureHandlerRequired,
		},
		"timeout too short": {
			policy: ObserverPolicy{
				Observers:      []ObserverFunc{noopObserver},
				FailureHandler: noopFailure,
				Timeout:        time.Nanosecond,
			},
			want: ErrInvalidObserverPolicy,
		},
		"timeout too long": {
			policy: ObserverPolicy{
				Observers:      []ObserverFunc{noopObserver},
				FailureHandler: noopFailure,
				Timeout:        5*time.Second + time.Nanosecond,
			},
			want: ErrInvalidObserverPolicy,
		},
	} {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := test.policy.Validate(); !errors.Is(err, test.want) {
				t.Fatalf("Validate() error = %v, want %v", err, test.want)
			}
		})
	}

	calls := 0
	observers := []ObserverFunc{
		func(context.Context, Observation) error {
			calls++

			return nil
		},
	}
	normalized, err := normalizeObserverPolicy(ObserverPolicy{
		Observers:      observers,
		FailureHandler: noopFailure,
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}
	observers[0] = nil
	if normalized.Timeout != 100*time.Millisecond {
		t.Fatalf("default timeout = %v", normalized.Timeout)
	}
	newObserverDispatcher(normalized).observe(
		context.Background(),
		Observation{Kind: ObservationProduceRecord},
	)
	if calls != 1 {
		t.Fatalf("owned observer calls = %d, want 1", calls)
	}
}

func TestProducerBatchObservationOmitsMixedTopicCardinality(t *testing.T) {
	t.Parallel()

	var got Observation
	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(_ context.Context, observation Observation) error {
				got = observation

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}
	producer := &Producer{
		client:          &recordingProducerBackend{},
		limits:          DefaultMessageLimits(),
		maxBatchRecords: 10,
		maxBatchBytes:   1 << 20,
		observers:       newObserverDispatcher(policy),
	}
	records := []ProducerRecord{
		{Topic: "events", Key: []byte("one")},
		{Topic: "commands", Key: []byte("two")},
	}

	if _, err := producer.PublishBatch(
		context.Background(),
		records,
	); err != nil {
		t.Fatalf("PublishBatch() error = %v", err)
	}

	if got.Topic != "" ||
		got.RecordBytes != recordSize(records[0])+recordSize(records[1]) {
		t.Fatalf("mixed-topic observation = %#v", got)
	}
}

func TestProducerBatchObservationBoundsRejectedMetadata(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		records         []ProducerRecord
		maxBatchRecords int
		maxBatchBytes   int64
		want            error
	}{
		"empty": {
			maxBatchRecords: 1,
			maxBatchBytes:   1 << 20,
			want:            ErrRecordsRequired,
		},
		"too many": {
			records: []ProducerRecord{
				{Topic: "events", Key: []byte("one")},
				{Topic: "events", Key: []byte("two")},
			},
			maxBatchRecords: 1,
			maxBatchBytes:   1 << 20,
			want:            ErrTooManyBatchRecords,
		},
		"invalid": {
			records:         []ProducerRecord{{Key: []byte("one")}},
			maxBatchRecords: 1,
			maxBatchBytes:   1 << 20,
			want:            ErrTopicRequired,
		},
		"too large": {
			records: []ProducerRecord{{
				Topic: "events",
				Key:   []byte("one"),
				Value: make([]byte, 600),
			}},
			maxBatchRecords: 1,
			maxBatchBytes:   512,
			want:            ErrBatchTooLarge,
		},
	} {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var got Observation
			policy, err := normalizeObserverPolicy(ObserverPolicy{
				Observers: []ObserverFunc{
					func(_ context.Context, observation Observation) error {
						got = observation

						return nil
					},
				},
				FailureHandler: func(context.Context, ObservationFailure) {},
			})
			if err != nil {
				t.Fatalf("normalize observer policy: %v", err)
			}
			producer := &Producer{
				client:          &recordingProducerBackend{},
				limits:          DefaultMessageLimits(),
				maxBatchRecords: test.maxBatchRecords,
				maxBatchBytes:   test.maxBatchBytes,
				observers:       newObserverDispatcher(policy),
			}

			_, err = producer.PublishBatch(context.Background(), test.records)

			if !errors.Is(err, test.want) {
				t.Fatalf("PublishBatch() error = %v, want %v", err, test.want)
			}
			if got.RecordCount != len(test.records) ||
				got.RecordBytes != 0 ||
				got.Topic != "" ||
				got.Succeeded ||
				got.Category != ErrorPermanent {
				t.Fatalf("rejected batch observation = %#v", got)
			}
		})
	}
}

func TestProducerObserverReportsAsyncFailureCategory(t *testing.T) {
	t.Parallel()

	var got Observation
	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(_ context.Context, observation Observation) error {
				got = observation

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}
	backend := &recordingProducerBackend{}
	producer := &Producer{
		client:    backend,
		limits:    DefaultMessageLimits(),
		observers: newObserverDispatcher(policy),
	}
	delivery, err := producer.PublishAsync(
		context.Background(),
		ProducerRecord{Topic: "events", Key: []byte("aggregate-1")},
	)
	if err != nil {
		t.Fatalf("PublishAsync() error = %v", err)
	}

	backend.completeAsync(0, 2, 0, kerr.NotLeaderForPartition)
	result := <-delivery

	if !errors.Is(result.Err, kerr.NotLeaderForPartition) {
		t.Fatalf("async delivery error = %v", result.Err)
	}
	if got.Succeeded ||
		got.Category != ErrorRetryable ||
		got.PartitionKnown ||
		got.OffsetKnown {
		t.Fatalf("async failure observation = %#v", got)
	}
}

func TestObserverFailuresAreContainedAndReportedInOrder(t *testing.T) {
	t.Parallel()

	sensitive := errors.New("token=observer-secret")
	var completed bool
	var failures []ObservationFailure
	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(context.Context, Observation) error {
				return sensitive
			},
			func(context.Context, Observation) error {
				panic("secret panic payload")
			},
			func(context.Context, Observation) error {
				completed = true

				return nil
			},
		},
		FailureHandler: func(
			_ context.Context,
			failure ObservationFailure,
		) {
			failures = append(failures, failure)
			if len(failures) == 1 {
				panic("failure reporter panic")
			}
		},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}

	newObserverDispatcher(policy).observe(
		context.Background(),
		Observation{Kind: ObservationProduceBatch},
	)

	if !completed {
		t.Fatal("later observer was not called")
	}
	if len(failures) != 2 {
		t.Fatalf("failures = %d, want 2", len(failures))
	}
	if failures[0].ObserverIndex != 0 ||
		failures[0].Kind != ObservationProduceBatch ||
		failures[0].Panicked ||
		failures[0].TimedOut ||
		!errors.Is(failures[0].Cause(), sensitive) {
		t.Fatalf("returned-error failure = %#v", failures[0])
	}
	if failures[1].ObserverIndex != 1 ||
		!failures[1].Panicked ||
		failures[1].TimedOut ||
		!errors.Is(failures[1].Cause(), ErrObserverPanic) {
		t.Fatalf("panic failure = %#v", failures[1])
	}
	if failures[0].Error() != "kafka: observer failed" {
		t.Fatalf("failure error = %q", failures[0].Error())
	}
}

func TestObserverTimeoutIsReportedAfterCooperativeReturn(t *testing.T) {
	t.Parallel()

	var failure ObservationFailure
	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(ctx context.Context, _ Observation) error {
				<-ctx.Done()

				return nil
			},
		},
		FailureHandler: func(
			_ context.Context,
			got ObservationFailure,
		) {
			failure = got
		},
		Timeout: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}

	newObserverDispatcher(policy).observe(
		context.Background(),
		Observation{Kind: ObservationProduceAsync},
	)

	if !failure.TimedOut ||
		failure.Panicked ||
		!errors.Is(failure.Cause(), context.DeadlineExceeded) {
		t.Fatalf("timeout failure = %#v, cause %v", failure, failure.Cause())
	}
}

func TestEmptyObserverDispatcherIsANoOp(t *testing.T) {
	t.Parallel()

	newObserverDispatcher(ObserverPolicy{}).observe(
		context.Background(),
		Observation{Kind: ObservationProduceRecord},
	)
}

func TestObservationKindString(t *testing.T) {
	t.Parallel()

	for kind, want := range map[ObservationKind]string{
		ObservationProduceRecord:    "producer.record",
		ObservationProduceBatch:     "producer.batch",
		ObservationProduceAsync:     "producer.async",
		ObservationConsumeRecord:    "consumer.record",
		ObservationConsumeBatch:     "consumer.batch",
		ObservationConsumeCommit:    "consumer.commit",
		ObservationConsumePoll:      "consumer.poll",
		ObservationBrokerConnect:    "broker.connect",
		ObservationBrokerRequest:    "broker.request",
		ObservationBrokerThrottle:   "broker.throttle",
		ObservationBrokerDisconnect: "broker.disconnect",
		ObservationKind(255):        "unknown",
	} {
		if got := kind.String(); got != want {
			t.Fatalf("ObservationKind(%d).String() = %q, want %q", kind, got, want)
		}
	}
}

func repeatedObservers(observer ObserverFunc, count int) []ObserverFunc {
	observers := make([]ObserverFunc, count)
	for index := range observers {
		observers[index] = observer
	}

	return observers
}
