//go:build integration

package kafka_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestApacheKafkaConsumerOwnershipTransitions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cluster := startApacheKafkaCluster(t, ctx)
	cluster.observeFailureState(t)
	cluster.assertRuntimeVersion(t, ctx, "4.3.1")
	brokers := cluster.brokers(t, ctx)
	waitForApacheBrokerEndpoints(t, ctx, brokers)

	t.Run("partial cooperative revocation", func(t *testing.T) {
		proveApacheKafkaPartialCooperativeRevocation(t, ctx, brokers)
	})
	t.Run("broker-forced ownership loss", func(t *testing.T) {
		proveApacheKafkaForcedOwnershipLoss(t, ctx, brokers)
	})
}

func proveApacheKafkaPartialCooperativeRevocation(
	t *testing.T,
	ctx context.Context,
	brokers []string,
) {
	t.Helper()

	topic := fmt.Sprintf(
		"golib-apache-partial-cooperative-revocation-%d",
		time.Now().UnixNano(),
	)
	groupID := topic + "-group"
	createApacheKafkaTopic(t, ctx, brokers, topic, 2)

	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       brokers,
		ClientID:      "golib-apache-partial-cooperative-producer",
		AllowedTopics: []string{topic},
		Security:      kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct partial cooperative producer: %v", err)
	}
	defer func() {
		if closeErr := producer.Close(); closeErr != nil {
			t.Errorf("close partial cooperative producer: %v", closeErr)
		}
	}()

	blocked := make(chan struct{})
	revoked := make(chan kafka.Observation, 1)
	var blockedOnce sync.Once
	original := newApacheKafkaOwnershipConsumer(
		t,
		brokers,
		topic,
		groupID,
		"golib-apache-partial-cooperative-original",
		"",
		kafka.ObserverPolicy{
			Observers: []kafka.ObserverFunc{func(
				_ context.Context,
				observation kafka.Observation,
			) error {
				switch observation.Kind {
				case kafka.ObservationConsumeBlocked:
					blockedOnce.Do(func() { close(blocked) })
				case kafka.ObservationConsumeRevoked:
					if observation.PartitionCount > 0 {
						select {
						case revoked <- observation:
						default:
						}
					}
				}

				return nil
			}},
			FailureHandler: func(context.Context, kafka.ObservationFailure) {},
		},
	)
	defer closeApacheKafkaConsumer(t, original)

	waitForApacheKafkaConsumerPartitions(t, ctx, original, topic, 2)
	partitions := []kafka.TopicPartition{
		{Topic: topic, Partition: 0},
		{Topic: topic, Partition: 1},
	}
	if err := original.PausePartitions(partitions...); err != nil {
		t.Fatalf("pause partial cooperative partitions: %v", err)
	}
	publishApacheKafkaPartitionRecords(t, ctx, producer, topic, 0)
	if err := original.ResumePartitions(partitions...); err != nil {
		t.Fatalf("resume partial cooperative partitions: %v", err)
	}
	waitForApacheKafkaBufferedConsumerRecords(t, ctx, original, 2)

	handling := make(chan int32, 2)
	release := make(chan struct{})
	originalResult := make(chan apacheKafkaConsumerRunResult, 1)
	go func() {
		result, runErr := original.RunOnce(
			ctx,
			kafka.HandlerFunc(func(
				handlerCtx context.Context,
				record kafka.ConsumedRecord,
			) error {
				handling <- record.Partition
				select {
				case <-release:
					return nil
				case <-handlerCtx.Done():
					return context.Cause(handlerCtx)
				}
			}),
		)
		originalResult <- apacheKafkaConsumerRunResult{result: result, err: runErr}
	}()
	waitForApacheKafkaHandledPartitions(t, ctx, handling, 0, 1)

	replacement := newApacheKafkaOwnershipConsumer(
		t,
		brokers,
		topic,
		groupID,
		"golib-apache-partial-cooperative-replacement",
		"",
		kafka.ObserverPolicy{},
	)
	defer closeApacheKafkaConsumer(t, replacement)
	replacementResult := make(chan apacheKafkaConsumerRecordRun, 1)
	go runApacheKafkaOneRecord(ctx, replacement, replacementResult)

	select {
	case <-blocked:
	case <-ctx.Done():
		t.Fatalf(
			"wait for partial cooperative rebalance block: %v",
			context.Cause(ctx),
		)
	}
	close(release)
	select {
	case run := <-originalResult:
		if run.err != nil || run.result != (kafka.PollResult{
			Polled: 2, Processed: 2, Committed: 2,
		}) {
			t.Fatalf("partial cooperative original result = %#v", run)
		}
	case <-ctx.Done():
		t.Fatalf(
			"wait for partial cooperative original: %v",
			context.Cause(ctx),
		)
	}

	select {
	case observation := <-revoked:
		if !observation.Succeeded || observation.PartitionCount != 1 ||
			observation.Truncated || observation.Category != kafka.ErrorUnknown {
			t.Fatalf("partial cooperative revocation = %#v", observation)
		}
	case <-ctx.Done():
		t.Fatalf(
			"wait for partial cooperative revocation: %v",
			context.Cause(ctx),
		)
	}
	originalPartition := waitForApacheKafkaConsumerPartition(
		t,
		ctx,
		original,
		topic,
	)
	replacementPartition := waitForApacheKafkaConsumerPartition(
		t,
		ctx,
		replacement,
		topic,
	)
	if originalPartition == replacementPartition {
		t.Fatalf(
			"partial cooperative assignments overlap on partition %d",
			originalPartition,
		)
	}
	assertApacheKafkaGroupCommits(
		t,
		ctx,
		brokers,
		groupID,
		map[kafka.TopicPartition]int64{
			{Topic: topic, Partition: 0}: 1,
			{Topic: topic, Partition: 1}: 1,
		},
	)

	publishApacheKafkaPartitionRecords(t, ctx, producer, topic, 1)
	originalNext := make(chan apacheKafkaConsumerRecordRun, 1)
	go runApacheKafkaOneRecord(ctx, original, originalNext)
	assertApacheKafkaOneRecordRun(
		t,
		ctx,
		waitForApacheKafkaConsumerRecordRun(ctx, originalNext),
		originalPartition,
		1,
	)
	assertApacheKafkaOneRecordRun(
		t,
		ctx,
		waitForApacheKafkaConsumerRecordRun(ctx, replacementResult),
		replacementPartition,
		1,
	)
	assertApacheKafkaGroupCommits(
		t,
		ctx,
		brokers,
		groupID,
		map[kafka.TopicPartition]int64{
			{Topic: topic, Partition: 0}: 2,
			{Topic: topic, Partition: 1}: 2,
		},
	)
}

