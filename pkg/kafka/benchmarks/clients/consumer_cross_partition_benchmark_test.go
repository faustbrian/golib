//go:build integration

package clients_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	policy "github.com/faustbrian/golib/pkg/kafka"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

const benchmarkCrossPartitionCount = 8

type crossPartitionConsumerMode uint8

const (
	crossPartitionSequential crossPartitionConsumerMode = iota
	crossPartitionParallel
)

func (mode crossPartitionConsumerMode) String() string {
	if mode == crossPartitionParallel {
		return "parallel"
	}

	return "sequential"
}

func (mode crossPartitionConsumerMode) concurrency() int {
	if mode == crossPartitionParallel {
		return benchmarkCrossPartitionCount
	}

	return 1
}

type crossPartitionConsumer interface {
	Consume(context.Context) (int, int, error)
	Close(context.Context) error
}

type crossPartitionConsumerCandidate struct {
	name string
	new  func(
		testing.TB,
		[]string,
		string,
		string,
		int,
		func(benchmarkConsumedRecord) error,
	) crossPartitionConsumer
}

var crossPartitionConsumerCandidates = []crossPartitionConsumerCandidate{
	{name: "golib-policy", new: newPolicyCrossPartitionConsumer},
	{name: "raw-franz-go", new: newFranzCrossPartitionConsumer},
}

type crossPartitionTopicKey struct {
	payloadBytes int
	compression  benchmarkCompression
}

var (
	crossPartitionTopicsMu sync.Mutex
	crossPartitionTopics   = make(map[crossPartitionTopicKey]string)
)

func BenchmarkEquivalentCrossPartitionConsumerHandling(
	benchmark *testing.B,
) {
	brokers := benchmarkBrokers(benchmark)
	for _, mode := range []crossPartitionConsumerMode{
		crossPartitionSequential,
		crossPartitionParallel,
	} {
		for _, payloadBytes := range []int{128, 1024} {
			for _, compression := range []benchmarkCompression{
				compressionNone,
				compressionSnappy,
			} {
				name := fmt.Sprintf(
					"%s/%dB/%s",
					mode,
					payloadBytes,
					compression,
				)
				benchmark.Run(name, func(benchmark *testing.B) {
					benchmarkEquivalentCrossPartitionConsumerHandling(
						benchmark,
						brokers,
						mode,
						payloadBytes,
						compression,
					)
				})
			}
		}
	}
}

