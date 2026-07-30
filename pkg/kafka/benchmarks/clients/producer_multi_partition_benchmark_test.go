//go:build integration

package clients_test

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/IBM/sarama"
	policy "github.com/faustbrian/golib/pkg/kafka"
	"github.com/twmb/franz-go/pkg/kgo"
)

const benchmarkMultiPartitionCount = 8

type multiPartitionMode uint8

const (
	multiPartitionKeyed multiPartitionMode = iota
	multiPartitionExplicit
)

func (mode multiPartitionMode) String() string {
	if mode == multiPartitionExplicit {
		return "explicit"
	}

	return "keyed"
}

type multiPartitionRecord struct {
	partition int32
	key       []byte
	value     []byte
}

type observedBenchmarkRecord struct {
	key   string
	value string
}

type multiPartitionProducer interface {
	ProduceBatch(context.Context, []multiPartitionRecord) error
	Close(context.Context) error
}

type multiPartitionProducerCandidate struct {
	name string
	new  func(
		testing.TB,
		[]string,
		string,
		multiPartitionMode,
		benchmarkCompression,
	) multiPartitionProducer
}

var multiPartitionProducerCandidates = []multiPartitionProducerCandidate{
	{name: "golib-policy", new: newPolicyMultiPartitionProducer},
	{name: "raw-franz-go", new: newFranzMultiPartitionProducer},
	{name: "sarama", new: newSaramaMultiPartitionProducer},
}

var (
	benchmarkMultiPartitionTopicOnce sync.Once
	benchmarkMultiPartitionTopic     string
	benchmarkMultiPartitionTopicErr  error
)

func BenchmarkEquivalentMultiPartitionProduce(benchmark *testing.B) {
	brokers := benchmarkBrokers(benchmark)
	for _, mode := range []multiPartitionMode{
		multiPartitionKeyed,
		multiPartitionExplicit,
	} {
		for _, size := range []int{128, 1024} {
			for _, compression := range []benchmarkCompression{
				compressionNone,
				compressionSnappy,
			} {
				name := fmt.Sprintf("%s/%dB/%s", mode, size, compression)
				benchmark.Run(name, func(benchmark *testing.B) {
					benchmarkEquivalentMultiPartitionProduce(
						benchmark,
						brokers,
						mode,
						size,
						compression,
					)
				})
			}
		}
	}
}

func benchmarkEquivalentMultiPartitionProduce(
	benchmark *testing.B,
	brokers []string,
	mode multiPartitionMode,
	payloadBytes int,
	compression benchmarkCompression,
) {
	benchmark.Helper()
	const recordsPerPartition = 10
	records, _ := benchmarkMultiPartitionRecords(
		benchmark,
		mode,
		recordsPerPartition,
		payloadBytes,
	)
	for _, candidate := range multiPartitionProducerCandidates {
		benchmark.Run(candidate.name, func(benchmark *testing.B) {
			topic := createMultiPartitionBenchmarkTopic(benchmark, brokers)
			producer := candidate.new(
				benchmark,
				brokers,
				topic,
				mode,
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
						"close %s %s producer: %v",
						candidate.name,
						mode,
						err,
					)
				}
			})

			warmupCtx, warmupCancel := context.WithTimeout(
				context.Background(),
				benchmarkDeliveryTimeout+benchmarkRetryMax,
			)
			if err := producer.ProduceBatch(warmupCtx, records); err != nil {
				warmupCancel()
				benchmark.Fatalf(
					"warm %s %s producer: %v",
					candidate.name,
					mode,
					err,
				)
			}
			warmupCancel()

			benchmark.ReportAllocs()
			benchmark.SetBytes(benchmarkMultiPartitionRecordBytes(records))
			benchmark.ResetTimer()
			for benchmark.Loop() {
				ctx, cancel := context.WithTimeout(
					context.Background(),
					benchmarkDeliveryTimeout+benchmarkRetryMax,
				)
				err := producer.ProduceBatch(ctx, records)
				cancel()
				if err != nil {
					benchmark.Fatalf(
						"produce %s batch with %s: %v",
						mode,
						candidate.name,
						err,
					)
				}
			}
			benchmark.StopTimer()
			benchmark.ReportMetric(float64(len(records)), "records/op")
			benchmark.ReportMetric(benchmarkMultiPartitionCount, "partitions/op")
		})
	}
}

