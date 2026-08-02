// Package kafka provides bounded Apache Kafka producer and consumer
// composition for Go services.
package kafka

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

var (
	ErrBrokersRequired           = errors.New("kafka: at least one broker is required")
	ErrTooManyBrokers            = errors.New("kafka: broker count exceeds configured limit")
	ErrInvalidBroker             = errors.New("kafka: broker address is invalid")
	ErrDuplicateBroker           = errors.New("kafka: broker address is duplicated")
	ErrClientIDRequired          = errors.New("kafka: client ID is required")
	ErrClientIDTooLarge          = errors.New("kafka: client ID exceeds configured limit")
	ErrInvalidClientID           = errors.New("kafka: client ID is invalid")
	ErrTopicRequired             = errors.New("kafka: topic is required")
	ErrTopicTooLarge             = errors.New("kafka: topic exceeds configured limit")
	ErrInvalidTopic              = errors.New("kafka: topic name is invalid")
	ErrTopicNotAllowed           = errors.New("kafka: topic is outside producer allowlist")
	ErrInvalidPartitionSelection = errors.New(
		"kafka: producer partition selection is invalid",
	)
	ErrKeyRequired          = errors.New("kafka: record key is required by producer policy")
	ErrKeyTooLarge          = errors.New("kafka: key exceeds configured limit")
	ErrValueTooLarge        = errors.New("kafka: value exceeds configured limit")
	ErrTooManyHeaders       = errors.New("kafka: header count exceeds configured limit")
	ErrHeaderKeyRequired    = errors.New("kafka: header key is required")
	ErrHeaderKeyTooLarge    = errors.New("kafka: header key exceeds configured limit")
	ErrHeaderValueTooLarge  = errors.New("kafka: header value exceeds configured limit")
	ErrHeadersTooLarge      = errors.New("kafka: headers exceed aggregate configured limit")
	ErrInvalidMessageLimits = errors.New(
		"kafka: all message limits must be positive",
	)
	ErrInvalidProducerConfig = errors.New(
		"kafka: producer configuration is outside bounded limits",
	)
	ErrInvalidCompressionPreference = errors.New(
		"kafka: producer compression preference is invalid",
	)
	ErrTransactionsDisabled      = errors.New("kafka: producer transactions are disabled")
	ErrTransactionRequired       = errors.New("kafka: transaction callback is required")
	ErrTransactionPanic          = errors.New("kafka: transaction callback panicked")
	ErrTransactionClosed         = errors.New("kafka: transaction is closed")
	ErrTransactionOutcomeUnknown = errors.New(
		"kafka: transaction commit outcome is unknown",
	)
	ErrDeliveryResultMissing = errors.New("kafka: producer omitted a delivery result")
	ErrDeliveryResultInvalid = errors.New("kafka: producer returned inconsistent delivery results")
	ErrRecordsRequired       = errors.New("kafka: at least one producer record is required")
	ErrTooManyBatchRecords   = errors.New("kafka: producer batch record count exceeds configured limit")
	ErrBatchTooLarge         = errors.New("kafka: producer batch exceeds configured byte limit")
	ErrBatchDeliveryFailed   = errors.New("kafka: one or more batch records failed delivery")
	ErrContextRequired       = errors.New("kafka: context is required")
	ErrProducerClosed        = errors.New("kafka: producer is closed")
	ErrProducerFatal         = errors.New("kafka: producer entered a fatal state")
	ErrProducerBusy          = errors.New("kafka: producer has in-flight operations")
	ErrTransactionInProgress = errors.New("kafka: producer transaction is in progress")
	ErrDrainIncomplete       = errors.New("kafka: producer drain is incomplete")
)

const defaultPartitionerBatchBytes = 65_536

// KeyPolicy controls whether a producer accepts records without keys.
type KeyPolicy uint8

const (
	// KeyRequired is the safe default and preserves a stable partition-ordering
	// identity for every record.
	KeyRequired KeyPolicy = iota
	// UnkeyedAllowed explicitly permits records for which the configured
	// partitioner chooses a partition without a key.
	UnkeyedAllowed
)

// CompressionCodec identifies one Kafka record-batch compression algorithm.
type CompressionCodec uint8

const (
	// CompressionNone disables record-batch compression.
	CompressionNone CompressionCodec = iota + 1
	// CompressionGzip selects gzip compression.
	CompressionGzip
	// CompressionSnappy selects snappy compression.
	CompressionSnappy
	// CompressionLz4 selects LZ4 compression.
	CompressionLz4
	// CompressionZstd selects Zstandard compression.
	CompressionZstd
)

// String returns the stable compression policy name.
func (codec CompressionCodec) String() string {
	switch codec {
	case CompressionNone:
		return "none"
	case CompressionGzip:
		return "gzip"
	case CompressionSnappy:
		return "snappy"
	case CompressionLz4:
		return "lz4"
	case CompressionZstd:
		return "zstd"
	default:
		return "unknown"
	}
}

// ProducerConfig defines bounded Kafka producer identity, routing, delivery,
// lifecycle, and security policy. AllowedTopics is required and copied during
// construction.
type ProducerConfig struct {
	Brokers                []string
	ClientID               string
	Protocol               ProtocolPolicy
	AllowedTopics          []string
	KeyPolicy              KeyPolicy
	Limits                 MessageLimits
	MaxBufferedRecords     int
	MaxBufferedBytes       int
	MaxBatchRecords        int
	MaxBatchBytes          int32
	RecordRetries          int
	RetryBackoffMin        time.Duration
	RetryBackoffMax        time.Duration
	DeliveryTimeout        time.Duration
	ShutdownTimeout        time.Duration
	RequestTimeout         time.Duration
	DialTimeout            time.Duration
	Linger                 time.Duration
	CompressionPreferences []CompressionCodec
	TransactionalID        string
	TransactionTimeout     time.Duration

	TransactionEndTimeout time.Duration
	Security              ClientSecurity
	Observers             ObserverPolicy
}

// Validate reports whether the producer configuration satisfies the bounded
// producer policy without constructing a client or dialing brokers.
func (config ProducerConfig) Validate() error {
	_, err := normalizeProducerConfig(config)

	return err
}

// MessageLimits bounds caller-controlled record fields before they reach the
// client buffer.
type MessageLimits struct {
	MaxTopicBytes       int
	MaxKeyBytes         int
	MaxValueBytes       int
	MaxHeaders          int
	MaxHeaderKeyBytes   int
	MaxHeaderValueBytes int
	MaxHeaderBytes      int
}

// DefaultMessageLimits returns conservative limits below Kafka's default
// one-megabyte broker record limit.
func DefaultMessageLimits() MessageLimits {
	return MessageLimits{
		MaxTopicBytes:       249,
		MaxKeyBytes:         64 << 10,
		MaxValueBytes:       900 << 10,
		MaxHeaders:          64,
		MaxHeaderKeyBytes:   128,
		MaxHeaderValueBytes: 8 << 10,
		MaxHeaderBytes:      32 << 10,
	}
}

