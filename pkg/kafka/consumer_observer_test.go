package kafka

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestConsumerObserversReportRebalanceLifecycle(t *testing.T) {

	var observations []Observation
	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(_ context.Context, observation Observation) error {
				observations = append(observations, observation)

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}
	consumer := consumerWithBackend(
		&recordingConsumerBackend{},
		10,
		time.Second,
		time.Second,
	)
	consumer.clientID = "projection"
	consumer.groupID = "projection-v1"
	consumer.observers = newObserverDispatcher(policy)

	consumer.onPartitionsAssigned(map[string][]int32{"events": {0, 1}})
	consumer.onPartitionsRevoked(map[string][]int32{"events": {0}})
	consumer.onRebalanceBlocked()
	consumer.rebalance.beginPoll()
	consumer.onRebalanceBlocked()
	consumer.rebalance.endPoll()
	consumer.onPartitionsLost(map[string][]int32{"events": {1}})

	if len(observations) != 4 {
		t.Fatalf("rebalance observations = %#v", observations)
	}
	for index, want := range []struct {
		kind       ObservationKind
		partitions int
		succeeded  bool
		category   ErrorCategory
	}{
		{ObservationConsumeAssigned, 2, true, ErrorUnknown},
		{ObservationConsumeRevoked, 1, true, ErrorUnknown},
		{ObservationConsumeBlocked, 0, true, ErrorUnknown},
		{ObservationConsumeLost, 1, false, ErrorFenced},
	} {
		got := observations[index]
		if got.Kind != want.kind ||
			got.ClientID != "projection" ||
			got.GroupID != "projection-v1" ||
			got.PartitionCount != want.partitions ||
			got.Succeeded != want.succeeded ||
			got.Category != want.category ||
			got.StartedAt.IsZero() ||
			got.Duration < 0 ||
			got.Truncated {
			t.Fatalf("rebalance observation %d = %#v", index, got)
		}
	}
}

func TestConsumerObserversBoundInvalidRebalanceMetadata(t *testing.T) {

	var observations []Observation
	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(_ context.Context, observation Observation) error {
				observations = append(observations, observation)

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}
	consumer := consumerWithBackend(
		&recordingConsumerBackend{},
		10,
		time.Second,
		time.Second,
	)
	consumer.assignment.maximum = 1
	consumer.observers = newObserverDispatcher(policy)

	consumer.onPartitionsAssigned(map[string][]int32{"events": {0, 1}})
	consumer.onPartitionsLost(map[string][]int32{
		"events":   {},
		"commands": {},
	})
	consumer.onPartitionsLost(map[string][]int32{"events": {0, 1}})
	consumer.onPartitionsAssigned(map[string][]int32{"commands": {0}})

	if len(observations) != 4 {
		t.Fatalf("rebalance observations = %#v", observations)
	}
	if got := observations[0]; got.Kind != ObservationConsumeAssigned ||
		got.PartitionCount != 1 ||
		got.Succeeded ||
		got.Category != ErrorPermanent ||
		!got.Truncated {
		t.Fatalf("oversized assignment observation = %#v", got)
	}
	if got := observations[1]; got.Kind != ObservationConsumeLost ||
		got.PartitionCount != 0 ||
		got.Succeeded ||
		got.Category != ErrorFenced ||
		!got.Truncated {
		t.Fatalf("oversized loss observation = %#v", got)
	}
	if got := observations[2]; got.Kind != ObservationConsumeLost ||
		got.PartitionCount != 1 ||
		got.Succeeded ||
		got.Category != ErrorFenced ||
		!got.Truncated {
		t.Fatalf("oversized partition-list observation = %#v", got)
	}
	if got := observations[3]; got.Kind != ObservationConsumeAssigned ||
		got.PartitionCount != 0 ||
		got.Succeeded ||
		got.Category != ErrorPermanent ||
		got.Truncated {
		t.Fatalf("invalid assignment observation = %#v", got)
	}
}

