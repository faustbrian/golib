//go:build integration

package clients_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IBM/sarama"
	policy "github.com/faustbrian/golib/pkg/kafka"
	segmentkafka "github.com/segmentio/kafka-go"
	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	benchmarkKafkaImage = "confluentinc/confluent-local:7.5.0@" +
		"sha256:8e391de42cfcd3498e7317dcf159790f1f1cc3f3ffce900b30d7da23888687fd"
	benchmarkKafkaVersion    = "7.5.0-ccs"
	benchmarkBatchBytes      = 1 << 20
	benchmarkDeliveryTimeout = 30 * time.Second
	benchmarkRequestTimeout  = 10 * time.Second
	benchmarkLinger          = 5 * time.Millisecond
	benchmarkRetryMin        = 250 * time.Millisecond
	benchmarkRetryMax        = time.Second
	benchmarkRecordRetries   = 10
)

var (
	benchmarkFixtureOnce    sync.Once
	benchmarkFixture        *tckafka.KafkaContainer
	benchmarkFixtureBrokers []string
	benchmarkFixtureVersion string
	benchmarkFixtureErr     error
	benchmarkRuntimeReport  sync.Once
	benchmarkTopicOnce      sync.Once
	benchmarkTopic          string
	benchmarkTopicErr       error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if benchmarkFixture != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := benchmarkFixture.Terminate(ctx); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "terminate benchmark Kafka fixture: %v\n", err)
			code = 1
		}
		cancel()
	}
	os.Exit(code)
}

type benchmarkCompression uint8

type benchmarkNoopLogger struct{}

func (benchmarkNoopLogger) Printf(string, ...any) {}

const (
	compressionNone benchmarkCompression = iota
	compressionSnappy
)

func (compression benchmarkCompression) String() string {
	if compression == compressionSnappy {
		return "snappy"
	}

	return "none"
}

type synchronousProducer interface {
	Produce(context.Context, []byte, []byte) error
	ProduceBatch(context.Context, []benchmarkProducerRecord) error
	Close(context.Context) error
}

type asynchronousProducer interface {
	ProduceWindow(context.Context, []benchmarkProducerRecord) error
	Close(context.Context) error
}

type benchmarkProducerRecord struct {
	key   []byte
	value []byte
}

type producerCandidate struct {
	name string
	new  func(testing.TB, []string, string, bool, benchmarkCompression) synchronousProducer
}

type asynchronousProducerCandidate struct {
	name string
	new  func(testing.TB, []string, string, bool, benchmarkCompression) asynchronousProducer
}

var producerCandidates = []producerCandidate{
	{name: "golib-policy", new: newPolicyProducer},
	{name: "raw-franz-go", new: newFranzProducer},
	{name: "sarama", new: newSaramaProducer},
}

var producerOutcomeCandidates = append(
	slices.Clone(producerCandidates),
	producerCandidate{
		name: "kafka-go-non-idempotent-control",
		new:  newKafkaGoProducer,
	},
)

var asynchronousProducerCandidates = []asynchronousProducerCandidate{
	{name: "golib-policy", new: newPolicyAsynchronousProducer},
	{name: "raw-franz-go", new: newFranzAsynchronousProducer},
	{name: "sarama", new: newSaramaAsynchronousProducer},
}

var asynchronousProducerOutcomeCandidates = append(
	slices.Clone(asynchronousProducerCandidates),
	asynchronousProducerCandidate{
		name: "kafka-go-non-idempotent-control",
		new:  newKafkaGoAsynchronousProducer,
	},
)

func BenchmarkEquivalentSynchronousProduce(benchmark *testing.B) {
	brokers := benchmarkBrokers(benchmark)
	for _, keyed := range []bool{true, false} {
		keyMode := "unkeyed"
		if keyed {
			keyMode = "keyed"
		}
		for _, size := range []int{128, 1024, 64 << 10} {
			for _, compression := range []benchmarkCompression{
				compressionNone,
				compressionSnappy,
			} {
				name := fmt.Sprintf(
					"%s/%dB/%s",
					keyMode,
					size,
					compression,
				)
				benchmark.Run(name, func(benchmark *testing.B) {
					benchmarkEquivalentSynchronousProduce(
						benchmark,
						brokers,
						keyed,
						size,
						compression,
					)
				})
			}
		}
	}
}

func BenchmarkEquivalentSynchronousBatchProduce(benchmark *testing.B) {
	brokers := benchmarkBrokers(benchmark)
	for _, keyed := range []bool{true, false} {
		keyMode := "unkeyed"
		if keyed {
			keyMode = "keyed"
		}
		for _, size := range []int{128, 1024} {
			for _, recordCount := range []int{10, 100} {
				for _, compression := range []benchmarkCompression{
					compressionNone,
					compressionSnappy,
				} {
					name := fmt.Sprintf(
						"%s/%dB/%d-records/%s",
						keyMode,
						size,
						recordCount,
						compression,
					)
					benchmark.Run(name, func(benchmark *testing.B) {
						benchmarkEquivalentSynchronousBatchProduce(
							benchmark,
							brokers,
							keyed,
							size,
							recordCount,
							compression,
						)
					})
				}
			}
		}
	}
}

