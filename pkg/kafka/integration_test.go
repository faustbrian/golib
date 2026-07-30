//go:build integration

package kafka_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

const integrationKafkaImage = "confluentinc/confluent-local:7.5.0@" +
	"sha256:8e391de42cfcd3498e7317dcf159790f1f1cc3f3ffce900b30d7da23888687fd"

func TestKafkaProducerConsumerCompatibility(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	container, err := tckafka.Run(
		ctx,
		integrationKafkaImage,
	)
	if err != nil {
		t.Fatalf("start Kafka: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			30*time.Second,
		)
		defer cleanupCancel()
		if err := container.Terminate(cleanupCtx); err != nil {
			t.Errorf("terminate Kafka: %v", err)
		}
	})

	brokers, err := container.Brokers(ctx)
	if err != nil {
		t.Fatalf("resolve Kafka brokers: %v", err)
	}
	topic := fmt.Sprintf("golib-compatibility-%d", time.Now().UnixNano())
	explicitTopic := topic + "-explicit"
	settlementTopic := topic + "-settlement"
	membershipTopic := topic + "-membership"
	staticFencingTopic := topic + "-static-fencing"
	pauseTopic := topic + "-pause"
	rebalanceTopic := topic + "-rebalance"
	batchTopic := topic + "-batch"
	batchFailureTopic := topic + "-batch-failure-v1"
	producerModesTopic := topic + "-producer-modes"
	producerThrottleTopic := topic + "-producer-throttle"
	transactionTopic := topic + "-transaction"
	transactionSourceTopic := topic + "-transaction-source"
	transactionOutputTopic := topic + "-transaction-output"
	retrySourceTopic := topic + "-retry-source"
	retryTopic := topic + "-retry-v2"
	deadLetterSourceTopic := topic + "-dead-letter-source"
	deadLetterTopic := topic + "-dead-letter-v3"
	replayTopic := topic + "-replay"
	var brokerConnectObserved atomic.Bool
	var brokerRequestObserved atomic.Bool
	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:  brokers,
		ClientID: "golib-compatibility-producer",
		AllowedTopics: []string{
			topic, explicitTopic, settlementTopic, membershipTopic, pauseTopic,
			staticFencingTopic, rebalanceTopic, batchTopic, producerModesTopic,
			batchFailureTopic,
			transactionSourceTopic,
			retrySourceTopic, retryTopic, deadLetterSourceTopic, deadLetterTopic,
			replayTopic,
		},
		CompressionPreferences: []kafka.CompressionCodec{kafka.CompressionZstd},
		Security:               kafka.DevelopmentPlaintextSecurity(),
		Observers: kafka.ObserverPolicy{
			Observers: []kafka.ObserverFunc{
				func(_ context.Context, observation kafka.Observation) error {
					switch observation.Kind {
					case kafka.ObservationBrokerConnect:
						if observation.ClientID == "golib-compatibility-producer" &&
							observation.Duration >= 0 {
							brokerConnectObserved.Store(true)
						}
					case kafka.ObservationBrokerRequest:
						if observation.ClientID == "golib-compatibility-producer" &&
							observation.APIKeyKnown &&
							observation.RequestBytes > 0 &&
							observation.Duration >= observation.QueueDuration {
							brokerRequestObserved.Store(true)
						}
					}

					return nil
				},
			},
			FailureHandler: func(context.Context, kafka.ObservationFailure) {},
		},
	})
	if err != nil {
		t.Fatalf("construct producer: %v", err)
	}
	t.Cleanup(func() {
		if err := producer.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	createIntegrationTopic(t, ctx, brokers, topic, 1)
	createIntegrationTopicWithConfigs(
		t,
		ctx,
		brokers,
		explicitTopic,
		4,
		map[string]*string{
			"cleanup.policy":                 kadm.StringPtr("compact,delete"),
			"retention.ms":                   kadm.StringPtr("86400000"),
			"retention.bytes":                kadm.StringPtr("10485760"),
			"delete.retention.ms":            kadm.StringPtr("43200000"),
			"min.compaction.lag.ms":          kadm.StringPtr("60000"),
			"max.compaction.lag.ms":          kadm.StringPtr("3600000"),
			"min.cleanable.dirty.ratio":      kadm.StringPtr("0.75"),
			"segment.bytes":                  kadm.StringPtr("1048576"),
			"segment.ms":                     kadm.StringPtr("900000"),
			"unclean.leader.election.enable": kadm.StringPtr("true"),
		},
	)
	createIntegrationTopic(t, ctx, brokers, settlementTopic, 2)
	createIntegrationTopic(t, ctx, brokers, membershipTopic, 2)
	createIntegrationTopic(t, ctx, brokers, staticFencingTopic, 1)
	createIntegrationTopic(t, ctx, brokers, pauseTopic, 1)
	createIntegrationTopic(t, ctx, brokers, rebalanceTopic, 1)
	createIntegrationTopic(t, ctx, brokers, batchTopic, 2)
	createIntegrationTopic(t, ctx, brokers, batchFailureTopic, 1)
	createIntegrationTopic(t, ctx, brokers, producerModesTopic, 1)
	createIntegrationTopic(t, ctx, brokers, producerThrottleTopic, 1)
	createIntegrationTopic(t, ctx, brokers, transactionTopic, 1)
	createIntegrationTopic(t, ctx, brokers, transactionSourceTopic, 1)
	createIntegrationTopic(t, ctx, brokers, transactionOutputTopic, 1)
	createIntegrationTopic(t, ctx, brokers, retrySourceTopic, 1)
	createIntegrationTopic(t, ctx, brokers, retryTopic, 1)
	createIntegrationTopic(t, ctx, brokers, deadLetterSourceTopic, 1)
	createIntegrationTopic(t, ctx, brokers, deadLetterTopic, 1)
	createIntegrationTopic(t, ctx, brokers, replayTopic, 2)
	assertInspectionState(t, ctx, brokers, explicitTopic)
	if err := producer.Health(ctx); err != nil {
		t.Fatalf("check Kafka health: %v", err)
	}
	if !brokerConnectObserved.Load() || !brokerRequestObserved.Load() {
		t.Fatalf(
			"broker observations connect/request = %t/%t",
			brokerConnectObserved.Load(),
			brokerRequestObserved.Load(),
		)
	}
	explicitResult := producer.PublishRecord(ctx, kafka.ProducerRecord{
		Topic:     explicitTopic,
		Partition: kafka.ExplicitPartition(3),
		Key:       []byte("aggregate-explicit"),
		Value:     []byte("explicit"),
	})
	if explicitResult.Err != nil || explicitResult.Partition != 3 {
		t.Fatalf("explicit partition delivery = %#v", explicitResult)
	}
	proveProducerModes(t, ctx, brokers, producer, producerModesTopic)
	proveProducerThrottle(t, ctx, brokers, producerThrottleTopic)

	for index, value := range []string{"first", "second", "third"} {
		err := producer.Publish(ctx, kafka.Message{
			Topic: topic,
			Key:   []byte("aggregate-1"),
			Value: []byte(value),
			Headers: []kafka.Header{{
				Key:   "event-index",
				Value: []byte(fmt.Sprintf("%d", index)),
			}},
		})
		if err != nil {
			t.Fatalf("publish message %d: %v", index, err)
		}
	}

	values := consumeValues(
		t,
		ctx,
		brokers,
		topic,
		"golib-compatibility-success",
		3,
	)
	if !slices.Equal(values, []string{"first", "second", "third"}) {
		t.Fatalf("consumed values = %q", values)
	}
	assertGroupCommitted(
		t,
		ctx,
		brokers,
		topic,
		"golib-compatibility-success",
	)

	failedConsumer := newIntegrationConsumer(
		t,
		brokers,
		topic,
		"golib-compatibility-retry",
	)
	processingFailure := errors.New("injected processing failure")
	func() {
		defer failedConsumer.Close()
		for {
			result, err := failedConsumer.RunOnce(
				ctx,
				kafka.HandlerFunc(func(
					context.Context,
					kafka.ConsumedMessage,
				) error {
					return processingFailure
				}),
			)
			if result.Polled == 0 && err == nil {
				continue
			}
			if !errors.Is(err, processingFailure) ||
				result.Committed != 0 {
				t.Fatalf(
					"failed delivery result = %#v, error = %v",
					result,
					err,
				)
			}
			break
		}
	}()

	retried := consumeValues(
		t,
		ctx,
		brokers,
		topic,
		"golib-compatibility-retry",
		3,
	)
	if !slices.Equal(retried, values) {
		t.Fatalf("retried values = %q, want %q", retried, values)
	}

	provePartitionSettlement(t, ctx, brokers, producer, settlementTopic)
	proveMembershipPolicy(t, ctx, brokers, producer, membershipTopic)
	proveStaticMemberFencing(t, ctx, brokers, producer, staticFencingTopic)
	provePauseResumePolicy(t, ctx, brokers, producer, pauseTopic)
	proveBlockedRebalancePolicy(t, ctx, brokers, producer, rebalanceTopic)
	proveBatchPolicy(t, ctx, brokers, producer, batchTopic, batchFailureTopic)
	proveProducerTransactionVisibility(t, ctx, brokers, transactionTopic)
	proveConsumeTransformProduce(
		t,
		ctx,
		brokers,
		producer,
		transactionSourceTopic,
		transactionOutputTopic,
		"golib-compatibility-"+transactionSourceTopic,
	)
	proveFailureTopicPolicy(
		t,
		ctx,
		brokers,
		retrySourceTopic,
		retryTopic,
		deadLetterSourceTopic,
		deadLetterTopic,
	)
	proveReplayPolicy(t, ctx, brokers, producer, replayTopic)
}

func proveProducerModes(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	producer *kafka.Producer,
	topic string,
) {
	t.Helper()

	batch, err := producer.PublishBatch(ctx, []kafka.ProducerRecord{
		{
			Topic:     topic,
			Partition: kafka.ExplicitPartition(0),
			Key:       []byte("producer-modes"),
			Value:     []byte("batch-first"),
		},
		{
			Topic:     topic,
			Partition: kafka.ExplicitPartition(0),
			Key:       []byte("producer-modes"),
			Value:     []byte("batch-second"),
		},
	})
	if err != nil {
		t.Fatalf("publish producer-mode batch: %v", err)
	}
	if len(batch) != 2 {
		t.Fatalf("producer-mode batch results = %d, want 2", len(batch))
	}
	for index, result := range batch {
		if result.Err != nil ||
			result.Topic != topic ||
			result.Partition != 0 ||
			result.Offset != int64(index) ||
			result.Timestamp.IsZero() {
			t.Fatalf("producer-mode batch result %d = %#v", index, result)
		}
	}

	async, err := producer.PublishAsync(ctx, kafka.ProducerRecord{
		Topic:     topic,
		Partition: kafka.ExplicitPartition(0),
		Key:       []byte("producer-modes"),
		Value:     []byte("async"),
	})
	if err != nil {
		t.Fatalf("admit asynchronous producer-mode record: %v", err)
	}
	asyncResult := awaitIntegrationDelivery(t, ctx, async)
	if asyncResult.Err != nil ||
		asyncResult.Topic != topic ||
		asyncResult.Partition != 0 ||
		asyncResult.Offset != 2 ||
		asyncResult.Timestamp.IsZero() {
		t.Fatalf("asynchronous producer-mode result = %#v", asyncResult)
	}

	shutdownProducer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       brokers,
		ClientID:      "golib-compatibility-shutdown-producer",
		AllowedTopics: []string{topic},
		Security:      kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct shutdown producer: %v", err)
	}
	shutdownDelivery, err := shutdownProducer.PublishAsync(
		ctx,
		kafka.ProducerRecord{
			Topic:     topic,
			Partition: kafka.ExplicitPartition(0),
			Key:       []byte("producer-modes"),
			Value:     []byte("shutdown-drained"),
		},
	)
	if err != nil {
		_ = shutdownProducer.Close()
		t.Fatalf("admit shutdown producer record: %v", err)
	}
	if err := shutdownProducer.Shutdown(ctx); err != nil {
		_ = shutdownProducer.Close()
		t.Fatalf("shutdown producer with admitted record: %v", err)
	}
	shutdownResult := awaitIntegrationDelivery(t, ctx, shutdownDelivery)
	if shutdownResult.Err != nil ||
		shutdownResult.Topic != topic ||
		shutdownResult.Partition != 0 ||
		shutdownResult.Offset != 3 ||
		shutdownResult.Timestamp.IsZero() {
		t.Fatalf("shutdown producer-mode result = %#v", shutdownResult)
	}
	if result := shutdownProducer.PublishRecord(ctx, kafka.ProducerRecord{
		Topic: topic,
		Key:   []byte("producer-modes"),
		Value: []byte("must-not-publish"),
	}); !errors.Is(result.Err, kafka.ErrProducerClosed) {
		t.Fatalf("post-shutdown producer result = %#v", result)
	}

	if values := consumeTransactionValues(
		t,
		brokers,
		topic,
		kgo.ReadUncommitted(),
		4,
	); !slices.Equal(values, []string{
		"batch-first",
		"batch-second",
		"async",
		"shutdown-drained",
	}) {
		t.Fatalf("producer-mode broker values = %q", values)
	}
}