func benchmarkEquivalentCrossPartitionConsumerHandling(
	benchmark *testing.B,
	brokers []string,
	mode crossPartitionConsumerMode,
	payloadBytes int,
	compression benchmarkCompression,
) {
	benchmark.Helper()
	topic := crossPartitionBenchmarkTopic(
		benchmark,
		brokers,
		payloadBytes,
		compression,
	)
	for _, candidate := range crossPartitionConsumerCandidates {
		benchmark.Run(candidate.name, func(benchmark *testing.B) {
			groupID := crossPartitionConsumerGroupID(candidate.name, mode)
			commitBenchmarkGroupToTopicEnd(
				benchmark,
				brokers,
				groupID,
				topic,
			)
			producer := newFranzMultiPartitionProducer(
				benchmark,
				brokers,
				topic,
				multiPartitionExplicit,
				compression,
			)
			benchmark.Cleanup(func() {
				ctx, cancel := context.WithTimeout(
					context.Background(),
					benchmarkDeliveryTimeout+benchmarkRetryMax,
				)
				defer cancel()
				if err := producer.Close(ctx); err != nil {
					benchmark.Errorf(
						"close cross-partition input producer: %v",
						err,
					)
				}
			})

			var digests [benchmarkCrossPartitionCount][sha256.Size]byte
			consumer := candidate.new(
				benchmark,
				brokers,
				topic,
				groupID,
				mode.concurrency(),
				func(record benchmarkConsumedRecord) error {
					digests[record.partition] =
						crossPartitionHandlerDigest(record.value)

					return nil
				},
			)
			benchmark.Cleanup(func() {
				ctx, cancel := context.WithTimeout(
					context.Background(),
					benchmarkConsumerOperationTimeout,
				)
				defer cancel()
				if err := consumer.Close(ctx); err != nil {
					benchmark.Errorf(
						"close %s %s cross-partition consumer: %v",
						candidate.name,
						mode,
						err,
					)
				}
			})

			operation := 0
			produceCrossPartitionOperationWithProducer(
				benchmark,
				producer,
				operation,
				payloadBytes,
			)
			ctx, cancel := context.WithTimeout(
				context.Background(),
				benchmarkConsumerOperationTimeout,
			)
			consumed, _, err := consumer.Consume(ctx)
			cancel()
			if err != nil {
				benchmark.Fatalf(
					"warm %s %s cross-partition consumer: %v",
					candidate.name,
					mode,
					err,
				)
			}
			if consumed != benchmarkCrossPartitionCount {
				benchmark.Fatalf(
					"warm %s %s count = %d, want %d",
					candidate.name,
					mode,
					consumed,
					benchmarkCrossPartitionCount,
				)
			}

			benchmark.ReportAllocs()
			benchmark.SetBytes(crossPartitionOperationBytes(payloadBytes))
			benchmark.ResetTimer()
			totalCommits := 0
			completedOperations := 0
			for benchmark.Loop() {
				benchmark.StopTimer()
				operation++
				produceCrossPartitionOperationWithProducer(
					benchmark,
					producer,
					operation,
					payloadBytes,
				)
				benchmark.StartTimer()

				ctx, cancel = context.WithTimeout(
					context.Background(),
					benchmarkConsumerOperationTimeout,
				)
				var commits int
				consumed, commits, err = consumer.Consume(ctx)
				cancel()
				if err != nil {
					benchmark.Fatalf(
						"consume %s with %s: %v",
						mode,
						candidate.name,
						err,
					)
				}
				if consumed != benchmarkCrossPartitionCount {
					benchmark.Fatalf(
						"consume %s count with %s = %d, want %d",
						mode,
						candidate.name,
						consumed,
						benchmarkCrossPartitionCount,
					)
				}
				totalCommits += commits
				completedOperations++
			}
			benchmark.StopTimer()
			benchmark.ReportMetric(
				float64(totalCommits)/float64(completedOperations),
				"commits/op",
			)
			benchmark.ReportMetric(
				benchmarkCrossPartitionCount,
				"partitions/op",
			)
			benchmark.ReportMetric(
				benchmarkCrossPartitionCount,
				"records/op",
			)
			benchmark.ReportMetric(
				crossPartitionHandlerRounds,
				"handler-rounds/record",
			)
			runtime.KeepAlive(digests)
		})
	}
}

func TestEquivalentCrossPartitionConsumerOutcomes(t *testing.T) {
	brokers := benchmarkBrokers(t)
	for _, mode := range []crossPartitionConsumerMode{
		crossPartitionSequential,
		crossPartitionParallel,
	} {
		t.Run(mode.String(), func(t *testing.T) {
			for _, candidate := range crossPartitionConsumerCandidates {
				t.Run(candidate.name, func(t *testing.T) {
					topic := createIsolatedBenchmarkTopicWithPartitions(
						t,
						brokers,
						benchmarkCrossPartitionCount,
					)
					groupID := crossPartitionConsumerGroupID(
						candidate.name,
						mode,
					)
					probe := newCrossPartitionProbe()
					consumer := candidate.new(
						t,
						brokers,
						topic,
						groupID,
						mode.concurrency(),
						probe.Handle,
					)
					for operation := range 3 {
						produceCrossPartitionOperation(
							t,
							brokers,
							topic,
							operation,
							128,
							compressionSnappy,
						)
						ctx, cancel := context.WithTimeout(
							context.Background(),
							benchmarkConsumerOperationTimeout,
						)
						consumed, commits, err := consumer.Consume(ctx)
						cancel()
						if err != nil {
							t.Fatalf(
								"consume operation %d with %s: %v",
								operation,
								candidate.name,
								err,
							)
						}
						if consumed != benchmarkCrossPartitionCount {
							t.Fatalf(
								"consume operation %d count with %s = %d, want %d",
								operation,
								candidate.name,
								consumed,
								benchmarkCrossPartitionCount,
							)
						}
						if commits < 1 ||
							commits > benchmarkCrossPartitionCount {
							t.Fatalf(
								"consume operation %d commits with %s = %d, want 1 through %d",
								operation,
								candidate.name,
								commits,
								benchmarkCrossPartitionCount,
							)
						}
					}
					closeCrossPartitionConsumer(t, consumer)
					probe.Assert(t)
					assertBenchmarkConsumerPartitionOffsets(
						t,
						brokers,
						groupID,
						topic,
						3,
					)
				})
			}
		})
	}
}