func BenchmarkEquivalentAsynchronousProduce(benchmark *testing.B) {
	brokers := benchmarkBrokers(benchmark)
	for _, keyed := range []bool{true, false} {
		keyMode := "unkeyed"
		if keyed {
			keyMode = "keyed"
		}
		for _, size := range []int{128, 1024} {
			for _, windowSize := range []int{10, 100} {
				for _, compression := range []benchmarkCompression{
					compressionNone,
					compressionSnappy,
				} {
					name := fmt.Sprintf(
						"%s/%dB/%d-outstanding/%s",
						keyMode,
						size,
						windowSize,
						compression,
					)
					benchmark.Run(name, func(benchmark *testing.B) {
						benchmarkEquivalentAsynchronousProduce(
							benchmark,
							brokers,
							keyed,
							size,
							windowSize,
							compression,
						)
					})
				}
			}
		}
	}
}

func benchmarkEquivalentSynchronousProduce(
	benchmark *testing.B,
	brokers []string,
	keyed bool,
	size int,
	compression benchmarkCompression,
) {
	benchmark.Helper()
	key := []byte(nil)
	if keyed {
		key = []byte("benchmark-key")
	}
	value := make([]byte, size)
	for index := range value {
		value[index] = byte(index % 251)
	}

	for _, candidate := range producerCandidates {
		benchmark.Run(candidate.name, func(benchmark *testing.B) {
			topic := createBenchmarkTopic(benchmark, brokers)
			producer := candidate.new(
				benchmark,
				brokers,
				topic,
				keyed,
				compression,
			)
			benchmark.Cleanup(func() {
				closeCtx, cancel := context.WithTimeout(
					context.Background(),
					benchmarkDeliveryTimeout+benchmarkRetryMax,
				)
				defer cancel()
				if err := producer.Close(closeCtx); err != nil {
					benchmark.Errorf("close %s producer: %v", candidate.name, err)
				}
			})

			warmupCtx, warmupCancel := context.WithTimeout(
				context.Background(),
				benchmarkDeliveryTimeout+benchmarkRetryMax,
			)
			if err := producer.Produce(warmupCtx, key, value); err != nil {
				warmupCancel()
				benchmark.Fatalf("warm %s producer: %v", candidate.name, err)
			}
			warmupCancel()

			benchmark.ReportAllocs()
			benchmark.SetBytes(int64(len(key) + len(value)))
			benchmark.ResetTimer()
			for benchmark.Loop() {
				ctx, cancel := context.WithTimeout(
					context.Background(),
					benchmarkDeliveryTimeout+benchmarkRetryMax,
				)
				err := producer.Produce(ctx, key, value)
				cancel()
				if err != nil {
					benchmark.Fatalf("produce with %s: %v", candidate.name, err)
				}
			}
			benchmark.StopTimer()
		})
	}
}

func benchmarkEquivalentSynchronousBatchProduce(
	benchmark *testing.B,
	brokers []string,
	keyed bool,
	size int,
	recordCount int,
	compression benchmarkCompression,
) {
	benchmark.Helper()
	key := []byte(nil)
	if keyed {
		key = []byte("benchmark-key")
	}
	value := make([]byte, size)
	for index := range value {
		value[index] = byte(index % 251)
	}
	records := make([]benchmarkProducerRecord, recordCount)
	for index := range records {
		records[index] = benchmarkProducerRecord{key: key, value: value}
	}

	for _, candidate := range producerCandidates {
		benchmark.Run(candidate.name, func(benchmark *testing.B) {
			topic := createBenchmarkTopic(benchmark, brokers)
			producer := candidate.new(
				benchmark,
				brokers,
				topic,
				keyed,
				compression,
			)
			benchmark.Cleanup(func() {
				closeCtx, cancel := context.WithTimeout(
					context.Background(),
					benchmarkDeliveryTimeout+benchmarkRetryMax,
				)
				defer cancel()
				if err := producer.Close(closeCtx); err != nil {
					benchmark.Errorf("close %s producer: %v", candidate.name, err)
				}
			})

			warmupCtx, warmupCancel := context.WithTimeout(
				context.Background(),
				benchmarkDeliveryTimeout+benchmarkRetryMax,
			)
			if err := producer.ProduceBatch(warmupCtx, records); err != nil {
				warmupCancel()
				benchmark.Fatalf("warm %s batch producer: %v", candidate.name, err)
			}
			warmupCancel()

			benchmark.ReportAllocs()
			benchmark.SetBytes(int64(recordCount * (len(key) + len(value))))
			benchmark.ResetTimer()
			for benchmark.Loop() {
				ctx, cancel := context.WithTimeout(
					context.Background(),
					benchmarkDeliveryTimeout+benchmarkRetryMax,
				)
				err := producer.ProduceBatch(ctx, records)
				cancel()
				if err != nil {
					benchmark.Fatalf("produce batch with %s: %v", candidate.name, err)
				}
			}
			benchmark.StopTimer()
			benchmark.ReportMetric(float64(recordCount), "records/op")
		})
	}
}