func TestConsumerObserversReportRecordCommitAndPollOutcomes(t *testing.T) {

	var observations []Observation
	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(_ context.Context, observation Observation) error {
				observations = append(observations, observation)

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}
	records := []*kgo.Record{
		{
			Topic: "events", Partition: 1, Offset: 4,
			Key: []byte("one"), Value: []byte("first"),
		},
		{
			Topic: "events", Partition: 1, Offset: 5,
			Key: []byte("two"), Value: []byte("second"),
		},
	}
	consumer := consumerWithBackend(
		&recordingConsumerBackend{fetches: recordFetches(records...)},
		10,
		time.Second,
		time.Second,
	)
	consumer.clientID = "projection"
	consumer.groupID = "projection-v1"
	consumer.observers = newObserverDispatcher(policy)

	result, err := consumer.RunOnce(
		context.Background(),
		HandlerFunc(func(context.Context, ConsumedMessage) error {
			return nil
		}),
	)

	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result != (PollResult{Polled: 2, Processed: 2, Committed: 2}) {
		t.Fatalf("RunOnce() result = %#v", result)
	}
	if len(observations) != 4 {
		t.Fatalf("observations = %#v", observations)

		return
	}
	kinds := make([]ObservationKind, len(observations))
	for index := range observations {
		kinds[index] = observations[index].Kind
	}
	if !reflect.DeepEqual(kinds, []ObservationKind{
		ObservationConsumeRecord,
		ObservationConsumeRecord,
		ObservationConsumeCommit,
		ObservationConsumePoll,
	}) {
		t.Fatalf("observation kinds = %v", kinds)

		return
	}

	for index, observation := range observations[:2] {
		record := records[index]
		if observation.ClientID != "projection" ||
			observation.GroupID != "projection-v1" ||
			observation.Topic != "events" ||
			observation.Partition != 1 ||
			!observation.PartitionKnown ||
			observation.Offset != record.Offset ||
			!observation.OffsetKnown ||
			observation.RecordCount != 1 ||
			observation.ProcessedCount != 1 ||
			observation.CommittedCount != 0 ||
			observation.RecordBytes == 0 ||
			!observation.Succeeded ||
			observation.Category != ErrorUnknown ||
			observation.StartedAt.IsZero() ||
			observation.Duration < 0 {
			t.Fatalf("record observation %d = %#v", index, observation)
		}
	}

	commit := observations[2]
	if commit.GroupID != "projection-v1" ||
		commit.Topic != "events" ||
		commit.PartitionCount != 1 ||
		commit.RecordCount != 2 ||
		commit.ProcessedCount != 2 ||
		commit.CommittedCount != 2 ||
		!commit.Succeeded ||
		commit.Category != ErrorUnknown {
		t.Fatalf("commit observation = %#v", commit)
	}

	poll := observations[3]
	if poll.GroupID != "projection-v1" ||
		poll.Topic != "events" ||
		poll.PartitionCount != 1 ||
		poll.RecordCount != 2 ||
		poll.ProcessedCount != 2 ||
		poll.CommittedCount != 2 ||
		!poll.Succeeded ||
		poll.Category != ErrorUnknown {
		t.Fatalf("poll observation = %#v", poll)
	}
}

func TestConsumerPollObservationDoesNotTruncateExactRecordLimit(t *testing.T) {

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
	consumer := &Consumer{
		limits:         DefaultMessageLimits(),
		maxPollRecords: 2,
		observers:      newObserverDispatcher(policy),
	}
	records := []*kgo.Record{
		{Topic: "events", Partition: 0},
		{Topic: "events", Partition: 1},
	}
	consumer.observeConsumerPoll(
		context.Background(),
		time.Now(),
		records,
		PollResult{Polled: 2},
		nil,
	)
	if got.Kind != ObservationConsumePoll || got.RecordCount != 2 ||
		got.PartitionCount != 2 || got.Truncated || !got.Succeeded {
		t.Fatalf("exact-limit poll observation = %#v", got)
	}
}

