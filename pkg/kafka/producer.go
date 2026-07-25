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
	ErrBrokersRequired      = errors.New("kafka: at least one broker is required")
	ErrTooManyBrokers       = errors.New("kafka: broker count exceeds configured limit")
	ErrInvalidBroker        = errors.New("kafka: broker address is invalid")
	ErrDuplicateBroker      = errors.New("kafka: broker address is duplicated")
	ErrClientIDRequired     = errors.New("kafka: client ID is required")
	ErrClientIDTooLarge     = errors.New("kafka: client ID exceeds configured limit")
	ErrTopicRequired        = errors.New("kafka: topic is required")
	ErrTopicTooLarge        = errors.New("kafka: topic exceeds configured limit")
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
	ErrTransactionsDisabled      = errors.New("kafka: producer transactions are disabled")
	ErrTransactionRequired       = errors.New("kafka: transaction callback is required")
	ErrTransactionPanic          = errors.New("kafka: transaction callback panicked")
	ErrTransactionClosed         = errors.New("kafka: transaction is closed")
	ErrTransactionOutcomeUnknown = errors.New(
		"kafka: transaction commit outcome is unknown",
	)
)

// ProducerConfig defines the Kafka seed brokers and client identity.
type ProducerConfig struct {
	Brokers            []string
	ClientID           string
	Limits             MessageLimits
	MaxBufferedRecords int
	MaxBatchBytes      int32
	RecordRetries      int
	DeliveryTimeout    time.Duration
	RequestTimeout     time.Duration
	DialTimeout        time.Duration
	Linger             time.Duration
	TransactionalID    string
	TransactionTimeout time.Duration

	TransactionEndTimeout time.Duration
	Security              ClientSecurity
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

// Message is one Kafka record. Key controls partition ordering.
type Message struct {
	Topic   string
	Key     []byte
	Value   []byte
	Headers []Header
}

type producerBackend interface {
	ProduceSync(context.Context, ...*kgo.Record) kgo.ProduceResults
	Ping(context.Context) error
	BeginTransaction() error
	AbortBufferedRecords(context.Context) error
	EndTransaction(context.Context, kgo.TransactionEndTry) error
	Close()
}

type producerClientFactory func(...kgo.Opt) (*kgo.Client, error)

// Producer publishes records with Kafka's idempotent producer and all in-sync
// replica acknowledgements.
type Producer struct {
	client                producerBackend
	limits                MessageLimits
	transactionsEnabled   bool
	transactionEndTimeout time.Duration
	transactionMu         sync.Mutex
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
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.MaxBufferedRecords(config.MaxBufferedRecords),
		kgo.ProducerBatchMaxBytes(config.MaxBatchBytes),
		kgo.RecordRetries(config.RecordRetries),
		kgo.RecordDeliveryTimeout(config.DeliveryTimeout),
		kgo.ProduceRequestTimeout(config.RequestTimeout),
		kgo.DialTimeout(config.DialTimeout),
		kgo.ProducerLinger(config.Linger),
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
	if config.MaxBufferedRecords == 0 {
		config.MaxBufferedRecords = 1_000
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
	producer.transactionMu.Lock()
	defer producer.transactionMu.Unlock()

	return producer.publish(ctx, message)
}

func (producer *Producer) publish(ctx context.Context, message Message) error {
	if err := message.validate(producer.limits); err != nil {
		return err
	}

	headers := make([]kgo.RecordHeader, len(message.Headers))
	for index, header := range message.Headers {
		headers[index] = kgo.RecordHeader{
			Key:   header.Key,
			Value: header.Value,
		}
	}

	return producer.client.ProduceSync(ctx, &kgo.Record{
		Topic:   message.Topic,
		Key:     message.Key,
		Value:   message.Value,
		Headers: headers,
	}).FirstErr()
}

// Health verifies that a broker is reachable and responds to a metadata
// request.
func (producer *Producer) Health(ctx context.Context) error {
	producer.transactionMu.Lock()
	defer producer.transactionMu.Unlock()

	return producer.client.Ping(ctx)
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
	if !producer.transactionsEnabled {
		return ErrTransactionsDisabled
	}
	if callback == nil {
		return ErrTransactionRequired
	}

	producer.transactionMu.Lock()
	defer producer.transactionMu.Unlock()

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

func (message Message) validate(limits MessageLimits) error {
	if message.Topic == "" {
		return ErrTopicRequired
	}
	if len(message.Topic) > limits.MaxTopicBytes {
		return ErrTopicTooLarge
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
	producer.transactionMu.Lock()
	defer producer.transactionMu.Unlock()

	producer.client.Close()
}
