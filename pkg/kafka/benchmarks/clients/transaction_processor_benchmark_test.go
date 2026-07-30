//go:build integration

package clients_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	policy "github.com/faustbrian/golib/pkg/kafka"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

var errAbortBenchmarkTransaction = errors.New(
	"abort benchmark consume-transform-produce transaction",
)

type benchmarkTransactionProcessor interface {
	Process(context.Context, int, int) (int, int, error)
	Close(context.Context) error
}

type transactionProcessorCandidate struct {
	name string
	new  func(
		testing.TB,
		[]string,
		string,
		string,
		string,
		string,
		int,
		benchmarkCompression,
	) benchmarkTransactionProcessor
}

var transactionProcessorCandidates = []transactionProcessorCandidate{
	{name: "golib-policy", new: newPolicyBenchmarkTransactionProcessor},
	{name: "raw-franz-go", new: newFranzBenchmarkTransactionProcessor},
}

type transactionProcessorTopicKey struct {
	payloadBytes int
	compression  benchmarkCompression
}

type transactionProcessorTopics struct {
	source string
	output string
}

var (
	transactionProcessorTopicsMu sync.Mutex
	transactionProcessorTopicMap = make(
		map[transactionProcessorTopicKey]transactionProcessorTopics,
	)
)

func BenchmarkEquivalentConsumeTransformProduce(benchmark *testing.B) {
	brokers := benchmarkBrokers(benchmark)
	for _, payloadBytes := range []int{128, 1024} {
		for _, recordCount := range []int{1, 10} {
			for _, compression := range []benchmarkCompression{
				compressionNone,
				compressionSnappy,
			} {
				name := fmt.Sprintf(
					"%d-records/%dB/%s",
					recordCount,
					payloadBytes,
					compression,
				)
				benchmark.Run(name, func(benchmark *testing.B) {
					benchmarkEquivalentConsumeTransformProduce(
						benchmark,
						brokers,
						payloadBytes,
						recordCount,
						compression,
					)
				})
			}
		}
	}
}

func benchmarkEquivalentConsumeTransformProduce(
	benchmark *testing.B,
	brokers []string,
	payloadBytes int,
	recordCount int,
	compression benchmarkCompression,
) {
	benchmark.Helper()
	topics := transactionProcessorBenchmarkTopics(
		benchmark,
		brokers,
		payloadBytes,
		compression,
	)
	sourceRecords := transactionalBenchmarkRecords(
		recordCount,
		payloadBytes,
	)
	for _, candidate := range transactionProcessorCandidates {
		benchmark.Run(candidate.name, func(benchmark *testing.B) {
			groupID := benchmarkTransactionProcessorGroupID(candidate.name)
			commitBenchmarkGroupToSinglePartitionEnd(
				benchmark,
				brokers,
				groupID,
				topics.source,
			)
			transactionalID := benchmarkTransactionalID(
				"processor-" + candidate.name,
			)
			processor := candidate.new(
				benchmark,
				brokers,
				topics.source,
				topics.output,
				groupID,
				transactionalID,
				recordCount,
				compression,
			)
			benchmark.Cleanup(func() {
				ctx, cancel := context.WithTimeout(
					context.Background(),
					benchmarkTransactionOperationTimeout,
				)
				defer cancel()
				if err := processor.Close(ctx); err != nil {
					benchmark.Errorf(
						"close %s transaction processor: %v",
						candidate.name,
						err,
					)
				}
			})
			sourceProducer := newFranzProducer(
				benchmark,
				brokers,
				topics.source,
				true,
				compression,
			)
			benchmark.Cleanup(func() {
				ctx, cancel := context.WithTimeout(
					context.Background(),
					benchmarkTransactionOperationTimeout,
				)
				defer cancel()
				if err := sourceProducer.Close(ctx); err != nil {
					benchmark.Errorf(
						"close transaction source producer: %v",
						err,
					)
				}
			})

			ctx, cancel := context.WithTimeout(
				context.Background(),
				benchmarkTransactionOperationTimeout,
			)
			if err := sourceProducer.ProduceBatch(ctx, sourceRecords); err != nil {
				cancel()
				benchmark.Fatalf("produce warm transaction source: %v", err)
			}
			processed, _, err := processor.Process(ctx, recordCount, 0)
			cancel()
			if err != nil || processed != recordCount {
				benchmark.Fatalf(
					"warm %s transaction processor = %d, %v",
					candidate.name,
					processed,
					err,
				)
			}

			benchmark.ReportAllocs()
			benchmark.SetBytes(
				2 * transactionalBenchmarkRecordBytes(sourceRecords),
			)
			benchmark.ResetTimer()
			totalTransactions := 0
			completedOperations := 0
			for benchmark.Loop() {
				benchmark.StopTimer()
				ctx, cancel = context.WithTimeout(
					context.Background(),
					benchmarkTransactionOperationTimeout,
				)
				if err := sourceProducer.ProduceBatch(
					ctx,
					sourceRecords,
				); err != nil {
					cancel()
					benchmark.Fatalf(
						"produce %s transaction source: %v",
						candidate.name,
						err,
					)
				}
				benchmark.StartTimer()

				var transactions int
				processed, transactions, err = processor.Process(
					ctx,
					recordCount,
					0,
				)
				cancel()
				if err != nil || processed != recordCount {
					benchmark.Fatalf(
						"process %s transaction = %d, %v",
						candidate.name,
						processed,
						err,
					)
				}
				totalTransactions += transactions
				completedOperations++
			}
			benchmark.StopTimer()
			benchmark.ReportMetric(
				float64(totalTransactions)/float64(completedOperations),
				"transactions/op",
			)
			benchmark.ReportMetric(float64(recordCount), "source-records/op")
			benchmark.ReportMetric(float64(recordCount), "output-records/op")
		})
	}
}