func TestConsumerObserversReportCommitFailureWithoutClaimingSettlement(t *testing.T) {

	commitErr := errors.New("commit unavailable")
	var observations []Observation
	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(_ context.Context, observation Observation) error {
				observations = append(observations, observation)

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}
	record := &kgo.Record{
		Topic: "events", Partition: 1, Offset: 4, Key: []byte("one"),
	}
	consumer := consumerWithBackend(
		&recordingConsumerBackend{
			fetches:   recordFetches(record),
			commitErr: commitErr,
		},
		10,
		time.Second,
		time.Second,
	)
	consumer.observers = newObserverDispatcher(policy)

	result, err := consumer.RunOnce(
		context.Background(),
		HandlerFunc(func(context.Context, ConsumedMessage) error {
			return nil
		}),
	)

	if !errors.Is(err, commitErr) {
		t.Fatalf("RunOnce() error = %v, want %v", err, commitErr)
	}
	if result != (PollResult{Polled: 1, Processed: 1}) {
		t.Fatalf("RunOnce() result = %#v", result)
	}
	if len(observations) != 3 {
		t.Fatalf("observations = %#v", observations)
	}
	commit := observations[1]
	if commit.Kind != ObservationConsumeCommit ||
		commit.RecordCount != 1 ||
		commit.ProcessedCount != 1 ||
		commit.CommittedCount != 0 ||
		commit.Succeeded ||
		commit.Category != ErrorPermanent {
		t.Fatalf("commit observation = %#v", commit)
	}
	poll := observations[2]
	if poll.Kind != ObservationConsumePoll ||
		poll.RecordCount != 1 ||
		poll.ProcessedCount != 1 ||
		poll.CommittedCount != 0 ||
		poll.Succeeded ||
		poll.Category != ErrorPermanent {
		t.Fatalf("poll observation = %#v", poll)
	}
}

func TestConsumerObserversReportPartitionBatchOutcome(t *testing.T) {

	var observations []Observation
	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(_ context.Context, observation Observation) error {
				observations = append(observations, observation)

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}
	records := []*kgo.Record{
		{Topic: "events", Partition: 1, Offset: 4, Key: []byte("one")},
		{Topic: "events", Partition: 1, Offset: 5, Key: []byte("two")},
	}
	consumer := consumerWithBackend(
		&recordingConsumerBackend{fetches: recordFetches(records...)},
		10,
		time.Second,
		time.Second,
	)
	consumer.clientID = "batch-projection"
	consumer.groupID = "batch-projection-v1"
	consumer.observers = newObserverDispatcher(policy)

	result, err := consumer.RunBatchOnce(
		context.Background(),
		BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
			return nil
		}),
	)

	if err != nil {
		t.Fatalf("RunBatchOnce() error = %v", err)
	}
	if result != (PollResult{Polled: 2, Processed: 2, Committed: 2}) {
		t.Fatalf("RunBatchOnce() result = %#v", result)
	}
	if len(observations) != 3 {
		t.Fatalf("observations = %#v", observations)

		return
	}
	kinds := make([]ObservationKind, len(observations))
	for index := range observations {
		kinds[index] = observations[index].Kind
	}
	if !reflect.DeepEqual(kinds, []ObservationKind{
		ObservationConsumeBatch,
		ObservationConsumeCommit,
		ObservationConsumePoll,
	}) {
		t.Fatalf("observation kinds = %v", kinds)

		return
	}
	batch := observations[0]
	if batch.ClientID != "batch-projection" ||
		batch.GroupID != "batch-projection-v1" ||
		batch.Topic != "events" ||
		batch.Partition != 1 ||
		!batch.PartitionKnown ||
		batch.Offset != 5 ||
		!batch.OffsetKnown ||
		batch.RecordCount != 2 ||
		batch.PartitionCount != 1 ||
		batch.ProcessedCount != 2 ||
		batch.RecordBytes != consumedRecordSize(records[0])+
			consumedRecordSize(records[1]) ||
		!batch.Succeeded {
		t.Fatalf("batch observation = %#v", batch)
	}
}

