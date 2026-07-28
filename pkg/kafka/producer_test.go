package kafka

import (
	"context"
	"crypto/tls"
	"errors"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestProducerConfigUsesExplicitCompressionPreference(t *testing.T) {
	t.Parallel()

	config, err := normalizeProducerConfig(ProducerConfig{
		Brokers:       []string{"broker.internal:9092"},
		ClientID:      "track",
		AllowedTopics: []string{"events"},
	})
	if err != nil {
		t.Fatalf("normalize default config: %v", err)
	}
	if !reflect.DeepEqual(
		config.CompressionPreferences,
		[]CompressionCodec{CompressionSnappy, CompressionNone},
	) {
		t.Fatalf(
			"default compression = %#v",
			config.CompressionPreferences,
		)
	}

	preferences := []CompressionCodec{
		CompressionZstd,
		CompressionLz4,
		CompressionSnappy,
		CompressionNone,
	}
	config, err = normalizeProducerConfig(ProducerConfig{
		Brokers:                []string{"broker.internal:9092"},
		ClientID:               "track",
		AllowedTopics:          []string{"events"},
		CompressionPreferences: preferences,
	})
	if err != nil {
		t.Fatalf("normalize explicit config: %v", err)
	}
	preferences[0] = CompressionNone
	if !reflect.DeepEqual(
		config.CompressionPreferences,
		[]CompressionCodec{
			CompressionZstd,
			CompressionLz4,
			CompressionSnappy,
			CompressionNone,
		},
	) {
		t.Fatalf(
			"owned compression = %#v",
			config.CompressionPreferences,
		)
	}

	got := franzCompressionCodecs(config.CompressionPreferences)
	want := []kgo.CompressionCodec{
		kgo.ZstdCompression(),
		kgo.Lz4Compression(),
		kgo.SnappyCompression(),
		kgo.NoCompression(),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("franz compression = %#v, want %#v", got, want)
	}
}

func TestProducerConfigRequiresAndOwnsTopicAllowlist(t *testing.T) {
	t.Parallel()

	if _, err := normalizeProducerConfig(ProducerConfig{
		Brokers:  []string{"broker.internal:9092"},
		ClientID: "track",
	}); !errors.Is(err, ErrTopicsRequired) {
		t.Fatalf("missing allowlist error = %v, want %v", err, ErrTopicsRequired)
	}

	allowed := []string{"events", "commands"}
	config, err := normalizeProducerConfig(ProducerConfig{
		Brokers:       []string{"broker.internal:9092"},
		ClientID:      "track",
		AllowedTopics: allowed,
	})
	if err != nil {
		t.Fatalf("normalizeProducerConfig() error = %v", err)
	}
	allowed[0] = "mutated"
	if !reflect.DeepEqual(config.AllowedTopics, []string{"events", "commands"}) {
		t.Fatalf("owned allowlist = %#v", config.AllowedTopics)
	}
	limits := DefaultMessageLimits()
	limits.MaxTopicBytes = 5
	if _, err := normalizeProducerConfig(ProducerConfig{
		Brokers:       []string{"broker.internal:9092"},
		ClientID:      "track",
		AllowedTopics: []string{"events"},
		Limits:        limits,
	}); !errors.Is(err, ErrInvalidTopic) {
		t.Fatalf("topic above message limit error = %v, want %v", err, ErrInvalidTopic)
	}

	for name, test := range map[string]struct {
		topics []string
		want   error
	}{
		"too many":  {topics: make([]string, 65), want: ErrTooManyTopics},
		"invalid":   {topics: []string{"events/commands"}, want: ErrInvalidTopic},
		"duplicate": {topics: []string{"events", "events"}, want: ErrDuplicateTopic},
	} {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for index := range test.topics {
				if test.topics[index] == "" {
					test.topics[index] = "topic-" + strconv.Itoa(index)
				}
			}
			_, err := normalizeProducerConfig(ProducerConfig{
				Brokers:       []string{"broker.internal:9092"},
				ClientID:      "track",
				AllowedTopics: test.topics,
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("normalizeProducerConfig() error = %v", err)
			}
		})
	}
}

func TestProducerConfigBoundsMaximumHeaderFraming(t *testing.T) {
	t.Parallel()

	limits := MessageLimits{
		MaxTopicBytes:       1,
		MaxKeyBytes:         1,
		MaxValueBytes:       1,
		MaxHeaders:          10_000,
		MaxHeaderKeyBytes:   1,
		MaxHeaderValueBytes: 1,
		MaxHeaderBytes:      1,
	}
	_, err := normalizeProducerConfig(ProducerConfig{
		Brokers:          []string{"broker.internal:9092"},
		ClientID:         "track",
		AllowedTopics:    []string{"e"},
		Limits:           limits,
		MaxBatchBytes:    2_000,
		MaxBufferedBytes: 2_000,
	})
	if !errors.Is(err, ErrInvalidProducerConfig) {
		t.Fatalf("normalizeProducerConfig() error = %v", err)
	}
}

func TestProducerPartitionerCombinesAutomaticAndExplicitSelection(t *testing.T) {
	t.Parallel()

	automatic := &recordingBackupTopicPartitioner{
		recordingTopicPartitioner: recordingTopicPartitioner{
			partition:  2,
			consistent: false,
		},
	}
	partitioner := newPolicyPartitioner(
		staticPartitioner{topic: automatic},
	).ForTopic("events")
	backup, ok := partitioner.(kgo.TopicBackupPartitioner)
	if !ok {
		t.Fatal("policy partitioner does not preserve backup partitioning")
	}
	batch, ok := partitioner.(kgo.TopicPartitionerOnNewBatch)
	if !ok {
		t.Fatal("policy partitioner does not preserve new-batch notification")
	}

	automaticRecord := &kgo.Record{Partition: -1}
	if partitioner.RequiresConsistency(automaticRecord) {
		t.Fatal("automatic record unexpectedly requires consistency")
	}
	if got := backup.PartitionByBackup(
		automaticRecord,
		4,
		&recordingBackupIter{remaining: 4},
	); got != 2 {
		t.Fatalf("automatic partition = %d, want 2", got)
	}
	batch.OnNewBatch()
	if automatic.backupCalls != 1 || automatic.batchCalls != 1 {
		t.Fatalf(
			"automatic calls = backup:%d batch:%d",
			automatic.backupCalls,
			automatic.batchCalls,
		)
	}

	explicitRecord := &kgo.Record{Partition: 3}
	if !partitioner.RequiresConsistency(explicitRecord) {
		t.Fatal("explicit record does not require partition consistency")
	}
	if got := backup.PartitionByBackup(
		explicitRecord,
		4,
		&recordingBackupIter{remaining: 4},
	); got != 3 {
		t.Fatalf("explicit partition = %d, want 3", got)
	}
	if automatic.backupCalls != 1 {
		t.Fatalf("explicit selection delegated %d backup calls", automatic.backupCalls)
	}
	batch.OnNewBatch()
	if automatic.batchCalls != 1 {
		t.Fatalf("explicit selection delegated %d batch calls", automatic.batchCalls)
	}
}

type staticPartitioner struct {
	topic kgo.TopicPartitioner
}

func (partitioner staticPartitioner) ForTopic(string) kgo.TopicPartitioner {
	return partitioner.topic
}

type recordingTopicPartitioner struct {
	partition      int
	consistent     bool
	partitionCalls int
}

func (partitioner *recordingTopicPartitioner) RequiresConsistency(*kgo.Record) bool {
	return partitioner.consistent
}

func (partitioner *recordingTopicPartitioner) Partition(*kgo.Record, int) int {
	partitioner.partitionCalls++

	return partitioner.partition
}