func benchmarkEquivalentAsynchronousProduce(
	benchmark *testing.B,
	brokers []string,
	keyed bool,
	size int,
	windowSize int,
	compression benchmarkCompression,
) {
	benchmark.Helper()
	key := []byte(nil)
	if keyed {
		key = []byte("benchmark-key")
	}
	value := make([]byte, size)
	for index := range value {
		value[index] = byte(index % 251)
	}
	records := make([]benchmarkProducerRecord, windowSize)
	for index := range records {
		records[index] = benchmarkProducerRecord{key: key, value: value}
	}

	for _, candidate := range asynchronousProducerCandidates {
		benchmark.Run(candidate.name, func(benchmark *testing.B) {
			topic := createBenchmarkTopic(benchmark, brokers)
			producer := candidate.new(
				benchmark,
				brokers,
				topic,
				keyed,
				compression,
			)
			benchmark.Cleanup(func() {
				closeCtx, cancel := context.WithTimeout(
					context.Background(),
					benchmarkDeliveryTimeout+benchmarkRetryMax,
				)
				defer cancel()
				if err := producer.Close(closeCtx); err != nil {
					benchmark.Errorf("close %s asynchronous producer: %v", candidate.name, err)
				}
			})

			warmupCtx, warmupCancel := context.WithTimeout(
				context.Background(),
				benchmarkDeliveryTimeout+benchmarkRetryMax,
			)
			if err := producer.ProduceWindow(warmupCtx, records); err != nil {
				warmupCancel()
				benchmark.Fatalf("warm %s asynchronous producer: %v", candidate.name, err)
			}
			warmupCancel()

			benchmark.ReportAllocs()
			benchmark.SetBytes(int64(windowSize * (len(key) + len(value))))
			benchmark.ResetTimer()
			for benchmark.Loop() {
				ctx, cancel := context.WithTimeout(
					context.Background(),
					benchmarkDeliveryTimeout+benchmarkRetryMax,
				)
				err := producer.ProduceWindow(ctx, records)
				cancel()
				if err != nil {
					benchmark.Fatalf("produce asynchronously with %s: %v", candidate.name, err)
				}
			}
			benchmark.StopTimer()
			benchmark.ReportMetric(float64(windowSize), "records/op")
		})
	}
}

func TestEquivalentProducerOutcomes(t *testing.T) {
	brokers := benchmarkBrokers(t)
	topic := createIsolatedBenchmarkTopic(t, brokers)
	wantValues := make([][]byte, 0, len(producerOutcomeCandidates))
	wantKeys := make([][]byte, 0, len(producerOutcomeCandidates))
	for index, candidate := range producerOutcomeCandidates {
		key := []byte(fmt.Sprintf("candidate-%d", index))
		value := []byte(candidate.name)
		producer := candidate.new(t, brokers, topic, true, compressionSnappy)
		ctx, cancel := context.WithTimeout(
			context.Background(),
			benchmarkDeliveryTimeout+benchmarkRetryMax,
		)
		err := producer.Produce(ctx, key, value)
		cancel()
		if err != nil {
			t.Fatalf("produce with %s: %v", candidate.name, err)
		}
		closeCtx, closeCancel := context.WithTimeout(
			context.Background(),
			benchmarkDeliveryTimeout+benchmarkRetryMax,
		)
		err = producer.Close(closeCtx)
		closeCancel()
		if err != nil {
			t.Fatalf("close %s producer: %v", candidate.name, err)
		}
		wantKeys = append(wantKeys, key)
		wantValues = append(wantValues, value)
	}

	gotKeys, gotValues := readBenchmarkRecords(
		t,
		brokers,
		topic,
		len(producerOutcomeCandidates),
	)
	if !slices.EqualFunc(gotKeys, wantKeys, slices.Equal[[]byte]) {
		t.Fatalf("consumed keys = %q, want %q", gotKeys, wantKeys)
	}
	if !slices.EqualFunc(gotValues, wantValues, slices.Equal[[]byte]) {
		t.Fatalf("consumed values = %q, want %q", gotValues, wantValues)
	}
}

func TestEquivalentProducerBatchOutcomes(t *testing.T) {
	brokers := benchmarkBrokers(t)
	topic := createIsolatedBenchmarkTopic(t, brokers)
	const recordsPerBatch = 3
	wantValues := make([][]byte, 0, len(producerOutcomeCandidates)*recordsPerBatch)
	wantKeys := make([][]byte, 0, len(producerOutcomeCandidates)*recordsPerBatch)
	for candidateIndex, candidate := range producerOutcomeCandidates {
		records := make([]benchmarkProducerRecord, 0, recordsPerBatch)
		for recordIndex := range recordsPerBatch {
			key := []byte(fmt.Sprintf("candidate-%d-record-%d", candidateIndex, recordIndex))
			value := []byte(fmt.Sprintf("%s-record-%d", candidate.name, recordIndex))
			records = append(records, benchmarkProducerRecord{key: key, value: value})
			wantKeys = append(wantKeys, key)
			wantValues = append(wantValues, value)
		}
		producer := candidate.new(t, brokers, topic, true, compressionSnappy)
		ctx, cancel := context.WithTimeout(
			context.Background(),
			benchmarkDeliveryTimeout+benchmarkRetryMax,
		)
		err := producer.ProduceBatch(ctx, records)
		cancel()
		if err != nil {
			t.Fatalf("produce batch with %s: %v", candidate.name, err)
		}
		closeCtx, closeCancel := context.WithTimeout(
			context.Background(),
			benchmarkDeliveryTimeout+benchmarkRetryMax,
		)
		err = producer.Close(closeCtx)
		closeCancel()
		if err != nil {
			t.Fatalf("close %s producer: %v", candidate.name, err)
		}
	}

	gotKeys, gotValues := readBenchmarkRecords(
		t,
		brokers,
		topic,
		len(wantKeys),
	)
	if !slices.EqualFunc(gotKeys, wantKeys, slices.Equal[[]byte]) {
		t.Fatalf("consumed batch keys = %q, want %q", gotKeys, wantKeys)
	}
	if !slices.EqualFunc(gotValues, wantValues, slices.Equal[[]byte]) {
		t.Fatalf("consumed batch values = %q, want %q", gotValues, wantValues)
	}
}