func TestConsumerObserverContextFencesConsumerReentry(t *testing.T) {

	var consumer *Consumer
	var reentryErrors []error
	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(ctx context.Context, observation Observation) error {
				if observation.Kind != ObservationConsumeRecord {
					return nil
				}
				_, runOnceErr := consumer.RunOnce(ctx, HandlerFunc(
					func(context.Context, ConsumedMessage) error { return nil },
				))
				_, batchErr := consumer.RunBatchOnce(ctx, BatchHandlerFunc(
					func(context.Context, ConsumedBatch) error { return nil },
				))
				reentryErrors = append(
					reentryErrors,
					runOnceErr,
					batchErr,
					consumer.Run(ctx, HandlerFunc(
						func(context.Context, ConsumedMessage) error { return nil },
					)),
					consumer.Shutdown(ctx),
					consumer.Drain(ctx),
					consumer.PausePartitions(TopicPartition{
						Topic: "events", Partition: 1,
					}),
					consumer.ResumePartitions(TopicPartition{
						Topic: "events", Partition: 1,
					}),
					consumer.Close(),
				)

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}
	consumer = consumerWithBackend(
		&recordingConsumerBackend{fetches: recordFetches(&kgo.Record{
			Topic: "events", Partition: 1, Offset: 4, Key: []byte("one"),
		})},
		10,
		time.Second,
		time.Millisecond,
	)
	consumer.shutdownTimeout = time.Millisecond
	consumer.observers = newObserverDispatcher(policy)

	result, err := consumer.RunOnce(
		context.Background(),
		HandlerFunc(func(context.Context, ConsumedMessage) error {
			return nil
		}),
	)

	if err != nil ||
		result != (PollResult{Polled: 1, Processed: 1, Committed: 1}) {
		t.Fatalf("RunOnce() = %#v, %v", result, err)
	}
	if len(reentryErrors) != 8 {
		t.Fatalf("reentry errors = %d, want 8", len(reentryErrors))
	}
	for index, reentryErr := range reentryErrors {
		if !errors.Is(reentryErr, ErrObserverReentry) {
			t.Fatalf("reentry error %d = %v", index, reentryErr)
		}
	}
}

func TestConsumerConfigValidatesObserverPolicyWithoutAllocatingClient(t *testing.T) {

	config := validConsumerConfig()
	config.Observers = ObserverPolicy{
		Observers: []ObserverFunc{
			func(context.Context, Observation) error { return nil },
		},
	}

	if err := config.Validate(); !errors.Is(
		err,
		ErrObserverFailureHandlerRequired,
	) {
		t.Fatalf(
			"ConsumerConfig.Validate() error = %v, want %v",
			err,
			ErrObserverFailureHandlerRequired,
		)
	}

	factoryCalled := false
	_, err := newConsumer(config, func(...kgo.Opt) (*kgo.Client, error) {
		factoryCalled = true

		return nil, errors.New("unexpected allocation")
	})
	if !errors.Is(err, ErrObserverFailureHandlerRequired) {
		t.Fatalf("NewConsumer() error = %v", err)
	}
	if factoryCalled {
		t.Fatal("consumer factory called for invalid observer policy")
	}
}