func proveApacheKafkaForcedOwnershipLoss(
	t *testing.T,
	ctx context.Context,
	brokers []string,
) {
	t.Helper()

	topic := fmt.Sprintf(
		"golib-apache-forced-ownership-loss-%d",
		time.Now().UnixNano(),
	)
	groupID := topic + "-group"
	instanceID := topic + "-instance"
	createApacheKafkaTopic(t, ctx, brokers, topic, 1)

	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       brokers,
		ClientID:      "golib-apache-forced-loss-producer",
		AllowedTopics: []string{topic},
		Security:      kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct forced-loss producer: %v", err)
	}
	defer func() {
		if closeErr := producer.Close(); closeErr != nil {
			t.Errorf("close forced-loss producer: %v", closeErr)
		}
	}()

	blocked := make(chan struct{})
	lost := make(chan kafka.Observation, 1)
	var blockedOnce sync.Once
	original := newApacheKafkaOwnershipConsumer(
		t,
		brokers,
		topic,
		groupID,
		"golib-apache-forced-loss-original",
		instanceID,
		kafka.ObserverPolicy{
			Observers: []kafka.ObserverFunc{func(
				_ context.Context,
				observation kafka.Observation,
			) error {
				switch observation.Kind {
				case kafka.ObservationConsumeBlocked:
					blockedOnce.Do(func() { close(blocked) })
				case kafka.ObservationConsumeLost:
					select {
					case lost <- observation:
					default:
					}
				}

				return nil
			}},
			FailureHandler: func(context.Context, kafka.ObservationFailure) {},
		},
	)
	originalClosed := false
	defer func() {
		if !originalClosed {
			closeApacheKafkaConsumer(t, original)
		}
	}()
	waitForApacheKafkaConsumerPartitions(t, ctx, original, topic, 1)
	partition := kafka.TopicPartition{Topic: topic, Partition: 0}
	if err := original.PausePartitions(partition); err != nil {
		t.Fatalf("pause forced-loss partition: %v", err)
	}
	if result := producer.PublishRecord(ctx, kafka.ProducerRecord{
		Topic:     topic,
		Partition: kafka.ExplicitPartition(0),
		Key:       []byte("forced-loss-offset-0"),
		Value:     []byte("forced-loss-offset-0"),
	}); result.Err != nil {
		t.Fatalf("publish forced-loss record: %v", result.Err)
	}
	if err := original.ResumePartitions(partition); err != nil {
		t.Fatalf("resume forced-loss partition: %v", err)
	}
	waitForApacheKafkaBufferedConsumerRecords(t, ctx, original, 1)

	handling := make(chan struct{})
	release := make(chan struct{})
	runResult := make(chan apacheKafkaConsumerRunResult, 1)
	go func() {
		result, runErr := original.RunOnce(
			ctx,
			kafka.HandlerFunc(func(
				handlerCtx context.Context,
				_ kafka.ConsumedRecord,
			) error {
				close(handling)
				select {
				case <-release:
					return nil
				case <-handlerCtx.Done():
					return context.Cause(handlerCtx)
				}
			}),
		)
		runResult <- apacheKafkaConsumerRunResult{result: result, err: runErr}
	}()
	select {
	case <-handling:
	case <-ctx.Done():
		t.Fatalf("wait for forced-loss handler: %v", context.Cause(ctx))
	}

	adminClient, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		t.Fatalf("construct forced-loss admin client: %v", err)
	}
	defer adminClient.Close()
	responses, err := kadm.NewClient(adminClient).LeaveGroup(
		ctx,
		kadm.LeaveGroup(groupID).
			InstanceIDs(instanceID).
			Reason("broker-forced ownership-loss integration proof"),
	)
	if err != nil || !responses.Ok() || len(responses) != 1 {
		t.Fatalf("remove forced-loss group member = %#v, %v", responses, err)
	}
	select {
	case <-blocked:
	case <-ctx.Done():
		t.Fatalf("wait for forced-loss rebalance block: %v", context.Cause(ctx))
	}
	close(release)

	select {
	case run := <-runResult:
		if run.result != (kafka.PollResult{Polled: 1, Processed: 1}) ||
			!errors.Is(run.err, kerr.UnknownMemberID) {
			t.Fatalf("forced-loss original result = %#v", run)
		}
	case <-ctx.Done():
		t.Fatalf("wait for forced-loss result: %v", context.Cause(ctx))
	}
	select {
	case observation := <-lost:
		if observation.Succeeded || observation.PartitionCount != 1 ||
			observation.Truncated || observation.Category != kafka.ErrorFenced {
			t.Fatalf("forced-loss observation = %#v", observation)
		}
	case <-ctx.Done():
		t.Fatalf("wait for forced-loss callback: %v", context.Cause(ctx))
	}
	assignment, assignmentErr := original.Assignment()
	if assignmentErr != nil || !assignment.Lost ||
		len(assignment.Partitions) != 0 {
		t.Fatalf("forced-loss assignment = %#v, %v", assignment, assignmentErr)
	}
	closeApacheKafkaConsumer(t, original)
	originalClosed = true

	replacement := newApacheKafkaOwnershipConsumer(
		t,
		brokers,
		topic,
		groupID,
		"golib-apache-forced-loss-replacement",
		"",
		kafka.ObserverPolicy{},
	)
	defer closeApacheKafkaConsumer(t, replacement)
	replacementResult := make(chan apacheKafkaConsumerRecordRun, 1)
	go runApacheKafkaOneRecord(ctx, replacement, replacementResult)
	assertApacheKafkaOneRecordRun(
		t,
		ctx,
		waitForApacheKafkaConsumerRecordRun(ctx, replacementResult),
		0,
		0,
	)
	assertApacheKafkaGroupCommits(
		t,
		ctx,
		brokers,
		groupID,
		map[kafka.TopicPartition]int64{partition: 1},
	)
}