// Validate reports whether every message limit is positive.
func (limits MessageLimits) Validate() error {
	if limits.MaxTopicBytes <= 0 ||
		limits.MaxTopicBytes > 249 ||
		limits.MaxKeyBytes <= 0 ||
		limits.MaxKeyBytes > 16<<20 ||
		limits.MaxValueBytes <= 0 ||
		limits.MaxValueBytes > 100<<20 ||
		limits.MaxHeaders <= 0 ||
		limits.MaxHeaders > 10_000 ||
		limits.MaxHeaderKeyBytes <= 0 ||
		limits.MaxHeaderKeyBytes > 64<<10 ||
		limits.MaxHeaderValueBytes <= 0 ||
		limits.MaxHeaderValueBytes > 100<<20 ||
		limits.MaxHeaderBytes <= 0 ||
		limits.MaxHeaderBytes > 100<<20 {
		return ErrInvalidMessageLimits
	}

	return nil
}

// Header is one ordered Kafka record header.
type Header struct {
	Key   string
	Value []byte
}

// DeliveryResult reports the broker outcome for one produced record. A nil Err
// means Kafka acknowledged the record under the configured acknowledgement
// policy; it does not mean an application side effect consumed the record.
type DeliveryResult struct {
	Topic     string
	Partition int32
	Offset    int64
	Timestamp time.Time
	Err       error
}

type producerBackend interface {
	ProduceSync(context.Context, ...*kgo.Record) kgo.ProduceResults
	Produce(context.Context, *kgo.Record, func(*kgo.Record, error))
	Flush(context.Context) error
	Ping(context.Context) error
	BeginTransaction() error
	AbortBufferedRecords(context.Context) error
	EndTransaction(context.Context, kgo.TransactionEndTry) error
	Close()
}

type policyPartitioner struct {
	automatic kgo.Partitioner
}

func newPolicyPartitioner(automatic kgo.Partitioner) kgo.Partitioner {
	return policyPartitioner{automatic: automatic}
}

func (partitioner policyPartitioner) ForTopic(topic string) kgo.TopicPartitioner {
	return &policyTopicPartitioner{
		automatic: partitioner.automatic.ForTopic(topic),
	}
}

type policyTopicPartitioner struct {
	automatic kgo.TopicPartitioner
	// franz-go serializes partitioner calls for one topic, so this state is
	// owned by that topic's producer path and requires no separate lock.
	lastAutomatic bool
}

func (partitioner *policyTopicPartitioner) RequiresConsistency(record *kgo.Record) bool {
	if record.Partition >= 0 {
		return true
	}

	return partitioner.automatic.RequiresConsistency(record)
}

func (partitioner *policyTopicPartitioner) Partition(record *kgo.Record, count int) int {
	if record.Partition >= 0 {
		partitioner.lastAutomatic = false

		return int(record.Partition)
	}
	partitioner.lastAutomatic = true

	return partitioner.automatic.Partition(record, count)
}

func (partitioner *policyTopicPartitioner) PartitionByBackup(
	record *kgo.Record,
	count int,
	backup kgo.TopicBackupIter,
) int {
	if record.Partition >= 0 {
		partitioner.lastAutomatic = false

		return int(record.Partition)
	}
	partitioner.lastAutomatic = true
	if automatic, ok := partitioner.automatic.(kgo.TopicBackupPartitioner); ok {
		return automatic.PartitionByBackup(record, count, backup)
	}

	return partitioner.automatic.Partition(record, count)
}

func (partitioner *policyTopicPartitioner) OnNewBatch() {
	if !partitioner.lastAutomatic {
		return
	}
	if automatic, ok := partitioner.automatic.(kgo.TopicPartitionerOnNewBatch); ok {
		automatic.OnNewBatch()
	}
}

type producerClientFactory func(...kgo.Opt) (*kgo.Client, error)

// Producer publishes records with Kafka's idempotent producer and all in-sync
// replica acknowledgements.
type Producer struct {
	client                producerBackend
	clientID              string
	limits                MessageLimits
	keyRequired           bool
	maxBatchRecords       int
	maxBatchBytes         int64
	deliveryWaitTimeout   time.Duration
	transactionsEnabled   bool
	transactionEndTimeout time.Duration
	shutdownTimeout       time.Duration
	allowedTopics         map[string]struct{}
	stateMu               sync.Mutex
	closed                bool
	transactionActive     bool
	maintenanceActive     bool
	shutdownComplete      bool
	inflight              int
	admitting             int
	observerCallbacks     int
	admissionsDone        chan struct{}
	closeOnce             sync.Once
	observers             observerDispatcher
	cancelClient          context.CancelFunc
	fatalErr              error
}

// NewProducer constructs a producer without dialing brokers. Connectivity is
// established lazily by franz-go.
func NewProducer(config ProducerConfig) (*Producer, error) {
	return newProducer(config, kgo.NewClient)
}

func newProducer(
	config ProducerConfig,
	factory producerClientFactory,
) (*Producer, error) {
	config, err := normalizeProducerConfig(config)
	if err != nil {
		return nil, err
	}

	clientCtx, cancelClient := context.WithCancel(context.Background())
	options := []kgo.Opt{
		kgo.WithContext(clientCtx),
		kgo.SeedBrokers(config.Brokers...),
		kgo.AlwaysRetryEOF(),
		kgo.ClientID(config.ClientID),
		kgo.RecordPartitioner(newPolicyPartitioner(
			kgo.UniformBytesPartitioner(defaultPartitionerBatchBytes, true, true, nil),
		)),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.StopProducerOnDataLossDetected(),
		kgo.MaxBufferedRecords(config.MaxBufferedRecords),
		kgo.MaxBufferedBytes(config.MaxBufferedBytes),
		kgo.ProducerBatchMaxBytes(config.MaxBatchBytes),
		kgo.RecordRetries(config.RecordRetries),
		kgo.RetryBackoffFn(newProducerRetryBackoff(
			config.ClientID,
			config.RetryBackoffMin,
			config.RetryBackoffMax,
		)),
		kgo.MetadataMinAge(producerMetadataMinAge(config.RetryBackoffMin)),
		kgo.RecordDeliveryTimeout(config.DeliveryTimeout),
		kgo.ProduceRequestTimeout(config.RequestTimeout),
		kgo.DialTimeout(config.DialTimeout),
		kgo.ProducerLinger(config.Linger),
		kgo.ProducerBatchCompression(
			franzCompressionCodecs(config.CompressionPreferences)...,
		),
	}
	if config.TransactionalID != "" {
		options = append(options,
			kgo.TransactionalID(config.TransactionalID),
			kgo.TransactionTimeout(config.TransactionTimeout),
		)
	} else {
		options = append(options, kgo.AllowIdempotentProduceCancellation())
	}
	options = append(options, clientProtocolOptions(config.Protocol)...)
	options = append(options, clientSecurityOptions(config.Security, config.DialTimeout)...)
	dispatcher := newObserverDispatcher(config.Observers)
	allowedTopics := make(map[string]struct{}, len(config.AllowedTopics))
	for _, topic := range config.AllowedTopics {
		allowedTopics[topic] = struct{}{}
	}
	producer := &Producer{
		clientID:              strings.Clone(config.ClientID),
		limits:                config.Limits,
		keyRequired:           config.KeyPolicy == KeyRequired,
		maxBatchRecords:       config.MaxBatchRecords,
		maxBatchBytes:         int64(config.MaxBatchBytes),
		deliveryWaitTimeout:   config.DeliveryTimeout + config.RetryBackoffMax,
		transactionsEnabled:   config.TransactionalID != "",
		transactionEndTimeout: config.TransactionEndTimeout,
		shutdownTimeout:       config.ShutdownTimeout,
		allowedTopics:         allowedTopics,
		observers:             dispatcher,
		cancelClient:          cancelClient,
	}
	if dispatcher.enabled() {
		observerHook := newFranzObserverHook(config.ClientID, "", dispatcher)
		observerHook.before = producer.beginObservation
		observerHook.after = producer.finishObservation
		options = append(options, kgo.WithHooks(observerHook))
	}

	client, err := factory(options...)
	if err != nil {
		cancelClient()

		return nil, err
	}
	producer.client = client

	return producer, nil
}