func TestConsumerWiresRebalanceCallbacksToObservers(t *testing.T) {

	var observations []Observation
	config := validConsumerConfig()
	config.Observers = ObserverPolicy{
		Observers: []ObserverFunc{
			func(_ context.Context, observation Observation) error {
				observations = append(observations, observation)

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	}
	var franzClient *kgo.Client
	consumer, err := newConsumer(config, func(options ...kgo.Opt) (*kgo.Client, error) {
		client, clientErr := kgo.NewClient(options...)
		franzClient = client

		return client, clientErr
	})
	if err != nil {
		t.Fatalf("newConsumer() error = %v", err)
	}
	defer closeConsumerForTest(t, consumer)
	onAssigned, ok := franzClient.OptValue(kgo.OnPartitionsAssigned).(func(
		context.Context,
		*kgo.Client,
		map[string][]int32,
	))
	if !ok {
		t.Fatal("OnPartitionsAssigned option is not configured")
	}

	onAssigned(context.Background(), franzClient, map[string][]int32{
		"track.tracking-event.v1": {0},
	})

	if len(observations) != 1 ||
		observations[0].Kind != ObservationConsumeAssigned ||
		observations[0].PartitionCount != 1 {
		t.Fatalf("assignment observations = %#v", observations)
	}
}

func TestConsumerObserversFailClosedForInvalidFetchedMetadata(t *testing.T) {

	var observations []Observation
	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(_ context.Context, observation Observation) error {
				observations = append(observations, observation)

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}
	record := &kgo.Record{
		Topic: "events", Partition: 1, Offset: 4, Key: []byte("one"),
		Headers: make([]kgo.RecordHeader, DefaultMessageLimits().MaxHeaders+1),
	}
	consumer := consumerWithBackend(
		&recordingConsumerBackend{fetches: recordFetches(record)},
		10,
		time.Second,
		time.Second,
	)
	consumer.observers = newObserverDispatcher(policy)

	result, err := consumer.RunOnce(
		context.Background(),
		HandlerFunc(func(context.Context, ConsumedMessage) error {
			t.Fatal("handler called for invalid fetched record")

			return nil
		}),
	)

	if !errors.Is(err, ErrTooManyHeaders) {
		t.Fatalf("RunOnce() error = %v, want %v", err, ErrTooManyHeaders)
	}
	if result != (PollResult{Polled: 1}) {
		t.Fatalf("RunOnce() result = %#v", result)
	}
	if len(observations) != 2 {
		t.Fatalf("observations = %#v", observations)
	}
	recordObservation := observations[0]
	if recordObservation.Kind != ObservationConsumeRecord ||
		recordObservation.Topic != "" ||
		recordObservation.PartitionCount != 0 ||
		recordObservation.PartitionKnown ||
		recordObservation.OffsetKnown ||
		recordObservation.RecordBytes != 0 ||
		recordObservation.Succeeded ||
		recordObservation.Category != ErrorPermanent {
		t.Fatalf("record observation = %#v", recordObservation)
	}
	poll := observations[1]
	if poll.Kind != ObservationConsumePoll ||
		poll.Topic != "" ||
		poll.PartitionCount != 0 ||
		poll.RecordBytes != 0 ||
		poll.Succeeded ||
		poll.Category != ErrorPermanent {
		t.Fatalf("poll observation = %#v", poll)
	}
}

func TestConsumerObserversOmitMixedTopicCardinality(t *testing.T) {

	var observations []Observation
	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(_ context.Context, observation Observation) error {
				observations = append(observations, observation)

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}
	first := &kgo.Record{
		Topic: "events", Partition: 0, Offset: 4, Key: []byte("one"),
		Headers: []kgo.RecordHeader{{Key: "correlation", Value: []byte("one")}},
	}
	second := &kgo.Record{
		Topic: "commands", Partition: 1, Offset: 8, Key: []byte("two"),
	}
	fetches := kgo.Fetches{
		{Topics: []kgo.FetchTopic{
			{
				Topic: "events",
				Partitions: []kgo.FetchPartition{{
					Partition: 0, Records: []*kgo.Record{first},
				}},
			},
			{
				Topic: "commands",
				Partitions: []kgo.FetchPartition{{
					Partition: 1, Records: []*kgo.Record{second},
				}},
			},
		}},
	}
	consumer := consumerWithBackend(
		&recordingConsumerBackend{fetches: fetches},
		10,
		time.Second,
		time.Second,
	)
	consumer.observers = newObserverDispatcher(policy)

	result, err := consumer.RunOnce(
		context.Background(),
		HandlerFunc(func(context.Context, ConsumedMessage) error {
			return nil
		}),
	)

	if err != nil ||
		result != (PollResult{Polled: 2, Processed: 2, Committed: 2}) {
		t.Fatalf("RunOnce() = %#v, %v", result, err)
	}
	if len(observations) != 4 {
		t.Fatalf("observations = %#v", observations)

		return
	}
	commit := observations[len(observations)-2]
	poll := observations[len(observations)-1]
	if commit.Kind != ObservationConsumeCommit ||
		commit.Topic != "" ||
		commit.PartitionCount != 2 {
		t.Fatalf("commit observation = %#v", commit)
	}
	if poll.Kind != ObservationConsumePoll ||
		poll.Topic != "" ||
		poll.PartitionCount != 2 ||
		poll.RecordBytes != consumedRecordSize(first)+consumedRecordSize(second) {
		t.Fatalf("poll observation = %#v", poll)
	}
}

func TestConsumerObserverReportsEmptyPoll(t *testing.T) {

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
	consumer := consumerWithBackend(
		&recordingConsumerBackend{},
		10,
		time.Second,
		time.Second,
	)
	consumer.observers = newObserverDispatcher(policy)

	result, err := consumer.RunOnce(
		context.Background(),
		HandlerFunc(func(context.Context, ConsumedMessage) error {
			t.Fatal("handler called for empty poll")

			return nil
		}),
	)

	if err != nil || result != (PollResult{}) {
		t.Fatalf("RunOnce() = %#v, %v", result, err)
	}
	if got.Kind != ObservationConsumePoll ||
		got.RecordCount != 0 ||
		got.PartitionCount != 0 ||
		!got.Succeeded {
		t.Fatalf("poll observation = %#v", got)
	}
}

func TestConsumerBatchObserversReportHandlerAndMetadataFailures(t *testing.T) {

	handlerErr := errors.New("batch projection failed")
	for name, test := range map[string]struct {
		record    *kgo.Record
		handler   BatchHandler
		want      error
		wantKnown bool
	}{
		"handler": {
			record: &kgo.Record{
				Topic: "events", Partition: 1, Offset: 4, Key: []byte("one"),
			},
			handler: BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
				return handlerErr
			}),
			want:      handlerErr,
			wantKnown: true,
		},
		"metadata": {
			record: &kgo.Record{
				Topic: "events", Partition: 1, Offset: 4, Key: []byte("one"),
				Headers: make(
					[]kgo.RecordHeader,
					DefaultMessageLimits().MaxHeaders+1,
				),
			},
			handler: BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
				t.Fatal("handler called for invalid batch metadata")

				return nil
			}),
			want: ErrTooManyHeaders,
		},
	} {
		test := test
		t.Run(name, func(t *testing.T) {

			var observations []Observation
			policy, err := normalizeObserverPolicy(ObserverPolicy{
				Observers: []ObserverFunc{
					func(_ context.Context, observation Observation) error {
						observations = append(observations, observation)

						return nil
					},
				},
				FailureHandler: func(context.Context, ObservationFailure) {},
			})
			if err != nil {
				t.Fatalf("normalize observer policy: %v", err)
			}
			consumer := consumerWithBackend(
				&recordingConsumerBackend{
					fetches: recordFetches(test.record),
				},
				10,
				time.Second,
				time.Second,
			)
			consumer.observers = newObserverDispatcher(policy)

			result, err := consumer.RunBatchOnce(context.Background(), test.handler)

			if !errors.Is(err, test.want) {
				t.Fatalf("RunBatchOnce() error = %v, want %v", err, test.want)
			}
			if result != (PollResult{Polled: 1}) {
				t.Fatalf("RunBatchOnce() result = %#v", result)
			}
			if len(observations) != 2 {
				t.Fatalf("observations = %#v", observations)
			}
			batch := observations[0]
			if batch.Kind != ObservationConsumeBatch ||
				batch.Topic != map[bool]string{true: "events"}[test.wantKnown] ||
				batch.PartitionCount != map[bool]int{true: 1}[test.wantKnown] ||
				batch.PartitionKnown != test.wantKnown ||
				batch.OffsetKnown != test.wantKnown ||
				batch.Succeeded ||
				batch.Category != ErrorPermanent ||
				(batch.ProcessedCount != 0) {
				t.Fatalf("batch observation = %#v", batch)
			}
		})
	}
}

