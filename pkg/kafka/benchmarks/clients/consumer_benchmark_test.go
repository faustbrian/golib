//go:build integration

package clients_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/IBM/sarama"
	policy "github.com/faustbrian/golib/pkg/kafka"
	segmentkafka "github.com/segmentio/kafka-go"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	benchmarkConsumerOperationTimeout = 30 * time.Second
	benchmarkConsumerSeedOperations   = 100
)

var (
	errSaramaBenchmarkConsumerStopped = errors.New(
		"Sarama benchmark consumer stopped",
	)
	errSaramaBenchmarkClaimClosed = errors.New(
		"Sarama benchmark claim closed",
	)
)

type benchmarkConsumerMode uint8

const (
	benchmarkConsumeRecord benchmarkConsumerMode = iota
	benchmarkConsumeBatch
)

func (mode benchmarkConsumerMode) String() string {
	if mode == benchmarkConsumeBatch {
		return "batch"
	}

	return "record"
}

type benchmarkConsumedRecord struct {
	topic     string
	partition int32
	offset    int64
	key       []byte
	value     []byte
}

type benchmarkConsumerSink struct {
	record func(benchmarkConsumedRecord) error
	batch  func([]benchmarkConsumedRecord) error
}

type benchmarkConsumer interface {
	Consume(context.Context) (int, error)
	Close(context.Context) error
}

type benchmarkConsumerCandidate struct {
	name string
	new  func(
		testing.TB,
		[]string,
		string,
		string,
		benchmarkConsumerMode,
		int,
		benchmarkConsumerSink,
	) benchmarkConsumer
}

var benchmarkConsumerCandidates = []benchmarkConsumerCandidate{
	{name: "golib-policy", new: newPolicyBenchmarkConsumer},
	{name: "raw-franz-go", new: newFranzBenchmarkConsumer},
	{name: "kafka-go", new: newKafkaGoBenchmarkConsumer},
	{name: "sarama", new: newSaramaBenchmarkConsumer},
}

type benchmarkConsumerTopicKey struct {
	payloadBytes int
	compression  benchmarkCompression
}

type benchmarkConsumerTopicState struct {
	mu      sync.Mutex
	topic   string
	records int
}

var (
	benchmarkConsumerTopicsMu sync.Mutex
	benchmarkConsumerTopics   = make(
		map[benchmarkConsumerTopicKey]*benchmarkConsumerTopicState,
	)
)

func BenchmarkEquivalentConsumerHandling(benchmark *testing.B) {
	brokers := benchmarkBrokers(benchmark)
	for _, mode := range []benchmarkConsumerMode{
		benchmarkConsumeRecord,
		benchmarkConsumeBatch,
	} {
		operationCounts := []int{1}
		if mode == benchmarkConsumeBatch {
			operationCounts = []int{10, 100}
		}
		for _, payloadBytes := range []int{128, 1024} {
			for _, operationRecords := range operationCounts {
				for _, compression := range []benchmarkCompression{
					compressionNone,
					compressionSnappy,
				} {
					name := fmt.Sprintf(
						"%s/%d-records/%dB/%s",
						mode,
						operationRecords,
						payloadBytes,
						compression,
					)
					benchmark.Run(name, func(benchmark *testing.B) {
						benchmarkEquivalentConsumerHandling(
							benchmark,
							brokers,
							mode,
							operationRecords,
							payloadBytes,
							compression,
						)
					})
				}
			}
		}
	}
}