func TestEquivalentAsynchronousProducerOutcomes(t *testing.T) {
	brokers := benchmarkBrokers(t)
	const recordsPerWindow = 3
	for candidateIndex, candidate := range asynchronousProducerOutcomeCandidates {
		topic := createIsolatedBenchmarkTopic(t, brokers)
		records := make([]benchmarkProducerRecord, 0, recordsPerWindow)
		wantKeys := make([][]byte, 0, recordsPerWindow)
		wantValues := make([][]byte, 0, recordsPerWindow)
		for recordIndex := range recordsPerWindow {
			key := []byte(fmt.Sprintf("candidate-%d-record-%d", candidateIndex, recordIndex))
			value := []byte(fmt.Sprintf("%s-record-%d", candidate.name, recordIndex))
			records = append(records, benchmarkProducerRecord{key: key, value: value})
			wantKeys = append(wantKeys, key)
			wantValues = append(wantValues, value)
		}
		producer := candidate.new(t, brokers, topic, true, compressionSnappy)
		ctx, cancel := context.WithTimeout(
			context.Background(),
			benchmarkDeliveryTimeout+benchmarkRetryMax,
		)
		err := producer.ProduceWindow(ctx, records)
		cancel()
		if err != nil {
			t.Fatalf("produce asynchronously with %s: %v", candidate.name, err)
		}
		closeCtx, closeCancel := context.WithTimeout(
			context.Background(),
			benchmarkDeliveryTimeout+benchmarkRetryMax,
		)
		err = producer.Close(closeCtx)
		closeCancel()
		if err != nil {
			t.Fatalf("close %s asynchronous producer: %v", candidate.name, err)
		}

		gotKeys, gotValues := readBenchmarkRecords(t, brokers, topic, recordsPerWindow)
		if !slices.EqualFunc(gotKeys, wantKeys, slices.Equal[[]byte]) {
			t.Fatalf("consumed asynchronous keys from %s = %q, want %q", candidate.name, gotKeys, wantKeys)
		}
		if !slices.EqualFunc(gotValues, wantValues, slices.Equal[[]byte]) {
			t.Fatalf("consumed asynchronous values from %s = %q, want %q", candidate.name, gotValues, wantValues)
		}
	}
}

func TestBenchmarkRuntimeVersionValidation(t *testing.T) {
	t.Parallel()
	if err := validateBenchmarkFixtureVersion("7.5.0-ccs"); err != nil {
		t.Fatalf("validate expected runtime version: %v", err)
	}
	if err := validateBenchmarkFixtureVersion("7.5.1-ccs"); err == nil {
		t.Fatal("unexpected runtime version was accepted")
	}
}

func TestBenchmarkRuntimeIdentityValidation(t *testing.T) {
	t.Parallel()
	for _, identity := range []string{
		"Apache Kafka 4.3.1",
		"MSK-Provisioned eu-north-1",
	} {
		if !validBenchmarkRuntimeIdentity(identity) {
			t.Fatalf("safe runtime identity %q was rejected", identity)
		}
	}
	for _, identity := range []string{
		"",
		"broker\tidentity",
		"broker\x1b[31midentity",
		strings.Repeat("a", 257),
	} {
		if validBenchmarkRuntimeIdentity(identity) {
			t.Fatalf("unsafe runtime identity %q was accepted", identity)
		}
	}
}

type policyProducer struct {
	producer *policy.Producer
	topic    string
}