func normalizeProducerConfig(config ProducerConfig) (ProducerConfig, error) {
	if err := validateClientIdentity(config.Brokers, config.ClientID); err != nil {
		return ProducerConfig{}, err
	}
	if err := config.Protocol.Validate(); err != nil {
		return ProducerConfig{}, err
	}
	security, err := normalizeClientSecurity(config.Security)
	if err != nil {
		return ProducerConfig{}, err
	}
	config.Security = security
	observers, err := normalizeObserverPolicy(config.Observers)
	if err != nil {
		return ProducerConfig{}, err
	}
	config.Observers = observers
	if config.Limits == (MessageLimits{}) {
		config.Limits = DefaultMessageLimits()
	}
	if err := config.Limits.Validate(); err != nil {
		return ProducerConfig{}, err
	}
	if config.KeyPolicy > UnkeyedAllowed {
		return ProducerConfig{}, ErrInvalidProducerConfig
	}
	if config.MaxBufferedRecords == 0 {
		config.MaxBufferedRecords = 1_000
	}
	if config.MaxBufferedBytes == 0 {
		config.MaxBufferedBytes = 64 << 20
	}
	if config.MaxBatchRecords == 0 {
		config.MaxBatchRecords = 100
	}
	if config.MaxBatchBytes == 0 {
		config.MaxBatchBytes = 1 << 20
	}
	if config.RecordRetries == 0 {
		config.RecordRetries = 10
	}
	if config.RetryBackoffMin == 0 {
		config.RetryBackoffMin = 250 * time.Millisecond
	}
	if config.RetryBackoffMax == 0 {
		config.RetryBackoffMax = time.Second
	}
	if config.DeliveryTimeout == 0 {
		config.DeliveryTimeout = 30 * time.Second
	}
	if config.ShutdownTimeout == 0 {
		config.ShutdownTimeout = config.DeliveryTimeout +
			config.RetryBackoffMax
	}
	if config.RequestTimeout == 0 {
		config.RequestTimeout = 10 * time.Second
	}
	if config.DialTimeout == 0 {
		config.DialTimeout = 10 * time.Second
	}
	if config.Linger == 0 {
		config.Linger = 5 * time.Millisecond
	}
	if len(config.CompressionPreferences) == 0 {
		config.CompressionPreferences = []CompressionCodec{
			CompressionSnappy,
			CompressionNone,
		}
	} else {
		config.CompressionPreferences = append(
			[]CompressionCodec(nil),
			config.CompressionPreferences...,
		)
	}
	if err := validateCompressionPreferences(
		config.CompressionPreferences,
	); err != nil {
		return ProducerConfig{}, err
	}
	if config.TransactionalID == "" {
		if config.TransactionTimeout != 0 || config.TransactionEndTimeout != 0 {
			return ProducerConfig{}, ErrInvalidProducerConfig
		}
	} else {
		if config.TransactionalID != strings.TrimSpace(config.TransactionalID) ||
			!validKafkaText(config.TransactionalID, 255) {
			return ProducerConfig{}, ErrInvalidProducerConfig
		}
		if config.TransactionTimeout == 0 {
			config.TransactionTimeout = 30 * time.Second
		}
		if config.TransactionEndTimeout == 0 {
			config.TransactionEndTimeout = 30 * time.Second
		}
	}
	if config.MaxBufferedRecords < 1 ||
		config.MaxBufferedRecords > 100_000 ||
		config.MaxBufferedBytes < int(config.MaxBatchBytes) ||
		config.MaxBufferedBytes > 1<<30 ||
		config.MaxBatchRecords < 1 ||
		config.MaxBatchRecords > 10_000 ||
		config.MaxBatchBytes < 512 ||
		config.MaxBatchBytes > 100<<20 ||
		config.RecordRetries < 1 ||
		config.RecordRetries > 1_000 ||
		config.RetryBackoffMin < time.Millisecond ||
		config.RetryBackoffMax < config.RetryBackoffMin ||
		config.RetryBackoffMax > 5*time.Second ||
		config.DeliveryTimeout < time.Second ||
		config.DeliveryTimeout > 10*time.Minute ||
		config.RetryBackoffMax > config.DeliveryTimeout ||
		config.ShutdownTimeout <
			config.DeliveryTimeout+config.RetryBackoffMax ||
		config.ShutdownTimeout > 15*time.Minute ||
		config.RequestTimeout < 100*time.Millisecond ||
		config.RequestTimeout > 2*time.Minute ||
		config.RequestTimeout > config.DeliveryTimeout ||
		config.DialTimeout < 100*time.Millisecond ||
		config.DialTimeout > 2*time.Minute ||
		config.Linger < time.Nanosecond ||
		config.Linger > time.Second ||
		(config.TransactionalID != "" &&
			(config.TransactionTimeout < time.Second ||
				config.TransactionTimeout > 15*time.Minute ||
				config.TransactionEndTimeout < time.Second ||
				config.TransactionEndTimeout > 2*time.Minute)) {
		return ProducerConfig{}, ErrInvalidProducerConfig
	}
	maximumRecordBytes := maximumRecordPolicyBytes(config.Limits)
	if int64(config.MaxBatchBytes) < maximumRecordBytes {
		return ProducerConfig{}, ErrInvalidProducerConfig
	}
	if len(config.AllowedTopics) == 0 {
		return ProducerConfig{}, ErrTopicsRequired
	}
	if len(config.AllowedTopics) > 64 {
		return ProducerConfig{}, ErrTooManyTopics
	}
	seenTopics := make(map[string]struct{}, len(config.AllowedTopics))
	for _, topic := range config.AllowedTopics {
		if !validKafkaTopicName(topic, config.Limits.MaxTopicBytes) {
			return ProducerConfig{}, ErrInvalidTopic
		}
		if _, duplicate := seenTopics[topic]; duplicate {
			return ProducerConfig{}, ErrDuplicateTopic
		}
		seenTopics[topic] = struct{}{}
	}
	config.AllowedTopics = append([]string(nil), config.AllowedTopics...)

	return config, nil
}

func newProducerRetryBackoff(
	clientID string,
	minimum time.Duration,
	maximum time.Duration,
) func(int) time.Duration {
	seed := producerRetrySeed(uint64(time.Now().UnixNano()), clientID)

	return func(attempt int) time.Duration {
		return producerRetryBackoffDuration(minimum, maximum, attempt, seed)
	}
}

func producerRetrySeed(seed uint64, clientID string) uint64 {
	for index := range len(clientID) {
		seed ^= uint64(clientID[index])
		seed *= 1099511628211
	}

	return seed
}