func benchmarkEquivalentConsumerHandling(
	benchmark *testing.B,
	brokers []string,
	mode benchmarkConsumerMode,
	operationRecords int,
	payloadBytes int,
	compression benchmarkCompression,
) {
	benchmark.Helper()
	for _, candidate := range benchmarkConsumerCandidates {
		benchmark.Run(candidate.name, func(benchmark *testing.B) {
			seededRecords := benchmarkConsumerSeedOperations * operationRecords
			topic := ensureBenchmarkConsumerTopic(
				benchmark,
				brokers,
				seededRecords,
				payloadBytes,
				compression,
			)
			var consumedBytes int64
			sink := benchmarkConsumerSink{
				record: func(record benchmarkConsumedRecord) error {
					consumedBytes += int64(len(record.key) + len(record.value))

					return nil
				},
				batch: func(records []benchmarkConsumedRecord) error {
					for _, record := range records {
						consumedBytes += int64(len(record.key) + len(record.value))
					}

					return nil
				},
			}
			consumer := candidate.new(
				benchmark,
				brokers,
				topic,
				benchmarkConsumerGroupID(candidate.name, mode),
				mode,
				operationRecords,
				sink,
			)
			benchmark.Cleanup(func() {
				closeCtx, cancel := context.WithTimeout(
					context.Background(),
					benchmarkConsumerOperationTimeout,
				)
				defer cancel()
				if err := consumer.Close(closeCtx); err != nil {
					benchmark.Errorf(
						"close %s %s consumer: %v",
						candidate.name,
						mode,
						err,
					)
				}
			})

			warmupCtx, warmupCancel := context.WithTimeout(
				context.Background(),
				benchmarkConsumerOperationTimeout,
			)
			consumed, err := consumer.Consume(warmupCtx)
			warmupCancel()
			if err != nil {
				benchmark.Fatalf(
					"warm %s %s consumer: %v",
					candidate.name,
					mode,
					err,
				)
			}
			if consumed != operationRecords {
				benchmark.Fatalf(
					"warm %s %s count = %d, want %d",
					candidate.name,
					mode,
					consumed,
					operationRecords,
				)
			}

			benchmark.ReportAllocs()
			benchmark.SetBytes(benchmarkConsumerOperationBytes(
				operationRecords,
				payloadBytes,
			))
			benchmark.ResetTimer()
			completedOperations := 1
			for benchmark.Loop() {
				requiredRecords := (completedOperations + 1) * operationRecords
				if requiredRecords > seededRecords {
					benchmark.StopTimer()
					seededRecords += benchmarkConsumerSeedOperations *
						operationRecords
					ensureBenchmarkConsumerTopic(
						benchmark,
						brokers,
						seededRecords,
						payloadBytes,
						compression,
					)
					benchmark.StartTimer()
				}
				ctx, cancel := context.WithTimeout(
					context.Background(),
					benchmarkConsumerOperationTimeout,
				)
				consumed, err = consumer.Consume(ctx)
				cancel()
				if err != nil {
					benchmark.Fatalf(
						"consume %s with %s: %v",
						mode,
						candidate.name,
						err,
					)
				}
				if consumed != operationRecords {
					benchmark.Fatalf(
						"consume %s count with %s = %d, want %d",
						mode,
						candidate.name,
						consumed,
						operationRecords,
					)
				}
				completedOperations++
			}
			benchmark.StopTimer()
			benchmark.ReportMetric(float64(operationRecords), "records/op")
			runtime.KeepAlive(consumedBytes)
		})
	}
}

func TestEquivalentConsumerOutcomes(t *testing.T) {
	brokers := benchmarkBrokers(t)
	for _, mode := range []benchmarkConsumerMode{
		benchmarkConsumeRecord,
		benchmarkConsumeBatch,
	} {
		t.Run(mode.String(), func(t *testing.T) {
			for _, candidate := range benchmarkConsumerCandidates {
				t.Run(candidate.name, func(t *testing.T) {
					const recordCount = 3
					topic, want := prepareBenchmarkConsumerTopic(
						t,
						brokers,
						recordCount,
						128,
						compressionSnappy,
					)
					groupID := benchmarkConsumerGroupID(candidate.name, mode)
					got := make([]benchmarkConsumedRecord, 0, recordCount)
					sink := benchmarkConsumerSink{
						record: func(record benchmarkConsumedRecord) error {
							got = append(got, retainBenchmarkConsumedRecord(record))

							return nil
						},
						batch: func(records []benchmarkConsumedRecord) error {
							for _, record := range records {
								got = append(
									got,
									retainBenchmarkConsumedRecord(record),
								)
							}

							return nil
						},
					}
					operationRecords := 1
					operations := recordCount
					if mode == benchmarkConsumeBatch {
						operationRecords = recordCount
						operations = 1
					}
					consumer := candidate.new(
						t,
						brokers,
						topic,
						groupID,
						mode,
						operationRecords,
						sink,
					)
					for range operations {
						ctx, cancel := context.WithTimeout(
							context.Background(),
							benchmarkConsumerOperationTimeout,
						)
						consumed, err := consumer.Consume(ctx)
						cancel()
						if err != nil {
							t.Fatalf(
								"consume %s with %s: %v",
								mode,
								candidate.name,
								err,
							)
						}
						if consumed != operationRecords {
							t.Fatalf(
								"consume %s count with %s = %d, want %d",
								mode,
								candidate.name,
								consumed,
								operationRecords,
							)
						}
					}
					closeBenchmarkConsumer(t, consumer)
					if !slices.EqualFunc(got, want, equalBenchmarkConsumedRecord) {
						t.Fatalf(
							"consume %s records with %s = %#v, want %#v",
							mode,
							candidate.name,
							got,
							want,
						)
					}
					assertBenchmarkConsumerOffset(
						t,
						brokers,
						groupID,
						topic,
						recordCount,
					)
				})
			}
		})
	}
}