func TestEquivalentMultiPartitionProducerOutcomes(t *testing.T) {
	brokers := benchmarkBrokers(t)
	for _, mode := range []multiPartitionMode{
		multiPartitionKeyed,
		multiPartitionExplicit,
	} {
		t.Run(mode.String(), func(t *testing.T) {
			for _, candidate := range multiPartitionProducerCandidates {
				t.Run(candidate.name, func(t *testing.T) {
					topic := createIsolatedBenchmarkTopicWithPartitions(
						t,
						brokers,
						benchmarkMultiPartitionCount,
					)
					records, want := benchmarkMultiPartitionRecords(t, mode, 3, 128)
					producer := candidate.new(
						t,
						brokers,
						topic,
						mode,
						compressionSnappy,
					)
					ctx, cancel := context.WithTimeout(
						context.Background(),
						benchmarkDeliveryTimeout+benchmarkRetryMax,
					)
					err := producer.ProduceBatch(ctx, records)
					cancel()
					if err != nil {
						t.Fatalf("produce %s batch with %s: %v", mode, candidate.name, err)
					}
					closeCtx, closeCancel := context.WithTimeout(
						context.Background(),
						benchmarkDeliveryTimeout+benchmarkRetryMax,
					)
					err = producer.Close(closeCtx)
					closeCancel()
					if err != nil {
						t.Fatalf("close %s %s producer: %v", candidate.name, mode, err)
					}

					got := readBenchmarkPartitionRecords(
						t,
						brokers,
						topic,
						benchmarkMultiPartitionCount,
						len(records),
					)
					for partition := range int32(benchmarkMultiPartitionCount) {
						if !slices.Equal(got[partition], want[partition]) {
							t.Fatalf(
								"partition %d records from %s = %#v, want %#v",
								partition,
								candidate.name,
								got[partition],
								want[partition],
							)
						}
					}
				})
			}
		})
	}
}

func TestBenchmarkMultiPartitionRecordBytes(t *testing.T) {
	t.Parallel()
	records := []multiPartitionRecord{
		{key: []byte("key"), value: []byte("value")},
		{key: []byte("other-key"), value: []byte("other-value")},
	}
	if got, want := benchmarkMultiPartitionRecordBytes(records), int64(28); got != want {
		t.Fatalf("record bytes = %d, want %d", got, want)
	}
}

func benchmarkMultiPartitionRecords(
	t testing.TB,
	mode multiPartitionMode,
	recordsPerPartition int,
	payloadBytes int,
) ([]multiPartitionRecord, map[int32][]observedBenchmarkRecord) {
	t.Helper()
	keys := benchmarkMurmur2PartitionKeys(
		t,
		benchmarkMultiPartitionCount,
		recordsPerPartition,
	)
	records := make([]multiPartitionRecord, 0, benchmarkMultiPartitionCount*recordsPerPartition)
	want := make(map[int32][]observedBenchmarkRecord, benchmarkMultiPartitionCount)
	for partition := range int32(benchmarkMultiPartitionCount) {
		for index := range recordsPerPartition {
			key := keys[partition][index]
			if mode == multiPartitionExplicit {
				key = []byte(fmt.Sprintf("explicit-%d-%d", partition, index))
			}
			value := make([]byte, payloadBytes)
			copy(value, fmt.Sprintf("%s-%d-%d", mode, partition, index))
			records = append(records, multiPartitionRecord{
				partition: partition,
				key:       key,
				value:     value,
			})
			want[partition] = append(want[partition], observedBenchmarkRecord{
				key:   string(key),
				value: string(value),
			})
		}
	}

	return records, want
}