func newPolicyProducer(
	t testing.TB,
	brokers []string,
	topic string,
	keyed bool,
	compression benchmarkCompression,
) synchronousProducer {
	t.Helper()
	keyPolicy := policy.UnkeyedAllowed
	if keyed {
		keyPolicy = policy.KeyRequired
	}
	codec := policy.CompressionNone
	if compression == compressionSnappy {
		codec = policy.CompressionSnappy
	}
	producer, err := policy.NewProducer(policy.ProducerConfig{
		Brokers:                brokers,
		ClientID:               "golib-kafka-client-benchmark",
		AllowedTopics:          []string{topic},
		KeyPolicy:              keyPolicy,
		MaxBufferedRecords:     1_000,
		MaxBufferedBytes:       64 << 20,
		MaxBatchRecords:        100,
		MaxBatchBytes:          benchmarkBatchBytes,
		RecordRetries:          benchmarkRecordRetries,
		RetryBackoffMin:        benchmarkRetryMin,
		RetryBackoffMax:        benchmarkRetryMax,
		DeliveryTimeout:        benchmarkDeliveryTimeout,
		ShutdownTimeout:        benchmarkDeliveryTimeout + benchmarkRetryMax,
		RequestTimeout:         benchmarkRequestTimeout,
		DialTimeout:            benchmarkRequestTimeout,
		Linger:                 benchmarkLinger,
		CompressionPreferences: []policy.CompressionCodec{codec},
		Security:               policy.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct policy producer: %v", err)
	}

	return &policyProducer{producer: producer, topic: topic}
}

func (producer *policyProducer) Produce(
	ctx context.Context,
	key []byte,
	value []byte,
) error {
	return producer.producer.PublishRecord(ctx, policy.ProducerRecord{
		Topic: producer.topic,
		Key:   key,
		Value: value,
	}).Err
}

func (producer *policyProducer) ProduceBatch(
	ctx context.Context,
	records []benchmarkProducerRecord,
) error {
	policyRecords := make([]policy.ProducerRecord, len(records))
	for index, record := range records {
		policyRecords[index] = policy.ProducerRecord{
			Topic: producer.topic,
			Key:   record.key,
			Value: record.value,
		}
	}
	_, err := producer.producer.PublishBatch(ctx, policyRecords)

	return err
}

func newPolicyAsynchronousProducer(
	t testing.TB,
	brokers []string,
	topic string,
	keyed bool,
	compression benchmarkCompression,
) asynchronousProducer {
	t.Helper()

	return newPolicyProducer(t, brokers, topic, keyed, compression).(*policyProducer)
}

func (producer *policyProducer) ProduceWindow(
	ctx context.Context,
	records []benchmarkProducerRecord,
) error {
	deliveries := make([]<-chan policy.DeliveryResult, 0, len(records))
	for _, record := range records {
		delivery, err := producer.producer.PublishAsync(ctx, policy.ProducerRecord{
			Topic: producer.topic,
			Key:   record.key,
			Value: record.value,
		})
		if err != nil {
			return err
		}
		deliveries = append(deliveries, delivery)
	}

	var resultErr error
	for _, delivery := range deliveries {
		select {
		case result := <-delivery:
			resultErr = errors.Join(resultErr, result.Err)
		case <-ctx.Done():
			return errors.Join(resultErr, ctx.Err())
		}
	}

	return resultErr
}

func (producer *policyProducer) Close(ctx context.Context) error {
	return producer.producer.Shutdown(ctx)
}

type franzProducer struct {
	client *kgo.Client
	topic  string
}

func newFranzProducer(
	t testing.TB,
	brokers []string,
	topic string,
	_ bool,
	compression benchmarkCompression,
) synchronousProducer {
	t.Helper()

	return newFranzProducerWithPartitioner(
		t,
		brokers,
		topic,
		compression,
		kgo.UniformBytesPartitioner(65_536, true, true, nil),
	)
}

func newFranzProducerWithPartitioner(
	t testing.TB,
	brokers []string,
	topic string,
	compression benchmarkCompression,
	partitioner kgo.Partitioner,
) *franzProducer {
	t.Helper()
	codec := kgo.NoCompression()
	if compression == compressionSnappy {
		codec = kgo.SnappyCompression()
	}
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("raw-franz-go-client-benchmark"),
		kgo.RecordPartitioner(partitioner),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.StopProducerOnDataLossDetected(),
		kgo.MaxBufferedRecords(1_000),
		kgo.MaxBufferedBytes(64<<20),
		kgo.ProducerBatchMaxBytes(benchmarkBatchBytes),
		kgo.RecordRetries(benchmarkRecordRetries),
		kgo.RetryBackoffFn(func(int) time.Duration { return benchmarkRetryMin }),
		kgo.MetadataMinAge(benchmarkRetryMin),
		kgo.RecordDeliveryTimeout(benchmarkDeliveryTimeout),
		kgo.ProduceRequestTimeout(benchmarkRequestTimeout),
		kgo.DialTimeout(benchmarkRequestTimeout),
		kgo.ProducerLinger(benchmarkLinger),
		kgo.ProducerBatchCompression(codec),
		kgo.AllowIdempotentProduceCancellation(),
	)
	if err != nil {
		t.Fatalf("construct raw franz-go producer: %v", err)
	}

	return &franzProducer{client: client, topic: topic}
}

func (producer *franzProducer) Produce(
	ctx context.Context,
	key []byte,
	value []byte,
) error {
	return producer.client.ProduceSync(ctx, &kgo.Record{
		Topic: producer.topic,
		Key:   key,
		Value: value,
	}).FirstErr()
}

func (producer *franzProducer) ProduceBatch(
	ctx context.Context,
	records []benchmarkProducerRecord,
) error {
	franzRecords := make([]*kgo.Record, len(records))
	for index, record := range records {
		franzRecords[index] = &kgo.Record{
			Topic: producer.topic,
			Key:   record.key,
			Value: record.value,
		}
	}

	return producer.client.ProduceSync(ctx, franzRecords...).FirstErr()
}

func newFranzAsynchronousProducer(
	t testing.TB,
	brokers []string,
	topic string,
	keyed bool,
	compression benchmarkCompression,
) asynchronousProducer {
	t.Helper()

	return newFranzProducer(t, brokers, topic, keyed, compression).(*franzProducer)
}

func (producer *franzProducer) ProduceWindow(
	ctx context.Context,
	records []benchmarkProducerRecord,
) error {
	results := make(chan error, len(records))
	for _, record := range records {
		producer.client.Produce(ctx, &kgo.Record{
			Topic: producer.topic,
			Key:   record.key,
			Value: record.value,
		}, func(_ *kgo.Record, err error) {
			results <- err
		})
	}

	var resultErr error
	for range records {
		select {
		case err := <-results:
			resultErr = errors.Join(resultErr, err)
		case <-ctx.Done():
			return errors.Join(resultErr, ctx.Err())
		}
	}

	return resultErr
}

func (producer *franzProducer) Close(context.Context) error {
	producer.client.Close()

	return nil
}

type saramaProducer struct {
	producer sarama.SyncProducer
	topic    string
}

func newSaramaProducer(
	t testing.TB,
	brokers []string,
	topic string,
	keyed bool,
	compression benchmarkCompression,
) synchronousProducer {
	t.Helper()
	config := newSaramaProducerConfig(keyed, compression)
	producer, err := sarama.NewSyncProducer(brokers, config)
	if err != nil {
		t.Fatalf("construct Sarama producer: %v", err)
	}

	return &saramaProducer{producer: producer, topic: topic}
}