func producerRetryBackoffDuration(
	minimum time.Duration,
	maximum time.Duration,
	attempt int,
	seed uint64,
) time.Duration {
	upper := minimum
	for retry := 1; retry < attempt && upper != maximum; retry++ {
		upper = min(upper*2, maximum)
	}
	lower := upper - upper/5
	mixed := seed + uint64(max(attempt, 0))*0x9e3779b97f4a7c15
	mixed = (mixed ^ (mixed >> 30)) * 0xbf58476d1ce4e5b9
	mixed = (mixed ^ (mixed >> 27)) * 0x94d049bb133111eb
	mixed ^= mixed >> 31
	spread := uint64(upper-lower) + 1

	return lower + time.Duration(mixed%spread)
}

func producerMetadataMinAge(retryBackoffMin time.Duration) time.Duration {
	return max(250*time.Millisecond, retryBackoffMin)
}

func maximumRecordPolicyBytes(limits MessageLimits) int64 {
	return int64(limits.MaxTopicBytes) +
		int64(limits.MaxKeyBytes) +
		int64(limits.MaxValueBytes) +
		int64(limits.MaxHeaderBytes) +
		int64(limits.MaxHeaders)*8 +
		32
}

func validateCompressionPreferences(
	preferences []CompressionCodec,
) error {
	if len(preferences) == 0 {
		return ErrInvalidCompressionPreference
	}
	if len(preferences) > 5 {
		return ErrInvalidCompressionPreference
	}
	seen := make(map[CompressionCodec]struct{}, len(preferences))
	for index, codec := range preferences {
		if codec < CompressionNone || codec > CompressionZstd {
			return ErrInvalidCompressionPreference
		}
		if _, duplicate := seen[codec]; duplicate {
			return ErrInvalidCompressionPreference
		}
		if codec == CompressionNone && index != len(preferences)-1 {
			return ErrInvalidCompressionPreference
		}
		seen[codec] = struct{}{}
	}

	return nil
}

func franzCompressionCodecs(
	preferences []CompressionCodec,
) []kgo.CompressionCodec {
	codecs := make([]kgo.CompressionCodec, len(preferences))
	for index, codec := range preferences {
		switch codec {
		case CompressionNone:
			codecs[index] = kgo.NoCompression()
		case CompressionGzip:
			codecs[index] = kgo.GzipCompression()
		case CompressionSnappy:
			codecs[index] = kgo.SnappyCompression()
		case CompressionLz4:
			codecs[index] = kgo.Lz4Compression()
		case CompressionZstd:
			codecs[index] = kgo.ZstdCompression()
		}
	}

	return codecs
}

func validateClientIdentity(brokers []string, clientID string) error {
	if len(brokers) == 0 {
		return ErrBrokersRequired
	}
	if len(brokers) > 32 {
		return ErrTooManyBrokers
	}
	seenBrokers := make(map[string]struct{}, len(brokers))
	for _, broker := range brokers {
		if broker == "" || broker != strings.TrimSpace(broker) ||
			!validKafkaText(broker, 255) {
			return ErrInvalidBroker
		}
		if _, exists := seenBrokers[broker]; exists {
			return ErrDuplicateBroker
		}
		seenBrokers[broker] = struct{}{}
	}
	if strings.TrimSpace(clientID) == "" {
		return ErrClientIDRequired
	}
	if len(clientID) > 255 {
		return ErrClientIDTooLarge
	}
	if clientID != strings.TrimSpace(clientID) || !validKafkaText(clientID, 255) {
		return ErrInvalidClientID
	}

	return nil
}

func validKafkaText(value string, maxBytes int) bool {
	return len(value) <= maxBytes && utf8.ValidString(value) &&
		strings.IndexFunc(value, unicode.IsControl) == -1
}

// Publish waits for Kafka to accept the message or returns the first delivery
// error. A nil result does not provide end-to-end exactly-once delivery.
func (producer *Producer) Publish(ctx context.Context, message Message) error {
	return producer.PublishRecord(ctx, message).Err
}

// PublishRecord synchronously publishes one record and returns its individual
// broker delivery metadata. The producer owns copies of all input bytes before
// passing the record to franz-go. Caller cancellation can stop an admitted
// non-transactional record and is reported as an ambiguous delivery.
func (producer *Producer) PublishRecord(
	ctx context.Context,
	record ProducerRecord,
) DeliveryResult {
	if ctx == nil {
		return DeliveryResult{Topic: record.Topic, Err: ErrContextRequired}
	}
	if isObserverContext(ctx) {
		return DeliveryResult{Topic: record.Topic, Err: ErrObserverReentry}
	}
	var startedAt time.Time
	if producer.observers.enabled() {
		startedAt = time.Now()
	}
	if err := producer.startOperation(); err != nil {
		result := DeliveryResult{Topic: record.Topic, Err: err}
		producer.observeProducerRecord(ctx, startedAt, record, result)

		return result
	}

	result := producer.publishRecord(ctx, record)
	producer.finishAdmission()
	producer.finishOperation()
	producer.observeProducerRecord(ctx, startedAt, record, result)

	return result
}

func (producer *Producer) observeProducerRecord(
	ctx context.Context,
	startedAt time.Time,
	record ProducerRecord,
	result DeliveryResult,
) {
	if !producer.observers.enabled() {
		return
	}
	topic := ""
	var bytes int64
	if record.validate(producer.limits) == nil {
		topic = strings.Clone(record.Topic)
		bytes = recordSize(record)
	}
	observation := Observation{
		Kind:           ObservationProduceRecord,
		StartedAt:      startedAt,
		Duration:       time.Since(startedAt),
		ClientID:       producer.clientID,
		Topic:          topic,
		Partition:      result.Partition,
		PartitionKnown: result.Err == nil,
		Offset:         result.Offset,
		OffsetKnown:    result.Err == nil,
		Timestamp:      result.Timestamp,
		RecordCount:    1,
		RecordBytes:    bytes,
		Succeeded:      result.Err == nil,
	}
	if result.Err != nil {
		observation.Category = classifyError(result.Err)
	}
	producer.dispatchObservation(ctx, observation)
}

func (producer *Producer) publish(ctx context.Context, message Message) error {
	result, expired := producer.publishTransactionalRecord(ctx, message)
	if expired {
		fatalErr := errors.Join(ErrProducerFatal, result.Err)
		producer.terminate(fatalErr)

		return fatalErr
	}
	if fatalErr := producer.fatalError(); fatalErr != nil {
		return errors.Join(result.Err, fatalErr)
	}

	return result.Err
}

func (producer *Producer) publishTransactionalRecord(
	ctx context.Context,
	record ProducerRecord,
) (DeliveryResult, bool) {
	result := DeliveryResult{Topic: record.Topic}
	if err := producer.validateRecord(record); err != nil {
		result.Err = err

		return result, false
	}
	if err := ctx.Err(); err != nil {
		result.Err = newDeliveryError(errors.Join(err, context.Cause(ctx)))

		return result, false
	}
	deliveries, cause := transactionalProduceSync(
		ctx,
		producer.deliveryWaitTimeout,
		producer.interruptClient,
		producer.client,
		franzRecord(record.owned()),
	)
	if cause != nil {
		result.Err = newDeliveryError(cause)

		return result, true
	}
	if len(deliveries) != 1 || deliveries[0].Record == nil {
		result.Err = newDeliveryError(ErrDeliveryResultMissing)

		return result, false
	}

	return deliveryResult(deliveries[0]), false
}

type synchronousRecordProducer interface {
	ProduceSync(context.Context, ...*kgo.Record) kgo.ProduceResults
}