func TestConsumerRejectsBackendPollBeyondConfiguredRecordLimit(t *testing.T) {

	records := []*kgo.Record{
		{Topic: "events", Partition: 1, Offset: 4, Key: []byte("one")},
		{Topic: "events", Partition: 1, Offset: 5, Key: []byte("two")},
	}
	for name, run := range map[string]func(*Consumer) (PollResult, error){
		"record": func(consumer *Consumer) (PollResult, error) {
			return consumer.RunOnce(
				context.Background(),
				HandlerFunc(func(context.Context, ConsumedMessage) error {
					t.Fatal("record handler called beyond poll limit")

					return nil
				}),
			)
		},
		"batch": func(consumer *Consumer) (PollResult, error) {
			return consumer.RunBatchOnce(
				context.Background(),
				BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
					t.Fatal("batch handler called beyond poll limit")

					return nil
				}),
			)
		},
	} {
		run := run
		t.Run(name, func(t *testing.T) {

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
			consumer := consumerWithBackend(
				&recordingConsumerBackend{fetches: recordFetches(records...)},
				1,
				time.Second,
				time.Second,
			)
			consumer.observers = newObserverDispatcher(policy)

			result, err := run(consumer)

			if !errors.Is(err, ErrTooManyFetchedRecords) {
				t.Fatalf(
					"consumer run error = %v, want %v",
					err,
					ErrTooManyFetchedRecords,
				)
			}
			if result != (PollResult{Polled: 2}) {
				t.Fatalf("consumer result = %#v", result)
			}
			if got.Kind != ObservationConsumePoll ||
				got.RecordCount != 1 ||
				got.PartitionCount != 0 ||
				got.RecordBytes != 0 ||
				!got.Truncated ||
				got.Succeeded ||
				got.Category != ErrorPermanent {
				t.Fatalf("poll observation = %#v", got)
			}
		})
	}
}