func newSaramaProducerConfig(
	keyed bool,
	compression benchmarkCompression,
) *sarama.Config {
	config := sarama.NewConfig()
	config.ClientID = "sarama-client-benchmark"
	config.Version = sarama.V3_5_0_0
	config.Net.MaxOpenRequests = 1
	config.Net.DialTimeout = benchmarkRequestTimeout
	config.Net.ReadTimeout = benchmarkRequestTimeout
	config.Net.WriteTimeout = benchmarkRequestTimeout
	config.Metadata.AllowAutoTopicCreation = false
	config.Metadata.Retry.Backoff = benchmarkRetryMin
	config.Producer.MaxMessageBytes = benchmarkBatchBytes
	config.Producer.RequiredAcks = sarama.WaitForAll
	config.Producer.Timeout = benchmarkRequestTimeout
	config.Producer.Idempotent = true
	config.Producer.Return.Successes = true
	config.Producer.Return.Errors = true
	config.Producer.Retry.Max = benchmarkRecordRetries
	config.Producer.Retry.Backoff = benchmarkRetryMin
	config.Producer.Retry.MaxBufferLength = 4_096
	config.Producer.Retry.MaxBufferBytes = 64 << 20
	config.Producer.Flush.Frequency = benchmarkLinger
	config.Producer.Flush.MaxMessages = 100
	config.ChannelBufferSize = 1_000
	config.Producer.Partitioner = sarama.NewRandomPartitioner
	if keyed {
		config.Producer.Partitioner = sarama.NewMurmur2Partitioner
	}
	if compression == compressionSnappy {
		config.Producer.Compression = sarama.CompressionSnappy
	} else {
		config.Producer.Compression = sarama.CompressionNone
	}

	return config
}

func (producer *saramaProducer) Produce(
	_ context.Context,
	key []byte,
	value []byte,
) error {
	message := &sarama.ProducerMessage{
		Topic: producer.topic,
		Value: sarama.ByteEncoder(value),
	}
	if key != nil {
		message.Key = sarama.ByteEncoder(key)
	}
	_, _, err := producer.producer.SendMessage(message)

	return err
}

func (producer *saramaProducer) ProduceBatch(
	_ context.Context,
	records []benchmarkProducerRecord,
) error {
	messages := make([]*sarama.ProducerMessage, len(records))
	for index, record := range records {
		message := &sarama.ProducerMessage{
			Topic: producer.topic,
			Value: sarama.ByteEncoder(record.value),
		}
		if record.key != nil {
			message.Key = sarama.ByteEncoder(record.key)
		}
		messages[index] = message
	}

	return producer.producer.SendMessages(messages)
}

func (producer *saramaProducer) Close(context.Context) error {
	return producer.producer.Close()
}

type saramaAsynchronousResult struct {
	delivery chan error
}

type saramaAsynchronousProducer struct {
	producer  sarama.AsyncProducer
	topic     string
	drained   chan struct{}
	closeOnce sync.Once
}

func newSaramaAsynchronousProducer(
	t testing.TB,
	brokers []string,
	topic string,
	keyed bool,
	compression benchmarkCompression,
) asynchronousProducer {
	t.Helper()
	producer, err := sarama.NewAsyncProducer(
		brokers,
		newSaramaProducerConfig(keyed, compression),
	)
	if err != nil {
		t.Fatalf("construct asynchronous Sarama producer: %v", err)
	}
	asynchronous := &saramaAsynchronousProducer{
		producer: producer,
		topic:    topic,
		drained:  make(chan struct{}),
	}
	go asynchronous.drainResults()

	return asynchronous
}