func transactionalProduceSync(
	ctx context.Context,
	waitTimeout time.Duration,
	interruptClient func(),
	client synchronousRecordProducer,
	record *kgo.Record,
) (kgo.ProduceResults, error) {
	deliveryCtx := ctx
	cancelDelivery := func() {}
	if waitTimeout > 0 {
		deliveryCtx, cancelDelivery = context.WithTimeout(ctx, waitTimeout)
	}
	onCancellation := interruptClient
	if onCancellation == nil {
		onCancellation = func() {}
	}
	stopCancellation := context.AfterFunc(deliveryCtx, onCancellation)
	deliveries := client.ProduceSync(deliveryCtx, record)
	expired := !stopCancellation()
	cause := errors.Join(deliveryCtx.Err(), context.Cause(deliveryCtx))
	cancelDelivery()
	if !expired {
		return deliveries, nil
	}

	return deliveries, cause
}

func (producer *Producer) publishRecord(
	ctx context.Context,
	record ProducerRecord,
) DeliveryResult {
	result := DeliveryResult{Topic: record.Topic}
	if err := producer.validateRecord(record); err != nil {
		result.Err = err

		return result
	}
	deliveryCtx, cancelDelivery := producer.deliveryContext(ctx)
	deliveries := producer.client.ProduceSync(
		deliveryCtx,
		franzRecord(record.owned()),
	)
	cancelDelivery()
	if len(deliveries) != 1 || deliveries[0].Record == nil {
		result.Err = newDeliveryError(ErrDeliveryResultMissing)

		return result
	}
	return deliveryResult(deliveries[0])
}

func (producer *Producer) validateRecord(record ProducerRecord) error {
	if err := record.validate(producer.limits); err != nil {
		return err
	}
	if !producer.topicAllowed(record.Topic) {
		return ErrTopicNotAllowed
	}
	if producer.keyRequired && len(record.Key) == 0 {
		return ErrKeyRequired
	}

	return nil
}

// PublishBatch validates and owns an entire bounded batch before producing any
// record. Results remain in input order and expose every partial delivery
// failure.
func (producer *Producer) PublishBatch(
	ctx context.Context,
	records []ProducerRecord,
) (results []DeliveryResult, resultErr error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	if isObserverContext(ctx) {
		return nil, ErrObserverReentry
	}
	var startedAt time.Time
	if producer.observers.enabled() {
		startedAt = time.Now()
		defer func() {
			producer.observeProducerBatch(
				ctx,
				startedAt,
				records,
				resultErr,
			)
		}()
	}
	if err := producer.startOperation(); err != nil {
		return nil, err
	}
	defer producer.finishOperation()
	defer producer.finishAdmission()

	if len(records) == 0 {
		return nil, ErrRecordsRequired
	}
	if len(records) > producer.maxBatchRecords {
		return nil, ErrTooManyBatchRecords
	}

	franzRecords := make([]*kgo.Record, len(records))
	var batchBytes int64
	for index, record := range records {
		if err := record.validate(producer.limits); err != nil {
			return nil, err
		}
		if !producer.topicAllowed(record.Topic) {
			return nil, ErrTopicNotAllowed
		}
		if producer.keyRequired && len(record.Key) == 0 {
			return nil, ErrKeyRequired
		}
		batchBytes += recordSize(record)
		if batchBytes > producer.maxBatchBytes {
			return nil, ErrBatchTooLarge
		}
		franzRecords[index] = franzRecord(record.owned())
	}

	deliveryCtx, cancelDelivery := producer.deliveryContext(ctx)
	deliveries := producer.client.ProduceSync(deliveryCtx, franzRecords...)
	cancelDelivery()
	inputIndexes := make(map[*kgo.Record]int, len(franzRecords))
	for index, record := range franzRecords {
		inputIndexes[record] = index
	}
	results = make([]DeliveryResult, len(records))
	delivered := make([]bool, len(records))
	duplicated := make([]bool, len(records))
	unexpected := false
	for _, delivery := range deliveries {
		if delivery.Record == nil {
			unexpected = true

			continue
		}
		index, expected := inputIndexes[delivery.Record]
		if !expected {
			unexpected = true

			continue
		}
		if delivered[index] {
			duplicated[index] = true

			continue
		}
		delivered[index] = true
		results[index] = deliveryResult(delivery)
	}
	var deliveryErrors []error
	if unexpected {
		deliveryErrors = append(
			deliveryErrors,
			newDeliveryError(ErrDeliveryResultInvalid),
		)
	}
	for index := range records {
		if !delivered[index] {
			results[index] = DeliveryResult{
				Topic: records[index].Topic,
				Err:   newDeliveryError(ErrDeliveryResultMissing),
			}
			deliveryErrors = append(deliveryErrors, results[index].Err)

			continue
		}
		if duplicated[index] {
			results[index] = DeliveryResult{
				Topic: records[index].Topic,
				Err:   newDeliveryError(ErrDeliveryResultInvalid),
			}
		}
		if results[index].Err != nil {
			deliveryErrors = append(deliveryErrors, results[index].Err)
		}
	}
	if len(deliveryErrors) != 0 {
		return results, errors.Join(
			append([]error{ErrBatchDeliveryFailed}, deliveryErrors...)...,
		)
	}

	return results, nil
}

func (producer *Producer) observeProducerBatch(
	ctx context.Context,
	startedAt time.Time,
	records []ProducerRecord,
	err error,
) {
	topic, bytes := producer.batchObservationMetadata(records)
	observation := Observation{
		Kind:        ObservationProduceBatch,
		StartedAt:   startedAt,
		Duration:    time.Since(startedAt),
		ClientID:    producer.clientID,
		Topic:       topic,
		RecordCount: len(records),
		RecordBytes: bytes,
		Succeeded:   err == nil,
	}
	if err != nil {
		observation.Category = classifyError(err)
	}
	producer.dispatchObservation(ctx, observation)
}

func (producer *Producer) batchObservationMetadata(
	records []ProducerRecord,
) (string, int64) {
	if len(records) == 0 || len(records) > producer.maxBatchRecords {
		return "", 0
	}

	topic := records[0].Topic
	var bytes int64
	for _, record := range records {
		if record.validate(producer.limits) != nil {
			return "", 0
		}
		if record.Topic != topic {
			topic = ""
		}
		size := recordSize(record)
		if size > producer.maxBatchBytes-bytes {
			return "", 0
		}
		bytes += size
	}

	return strings.Clone(topic), bytes
}