func TestBenchmarkConsumerOperationBytes(t *testing.T) {
	t.Parallel()
	if got, want := benchmarkConsumerOperationBytes(100, 1024), int64(104_700); got != want {
		t.Fatalf("consumer operation bytes = %d, want %d", got, want)
	}
}

func TestBenchmarkConsumerTopicReuse(t *testing.T) {
	brokers := benchmarkBrokers(t)
	first, _ := prepareBenchmarkConsumerTopic(
		t,
		brokers,
		3,
		128,
		compressionNone,
	)
	second, _ := prepareBenchmarkConsumerTopic(
		t,
		brokers,
		5,
		128,
		compressionNone,
	)
	if first != second {
		t.Fatalf("consumer fixture topic changed from %q to %q", first, second)
	}
}

func closeBenchmarkConsumer(t testing.TB, consumer benchmarkConsumer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(
		context.Background(),
		benchmarkConsumerOperationTimeout,
	)
	defer cancel()
	if err := consumer.Close(ctx); err != nil {
		t.Fatalf("close benchmark consumer: %v", err)
	}
}

func benchmarkConsumerGroupID(
	candidate string,
	mode benchmarkConsumerMode,
) string {
	return fmt.Sprintf(
		"golib-client-consumer-benchmark-%s-%s-%d",
		candidate,
		mode,
		time.Now().UnixNano(),
	)
}

const benchmarkConsumerKeyBytes = len("consumer-key-0000000000")

func benchmarkConsumerOperationBytes(
	operationRecords int,
	payloadBytes int,
) int64 {
	return int64(operationRecords * (benchmarkConsumerKeyBytes + payloadBytes))
}

type policyBenchmarkConsumer struct {
	consumer         *policy.Consumer
	mode             benchmarkConsumerMode
	operationRecords int
	sink             benchmarkConsumerSink
}