func awaitIntegrationDelivery(
	t *testing.T,
	ctx context.Context,
	delivery <-chan kafka.DeliveryResult,
) kafka.DeliveryResult {
	t.Helper()

	select {
	case result, ok := <-delivery:
		if !ok {
			t.Fatal("delivery channel closed without a result")
		}

		return result
	case <-ctx.Done():
		t.Fatalf("wait for delivery: %v", ctx.Err())

		return kafka.DeliveryResult{}
	}
}

func proveProducerThrottle(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
) {
	t.Helper()

	const clientID = "golib-compatibility-throttled-producer"
	adminClient, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("golib-compatibility-quota-admin"),
		kgo.DialTimeout(10*time.Second),
	)
	if err != nil {
		t.Fatalf("construct quota admin client: %v", err)
	}
	defer adminClient.Close()
	admin := kadm.NewClient(adminClient)
	entity := kadm.ClientQuotaEntity{{
		Type: "client-id",
		Name: kadm.StringPtr(clientID),
	}}
	alterClientQuota(
		t,
		ctx,
		admin,
		entity,
		kadm.AlterClientQuotaOp{
			Key:   "producer_byte_rate",
			Value: 1024,
		},
	)
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cancel()
		alterClientQuota(
			t,
			cleanupCtx,
			admin,
			entity,
			kadm.AlterClientQuotaOp{
				Key:    "producer_byte_rate",
				Remove: true,
			},
		)
	}()

	var throttleDuration atomic.Int64
	var throttledAfterResponse atomic.Bool
	var throttleBrokerKnown atomic.Bool
	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:                brokers,
		ClientID:               clientID,
		AllowedTopics:          []string{topic},
		CompressionPreferences: []kafka.CompressionCodec{kafka.CompressionNone},
		Security:               kafka.DevelopmentPlaintextSecurity(),
		Observers: kafka.ObserverPolicy{
			Observers: []kafka.ObserverFunc{
				func(
					_ context.Context,
					observation kafka.Observation,
				) error {
					if observation.Kind != kafka.ObservationBrokerThrottle {
						return nil
					}
					throttledAfterResponse.Store(
						observation.ThrottledAfterResponse,
					)
					throttleBrokerKnown.Store(observation.BrokerKnown)
					throttleDuration.Store(
						int64(observation.ThrottleDuration),
					)

					return nil
				},
			},
			FailureHandler: func(context.Context, kafka.ObservationFailure) {},
		},
	})
	if err != nil {
		t.Fatalf("construct throttled producer: %v", err)
	}
	defer func() {
		if err := producer.Close(); err != nil {
			t.Errorf("close throttled producer: %v", err)
		}
	}()

	result := producer.PublishRecord(ctx, kafka.ProducerRecord{
		Topic:     topic,
		Partition: kafka.ExplicitPartition(0),
		Key:       []byte("producer-throttle"),
		Value:     make([]byte, 128*1024),
	})
	if result.Err != nil || result.Topic != topic || result.Partition != 0 {
		t.Fatalf("throttled producer result = %#v", result)
	}
	if duration := time.Duration(throttleDuration.Load()); duration <= 0 ||
		!throttledAfterResponse.Load() ||
		!throttleBrokerKnown.Load() {
		t.Fatalf(
			"broker throttle observation = duration:%s after-response:%t broker-known:%t",
			duration,
			throttledAfterResponse.Load(),
			throttleBrokerKnown.Load(),
		)
	}
}

func alterClientQuota(
	t *testing.T,
	ctx context.Context,
	admin *kadm.Client,
	entity kadm.ClientQuotaEntity,
	op kadm.AlterClientQuotaOp,
) {
	t.Helper()

	results, err := admin.AlterClientQuotas(
		ctx,
		[]kadm.AlterClientQuotaEntry{{
			Entity: entity,
			Ops:    []kadm.AlterClientQuotaOp{op},
		}},
	)
	if err != nil {
		t.Fatalf("alter client quota: %v", err)
	}
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("alter client quota results = %#v", results)
	}
}