func benchmarkMultiPartitionRecordBytes(records []multiPartitionRecord) int64 {
	var total int64
	for _, record := range records {
		total += int64(len(record.key) + len(record.value))
	}

	return total
}

func benchmarkMurmur2PartitionKeys(
	t testing.TB,
	partitionCount int,
	keysPerPartition int,
) map[int32][][]byte {
	t.Helper()
	partitioner := sarama.NewMurmur2Partitioner("benchmark")
	keys := make(map[int32][][]byte, partitionCount)
	remaining := partitionCount * keysPerPartition
	for candidate := 0; candidate < 1_000_000 && remaining > 0; candidate++ {
		key := []byte(fmt.Sprintf("partition-key-%d", candidate))
		partition, err := partitioner.Partition(
			&sarama.ProducerMessage{Key: sarama.ByteEncoder(key)},
			int32(partitionCount),
		)
		if err != nil {
			t.Fatalf("partition benchmark key: %v", err)
		}
		if len(keys[partition]) == keysPerPartition {
			continue
		}
		keys[partition] = append(keys[partition], key)
		remaining--
	}
	if remaining != 0 {
		t.Fatalf("resolve balanced Murmur2 keys: %d keys missing", remaining)
	}

	return keys
}

type policyMultiPartitionProducer struct {
	producer *policyProducer
	mode     multiPartitionMode
}

func newPolicyMultiPartitionProducer(
	t testing.TB,
	brokers []string,
	topic string,
	mode multiPartitionMode,
	compression benchmarkCompression,
) multiPartitionProducer {
	t.Helper()

	return &policyMultiPartitionProducer{
		producer: newPolicyProducer(
			t,
			brokers,
			topic,
			true,
			compression,
		).(*policyProducer),
		mode: mode,
	}
}

func (producer *policyMultiPartitionProducer) ProduceBatch(
	ctx context.Context,
	records []multiPartitionRecord,
) error {
	policyRecords := make([]policy.ProducerRecord, len(records))
	for index, record := range records {
		policyRecords[index] = policy.ProducerRecord{
			Topic: producer.producer.topic,
			Key:   record.key,
			Value: record.value,
		}
		if producer.mode == multiPartitionExplicit {
			policyRecords[index].Partition = policy.ExplicitPartition(record.partition)
		}
	}
	_, err := producer.producer.producer.PublishBatch(ctx, policyRecords)

	return err
}

func (producer *policyMultiPartitionProducer) Close(ctx context.Context) error {
	return producer.producer.Close(ctx)
}

type franzMultiPartitionProducer struct {
	producer *franzProducer
	mode     multiPartitionMode
}

func newFranzMultiPartitionProducer(
	t testing.TB,
	brokers []string,
	topic string,
	mode multiPartitionMode,
	compression benchmarkCompression,
) multiPartitionProducer {
	t.Helper()
	partitioner := kgo.Partitioner(
		kgo.UniformBytesPartitioner(65_536, true, true, nil),
	)
	if mode == multiPartitionExplicit {
		partitioner = kgo.ManualPartitioner()
	}

	return &franzMultiPartitionProducer{
		producer: newFranzProducerWithPartitioner(
			t,
			brokers,
			topic,
			compression,
			partitioner,
		),
		mode: mode,
	}
}

func (producer *franzMultiPartitionProducer) ProduceBatch(
	ctx context.Context,
	records []multiPartitionRecord,
) error {
	franzRecords := make([]*kgo.Record, len(records))
	for index, record := range records {
		franzRecords[index] = &kgo.Record{
			Topic: producer.producer.topic,
			Key:   record.key,
			Value: record.value,
		}
		if producer.mode == multiPartitionExplicit {
			franzRecords[index].Partition = record.partition
		}
	}

	return producer.producer.client.ProduceSync(ctx, franzRecords...).FirstErr()
}