func newPolicyBenchmarkConsumer(
	t testing.TB,
	brokers []string,
	topic string,
	groupID string,
	mode benchmarkConsumerMode,
	operationRecords int,
	sink benchmarkConsumerSink,
) benchmarkConsumer {
	t.Helper()
	consumer, err := policy.NewConsumer(policy.ConsumerConfig{
		Brokers:                brokers,
		ClientID:               groupID,
		GroupID:                groupID,
		Topics:                 []string{topic},
		ResetOffset:            policy.OffsetEarliest,
		BalancePolicy:          policy.BalanceCooperativeSticky,
		Limits:                 policy.DefaultMessageLimits(),
		MaxPollRecords:         operationRecords,
		MaxConcurrentFetches:   1,
		MaxConcurrentHandlers:  1,
		FetchMaxBytes:          1 << 20,
		FetchMaxPartitionBytes: 1 << 20,
		FetchMaxWait:           50 * time.Millisecond,
		SessionTimeout:         10 * time.Second,
		RebalanceTimeout:       15 * time.Second,
		HeartbeatInterval:      time.Second,
		HandlerTimeout:         3 * time.Second,
		CommitTimeout:          3 * time.Second,
		ShutdownTimeout:        benchmarkConsumerOperationTimeout,
		DialTimeout:            benchmarkRequestTimeout,
		Security:               policy.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct policy benchmark consumer: %v", err)
	}

	return &policyBenchmarkConsumer{
		consumer:         consumer,
		mode:             mode,
		operationRecords: operationRecords,
		sink:             sink,
	}
}

func (consumer *policyBenchmarkConsumer) Consume(
	ctx context.Context,
) (int, error) {
	if consumer.mode == benchmarkConsumeBatch {
		result, err := consumer.consumer.RunBatchOnce(
			ctx,
			policy.BatchHandlerFunc(func(
				_ context.Context,
				batch policy.ConsumedBatch,
			) error {
				records := make([]benchmarkConsumedRecord, len(batch.Records))
				for index, record := range batch.Records {
					records[index] = benchmarkRecordFromPolicy(record)
				}

				return consumer.sink.batch(records)
			}),
		)

		return validateBenchmarkPollResult(result, consumer.operationRecords, err)
	}
	result, err := consumer.consumer.RunOnce(
		ctx,
		policy.HandlerFunc(func(
			_ context.Context,
			record policy.ConsumedMessage,
		) error {
			return consumer.sink.record(benchmarkRecordFromPolicy(record))
		}),
	)

	return validateBenchmarkPollResult(result, consumer.operationRecords, err)
}

func (consumer *policyBenchmarkConsumer) Close(ctx context.Context) error {
	return consumer.consumer.Shutdown(ctx)
}

func benchmarkRecordFromPolicy(
	record policy.ConsumedRecord,
) benchmarkConsumedRecord {
	return benchmarkConsumedRecord{
		topic:     record.Topic,
		partition: record.Partition,
		offset:    record.Offset,
		key:       record.Key,
		value:     record.Value,
	}
}

func validateBenchmarkPollResult(
	result policy.PollResult,
	want int,
	err error,
) (int, error) {
	if err != nil {
		return 0, err
	}
	if result.Polled != want ||
		result.Processed != want ||
		result.Committed != want {
		return 0, fmt.Errorf(
			"consumer poll result = %#v, want %d polled, processed, and committed",
			result,
			want,
		)
	}

	return result.Processed, nil
}

type franzBenchmarkConsumer struct {
	client           *kgo.Client
	mode             benchmarkConsumerMode
	operationRecords int
	sink             benchmarkConsumerSink
}

func newFranzBenchmarkConsumer(
	t testing.TB,
	brokers []string,
	topic string,
	groupID string,
	mode benchmarkConsumerMode,
	operationRecords int,
	sink benchmarkConsumerSink,
) benchmarkConsumer {
	t.Helper()
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID(groupID),
		kgo.ConsumerGroup(groupID),
		kgo.ConsumeTopics(topic),
		kgo.ConsumeStartOffset(kgo.NewOffset().AtStart()),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.DisableAutoCommit(),
		kgo.BlockRebalanceOnPoll(),
		kgo.Balancers(kgo.CooperativeStickyBalancer()),
		kgo.MaxConcurrentFetches(1),
		kgo.FetchMaxBytes(1<<20),
		kgo.FetchMaxPartitionBytes(1<<20),
		kgo.FetchMaxWait(50*time.Millisecond),
		kgo.SessionTimeout(10*time.Second),
		kgo.RebalanceTimeout(15*time.Second),
		kgo.HeartbeatInterval(time.Second),
		kgo.DialTimeout(benchmarkRequestTimeout),
	)
	if err != nil {
		t.Fatalf("construct raw franz-go benchmark consumer: %v", err)
	}

	return &franzBenchmarkConsumer{
		client:           client,
		mode:             mode,
		operationRecords: operationRecords,
		sink:             sink,
	}
}

func (consumer *franzBenchmarkConsumer) Consume(
	ctx context.Context,
) (int, error) {
	fetches := consumer.client.PollRecords(ctx, consumer.operationRecords)
	defer consumer.client.AllowRebalance()
	if err := fetches.Err(); err != nil {
		return 0, err
	}
	records := fetches.Records()
	if len(records) != consumer.operationRecords {
		return 0, fmt.Errorf(
			"raw franz-go poll count = %d, want %d",
			len(records),
			consumer.operationRecords,
		)
	}
	if err := consumeFranzBenchmarkRecords(
		consumer.mode,
		consumer.sink,
		records,
	); err != nil {
		return 0, err
	}
	if err := consumer.client.CommitRecords(ctx, records[len(records)-1]); err != nil {
		return 0, err
	}

	return len(records), nil
}

func (consumer *franzBenchmarkConsumer) Close(ctx context.Context) error {
	err := consumer.client.LeaveGroupContext(ctx)
	consumer.client.Close()

	return err
}