func (producer *saramaAsynchronousProducer) ProduceWindow(
	ctx context.Context,
	records []benchmarkProducerRecord,
) error {
	results := make([]*saramaAsynchronousResult, 0, len(records))
	for _, record := range records {
		result := &saramaAsynchronousResult{delivery: make(chan error, 1)}
		message := &sarama.ProducerMessage{
			Topic:    producer.topic,
			Value:    sarama.ByteEncoder(record.value),
			Metadata: result,
		}
		if record.key != nil {
			message.Key = sarama.ByteEncoder(record.key)
		}
		select {
		case producer.producer.Input() <- message:
			results = append(results, result)
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	var resultErr error
	for _, result := range results {
		select {
		case err := <-result.delivery:
			resultErr = errors.Join(resultErr, err)
		case <-ctx.Done():
			return errors.Join(resultErr, ctx.Err())
		}
	}

	return resultErr
}

func (producer *saramaAsynchronousProducer) drainResults() {
	defer close(producer.drained)
	successes := producer.producer.Successes()
	failures := producer.producer.Errors()
	for successes != nil || failures != nil {
		select {
		case message, ok := <-successes:
			if !ok {
				successes = nil
				continue
			}
			completeSaramaAsynchronousResult(message, nil)
		case failure, ok := <-failures:
			if !ok {
				failures = nil
				continue
			}
			completeSaramaAsynchronousResult(failure.Msg, failure.Err)
		}
	}
}

func completeSaramaAsynchronousResult(message *sarama.ProducerMessage, err error) {
	result, ok := message.Metadata.(*saramaAsynchronousResult)
	if ok {
		result.delivery <- err
	}
}

func (producer *saramaAsynchronousProducer) Close(ctx context.Context) error {
	producer.closeOnce.Do(producer.producer.AsyncClose)
	select {
	case <-producer.drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type kafkaGoProducer struct {
	writer    *segmentkafka.Writer
	transport *segmentkafka.Transport
}

func newKafkaGoProducer(
	_ testing.TB,
	brokers []string,
	topic string,
	keyed bool,
	compression benchmarkCompression,
) synchronousProducer {
	balancer := segmentkafka.Balancer(&segmentkafka.RoundRobin{})
	if keyed {
		balancer = &segmentkafka.Murmur2Balancer{}
	}
	codec := segmentkafka.Compression(0)
	if compression == compressionSnappy {
		codec = segmentkafka.Snappy
	}
	transport := &segmentkafka.Transport{
		ClientID:    "kafka-go-client-benchmark",
		MetadataTTL: benchmarkRetryMin,
	}

	return &kafkaGoProducer{
		transport: transport,
		writer: &segmentkafka.Writer{
			Addr:                   segmentkafka.TCP(brokers...),
			Topic:                  topic,
			Balancer:               balancer,
			MaxAttempts:            benchmarkRecordRetries,
			BatchBytes:             benchmarkBatchBytes,
			BatchTimeout:           benchmarkLinger,
			ReadTimeout:            benchmarkRequestTimeout,
			WriteTimeout:           benchmarkRequestTimeout,
			RequiredAcks:           segmentkafka.RequireAll,
			Async:                  false,
			Compression:            codec,
			AllowAutoTopicCreation: false,
			Transport:              transport,
		},
	}
}

func (producer *kafkaGoProducer) Produce(
	ctx context.Context,
	key []byte,
	value []byte,
) error {
	return producer.writer.WriteMessages(ctx, segmentkafka.Message{
		Key:   key,
		Value: value,
	})
}

func (producer *kafkaGoProducer) ProduceBatch(
	ctx context.Context,
	records []benchmarkProducerRecord,
) error {
	messages := make([]segmentkafka.Message, len(records))
	for index, record := range records {
		messages[index] = segmentkafka.Message{
			Key:   record.key,
			Value: record.value,
		}
	}

	return producer.writer.WriteMessages(ctx, messages...)
}

func (producer *kafkaGoProducer) Close(context.Context) error {
	err := producer.writer.Close()
	producer.transport.CloseIdleConnections()

	return err
}

type kafkaGoAsynchronousProducer struct {
	writer    *segmentkafka.Writer
	transport *segmentkafka.Transport
}

func newKafkaGoAsynchronousProducer(
	_ testing.TB,
	brokers []string,
	topic string,
	keyed bool,
	compression benchmarkCompression,
) asynchronousProducer {
	balancer := segmentkafka.Balancer(&segmentkafka.RoundRobin{})
	if keyed {
		balancer = &segmentkafka.Murmur2Balancer{}
	}
	codec := segmentkafka.Compression(0)
	if compression == compressionSnappy {
		codec = segmentkafka.Snappy
	}
	transport := &segmentkafka.Transport{
		ClientID:    "kafka-go-async-client-benchmark",
		MetadataTTL: benchmarkRetryMin,
	}

	return &kafkaGoAsynchronousProducer{
		transport: transport,
		writer: &segmentkafka.Writer{
			Addr:                   segmentkafka.TCP(brokers...),
			Topic:                  topic,
			Balancer:               balancer,
			MaxAttempts:            benchmarkRecordRetries,
			BatchSize:              100,
			BatchBytes:             benchmarkBatchBytes,
			BatchTimeout:           benchmarkLinger,
			ReadTimeout:            benchmarkRequestTimeout,
			WriteTimeout:           benchmarkRequestTimeout,
			RequiredAcks:           segmentkafka.RequireAll,
			Async:                  true,
			Compression:            codec,
			AllowAutoTopicCreation: false,
			Transport:              transport,
			Completion: func(messages []segmentkafka.Message, err error) {
				for _, message := range messages {
					result, ok := message.WriterData.(chan error)
					if ok {
						result <- err
					}
				}
			},
		},
	}
}

func (producer *kafkaGoAsynchronousProducer) ProduceWindow(
	ctx context.Context,
	records []benchmarkProducerRecord,
) error {
	results := make([]chan error, len(records))
	messages := make([]segmentkafka.Message, len(records))
	for index, record := range records {
		results[index] = make(chan error, 1)
		messages[index] = segmentkafka.Message{
			Key:        record.key,
			Value:      record.value,
			WriterData: results[index],
		}
	}
	if err := producer.writer.WriteMessages(ctx, messages...); err != nil {
		return err
	}

	var resultErr error
	for _, result := range results {
		select {
		case err := <-result:
			resultErr = errors.Join(resultErr, err)
		case <-ctx.Done():
			return errors.Join(resultErr, ctx.Err())
		}
	}

	return resultErr
}

func (producer *kafkaGoAsynchronousProducer) Close(context.Context) error {
	err := producer.writer.Close()
	producer.transport.CloseIdleConnections()

	return err
}

func benchmarkBrokers(tb testing.TB) []string {
	tb.Helper()
	if configured := os.Getenv("KAFKA_BENCH_BROKERS"); configured != "" {
		identity := os.Getenv("KAFKA_BENCH_BROKER_IDENTITY")
		if !validBenchmarkRuntimeIdentity(identity) {
			tb.Fatal("KAFKA_BENCH_BROKER_IDENTITY must be a bounded safe description")
		}
		brokers := strings.Split(configured, ",")
		for _, broker := range brokers {
			if broker == "" || broker != strings.TrimSpace(broker) ||
				strings.ContainsAny(broker, "@/?#") {
				tb.Fatal("KAFKA_BENCH_BROKERS contains an invalid or secret-bearing address")
			}
		}
		reportBenchmarkRuntime(identity)

		return brokers
	}

	benchmarkFixtureOnce.Do(startBenchmarkFixture)
	if benchmarkFixtureErr != nil {
		tb.Fatalf("start benchmark Kafka fixture: %v", benchmarkFixtureErr)
	}
	reportBenchmarkRuntime("Confluent Local " + benchmarkFixtureVersion)

	return slices.Clone(benchmarkFixtureBrokers)
}

func reportBenchmarkRuntime(identity string) {
	benchmarkRuntimeReport.Do(func() {
		fmt.Printf("benchmark-broker-runtime=%s\n", identity)
	})
}

func validBenchmarkRuntimeIdentity(identity string) bool {
	if identity == "" || len(identity) > 256 || identity != strings.TrimSpace(identity) {
		return false
	}
	for index := range len(identity) {
		character := identity[index]
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			strings.ContainsRune(" ._:+()-", rune(character)) {
			continue
		}

		return false
	}

	return true
}

func validateBenchmarkFixtureVersion(version string) error {
	if version != benchmarkKafkaVersion {
		return errors.New("runtime version does not match pinned broker image")
	}

	return nil
}

func startBenchmarkFixture() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := tckafka.Run(
		ctx,
		benchmarkKafkaImage,
		testcontainers.WithLogger(benchmarkNoopLogger{}),
	)
	benchmarkFixture = container
	if err != nil {
		benchmarkFixtureErr = err

		return
	}

	versionCtx, versionCancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	exitCode, output, err := container.Exec(
		versionCtx,
		[]string{"kafka-topics", "--version"},
		tcexec.Multiplexed(),
	)
	versionCancel()
	if err != nil || exitCode != 0 {
		benchmarkFixtureErr = fmt.Errorf(
			"inspect runtime version: exit=%d error=%v",
			exitCode,
			err,
		)

		return
	}
	version, err := io.ReadAll(io.LimitReader(output, 256))
	if err != nil {
		benchmarkFixtureErr = fmt.Errorf("read runtime version: %w", err)

		return
	}
	benchmarkFixtureVersion = strings.TrimSpace(string(version))
	if err := validateBenchmarkFixtureVersion(benchmarkFixtureVersion); err != nil {
		benchmarkFixtureErr = err

		return
	}
	benchmarkFixtureBrokers, err = container.Brokers(ctx)
	if err != nil {
		benchmarkFixtureErr = fmt.Errorf("resolve brokers: %w", err)
	}
}

func createBenchmarkTopic(tb testing.TB, brokers []string) string {
	tb.Helper()
	benchmarkTopicOnce.Do(func() {
		benchmarkTopic, benchmarkTopicErr = createBenchmarkTopicOnce(brokers)
	})
	if benchmarkTopicErr != nil {
		tb.Fatalf("create benchmark topic: %v", benchmarkTopicErr)
	}

	return benchmarkTopic
}

func createIsolatedBenchmarkTopic(tb testing.TB, brokers []string) string {
	tb.Helper()
	topic, err := createBenchmarkTopicOnce(brokers)
	if err != nil {
		tb.Fatalf("create isolated benchmark topic: %v", err)
	}

	return topic
}

func createBenchmarkTopicOnce(brokers []string) (string, error) {
	return createBenchmarkTopicWithPartitionsOnce(brokers, 1)
}

func createBenchmarkTopicWithPartitionsOnce(
	brokers []string,
	partitionCount int32,
) (string, error) {
	topic := fmt.Sprintf("golib-client-benchmark-%d", time.Now().UnixNano())
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("golib-client-benchmark-admin"),
		kgo.AllowAutoTopicCreation(),
	)
	if err != nil {
		return "", fmt.Errorf("construct topic admin: %w", err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin := kadm.NewClient(client)
	results, err := admin.CreateTopics(ctx, partitionCount, 1, nil, topic)
	if err != nil {
		return "", err
	}
	result, ok := results[topic]
	if !ok {
		return "", fmt.Errorf("response omitted %q", topic)
	}
	if result.Err != nil {
		return "", fmt.Errorf("topic %q: %w", topic, result.Err)
	}
	metadataPoll := time.NewTicker(10 * time.Millisecond)
	defer metadataPoll.Stop()
	for {
		details, listErr := admin.ListTopics(ctx, topic)
		if listErr != nil {
			return "", fmt.Errorf("inspect topic %q readiness: %w", topic, listErr)
		}
		detail, topicReady := details[topic]
		ready := topicReady && detail.Err == nil &&
			len(detail.Partitions) == int(partitionCount)
		for partitionID := range partitionCount {
			partition, partitionReady := detail.Partitions[partitionID]
			if !partitionReady || partition.Err != nil || partition.Leader < 0 {
				ready = false
				break
			}
		}
		if ready {
			break
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("topic %q did not become ready: %w", topic, ctx.Err())
		case <-metadataPoll.C:
		}
	}

	return topic, nil
}

func readBenchmarkRecords(
	t *testing.T,
	brokers []string,
	topic string,
	count int,
) ([][]byte, [][]byte) {
	t.Helper()
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("golib-client-benchmark-verifier"),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{
			topic: {0: kgo.NewOffset().AtStart()},
		}),
		kgo.FetchMaxWait(50*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("construct benchmark verifier: %v", err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	keys := make([][]byte, 0, count)
	values := make([][]byte, 0, count)
	for len(values) < count {
		fetches := client.PollRecords(ctx, count-len(values))
		if err := fetches.Err(); err != nil {
			t.Fatalf("read benchmark records: %v", err)
		}
		for _, record := range fetches.Records() {
			keys = append(keys, slices.Clone(record.Key))
			values = append(values, slices.Clone(record.Value))
		}
	}

	return keys, values
}