func (producer *franzMultiPartitionProducer) Close(ctx context.Context) error {
	return producer.producer.Close(ctx)
}

type saramaMultiPartitionProducer struct {
	producer *saramaProducer
	mode     multiPartitionMode
}

func newSaramaMultiPartitionProducer(
	t testing.TB,
	brokers []string,
	topic string,
	mode multiPartitionMode,
	compression benchmarkCompression,
) multiPartitionProducer {
	t.Helper()
	config := newSaramaProducerConfig(true, compression)
	if mode == multiPartitionExplicit {
		config.Producer.Partitioner = sarama.NewManualPartitioner
	}
	producer, err := sarama.NewSyncProducer(brokers, config)
	if err != nil {
		t.Fatalf("construct multi-partition Sarama producer: %v", err)
	}

	return &saramaMultiPartitionProducer{
		producer: &saramaProducer{producer: producer, topic: topic},
		mode:     mode,
	}
}

func (producer *saramaMultiPartitionProducer) ProduceBatch(
	_ context.Context,
	records []multiPartitionRecord,
) error {
	messages := make([]*sarama.ProducerMessage, len(records))
	for index, record := range records {
		messages[index] = &sarama.ProducerMessage{
			Topic: producer.producer.topic,
			Key:   sarama.ByteEncoder(record.key),
			Value: sarama.ByteEncoder(record.value),
		}
		if producer.mode == multiPartitionExplicit {
			messages[index].Partition = record.partition
		}
	}

	return producer.producer.producer.SendMessages(messages)
}

func (producer *saramaMultiPartitionProducer) Close(ctx context.Context) error {
	return producer.producer.Close(ctx)
}

func createMultiPartitionBenchmarkTopic(
	tb testing.TB,
	brokers []string,
) string {
	tb.Helper()
	benchmarkMultiPartitionTopicOnce.Do(func() {
		benchmarkMultiPartitionTopic, benchmarkMultiPartitionTopicErr =
			createBenchmarkTopicWithPartitionsOnce(
				brokers,
				benchmarkMultiPartitionCount,
			)
	})
	if benchmarkMultiPartitionTopicErr != nil {
		tb.Fatalf(
			"create multi-partition benchmark topic: %v",
			benchmarkMultiPartitionTopicErr,
		)
	}

	return benchmarkMultiPartitionTopic
}

func createIsolatedBenchmarkTopicWithPartitions(
	tb testing.TB,
	brokers []string,
	partitionCount int32,
) string {
	tb.Helper()
	topic, err := createBenchmarkTopicWithPartitionsOnce(brokers, partitionCount)
	if err != nil {
		tb.Fatalf("create isolated multi-partition benchmark topic: %v", err)
	}

	return topic
}

func readBenchmarkPartitionRecords(
	t *testing.T,
	brokers []string,
	topic string,
	partitionCount int,
	recordCount int,
) map[int32][]observedBenchmarkRecord {
	t.Helper()
	partitions := make(map[int32]kgo.Offset, partitionCount)
	for partition := range int32(partitionCount) {
		partitions[partition] = kgo.NewOffset().AtStart()
	}
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("golib-client-multi-partition-verifier"),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{topic: partitions}),
		kgo.FetchMaxWait(50*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("construct multi-partition verifier: %v", err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result := make(map[int32][]observedBenchmarkRecord, partitionCount)
	read := 0
	for read < recordCount {
		fetches := client.PollRecords(ctx, recordCount-read)
		if err := fetches.Err(); err != nil {
			t.Fatalf("read multi-partition benchmark records: %v", err)
		}
		for _, record := range fetches.Records() {
			if record.Partition < 0 || record.Partition >= int32(partitionCount) {
				t.Fatalf("unexpected benchmark partition %d", record.Partition)
			}
			result[record.Partition] = append(
				result[record.Partition],
				observedBenchmarkRecord{
					key:   string(record.Key),
					value: string(record.Value),
				},
			)
			read++
		}
	}

	return result
}