func proveReplayPolicy(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	producer *kafka.Producer,
	topic string,
) {
	t.Helper()

	replayStart := time.Now().UTC().Truncate(time.Millisecond)
	for index := range 3 {
		result := producer.PublishRecord(ctx, kafka.ProducerRecord{
			Topic:     topic,
			Partition: kafka.ExplicitPartition(0),
			Key:       []byte("replay-key"),
			Value:     []byte(fmt.Sprintf("replay-%d", index)),
			Timestamp: replayStart.Add(time.Duration(index) * time.Second),
		})
		if result.Err != nil || result.Partition != 0 || result.Offset != int64(index) {
			t.Fatalf("publish replay fixture %d: %#v", index, result)
		}
	}

	inspector, err := kafka.NewInspector(kafka.InspectorConfig{
		Brokers:  brokers,
		ClientID: "golib-compatibility-replay-timestamps",
		Security: kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct timestamp replay inspector: %v", err)
	}
	timestampPlan, err := inspector.PlanReplayByTimestamp(
		ctx,
		kafka.ReplayTimestampRequest{
			StartInclusive: replayStart,
			EndExclusive:   replayStart.Add(3 * time.Second),
			Partitions: []kafka.TopicPartition{{
				Topic: topic, Partition: 0,
			}},
		},
	)
	if closeErr := inspector.Close(); closeErr != nil {
		t.Fatalf("close timestamp replay inspector: %v", closeErr)
	}
	if err != nil ||
		timestampPlan.TotalRemaining != 3 ||
		len(timestampPlan.Partitions) != 1 ||
		timestampPlan.Partitions[0].StartOffset != 0 ||
		timestampPlan.Partitions[0].EndOffset != 3 {
		t.Fatalf("timestamp replay plan/error = %#v/%v", timestampPlan, err)
	}

	config := kafka.ReplayConfig{
		Brokers:     brokers,
		ClientID:    "golib-compatibility-replay-initial",
		Ranges:      timestampPlan.ReplayRanges(),
		SideEffects: kafka.ReplaySideEffectsAllowed,
		Security:    kafka.DevelopmentPlaintextSecurity(),
	}
	reader, err := kafka.NewReplayReader(config)
	if err != nil {
		t.Fatalf("construct initial replay reader: %v", err)
	}
	plan, err := reader.PlanAgainstBroker(ctx)
	if err != nil {
		t.Fatalf("plan initial replay against broker: %v", err)
	}
	if plan.TotalRemaining != 3 ||
		len(plan.Ranges) != 1 ||
		plan.Ranges[0].Topic != topic ||
		plan.Ranges[0].Partition != 0 ||
		plan.Ranges[0].NextOffset != 0 ||
		plan.Ranges[0].Remaining != 3 {
		t.Fatalf("broker-validated replay plan = %#v", plan)
	}
	injectedFailure := errors.New("injected replay interruption")
	first, replayErr := reader.Replay(ctx, kafka.ReplayHandlerFunc(func(
		_ context.Context,
		message kafka.ReplayRecord,
	) error {
		if message.Offset == 1 {
			return injectedFailure
		}

		return nil
	}))
	if closeErr := reader.Close(); closeErr != nil {
		t.Fatalf("close initial replay reader: %v", closeErr)
	}
	if !errors.Is(replayErr, injectedFailure) ||
		first.Processed != 1 ||
		first.Failed != 1 ||
		first.IncompleteRanges != 1 ||
		len(first.Ranges) != 1 ||
		first.Ranges[0].NextOffset != 1 {
		t.Fatalf("initial replay result/error = %#v/%v", first, replayErr)
	}

	config.ClientID = "golib-compatibility-replay-resume"
	config.Checkpoint = first.Checkpoint()
	reader, err = kafka.NewReplayReader(config)
	if err != nil {
		t.Fatalf("construct resumed replay reader: %v", err)
	}
	var resumed []int64
	second, replayErr := reader.Replay(ctx, kafka.ReplayHandlerFunc(func(
		_ context.Context,
		message kafka.ReplayRecord,
	) error {
		resumed = append(resumed, message.Offset)

		return nil
	}))
	if closeErr := reader.Close(); closeErr != nil {
		t.Fatalf("close resumed replay reader: %v", closeErr)
	}
	if replayErr != nil ||
		!slices.Equal(resumed, []int64{1, 2}) ||
		second.Processed != 2 ||
		second.CompletedRanges != 1 ||
		second.IncompleteRanges != 0 ||
		len(second.Ranges) != 1 ||
		second.Ranges[0].NextOffset != 3 ||
		!second.Ranges[0].Complete {
		t.Fatalf(
			"resumed replay result/error/offsets = %#v/%v/%v",
			second,
			replayErr,
			resumed,
		)
	}

	canceledConfig := config
	canceledConfig.ClientID = "golib-compatibility-replay-canceled"
	canceledConfig.Checkpoint = kafka.ReplayCheckpoint{}
	canceledConfig.Ranges = []kafka.ReplayRange{{
		Topic: topic, Partition: 0, StartOffset: 0, EndOffset: 1,
	}}
	canceledReader, err := kafka.NewReplayReader(canceledConfig)
	if err != nil {
		t.Fatalf("construct canceled replay reader: %v", err)
	}
	canceledCtx, cancelReplay := context.WithCancel(ctx)
	defer cancelReplay()
	handlerObservedCancellation := false
	canceledResult, replayErr := canceledReader.Replay(
		canceledCtx,
		kafka.ReplayHandlerFunc(func(
			handlerCtx context.Context,
			_ kafka.ReplayRecord,
		) error {
			cancelReplay()
			<-handlerCtx.Done()
			handlerObservedCancellation = errors.Is(
				context.Cause(handlerCtx),
				context.Canceled,
			)

			return nil
		}),
	)
	if closeErr := canceledReader.Close(); closeErr != nil {
		t.Fatalf("close canceled replay reader: %v", closeErr)
	}
	if !errors.Is(replayErr, context.Canceled) ||
		!handlerObservedCancellation ||
		canceledResult.Processed != 0 ||
		canceledResult.Failed != 1 ||
		canceledResult.IncompleteRanges != 1 ||
		len(canceledResult.Ranges) != 1 ||
		canceledResult.Ranges[0].NextOffset != 0 {
		t.Fatalf(
			"canceled replay result/error/handler = %#v/%v/%t",
			canceledResult,
			replayErr,
			handlerObservedCancellation,
		)
	}

	for index := range 2 {
		result := producer.PublishRecord(ctx, kafka.ProducerRecord{
			Topic:     topic,
			Partition: kafka.ExplicitPartition(1),
			Key:       []byte("replay-key-partition-1"),
			Value:     []byte(fmt.Sprintf("replay-partition-1-%d", index)),
		})
		if result.Err != nil ||
			result.Partition != 1 ||
			result.Offset != int64(index) {
			t.Fatalf("publish parallel replay fixture %d: %#v", index, result)
		}
	}

	parallel, err := kafka.NewReplayReader(kafka.ReplayConfig{
		Brokers:  brokers,
		ClientID: "golib-compatibility-replay-parallel",
		Ranges: []kafka.ReplayRange{
			{Topic: topic, Partition: 0, StartOffset: 0, EndOffset: 3},
			{Topic: topic, Partition: 1, StartOffset: 0, EndOffset: 2},
		},
		SideEffects:           kafka.ReplaySideEffectsAllowed,
		MaxConcurrentFetches:  2,
		MaxConcurrentHandlers: 2,
		Security:              kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct parallel replay reader: %v", err)
	}
	release := make(chan struct{})
	var firstPartitions atomic.Int32
	var sequenceMu sync.Mutex
	sequences := make(map[int32][]int64)
	parallelResult, replayErr := parallel.Replay(
		ctx,
		kafka.ReplayHandlerFunc(func(
			handlerCtx context.Context,
			message kafka.ReplayRecord,
		) error {
			sequenceMu.Lock()
			sequences[message.Partition] = append(
				sequences[message.Partition],
				message.Offset,
			)
			sequenceMu.Unlock()
			if message.Offset != 0 {
				return nil
			}
			if firstPartitions.Add(1) == 2 {
				close(release)
			}
			select {
			case <-release:
				return nil
			case <-handlerCtx.Done():
				return context.Cause(handlerCtx)
			}
		}),
	)
	if closeErr := parallel.Close(); closeErr != nil {
		t.Fatalf("close parallel replay reader: %v", closeErr)
	}
	if replayErr != nil ||
		parallelResult.Processed != 5 ||
		parallelResult.CompletedRanges != 2 ||
		parallelResult.IncompleteRanges != 0 ||
		!slices.Equal(sequences[0], []int64{0, 1, 2}) ||
		!slices.Equal(sequences[1], []int64{0, 1}) {
		t.Fatalf(
			"parallel replay result/error/sequences = %#v/%v/%v",
			parallelResult,
			replayErr,
			sequences,
		)
	}

	outOfRange, err := kafka.NewReplayReader(kafka.ReplayConfig{
		Brokers:  brokers,
		ClientID: "golib-compatibility-replay-out-of-range",
		Ranges: []kafka.ReplayRange{{
			Topic: topic, Partition: 0, StartOffset: 10, EndOffset: 11,
		}},
		SideEffects: kafka.ReplaySideEffectsAllowed,
		Security:    kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct out-of-range replay reader: %v", err)
	}
	_, replayErr = outOfRange.Replay(ctx, kafka.ReplayHandlerFunc(func(
		context.Context,
		kafka.ReplayRecord,
	) error {
		t.Fatal("out-of-range replay invoked handler")

		return nil
	}))
	if closeErr := outOfRange.Close(); closeErr != nil {
		t.Fatalf("close out-of-range replay reader: %v", closeErr)
	}
	if !errors.Is(replayErr, kafka.ErrReplayOffsetOutOfRange) {
		t.Fatalf("out-of-range replay error = %v", replayErr)
	}

	deleteIntegrationRecords(t, ctx, brokers, topic, 0, 1)
	retentionReader, err := kafka.NewReplayReader(kafka.ReplayConfig{
		Brokers:  brokers,
		ClientID: "golib-compatibility-replay-retention-gap",
		Ranges: []kafka.ReplayRange{{
			Topic: topic, Partition: 0, StartOffset: 0, EndOffset: 1,
		}},
		SideEffects: kafka.ReplaySideEffectsAllowed,
		Security:    kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct retention-gap replay reader: %v", err)
	}
	retentionResult, replayErr := retentionReader.Replay(
		ctx,
		kafka.ReplayHandlerFunc(func(
			context.Context,
			kafka.ReplayRecord,
		) error {
			t.Fatal("retention-gap replay invoked handler")

			return nil
		}),
	)
	if closeErr := retentionReader.Close(); closeErr != nil {
		t.Fatalf("close retention-gap replay reader: %v", closeErr)
	}
	if !errors.Is(replayErr, kafka.ErrReplayOffsetOutOfRange) ||
		retentionResult.Polled != 0 ||
		retentionResult.Processed != 0 ||
		retentionResult.IncompleteRanges != 1 ||
		len(retentionResult.Ranges) != 1 ||
		retentionResult.Ranges[0].NextOffset != 0 {
		t.Fatalf(
			"retention-gap replay result/error = %#v/%v",
			retentionResult,
			replayErr,
		)
	}
}

func proveFailureTopicPolicy(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	retrySourceTopic string,
	retryTopic string,
	deadLetterSourceTopic string,
	deadLetterTopic string,
) {
	t.Helper()

	failureProducer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:  brokers,
		ClientID: "golib-compatibility-failure-policy-producer",
		AllowedTopics: []string{
			retrySourceTopic,
			retryTopic,
			deadLetterSourceTopic,
			deadLetterTopic,
		},
		Security: kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct failure-policy producer: %v", err)
	}
	defer func() {
		if err := failureProducer.Close(); err != nil {
			t.Errorf("close failure-policy producer: %v", err)
		}
	}()
	if err := failureProducer.Health(ctx); err != nil {
		t.Fatalf("check failure-policy producer health: %v", err)
	}

	publishSource := func(topic string, value string) time.Time {
		t.Helper()
		timestamp := time.Now().UTC().Truncate(time.Millisecond)
		result := failureProducer.PublishRecord(ctx, kafka.ProducerRecord{
			Topic:     topic,
			Partition: kafka.ExplicitPartition(0),
			Key:       []byte("aggregate-failure"),
			Value:     []byte(value),
			Headers: []kafka.Header{{
				Key: "correlation-id", Value: []byte("correlation-123"),
			}},
			Timestamp: timestamp,
		})
		if result.Err != nil {
			t.Fatalf("publish failure-policy source %q: %v", value, result.Err)
		}
		if !result.Timestamp.Equal(timestamp) {
			t.Fatalf(
				"failure-policy source timestamp = %s, want %s",
				result.Timestamp,
				timestamp,
			)
		}
		return result.Timestamp
	}
	retrySourceTimestamp := publishSource(retrySourceTopic, "retry-payload")
	deadLetterSourceTimestamp := publishSource(
		deadLetterSourceTopic,
		"dead-letter-payload",
	)

	runFailureHandler := func(
		sourceTopic string,
		groupID string,
		mode kafka.FailureMode,
		target kafka.FailureTarget,
		publisher kafka.FailurePublisher,
		wantErr error,
	) kafka.PollResult {
		t.Helper()

		consumer := newIntegrationConsumer(t, brokers, sourceTopic, groupID)
		defer closeIntegrationConsumer(t, consumer)
		handler, err := kafka.NewFailureHandler(kafka.FailureHandlerConfig{
			Handler: kafka.HandlerFunc(func(
				context.Context,
				kafka.ConsumedMessage,
			) error {
				return errors.New("injected terminal application failure")
			}),
			Mode:      mode,
			Target:    target,
			Publisher: publisher,
		})
		if err != nil {
			t.Fatalf("construct failure handler: %v", err)
		}
		for {
			result, runErr := consumer.RunOnce(ctx, handler)
			if result.Polled == 0 && runErr == nil {
				continue
			}
			if !errors.Is(runErr, wantErr) {
				t.Fatalf(
					"failure-policy result/error = %#v/%v, want %v",
					result,
					runErr,
					wantErr,
				)
			}

			return result
		}
	}

	retryGroup := "golib-compatibility-retry-topic"
	retryResult := runFailureHandler(
		retrySourceTopic,
		retryGroup,
		kafka.FailureModeRetryTopic,
		kafka.FailureTarget{Topic: retryTopic, Version: 2},
		failureProducer,
		nil,
	)
	if retryResult != (kafka.PollResult{Polled: 1, Processed: 1, Committed: 1}) {
		t.Fatalf("retry-topic result = %#v", retryResult)
	}
	assertPartitionCommits(
		t,
		ctx,
		brokers,
		retrySourceTopic,
		retryGroup,
		map[int32]int64{0: 1},
	)
	assertFailureTopicRecord(
		t,
		ctx,
		brokers,
		retryTopic,
		"retry",
		"2",
		retrySourceTopic,
		"retry-payload",
		retrySourceTimestamp,
	)

	deniedProducer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       brokers,
		ClientID:      "golib-compatibility-denied-dead-letter-producer",
		AllowedTopics: []string{deadLetterSourceTopic},
		Security:      kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct denied dead-letter producer: %v", err)
	}
	deadLetterGroup := "golib-compatibility-dead-letter"
	failedResult := runFailureHandler(
		deadLetterSourceTopic,
		deadLetterGroup,
		kafka.FailureModeDeadLetter,
		kafka.FailureTarget{Topic: deadLetterTopic, Version: 3},
		deniedProducer,
		kafka.ErrFailurePublish,
	)
	if err := deniedProducer.Close(); err != nil {
		t.Fatalf("close denied dead-letter producer: %v", err)
	}
	if failedResult != (kafka.PollResult{Polled: 1}) {
		t.Fatalf("failed dead-letter result = %#v", failedResult)
	}

	redeliveredResult := runFailureHandler(
		deadLetterSourceTopic,
		deadLetterGroup,
		kafka.FailureModeDeadLetter,
		kafka.FailureTarget{Topic: deadLetterTopic, Version: 3},
		failureProducer,
		nil,
	)
	if redeliveredResult != (kafka.PollResult{
		Polled: 1, Processed: 1, Committed: 1,
	}) {
		t.Fatalf("redelivered dead-letter result = %#v", redeliveredResult)
	}
	assertPartitionCommits(
		t,
		ctx,
		brokers,
		deadLetterSourceTopic,
		deadLetterGroup,
		map[int32]int64{0: 1},
	)
	assertFailureTopicRecord(
		t,
		ctx,
		brokers,
		deadLetterTopic,
		"dead-letter",
		"3",
		deadLetterSourceTopic,
		"dead-letter-payload",
		deadLetterSourceTimestamp,
	)
}

func assertFailureTopicRecord(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	kind string,
	targetVersion string,
	sourceTopic string,
	value string,
	sourceTimestamp time.Time,
) {
	t.Helper()

	consumer := newIntegrationConsumer(
		t,
		brokers,
		topic,
		"golib-compatibility-failure-target-"+kind,
	)
	defer closeIntegrationConsumer(t, consumer)
	var retained kafka.ConsumedRecord
	for {
		result, err := consumer.RunOnce(ctx, kafka.HandlerFunc(func(
			_ context.Context,
			record kafka.ConsumedMessage,
		) error {
			retained = record.Retain()

			return nil
		}))
		if err != nil {
			t.Fatalf("consume failure target: %v", err)
		}
		if result.Polled == 0 {
			continue
		}
		if result != (kafka.PollResult{Polled: 1, Processed: 1, Committed: 1}) {
			t.Fatalf("failure target result = %#v", result)
		}

		break
	}

	headers := make(map[string]string, len(retained.Headers))
	for _, header := range retained.Headers {
		headers[header.Key] = string(header.Value)
	}
	if string(retained.Key) != "aggregate-failure" ||
		string(retained.Value) != value ||
		headers["correlation-id"] != "correlation-123" ||
		headers["golib.kafka.failure.schema-version"] != "1" ||
		headers["golib.kafka.failure.kind"] != kind ||
		headers["golib.kafka.failure.target-version"] != targetVersion ||
		headers["golib.kafka.failure.source-topic"] != sourceTopic ||
		headers["golib.kafka.failure.source-partition"] != "0" ||
		headers["golib.kafka.failure.source-offset"] != "0" ||
		headers["golib.kafka.failure.source-timestamp"] !=
			sourceTimestamp.UTC().Format(time.RFC3339Nano) ||
		headers["golib.kafka.failure.source-timestamp-type"] != "create-time" ||
		headers["golib.kafka.failure.source-leader-epoch"] == "" ||
		headers["golib.kafka.failure.attempt"] != "1" ||
		headers["golib.kafka.failure.error-category"] != "permanent" {
		t.Fatalf("failure target record/headers = %#v/%#v", retained, headers)
	}
}

func proveConsumeTransformProduce(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	producer *kafka.Producer,
	sourceTopic string,
	outputTopic string,
	sourceTransactionalID string,
) {
	t.Helper()

	sourceTransaction, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:         brokers,
		ClientID:        "golib-compatibility-transaction-source-producer",
		AllowedTopics:   []string{sourceTopic},
		TransactionalID: sourceTransactionalID,
		Security:        kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct transactional source producer: %v", err)
	}
	hiddenSourceErr := errors.New("abort source transaction")
	err = sourceTransaction.RunTransaction(ctx, func(
		transaction kafka.Transaction,
	) error {
		if err := transaction.Publish(ctx, kafka.ProducerRecord{
			Topic: sourceTopic,
			Key:   []byte("hidden"),
			Value: []byte("hidden"),
		}); err != nil {
			return err
		}

		return hiddenSourceErr
	})
	if !errors.Is(err, hiddenSourceErr) {
		var transactionErr *kafka.TransactionError
		var protocolErr *kerr.Error
		errors.As(err, &transactionErr)
		errors.As(err, &protocolErr)
		var operation kafka.TransactionOperation
		var category kafka.ErrorCategory
		var abortable bool
		var outcomeKnown bool
		if transactionErr != nil {
			operation = transactionErr.Operation()
			category = transactionErr.Category()
			abortable = transactionErr.Abortable()
			outcomeKnown = transactionErr.OutcomeKnown()
		}
		var protocolCode int16
		if protocolErr != nil {
			protocolCode = protocolErr.Code
		}
		causeTypes := make([]string, 0, 4)
		for cause := err; cause != nil; cause = errors.Unwrap(cause) {
			causeTypes = append(causeTypes, fmt.Sprintf("%T", cause))
		}
		var networkErr net.Error
		hasNetworkErr := errors.As(err, &networkErr)
		networkTimeout := hasNetworkErr && networkErr.Timeout()
		networkTemporary := hasNetworkErr && networkErr.Temporary()
		t.Fatalf(
			"abort source transaction: %v; operation=%s category=%s "+
				"abortable=%t outcome-known=%t protocol-code=%d "+
				"cause-types=%q deadline=%t canceled=%t client-closed=%t "+
				"network=%t timeout=%t temporary=%t",
			err,
			operation,
			category,
			abortable,
			outcomeKnown,
			protocolCode,
			causeTypes,
			errors.Is(err, context.DeadlineExceeded),
			errors.Is(err, context.Canceled),
			errors.Is(err, kgo.ErrClientClosed),
			hasNetworkErr,
			networkTimeout,
			networkTemporary,
		)
	}
	if err := sourceTransaction.Close(); err != nil {
		t.Fatalf("close transactional source producer: %v", err)
	}

	for _, value := range []string{"first", "second"} {
		if err := producer.Publish(ctx, kafka.ProducerRecord{
			Topic: sourceTopic, Key: []byte(value), Value: []byte(value),
		}); err != nil {
			t.Fatalf("publish transaction source %q: %v", value, err)
		}
	}

	const groupID = "golib-compatibility-transaction-processor"
	var transactionBegins atomic.Int32
	var transactionCommits atomic.Int32
	var transactionAborts atomic.Int32
	var transactionBrokerRequest atomic.Bool
	processor, err := kafka.NewTransactionProcessor(
		kafka.TransactionProcessorConfig{
			Connection: kafka.TransactionConnectionConfig{
				Brokers:  brokers,
				ClientID: "golib-compatibility-transaction-processor",
				Security: kafka.DevelopmentPlaintextSecurity(),
			},
			Group: kafka.TransactionGroupConfig{
				GroupID:        groupID,
				Topics:         []string{sourceTopic},
				ResetOffset:    kafka.OffsetEarliest,
				MaxPollRecords: 10,
			},
			Output: kafka.TransactionOutputConfig{
				AllowedTopics:   []string{outputTopic},
				TransactionalID: "golib-compatibility-transaction-processor",
			},
			Observers: kafka.ObserverPolicy{
				Observers: []kafka.ObserverFunc{
					func(
						_ context.Context,
						observation kafka.Observation,
					) error {
						if observation.ClientID !=
							"golib-compatibility-transaction-processor" ||
							observation.GroupID != groupID {
							return nil
						}
						switch observation.Kind {
						case kafka.ObservationTransactionBegin:
							if observation.Succeeded {
								transactionBegins.Add(1)
							}
						case kafka.ObservationTransactionCommit:
							if observation.Succeeded {
								transactionCommits.Add(1)
							}
						case kafka.ObservationTransactionAbort:
							if observation.Succeeded {
								transactionAborts.Add(1)
							}
						case kafka.ObservationBrokerRequest:
							transactionBrokerRequest.Store(true)
						}

						return nil
					},
				},
				FailureHandler: func(
					context.Context,
					kafka.ObservationFailure,
				) {
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("construct transaction processor: %v", err)
	}
	defer func() {
		if err := processor.Close(); err != nil {
			t.Errorf("transaction processor close: %v", err)
		}
	}()

	transform := func(
		ctx context.Context,
		record kafka.ConsumedRecord,
		transaction kafka.Transaction,
	) error {
		return transaction.Publish(ctx, kafka.ProducerRecord{
			Topic: outputTopic,
			Key:   record.Key,
			Value: append([]byte("derived-"), record.Value...),
		})
	}
	for {
		result, err := processor.RunOnce(
			ctx,
			kafka.TransactionHandlerFunc(transform),
		)
		if err != nil {
			t.Fatalf("commit consume-transform-produce: %v", err)
		}
		if result.Polled == 0 {
			continue
		}
		if result != (kafka.TransactionPollResult{
			Polled: 2, Processed: 2, Published: 2, Committed: true,
		}) {
			t.Fatalf("transaction processor result = %#v", result)
		}

		break
	}
	assertPartitionCommits(
		t,
		ctx,
		brokers,
		sourceTopic,
		groupID,
		map[int32]int64{0: 4},
	)
	if values := consumeTransactionValues(
		t,
		brokers,
		outputTopic,
		kgo.ReadCommitted(),
		2,
	); !slices.Equal(values, []string{"derived-first", "derived-second"}) {
		t.Fatalf("committed transaction outputs = %q", values)
	}

	if err := producer.Publish(ctx, kafka.ProducerRecord{
		Topic: sourceTopic, Key: []byte("third"), Value: []byte("third"),
	}); err != nil {
		t.Fatalf("publish abort source: %v", err)
	}
	transformErr := errors.New("abort transformed source")
	for {
		result, err := processor.RunOnce(
			ctx,
			kafka.TransactionHandlerFunc(func(
				ctx context.Context,
				record kafka.ConsumedRecord,
				transaction kafka.Transaction,
			) error {
				if err := transform(ctx, record, transaction); err != nil {
					return err
				}

				return transformErr
			}),
		)
		if result.Polled == 0 && err == nil {
			continue
		}
		if !errors.Is(err, transformErr) ||
			result != (kafka.TransactionPollResult{
				Polled: 1, Published: 1,
			}) {
			t.Fatalf("aborted transaction result = %#v, error = %v", result, err)
		}

		break
	}
	assertPartitionCommits(
		t,
		ctx,
		brokers,
		sourceTopic,
		groupID,
		map[int32]int64{0: 4},
	)
	if values := consumeTransactionValues(
		t,
		brokers,
		outputTopic,
		kgo.ReadUncommitted(),
		3,
	); !slices.Equal(
		values,
		[]string{"derived-first", "derived-second", "derived-third"},
	) {
		t.Fatalf("uncommitted transaction outputs = %q", values)
	}

	for {
		result, err := processor.RunOnce(
			ctx,
			kafka.TransactionHandlerFunc(transform),
		)
		if err != nil {
			t.Fatalf("retry consume-transform-produce: %v", err)
		}
		if result.Polled == 0 {
			continue
		}
		if result != (kafka.TransactionPollResult{
			Polled: 1, Processed: 1, Published: 1, Committed: true,
		}) {
			t.Fatalf("retried transaction result = %#v", result)
		}

		break
	}
	assertPartitionCommits(
		t,
		ctx,
		brokers,
		sourceTopic,
		groupID,
		map[int32]int64{0: 5},
	)
	if values := consumeTransactionValues(
		t,
		brokers,
		outputTopic,
		kgo.ReadCommitted(),
		3,
	); !slices.Equal(
		values,
		[]string{"derived-first", "derived-second", "derived-third"},
	) {
		t.Fatalf("retried committed transaction outputs = %q", values)
	}
	if transactionBegins.Load() != 3 ||
		transactionCommits.Load() != 2 ||
		transactionAborts.Load() != 1 ||
		!transactionBrokerRequest.Load() {
		t.Fatalf(
			"transaction processor observations = begin:%d commit:%d abort:%d broker:%t",
			transactionBegins.Load(),
			transactionCommits.Load(),
			transactionAborts.Load(),
			transactionBrokerRequest.Load(),
		)
	}
}

func proveProducerTransactionVisibility(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
) {
	t.Helper()

	var transactionBegins atomic.Int32
	var transactionCommits atomic.Int32
	var transactionAborts atomic.Int32
	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:         brokers,
		ClientID:        "golib-compatibility-transaction-producer",
		AllowedTopics:   []string{topic},
		TransactionalID: "golib-compatibility-transaction-producer",
		Security:        kafka.DevelopmentPlaintextSecurity(),
		Observers: kafka.ObserverPolicy{
			Observers: []kafka.ObserverFunc{
				func(_ context.Context, observation kafka.Observation) error {
					if observation.ClientID !=
						"golib-compatibility-transaction-producer" ||
						!observation.Succeeded {
						return nil
					}
					switch observation.Kind {
					case kafka.ObservationTransactionBegin:
						transactionBegins.Add(1)
					case kafka.ObservationTransactionCommit:
						transactionCommits.Add(1)
					case kafka.ObservationTransactionAbort:
						transactionAborts.Add(1)
					}

					return nil
				},
			},
			FailureHandler: func(context.Context, kafka.ObservationFailure) {},
		},
	})
	if err != nil {
		t.Fatalf("construct transactional producer: %v", err)
	}
	defer func() {
		if err := producer.Close(); err != nil {
			t.Errorf("transactional producer close: %v", err)
		}
	}()

	if err := producer.RunTransaction(ctx, func(transaction kafka.Transaction) error {
		return transaction.Publish(ctx, kafka.Message{
			Topic: topic, Key: []byte("committed"), Value: []byte("committed"),
		})
	}); err != nil {
		var brokerErr *kerr.Error
		if errors.As(err, &brokerErr) {
			t.Fatalf(
				"commit transaction: %v (Kafka error code %d)",
				err,
				brokerErr.Code,
			)
		}
		t.Fatalf(
			"commit transaction: %v (non-broker cause %s)",
			err,
			errorFingerprint(err, brokers),
		)
	}
	abortCause := errors.New("abort transaction fixture")
	err = producer.RunTransaction(ctx, func(transaction kafka.Transaction) error {
		if err := transaction.Publish(ctx, kafka.Message{
			Topic: topic, Key: []byte("aborted"), Value: []byte("aborted"),
		}); err != nil {
			return err
		}

		return abortCause
	})
	if !errors.Is(err, abortCause) {
		t.Fatalf("abort transaction: %v", err)
	}

	committed := consumeTransactionValues(
		t,
		brokers,
		topic,
		kgo.ReadCommitted(),
		1,
	)
	if !slices.Equal(committed, []string{"committed"}) {
		t.Fatalf("read-committed values = %q", committed)
	}
	uncommitted := consumeTransactionValues(
		t,
		brokers,
		topic,
		kgo.ReadUncommitted(),
		2,
	)
	if !slices.Equal(uncommitted, []string{"committed", "aborted"}) {
		t.Fatalf("read-uncommitted values = %q", uncommitted)
	}
	if transactionBegins.Load() != 2 ||
		transactionCommits.Load() != 1 ||
		transactionAborts.Load() != 1 {
		t.Fatalf(
			"producer transaction observations = begin:%d commit:%d abort:%d",
			transactionBegins.Load(),
			transactionCommits.Load(),
			transactionAborts.Load(),
		)
	}
}

func errorFingerprint(err error, brokers []string) string {
	type multiUnwrapper interface {
		Unwrap() []error
	}

	var operationErr *net.OpError
	brokerIndex := -1
	operation := ""
	if errors.As(err, &operationErr) && operationErr.Addr != nil {
		operation = operationErr.Op
		for index, broker := range brokers {
			if operationErr.Addr.String() == broker {
				brokerIndex = index

				break
			}
		}
	}
	for err != nil {
		if joined, ok := err.(multiUnwrapper); ok {
			causes := joined.Unwrap()
			if len(causes) == 0 {
				break
			}
			err = causes[0]

			continue
		}
		next := errors.Unwrap(err)
		if next == nil {
			break
		}
		err = next
	}
	if err == nil {
		return "none"
	}
	digest := sha256.Sum256([]byte(err.Error()))

	return fmt.Sprintf(
		"%T sha256:%x operation:%s broker-index:%d",
		err,
		digest[:8],
		operation,
		brokerIndex,
	)
}

func consumeTransactionValues(
	t *testing.T,
	brokers []string,
	topic string,
	isolation kgo.IsolationLevel,
	want int,
) []string {
	t.Helper()

	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("golib-compatibility-transaction-reader"),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{
			topic: {0: kgo.NewOffset().AtStart()},
		}),
		kgo.FetchIsolationLevel(isolation),
		kgo.DialTimeout(10*time.Second),
	)
	if err != nil {
		t.Fatalf("construct transaction reader: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	values := make([]string, 0, want)
	for len(values) < want {
		fetches := client.PollRecords(ctx, want-len(values))
		if err := fetches.Err(); err != nil {
			t.Fatalf("read transaction records: %v", err)
		}
		for _, record := range fetches.Records() {
			values = append(values, string(record.Value))
		}
	}

	return values
}

func proveBatchPolicy(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	producer *kafka.Producer,
	topic string,
	failureTopic string,
) {
	t.Helper()

	for _, record := range []kafka.ProducerRecord{
		{Topic: topic, Partition: kafka.ExplicitPartition(0), Key: []byte("p0"), Value: []byte("p0-1")},
		{Topic: topic, Partition: kafka.ExplicitPartition(0), Key: []byte("p0"), Value: []byte("p0-2")},
		{Topic: topic, Partition: kafka.ExplicitPartition(1), Key: []byte("p1"), Value: []byte("p1-1")},
		{Topic: topic, Partition: kafka.ExplicitPartition(1), Key: []byte("p1"), Value: []byte("p1-2")},
	} {
		if result := producer.PublishRecord(ctx, record); result.Err != nil {
			t.Fatalf("publish batch fixture: %v", result.Err)
		}
	}

	const groupID = "golib-compatibility-batch"
	consumer := newIntegrationConsumerWithHandlerConcurrency(
		t,
		brokers,
		topic,
		groupID,
		2,
	)
	defer closeIntegrationConsumer(t, consumer)
	var batches []kafka.ConsumedBatch
	var handlerMu sync.Mutex
	started := 0
	barrier := make(chan struct{})
	for {
		result, err := consumer.RunBatchOnce(
			ctx,
			kafka.BatchHandlerFunc(func(
				handlerCtx context.Context,
				batch kafka.ConsumedBatch,
			) error {
				handlerMu.Lock()
				started++
				if started == 2 {
					close(barrier)
				}
				handlerMu.Unlock()
				select {
				case <-barrier:
				case <-handlerCtx.Done():
					return context.Cause(handlerCtx)
				}
				handlerMu.Lock()
				batches = append(batches, batch.Retain())
				handlerMu.Unlock()

				return nil
			}),
		)
		if err != nil {
			t.Fatalf("consume partition batches: %v", err)
		}
		if result.Polled == 0 {
			continue
		}
		if result != (kafka.PollResult{Polled: 4, Processed: 4, Committed: 4}) {
			t.Fatalf("batch poll result = %#v", result)
		}

		break
	}
	if len(batches) != 2 || len(batches[0].Records) != 2 ||
		len(batches[1].Records) != 2 ||
		batches[0].Topic != topic || batches[1].Topic != topic ||
		batches[0].Partition == batches[1].Partition {
		t.Fatalf("consumed batches = %#v", batches)
	}
	assertPartitionCommits(t, ctx, brokers, topic, groupID, map[int32]int64{0: 2, 1: 2})

	const failureGroupID = "golib-compatibility-batch-failure"
	failureConsumer := newIntegrationConsumer(
		t,
		brokers,
		topic,
		failureGroupID,
	)
	defer closeIntegrationConsumer(t, failureConsumer)
	failureHandler, err := kafka.NewBatchFailureHandler(
		kafka.BatchFailureHandlerConfig{
			Handler: kafka.BatchHandlerFunc(func(
				context.Context,
				kafka.ConsumedBatch,
			) error {
				return errors.New("injected batch processing failure")
			}),
			Mode: kafka.FailureModeRetryTopic,
			Target: kafka.FailureTarget{
				Topic: failureTopic, Version: 1,
			},
			Publisher: producer,
		},
	)
	if err != nil {
		t.Fatalf("construct batch failure handler: %v", err)
	}
	for {
		result, runErr := failureConsumer.RunBatchOnce(ctx, failureHandler)
		if result.Polled == 0 && runErr == nil {
			continue
		}
		if runErr != nil || result != (kafka.PollResult{
			Polled: 4, Processed: 4, Committed: 4,
		}) {
			t.Fatalf("batch failure result/error = %#v/%v", result, runErr)
		}

		break
	}
	assertPartitionCommits(
		t,
		ctx,
		brokers,
		topic,
		failureGroupID,
		map[int32]int64{0: 2, 1: 2},
	)
	assertBatchFailureTopicRecords(t, ctx, brokers, failureTopic, topic)
}

func assertBatchFailureTopicRecords(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	sourceTopic string,
) {
	t.Helper()

	consumer := newIntegrationConsumer(
		t,
		brokers,
		topic,
		"golib-compatibility-batch-failure-target",
	)
	defer closeIntegrationConsumer(t, consumer)
	records := make([]kafka.ConsumedRecord, 0, 4)
	for len(records) < 4 {
		result, err := consumer.RunOnce(ctx, kafka.HandlerFunc(func(
			_ context.Context,
			record kafka.ConsumedMessage,
		) error {
			records = append(records, record.Retain())

			return nil
		}))
		if err != nil {
			t.Fatalf("consume batch failure target: %v", err)
		}
		if result.Polled == 0 {
			continue
		}
	}

	want := map[string]string{
		"0/0": "0/2/p0-1",
		"0/1": "1/2/p0-2",
		"1/0": "0/2/p1-1",
		"1/1": "1/2/p1-2",
	}
	for _, record := range records {
		headers := make(map[string]string, len(record.Headers))
		for _, header := range record.Headers {
			headers[header.Key] = string(header.Value)
		}
		coordinate := headers["golib.kafka.failure.source-partition"] + "/" +
			headers["golib.kafka.failure.source-offset"]
		got := headers["golib.kafka.failure.batch-index"] + "/" +
			headers["golib.kafka.failure.batch-count"] + "/" +
			string(record.Value)
		if headers["golib.kafka.failure.source-topic"] != sourceTopic ||
			headers["golib.kafka.failure.kind"] != "retry" ||
			headers["golib.kafka.failure.target-version"] != "1" ||
			want[coordinate] != got {
			t.Fatalf("batch failure target record = %#v/%#v", record, headers)
		}
		delete(want, coordinate)
	}
	if len(want) != 0 {
		t.Fatalf("missing batch failure source coordinates = %#v", want)
	}
}

func proveBlockedRebalancePolicy(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	producer *kafka.Producer,
	topic string,
) {
	t.Helper()

	if result := producer.PublishRecord(ctx, kafka.ProducerRecord{
		Topic: topic, Key: []byte("aggregate-1"), Value: []byte("redeliver"),
		Headers: []kafka.Header{{Key: "event-index", Value: []byte("0")}},
	}); result.Err != nil {
		t.Fatalf("publish rebalance fixture: %v", result.Err)
	}

	const groupID = "golib-compatibility-blocked-rebalance"
	first := newIntegrationConsumer(t, brokers, topic, groupID)
	t.Cleanup(func() {
		closeIntegrationConsumer(t, first)
	})
	firstStarted := make(chan struct{})
	firstDone := make(chan struct {
		result kafka.PollResult
		err    error
	}, 1)
	go func() {
		result, err := first.RunOnce(ctx, kafka.HandlerFunc(func(
			handlerCtx context.Context,
			_ kafka.ConsumedMessage,
		) error {
			close(firstStarted)
			<-handlerCtx.Done()

			return context.Cause(handlerCtx)
		}))
		firstDone <- struct {
			result kafka.PollResult
			err    error
		}{result: result, err: err}
	}()
	select {
	case <-firstStarted:
	case <-ctx.Done():
		t.Fatalf("wait for blocked rebalance handler: %v", ctx.Err())
	}

	second := newIntegrationConsumer(t, brokers, topic, groupID)
	t.Cleanup(func() {
		closeIntegrationConsumer(t, second)
	})
	secondCtx, cancelSecond := context.WithCancel(ctx)
	secondDone := make(chan error, 1)
	probeErr := errors.New("second member must not settle the probe")
	go func() {
		_, err := second.RunOnce(secondCtx, kafka.HandlerFunc(func(
			context.Context,
			kafka.ConsumedMessage,
		) error {
			return probeErr
		}))
		secondDone <- err
	}()

	var firstResult struct {
		result kafka.PollResult
		err    error
	}
	select {
	case firstResult = <-firstDone:
	case <-ctx.Done():
		cancelSecond()
		t.Fatalf("wait for blocked rebalance cancellation: %v", ctx.Err())
	}
	if !errors.Is(firstResult.err, kafka.ErrConsumerRebalance) ||
		firstResult.result != (kafka.PollResult{Polled: 1}) {
		cancelSecond()
		t.Fatalf(
			"blocked rebalance result/error = %#v/%v",
			firstResult.result,
			firstResult.err,
		)
	}

	cancelSecond()
	secondErr := <-secondDone
	if secondErr != nil &&
		!errors.Is(secondErr, context.Canceled) &&
		!errors.Is(secondErr, probeErr) {
		t.Fatalf("second rebalance member error = %v", secondErr)
	}
	closeIntegrationConsumer(t, first)
	closeIntegrationConsumer(t, second)

	redelivered := consumeValues(
		t,
		ctx,
		brokers,
		topic,
		groupID,
		1,
	)
	if !slices.Equal(redelivered, []string{"redeliver"}) {
		t.Fatalf("rebalance redelivery values = %q", redelivered)
	}
}

func provePauseResumePolicy(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	producer *kafka.Producer,
	topic string,
) {
	t.Helper()

	publish := func(value string) {
		t.Helper()
		result := producer.PublishRecord(ctx, kafka.ProducerRecord{
			Topic: topic, Partition: kafka.ExplicitPartition(0),
			Key: []byte(value), Value: []byte(value),
		})
		if result.Err != nil {
			t.Fatalf("publish pause fixture %q: %v", value, result.Err)
		}
	}

	consumer := newIntegrationConsumer(
		t,
		brokers,
		topic,
		"golib-compatibility-pause-resume",
	)
	t.Cleanup(func() {
		if err := consumer.Close(); err != nil {
			t.Errorf("close pause consumer: %v", err)
		}
	})
	publish("prime")
	for {
		primeResult, runErr := consumer.RunOnce(ctx, kafka.HandlerFunc(func(
			_ context.Context,
			message kafka.ConsumedMessage,
		) error {
			if string(message.Value) != "prime" {
				return fmt.Errorf("unexpected prime value %q", message.Value)
			}

			return nil
		}))
		if runErr != nil {
			t.Fatalf("establish pause consumer assignment: %v", runErr)
		}
		if primeResult.Polled == 0 {
			continue
		}
		if primeResult != (kafka.PollResult{Polled: 1, Processed: 1, Committed: 1}) {
			t.Fatalf("prime poll result = %#v", primeResult)
		}

		break
	}

	partition := kafka.TopicPartition{Topic: topic, Partition: 0}
	if err := consumer.PausePartitions(partition); err != nil {
		t.Fatalf("pause partition: %v", err)
	}
	publish("paused")

	pauseCtx, cancelPause := context.WithTimeout(ctx, time.Second)
	defer cancelPause()
	for pauseCtx.Err() == nil {
		pausedResult, runErr := consumer.RunOnce(pauseCtx, kafka.HandlerFunc(func(
			context.Context,
			kafka.ConsumedMessage,
		) error {
			t.Fatal("paused partition delivered a record")

			return nil
		}))
		if pausedResult != (kafka.PollResult{}) ||
			(runErr != nil && !errors.Is(runErr, context.DeadlineExceeded)) {
			t.Fatalf("paused poll result/error = %#v/%v", pausedResult, runErr)
		}
	}
	if !errors.Is(pauseCtx.Err(), context.DeadlineExceeded) {
		t.Fatalf("paused poll context error = %v", pauseCtx.Err())
	}
	if err := consumer.ResumePartitions(partition); err != nil {
		t.Fatalf("resume partition: %v", err)
	}

	for {
		resumedResult, runErr := consumer.RunOnce(ctx, kafka.HandlerFunc(func(
			_ context.Context,
			message kafka.ConsumedMessage,
		) error {
			if string(message.Value) != "paused" {
				return fmt.Errorf("unexpected resumed value %q", message.Value)
			}

			return nil
		}))
		if runErr != nil {
			t.Fatalf("consume resumed partition: %v", runErr)
		}
		if resumedResult.Polled == 0 {
			continue
		}
		if resumedResult != (kafka.PollResult{Polled: 1, Processed: 1, Committed: 1}) {
			t.Fatalf("resumed poll result = %#v", resumedResult)
		}

		break
	}
}

func proveMembershipPolicy(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	producer *kafka.Producer,
	topic string,
) {
	t.Helper()

	publish := func(partition int32, value string) {
		t.Helper()
		result := producer.PublishRecord(ctx, kafka.ProducerRecord{
			Topic: topic, Partition: kafka.ExplicitPartition(partition),
			Key: []byte(value), Value: []byte(value),
		})
		if result.Err != nil {
			t.Fatalf("publish membership fixture: %v", result.Err)
		}
	}
	publish(0, "first")
	publish(1, "second")

	const groupID = "golib-compatibility-static-member"
	first := consumeMembershipValues(t, ctx, brokers, topic, groupID, 2)
	slices.Sort(first)
	if !slices.Equal(first, []string{"first", "second"}) {
		t.Fatalf("first static membership values = %q", first)
	}

	publish(0, "third")
	second := consumeMembershipValues(t, ctx, brokers, topic, groupID, 1)
	if !slices.Equal(second, []string{"third"}) {
		t.Fatalf("restarted static membership values = %q", second)
	}
}

func consumeMembershipValues(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	groupID string,
	count int,
) []string {
	t.Helper()

	var assignmentObserved atomic.Bool
	consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:           brokers,
		ClientID:          groupID,
		GroupID:           groupID,
		InstanceID:        "static-member-01",
		Rack:              "integration-rack",
		Topics:            []string{topic},
		ResetOffset:       kafka.OffsetEarliest,
		BalancePolicy:     kafka.BalanceEagerSticky,
		MaxPollRecords:    10,
		SessionTimeout:    10 * time.Second,
		RebalanceTimeout:  10 * time.Second,
		HeartbeatInterval: time.Second,
		HandlerTimeout:    3 * time.Second,
		CommitTimeout:     2 * time.Second,
		DialTimeout:       10 * time.Second,
		Security:          kafka.DevelopmentPlaintextSecurity(),
		Observers: kafka.ObserverPolicy{
			Observers: []kafka.ObserverFunc{
				func(_ context.Context, observation kafka.Observation) error {
					if observation.Kind == kafka.ObservationConsumeAssigned &&
						observation.ClientID == groupID &&
						observation.GroupID == groupID &&
						observation.PartitionCount > 0 &&
						observation.Succeeded {
						assignmentObserved.Store(true)
					}

					return nil
				},
			},
			FailureHandler: func(context.Context, kafka.ObservationFailure) {},
		},
	})
	if err != nil {
		t.Fatalf("construct membership consumer: %v", err)
	}
	defer closeIntegrationConsumer(t, consumer)

	values := make([]string, 0, count)
	for len(values) < count {
		result, runErr := consumer.RunOnce(ctx, kafka.HandlerFunc(func(
			_ context.Context,
			message kafka.ConsumedMessage,
		) error {
			values = append(values, string(message.Value))

			return nil
		}))
		if runErr != nil {
			t.Fatalf("consume membership fixture: %v", runErr)
		}
		if result.Processed != result.Committed {
			t.Fatalf("membership consume result = %#v", result)
		}
		if result.Polled == 0 && ctx.Err() != nil {
			t.Fatalf("consume membership fixture: %v", ctx.Err())
		}
	}
	assignment, assignmentErr := consumer.Assignment()
	if assignmentErr != nil {
		t.Fatalf("snapshot active static assignment: %v", assignmentErr)
	}
	assertActiveGroupAssignment(
		t,
		ctx,
		brokers,
		groupID,
		groupID,
		"static-member-01",
		assignment.Partitions,
	)
	if !assignmentObserved.Load() {
		t.Fatal("consumer assignment was not observed")
	}

	return values
}