// PublishAsync admits one owned record to the bounded franz-go producer and
// returns a one-result buffered channel. The caller may stop waiting without
// cancelling a record after this method returns; the package delivery deadline
// continues resolving that record and publishes the eventual result. Caller
// cancellation while admission is still blocked remains authoritative.
func (producer *Producer) PublishAsync(
	ctx context.Context,
	record ProducerRecord,
) (<-chan DeliveryResult, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	if isObserverContext(ctx) {
		return nil, ErrObserverReentry
	}
	var startedAt time.Time
	if producer.observers.enabled() {
		startedAt = time.Now()
	}

	if err := producer.startOperation(); err != nil {
		producer.observeProducerAsync(
			ctx,
			startedAt,
			record,
			DeliveryResult{Topic: record.Topic, Err: err},
		)

		return nil, err
	}
	defer producer.finishAdmission()

	if err := record.validate(producer.limits); err != nil {
		producer.finishOperation()
		producer.observeProducerAsync(
			ctx,
			startedAt,
			record,
			DeliveryResult{Topic: record.Topic, Err: err},
		)

		return nil, err
	}
	if !producer.topicAllowed(record.Topic) {
		producer.finishOperation()
		producer.observeProducerAsync(
			ctx,
			startedAt,
			record,
			DeliveryResult{Topic: record.Topic, Err: ErrTopicNotAllowed},
		)

		return nil, ErrTopicNotAllowed
	}
	if producer.keyRequired && len(record.Key) == 0 {
		producer.finishOperation()
		producer.observeProducerAsync(
			ctx,
			startedAt,
			record,
			DeliveryResult{Topic: record.Topic, Err: ErrKeyRequired},
		)

		return nil, ErrKeyRequired
	}

	delivery := make(chan DeliveryResult, 1)
	topic := record.Topic
	owned := record.owned()
	deliveryCtx, cancelDelivery, stopCallerCancellation :=
		producer.asyncDeliveryContext(ctx)
	producer.client.Produce(deliveryCtx, franzRecord(owned), func(
		record *kgo.Record,
		err error,
	) {
		cancelDelivery()
		if producer.observers.enabled() {
			producer.beginObservation()
			defer producer.finishObservation()
		}

		var result DeliveryResult
		if record == nil {
			result = DeliveryResult{Topic: topic, Err: newDeliveryError(errors.Join(
				ErrDeliveryResultMissing,
				err,
			))}
		} else {
			result = deliveryResult(kgo.ProduceResult{Record: record, Err: err})
		}
		producer.finishOperation()
		producer.observeProducerAsync(ctx, startedAt, owned, result)
		delivery <- result
		close(delivery)
	})
	if ctx.Err() != nil {
		cancelDelivery()
	}
	stopCallerCancellation()

	return delivery, nil
}

func (producer *Producer) deliveryContext(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	if producer.deliveryWaitTimeout <= 0 {
		return ctx, func() {}
	}

	return context.WithTimeout(ctx, producer.deliveryWaitTimeout)
}

func (producer *Producer) asyncDeliveryContext(
	ctx context.Context,
) (context.Context, context.CancelFunc, func() bool) {
	base := context.WithoutCancel(ctx)
	var (
		deliveryCtx context.Context
		cancel      context.CancelFunc
	)
	if producer.deliveryWaitTimeout <= 0 {
		deliveryCtx, cancel = context.WithCancel(base)
	} else {
		deliveryCtx, cancel = context.WithTimeout(
			base,
			producer.deliveryWaitTimeout,
		)
	}
	stopCallerCancellation := context.AfterFunc(ctx, cancel)

	return deliveryCtx, cancel, stopCallerCancellation
}

func (producer *Producer) observeProducerAsync(
	ctx context.Context,
	startedAt time.Time,
	record ProducerRecord,
	result DeliveryResult,
) {
	if !producer.observers.enabled() {
		return
	}
	topic := ""
	var bytes int64
	if record.validate(producer.limits) == nil {
		topic = strings.Clone(record.Topic)
		bytes = recordSize(record)
	}
	observation := Observation{
		Kind:           ObservationProduceAsync,
		StartedAt:      startedAt,
		Duration:       time.Since(startedAt),
		ClientID:       producer.clientID,
		Topic:          topic,
		Partition:      result.Partition,
		PartitionKnown: result.Err == nil,
		Offset:         result.Offset,
		OffsetKnown:    result.Err == nil,
		Timestamp:      result.Timestamp,
		RecordCount:    1,
		RecordBytes:    bytes,
		Succeeded:      result.Err == nil,
	}
	if result.Err != nil {
		observation.Category = classifyError(result.Err)
	}
	producer.dispatchObservation(ctx, observation)
}

func franzRecord(record ProducerRecord) *kgo.Record {
	headers := make([]kgo.RecordHeader, len(record.Headers))
	for index, header := range record.Headers {
		headers[index] = kgo.RecordHeader{Key: header.Key, Value: header.Value}
	}

	partition := int32(-1)
	if record.Partition.Mode == PartitionExplicit {
		partition = record.Partition.Partition
	}

	return &kgo.Record{
		Topic:     record.Topic,
		Partition: partition,
		Key:       record.Key,
		Value:     record.Value,
		Headers:   headers,
		Timestamp: record.Timestamp,
	}
}

func (producer *Producer) topicAllowed(topic string) bool {
	if producer.allowedTopics == nil {
		return true
	}
	_, allowed := producer.allowedTopics[topic]

	return allowed
}

func deliveryResult(delivery kgo.ProduceResult) DeliveryResult {
	var deliveryErr error
	if delivery.Err != nil {
		deliveryErr = newDeliveryError(delivery.Err)
	}

	return DeliveryResult{
		Topic:     delivery.Record.Topic,
		Partition: delivery.Record.Partition,
		Offset:    delivery.Record.Offset,
		Timestamp: delivery.Record.Timestamp,
		Err:       deliveryErr,
	}
}

func recordSize(record ProducerRecord) int64 {
	// The fixed allowance conservatively covers record framing and length
	// varints without computing franz-go's private encoded representation.
	size := int64(len(record.Topic) + len(record.Key) + len(record.Value) + 32)
	for _, header := range record.Headers {
		size += int64(len(header.Key) + len(header.Value) + 8)
	}

	return size
}

// Health verifies that a broker is reachable and responds to a metadata
// request.
func (producer *Producer) Health(ctx context.Context) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if isObserverContext(ctx) {
		return ErrObserverReentry
	}
	if err := producer.startOperation(); err != nil {
		return err
	}
	defer producer.finishOperation()
	defer producer.finishAdmission()

	return producer.client.Ping(ctx)
}

// Drain waits for every admitted asynchronous record to resolve without
// closing the producer. New operations are rejected while the drain is in
// progress. A timeout or cancellation reports both ErrDrainIncomplete and the
// context failure; admitted records remain owned by the producer.
func (producer *Producer) Drain(ctx context.Context) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if isObserverContext(ctx) {
		return ErrObserverReentry
	}
	admissions, err := producer.startMaintenance(false)
	if err != nil {
		return err
	}
	defer producer.finishMaintenance(false)
	if err := waitAdmissions(ctx, admissions); err != nil {
		return drainResult(err)
	}

	return drainResult(producer.client.Flush(ctx))
}

// Abort drops records still buffered by franz-go and waits for their delivery
// callbacks to run. It is an explicit data-loss operation intended for
// recovery after the caller has accepted the undelivered-record outcome.
func (producer *Producer) Abort(ctx context.Context) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if isObserverContext(ctx) {
		return ErrObserverReentry
	}
	admissions, err := producer.startMaintenance(true)
	if err != nil {
		return err
	}
	defer producer.finishMaintenance(false)
	if err := waitAdmissions(ctx, admissions); err != nil {
		return err
	}

	return producer.client.AbortBufferedRecords(ctx)
}