type crossPartitionProbe struct {
	mu      sync.Mutex
	offsets map[int32][]int64
}

func newCrossPartitionProbe() *crossPartitionProbe {
	return &crossPartitionProbe{
		offsets: make(map[int32][]int64, benchmarkCrossPartitionCount),
	}
}

func (probe *crossPartitionProbe) Handle(record benchmarkConsumedRecord) error {
	probe.mu.Lock()
	probe.offsets[record.partition] = append(
		probe.offsets[record.partition],
		record.offset,
	)
	probe.mu.Unlock()

	return nil
}

func (probe *crossPartitionProbe) Assert(t *testing.T) {
	t.Helper()
	probe.mu.Lock()
	defer probe.mu.Unlock()
	if len(probe.offsets) != benchmarkCrossPartitionCount {
		t.Fatalf(
			"handled partition count = %d, want %d",
			len(probe.offsets),
			benchmarkCrossPartitionCount,
		)
	}
	for partition := range int32(benchmarkCrossPartitionCount) {
		if got, want := probe.offsets[partition], []int64{0, 1, 2}; !slices.Equal(got, want) {
			t.Fatalf(
				"partition %d offsets = %v, want %v",
				partition,
				got,
				want,
			)
		}
	}
}

func TestRunCrossPartitionHandlersConcurrency(t *testing.T) {
	records := []benchmarkConsumedRecord{
		{partition: 0},
		{partition: 1},
	}
	entered := make(chan struct{}, len(records))
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- runCrossPartitionHandlers(
			records,
			len(records),
			func(benchmarkConsumedRecord) error {
				entered <- struct{}{}
				<-release

				return nil
			},
		)
	}()
	for range records {
		select {
		case <-entered:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("cross-partition handlers did not overlap")
		}
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run cross-partition handlers: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cross-partition handlers did not finish")
	}
}

func closeCrossPartitionConsumer(
	t testing.TB,
	consumer crossPartitionConsumer,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(
		context.Background(),
		benchmarkConsumerOperationTimeout,
	)
	defer cancel()
	if err := consumer.Close(ctx); err != nil {
		t.Fatalf("close cross-partition consumer: %v", err)
	}
}

func crossPartitionConsumerGroupID(
	candidate string,
	mode crossPartitionConsumerMode,
) string {
	return fmt.Sprintf(
		"golib-client-cross-partition-consumer-%s-%s-%d",
		candidate,
		mode,
		time.Now().UnixNano(),
	)
}

const (
	crossPartitionHandlerRounds = 256
	crossPartitionKeyBytes      = len("cross-partition-key-00-0000000000")
)

func crossPartitionHandlerDigest(value []byte) [sha256.Size]byte {
	digest := sha256.Sum256(value)
	for range crossPartitionHandlerRounds - 1 {
		digest = sha256.Sum256(digest[:])
	}

	return digest
}

func crossPartitionOperationBytes(payloadBytes int) int64 {
	return int64(
		benchmarkCrossPartitionCount *
			(crossPartitionKeyBytes + payloadBytes),
	)
}