func proveStaticMemberFencing(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	producer *kafka.Producer,
	topic string,
) {
	t.Helper()

	const (
		groupID    = "golib-compatibility-static-fencing"
		instanceID = "static-fencing-member-01"
	)
	newStaticConsumer := func(clientID string) *kafka.Consumer {
		consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
			Brokers:           brokers,
			ClientID:          clientID,
			GroupID:           groupID,
			InstanceID:        instanceID,
			Topics:            []string{topic},
			ResetOffset:       kafka.OffsetEarliest,
			BalancePolicy:     kafka.BalanceEagerSticky,
			MaxPollRecords:    1,
			SessionTimeout:    10 * time.Second,
			RebalanceTimeout:  10 * time.Second,
			HeartbeatInterval: 200 * time.Millisecond,
			HandlerTimeout:    3 * time.Second,
			CommitTimeout:     2 * time.Second,
			DialTimeout:       10 * time.Second,
			Security:          kafka.DevelopmentPlaintextSecurity(),
		})
		if err != nil {
			t.Fatalf("construct %s: %v", clientID, err)
		}
		t.Cleanup(func() {
			if err := consumer.Close(); err != nil {
				t.Errorf("close %s: %v", clientID, err)
			}
		})

		return consumer
	}
	publish := func(value string) {
		t.Helper()
		result := producer.PublishRecord(ctx, kafka.ProducerRecord{
			Topic: topic,
			Key:   []byte("static-fencing"),
			Value: []byte(value),
		})
		if result.Err != nil {
			t.Fatalf("publish static-fencing record: %v", result.Err)
		}
	}
	runOne := func(
		consumer *kafka.Consumer,
		want string,
	) kafka.PollResult {
		t.Helper()
		for {
			result, err := consumer.RunOnce(ctx, kafka.HandlerFunc(func(
				_ context.Context,
				record kafka.ConsumedMessage,
			) error {
				if string(record.Value) != want {
					t.Fatalf(
						"static-fencing value = %q, want %q",
						record.Value,
						want,
					)
				}

				return nil
			}))
			if err != nil {
				t.Fatalf("consume static-fencing record: %v", err)
			}
			if result.Polled == 0 {
				continue
			}

			return result
		}
	}

	original := newStaticConsumer("golib-static-fencing-original")
	publish("original")
	if result := runOne(original, "original"); result != (kafka.PollResult{
		Polled: 1, Processed: 1, Committed: 1,
	}) {
		t.Fatalf("original static-fencing result = %#v", result)
	}

	replacement := newStaticConsumer("golib-static-fencing-replacement")
	publish("replacement")
	if result := runOne(replacement, "replacement"); result != (kafka.PollResult{
		Polled: 1, Processed: 1, Committed: 1,
	}) {
		t.Fatalf("replacement static-fencing result = %#v", result)
	}

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	var assignment kafka.ConsumerAssignment
	for !assignment.Lost {
		var err error
		assignment, err = original.Assignment()
		if err != nil {
			t.Fatalf("inspect static-fencing assignment: %v", err)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for static fencing: %v", ctx.Err())
		case <-ticker.C:
		}
	}
	_, originalErr := original.RunOnce(
		ctx,
		kafka.HandlerFunc(func(
			context.Context,
			kafka.ConsumedMessage,
		) error {
			t.Fatal("fenced static consumer invoked handler")

			return nil
		}),
	)
	if !errors.Is(originalErr, kafka.ErrConsumerFatal) ||
		!errors.Is(originalErr, kafka.ErrConsumerInstanceFenced) ||
		!errors.Is(originalErr, kerr.FencedInstanceID) {
		t.Fatalf("fenced static consumer error = %v", originalErr)
	}
	assignment, assignmentErr := original.Assignment()
	if assignmentErr != nil ||
		!assignment.Lost ||
		len(assignment.Partitions) != 0 {
		t.Fatalf(
			"fenced static assignment/error = %#v/%v",
			assignment,
			assignmentErr,
		)
	}
	if _, err := original.RunOnce(
		ctx,
		kafka.HandlerFunc(func(context.Context, kafka.ConsumedMessage) error {
			t.Fatal("terminal static consumer invoked handler")

			return nil
		}),
	); !errors.Is(err, kafka.ErrConsumerFatal) ||
		!errors.Is(err, kafka.ErrConsumerInstanceFenced) ||
		!errors.Is(err, kerr.FencedInstanceID) {
		t.Fatalf("terminal static consumer error = %v", err)
	}
}

