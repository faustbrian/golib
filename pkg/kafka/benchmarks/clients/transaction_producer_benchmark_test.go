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

	"github.com/IBM/sarama"
	policy "github.com/faustbrian/golib/pkg/kafka"
	"github.com/twmb/franz-go/pkg/kgo"
)

const benchmarkTransactionOperationTimeout = 60 * time.Second

type benchmarkTransactionalProducer interface {
	Commit(context.Context, []benchmarkProducerRecord) error
	Abort(context.Context, []benchmarkProducerRecord) error
	Close(context.Context) error
}

type transactionalProducerCandidate struct {
	name string
	new  func(
		testing.TB,
		[]string,
		string,
		string,
		benchmarkCompression,
	) benchmarkTransactionalProducer
}

var transactionalProducerCandidates = []transactionalProducerCandidate{
	{name: "golib-policy", new: newPolicyTransactionalProducer},
	{name: "raw-franz-go", new: newFranzTransactionalProducer},
	{name: "sarama", new: newSaramaTransactionalProducer},
}

type transactionalProducerTopicKey struct {
	payloadBytes int
	compression  benchmarkCompression
}

var (
	transactionalProducerTopicsMu sync.Mutex
	transactionalProducerTopics   = make(
		map[transactionalProducerTopicKey]string,
	)
)