func consumeFranzBenchmarkRecords(
	mode benchmarkConsumerMode,
	sink benchmarkConsumerSink,
	records []*kgo.Record,
) error {
	if mode == benchmarkConsumeRecord {
		return sink.record(benchmarkRecordFromFranz(records[0]))
	}
	batch := make([]benchmarkConsumedRecord, len(records))
	for index, record := range records {
		batch[index] = benchmarkRecordFromFranz(record)
	}

	return sink.batch(batch)
}

func benchmarkRecordFromFranz(record *kgo.Record) benchmarkConsumedRecord {
	return benchmarkConsumedRecord{
		topic:     record.Topic,
		partition: record.Partition,
		offset:    record.Offset,
		key:       record.Key,
		value:     record.Value,
	}
}

type kafkaGoBenchmarkConsumer struct {
	reader           *segmentkafka.Reader
	mode             benchmarkConsumerMode
	operationRecords int
	sink             benchmarkConsumerSink
}

func newKafkaGoBenchmarkConsumer(
	t testing.TB,
	brokers []string,
	topic string,
	groupID string,
	mode benchmarkConsumerMode,
	operationRecords int,
	sink benchmarkConsumerSink,
) benchmarkConsumer {
	t.Helper()
	queueCapacity := max(100, operationRecords*2)
	reader := segmentkafka.NewReader(segmentkafka.ReaderConfig{
		Brokers:          slices.Clone(brokers),
		GroupID:          groupID,
		Topic:            topic,
		QueueCapacity:    queueCapacity,
		MinBytes:         1,
		MaxBytes:         1 << 20,
		MaxWait:          50 * time.Millisecond,
		ReadBatchTimeout: 50 * time.Millisecond,
		ReadLagInterval:  -1,
		GroupBalancers: []segmentkafka.GroupBalancer{
			segmentkafka.RangeGroupBalancer{},
		},
		HeartbeatInterval: time.Second,
		CommitInterval:    0,
		SessionTimeout:    10 * time.Second,
		RebalanceTimeout:  15 * time.Second,
		StartOffset:       segmentkafka.FirstOffset,
		MaxAttempts:       benchmarkRecordRetries,
	})

	return &kafkaGoBenchmarkConsumer{
		reader:           reader,
		mode:             mode,
		operationRecords: operationRecords,
		sink:             sink,
	}
}

func (consumer *kafkaGoBenchmarkConsumer) Consume(
	ctx context.Context,
) (int, error) {
	messages := make([]segmentkafka.Message, consumer.operationRecords)
	for index := range messages {
		message, err := consumer.reader.FetchMessage(ctx)
		if err != nil {
			return 0, err
		}
		messages[index] = message
	}
	if err := consumeKafkaGoBenchmarkRecords(
		consumer.mode,
		consumer.sink,
		messages,
	); err != nil {
		return 0, err
	}
	if err := consumer.reader.CommitMessages(
		ctx,
		messages[len(messages)-1],
	); err != nil {
		return 0, err
	}

	return len(messages), nil
}

func (consumer *kafkaGoBenchmarkConsumer) Close(context.Context) error {
	return consumer.reader.Close()
}

func consumeKafkaGoBenchmarkRecords(
	mode benchmarkConsumerMode,
	sink benchmarkConsumerSink,
	messages []segmentkafka.Message,
) error {
	if mode == benchmarkConsumeRecord {
		return sink.record(benchmarkRecordFromKafkaGo(messages[0]))
	}
	batch := make([]benchmarkConsumedRecord, len(messages))
	for index, message := range messages {
		batch[index] = benchmarkRecordFromKafkaGo(message)
	}

	return sink.batch(batch)
}

func benchmarkRecordFromKafkaGo(
	message segmentkafka.Message,
) benchmarkConsumedRecord {
	return benchmarkConsumedRecord{
		topic:     message.Topic,
		partition: int32(message.Partition),
		offset:    message.Offset,
		key:       message.Key,
		value:     message.Value,
	}
}

type saramaBenchmarkConsumer struct {
	group      sarama.ConsumerGroup
	cancel     context.CancelFunc
	done       chan struct{}
	fatal      chan error
	requests   chan saramaBenchmarkConsumerRequest
	closeOnce  sync.Once
	closeError error
}

type saramaBenchmarkConsumerRequest struct {
	ctx  context.Context
	done chan saramaBenchmarkConsumerResult
}

type saramaBenchmarkConsumerResult struct {
	consumed int
	err      error
}