type recordingBackupTopicPartitioner struct {
	recordingTopicPartitioner
	backupCalls int
	batchCalls  int
}

func (partitioner *recordingBackupTopicPartitioner) PartitionByBackup(
	_ *kgo.Record,
	_ int,
	backup kgo.TopicBackupIter,
) int {
	partitioner.backupCalls++
	backup.Next()

	return partitioner.partition
}

func (partitioner *recordingBackupTopicPartitioner) OnNewBatch() {
	partitioner.batchCalls++
}

type recordingBackupIter struct {
	remaining int
}

func (iterator *recordingBackupIter) Next() (int, int64) {
	iterator.remaining--

	return iterator.remaining, 0
}

func (iterator *recordingBackupIter) Rem() int {
	return iterator.remaining
}

func TestProducerPartitionerFallsBackToAutomaticPartition(t *testing.T) {
	t.Parallel()

	automatic := &recordingTopicPartitioner{partition: 1}
	partitioner := newPolicyPartitioner(
		staticPartitioner{topic: automatic},
	).ForTopic("events")
	backup := partitioner.(kgo.TopicBackupPartitioner)

	if got := backup.PartitionByBackup(
		&kgo.Record{Partition: -1},
		3,
		&recordingBackupIter{remaining: 3},
	); got != 1 {
		t.Fatalf("automatic partition = %d, want 1", got)
	}
	if automatic.partitionCalls != 1 {
		t.Fatalf("automatic partition calls = %d, want 1", automatic.partitionCalls)
	}
	if got := partitioner.Partition(&kgo.Record{Partition: -1}, 3); got != 1 {
		t.Fatalf("direct automatic partition = %d, want 1", got)
	}
	if got := partitioner.Partition(&kgo.Record{Partition: 2}, 3); got != 2 {
		t.Fatalf("direct explicit partition = %d, want 2", got)
	}
	partitioner.(kgo.TopicPartitionerOnNewBatch).OnNewBatch()
}

func TestProducerConfigRejectsInvalidCompressionPreference(t *testing.T) {
	t.Parallel()

	tests := map[string][]CompressionCodec{
		"empty codec":       {0},
		"unknown codec":     {CompressionCodec(99)},
		"duplicate codec":   {CompressionSnappy, CompressionSnappy},
		"none before codec": {CompressionNone, CompressionSnappy},
		"too many codecs": {
			CompressionZstd,
			CompressionLz4,
			CompressionSnappy,
			CompressionGzip,
			CompressionNone,
			CompressionNone,
		},
	}
	for name, preferences := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			producer, err := NewProducer(ProducerConfig{
				Brokers:                []string{"broker.internal:9092"},
				ClientID:               "track",
				CompressionPreferences: preferences,
			})
			if producer != nil {
				closeErr := producer.Close()
				t.Fatalf(
					"constructed producer with invalid compression; close error = %v",
					closeErr,
				)
			}
			if !errors.Is(err, ErrInvalidCompressionPreference) {
				t.Fatalf(
					"error = %v, want ErrInvalidCompressionPreference",
					err,
				)
			}
		})
	}
}

func TestCompressionCodecNamesAndFranzMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		codec CompressionCodec
		name  string
		franz kgo.CompressionCodec
	}{
		{CompressionNone, "none", kgo.NoCompression()},
		{CompressionGzip, "gzip", kgo.GzipCompression()},
		{CompressionSnappy, "snappy", kgo.SnappyCompression()},
		{CompressionLz4, "lz4", kgo.Lz4Compression()},
		{CompressionZstd, "zstd", kgo.ZstdCompression()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if test.codec.String() != test.name {
				t.Fatalf("name = %q, want %q", test.codec, test.name)
			}
			got := franzCompressionCodecs([]CompressionCodec{test.codec})
			if len(got) != 1 || !reflect.DeepEqual(got[0], test.franz) {
				t.Fatalf("franz codec = %#v, want %#v", got, test.franz)
			}
		})
	}
	if got := CompressionCodec(99).String(); got != "unknown" {
		t.Fatalf("unknown name = %q", got)
	}
}

func TestNewProducerRequiresAtLeastOneBroker(t *testing.T) {
	t.Parallel()

	producer, err := NewProducer(ProducerConfig{
		ClientID: "track",
	})

	if producer != nil {
		t.Fatal("NewProducer() returned a producer without any brokers")
	}
	if !errors.Is(err, ErrBrokersRequired) {
		t.Fatalf("NewProducer() error = %v, want %v", err, ErrBrokersRequired)
	}
}

func TestNewProducerRequiresClientIdentity(t *testing.T) {
	t.Parallel()

	producer, err := NewProducer(ProducerConfig{
		Brokers: []string{"broker.internal:9092"},
	})

	if producer != nil {
		t.Fatal("NewProducer() returned a producer without a client identity")
	}
	if !errors.Is(err, ErrClientIDRequired) {
		t.Fatalf("NewProducer() error = %v, want %v", err, ErrClientIDRequired)
	}
}