func BenchmarkEquivalentTransactionalProduce(benchmark *testing.B) {
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
					benchmarkEquivalentTransactionalProduce(
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

func benchmarkEquivalentTransactionalProduce(
	benchmark *testing.B,
	brokers []string,
	payloadBytes int,
	recordCount int,
	compression benchmarkCompression,
) {
	benchmark.Helper()
	topic := transactionalProducerBenchmarkTopic(
		benchmark,
		brokers,
		payloadBytes,
		compression,
	)
	records := transactionalBenchmarkRecords(recordCount, payloadBytes)
	for _, candidate := range transactionalProducerCandidates {
		benchmark.Run(candidate.name, func(benchmark *testing.B) {
			transactionalID := benchmarkTransactionalID(candidate.name)
			producer := candidate.new(
				benchmark,
				brokers,
				topic,
				transactionalID,
				compression,
			)
			benchmark.Cleanup(func() {
				ctx, cancel := context.WithTimeout(
					context.Background(),
					benchmarkTransactionOperationTimeout,
				)
				defer cancel()
				if err := producer.Close(ctx); err != nil {
					benchmark.Errorf(
						"close %s transactional producer: %v",
						candidate.name,
						err,
					)
				}
			})

			ctx, cancel := context.WithTimeout(
				context.Background(),
				benchmarkTransactionOperationTimeout,
			)
			err := producer.Commit(ctx, records)
			cancel()
			if err != nil {
				benchmark.Fatalf(
					"warm %s transactional producer: %v",
					candidate.name,
					err,
				)
			}

			benchmark.ReportAllocs()
			benchmark.SetBytes(transactionalBenchmarkRecordBytes(records))
			benchmark.ResetTimer()
			for benchmark.Loop() {
				ctx, cancel = context.WithTimeout(
					context.Background(),
					benchmarkTransactionOperationTimeout,
				)
				err = producer.Commit(ctx, records)
				cancel()
				if err != nil {
					benchmark.Fatalf(
						"commit %s transaction: %v",
						candidate.name,
						err,
					)
				}
			}
			benchmark.ReportMetric(float64(recordCount), "records/op")
			benchmark.ReportMetric(1, "transactions/op")
		})
	}
}

func TestEquivalentTransactionalProducerOutcomes(t *testing.T) {
	brokers := benchmarkBrokers(t)
	committedRecords := []benchmarkProducerRecord{
		{key: []byte("committed-0"), value: []byte("committed-value-0")},
		{key: []byte("committed-1"), value: []byte("committed-value-1")},
	}
	abortedRecords := []benchmarkProducerRecord{
		{key: []byte("aborted-0"), value: []byte("aborted-value-0")},
	}
	for _, candidate := range transactionalProducerCandidates {
		t.Run(candidate.name, func(t *testing.T) {
			topic := createIsolatedBenchmarkTopic(t, brokers)
			producer := candidate.new(
				t,
				brokers,
				topic,
				benchmarkTransactionalID(candidate.name),
				compressionSnappy,
			)
			ctx, cancel := context.WithTimeout(
				context.Background(),
				benchmarkTransactionOperationTimeout,
			)
			defer cancel()
			if err := producer.Commit(ctx, committedRecords); err != nil {
				t.Fatalf("commit transaction: %v", err)
			}
			if err := producer.Abort(ctx, abortedRecords); err != nil {
				t.Fatalf("abort transaction: %v", err)
			}
			if err := producer.Close(ctx); err != nil {
				t.Fatalf("close transactional producer: %v", err)
			}

			committed := readTransactionalBenchmarkRecords(
				t,
				brokers,
				topic,
				kgo.ReadCommitted(),
				len(committedRecords),
			)
			if want := benchmarkObservedProducerRecords(
				committedRecords,
			); !slices.Equal(committed, want) {
				t.Fatalf(
					"read-committed records = %#v, want %#v",
					committed,
					want,
				)
			}
			uncommitted := readTransactionalBenchmarkRecords(
				t,
				brokers,
				topic,
				kgo.ReadUncommitted(),
				len(committedRecords)+len(abortedRecords),
			)
			wantUncommitted := append(
				benchmarkObservedProducerRecords(committedRecords),
				benchmarkObservedProducerRecords(abortedRecords)...,
			)
			if !slices.Equal(uncommitted, wantUncommitted) {
				t.Fatalf(
					"read-uncommitted records = %#v, want %#v",
					uncommitted,
					wantUncommitted,
				)
			}
		})
	}
}

func TestTransactionalBenchmarkRecordBytes(t *testing.T) {
	records := []benchmarkProducerRecord{
		{key: []byte("a"), value: []byte("bc")},
		{key: []byte("def"), value: []byte("ghij")},
	}
	if got, want := transactionalBenchmarkRecordBytes(records), int64(10); got != want {
		t.Fatalf("transactional record bytes = %d, want %d", got, want)
	}
}

func transactionalBenchmarkRecords(
	recordCount int,
	payloadBytes int,
) []benchmarkProducerRecord {
	records := make([]benchmarkProducerRecord, recordCount)
	for index := range records {
		value := make([]byte, payloadBytes)
		for valueIndex := range value {
			value[valueIndex] = byte((index + valueIndex) % 251)
		}
		records[index] = benchmarkProducerRecord{
			key: []byte(fmt.Sprintf(
				"transaction-key-%010d",
				index,
			)),
			value: value,
		}
	}

	return records
}

func transactionalBenchmarkRecordBytes(
	records []benchmarkProducerRecord,
) int64 {
	var total int64
	for _, record := range records {
		total += int64(len(record.key) + len(record.value))
	}

	return total
}

func benchmarkTransactionalID(candidate string) string {
	return fmt.Sprintf(
		"golib-client-transaction-%s-%d",
		candidate,
		time.Now().UnixNano(),
	)
}

func transactionalProducerBenchmarkTopic(
	t testing.TB,
	brokers []string,
	payloadBytes int,
	compression benchmarkCompression,
) string {
	t.Helper()
	key := transactionalProducerTopicKey{
		payloadBytes: payloadBytes,
		compression:  compression,
	}
	transactionalProducerTopicsMu.Lock()
	defer transactionalProducerTopicsMu.Unlock()
	if topic := transactionalProducerTopics[key]; topic != "" {
		return topic
	}
	topic := createIsolatedBenchmarkTopic(t, brokers)
	transactionalProducerTopics[key] = topic

	return topic
}

func benchmarkObservedProducerRecords(
	records []benchmarkProducerRecord,
) []observedBenchmarkRecord {
	observed := make([]observedBenchmarkRecord, len(records))
	for index, record := range records {
		observed[index] = observedBenchmarkRecord{
			key:   string(record.key),
			value: string(record.value),
		}
	}

	return observed
}

func readTransactionalBenchmarkRecords(
	t testing.TB,
	brokers []string,
	topic string,
	isolation kgo.IsolationLevel,
	count int,
) []observedBenchmarkRecord {
	t.Helper()
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("golib-client-transaction-verifier"),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{
			topic: {0: kgo.NewOffset().AtStart()},
		}),
		kgo.FetchIsolationLevel(isolation),
		kgo.FetchMaxWait(50*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("construct transactional verifier: %v", err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(
		context.Background(),
		benchmarkTransactionOperationTimeout,
	)
	defer cancel()
	observed := make([]observedBenchmarkRecord, 0, count)
	for len(observed) < count {
		fetches := client.PollRecords(ctx, count-len(observed))
		if err := fetches.Err(); err != nil {
			t.Fatalf("read transactional records: %v", err)
		}
		for _, record := range fetches.Records() {
			observed = append(observed, observedBenchmarkRecord{
				key:   string(record.Key),
				value: string(record.Value),
			})
		}
	}

	return observed
}

type policyTransactionalProducer struct {
	producer *policy.Producer
	topic    string
}

func newPolicyTransactionalProducer(
	t testing.TB,
	brokers []string,
	topic string,
	transactionalID string,
	compression benchmarkCompression,
) benchmarkTransactionalProducer {
	t.Helper()
	codec := policy.CompressionNone
	if compression == compressionSnappy {
		codec = policy.CompressionSnappy
	}
	producer, err := policy.NewProducer(policy.ProducerConfig{
		Brokers:                brokers,
		ClientID:               transactionalID,
		AllowedTopics:          []string{topic},
		KeyPolicy:              policy.KeyRequired,
		MaxBufferedRecords:     1_000,
		MaxBufferedBytes:       64 << 20,
		MaxBatchRecords:        10,
		MaxBatchBytes:          benchmarkBatchBytes,
		RecordRetries:          benchmarkRecordRetries,
		RetryBackoffMin:        benchmarkRetryMin,
		RetryBackoffMax:        benchmarkRetryMax,
		DeliveryTimeout:        benchmarkDeliveryTimeout,
		ShutdownTimeout:        benchmarkTransactionOperationTimeout,
		RequestTimeout:         benchmarkRequestTimeout,
		DialTimeout:            benchmarkRequestTimeout,
		Linger:                 benchmarkLinger,
		CompressionPreferences: []policy.CompressionCodec{codec},
		TransactionalID:        transactionalID,
		TransactionTimeout:     30 * time.Second,
		TransactionEndTimeout:  30 * time.Second,
		Security:               policy.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct policy transactional producer: %v", err)
	}

	return &policyTransactionalProducer{
		producer: producer,
		topic:    topic,
	}
}

func (producer *policyTransactionalProducer) Commit(
	ctx context.Context,
	records []benchmarkProducerRecord,
) error {
	return producer.run(ctx, records, true)
}

func (producer *policyTransactionalProducer) Abort(
	ctx context.Context,
	records []benchmarkProducerRecord,
) error {
	return producer.run(ctx, records, false)
}

func (producer *policyTransactionalProducer) run(
	ctx context.Context,
	records []benchmarkProducerRecord,
	commit bool,
) error {
	abortCause := errors.New("abort policy benchmark transaction")
	err := producer.producer.RunTransaction(
		ctx,
		func(transaction policy.Transaction) error {
			for _, record := range records {
				if err := transaction.Publish(ctx, policy.ProducerRecord{
					Topic: producer.topic,
					Key:   record.key,
					Value: record.value,
				}); err != nil {
					return err
				}
			}
			if !commit {
				return abortCause
			}

			return nil
		},
	)
	if !commit && errors.Is(err, abortCause) {
		return nil
	}

	return err
}

func (producer *policyTransactionalProducer) Close(ctx context.Context) error {
	return producer.producer.Shutdown(ctx)
}

type franzTransactionalProducer struct {
	client *kgo.Client
	topic  string
}

func newFranzTransactionalProducer(
	t testing.TB,
	brokers []string,
	topic string,
	transactionalID string,
	compression benchmarkCompression,
) benchmarkTransactionalProducer {
	t.Helper()
	codec := kgo.NoCompression()
	if compression == compressionSnappy {
		codec = kgo.SnappyCompression()
	}
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID(transactionalID),
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
		kgo.DialTimeout(benchmarkRequestTimeout),
		kgo.ProducerLinger(benchmarkLinger),
		kgo.ProducerBatchCompression(codec),
		kgo.TransactionalID(transactionalID),
		kgo.TransactionTimeout(30*time.Second),
	)
	if err != nil {
		t.Fatalf("construct raw franz-go transactional producer: %v", err)
	}

	return &franzTransactionalProducer{
		client: client,
		topic:  topic,
	}
}

func (producer *franzTransactionalProducer) Commit(
	ctx context.Context,
	records []benchmarkProducerRecord,
) error {
	if err := producer.beginAndProduce(ctx, records); err != nil {
		return err
	}

	return producer.client.EndTransaction(ctx, kgo.TryCommit)
}

func (producer *franzTransactionalProducer) Abort(
	ctx context.Context,
	records []benchmarkProducerRecord,
) error {
	if err := producer.beginAndProduce(ctx, records); err != nil {
		return err
	}

	return producer.client.EndTransaction(ctx, kgo.TryAbort)
}

func (producer *franzTransactionalProducer) beginAndProduce(
	ctx context.Context,
	records []benchmarkProducerRecord,
) error {
	if err := producer.client.BeginTransaction(); err != nil {
		return err
	}
	for _, record := range records {
		err := producer.client.ProduceSync(ctx, &kgo.Record{
			Topic: producer.topic,
			Key:   record.key,
			Value: record.value,
		}).FirstErr()
		if err != nil {
			return errors.Join(
				err,
				producer.client.EndTransaction(ctx, kgo.TryAbort),
			)
		}
	}

	return nil
}

func (producer *franzTransactionalProducer) Close(context.Context) error {
	producer.client.Close()

	return nil
}

type saramaTransactionalProducer struct {
	producer sarama.SyncProducer
	topic    string
}

func newSaramaTransactionalProducer(
	t testing.TB,
	brokers []string,
	topic string,
	transactionalID string,
	compression benchmarkCompression,
) benchmarkTransactionalProducer {
	t.Helper()
	config := newSaramaProducerConfig(true, compression)
	config.ClientID = transactionalID
	config.Producer.Transaction.ID = transactionalID
	config.Producer.Transaction.Timeout = 30 * time.Second
	config.Producer.Transaction.Retry.Max = benchmarkRecordRetries
	config.Producer.Transaction.Retry.Backoff = benchmarkRetryMin
	producer, err := sarama.NewSyncProducer(brokers, config)
	if err != nil {
		t.Fatalf("construct Sarama transactional producer: %v", err)
	}
	if !producer.IsTransactional() {
		t.Fatal("Sarama transactional producer is not transactional")
	}

	return &saramaTransactionalProducer{
		producer: producer,
		topic:    topic,
	}
}

func (producer *saramaTransactionalProducer) Commit(
	ctx context.Context,
	records []benchmarkProducerRecord,
) error {
	if err := producer.beginAndProduce(ctx, records); err != nil {
		return err
	}

	return producer.producer.CommitTxn()
}

func (producer *saramaTransactionalProducer) Abort(
	ctx context.Context,
	records []benchmarkProducerRecord,
) error {
	if err := producer.beginAndProduce(ctx, records); err != nil {
		return err
	}

	return producer.producer.AbortTxn()
}

func (producer *saramaTransactionalProducer) beginAndProduce(
	ctx context.Context,
	records []benchmarkProducerRecord,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := producer.producer.BeginTxn(); err != nil {
		return err
	}
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return errors.Join(err, producer.producer.AbortTxn())
		}
		_, _, err := producer.producer.SendMessage(&sarama.ProducerMessage{
			Topic: producer.topic,
			Key:   sarama.ByteEncoder(record.key),
			Value: sarama.ByteEncoder(record.value),
		})
		if err != nil {
			return errors.Join(err, producer.producer.AbortTxn())
		}
	}

	return nil
}

func (producer *saramaTransactionalProducer) Close(context.Context) error {
	return producer.producer.Close()
}