type saramaBenchmarkConsumerHandler struct {
	mode             benchmarkConsumerMode
	operationRecords int
	sink             benchmarkConsumerSink
	requests         <-chan saramaBenchmarkConsumerRequest
	ready            chan<- struct{}
}

func newSaramaBenchmarkConsumer(
	t testing.TB,
	brokers []string,
	topic string,
	groupID string,
	mode benchmarkConsumerMode,
	operationRecords int,
	sink benchmarkConsumerSink,
) benchmarkConsumer {
	t.Helper()
	config := sarama.NewConfig()
	config.Version = sarama.V3_6_0_0
	config.ClientID = groupID
	config.Metadata.Full = false
	config.Net.DialTimeout = benchmarkRequestTimeout
	config.Net.ReadTimeout = benchmarkRequestTimeout
	config.Net.WriteTimeout = benchmarkRequestTimeout
	config.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{
		sarama.NewBalanceStrategyCooperativeSticky(),
	}
	config.Consumer.Group.Session.Timeout = 10 * time.Second
	config.Consumer.Group.Rebalance.Timeout = 15 * time.Second
	config.Consumer.Group.Heartbeat.Interval = time.Second
	config.Consumer.Offsets.AutoCommit.Enable = false
	config.Consumer.Offsets.Initial = sarama.OffsetOldest
	config.Consumer.Fetch.Min = 1
	config.Consumer.Fetch.Default = 1 << 20
	config.Consumer.Fetch.Max = 1 << 20
	config.Consumer.MaxWaitTime = 50 * time.Millisecond
	config.ChannelBufferSize = max(100, operationRecords*2)
	config.Consumer.Return.Errors = false
	config.Consumer.Retry.Backoff = benchmarkRetryMin
	config.Consumer.Offsets.Retry.Max = benchmarkRecordRetries
	config.Consumer.Group.Rebalance.Retry.Max = benchmarkRecordRetries
	config.Consumer.Group.Rebalance.Retry.Backoff = benchmarkRetryMin
	config.Consumer.Offsets.AutoCommit.Interval = time.Second
	config.Consumer.Group.ResetInvalidOffsets = false
	group, err := sarama.NewConsumerGroup(brokers, groupID, config)
	if err != nil {
		t.Fatalf("construct Sarama benchmark consumer: %v", err)
	}
	lifecycleCtx, cancel := context.WithCancel(context.Background())
	requests := make(chan saramaBenchmarkConsumerRequest)
	ready := make(chan struct{}, 1)
	fatal := make(chan error, 1)
	done := make(chan struct{})
	handler := &saramaBenchmarkConsumerHandler{
		mode:             mode,
		operationRecords: operationRecords,
		sink:             sink,
		requests:         requests,
		ready:            ready,
	}
	consumer := &saramaBenchmarkConsumer{
		group:    group,
		cancel:   cancel,
		done:     done,
		fatal:    fatal,
		requests: requests,
	}
	go func() {
		defer close(done)
		for lifecycleCtx.Err() == nil {
			consumeErr := group.Consume(lifecycleCtx, []string{topic}, handler)
			if consumeErr != nil && lifecycleCtx.Err() == nil {
				select {
				case fatal <- consumeErr:
				default:
				}

				return
			}
		}
	}()
	readyCtx, readyCancel := context.WithTimeout(
		context.Background(),
		benchmarkConsumerOperationTimeout,
	)
	defer readyCancel()
	select {
	case <-ready:
		return consumer
	case consumeErr := <-fatal:
		consumer.cancel()
		_ = group.Close()
		t.Fatalf("start Sarama benchmark consumer: %v", consumeErr)
	case <-readyCtx.Done():
		consumer.cancel()
		_ = group.Close()
		t.Fatalf("start Sarama benchmark consumer: %v", readyCtx.Err())
	}

	return nil
}

