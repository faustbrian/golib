//go:build integration

package kafka_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
	"github.com/twmb/franz-go/pkg/kadm"
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
	pauseTopic := topic + "-pause"
	rebalanceTopic := topic + "-rebalance"
	batchTopic := topic + "-batch"
	transactionTopic := topic + "-transaction"
	transactionSourceTopic := topic + "-transaction-source"
	transactionOutputTopic := topic + "-transaction-output"
	retrySourceTopic := topic + "-retry-source"
	retryTopic := topic + "-retry-v2"
	deadLetterSourceTopic := topic + "-dead-letter-source"
	deadLetterTopic := topic + "-dead-letter-v3"
	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:  brokers,
		ClientID: "golib-compatibility-producer",
		AllowedTopics: []string{
			topic, explicitTopic, settlementTopic, membershipTopic, pauseTopic,
			rebalanceTopic, batchTopic,
			transactionSourceTopic,
			retrySourceTopic, retryTopic, deadLetterSourceTopic, deadLetterTopic,
		},
		CompressionPreferences: []kafka.CompressionCodec{kafka.CompressionZstd},
		Security:               kafka.DevelopmentPlaintextSecurity(),
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
	createIntegrationTopic(t, ctx, brokers, explicitTopic, 4)
	createIntegrationTopic(t, ctx, brokers, settlementTopic, 2)
	createIntegrationTopic(t, ctx, brokers, membershipTopic, 2)
	createIntegrationTopic(t, ctx, brokers, pauseTopic, 1)
	createIntegrationTopic(t, ctx, brokers, rebalanceTopic, 1)
	createIntegrationTopic(t, ctx, brokers, batchTopic, 2)
	createIntegrationTopic(t, ctx, brokers, transactionTopic, 1)
	createIntegrationTopic(t, ctx, brokers, transactionSourceTopic, 1)
	createIntegrationTopic(t, ctx, brokers, transactionOutputTopic, 1)
	createIntegrationTopic(t, ctx, brokers, retrySourceTopic, 1)
	createIntegrationTopic(t, ctx, brokers, retryTopic, 1)
	createIntegrationTopic(t, ctx, brokers, deadLetterSourceTopic, 1)
	createIntegrationTopic(t, ctx, brokers, deadLetterTopic, 1)
	if err := producer.Health(ctx); err != nil {
		t.Fatalf("check Kafka health: %v", err)
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
	provePauseResumePolicy(t, ctx, brokers, producer, pauseTopic)
	proveBlockedRebalancePolicy(t, ctx, brokers, producer, rebalanceTopic)
	proveBatchPolicy(t, ctx, brokers, producer, batchTopic)
	proveProducerTransactionVisibility(t, ctx, brokers, transactionTopic)
	proveConsumeTransformProduce(
		t,
		ctx,
		brokers,
		producer,
		transactionSourceTopic,
		transactionOutputTopic,
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
) {
	t.Helper()

	sourceTransaction, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:         brokers,
		ClientID:        "golib-compatibility-transaction-source-producer",
		AllowedTopics:   []string{sourceTopic},
		TransactionalID: "golib-compatibility-transaction-source-producer",
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
		t.Fatalf("abort source transaction: %v", err)
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
}

func proveProducerTransactionVisibility(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
) {
	t.Helper()

	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:         brokers,
		ClientID:        "golib-compatibility-transaction-producer",
		AllowedTopics:   []string{topic},
		TransactionalID: "golib-compatibility-transaction-producer",
		Security:        kafka.DevelopmentPlaintextSecurity(),
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
		t.Fatalf("commit transaction: %v", err)
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

	return values
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
	groups, err := inspector.ConsumerGroupLag(ctx, groupID)
	if err != nil {
		t.Fatalf("inspect partition settlement offsets: %v", err)
	}
	if len(groups) != 1 || len(groups[0].Partitions) != len(want) {
		t.Fatalf("partition settlement group state = %#v", groups)
	}
	for _, partition := range groups[0].Partitions {
		if partition.Topic != topic ||
			partition.CommittedOffset != want[partition.Partition] {
			t.Fatalf("partition settlement group state = %#v", groups[0])
		}
		delete(want, partition.Partition)
	}
	if len(want) != 0 {
		t.Fatalf("partition settlement missing offsets = %#v", want)
	}
}

func createIntegrationTopic(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	partitions int32,
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
		1,
		nil,
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