// Shutdown fences new operations, drains every admitted record within ctx,
// and closes the underlying client. If draining is incomplete the producer
// remains fenced but open so the caller can retry Shutdown or explicitly
// Abort. Successful shutdown is idempotent.
func (producer *Producer) Shutdown(ctx context.Context) (resultErr error) {
	if ctx == nil {
		return ErrContextRequired
	}
	if isObserverContext(ctx) {
		return ErrObserverReentry
	}

	producer.stateMu.Lock()
	if producer.observerCallbacks != 0 {
		producer.stateMu.Unlock()

		return ErrObserverReentry
	}
	if producer.shutdownComplete {
		producer.stateMu.Unlock()

		return nil
	}
	if producer.maintenanceActive {
		producer.stateMu.Unlock()

		return ErrProducerBusy
	}
	if producer.transactionActive {
		producer.stateMu.Unlock()

		return ErrProducerBusy
	}
	producer.closed = true
	producer.maintenanceActive = true
	admissions := producer.admissionsDone
	producer.stateMu.Unlock()
	var startedAt time.Time
	if producer.observers.enabled() {
		startedAt = time.Now()
		defer func() {
			producer.observeShutdown(ctx, startedAt, resultErr)
		}()
	}
	if err := waitAdmissions(ctx, admissions); err != nil {
		producer.finishMaintenance(false)

		return drainResult(err)
	}

	if err := drainResult(producer.client.Flush(ctx)); err != nil {
		producer.finishMaintenance(false)

		return err
	}
	if producer.cancelClient != nil {
		producer.cancelClient()
	}
	producer.closeOnce.Do(producer.client.Close)
	producer.finishMaintenance(true)

	return nil
}

func drainResult(err error) error {
	if err == nil {
		return nil
	}

	return errors.Join(ErrDrainIncomplete, err)
}

func waitAdmissions(ctx context.Context, admissions <-chan struct{}) error {
	if admissions == nil {
		return nil
	}
	select {
	case <-admissions:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Transaction is the producer surface available inside RunTransaction.
type Transaction struct {
	session *transactionSession
}

// Publish synchronously publishes one message inside the active transaction.
// If its context or the configured delivery bound expires after admission,
// the outcome is ambiguous and the owning producer enters ErrProducerFatal;
// callers must close and replace it rather than retrying on the same client.
func (transaction Transaction) Publish(ctx context.Context, message Message) error {
	if transaction.session == nil {
		return ErrTransactionClosed
	}

	return transaction.session.publish(ctx, message)
}

// RunTransaction serializes one producer transaction. The transaction is
// committed only when callback returns nil; callback failure or panic triggers
// a bounded abort whose completion ignores caller cancellation. An ambiguous
// transactional publish deadline closes and permanently fences the producer,
// so RunTransaction returns without attempting commit or abort on that client.
func (producer *Producer) RunTransaction(
	ctx context.Context,
	callback func(Transaction) error,
) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if isObserverContext(ctx) {
		return ErrObserverReentry
	}
	if !producer.transactionsEnabled {
		return ErrTransactionsDisabled
	}
	if callback == nil {
		return ErrTransactionRequired
	}

	if err := producer.startTransaction(); err != nil {
		return err
	}
	defer producer.finishTransaction()

	if err := producer.beginTransaction(ctx); err != nil {
		return err
	}

	session := &transactionSession{publisher: producer}
	callbackErr := callTransaction(callback, Transaction{session: session})
	session.closeAndWait()
	if fatalErr := producer.fatalError(); fatalErr != nil {
		return errors.Join(callbackErr, fatalErr)
	}
	if callbackErr != nil {
		return errors.Join(callbackErr, producer.abortTransaction(ctx))
	}

	commitErr := producer.commitTransaction(ctx)
	if commitErr == nil {
		return nil
	}
	var transactionErr *TransactionError
	if errors.As(commitErr, &transactionErr) && transactionErr.Abortable() {
		abortErr := producer.abortTransaction(ctx)

		return errors.Join(abortErr, commitErr)
	}

	return commitErr
}

func (producer *Producer) beginTransaction(ctx context.Context) error {
	var startedAt time.Time
	if producer.observers.enabled() {
		startedAt = time.Now()
	}
	err := newTransactionError(
		TransactionOperationBegin,
		producer.client.BeginTransaction(),
		false,
		true,
	)
	producer.observeTransaction(
		ctx,
		ObservationTransactionBegin,
		startedAt,
		err,
	)

	return err
}

func (producer *Producer) commitTransaction(ctx context.Context) error {
	var startedAt time.Time
	if producer.observers.enabled() {
		startedAt = time.Now()
	}
	endCtx, cancel := producer.transactionEndContext(ctx)
	commitErr := producer.client.EndTransaction(endCtx, kgo.TryCommit)
	cancel()

	var err error
	switch {
	case commitErr == nil:
	case errors.Is(commitErr, kerr.OperationNotAttempted),
		errors.Is(commitErr, kerr.TransactionAbortable):
		err = newTransactionError(
			TransactionOperationCommit,
			commitErr,
			true,
			true,
		)
	default:
		category := classifyError(commitErr)
		if category == ErrorAuthorization ||
			category == ErrorFenced ||
			category == ErrorFatal {
			err = newTransactionError(
				TransactionOperationCommit,
				commitErr,
				false,
				true,
			)
		} else {
			err = newTransactionError(
				TransactionOperationCommit,
				errors.Join(ErrTransactionOutcomeUnknown, commitErr),
				false,
				false,
			)
		}
	}
	producer.observeTransaction(
		ctx,
		ObservationTransactionCommit,
		startedAt,
		err,
	)

	return err
}

type transactionSession struct {
	publisher transactionRecordPublisher
	mu        sync.Mutex
	active    sync.WaitGroup
	closed    bool
}

type transactionRecordPublisher interface {
	publish(context.Context, ProducerRecord) error
}

func (session *transactionSession) publish(ctx context.Context, message Message) error {
	if ctx == nil {
		return ErrContextRequired
	}
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()

		return ErrTransactionClosed
	}
	session.active.Add(1)
	session.mu.Unlock()
	defer session.active.Done()

	return session.publisher.publish(ctx, message)
}

func (session *transactionSession) closeAndWait() {
	session.mu.Lock()
	session.closed = true
	session.mu.Unlock()
	session.active.Wait()
}

func callTransaction(
	callback func(Transaction) error,
	transaction Transaction,
) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrTransactionPanic
		}
	}()

	return callback(transaction)
}

func (producer *Producer) abortTransaction(ctx context.Context) (resultErr error) {
	var startedAt time.Time
	if producer.observers.enabled() {
		startedAt = time.Now()
		defer func() {
			producer.observeTransaction(
				ctx,
				ObservationTransactionAbort,
				startedAt,
				resultErr,
			)
		}()
	}
	abortCtx, cancelAbort := producer.transactionEndContext(ctx)
	abortErr := producer.client.AbortBufferedRecords(abortCtx)
	cancelAbort()

	endCtx, cancelEnd := producer.transactionEndContext(ctx)
	endErr := producer.client.EndTransaction(endCtx, kgo.TryAbort)
	cancelEnd()

	var bufferedErr error
	if abortErr != nil {
		bufferedErr = newTransactionErrorWithCategory(
			TransactionOperationAbort,
			abortErr,
			ErrorFatal,
			false,
			endErr == nil || transactionAbortOutcomeKnown(endErr),
		)
	}

	resultErr = errors.Join(
		bufferedErr,
		newTransactionError(
			TransactionOperationAbort,
			endErr,
			false,
			transactionAbortOutcomeKnown(endErr),
		),
	)

	return resultErr
}