func TestBenchmarkCrossPartitionOperationBytes(t *testing.T) {
	if got, want := crossPartitionOperationBytes(1024), int64(8456); got != want {
		t.Fatalf("cross-partition operation bytes = %d, want %d", got, want)
	}
}

func crossPartitionBenchmarkTopic(
	t testing.TB,
	brokers []string,
	payloadBytes int,
	compression benchmarkCompression,
) string {
	t.Helper()
	key := crossPartitionTopicKey{
		payloadBytes: payloadBytes,
		compression:  compression,
	}
	crossPartitionTopicsMu.Lock()
	defer crossPartitionTopicsMu.Unlock()
	if topic := crossPartitionTopics[key]; topic != "" {
		return topic
	}
	topic := createIsolatedBenchmarkTopicWithPartitions(
		t,
		brokers,
		benchmarkCrossPartitionCount,
	)
	crossPartitionTopics[key] = topic

	return topic
}

func produceCrossPartitionOperation(
	t testing.TB,
	brokers []string,
	topic string,
	operation int,
	payloadBytes int,
	compression benchmarkCompression,
) {
	t.Helper()
	producer := newFranzMultiPartitionProducer(
		t,
		brokers,
		topic,
		multiPartitionExplicit,
		compression,
	)
	produceCrossPartitionOperationWithProducer(
		t,
		producer,
		operation,
		payloadBytes,
	)
	ctx, cancel := context.WithTimeout(
		context.Background(),
		benchmarkDeliveryTimeout+benchmarkRetryMax,
	)
	defer cancel()
	if err := producer.Close(ctx); err != nil {
		t.Fatalf("close cross-partition input producer: %v", err)
	}
}

func produceCrossPartitionOperationWithProducer(
	t testing.TB,
	producer multiPartitionProducer,
	operation int,
	payloadBytes int,
) {
	t.Helper()
	records := make(
		[]multiPartitionRecord,
		benchmarkCrossPartitionCount,
	)
	for partition := range int32(benchmarkCrossPartitionCount) {
		value := make([]byte, payloadBytes)
		for index := range value {
			value[index] = byte((operation + int(partition) + index) % 251)
		}
		records[partition] = multiPartitionRecord{
			partition: partition,
			key: []byte(fmt.Sprintf(
				"cross-partition-key-%02d-%010d",
				partition,
				operation,
			)),
			value: value,
		}
	}
	ctx, cancel := context.WithTimeout(
		context.Background(),
		benchmarkDeliveryTimeout+benchmarkRetryMax,
	)
	defer cancel()
	if err := producer.ProduceBatch(ctx, records); err != nil {
		t.Fatalf("produce cross-partition input: %v", err)
	}
}