func assertActiveGroupAssignment(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	groupID string,
	clientID string,
	instanceID string,
	assignments []kafka.TopicPartition,
) {
	t.Helper()

	inspector, err := kafka.NewInspector(kafka.InspectorConfig{
		Brokers:  brokers,
		ClientID: "golib-active-group-inspector",
		Security: kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct active group inspector: %v", err)
	}
	defer inspector.Close()

	groups, err := inspector.ConsumerGroupLag(ctx, groupID)
	if err != nil {
		t.Fatalf("inspect active group assignment: %v", err)
	}
	if len(groups) != 1 ||
		groups[0].Group != groupID ||
		groups[0].CoordinatorID < 0 ||
		groups[0].State != "Stable" ||
		groups[0].ProtocolType != "consumer" ||
		len(groups[0].Members) != 1 {
		t.Fatalf("active group state = %#v", groups)
	}
	member := groups[0].Members[0]
	if member.MemberID == "" ||
		!member.InstanceIDVisible ||
		member.InstanceID != instanceID ||
		member.ClientID != clientID ||
		member.ClientHost == "" ||
		!slices.Equal(member.Assignments, assignments) {
		t.Fatalf("active group member = %#v", member)
	}
}

func provePartitionSettlement(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	producer *kafka.Producer,
	topic string,
) {
	t.Helper()

	for _, record := range []kafka.ProducerRecord{
		{Topic: topic, Partition: kafka.ExplicitPartition(0), Key: []byte("p0"), Value: []byte("p0-ok")},
		{Topic: topic, Partition: kafka.ExplicitPartition(0), Key: []byte("p0"), Value: []byte("p0-fail")},
		{Topic: topic, Partition: kafka.ExplicitPartition(0), Key: []byte("p0"), Value: []byte("p0-skipped")},
		{Topic: topic, Partition: kafka.ExplicitPartition(1), Key: []byte("p1"), Value: []byte("p1-first")},
		{Topic: topic, Partition: kafka.ExplicitPartition(1), Key: []byte("p1"), Value: []byte("p1-second")},
	} {
		if result := producer.PublishRecord(ctx, record); result.Err != nil {
			t.Fatalf("publish settlement fixture: %v", result.Err)
		}
	}

	const groupID = "golib-compatibility-partition-settlement"
	consumer := newIntegrationConsumerWithHandlerConcurrency(
		t,
		brokers,
		topic,
		groupID,
		2,
	)
	defer closeIntegrationConsumer(t, consumer)
	var handled []string
	var handlerMu sync.Mutex
	started := 0
	barrier := make(chan struct{})
	for {
		result, err := consumer.RunOnce(ctx, kafka.HandlerFunc(func(
			handlerCtx context.Context,
			message kafka.ConsumedMessage,
		) error {
			value := string(message.Value)
			if value == "p0-ok" || value == "p1-first" {
				handlerMu.Lock()
				started++
				if started == 2 {
					close(barrier)
				}
				handlerMu.Unlock()
				select {
				case <-barrier:
				case <-handlerCtx.Done():
					return context.Cause(handlerCtx)
				}
			}
			handlerMu.Lock()
			handled = append(handled, string(message.Value))
			handlerMu.Unlock()
			if value == "p0-fail" {
				return errors.New("partition zero failed")
			}

			return nil
		}))
		if result.Polled == 0 && err == nil {
			continue
		}
		if err == nil || result != (kafka.PollResult{Polled: 5, Processed: 3, Committed: 3}) {
			t.Fatalf("partition settlement result/error = %#v/%v", result, err)
		}
		break
	}
	slices.Sort(handled)
	if !slices.Equal(handled, []string{"p0-fail", "p0-ok", "p1-first", "p1-second"}) {
		t.Fatalf("partition settlement handled values = %q", handled)
	}
	assertPartitionCommits(t, ctx, brokers, topic, groupID, map[int32]int64{0: 1, 1: 2})
}

func consumeValues(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	groupID string,
	count int,
) []string {
	t.Helper()

	consumer := newIntegrationConsumer(t, brokers, topic, groupID)
	defer closeIntegrationConsumer(t, consumer)

	values := make([]string, 0, count)
	for len(values) < count {
		result, err := consumer.RunOnce(
			ctx,
			kafka.HandlerFunc(func(
				_ context.Context,
				message kafka.ConsumedMessage,
			) error {
				if string(message.Key) != "aggregate-1" {
					return fmt.Errorf("unexpected key %q", message.Key)
				}
				if len(message.Headers) != 1 ||
					message.Headers[0].Key != "event-index" ||
					string(message.Headers[0].Value) !=
						fmt.Sprintf("%d", len(values)) {
					return fmt.Errorf("unexpected headers %#v", message.Headers)
				}
				values = append(values, string(message.Value))

				return nil
			}),
		)
		if err != nil {
			t.Fatalf("consume messages: %v", err)
		}
		if result.Processed != result.Polled ||
			result.Committed != result.Processed {
			t.Fatalf("consume result = %#v", result)
		}
		if result.Polled == 0 && ctx.Err() != nil {
			t.Fatalf("consume messages: %v", ctx.Err())
		}
	}

	return values
}

func newIntegrationConsumer(
	t *testing.T,
	brokers []string,
	topic string,
	groupID string,
) *kafka.Consumer {
	t.Helper()

	return newIntegrationConsumerWithHandlerConcurrency(
		t,
		brokers,
		topic,
		groupID,
		1,
	)
}

func newIntegrationConsumerWithHandlerConcurrency(
	t *testing.T,
	brokers []string,
	topic string,
	groupID string,
	maxConcurrentHandlers int,
) *kafka.Consumer {
	t.Helper()

	consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:               brokers,
		ClientID:              groupID,
		GroupID:               groupID,
		Topics:                []string{topic},
		ResetOffset:           kafka.OffsetEarliest,
		MaxPollRecords:        10,
		MaxConcurrentHandlers: maxConcurrentHandlers,
		FetchMaxWait:          100 * time.Millisecond,
		SessionTimeout:        10 * time.Second,
		RebalanceTimeout:      10 * time.Second,
		HeartbeatInterval:     time.Second,
		HandlerTimeout:        3 * time.Second,
		CommitTimeout:         2 * time.Second,
		DialTimeout:           10 * time.Second,
		Security:              kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct consumer: %v", err)
	}

	return consumer
}