func TestEquivalentConsumeTransformProduceOutcomes(t *testing.T) {
	brokers := benchmarkBrokers(t)
	initial := []benchmarkProducerRecord{
		{key: []byte("source-0"), value: []byte("source-value-0")},
		{key: []byte("source-1"), value: []byte("source-value-1")},
	}
	retried := []benchmarkProducerRecord{
		{key: []byte("source-2"), value: []byte("source-value-2")},
	}
	for _, candidate := range transactionProcessorCandidates {
		t.Run(candidate.name, func(t *testing.T) {
			sourceTopic := createIsolatedBenchmarkTopic(t, brokers)
			outputTopic := createIsolatedBenchmarkTopic(t, brokers)
			groupID := benchmarkTransactionProcessorGroupID(candidate.name)
			processor := candidate.new(
				t,
				brokers,
				sourceTopic,
				outputTopic,
				groupID,
				benchmarkTransactionalID("processor-"+candidate.name),
				2,
				compressionSnappy,
			)
			sourceProducer := newFranzProducer(
				t,
				brokers,
				sourceTopic,
				true,
				compressionSnappy,
			)
			ctx, cancel := context.WithTimeout(
				context.Background(),
				benchmarkTransactionOperationTimeout,
			)
			defer cancel()
			if err := sourceProducer.ProduceBatch(ctx, initial); err != nil {
				t.Fatalf("produce initial transaction source: %v", err)
			}
			processed, transactions, err := processor.Process(
				ctx,
				len(initial),
				0,
			)
			if err != nil ||
				processed != len(initial) ||
				transactions < 1 ||
				transactions > len(initial) {
				t.Fatalf(
					"initial process = records:%d transactions:%d error:%v",
					processed,
					transactions,
					err,
				)
			}
			assertBenchmarkSinglePartitionOffset(
				t,
				brokers,
				groupID,
				sourceTopic,
				2,
			)
			assertTransactionalBenchmarkRecords(
				t,
				brokers,
				outputTopic,
				kgo.ReadCommitted(),
				transformedBenchmarkRecords(initial),
			)

			if err := sourceProducer.ProduceBatch(ctx, retried); err != nil {
				t.Fatalf("produce retry transaction source: %v", err)
			}
			processed, _, err = processor.Process(ctx, len(retried), 1)
			if !errors.Is(err, errAbortBenchmarkTransaction) ||
				processed != 0 {
				t.Fatalf(
					"aborted process = records:%d error:%v",
					processed,
					err,
				)
			}
			assertBenchmarkSinglePartitionOffset(
				t,
				brokers,
				groupID,
				sourceTopic,
				2,
			)
			assertTransactionalBenchmarkRecords(
				t,
				brokers,
				outputTopic,
				kgo.ReadCommitted(),
				transformedBenchmarkRecords(initial),
			)

			processed, transactions, err = processor.Process(
				ctx,
				len(retried),
				0,
			)
			if err != nil ||
				processed != len(retried) ||
				transactions != 1 {
				t.Fatalf(
					"retry process = records:%d transactions:%d error:%v",
					processed,
					transactions,
					err,
				)
			}
			assertBenchmarkSinglePartitionOffset(
				t,
				brokers,
				groupID,
				sourceTopic,
				3,
			)
			committedWant := append(
				transformedBenchmarkRecords(initial),
				transformedBenchmarkRecords(retried)...,
			)
			assertTransactionalBenchmarkRecords(
				t,
				brokers,
				outputTopic,
				kgo.ReadCommitted(),
				committedWant,
			)
			uncommittedWant := append(
				transformedBenchmarkRecords(initial),
				transformedBenchmarkRecords(retried)...,
			)
			uncommittedWant = append(
				uncommittedWant,
				transformedBenchmarkRecords(retried)...,
			)
			assertTransactionalBenchmarkRecords(
				t,
				brokers,
				outputTopic,
				kgo.ReadUncommitted(),
				uncommittedWant,
			)
			if err := processor.Close(ctx); err != nil {
				t.Fatalf("close transaction processor: %v", err)
			}
			if err := sourceProducer.Close(ctx); err != nil {
				t.Fatalf("close transaction source producer: %v", err)
			}
		})
	}
}