func commitBenchmarkGroupToTopicEnd(
	t testing.TB,
	brokers []string,
	groupID string,
	topic string,
) {
	t.Helper()
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("golib-client-cross-partition-offset-seed"),
	)
	if err != nil {
		t.Fatalf("construct cross-partition offset seed client: %v", err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(
		context.Background(),
		benchmarkConsumerOperationTimeout,
	)
	defer cancel()
	admin := kadm.NewClient(client)
	endOffsets, err := admin.ListEndOffsets(ctx, topic)
	if err != nil {
		t.Fatalf("list cross-partition end offsets: %v", err)
	}
	if err := endOffsets.Error(); err != nil {
		t.Fatalf("cross-partition end offset: %v", err)
	}
	if len(endOffsets[topic]) != benchmarkCrossPartitionCount {
		t.Fatalf(
			"cross-partition end offset count = %d, want %d",
			len(endOffsets[topic]),
			benchmarkCrossPartitionCount,
		)
	}
	responses, err := admin.CommitOffsets(
		ctx,
		groupID,
		endOffsets.Offsets(),
	)
	if err != nil {
		t.Fatalf("seed cross-partition group offsets: %v", err)
	}
	if err := responses.Error(); err != nil {
		t.Fatalf("cross-partition group offset response: %v", err)
	}
}

func assertBenchmarkConsumerPartitionOffsets(
	t testing.TB,
	brokers []string,
	groupID string,
	topic string,
	want int64,
) {
	t.Helper()
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("golib-client-cross-partition-offset-inspector"),
	)
	if err != nil {
		t.Fatalf("construct cross-partition offset inspector: %v", err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(
		context.Background(),
		benchmarkConsumerOperationTimeout,
	)
	defer cancel()
	offsets, err := kadm.NewClient(client).FetchOffsets(ctx, groupID)
	if err != nil {
		t.Fatalf("fetch cross-partition offsets: %v", err)
	}
	for partition := range int32(benchmarkCrossPartitionCount) {
		offset, exists := offsets.Lookup(topic, partition)
		if !exists {
			t.Fatalf(
				"cross-partition offset for %s[%d] is missing",
				topic,
				partition,
			)
		}
		if offset.Err != nil {
			t.Fatalf(
				"cross-partition offset %d error: %v",
				partition,
				offset.Err,
			)
		}
		if offset.At != want {
			t.Fatalf(
				"cross-partition offset %d = %d, want %d",
				partition,
				offset.At,
				want,
			)
		}
	}
}

func validateCrossPartitionRecords(
	records []benchmarkConsumedRecord,
) error {
	if len(records) < 1 || len(records) > benchmarkCrossPartitionCount {
		return fmt.Errorf(
			"cross-partition record count = %d, want 1 through %d",
			len(records),
			benchmarkCrossPartitionCount,
		)
	}
	seen := make([]bool, benchmarkCrossPartitionCount)
	for _, record := range records {
		if record.partition < 0 ||
			record.partition >= benchmarkCrossPartitionCount {
			return fmt.Errorf(
				"cross-partition record has partition %d",
				record.partition,
			)
		}
		if seen[record.partition] {
			return fmt.Errorf(
				"cross-partition record duplicates partition %d",
				record.partition,
			)
		}
		seen[record.partition] = true
	}

	return nil
}

func runCrossPartitionHandlers(
	records []benchmarkConsumedRecord,
	concurrency int,
	handle func(benchmarkConsumedRecord) error,
) error {
	if err := validateCrossPartitionRecords(records); err != nil {
		return err
	}
	results := make([]error, len(records))
	jobs := make(chan int)
	workers := min(concurrency, len(records))
	var waitGroup sync.WaitGroup
	waitGroup.Add(workers)
	for range workers {
		go func() {
			defer waitGroup.Done()
			for index := range jobs {
				results[index] = handle(records[index])
			}
		}()
	}
	for index := range records {
		jobs <- index
	}
	close(jobs)
	waitGroup.Wait()
	for _, err := range results {
		if err != nil {
			return err
		}
	}

	return nil
}

type policyCrossPartitionConsumer struct {
	consumer *policy.Consumer
	handle   func(benchmarkConsumedRecord) error
}

func newPolicyCrossPartitionConsumer(
	t testing.TB,
	brokers []string,
	topic string,
	groupID string,
	concurrency int,
	handle func(benchmarkConsumedRecord) error,
) crossPartitionConsumer {
	t.Helper()
	consumer, err := policy.NewConsumer(policy.ConsumerConfig{
		Brokers:                brokers,
		ClientID:               groupID,
		GroupID:                groupID,
		Topics:                 []string{topic},
		ResetOffset:            policy.OffsetEarliest,
		BalancePolicy:          policy.BalanceCooperativeSticky,
		Limits:                 policy.DefaultMessageLimits(),
		MaxPollRecords:         benchmarkCrossPartitionCount,
		MaxConcurrentFetches:   1,
		MaxConcurrentHandlers:  concurrency,
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
		t.Fatalf("construct policy cross-partition consumer: %v", err)
	}

	return &policyCrossPartitionConsumer{
		consumer: consumer,
		handle:   handle,
	}
}

func (consumer *policyCrossPartitionConsumer) Consume(
	ctx context.Context,
) (int, int, error) {
	partitions := make(map[int32]struct{}, benchmarkCrossPartitionCount)
	var partitionsMu sync.Mutex
	processed := 0
	commits := 0
	for processed < benchmarkCrossPartitionCount {
		result, err := consumer.consumer.RunOnce(
			ctx,
			policy.HandlerFunc(func(
				_ context.Context,
				message policy.ConsumedMessage,
			) error {
				partitionsMu.Lock()
				_, duplicate := partitions[message.Partition]
				partitions[message.Partition] = struct{}{}
				partitionsMu.Unlock()
				if duplicate {
					return fmt.Errorf(
						"policy cross-partition duplicate %d",
						message.Partition,
					)
				}

				return consumer.handle(benchmarkRecordFromPolicy(message))
			}),
		)
		if err != nil {
			return 0, 0, err
		}
		if result.Polled == 0 {
			continue
		}
		if result.Polled != result.Processed ||
			result.Polled != result.Committed ||
			processed+result.Processed > benchmarkCrossPartitionCount {
			return 0, 0, fmt.Errorf(
				"policy cross-partition result = %#v after %d records",
				result,
				processed,
			)
		}
		processed += result.Processed
		commits++
	}
	if len(partitions) != benchmarkCrossPartitionCount {
		return 0, 0, fmt.Errorf(
			"policy cross-partition handled %d partitions",
			len(partitions),
		)
	}

	return processed, commits, nil
}

func (consumer *policyCrossPartitionConsumer) Close(ctx context.Context) error {
	return consumer.consumer.Shutdown(ctx)
}

type franzCrossPartitionConsumer struct {
	client      *kgo.Client
	concurrency int
	handle      func(benchmarkConsumedRecord) error
}

func newFranzCrossPartitionConsumer(
	t testing.TB,
	brokers []string,
	topic string,
	groupID string,
	concurrency int,
	handle func(benchmarkConsumedRecord) error,
) crossPartitionConsumer {
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
		t.Fatalf("construct raw franz-go cross-partition consumer: %v", err)
	}

	return &franzCrossPartitionConsumer{
		client:      client,
		concurrency: concurrency,
		handle:      handle,
	}
}

func (consumer *franzCrossPartitionConsumer) Consume(
	ctx context.Context,
) (int, int, error) {
	processed := 0
	commits := 0
	partitions := make(map[int32]struct{}, benchmarkCrossPartitionCount)
	for processed < benchmarkCrossPartitionCount {
		fetches := consumer.client.PollRecords(
			ctx,
			benchmarkCrossPartitionCount-processed,
		)
		if err := fetches.Err(); err != nil {
			consumer.client.AllowRebalance()

			return 0, 0, err
		}
		rawRecords := fetches.Records()
		if len(rawRecords) == 0 {
			consumer.client.AllowRebalance()

			continue
		}
		records := make([]benchmarkConsumedRecord, len(rawRecords))
		for index, record := range rawRecords {
			records[index] = benchmarkRecordFromFranz(record)
			if _, duplicate := partitions[record.Partition]; duplicate {
				consumer.client.AllowRebalance()

				return 0, 0, fmt.Errorf(
					"raw franz-go cross-partition duplicate %d",
					record.Partition,
				)
			}
			partitions[record.Partition] = struct{}{}
		}
		if err := runCrossPartitionHandlers(
			records,
			consumer.concurrency,
			consumer.handle,
		); err != nil {
			consumer.client.AllowRebalance()

			return 0, 0, err
		}
		if err := consumer.client.CommitRecords(ctx, rawRecords...); err != nil {
			consumer.client.AllowRebalance()

			return 0, 0, err
		}
		consumer.client.AllowRebalance()
		processed += len(records)
		commits++
	}

	return processed, commits, nil
}

func (consumer *franzCrossPartitionConsumer) Close(
	ctx context.Context,
) error {
	err := consumer.client.LeaveGroupContext(ctx)
	consumer.client.Close()

	return err
}
