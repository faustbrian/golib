// Package kafka provides bounded Apache Kafka producer and consumer
// composition for Go services.
package kafka

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

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
	ErrTopicRequired             = errors.New("kafka: topic is required")
	ErrTopicTooLarge             = errors.New("kafka: topic exceeds configured limit")
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
	ErrRecordsRequired       = errors.New("kafka: at least one producer record is required")
	ErrTooManyBatchRecords   = errors.New("kafka: producer batch record count exceeds configured limit")
	ErrBatchTooLarge         = errors.New("kafka: producer batch exceeds configured byte limit")
	ErrBatchDeliveryFailed   = errors.New("kafka: one or more batch records failed delivery")
	ErrContextRequired       = errors.New("kafka: context is required")
	ErrProducerClosed        = errors.New("kafka: producer is closed")
	ErrProducerBusy          = errors.New("kafka: producer has in-flight operations")
	ErrTransactionInProgress = errors.New("kafka: producer transaction is in progress")
	ErrDrainIncomplete       = errors.New("kafka: producer drain is incomplete")
)

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

// ProducerConfig defines the Kafka seed brokers and client identity.
type ProducerConfig struct {
	Brokers                []string
	ClientID               string
	KeyPolicy              KeyPolicy
	Limits                 MessageLimits
	MaxBufferedRecords     int
	MaxBufferedBytes       int
	MaxBatchRecords        int
	MaxBatchBytes          int32
	RecordRetries          int
	DeliveryTimeout        time.Duration
	RequestTimeout         time.Duration
	DialTimeout            time.Duration
	Linger                 time.Duration
	CompressionPreferences []CompressionCodec
	TransactionalID        string
	TransactionTimeout     time.Duration

	TransactionEndTimeout time.Duration
	Security              ClientSecurity
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
	limits                MessageLimits
	keyRequired           bool
	maxBatchRecords       int
	maxBatchBytes         int64
	transactionsEnabled   bool
	transactionEndTimeout time.Duration
	stateMu               sync.Mutex
	closed                bool
	transactionActive     bool
	maintenanceActive     bool
	shutdownComplete      bool
	inflight              int
	closeOnce             sync.Once
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

	options := []kgo.Opt{
		kgo.SeedBrokers(config.Brokers...),
		kgo.ClientID(config.ClientID),
		kgo.RecordPartitioner(newPolicyPartitioner(
			kgo.UniformBytesPartitioner(64<<10, true, true, nil),
		)),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.MaxBufferedRecords(config.MaxBufferedRecords),
		kgo.MaxBufferedBytes(config.MaxBufferedBytes),
		kgo.ProducerBatchMaxBytes(config.MaxBatchBytes),
		kgo.RecordRetries(config.RecordRetries),
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
	}
	options = append(options, clientSecurityOptions(config.Security)...)

	client, err := factory(options...)
	if err != nil {
		return nil, err
	}

	return &Producer{
		client:                client,
		limits:                config.Limits,
		keyRequired:           config.KeyPolicy == KeyRequired,
		maxBatchRecords:       config.MaxBatchRecords,
		maxBatchBytes:         int64(config.MaxBatchBytes),
		transactionsEnabled:   config.TransactionalID != "",
		transactionEndTimeout: config.TransactionEndTimeout,
	}, nil
}