func closeIntegrationConsumer(t *testing.T, consumer *kafka.Consumer) {
	t.Helper()
	if err := consumer.Close(); err != nil {
		t.Errorf("consumer close: %v", err)
	}
}

func assertGroupCommitted(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	groupID string,
) {
	t.Helper()

	inspector, err := kafka.NewInspector(kafka.InspectorConfig{
		Brokers:  brokers,
		ClientID: "golib-compatibility-inspector",
		Security: kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct inspector: %v", err)
	}
	defer inspector.Close()
	groups, err := inspector.ConsumerGroupLag(ctx, groupID)
	if err != nil {
		t.Fatalf("inspect committed group offset: %v", err)
	}
	if len(groups) != 1 || len(groups[0].Partitions) != 1 {
		t.Fatalf("committed group state = %#v", groups)
	}
	partition := groups[0].Partitions[0]
	if groups[0].Group != groupID ||
		partition.Topic != topic ||
		partition.CommittedOffset != 3 ||
		partition.EndOffset != 3 ||
		partition.Lag != 0 {
		t.Fatalf("committed group state = %#v", groups[0])
	}
}

func assertInspectionState(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
) {
	t.Helper()

	inspector, err := kafka.NewInspector(kafka.InspectorConfig{
		Brokers:  brokers,
		ClientID: "golib-cluster-inspector",
		Security: kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct cluster inspector: %v", err)
	}
	defer inspector.Close()

	cluster, err := inspector.Cluster(ctx)
	if err != nil {
		t.Fatalf("inspect cluster: %v", err)
	}
	if !cluster.IDVisible ||
		cluster.ID == "" ||
		!cluster.ControllerVisible ||
		len(cluster.Brokers) != 1 ||
		cluster.Brokers[0].NodeID != cluster.ControllerID {
		t.Fatalf("cluster inspection state = %#v", cluster)
	}

	topics, err := inspector.Topics(ctx, topic)
	if err != nil {
		t.Fatalf("inspect topic: %v", err)
	}
	if len(topics) != 1 ||
		topics[0].Name != topic ||
		topics[0].MinInSyncReplicas != 1 ||
		topics[0].CleanupPolicy !=
			kafka.TopicCleanupDelete|kafka.TopicCleanupCompact ||
		topics[0].RetentionMilliseconds != 86_400_000 ||
		topics[0].RetentionBytesPerPartition != 10_485_760 ||
		topics[0].DeleteRetentionMilliseconds != 43_200_000 ||
		topics[0].MinimumCompactionLagMilliseconds != 60_000 ||
		topics[0].MaximumCompactionLagMilliseconds != 3_600_000 ||
		topics[0].MinimumCleanableDirtyRatio != 0.75 ||
		topics[0].SegmentBytes != 1_048_576 ||
		topics[0].SegmentMilliseconds != 900_000 ||
		!topics[0].UncleanLeaderElectionEnabled ||
		len(topics[0].Partitions) != 4 {
		t.Fatalf("topic inspection state = %#v", topics)
	}
	for index, partition := range topics[0].Partitions {
		if partition.Partition != int32(index) ||
			partition.Leader != cluster.ControllerID ||
			partition.ReplicationFactor != 1 ||
			len(partition.Replicas) != 1 ||
			partition.Replicas[0] != cluster.ControllerID ||
			partition.InSyncReplicas != 1 ||
			len(partition.InSyncReplicaIDs) != 1 ||
			partition.InSyncReplicaIDs[0] != cluster.ControllerID ||
			partition.OfflineReplicas != 0 ||
			len(partition.OfflineReplicaIDs) != 0 ||
			partition.BeginningOffset != 0 ||
			partition.EndOffset != 0 {
			t.Fatalf("topic partition inspection state = %#v", partition)
		}
	}
}

func assertPartitionCommits(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	groupID string,
	want map[int32]int64,
) {
	t.Helper()

	inspector, err := kafka.NewInspector(kafka.InspectorConfig{
		Brokers:  brokers,
		ClientID: "golib-partition-settlement-inspector",
		Security: kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct partition settlement inspector: %v", err)
	}
	defer inspector.Close()

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastGroups []kafka.ConsumerGroupState
	var lastErr error
	for {
		groups, err := inspector.ConsumerGroupLag(waitCtx, groupID)
		if err == nil && partitionCommitsMatch(groups, topic, want) {
			return
		}
		lastGroups = groups
		lastErr = err

		select {
		case <-waitCtx.Done():
			t.Fatalf(
				"wait for partition settlement offsets: %v; state = %#v; "+
					"last error = %v",
				context.Cause(waitCtx),
				lastGroups,
				lastErr,
			)
		case <-ticker.C:
		}
	}
}

func partitionCommitsMatch(
	groups []kafka.ConsumerGroupState,
	topic string,
	want map[int32]int64,
) bool {
	if len(groups) != 1 || len(groups[0].Partitions) != len(want) {
		return false
	}
	for _, partition := range groups[0].Partitions {
		offset, exists := want[partition.Partition]
		if !exists ||
			partition.Topic != topic ||
			partition.CommittedOffset != offset {
			return false
		}
	}

	return true
}

func createIntegrationTopic(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	partitions int32,
) {
	t.Helper()

	createIntegrationTopicWithConfigs(
		t,
		ctx,
		brokers,
		topic,
		partitions,
		nil,
	)
}

func createIntegrationTopicWithConfigs(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	partitions int32,
	configs map[string]*string,
) {
	t.Helper()

	createIntegrationTopicWithReplication(
		t,
		ctx,
		brokers,
		topic,
		partitions,
		1,
		configs,
	)
}

func createIntegrationTopicWithReplication(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	partitions int32,
	replicationFactor int16,
	configs map[string]*string,
) {
	t.Helper()

	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		t.Fatalf("construct Kafka administrator: %v", err)
	}
	defer client.Close()

	responses, err := kadm.NewClient(client).CreateTopics(
		ctx,
		partitions,
		replicationFactor,
		configs,
		topic,
	)
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	response, exists := responses[topic]
	if !exists {
		t.Fatalf("create topic response omitted %q", topic)
	}
	if response.Err != nil {
		t.Fatalf("create topic %q: %v", topic, response.Err)
	}
}

func deleteIntegrationRecords(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	partition int32,
	beforeOffset int64,
) {
	t.Helper()

	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		t.Fatalf("construct Kafka deletion administrator: %v", err)
	}
	defer client.Close()

	var offsets kadm.Offsets
	offsets.AddOffset(topic, partition, beforeOffset, -1)
	responses, err := kadm.NewClient(client).DeleteRecords(ctx, offsets)
	if err != nil {
		t.Fatalf("delete Kafka records: %v", err)
	}
	response, exists := responses.Lookup(topic, partition)
	if !exists ||
		response.Err != nil ||
		response.LowWatermark != beforeOffset {
		t.Fatalf("delete Kafka records response = %#v/%t", response, exists)
	}
}