func transformBenchmarkRecord(
	record benchmarkConsumedRecord,
) benchmarkProducerRecord {
	value := slices.Clone(record.value)
	if len(value) != 0 {
		value[0] ^= 0xff
	}

	return benchmarkProducerRecord{
		key:   slices.Clone(record.key),
		value: value,
	}
}

func transformedBenchmarkRecords(
	records []benchmarkProducerRecord,
) []observedBenchmarkRecord {
	transformed := make([]observedBenchmarkRecord, len(records))
	for index, record := range records {
		value := slices.Clone(record.value)
		if len(value) != 0 {
			value[0] ^= 0xff
		}
		transformed[index] = observedBenchmarkRecord{
			key:   string(record.key),
			value: string(value),
		}
	}

	return transformed
}

func benchmarkTransactionProcessorGroupID(candidate string) string {
	return fmt.Sprintf(
		"golib-client-transaction-processor-%s-%d",
		candidate,
		time.Now().UnixNano(),
	)
}

func transactionProcessorBenchmarkTopics(
	t testing.TB,
	brokers []string,
	payloadBytes int,
	compression benchmarkCompression,
) transactionProcessorTopics {
	t.Helper()
	key := transactionProcessorTopicKey{
		payloadBytes: payloadBytes,
		compression:  compression,
	}
	transactionProcessorTopicsMu.Lock()
	defer transactionProcessorTopicsMu.Unlock()
	if topics := transactionProcessorTopicMap[key]; topics.source != "" {
		return topics
	}
	topics := transactionProcessorTopics{
		source: createIsolatedBenchmarkTopic(t, brokers),
		output: createIsolatedBenchmarkTopic(t, brokers),
	}
	transactionProcessorTopicMap[key] = topics

	return topics
}