func TestConsumerObserverPreservesFailurePolicyCategory(t *testing.T) {

	var got Observation
	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(_ context.Context, observation Observation) error {
				if observation.Kind == ObservationConsumeRecord {
					got = observation
				}

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}
	consumer := consumerWithBackend(
		&recordingConsumerBackend{fetches: recordFetches(&kgo.Record{
			Topic: "events", Partition: 1, Offset: 4, Key: []byte("one"),
		})},
		10,
		time.Second,
		time.Second,
	)
	consumer.observers = newObserverDispatcher(policy)
	handlerErr := newFailureHandlingError(
		FailureStageStop,
		ErrorAuthorization,
		1,
		errors.New("application failure"),
	)

	_, err = consumer.RunOnce(
		context.Background(),
		HandlerFunc(func(context.Context, ConsumedMessage) error {
			return handlerErr
		}),
	)

	if !errors.Is(err, handlerErr) {
		t.Fatalf("RunOnce() error = %v, want %v", err, handlerErr)
	}
	if got.Succeeded || got.Category != ErrorAuthorization {
		t.Fatalf("record observation = %#v", got)
	}
}

func TestConsumerObserverContainsPanickingErrorClassification(t *testing.T) {

	var got Observation
	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(_ context.Context, observation Observation) error {
				if observation.Kind == ObservationConsumeRecord {
					got = observation
				}

				return nil
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {},
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}
	consumer := consumerWithBackend(
		&recordingConsumerBackend{fetches: recordFetches(&kgo.Record{
			Topic: "events", Partition: 1, Offset: 4, Key: []byte("one"),
		})},
		10,
		time.Second,
		time.Second,
	)
	consumer.observers = newObserverDispatcher(policy)
	handlerErr := panickingObservationCategoryError{}

	_, err = consumer.RunOnce(
		context.Background(),
		HandlerFunc(func(context.Context, ConsumedMessage) error {
			return handlerErr
		}),
	)

	if err != handlerErr {
		t.Fatalf("RunOnce() error = %v, want original handler error", err)
	}
	if got.Succeeded || got.Category != ErrorPermanent {
		t.Fatalf("record observation = %#v", got)
	}
}

type panickingObservationCategoryError struct{}

func (panickingObservationCategoryError) Error() string {
	return "categorized handler failure"
}

func (panickingObservationCategoryError) Category() ErrorCategory {
	panic("sensitive category panic")
}

func TestConsumerRecordObserversCanRunAcrossPartitionsConcurrently(t *testing.T) {

	var entered atomic.Int32
	var failures atomic.Int32
	bothEntered := make(chan struct{})
	policy, err := normalizeObserverPolicy(ObserverPolicy{
		Observers: []ObserverFunc{
			func(ctx context.Context, observation Observation) error {
				if observation.Kind != ObservationConsumeRecord {
					return nil
				}
				if entered.Add(1) == 2 {
					close(bothEntered)
				}
				select {
				case <-bothEntered:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			},
		},
		FailureHandler: func(context.Context, ObservationFailure) {
			failures.Add(1)
		},
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("normalize observer policy: %v", err)
	}
	fetches := kgo.Fetches{{
		Topics: []kgo.FetchTopic{{
			Topic: "events",
			Partitions: []kgo.FetchPartition{
				{
					Partition: 0,
					Records: []*kgo.Record{{
						Topic: "events", Partition: 0, Offset: 4,
						Key: []byte("one"),
					}},
				},
				{
					Partition: 1,
					Records: []*kgo.Record{{
						Topic: "events", Partition: 1, Offset: 8,
						Key: []byte("two"),
					}},
				},
			},
		}},
	}}
	consumer := consumerWithBackend(
		&recordingConsumerBackend{fetches: fetches},
		10,
		time.Second,
		time.Second,
	)
	consumer.maxConcurrentHandlers = 2
	consumer.observers = newObserverDispatcher(policy)

	result, err := consumer.RunOnce(
		context.Background(),
		HandlerFunc(func(context.Context, ConsumedMessage) error {
			return nil
		}),
	)

	if err != nil ||
		result != (PollResult{Polled: 2, Processed: 2, Committed: 2}) {
		t.Fatalf("RunOnce() = %#v, %v", result, err)
	}
	if entered.Load() != 2 {
		t.Fatalf("record observers entered = %d, want 2", entered.Load())
	}
	if failures.Load() != 0 {
		t.Fatalf("observer failures = %d, want 0", failures.Load())
	}
}