type apacheKafkaConsumerRunResult struct {
	result kafka.PollResult
	err    error
}

type apacheKafkaConsumerRecordRun struct {
	result    kafka.PollResult
	partition int32
	offset    int64
	err       error
}

func newApacheKafkaOwnershipConsumer(
	t *testing.T,
	brokers []string,
	topic string,
	groupID string,
	clientID string,
	instanceID string,
	observers kafka.ObserverPolicy,
) *kafka.Consumer {
	t.Helper()

	consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:               brokers,
		ClientID:              clientID,
		GroupID:               groupID,
		InstanceID:            instanceID,
		Topics:                []string{topic},
		ResetOffset:           kafka.OffsetEarliest,
		BalancePolicy:         kafka.BalanceCooperativeSticky,
		RebalanceHandler:      kafka.RebalanceDrainHandler,
		MaxPollRecords:        2,
		MaxConcurrentHandlers: 2,
		MaxAssignedPartitions: 2,
		FetchMaxWait:          100 * time.Millisecond,
		SessionTimeout:        6 * time.Second,
		HeartbeatInterval:     time.Second,
		RebalanceTimeout:      30 * time.Second,
		HandlerTimeout:        20 * time.Second,
		CommitTimeout:         3 * time.Second,
		ShutdownTimeout:       10 * time.Second,
		Security:              kafka.DevelopmentPlaintextSecurity(),
		Observers:             observers,
	})
	if err != nil {
		t.Fatalf("construct ownership consumer %q: %v", clientID, err)
	}

	return consumer
}