func TestNewProducerValidatesBrokerAndClientIdentity(t *testing.T) {
	t.Parallel()

	manyBrokers := make([]string, 33)
	for index := range manyBrokers {
		manyBrokers[index] = "broker-" + strings.Repeat("x", index+1)
	}

	tests := []struct {
		name    string
		brokers []string
		client  string
		want    error
	}{
		{
			name:    "empty broker",
			brokers: []string{""},
			client:  "track",
			want:    ErrInvalidBroker,
		},
		{
			name:    "whitespace broker",
			brokers: []string{" broker.internal:9092 "},
			client:  "track",
			want:    ErrInvalidBroker,
		},
		{
			name:    "oversized broker",
			brokers: []string{strings.Repeat("b", 256)},
			client:  "track",
			want:    ErrInvalidBroker,
		},
		{
			name:    "invalid UTF-8 broker",
			brokers: []string{string([]byte{0xff})},
			client:  "track",
			want:    ErrInvalidBroker,
		},
		{
			name:    "control character broker",
			brokers: []string{"broker\n.internal:9092"},
			client:  "track",
			want:    ErrInvalidBroker,
		},
		{
			name:    "too many brokers",
			brokers: manyBrokers,
			client:  "track",
			want:    ErrTooManyBrokers,
		},
		{
			name:    "duplicate broker",
			brokers: []string{"broker:9092", "broker:9092"},
			client:  "track",
			want:    ErrDuplicateBroker,
		},
		{
			name:    "whitespace client ID",
			brokers: []string{"broker:9092"},
			client:  " ",
			want:    ErrClientIDRequired,
		},
		{
			name:    "oversized client ID",
			brokers: []string{"broker:9092"},
			client:  strings.Repeat("c", 256),
			want:    ErrClientIDTooLarge,
		},
		{
			name:    "invalid UTF-8 client ID",
			brokers: []string{"broker:9092"},
			client:  string([]byte{0xff}),
			want:    ErrInvalidClientID,
		},
		{
			name:    "control character client ID",
			brokers: []string{"broker:9092"},
			client:  "track\tid",
			want:    ErrInvalidClientID,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			producer, err := NewProducer(ProducerConfig{
				Brokers:  test.brokers,
				ClientID: test.client,
			})
			if producer != nil {
				closeErr := producer.Close()
				t.Fatalf(
					"NewProducer() returned a producer with invalid identity; close error = %v",
					closeErr,
				)
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("NewProducer() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestNewProducerUsesBoundedMessageDefaults(t *testing.T) {
	t.Parallel()

	producer, err := NewProducer(ProducerConfig{
		Brokers:       []string{"broker.internal:9092"},
		ClientID:      "track",
		AllowedTopics: []string{"events"},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	t.Cleanup(func() {
		if err := producer.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	if producer.limits != DefaultMessageLimits() {
		t.Fatalf("NewProducer() limits = %#v, want %#v", producer.limits, DefaultMessageLimits())
	}
}

func TestProducerConfigAppliesBoundedReliabilityDefaults(t *testing.T) {
	t.Parallel()

	config, err := normalizeProducerConfig(ProducerConfig{
		Brokers:       []string{"broker.internal:9092"},
		ClientID:      "track",
		AllowedTopics: []string{"events"},
	})
	if err != nil {
		t.Fatalf("normalizeProducerConfig() error = %v", err)
	}

	if config.MaxBufferedRecords != 1_000 {
		t.Fatalf("MaxBufferedRecords = %d, want 1000", config.MaxBufferedRecords)
	}
	if config.MaxBatchBytes != 1<<20 {
		t.Fatalf("MaxBatchBytes = %d, want %d", config.MaxBatchBytes, 1<<20)
	}
	if config.RecordRetries != 10 {
		t.Fatalf("RecordRetries = %d, want 10", config.RecordRetries)
	}
	if config.RetryBackoffMin != 250*time.Millisecond ||
		config.RetryBackoffMax != time.Second {
		t.Fatalf(
			"RetryBackoffMin/Max = %s/%s, want 250ms/1s",
			config.RetryBackoffMin,
			config.RetryBackoffMax,
		)
	}
	if config.DeliveryTimeout != 30*time.Second {
		t.Fatalf("DeliveryTimeout = %s, want 30s", config.DeliveryTimeout)
	}
	if config.ShutdownTimeout !=
		config.DeliveryTimeout+config.RetryBackoffMax {
		t.Fatalf(
			"ShutdownTimeout = %s, want %s",
			config.ShutdownTimeout,
			config.DeliveryTimeout+config.RetryBackoffMax,
		)
	}
	if config.RequestTimeout != 10*time.Second {
		t.Fatalf("RequestTimeout = %s, want 10s", config.RequestTimeout)
	}
	if config.DialTimeout != 10*time.Second {
		t.Fatalf("DialTimeout = %s, want 10s", config.DialTimeout)
	}
	if config.Linger != 5*time.Millisecond {
		t.Fatalf("Linger = %s, want 5ms", config.Linger)
	}

	transactionalConfig, err := normalizeProducerConfig(ProducerConfig{
		Brokers:         []string{"broker.internal:9092"},
		ClientID:        "track",
		AllowedTopics:   []string{"events"},
		TransactionalID: "track-outbox-0",
	})
	if err != nil {
		t.Fatalf("normalizeProducerConfig() transactional error = %v", err)
	}
	if transactionalConfig.TransactionTimeout != 30*time.Second ||
		transactionalConfig.TransactionEndTimeout != 30*time.Second {
		t.Fatalf("transactional defaults = %#v", transactionalConfig)
	}
}

func TestNewProducerAcceptsMinimumRetryBackoff(t *testing.T) {
	t.Parallel()

	producer, err := NewProducer(ProducerConfig{
		Brokers:         []string{"broker.internal:9092"},
		ClientID:        "track",
		AllowedTopics:   []string{"events"},
		RetryBackoffMin: time.Millisecond,
		RetryBackoffMax: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	defer closeProducerForTest(t, producer)
}

func TestProducerRetryBackoffIsExponentiallyBoundedAndJittered(t *testing.T) {
	t.Parallel()

	const (
		minimum = 250 * time.Millisecond
		maximum = time.Second
		seed    = 1
	)
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: -1, want: 229126326 * time.Nanosecond},
		{attempt: 0, want: 229126326 * time.Nanosecond},
		{attempt: 1, want: 226499062 * time.Nanosecond},
		{attempt: 2, want: 493977785 * time.Nanosecond},
		{attempt: 3, want: 923694587 * time.Nanosecond},
		{attempt: 10, want: 813376416 * time.Nanosecond},
	}
	for _, test := range tests {
		got := producerRetryBackoffDuration(
			minimum,
			maximum,
			test.attempt,
			seed,
		)
		if got != test.want {
			t.Fatalf(
				"producerRetryBackoffDuration(%d) = %s, want %s",
				test.attempt,
				got,
				test.want,
			)
		}
		if got < minimum-minimum/5 || got > maximum {
			t.Fatalf(
				"producerRetryBackoffDuration(%d) = %s outside bounds",
				test.attempt,
				got,
			)
		}
	}

	got := producerRetryBackoffDuration(
		400*time.Millisecond,
		maximum,
		3,
		seed,
	)
	if got < 800*time.Millisecond || got > maximum {
		t.Fatalf("non-power-of-two retry backoff = %s, want [800ms, 1s]", got)
	}
}

func TestProducerMetadataMinAgeHasIndependentSafeFloor(t *testing.T) {
	t.Parallel()

	if got := producerMetadataMinAge(time.Millisecond); got != 250*time.Millisecond {
		t.Fatalf("producerMetadataMinAge(1ms) = %s, want 250ms", got)
	}
	if got := producerMetadataMinAge(time.Second); got != time.Second {
		t.Fatalf("producerMetadataMinAge(1s) = %s, want 1s", got)
	}
}

func TestProducerConfigNormalizesSecureTransport(t *testing.T) {
	t.Parallel()

	sourceTLS := &tls.Config{}
	config, err := normalizeProducerConfig(ProducerConfig{
		Brokers:       []string{"broker.internal:9092"},
		ClientID:      "track",
		AllowedTopics: []string{"events"},
		Security:      ClientSecurity{TLS: sourceTLS},
	})
	if err != nil {
		t.Fatalf("normalizeProducerConfig() error = %v", err)
	}
	if config.Security.TLS == sourceTLS ||
		config.Security.TLS.MinVersion != tls.VersionTLS12 ||
		sourceTLS.MinVersion != 0 {
		t.Fatalf("normalized/source TLS = %#v/%#v", config.Security.TLS, sourceTLS)
	}

	tests := []*tls.Config{
		{InsecureSkipVerify: true},
		{MinVersion: tls.VersionTLS11},
		{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS12},
	}
	for _, tlsConfig := range tests {
		_, err := normalizeProducerConfig(ProducerConfig{
			Brokers:  []string{"broker.internal:9092"},
			ClientID: "track",
			Security: ClientSecurity{TLS: tlsConfig},
		})
		if !errors.Is(err, ErrInvalidSecurityConfig) {
			t.Fatalf("normalizeProducerConfig() security error = %v, want %v", err, ErrInvalidSecurityConfig)
		}
	}
}

func TestNewProducerRejectsUnboundedProducerConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(*ProducerConfig)
	}{
		{
			name: "negative buffer",
			change: func(config *ProducerConfig) {
				config.MaxBufferedRecords = -1
			},
		},
		{
			name: "oversized buffer",
			change: func(config *ProducerConfig) {
				config.MaxBufferedRecords = 100_001
			},
		},
		{
			name: "negative batch",
			change: func(config *ProducerConfig) {
				config.MaxBatchBytes = -1
			},
		},
		{
			name: "batch below message budget",
			change: func(config *ProducerConfig) {
				config.MaxBatchBytes = 512
			},
		},
		{
			name: "oversized batch",
			change: func(config *ProducerConfig) {
				config.MaxBatchBytes = 101 << 20
			},
		},
		{
			name: "negative retries",
			change: func(config *ProducerConfig) {
				config.RecordRetries = -1
			},
		},
		{
			name: "excessive retries",
			change: func(config *ProducerConfig) {
				config.RecordRetries = 1_001
			},
		},
		{
			name: "negative minimum retry backoff",
			change: func(config *ProducerConfig) {
				config.RetryBackoffMin = -time.Second
			},
		},
		{
			name: "maximum retry backoff below minimum",
			change: func(config *ProducerConfig) {
				config.RetryBackoffMin = time.Second
				config.RetryBackoffMax = time.Millisecond
			},
		},
		{
			name: "excessive maximum retry backoff",
			change: func(config *ProducerConfig) {
				config.RetryBackoffMax = 5*time.Second + time.Nanosecond
			},
		},
		{
			name: "retry backoff exceeds delivery timeout",
			change: func(config *ProducerConfig) {
				config.DeliveryTimeout = time.Second
				config.RetryBackoffMax = 2 * time.Second
			},
		},
		{
			name: "negative delivery timeout",
			change: func(config *ProducerConfig) {
				config.DeliveryTimeout = -time.Second
			},
		},
		{
			name: "excessive delivery timeout",
			change: func(config *ProducerConfig) {
				config.DeliveryTimeout = 10*time.Minute + time.Nanosecond
			},
		},
		{
			name: "shutdown shorter than delivery and retry bound",
			change: func(config *ProducerConfig) {
				config.ShutdownTimeout = 30 * time.Second
			},
		},
		{
			name: "excessive shutdown timeout",
			change: func(config *ProducerConfig) {
				config.ShutdownTimeout = 15*time.Minute + time.Nanosecond
			},
		},
		{
			name: "negative request timeout",
			change: func(config *ProducerConfig) {
				config.RequestTimeout = -time.Second
			},
		},
		{
			name: "excessive request timeout",
			change: func(config *ProducerConfig) {
				config.RequestTimeout = 2*time.Minute + time.Nanosecond
			},
		},
		{
			name: "request exceeds delivery timeout",
			change: func(config *ProducerConfig) {
				config.DeliveryTimeout = time.Second
				config.RequestTimeout = 2 * time.Second
			},
		},
		{
			name: "negative dial timeout",
			change: func(config *ProducerConfig) {
				config.DialTimeout = -time.Second
			},
		},
		{
			name: "excessive dial timeout",
			change: func(config *ProducerConfig) {
				config.DialTimeout = 2*time.Minute + time.Nanosecond
			},
		},
		{
			name: "negative linger",
			change: func(config *ProducerConfig) {
				config.Linger = -time.Second
			},
		},
		{
			name: "excessive linger",
			change: func(config *ProducerConfig) {
				config.Linger = time.Second + time.Nanosecond
			},
		},
		{
			name: "whitespace transactional ID",
			change: func(config *ProducerConfig) {
				config.TransactionalID = " transaction "
			},
		},
		{
			name: "oversized transactional ID",
			change: func(config *ProducerConfig) {
				config.TransactionalID = strings.Repeat("t", 256)
			},
		},
		{
			name: "invalid UTF-8 transactional ID",
			change: func(config *ProducerConfig) {
				config.TransactionalID = string([]byte{0xff})
			},
		},
		{
			name: "control character transactional ID",
			change: func(config *ProducerConfig) {
				config.TransactionalID = "track\noutbox"
			},
		},
		{
			name: "transaction timeout without transactional ID",
			change: func(config *ProducerConfig) {
				config.TransactionTimeout = time.Second
			},
		},
		{
			name: "short transaction timeout",
			change: func(config *ProducerConfig) {
				config.TransactionalID = "track-outbox"
				config.TransactionTimeout = 999 * time.Millisecond
			},
		},
		{
			name: "excessive transaction timeout",
			change: func(config *ProducerConfig) {
				config.TransactionalID = "track-outbox"
				config.TransactionTimeout = 16 * time.Minute
			},
		},
		{
			name: "short transaction end timeout",
			change: func(config *ProducerConfig) {
				config.TransactionalID = "track-outbox"
				config.TransactionEndTimeout = 999 * time.Millisecond
			},
		},
		{
			name: "excessive transaction end timeout",
			change: func(config *ProducerConfig) {
				config.TransactionalID = "track-outbox"
				config.TransactionEndTimeout = 3 * time.Minute
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			config := ProducerConfig{
				Brokers:  []string{"broker.internal:9092"},
				ClientID: "track",
			}
			test.change(&config)

			producer, err := NewProducer(config)

			if producer != nil {
				closeErr := producer.Close()
				t.Fatalf(
					"NewProducer() returned a producer with invalid configuration; close error = %v",
					closeErr,
				)
			}
			if !errors.Is(err, ErrInvalidProducerConfig) {
				t.Fatalf("NewProducer() error = %v, want %v", err, ErrInvalidProducerConfig)
			}
		})
	}
}

func TestNewProducerRejectsPartiallyConfiguredMessageLimits(t *testing.T) {
	t.Parallel()

	limits := DefaultMessageLimits()
	limits.MaxHeaders = 0

	producer, err := NewProducer(ProducerConfig{
		Brokers:  []string{"broker.internal:9092"},
		ClientID: "track",
		Limits:   limits,
	})

	if producer != nil {
		t.Fatal("NewProducer() returned a producer with invalid message limits")
	}
	if !errors.Is(err, ErrInvalidMessageLimits) {
		t.Fatalf("NewProducer() error = %v, want %v", err, ErrInvalidMessageLimits)
	}
}

func TestNewProducerPreservesClientConstructionFailure(t *testing.T) {
	t.Parallel()

	want := errors.New("client construction failed")
	producer, err := newProducer(
		ProducerConfig{
			Brokers:         []string{"broker.internal:9092"},
			ClientID:        "track",
			AllowedTopics:   []string{"events"},
			TransactionalID: "track-outbox-0",
		},
		func(...kgo.Opt) (*kgo.Client, error) {
			return nil, want
		},
	)

	if producer != nil {
		t.Fatal("newProducer() returned a producer after client construction failed")
	}
	if !errors.Is(err, want) {
		t.Fatalf("newProducer() error = %v, want %v", err, want)
	}
}

func TestNewProducerAppliesBoundedIdempotentDeliveryPolicy(t *testing.T) {
	t.Parallel()

	var franzClient *kgo.Client
	producer, err := newProducer(
		ProducerConfig{
			Brokers:       []string{"broker.internal:9092"},
			ClientID:      "track",
			AllowedTopics: []string{"events"},
		},
		func(options ...kgo.Opt) (*kgo.Client, error) {
			client, clientErr := kgo.NewClient(options...)
			franzClient = client

			return client, clientErr
		},
	)
	if err != nil {
		t.Fatalf("newProducer() error = %v", err)
	}
	defer closeProducerForTest(t, producer)

	if got := franzClient.OptValue(kgo.StopProducerOnDataLossDetected); got != true {
		t.Fatalf("StopProducerOnDataLossDetected = %v, want true", got)
	}
	if got := franzClient.OptValue(kgo.AllowIdempotentProduceCancellation); got != true {
		t.Fatalf("AllowIdempotentProduceCancellation = %v, want true", got)
	}
	if got := franzClient.OptValue(kgo.MetadataMinAge); got != 250*time.Millisecond {
		t.Fatalf("MetadataMinAge = %v, want 250ms", got)
	}
	retryBackoff, ok := franzClient.OptValue(kgo.RetryBackoffFn).(func(int) time.Duration)
	if !ok {
		t.Fatalf("RetryBackoffFn = %T, want func(int) time.Duration", franzClient.OptValue(kgo.RetryBackoffFn))
	}
	if got := retryBackoff(10); got < 800*time.Millisecond || got > time.Second {
		t.Fatalf("RetryBackoffFn(10) = %s, want [800ms, 1s]", got)
	}

	var transactionalClient *kgo.Client
	transactional, err := newProducer(
		ProducerConfig{
			Brokers:         []string{"broker.internal:9092"},
			ClientID:        "track-transaction",
			AllowedTopics:   []string{"events"},
			TransactionalID: "track-transaction-0",
		},
		func(options ...kgo.Opt) (*kgo.Client, error) {
			client, clientErr := kgo.NewClient(options...)
			transactionalClient = client

			return client, clientErr
		},
	)
	if err != nil {
		t.Fatalf("newProducer() transactional error = %v", err)
	}
	defer closeProducerForTest(t, transactional)
	if got := transactionalClient.OptValue(kgo.AllowIdempotentProduceCancellation); got != false {
		t.Fatalf("transactional AllowIdempotentProduceCancellation = %v, want false", got)
	}
}

func TestProducerBoundsDeliveryContextsAndDetachesAdmittedAsyncRecord(t *testing.T) {
	t.Parallel()

	const deliveryWaitTimeout = time.Minute
	backend := &recordingProducerBackend{}
	producer := &Producer{
		client:              backend,
		limits:              DefaultMessageLimits(),
		maxBatchRecords:     1,
		maxBatchBytes:       1 << 20,
		deliveryWaitTimeout: deliveryWaitTimeout,
	}
	record := ProducerRecord{Topic: "events", Key: []byte("key")}
	assertDeliveryDeadline := func(ctx context.Context) {
		t.Helper()
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("delivery context has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 59*time.Second || remaining > deliveryWaitTimeout {
			t.Fatalf("delivery deadline remaining = %s, want (59s, 1m]", remaining)
		}
	}

	if result := producer.PublishRecord(context.Background(), record); result.Err != nil {
		t.Fatalf("PublishRecord() error = %v", result.Err)
	}
	assertDeliveryDeadline(backend.syncContexts[0])

	if _, err := producer.PublishBatch(context.Background(), []ProducerRecord{record}); err != nil {
		t.Fatalf("PublishBatch() error = %v", err)
	}
	assertDeliveryDeadline(backend.syncContexts[1])

	callerCtx, cancelCaller := context.WithCancel(context.Background())
	delivery, err := producer.PublishAsync(callerCtx, record)
	if err != nil {
		t.Fatalf("PublishAsync() error = %v", err)
	}
	cancelCaller()
	if err := backend.asyncContexts[0].Err(); err != nil {
		t.Fatalf("admitted async context error = %v, want nil", err)
	}
	assertDeliveryDeadline(backend.asyncContexts[0])
	backend.completeAsync(0, 0, 1, nil)
	if result := <-delivery; result.Err != nil {
		t.Fatalf("PublishAsync() delivery error = %v", result.Err)
	}
}

func TestProducerAsyncCancellationDuringAdmissionRemainsAuthoritative(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{
		produceAdmissionStarted: make(chan struct{}),
		produceAdmissionRelease: make(chan struct{}),
	}
	producer := &Producer{
		client:              backend,
		limits:              DefaultMessageLimits(),
		deliveryWaitTimeout: time.Minute,
	}
	callerCtx, cancelCaller := context.WithCancel(context.Background())
	published := make(chan struct {
		delivery <-chan DeliveryResult
		err      error
	}, 1)
	go func() {
		delivery, err := producer.PublishAsync(callerCtx, ProducerRecord{
			Topic: "events",
			Key:   []byte("key"),
		})
		published <- struct {
			delivery <-chan DeliveryResult
			err      error
		}{delivery: delivery, err: err}
	}()
	<-backend.produceAdmissionStarted
	cancelCaller()
	close(backend.produceAdmissionRelease)

	result := <-published
	if result.err != nil {
		t.Fatalf("PublishAsync() error = %v", result.err)
	}
	if err := backend.asyncContexts[0].Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("admission context error = %v, want context.Canceled", err)
	}
	backend.completeAsync(0, 0, 0, backend.asyncContexts[0].Err())
	delivery := <-result.delivery
	var deliveryErr *DeliveryError
	if !errors.As(delivery.Err, &deliveryErr) ||
		deliveryErr.Category() != ErrorAmbiguous ||
		!errors.Is(delivery.Err, context.Canceled) {
		t.Fatalf("delivery error = %T %v, want ambiguous cancellation", delivery.Err, delivery.Err)
	}
}

func TestProducerPublishesMessageSynchronously(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{}
	producer := &Producer{client: backend, limits: DefaultMessageLimits()}
	message := Message{
		Topic: "track.tracking-event.v1",
		Key:   []byte("consignment-42"),
		Value: []byte(`{"event_id":"event-1"}`),
		Headers: []Header{
			{Key: "content-type", Value: []byte("application/json")},
			{Key: "schema-version", Value: []byte("1")},
		},
	}

	if err := producer.Publish(context.Background(), message); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	if len(backend.records) != 1 {
		t.Fatalf("Publish() records = %d, want 1", len(backend.records))
	}
	record := backend.records[0]
	if record.Topic != message.Topic {
		t.Fatalf("Publish() topic = %q, want %q", record.Topic, message.Topic)
	}
	if string(record.Key) != string(message.Key) {
		t.Fatalf("Publish() key = %q, want %q", record.Key, message.Key)
	}
	if string(record.Value) != string(message.Value) {
		t.Fatalf("Publish() value = %q, want %q", record.Value, message.Value)
	}
	if len(record.Headers) != len(message.Headers) {
		t.Fatalf("Publish() headers = %d, want %d", len(record.Headers), len(message.Headers))
	}
	for index, header := range record.Headers {
		if header.Key != message.Headers[index].Key {
			t.Fatalf("Publish() header %d key = %q, want %q", index, header.Key, message.Headers[index].Key)
		}
		if string(header.Value) != string(message.Headers[index].Value) {
			t.Fatalf(
				"Publish() header %d value = %q, want %q",
				index,
				header.Value,
				message.Headers[index].Value,
			)
		}
	}
}

func TestProducerPreservesDeliveryAndHealthFailures(t *testing.T) {
	t.Parallel()

	deliveryErr := errors.New("delivery failed")
	healthErr := errors.New("broker unavailable")
	backend := &recordingProducerBackend{
		deliveryErr: deliveryErr,
		healthErr:   healthErr,
	}
	producer := &Producer{client: backend, limits: DefaultMessageLimits()}

	if err := producer.Publish(context.Background(), Message{
		Topic: "events",
		Value: []byte("payload"),
	}); !errors.Is(err, deliveryErr) {
		t.Fatalf("Publish() error = %v, want %v", err, deliveryErr)
	}
	if err := producer.Health(context.Background()); !errors.Is(err, healthErr) {
		t.Fatalf("Health() error = %v, want %v", err, healthErr)
	}
}

func TestProducerRunsSerializedTransaction(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{}
	producer := &Producer{
		client:                backend,
		limits:                DefaultMessageLimits(),
		transactionsEnabled:   true,
		transactionEndTimeout: time.Second,
	}

	err := producer.RunTransaction(context.Background(), func(transaction Transaction) error {
		return transaction.Publish(context.Background(), Message{
			Topic: "events",
			Key:   []byte("aggregate-1"),
			Value: []byte("payload"),
		})
	})

	if err != nil {
		t.Fatalf("RunTransaction() error = %v", err)
	}
	if backend.begins != 1 ||
		len(backend.records) != 1 ||
		len(backend.endTries) != 1 ||
		backend.endTries[0] != kgo.TryCommit ||
		backend.aborts != 0 {
		t.Fatalf("transaction backend = %#v", backend)
	}
}

func TestProducerAbortsFailedOrPanickingTransaction(t *testing.T) {
	t.Parallel()

	callbackErr := errors.New("application failed")
	tests := []struct {
		name     string
		callback func(Transaction) error
		want     error
	}{
		{
			name:     "callback error",
			callback: func(Transaction) error { return callbackErr },
			want:     callbackErr,
		},
		{
			name: "callback panic",
			callback: func(Transaction) error {
				panic("secret transaction state")
			},
			want: ErrTransactionPanic,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			backend := &recordingProducerBackend{}
			producer := &Producer{
				client:                backend,
				limits:                DefaultMessageLimits(),
				transactionsEnabled:   true,
				transactionEndTimeout: time.Second,
			}
			err := producer.RunTransaction(context.Background(), test.callback)

			if !errors.Is(err, test.want) ||
				strings.Contains(err.Error(), "secret transaction state") ||
				backend.begins != 1 ||
				backend.aborts != 1 ||
				len(backend.endTries) != 1 ||
				backend.endTries[0] != kgo.TryAbort {
				t.Fatalf("error/backend = %v/%#v", err, backend)
			}
		})
	}
}

func TestProducerRejectsUnavailableTransaction(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{}
	producer := &Producer{
		client:                backend,
		limits:                DefaultMessageLimits(),
		transactionEndTimeout: time.Second,
	}
	if err := producer.RunTransaction(context.Background(), func(Transaction) error {
		t.Fatal("callback called while transactions disabled")

		return nil
	}); !errors.Is(err, ErrTransactionsDisabled) {
		t.Fatalf("RunTransaction() error = %v, want %v", err, ErrTransactionsDisabled)
	}
	producer.transactionsEnabled = true
	if err := producer.RunTransaction(context.Background(), nil); !errors.Is(err, ErrTransactionRequired) {
		t.Fatalf("RunTransaction(nil) error = %v, want %v", err, ErrTransactionRequired)
	}

	backend.beginErr = errors.New("begin failed")
	if err := producer.RunTransaction(context.Background(), func(Transaction) error {
		return nil
	}); !errors.Is(err, backend.beginErr) {
		t.Fatalf("RunTransaction() begin error = %v, want %v", err, backend.beginErr)
	}
}

func TestProducerPreservesTransactionCleanupFailures(t *testing.T) {
	t.Parallel()

	callbackErr := errors.New("application failed")
	abortErr := errors.New("abort buffered failed")
	endErr := errors.New("end transaction failed")
	backend := &recordingProducerBackend{abortErr: abortErr, endErr: endErr}
	producer := &Producer{
		client:                backend,
		limits:                DefaultMessageLimits(),
		transactionsEnabled:   true,
		transactionEndTimeout: time.Second,
	}

	err := producer.RunTransaction(context.Background(), func(Transaction) error {
		return callbackErr
	})

	if !errors.Is(err, callbackErr) ||
		!errors.Is(err, abortErr) ||
		!errors.Is(err, endErr) {
		t.Fatalf("RunTransaction() error = %v", err)
	}
}

func TestProducerClassifiesTransactionCommitFailure(t *testing.T) {
	t.Parallel()

	unknownErr := errors.New("connection lost during commit")
	unknownBackend := &recordingProducerBackend{endErr: unknownErr}
	unknownProducer := transactionalProducer(unknownBackend)
	err := unknownProducer.RunTransaction(context.Background(), func(Transaction) error {
		return nil
	})
	if !errors.Is(err, unknownErr) ||
		!errors.Is(err, ErrTransactionOutcomeUnknown) ||
		unknownBackend.aborts != 0 ||
		len(unknownBackend.endTries) != 1 {
		t.Fatalf("unknown outcome error/backend = %v/%#v", err, unknownBackend)
	}

	abortableBackend := &recordingProducerBackend{
		endErrors: []error{kerr.TransactionAbortable, nil},
	}
	abortableProducer := transactionalProducer(abortableBackend)
	err = abortableProducer.RunTransaction(context.Background(), func(Transaction) error {
		return nil
	})
	if !errors.Is(err, kerr.TransactionAbortable) ||
		errors.Is(err, ErrTransactionOutcomeUnknown) ||
		abortableBackend.aborts != 1 ||
		len(abortableBackend.endTries) != 2 ||
		abortableBackend.endTries[0] != kgo.TryCommit ||
		abortableBackend.endTries[1] != kgo.TryAbort {
		t.Fatalf("abortable error/backend = %v/%#v", err, abortableBackend)
	}
}

func TestTransactionCannotPublishAfterCallbackReturns(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{}
	producer := transactionalProducer(backend)
	var retained Transaction
	if err := producer.RunTransaction(context.Background(), func(transaction Transaction) error {
		retained = transaction

		return nil
	}); err != nil {
		t.Fatalf("RunTransaction() error = %v", err)
	}

	err := retained.Publish(context.Background(), Message{Topic: "events"})
	if !errors.Is(err, ErrTransactionClosed) || len(backend.records) != 0 {
		t.Fatalf("retained Publish() error/backend = %v/%#v", err, backend)
	}
	if err := (Transaction{}).Publish(context.Background(), Message{
		Topic: "events",
	}); !errors.Is(err, ErrTransactionClosed) {
		t.Fatalf("zero Transaction.Publish() error = %v, want %v", err, ErrTransactionClosed)
	}
}

func TestTransactionWaitsForStartedPublishBeforeCommit(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{
		produceStarted: make(chan struct{}),
		produceRelease: make(chan struct{}),
	}
	producer := transactionalProducer(backend)
	publishDone := make(chan error, 1)
	transactionDone := make(chan error, 1)
	go func() {
		transactionDone <- producer.RunTransaction(
			context.Background(),
			func(transaction Transaction) error {
				go func() {
					publishDone <- transaction.Publish(context.Background(), Message{
						Topic: "events",
					})
				}()
				<-backend.produceStarted

				return nil
			},
		)
	}()
	<-backend.produceStarted
	select {
	case err := <-transactionDone:
		t.Fatalf("transaction completed before in-flight publish: %v", err)
	default:
	}
	close(backend.produceRelease)
	if err := <-publishDone; err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if err := <-transactionDone; err != nil {
		t.Fatalf("RunTransaction() error = %v", err)
	}
	if len(backend.endTries) != 1 || backend.endTries[0] != kgo.TryCommit {
		t.Fatalf("transaction backend = %#v", backend)
	}
}

func transactionalProducer(backend producerBackend) *Producer {
	return &Producer{
		client:                backend,
		limits:                DefaultMessageLimits(),
		transactionsEnabled:   true,
		deliveryWaitTimeout:   time.Second,
		transactionEndTimeout: time.Second,
	}
}

func TestRunTransactionTerminatesAfterAmbiguousPublishDeadline(t *testing.T) {
	t.Parallel()

	clientCtx, cancelClient := context.WithCancel(context.Background())
	release := make(chan struct{})
	releaseTimer := time.AfterFunc(250*time.Millisecond, func() { close(release) })
	defer releaseTimer.Stop()
	backend := &recordingProducerBackend{
		produceSync: func(
			_ context.Context,
			records ...*kgo.Record,
		) kgo.ProduceResults {
			select {
			case <-clientCtx.Done():
				return kgo.ProduceResults{{
					Record: records[0],
					Err:    kgo.ErrClientClosed,
				}}
			case <-release:
				return kgo.ProduceResults{{Record: records[0]}}
			}
		},
	}
	producer := transactionalProducer(backend)
	producer.deliveryWaitTimeout = 20 * time.Millisecond
	producer.cancelClient = cancelClient

	err := producer.RunTransaction(
		context.Background(),
		func(transaction Transaction) error {
			return transaction.Publish(context.Background(), ProducerRecord{
				Topic: "events",
				Key:   []byte("key"),
				Value: []byte("value"),
			})
		},
	)
	var deliveryErr *DeliveryError
	if !errors.Is(err, ErrProducerFatal) ||
		!errors.Is(err, context.DeadlineExceeded) ||
		!errors.As(err, &deliveryErr) ||
		deliveryErr.Category() != ErrorAmbiguous ||
		backend.aborts != 0 || len(backend.endTries) != 0 || backend.closes != 1 {
		t.Fatalf("RunTransaction() error/backend = %v/%#v", err, backend)
	}
	if err := producer.RunTransaction(
		context.Background(),
		func(Transaction) error { return nil },
	); !errors.Is(err, ErrProducerFatal) {
		t.Fatalf("RunTransaction() after fatal delivery = %v", err)
	}
	if err := producer.Publish(context.Background(), ProducerRecord{
		Topic: "events",
		Key:   []byte("key"),
		Value: []byte("value"),
	}); !errors.Is(err, ErrProducerFatal) {
		t.Fatalf("Publish() after fatal delivery = %v", err)
	}
	if err := producer.Abort(context.Background()); !errors.Is(err, ErrProducerFatal) {
		t.Fatalf("Abort() after fatal delivery = %v", err)
	}
	if err := producer.Close(); err != nil {
		t.Fatalf("Close() after fatal delivery = %v", err)
	}
}

func TestProducerCloseDoesNotReportSuccessWhileTerminalCloseIsRunning(t *testing.T) {
	t.Parallel()

	clientCtx, cancelClient := context.WithCancel(context.Background())
	closeStarted := make(chan struct{})
	closeRelease := make(chan struct{})
	backend := &recordingProducerBackend{
		closeStarted: closeStarted,
		closeRelease: closeRelease,
		produceSync: func(
			_ context.Context,
			records ...*kgo.Record,
		) kgo.ProduceResults {
			<-clientCtx.Done()

			return kgo.ProduceResults{{
				Record: records[0],
				Err:    kgo.ErrClientClosed,
			}}
		},
	}
	producer := transactionalProducer(backend)
	producer.deliveryWaitTimeout = 20 * time.Millisecond
	producer.cancelClient = cancelClient
	transactionDone := make(chan error, 1)
	go func() {
		transactionDone <- producer.RunTransaction(
			context.Background(),
			func(transaction Transaction) error {
				return transaction.Publish(context.Background(), ProducerRecord{
					Topic: "events",
					Key:   []byte("key"),
					Value: []byte("value"),
				})
			},
		)
	}()
	<-closeStarted
	deadline := time.Now().Add(time.Second)
	for producer.fatalError() == nil {
		if time.Now().After(deadline) {
			close(closeRelease)
			t.Fatal("producer did not publish terminal state")
		}
		runtime.Gosched()
	}

	if err := producer.Close(); !errors.Is(err, ErrProducerBusy) {
		t.Fatalf("Close() during terminal close = %v, want %v", err, ErrProducerBusy)
	}
	close(closeRelease)
	if err := <-transactionDone; !errors.Is(err, ErrProducerFatal) {
		t.Fatalf("RunTransaction() error = %v", err)
	}
	if err := producer.Close(); err != nil {
		t.Fatalf("Close() after terminal close = %v", err)
	}
}

func TestTransactionalPublishPreservesExistingFatalState(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{}
	producer := transactionalProducer(backend)
	producer.allowedTopics = map[string]struct{}{"events": {}}
	producer.fatalErr = errors.Join(ErrProducerFatal, context.DeadlineExceeded)

	err := producer.publish(context.Background(), ProducerRecord{
		Topic: "events",
		Key:   []byte("key"),
		Value: []byte("value"),
	})
	if !errors.Is(err, ErrProducerFatal) ||
		!errors.Is(err, context.DeadlineExceeded) || len(backend.records) != 1 {
		t.Fatalf("publish() with existing fatal state = %v/%#v", err, backend)
	}
}

func TestTransactionalPublishRejectsCanceledContextBeforeAdmission(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{}
	producer := transactionalProducer(backend)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := producer.RunTransaction(
		context.Background(),
		func(transaction Transaction) error {
			return transaction.Publish(ctx, ProducerRecord{
				Topic: "events",
				Key:   []byte("key"),
				Value: []byte("value"),
			})
		},
	)
	var deliveryErr *DeliveryError
	if !errors.Is(err, context.Canceled) ||
		!errors.As(err, &deliveryErr) ||
		deliveryErr.Category() != ErrorAmbiguous ||
		errors.Is(err, ErrProducerFatal) || len(backend.records) != 0 ||
		backend.aborts != 1 || len(backend.endTries) != 1 ||
		backend.endTries[0] != kgo.TryAbort {
		t.Fatalf("canceled transaction publish = %v/%#v", err, backend)
	}
}

func TestTransactionalPublishRejectsMissingDeliveryResult(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{omitDeliveries: true}
	producer := transactionalProducer(backend)

	err := producer.RunTransaction(
		context.Background(),
		func(transaction Transaction) error {
			return transaction.Publish(context.Background(), ProducerRecord{
				Topic: "events",
				Key:   []byte("key"),
				Value: []byte("value"),
			})
		},
	)
	var deliveryErr *DeliveryError
	if !errors.Is(err, ErrDeliveryResultMissing) ||
		!errors.As(err, &deliveryErr) ||
		deliveryErr.Category() != ErrorAmbiguous ||
		errors.Is(err, ErrProducerFatal) || backend.aborts != 1 ||
		len(backend.endTries) != 1 || backend.endTries[0] != kgo.TryAbort {
		t.Fatalf("missing transaction delivery = %v/%#v", err, backend)
	}
}

func TestProducerRejectsMessageWithoutTopic(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{}
	producer := &Producer{client: backend, limits: DefaultMessageLimits()}

	err := producer.Publish(context.Background(), Message{Value: []byte("payload")})

	if !errors.Is(err, ErrTopicRequired) {
		t.Fatalf("Publish() error = %v, want %v", err, ErrTopicRequired)
	}
	if len(backend.records) != 0 {
		t.Fatalf("Publish() records = %d, want 0", len(backend.records))
	}
}

func TestProducerRejectsTopicAboveConfiguredLimit(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{}
	limits := DefaultMessageLimits()
	producer := &Producer{client: backend, limits: limits}

	err := producer.Publish(context.Background(), Message{
		Topic: strings.Repeat("t", limits.MaxTopicBytes+1),
		Value: []byte("payload"),
	})

	if !errors.Is(err, ErrTopicTooLarge) {
		t.Fatalf("Publish() error = %v, want %v", err, ErrTopicTooLarge)
	}
	if len(backend.records) != 0 {
		t.Fatalf("Publish() records = %d, want 0", len(backend.records))
	}
}

func TestProducerRejectsBrokerInvalidTopicName(t *testing.T) {
	t.Parallel()

	for _, topic := range []string{".", "..", "events/commands", "events\x00", "events-\xff"} {
		topic := topic
		t.Run(topic, func(t *testing.T) {
			t.Parallel()

			backend := &recordingProducerBackend{}
			producer := &Producer{client: backend, limits: DefaultMessageLimits()}

			err := producer.Publish(context.Background(), Message{Topic: topic})

			if !errors.Is(err, ErrInvalidTopic) {
				t.Fatalf("Publish() error = %v, want %v", err, ErrInvalidTopic)
			}
			if len(backend.records) != 0 {
				t.Fatalf("Publish() records = %d, want 0", len(backend.records))
			}
		})
	}
}

func TestProducerRejectsMessageFieldsAboveConfiguredLimits(t *testing.T) {
	t.Parallel()

	limits := DefaultMessageLimits()
	manyHeaders := make([]Header, limits.MaxHeaders+1)
	for index := range manyHeaders {
		manyHeaders[index] = Header{Key: "key"}
	}
	largeHeaders := make([]Header, 5)
	for index := range largeHeaders {
		largeHeaders[index] = Header{
			Key:   "key",
			Value: []byte(strings.Repeat("v", limits.MaxHeaderValueBytes)),
		}
	}

	tests := []struct {
		name    string
		message Message
		want    error
	}{
		{
			name: "key",
			message: Message{
				Topic: "events",
				Key:   []byte(strings.Repeat("k", limits.MaxKeyBytes+1)),
			},
			want: ErrKeyTooLarge,
		},
		{
			name: "value",
			message: Message{
				Topic: "events",
				Value: []byte(strings.Repeat("v", limits.MaxValueBytes+1)),
			},
			want: ErrValueTooLarge,
		},
		{
			name: "header count",
			message: Message{
				Topic:   "events",
				Headers: manyHeaders,
			},
			want: ErrTooManyHeaders,
		},
		{
			name: "empty header key",
			message: Message{
				Topic:   "events",
				Headers: []Header{{Value: []byte("value")}},
			},
			want: ErrHeaderKeyRequired,
		},
		{
			name: "header key",
			message: Message{
				Topic: "events",
				Headers: []Header{{
					Key: strings.Repeat("k", limits.MaxHeaderKeyBytes+1),
				}},
			},
			want: ErrHeaderKeyTooLarge,
		},
		{
			name: "header value",
			message: Message{
				Topic: "events",
				Headers: []Header{{
					Key:   "key",
					Value: []byte(strings.Repeat("v", limits.MaxHeaderValueBytes+1)),
				}},
			},
			want: ErrHeaderValueTooLarge,
		},
		{
			name: "aggregate headers",
			message: Message{
				Topic:   "events",
				Headers: largeHeaders,
			},
			want: ErrHeadersTooLarge,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			backend := &recordingProducerBackend{}
			producer := &Producer{client: backend, limits: limits}

			err := producer.Publish(context.Background(), test.message)

			if !errors.Is(err, test.want) {
				t.Fatalf("Publish() error = %v, want %v", err, test.want)
			}
			if len(backend.records) != 0 {
				t.Fatalf("Publish() records = %d, want 0", len(backend.records))
			}
		})
	}
}

func TestProducerRejectsAggregateHeaderKeyOverflow(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{}
	limits := DefaultMessageLimits()
	limits.MaxHeaderBytes = 5
	producer := &Producer{client: backend, limits: limits}

	err := producer.Publish(context.Background(), Message{
		Topic: "events",
		Headers: []Header{
			{Key: "key", Value: []byte("12")},
			{Key: "x"},
		},
	})

	if !errors.Is(err, ErrHeadersTooLarge) {
		t.Fatalf("Publish() error = %v, want %v", err, ErrHeadersTooLarge)
	}
	if len(backend.records) != 0 {
		t.Fatalf("Publish() records = %d, want 0", len(backend.records))
	}
}

func closeProducerForTest(t *testing.T, producer *Producer) {
	t.Helper()
	if err := producer.Close(); err != nil {
		t.Errorf("Producer.Close() error = %v", err)
	}
}

type recordingProducerBackend struct {
	records                 []*kgo.Record
	syncContexts            []context.Context
	asyncContexts           []context.Context
	deliveryErr             error
	deliveryErrors          []error
	healthErr               error
	beginErr                error
	abortErr                error
	endErr                  error
	endErrors               []error
	begins                  int
	aborts                  int
	endTries                []kgo.TransactionEndTry
	produceStarted          chan struct{}
	produceRelease          chan struct{}
	produceAdmissionStarted chan struct{}
	produceAdmissionRelease chan struct{}
	prepareDelivery         func(*kgo.Record)
	asyncPromises           []func(*kgo.Record, error)
	asyncRecords            []*kgo.Record
	closes                  int
	flushes                 int
	flushErr                error
	flushStarted            chan struct{}
	flushRelease            chan struct{}
	flushSignalOnce         sync.Once
	flushCompletesAsync     bool
	closeCompletesAsync     bool
	closeStarted            chan struct{}
	closeRelease            chan struct{}
	closeSignalOnce         sync.Once
	omitDeliveries          bool
	produceSync             func(context.Context, ...*kgo.Record) kgo.ProduceResults
}

func (backend *recordingProducerBackend) ProduceSync(
	ctx context.Context,
	records ...*kgo.Record,
) kgo.ProduceResults {
	backend.syncContexts = append(backend.syncContexts, ctx)
	backend.records = append(backend.records, records...)
	if backend.produceSync != nil {
		return backend.produceSync(ctx, records...)
	}
	if backend.produceStarted != nil {
		close(backend.produceStarted)
		<-backend.produceRelease
	}
	if backend.omitDeliveries {
		return nil
	}

	results := make(kgo.ProduceResults, len(records))
	for index, record := range records {
		if backend.prepareDelivery != nil {
			backend.prepareDelivery(record)
		}
		deliveryErr := backend.deliveryErr
		if index < len(backend.deliveryErrors) {
			deliveryErr = backend.deliveryErrors[index]
		}
		results[index] = kgo.ProduceResult{Record: record, Err: deliveryErr}
	}

	return results
}

func (backend *recordingProducerBackend) Produce(
	ctx context.Context,
	record *kgo.Record,
	promise func(*kgo.Record, error),
) {
	if backend.produceAdmissionStarted != nil {
		close(backend.produceAdmissionStarted)
		<-backend.produceAdmissionRelease
	}
	backend.asyncContexts = append(backend.asyncContexts, ctx)
	backend.records = append(backend.records, record)
	backend.asyncRecords = append(backend.asyncRecords, record)
	backend.asyncPromises = append(backend.asyncPromises, promise)
}

func (backend *recordingProducerBackend) completeAsync(
	index int,
	partition int32,
	offset int64,
	err error,
) {
	record := backend.asyncRecords[index]
	record.Partition = partition
	record.Offset = offset
	backend.asyncPromises[index](record, err)
}

func (backend *recordingProducerBackend) completeAsyncMissing(index int, err error) {
	backend.asyncPromises[index](nil, err)
}

func (backend *recordingProducerBackend) Ping(context.Context) error {
	return backend.healthErr
}

func (backend *recordingProducerBackend) Flush(context.Context) error {
	backend.flushes++
	if backend.flushStarted != nil {
		backend.flushSignalOnce.Do(func() {
			close(backend.flushStarted)
			<-backend.flushRelease
		})
	}
	if backend.flushCompletesAsync {
		backend.completePendingAsync(nil)
	}

	return backend.flushErr
}

func (backend *recordingProducerBackend) BeginTransaction() error {
	backend.begins++

	return backend.beginErr
}

func (backend *recordingProducerBackend) AbortBufferedRecords(context.Context) error {
	backend.aborts++

	return backend.abortErr
}

func (backend *recordingProducerBackend) EndTransaction(
	_ context.Context,
	try kgo.TransactionEndTry,
) error {
	backend.endTries = append(backend.endTries, try)
	if len(backend.endErrors) != 0 {
		err := backend.endErrors[0]
		backend.endErrors = backend.endErrors[1:]

		return err
	}

	return backend.endErr
}

func (backend *recordingProducerBackend) Close() {
	backend.closes++
	if backend.closeStarted != nil {
		backend.closeSignalOnce.Do(func() { close(backend.closeStarted) })
		<-backend.closeRelease
	}
	if backend.closeCompletesAsync {
		backend.completePendingAsync(kgo.ErrClientClosed)
	}
}

func (backend *recordingProducerBackend) completePendingAsync(err error) {
	for index, promise := range backend.asyncPromises {
		promise(backend.asyncRecords[index], err)
	}
	backend.asyncPromises = nil
	backend.asyncRecords = nil
}