func (producer *Producer) observeTransaction(
	ctx context.Context,
	kind ObservationKind,
	startedAt time.Time,
	err error,
) {
	if !producer.observers.enabled() {
		return
	}
	observation := Observation{
		Kind:      kind,
		StartedAt: startedAt,
		Duration:  time.Since(startedAt),
		ClientID:  producer.clientID,
		Succeeded: err == nil,
	}
	if err != nil {
		observation.Category = transactionObservationCategory(err)
	}
	producer.dispatchObservation(ctx, observation)
}

func transactionObservationCategory(err error) ErrorCategory {
	if errors.Is(err, ErrTransactionOutcomeUnknown) {
		return ErrorAmbiguous
	}
	var transactionErr *TransactionError
	if errors.As(err, &transactionErr) {
		return transactionErr.Category()
	}

	return classifyError(err)
}

func transactionAbortOutcomeKnown(err error) bool {
	category := classifyError(err)

	return category == ErrorAuthorization ||
		category == ErrorFenced ||
		category == ErrorFatal
}

func (producer *Producer) transactionEndContext(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), producer.transactionEndTimeout)
}

func (message ProducerRecord) validate(limits MessageLimits) error {
	if message.Topic == "" {
		return ErrTopicRequired
	}
	if len(message.Topic) > limits.MaxTopicBytes {
		return ErrTopicTooLarge
	}
	if !validKafkaTopicName(message.Topic, limits.MaxTopicBytes) {
		return ErrInvalidTopic
	}
	switch message.Partition.Mode {
	case PartitionAutomatic:
		if message.Partition.Partition != 0 {
			return ErrInvalidPartitionSelection
		}
	case PartitionExplicit:
		if message.Partition.Partition < 0 {
			return ErrInvalidPartitionSelection
		}
	default:
		return ErrInvalidPartitionSelection
	}
	if len(message.Key) > limits.MaxKeyBytes {
		return ErrKeyTooLarge
	}
	if len(message.Value) > limits.MaxValueBytes {
		return ErrValueTooLarge
	}
	if len(message.Headers) > limits.MaxHeaders {
		return ErrTooManyHeaders
	}

	headerBytes := 0
	for _, header := range message.Headers {
		if header.Key == "" {
			return ErrHeaderKeyRequired
		}
		if len(header.Key) > limits.MaxHeaderKeyBytes {
			return ErrHeaderKeyTooLarge
		}
		if len(header.Value) > limits.MaxHeaderValueBytes {
			return ErrHeaderValueTooLarge
		}
		headerSize := len(header.Key) + len(header.Value)
		if headerSize > limits.MaxHeaderBytes-headerBytes {
			return ErrHeadersTooLarge
		}
		headerBytes += headerSize
	}

	return nil
}

// Close performs a configured bounded graceful shutdown. It returns
// ErrDrainIncomplete without closing the client when admitted records cannot
// resolve before ShutdownTimeout; callers may retry Shutdown or explicitly
// Abort.
func (producer *Producer) Close() error {
	producer.stateMu.Lock()
	if producer.observerCallbacks != 0 {
		producer.stateMu.Unlock()

		return ErrObserverReentry
	}
	producer.stateMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), producer.shutdownTimeout)
	defer cancel()

	return producer.Shutdown(ctx)
}

func (producer *Producer) observeShutdown(
	ctx context.Context,
	startedAt time.Time,
	err error,
) {
	observation := Observation{
		Kind:      ObservationProducerShutdown,
		StartedAt: startedAt,
		Duration:  time.Since(startedAt),
		ClientID:  producer.clientID,
		Succeeded: err == nil,
	}
	if err != nil {
		observation.Category = classifyError(err)
	}
	producer.dispatchObservation(ctx, observation)
}

func (producer *Producer) dispatchObservation(
	ctx context.Context,
	observation Observation,
) {
	producer.beginObservation()
	defer producer.finishObservation()

	producer.observers.observe(ctx, observation)
}

func (producer *Producer) beginObservation() {
	producer.stateMu.Lock()
	producer.observerCallbacks++
	producer.stateMu.Unlock()
}

func (producer *Producer) finishObservation() {
	producer.stateMu.Lock()
	producer.observerCallbacks--
	producer.stateMu.Unlock()
}

func (producer *Producer) startOperation() error {
	producer.stateMu.Lock()
	defer producer.stateMu.Unlock()

	if producer.fatalErr != nil {
		return producer.fatalErr
	}
	if producer.closed {
		return ErrProducerClosed
	}
	if producer.transactionActive {
		return ErrTransactionInProgress
	}
	if producer.maintenanceActive {
		return ErrProducerBusy
	}
	switch producer.admitting {
	case 0:
		producer.admissionsDone = make(chan struct{})
	}
	producer.admitting++
	producer.inflight++

	return nil
}

func (producer *Producer) finishAdmission() {
	producer.stateMu.Lock()
	producer.admitting--
	if producer.admitting == 0 {
		close(producer.admissionsDone)
		producer.admissionsDone = nil
	}
	producer.stateMu.Unlock()
}

func (producer *Producer) finishOperation() {
	producer.stateMu.Lock()
	producer.inflight--
	producer.stateMu.Unlock()
}

func (producer *Producer) startTransaction() error {
	producer.stateMu.Lock()
	defer producer.stateMu.Unlock()

	if producer.fatalErr != nil {
		return producer.fatalErr
	}
	if producer.closed {
		return ErrProducerClosed
	}
	if producer.transactionActive {
		return ErrTransactionInProgress
	}
	if producer.maintenanceActive {
		return ErrProducerBusy
	}
	if producer.inflight != 0 {
		return ErrProducerBusy
	}
	producer.transactionActive = true

	return nil
}

func (producer *Producer) finishTransaction() {
	producer.stateMu.Lock()
	producer.transactionActive = false
	producer.stateMu.Unlock()
}

func (producer *Producer) startMaintenance(allowClosed bool) (<-chan struct{}, error) {
	producer.stateMu.Lock()
	defer producer.stateMu.Unlock()

	if producer.fatalErr != nil {
		return nil, producer.fatalErr
	}
	if producer.shutdownComplete || (producer.closed && !allowClosed) {
		return nil, ErrProducerClosed
	}
	if producer.maintenanceActive || producer.transactionActive {
		return nil, ErrProducerBusy
	}
	producer.maintenanceActive = true

	return producer.admissionsDone, nil
}

func (producer *Producer) fatalError() error {
	producer.stateMu.Lock()
	defer producer.stateMu.Unlock()

	return producer.fatalErr
}

func (producer *Producer) terminate(err error) {
	producer.stateMu.Lock()
	if producer.fatalErr == nil {
		producer.fatalErr = err
	}
	producer.closed = true
	producer.stateMu.Unlock()
	producer.interruptClient()
	producer.stateMu.Lock()
	producer.shutdownComplete = true
	producer.stateMu.Unlock()
}

func (producer *Producer) interruptClient() {
	cancelClient := producer.cancelClient
	if cancelClient != nil {
		cancelClient()
	}
	producer.closeOnce.Do(producer.client.Close)
}

func (producer *Producer) finishMaintenance(shutdownComplete bool) {
	producer.stateMu.Lock()
	producer.maintenanceActive = false
	producer.shutdownComplete = producer.shutdownComplete || shutdownComplete
	producer.stateMu.Unlock()
}