func commitBenchmarkGroupToSinglePartitionEnd(
	t testing.TB,
	brokers []string,
	groupID string,
	topic string,
) {
	t.Helper()
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("golib-client-transaction-offset-seed"),
	)
	if err != nil {
		t.Fatalf("construct transaction offset seed client: %v", err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(
		context.Background(),
		benchmarkTransactionOperationTimeout,
	)
	defer cancel()
	admin := kadm.NewClient(client)
	endOffsets, err := admin.ListEndOffsets(ctx, topic)
	if err != nil {
		t.Fatalf("list transaction source end offset: %v", err)
	}
	if err := endOffsets.Error(); err != nil {
		t.Fatalf("transaction source end offset: %v", err)
	}
	if len(endOffsets[topic]) != 1 {
		t.Fatalf(
			"transaction source end offset count = %d, want 1",
			len(endOffsets[topic]),
		)
	}
	responses, err := admin.CommitOffsets(
		ctx,
		groupID,
		endOffsets.Offsets(),
	)
	if err != nil {
		t.Fatalf("seed transaction group offsets: %v", err)
	}
	if err := responses.Error(); err != nil {
		t.Fatalf("transaction group offset response: %v", err)
	}
}

func assertBenchmarkSinglePartitionOffset(
	t testing.TB,
	brokers []string,
	groupID string,
	topic string,
	want int64,
) {
	t.Helper()
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("golib-client-transaction-offset-inspector"),
	)
	if err != nil {
		t.Fatalf("construct transaction offset inspector: %v", err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(
		context.Background(),
		benchmarkTransactionOperationTimeout,
	)
	defer cancel()
	admin := kadm.NewClient(client)
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	var lastOffset int64
	var lastOffsetExists bool
	for {
		offsets, err := admin.FetchOffsets(kadm.RequireStable(ctx), groupID)
		if err != nil {
			t.Fatalf("fetch transaction group offset: %v", err)
		}
		offset, exists := offsets.Lookup(topic, 0)
		if exists && offset.Err != nil {
			t.Fatalf("transaction offset error: %v", offset.Err)
		}
		if exists && offset.At == want {
			return
		}
		if exists && offset.At > want {
			t.Fatalf("transaction offset = %d, want %d", offset.At, want)
		}
		lastOffsetExists = exists
		lastOffset = offset.At
		select {
		case <-ctx.Done():
			t.Fatalf(
				"wait for transaction offset %s[0] = %d: %v; "+
					"last exists = %t; last offset = %d",
				topic,
				want,
				ctx.Err(),
				lastOffsetExists,
				lastOffset,
			)
		case <-poll.C:
		}
	}
}

func assertTransactionalBenchmarkRecords(
	t testing.TB,
	brokers []string,
	topic string,
	isolation kgo.IsolationLevel,
	want []observedBenchmarkRecord,
) {
	t.Helper()
	got := readTransactionalBenchmarkRecords(
		t,
		brokers,
		topic,
		isolation,
		len(want),
	)
	if !slices.Equal(got, want) {
		t.Fatalf("transaction output records = %#v, want %#v", got, want)
	}
}

type policyBenchmarkTransactionProcessor struct {
	processor   *policy.TransactionProcessor
	outputTopic string
}

func newPolicyBenchmarkTransactionProcessor(
	t testing.TB,
	brokers []string,
	sourceTopic string,
	outputTopic string,
	groupID string,
	transactionalID string,
	maxPollRecords int,
	compression benchmarkCompression,
) benchmarkTransactionProcessor {
	t.Helper()
	codec := policy.CompressionNone
	if compression == compressionSnappy {
		codec = policy.CompressionSnappy
	}
	processor, err := policy.NewTransactionProcessor(
		policy.TransactionProcessorConfig{
			Connection: policy.TransactionConnectionConfig{
				Brokers:     brokers,
				ClientID:    transactionalID,
				DialTimeout: benchmarkRequestTimeout,
				Security:    policy.DevelopmentPlaintextSecurity(),
			},
			Group: policy.TransactionGroupConfig{
				GroupID:                groupID,
				Topics:                 []string{sourceTopic},
				ResetOffset:            policy.OffsetEarliest,
				BalancePolicy:          policy.BalanceCooperativeSticky,
				MaxPollRecords:         maxPollRecords,
				MaxConcurrentFetches:   1,
				FetchMaxBytes:          1 << 20,
				FetchMaxPartitionBytes: 1 << 20,
				FetchMaxWait:           50 * time.Millisecond,
				SessionTimeout:         10 * time.Second,
				RebalanceTimeout:       30 * time.Second,
				HeartbeatInterval:      time.Second,
				ProcessingTimeout:      10 * time.Second,
			},
			Output: policy.TransactionOutputConfig{
				AllowedTopics:          []string{outputTopic},
				KeyPolicy:              policy.KeyRequired,
				MaxBufferedRecords:     1_000,
				MaxBufferedBytes:       64 << 20,
				MaxBatchBytes:          benchmarkBatchBytes,
				MaxOutputRecords:       maxPollRecords,
				MaxOutputBytes:         64 << 20,
				RecordRetries:          benchmarkRecordRetries,
				RetryBackoffMin:        benchmarkRetryMin,
				RetryBackoffMax:        benchmarkRetryMax,
				DeliveryTimeout:        benchmarkDeliveryTimeout,
				RequestTimeout:         benchmarkRequestTimeout,
				Linger:                 benchmarkLinger,
				CompressionPreferences: []policy.CompressionCodec{codec},
				TransactionalID:        transactionalID,
				TransactionTimeout:     30 * time.Second,
				TransactionEndTimeout:  10 * time.Second,
			},
			Limits:          policy.DefaultMessageLimits(),
			ShutdownTimeout: benchmarkTransactionOperationTimeout,
		},
	)
	if err != nil {
		t.Fatalf("construct policy transaction processor: %v", err)
	}

	return &policyBenchmarkTransactionProcessor{
		processor:   processor,
		outputTopic: outputTopic,
	}
}

func (processor *policyBenchmarkTransactionProcessor) Process(
	ctx context.Context,
	recordCount int,
	failAfter int,
) (int, int, error) {
	processed := 0
	transactions := 0
	handled := 0
	for processed < recordCount {
		result, err := processor.processor.RunOnce(
			ctx,
			policy.TransactionHandlerFunc(func(
				ctx context.Context,
				record policy.ConsumedRecord,
				transaction policy.Transaction,
			) error {
				output := transformBenchmarkRecord(
					benchmarkRecordFromPolicy(record),
				)
				if err := transaction.Publish(ctx, policy.ProducerRecord{
					Topic: processor.outputTopic,
					Key:   output.key,
					Value: output.value,
				}); err != nil {
					return err
				}
				handled++
				if failAfter > 0 && handled == failAfter {
					return errAbortBenchmarkTransaction
				}

				return nil
			}),
		)
		if result.Polled != 0 {
			transactions++
		}
		if err != nil {
			return processed + result.Processed, transactions, err
		}
		if result.Polled == 0 {
			continue
		}
		if !result.Committed ||
			result.Polled != result.Processed ||
			result.Polled != result.Published ||
			processed+result.Processed > recordCount {
			return processed, transactions, fmt.Errorf(
				"policy transaction result = %#v after %d records",
				result,
				processed,
			)
		}
		processed += result.Processed
	}

	return processed, transactions, nil
}

func (processor *policyBenchmarkTransactionProcessor) Close(
	ctx context.Context,
) error {
	return processor.processor.Shutdown(ctx)
}

type franzBenchmarkTransactionProcessor struct {
	session     *kgo.GroupTransactSession
	outputTopic string
}

func newFranzBenchmarkTransactionProcessor(
	t testing.TB,
	brokers []string,
	sourceTopic string,
	outputTopic string,
	groupID string,
	transactionalID string,
	maxPollRecords int,
	compression benchmarkCompression,
) benchmarkTransactionProcessor {
	t.Helper()
	codec := kgo.NoCompression()
	if compression == compressionSnappy {
		codec = kgo.SnappyCompression()
	}
	session, err := kgo.NewGroupTransactSession(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID(transactionalID),
		kgo.ConsumerGroup(groupID),
		kgo.ConsumeTopics(sourceTopic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.DisableAutoCommit(),
		kgo.FetchIsolationLevel(kgo.ReadCommitted()),
		kgo.Balancers(kgo.CooperativeStickyBalancer()),
		kgo.MaxConcurrentFetches(1),
		kgo.FetchMaxBytes(1<<20),
		kgo.FetchMaxPartitionBytes(1<<20),
		kgo.FetchMaxWait(50*time.Millisecond),
		kgo.SessionTimeout(10*time.Second),
		kgo.RebalanceTimeout(30*time.Second),
		kgo.HeartbeatInterval(time.Second),
		kgo.RecordPartitioner(
			kgo.UniformBytesPartitioner(65_536, true, true, nil),
		),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.StopProducerOnDataLossDetected(),
		kgo.MaxBufferedRecords(1_000),
		kgo.MaxBufferedBytes(64<<20),
		kgo.ProducerBatchMaxBytes(benchmarkBatchBytes),
		kgo.RecordRetries(benchmarkRecordRetries),
		kgo.RetryBackoffFn(func(int) time.Duration {
			return benchmarkRetryMin
		}),
		kgo.MetadataMinAge(benchmarkRetryMin),
		kgo.RecordDeliveryTimeout(benchmarkDeliveryTimeout),
		kgo.ProduceRequestTimeout(benchmarkRequestTimeout),
		kgo.ProducerLinger(benchmarkLinger),
		kgo.ProducerBatchCompression(codec),
		kgo.TransactionalID(transactionalID),
		kgo.TransactionTimeout(30*time.Second),
		kgo.DialTimeout(benchmarkRequestTimeout),
	)
	if err != nil {
		t.Fatalf("construct raw franz-go transaction processor: %v", err)
	}

	return &franzBenchmarkTransactionProcessor{
		session:     session,
		outputTopic: outputTopic,
	}
}

func (processor *franzBenchmarkTransactionProcessor) Process(
	ctx context.Context,
	recordCount int,
	failAfter int,
) (int, int, error) {
	processed := 0
	transactions := 0
	handled := 0
	for processed < recordCount {
		fetches := processor.session.PollRecords(ctx, recordCount-processed)
		if err := fetches.Err(); err != nil {
			return processed, transactions, err
		}
		records := fetches.Records()
		if len(records) == 0 {
			continue
		}
		if processed+len(records) > recordCount {
			return processed, transactions, fmt.Errorf(
				"raw franz-go transaction polled %d after %d records",
				len(records),
				processed,
			)
		}
		transactions++
		if err := processor.session.Begin(); err != nil {
			return processed, transactions, err
		}
		for _, record := range records {
			output := transformBenchmarkRecord(
				benchmarkRecordFromFranz(record),
			)
			err := processor.session.ProduceSync(ctx, &kgo.Record{
				Topic: processor.outputTopic,
				Key:   output.key,
				Value: output.value,
			}).FirstErr()
			if err != nil {
				_, abortErr := processor.session.End(ctx, kgo.TryAbort)

				return processed, transactions, errors.Join(err, abortErr)
			}
			handled++
			if failAfter > 0 && handled == failAfter {
				_, abortErr := processor.session.End(ctx, kgo.TryAbort)

				return processed, transactions, errors.Join(
					errAbortBenchmarkTransaction,
					abortErr,
				)
			}
		}
		committed, err := processor.session.End(ctx, kgo.TryCommit)
		if err != nil {
			return processed, transactions, err
		}
		if !committed {
			return processed, transactions, errors.New(
				"raw franz-go transaction was not committed",
			)
		}
		processed += len(records)
	}

	return processed, transactions, nil
}

func (processor *franzBenchmarkTransactionProcessor) Close(
	ctx context.Context,
) error {
	err := processor.session.Client().LeaveGroupContext(ctx)
	processor.session.Close()

	return err
}