func normalizeProducerConfig(config ProducerConfig) (ProducerConfig, error) {
	if err := validateClientIdentity(config.Brokers, config.ClientID); err != nil {
		return ProducerConfig{}, err
	}
	security, err := normalizeClientSecurity(config.Security)
	if err != nil {
		return ProducerConfig{}, err
	}
	config.Security = security
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
	if config.DeliveryTimeout == 0 {
		config.DeliveryTimeout = 30 * time.Second
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
			len(config.TransactionalID) > 255 {
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
		config.DeliveryTimeout < time.Second ||
		config.DeliveryTimeout > 10*time.Minute ||
		config.RequestTimeout < 100*time.Millisecond ||
		config.RequestTimeout > 2*time.Minute ||
		config.RequestTimeout > config.DeliveryTimeout ||
		config.DialTimeout < 100*time.Millisecond ||
		config.DialTimeout > 2*time.Minute ||
		config.Linger < 0 ||
		config.Linger > time.Second ||
		(config.TransactionalID != "" &&
			(config.TransactionTimeout < time.Second ||
				config.TransactionTimeout > 15*time.Minute ||
				config.TransactionEndTimeout < time.Second ||
				config.TransactionEndTimeout > 2*time.Minute)) {
		return ProducerConfig{}, ErrInvalidProducerConfig
	}
	maximumRecordBytes := int64(config.Limits.MaxTopicBytes) +
		int64(config.Limits.MaxKeyBytes) +
		int64(config.Limits.MaxValueBytes) +
		int64(config.Limits.MaxHeaderBytes) +
		1_024
	if int64(config.MaxBatchBytes) < maximumRecordBytes {
		return ProducerConfig{}, ErrInvalidProducerConfig
	}

	return config, nil
}

func validateCompressionPreferences(
	preferences []CompressionCodec,
) error {
	if len(preferences) == 0 || len(preferences) > 5 {
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
		if broker == "" || broker != strings.TrimSpace(broker) || len(broker) > 255 {
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
	if clientID != strings.TrimSpace(clientID) || len(clientID) > 255 {
		return ErrClientIDTooLarge
	}

	return nil
}

// Publish waits for Kafka to accept the message or returns the first delivery
// error. A nil result does not provide end-to-end exactly-once delivery.
func (producer *Producer) Publish(ctx context.Context, message Message) error {
	return producer.PublishRecord(ctx, message).Err
}

// PublishRecord synchronously publishes one record and returns its individual
// broker delivery metadata. The producer owns copies of all input bytes before
// passing the record to franz-go.
func (producer *Producer) PublishRecord(
	ctx context.Context,
	record ProducerRecord,
) DeliveryResult {
	if ctx == nil {
		return DeliveryResult{Topic: record.Topic, Err: ErrContextRequired}
	}
	if err := producer.startOperation(); err != nil {
		return DeliveryResult{Topic: record.Topic, Err: err}
	}
	defer producer.finishOperation()

	return producer.publishRecord(ctx, record)
}

func (producer *Producer) publish(ctx context.Context, message Message) error {
	return producer.publishRecord(ctx, message).Err
}

func (producer *Producer) publishRecord(
	ctx context.Context,
	record ProducerRecord,
) DeliveryResult {
	result := DeliveryResult{Topic: record.Topic}
	if err := record.validate(producer.limits); err != nil {
		result.Err = err

		return result
	}
	if producer.keyRequired && len(record.Key) == 0 {
		result.Err = ErrKeyRequired

		return result
	}
	deliveries := producer.client.ProduceSync(ctx, franzRecord(record.owned()))
	if len(deliveries) != 1 || deliveries[0].Record == nil {
		result.Err = ErrDeliveryResultMissing

		return result
	}
	return deliveryResult(deliveries[0])
}

// PublishBatch validates and owns an entire bounded batch before producing any
// record. Results remain in input order and expose every partial delivery
// failure.
func (producer *Producer) PublishBatch(
	ctx context.Context,
	records []ProducerRecord,
) ([]DeliveryResult, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	if err := producer.startOperation(); err != nil {
		return nil, err
	}
	defer producer.finishOperation()

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
		if producer.keyRequired && len(record.Key) == 0 {
			return nil, ErrKeyRequired
		}
		batchBytes += recordSize(record)
		if batchBytes > producer.maxBatchBytes {
			return nil, ErrBatchTooLarge
		}
		franzRecords[index] = franzRecord(record.owned())
	}

	deliveries := producer.client.ProduceSync(ctx, franzRecords...)
	results := make([]DeliveryResult, len(records))
	var deliveryErrors []error
	for index := range records {
		if index >= len(deliveries) || deliveries[index].Record == nil {
			results[index] = DeliveryResult{
				Topic: records[index].Topic,
				Err:   ErrDeliveryResultMissing,
			}
			deliveryErrors = append(deliveryErrors, ErrDeliveryResultMissing)

			continue
		}
		results[index] = deliveryResult(deliveries[index])
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

// PublishAsync admits one owned record to the bounded franz-go producer and
// returns a one-result buffered channel. The caller may stop waiting without
// cancelling a record already sent to Kafka; franz-go's idempotent producer
// continues resolving that record and publishes the eventual result.
func (producer *Producer) PublishAsync(
	ctx context.Context,
	record ProducerRecord,
) (<-chan DeliveryResult, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}

	if err := producer.startOperation(); err != nil {
		return nil, err
	}

	if err := record.validate(producer.limits); err != nil {
		producer.finishOperation()

		return nil, err
	}
	if producer.keyRequired && len(record.Key) == 0 {
		producer.finishOperation()

		return nil, ErrKeyRequired
	}

	delivery := make(chan DeliveryResult, 1)
	topic := record.Topic
	producer.client.Produce(ctx, franzRecord(record.owned()), func(
		record *kgo.Record,
		err error,
	) {
		defer producer.finishOperation()
		if record == nil {
			delivery <- DeliveryResult{Topic: topic, Err: errors.Join(
				ErrDeliveryResultMissing,
				newDeliveryError(err),
			)}
		} else {
			delivery <- deliveryResult(kgo.ProduceResult{Record: record, Err: err})
		}
		close(delivery)
	})

	return delivery, nil
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
	if err := producer.startOperation(); err != nil {
		return err
	}
	defer producer.finishOperation()

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
	if err := producer.startMaintenance(false); err != nil {
		return err
	}
	defer producer.finishMaintenance(false)

	return drainResult(producer.client.Flush(ctx))
}

// Abort drops records still buffered by franz-go and waits for their delivery
// callbacks to run. It is an explicit data-loss operation intended for
// recovery after the caller has accepted the undelivered-record outcome.
func (producer *Producer) Abort(ctx context.Context) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if err := producer.startMaintenance(true); err != nil {
		return err
	}
	defer producer.finishMaintenance(false)

	return producer.client.AbortBufferedRecords(ctx)
}

// Shutdown fences new operations, drains every admitted record within ctx,
// and closes the underlying client. If draining is incomplete the producer
// remains fenced but open so the caller can retry Shutdown or explicitly
// Abort. Successful shutdown is idempotent.
func (producer *Producer) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return ErrContextRequired
	}

	producer.stateMu.Lock()
	if producer.shutdownComplete {
		producer.stateMu.Unlock()

		return nil
	}
	if producer.maintenanceActive || producer.transactionActive {
		producer.stateMu.Unlock()

		return ErrProducerBusy
	}
	producer.closed = true
	producer.maintenanceActive = true
	producer.stateMu.Unlock()

	if err := drainResult(producer.client.Flush(ctx)); err != nil {
		producer.finishMaintenance(false)

		return err
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

// Transaction is the producer surface available inside RunTransaction.
type Transaction struct {
	session *transactionSession
}

// Publish synchronously publishes one message inside the active transaction.
func (transaction Transaction) Publish(ctx context.Context, message Message) error {
	if transaction.session == nil {
		return ErrTransactionClosed
	}

	return transaction.session.publish(ctx, message)
}

// RunTransaction serializes one producer transaction. The transaction is
// committed only when callback returns nil; callback failure or panic triggers
// a bounded abort whose completion ignores caller cancellation.
func (producer *Producer) RunTransaction(
	ctx context.Context,
	callback func(Transaction) error,
) error {
	if ctx == nil {
		return ErrContextRequired
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

	if err := producer.client.BeginTransaction(); err != nil {
		return err
	}

	session := &transactionSession{producer: producer}
	callbackErr := callTransaction(callback, Transaction{session: session})
	session.closeAndWait()
	if callbackErr != nil {
		return errors.Join(callbackErr, producer.abortTransaction(ctx))
	}

	endCtx, cancel := producer.transactionEndContext(ctx)
	defer cancel()

	commitErr := producer.client.EndTransaction(endCtx, kgo.TryCommit)
	if commitErr == nil {
		return nil
	}
	if errors.Is(commitErr, kerr.OperationNotAttempted) ||
		errors.Is(commitErr, kerr.TransactionAbortable) {
		return errors.Join(commitErr, producer.abortTransaction(ctx))
	}

	return errors.Join(ErrTransactionOutcomeUnknown, commitErr)
}

type transactionSession struct {
	producer *Producer
	mu       sync.Mutex
	active   sync.WaitGroup
	closed   bool
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

	return session.producer.publish(ctx, message)
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

func (producer *Producer) abortTransaction(ctx context.Context) error {
	abortCtx, cancelAbort := producer.transactionEndContext(ctx)
	abortErr := producer.client.AbortBufferedRecords(abortCtx)
	cancelAbort()

	endCtx, cancelEnd := producer.transactionEndContext(ctx)
	endErr := producer.client.EndTransaction(endCtx, kgo.TryAbort)
	cancelEnd()

	return errors.Join(abortErr, endErr)
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
		if len(header.Key) > limits.MaxHeaderBytes-headerBytes {
			return ErrHeadersTooLarge
		}
		headerBytes += len(header.Key)
		if len(header.Value) > limits.MaxHeaderBytes-headerBytes {
			return ErrHeadersTooLarge
		}
		headerBytes += len(header.Value)
	}

	return nil
}

// Close waits for in-flight requests and closes the underlying Kafka client.
func (producer *Producer) Close() {
	producer.stateMu.Lock()
	producer.closed = true
	producer.stateMu.Unlock()

	producer.closeOnce.Do(producer.client.Close)

	producer.stateMu.Lock()
	producer.shutdownComplete = true
	producer.stateMu.Unlock()
}

func (producer *Producer) startOperation() error {
	producer.stateMu.Lock()
	defer producer.stateMu.Unlock()

	if producer.closed {
		return ErrProducerClosed
	}
	if producer.transactionActive {
		return ErrTransactionInProgress
	}
	if producer.maintenanceActive {
		return ErrProducerBusy
	}
	producer.inflight++

	return nil
}

func (producer *Producer) finishOperation() {
	producer.stateMu.Lock()
	producer.inflight--
	producer.stateMu.Unlock()
}

func (producer *Producer) startTransaction() error {
	producer.stateMu.Lock()
	defer producer.stateMu.Unlock()

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

func (producer *Producer) startMaintenance(allowClosed bool) error {
	producer.stateMu.Lock()
	defer producer.stateMu.Unlock()

	if producer.shutdownComplete || (producer.closed && !allowClosed) {
		return ErrProducerClosed
	}
	if producer.maintenanceActive || producer.transactionActive {
		return ErrProducerBusy
	}
	producer.maintenanceActive = true

	return nil
}

func (producer *Producer) finishMaintenance(shutdownComplete bool) {
	producer.stateMu.Lock()
	producer.maintenanceActive = false
	producer.shutdownComplete = producer.shutdownComplete || shutdownComplete
	producer.stateMu.Unlock()
}