func (consumer *saramaBenchmarkConsumer) Consume(
	ctx context.Context,
) (int, error) {
	result := make(chan saramaBenchmarkConsumerResult, 1)
	request := saramaBenchmarkConsumerRequest{ctx: ctx, done: result}
	select {
	case consumer.requests <- request:
	case err := <-consumer.fatal:
		return 0, err
	case <-consumer.done:
		return 0, errSaramaBenchmarkConsumerStopped
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	select {
	case consumed := <-result:
		return consumed.consumed, consumed.err
	case err := <-consumer.fatal:
		return 0, err
	case <-consumer.done:
		return 0, errSaramaBenchmarkConsumerStopped
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func (consumer *saramaBenchmarkConsumer) Close(ctx context.Context) error {
	consumer.closeOnce.Do(func() {
		consumer.cancel()
		consumer.closeError = consumer.group.Close()
	})
	select {
	case <-consumer.done:
		return consumer.closeError
	case <-ctx.Done():
		return errors.Join(consumer.closeError, ctx.Err())
	}
}

func (handler *saramaBenchmarkConsumerHandler) Setup(
	sarama.ConsumerGroupSession,
) error {
	select {
	case handler.ready <- struct{}{}:
	default:
	}

	return nil
}

func (*saramaBenchmarkConsumerHandler) Cleanup(
	sarama.ConsumerGroupSession,
) error {
	return nil
}

func (handler *saramaBenchmarkConsumerHandler) ConsumeClaim(
	session sarama.ConsumerGroupSession,
	claim sarama.ConsumerGroupClaim,
) error {
	for {
		var request saramaBenchmarkConsumerRequest
		select {
		case request = <-handler.requests:
		case <-session.Context().Done():
			return nil
		}
		messages := make([]*sarama.ConsumerMessage, handler.operationRecords)
		for index := range messages {
			select {
			case message, ok := <-claim.Messages():
				if !ok {
					request.done <- saramaBenchmarkConsumerResult{
						err: errSaramaBenchmarkClaimClosed,
					}

					return nil
				}
				messages[index] = message
			case <-request.ctx.Done():
				request.done <- saramaBenchmarkConsumerResult{
					err: request.ctx.Err(),
				}

				return request.ctx.Err()
			case <-session.Context().Done():
				request.done <- saramaBenchmarkConsumerResult{
					err: session.Context().Err(),
				}

				return nil
			}
		}
		if err := consumeSaramaBenchmarkRecords(
			handler.mode,
			handler.sink,
			messages,
		); err != nil {
			request.done <- saramaBenchmarkConsumerResult{err: err}

			continue
		}
		session.MarkMessage(messages[len(messages)-1], "")
		session.Commit()
		request.done <- saramaBenchmarkConsumerResult{
			consumed: len(messages),
		}
	}
}

func consumeSaramaBenchmarkRecords(
	mode benchmarkConsumerMode,
	sink benchmarkConsumerSink,
	messages []*sarama.ConsumerMessage,
) error {
	if mode == benchmarkConsumeRecord {
		return sink.record(benchmarkRecordFromSarama(messages[0]))
	}
	batch := make([]benchmarkConsumedRecord, len(messages))
	for index, message := range messages {
		batch[index] = benchmarkRecordFromSarama(message)
	}

	return sink.batch(batch)
}

func benchmarkRecordFromSarama(
	message *sarama.ConsumerMessage,
) benchmarkConsumedRecord {
	return benchmarkConsumedRecord{
		topic:     message.Topic,
		partition: message.Partition,
		offset:    message.Offset,
		key:       message.Key,
		value:     message.Value,
	}
}

func prepareBenchmarkConsumerTopic(
	t testing.TB,
	brokers []string,
	recordCount int,
	payloadBytes int,
	compression benchmarkCompression,
) (string, []benchmarkConsumedRecord) {
	t.Helper()
	topic := ensureBenchmarkConsumerTopic(
		t,
		brokers,
		recordCount,
		payloadBytes,
		compression,
	)

	return topic, benchmarkConsumerExpectedRecords(topic, recordCount, payloadBytes)
}

func ensureBenchmarkConsumerTopic(
	t testing.TB,
	brokers []string,
	recordCount int,
	payloadBytes int,
	compression benchmarkCompression,
) string {
	t.Helper()
	key := benchmarkConsumerTopicKey{
		payloadBytes: payloadBytes,
		compression:  compression,
	}
	benchmarkConsumerTopicsMu.Lock()
	state := benchmarkConsumerTopics[key]
	if state == nil {
		state = &benchmarkConsumerTopicState{}
		benchmarkConsumerTopics[key] = state
	}
	benchmarkConsumerTopicsMu.Unlock()

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.topic == "" {
		state.topic = createIsolatedBenchmarkTopic(t, brokers)
	}
	if state.records >= recordCount {
		return state.topic
	}
	records := benchmarkConsumerProducerRecords(
		state.records,
		recordCount,
		payloadBytes,
	)
	producer := newFranzProducer(
		t,
		brokers,
		state.topic,
		true,
		compression,
	)
	const produceChunk = 500
	for start := 0; start < len(records); start += produceChunk {
		end := min(start+produceChunk, len(records))
		ctx, cancel := context.WithTimeout(
			context.Background(),
			benchmarkDeliveryTimeout+benchmarkRetryMax,
		)
		err := producer.ProduceBatch(ctx, records[start:end])
		cancel()
		if err != nil {
			closeBenchmarkProducerAfterSetupFailure(t, producer)
			t.Fatalf("prepare benchmark consumer topic: %v", err)
		}
	}
	ctx, cancel := context.WithTimeout(
		context.Background(),
		benchmarkDeliveryTimeout+benchmarkRetryMax,
	)
	err := producer.Close(ctx)
	cancel()
	if err != nil {
		t.Fatalf("close benchmark setup producer: %v", err)
	}
	state.records = recordCount

	return state.topic
}

func benchmarkConsumerProducerRecords(
	start int,
	end int,
	payloadBytes int,
) []benchmarkProducerRecord {
	records := make([]benchmarkProducerRecord, 0, end-start)
	for index := start; index < end; index++ {
		key, value := benchmarkConsumerRecordBytes(index, payloadBytes)
		records = append(records, benchmarkProducerRecord{key: key, value: value})
	}

	return records
}

func benchmarkConsumerExpectedRecords(
	topic string,
	recordCount int,
	payloadBytes int,
) []benchmarkConsumedRecord {
	want := make([]benchmarkConsumedRecord, recordCount)
	for index := range recordCount {
		key, value := benchmarkConsumerRecordBytes(index, payloadBytes)
		want[index] = benchmarkConsumedRecord{
			topic:     topic,
			partition: 0,
			offset:    int64(index),
			key:       slices.Clone(key),
			value:     slices.Clone(value),
		}
	}

	return want
}

func benchmarkConsumerRecordBytes(
	index int,
	payloadBytes int,
) ([]byte, []byte) {
	key := []byte(fmt.Sprintf("consumer-key-%010d", index))
	value := make([]byte, payloadBytes)
	for valueIndex := range value {
		value[valueIndex] = byte((index + valueIndex) % 251)
	}

	return key, value
}

func closeBenchmarkProducerAfterSetupFailure(
	t testing.TB,
	producer synchronousProducer,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(
		context.Background(),
		benchmarkDeliveryTimeout+benchmarkRetryMax,
	)
	defer cancel()
	if err := producer.Close(ctx); err != nil {
		t.Errorf("close failed benchmark setup producer: %v", err)
	}
}

func assertBenchmarkConsumerOffset(
	t testing.TB,
	brokers []string,
	groupID string,
	topic string,
	want int,
) {
	t.Helper()
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("golib-client-consumer-benchmark-offset-inspector"),
	)
	if err != nil {
		t.Fatalf("construct benchmark offset inspector: %v", err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(
		context.Background(),
		benchmarkConsumerOperationTimeout,
	)
	defer cancel()
	offsets, err := kadm.NewClient(client).FetchOffsets(ctx, groupID)
	if err != nil {
		t.Fatalf("fetch benchmark consumer offset: %v", err)
	}
	offset, exists := offsets.Lookup(topic, 0)
	if !exists {
		t.Fatalf("benchmark consumer offset for %s[0] is missing", topic)
	}
	if offset.Err != nil {
		t.Fatalf("benchmark consumer offset error: %v", offset.Err)
	}
	if offset.At != int64(want) {
		t.Fatalf("benchmark consumer offset = %d, want %d", offset.At, want)
	}
}

func retainBenchmarkConsumedRecord(
	record benchmarkConsumedRecord,
) benchmarkConsumedRecord {
	record.key = slices.Clone(record.key)
	record.value = slices.Clone(record.value)

	return record
}

func equalBenchmarkConsumedRecord(
	left benchmarkConsumedRecord,
	right benchmarkConsumedRecord,
) bool {
	return left.topic == right.topic &&
		left.partition == right.partition &&
		left.offset == right.offset &&
		slices.Equal(left.key, right.key) &&
		slices.Equal(left.value, right.value)
}
