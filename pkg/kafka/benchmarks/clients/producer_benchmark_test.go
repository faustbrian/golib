//go:build integration

package clients_test

import (
	"context"
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
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	benchmarkKafkaImage = "confluentinc/confluent-local:7.5.0@" +
		"sha256:8e391de42cfcd3498e7317dcf159790f1f1cc3f3ffce900b30d7da23888687fd"
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
	Close(context.Context) error
}

type producerCandidate struct {
	name string
	new  func(testing.TB, []string, string, bool, benchmarkCompression) synchronousProducer
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

func TestEquivalentProducerOutcomes(t *testing.T) {
	brokers := benchmarkBrokers(t)
	topic := createBenchmarkTopic(t, brokers)
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
	codec := kgo.NoCompression()
	if compression == compressionSnappy {
		codec = kgo.SnappyCompression()
	}
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("raw-franz-go-client-benchmark"),
		kgo.RecordPartitioner(kgo.UniformBytesPartitioner(65_536, true, true, nil)),
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
	config.Producer.Retry.Max = benchmarkRecordRetries
	config.Producer.Retry.Backoff = benchmarkRetryMin
	config.Producer.Flush.Frequency = benchmarkLinger
	config.Producer.Flush.MaxMessages = 100
	config.Producer.Partitioner = sarama.NewRandomPartitioner
	if keyed {
		config.Producer.Partitioner = sarama.NewHashPartitioner
	}
	if compression == compressionSnappy {
		config.Producer.Compression = sarama.CompressionSnappy
	} else {
		config.Producer.Compression = sarama.CompressionNone
	}
	producer, err := sarama.NewSyncProducer(brokers, config)
	if err != nil {
		t.Fatalf("construct Sarama producer: %v", err)
	}

	return &saramaProducer{producer: producer, topic: topic}
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

func (producer *saramaProducer) Close(context.Context) error {
	return producer.producer.Close()
}

type kafkaGoProducer struct {
	writer *segmentkafka.Writer
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

	return &kafkaGoProducer{
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

func (producer *kafkaGoProducer) Close(context.Context) error {
	return producer.writer.Close()
}

func benchmarkBrokers(tb testing.TB) []string {
	tb.Helper()
	if configured := os.Getenv("KAFKA_BENCH_BROKERS"); configured != "" {
		identity := os.Getenv("KAFKA_BENCH_BROKER_IDENTITY")
		if identity == "" || len(identity) > 256 || strings.ContainsAny(identity, "\r\n") {
			tb.Fatal("KAFKA_BENCH_BROKER_IDENTITY must be a bounded single-line description")
		}
		brokers := strings.Split(configured, ",")
		for _, broker := range brokers {
			if broker == "" || broker != strings.TrimSpace(broker) ||
				strings.ContainsAny(broker, "@/?#") {
				tb.Fatal("KAFKA_BENCH_BROKERS contains an invalid or secret-bearing address")
			}
		}
		tb.Logf("benchmark broker runtime: %s", identity)

		return brokers
	}

	benchmarkFixtureOnce.Do(startBenchmarkFixture)
	if benchmarkFixtureErr != nil {
		tb.Fatalf("start benchmark Kafka fixture: %v", benchmarkFixtureErr)
	}
	tb.Logf("benchmark broker runtime: Confluent Local %s", benchmarkFixtureVersion)

	return slices.Clone(benchmarkFixtureBrokers)
}

func startBenchmarkFixture() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := tckafka.Run(ctx, benchmarkKafkaImage)
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
	if benchmarkFixtureVersion == "" || len(benchmarkFixtureVersion) > 128 {
		benchmarkFixtureErr = fmt.Errorf("runtime version is invalid")

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

func createBenchmarkTopicOnce(brokers []string) (string, error) {
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
	results, err := kadm.NewClient(client).CreateTopics(ctx, 1, 1, nil, topic)
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