func closeApacheKafkaConsumer(t *testing.T, consumer *kafka.Consumer) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		err := consumer.Close()
		if err == nil {
			return
		}
		if !errors.Is(err, kafka.ErrObserverReentry) || time.Now().After(deadline) {
			t.Errorf("close ownership consumer: %v", err)

			return
		}
		<-ticker.C
	}
}

func waitForApacheKafkaConsumerPartitions(
	t *testing.T,
	ctx context.Context,
	consumer *kafka.Consumer,
	topic string,
	want int,
) []kafka.TopicPartition {
	t.Helper()

	runCtx, cancelRun := context.WithCancel(ctx)
	runResult := make(chan error, 1)
	go func() {
		_, runErr := consumer.RunOnce(
			runCtx,
			kafka.HandlerFunc(func(context.Context, kafka.ConsumedRecord) error {
				return nil
			}),
		)
		runResult <- runErr
	}()
	partitions := waitForApacheKafkaOwnershipAssignment(
		t,
		ctx,
		consumer,
		topic,
		want,
	)
	cancelRun()
	select {
	case runErr := <-runResult:
		if runErr != nil && !errors.Is(runErr, context.Canceled) {
			t.Fatalf("stop ownership assignment poll: %v", runErr)
		}
	case <-ctx.Done():
		t.Fatalf("stop ownership assignment poll: %v", context.Cause(ctx))
	}

	return partitions
}

func waitForApacheKafkaOwnershipAssignment(
	t *testing.T,
	ctx context.Context,
	consumer *kafka.Consumer,
	topic string,
	want int,
) []kafka.TopicPartition {
	t.Helper()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	var last kafka.ConsumerAssignment
	var lastErr error
	for {
		last, lastErr = consumer.Assignment()
		if lastErr == nil && len(last.Partitions) == want {
			valid := true
			for _, partition := range last.Partitions {
				if partition.Topic != topic {
					valid = false
				}
			}
			if valid {
				return last.Partitions
			}
		}

		select {
		case <-ctx.Done():
			t.Fatalf(
				"wait for %d ownership partitions: %v; assignment = %#v; error = %v",
				want,
				context.Cause(ctx),
				last,
				lastErr,
			)
		case <-ticker.C:
		}
	}
}

func waitForApacheKafkaConsumerPartition(
	t *testing.T,
	ctx context.Context,
	consumer *kafka.Consumer,
	topic string,
) int32 {
	t.Helper()

	return waitForApacheKafkaOwnershipAssignment(
		t,
		ctx,
		consumer,
		topic,
		1,
	)[0].Partition
}

func waitForApacheKafkaHandledPartitions(
	t *testing.T,
	ctx context.Context,
	handled <-chan int32,
	want ...int32,
) {
	t.Helper()

	seen := make(map[int32]bool, len(want))
	for len(seen) < len(want) {
		select {
		case partition := <-handled:
			if !containsApacheKafkaPartition(want, partition) || seen[partition] {
				t.Fatalf("unexpected handled partition %d", partition)
			}
			seen[partition] = true
		case <-ctx.Done():
			t.Fatalf("wait for handled partitions: %v", context.Cause(ctx))
		}
	}
}

func containsApacheKafkaPartition(partitions []int32, want int32) bool {
	for _, partition := range partitions {
		if partition == want {
			return true
		}
	}

	return false
}

func runApacheKafkaOneRecord(
	ctx context.Context,
	consumer *kafka.Consumer,
	result chan<- apacheKafkaConsumerRecordRun,
) {
	var run apacheKafkaConsumerRecordRun
	for run.result.Polled == 0 && run.err == nil && context.Cause(ctx) == nil {
		run.result, run.err = consumer.RunOnce(
			ctx,
			kafka.HandlerFunc(func(
				_ context.Context,
				record kafka.ConsumedRecord,
			) error {
				run.partition = record.Partition
				run.offset = record.Offset

				return nil
			}),
		)
	}
	result <- run
}

func waitForApacheKafkaConsumerRecordRun(
	ctx context.Context,
	result <-chan apacheKafkaConsumerRecordRun,
) apacheKafkaConsumerRecordRun {
	select {
	case run := <-result:
		return run
	case <-ctx.Done():
		return apacheKafkaConsumerRecordRun{err: context.Cause(ctx)}
	}
}

func assertApacheKafkaOneRecordRun(
	t *testing.T,
	ctx context.Context,
	run apacheKafkaConsumerRecordRun,
	wantPartition int32,
	wantOffset int64,
) {
	t.Helper()

	if run.err != nil ||
		run.result != (kafka.PollResult{Polled: 1, Processed: 1, Committed: 1}) ||
		run.partition != wantPartition ||
		run.offset != wantOffset {
		t.Fatalf(
			"one-record ownership run = %#v, context error = %v",
			run,
			context.Cause(ctx),
		)
	}
}
