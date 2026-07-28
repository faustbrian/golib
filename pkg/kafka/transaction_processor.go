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
	ErrInvalidTransactionProcessorConfig = errors.New(
		"kafka: transaction processor configuration is invalid",
	)
	ErrTransactionHandlerRequired = errors.New(
		"kafka: transaction processor handler is required",
	)
	ErrTransactionNotCommitted = errors.New(
		"kafka: consume-transform-produce transaction was not committed",
	)
	ErrTooManyTransactionOutputRecords = errors.New(
		"kafka: transaction output record count exceeds configured limit",
	)
	ErrTransactionOutputTooLarge = errors.New(
		"kafka: transaction output bytes exceed configured limit",
	)
	ErrTransactionProcessorBusy = errors.New(
		"kafka: transaction processor runner is already active",
	)
	ErrTransactionProcessorClosing = errors.New(
		"kafka: transaction processor is shutting down",
	)
	ErrTransactionProcessorClosed = errors.New(
		"kafka: transaction processor is closed",
	)
	ErrTransactionProcessorShutdownActive = errors.New(
		"kafka: transaction processor shutdown is already active",
	)
	ErrTransactionProcessorShutdownIncomplete = errors.New(
		"kafka: transaction processor shutdown is incomplete",
	)
	ErrTransactionProcessorFatal = errors.New(
		"kafka: transaction processor entered a fatal state",
	)
)

// TransactionConnectionConfig defines shared broker, identity, protocol, and
// security policy for one consume-transform-produce client.
type TransactionConnectionConfig struct {
	Brokers     []string
	ClientID    string
	Protocol    ProtocolPolicy
	DialTimeout time.Duration
	Security    ClientSecurity
}

// TransactionGroupConfig defines the bounded read-committed consumer-group
// side of one consume-transform-produce client.
type TransactionGroupConfig struct {
	GroupID                string
	InstanceID             string
	Rack                   string
	Topics                 []string
	ResetOffset            OffsetPolicy
	BalancePolicy          GroupBalancePolicy
	MaxPollRecords         int
	MaxConcurrentFetches   int
	FetchMaxBytes          int32
	FetchMaxPartitionBytes int32
	FetchMaxWait           time.Duration
	SessionTimeout         time.Duration
	RebalanceTimeout       time.Duration
	HeartbeatInterval      time.Duration
	ProcessingTimeout      time.Duration
}

// TransactionOutputConfig defines bounded transactional production policy.
// TransactionalID must be unique to one live processor instance.
type TransactionOutputConfig struct {
	AllowedTopics          []string
	KeyPolicy              KeyPolicy
	MaxBufferedRecords     int
	MaxBufferedBytes       int
	MaxBatchBytes          int32
	MaxOutputRecords       int
	MaxOutputBytes         int64
	RecordRetries          int
	RetryBackoffMin        time.Duration
	RetryBackoffMax        time.Duration
	DeliveryTimeout        time.Duration
	RequestTimeout         time.Duration
	Linger                 time.Duration
	CompressionPreferences []CompressionCodec
	TransactionalID        string
	TransactionTimeout     time.Duration
	TransactionEndTimeout  time.Duration
}

// TransactionProcessorConfig composes shared connection, source group, output,
// record-limit, and lifecycle policy for Kafka-only consume-transform-produce.
type TransactionProcessorConfig struct {
	Connection      TransactionConnectionConfig
	Group           TransactionGroupConfig
	Output          TransactionOutputConfig
	Limits          MessageLimits
	Observers       ObserverPolicy
	ShutdownTimeout time.Duration
}

// Validate reports whether the complete processor policy is bounded and
// internally consistent without constructing a client or dialing brokers.
func (config TransactionProcessorConfig) Validate() error {
	_, err := normalizeTransactionProcessorConfig(config)

	return err
}

// TransactionHandler processes one borrowed source record and may publish
// records through the transaction capability. Returning an error aborts the
// complete poll and leaves every source offset unsettled.
type TransactionHandler interface {
	Handle(context.Context, ConsumedRecord, Transaction) error
}

// TransactionHandlerFunc adapts a function to TransactionHandler.
type TransactionHandlerFunc func(context.Context, ConsumedRecord, Transaction) error

// Handle invokes handler.
func (handler TransactionHandlerFunc) Handle(
	ctx context.Context,
	record ConsumedRecord,
	transaction Transaction,
) error {
	return handler(ctx, record, transaction)
}

// TransactionPollResult summarizes one all-or-nothing source poll.
// Published counts broker-acknowledged output records inside the transaction;
// those records are visible to read-committed consumers only when Committed is
// true.
type TransactionPollResult struct {
	Polled    int
	Processed int
	Published int
	Committed bool
}

type transactionProcessorBackend interface {
	PollRecords(context.Context, int) kgo.Fetches
	Begin() error
	ProduceSync(context.Context, ...*kgo.Record) kgo.ProduceResults
	End(context.Context, kgo.TransactionEndTry) (bool, error)
	LeaveGroupContext(context.Context) error
	Close()
}

type transactionProcessorFactory func(
	...kgo.Opt,
) (transactionProcessorBackend, error)

type franzTransactionProcessorBackend struct {
	session *kgo.GroupTransactSession
}

func newFranzTransactionProcessorBackend(
	options ...kgo.Opt,
) (transactionProcessorBackend, error) {
	session, err := kgo.NewGroupTransactSession(options...)
	if err != nil {
		return nil, err
	}

	return &franzTransactionProcessorBackend{session: session}, nil
}

func (backend *franzTransactionProcessorBackend) PollRecords(
	ctx context.Context,
	maxRecords int,
) kgo.Fetches {
	return backend.session.PollRecords(ctx, maxRecords)
}

func (backend *franzTransactionProcessorBackend) Begin() error {
	return backend.session.Begin()
}

func (backend *franzTransactionProcessorBackend) ProduceSync(
	ctx context.Context,
	records ...*kgo.Record,
) kgo.ProduceResults {
	return backend.session.ProduceSync(ctx, records...)
}

func (backend *franzTransactionProcessorBackend) End(
	ctx context.Context,
	try kgo.TransactionEndTry,
) (bool, error) {
	return backend.session.End(ctx, try)
}

func (backend *franzTransactionProcessorBackend) LeaveGroupContext(
	ctx context.Context,
) error {
	return backend.session.Client().LeaveGroupContext(ctx)
}

func (backend *franzTransactionProcessorBackend) Close() {
	backend.session.Close()
}

// TransactionProcessor owns one read-committed group member and transactional
// producer. One Run or RunOnce call may be active at a time.
type TransactionProcessor struct {
	client             transactionProcessorBackend
	clientID           string
	groupID            string
	limits             MessageLimits
	maxPollRecords     int
	processingTimeout  time.Duration
	transactionEndTime time.Duration
	shutdownTimeout    time.Duration
	keyRequired        bool
	allowedTopics      map[string]struct{}
	maxOutputRecords   int
	maxOutputBytes     int64
	staticMembership   bool

	lifecycleMu       sync.Mutex
	running           bool
	runDone           chan struct{}
	closing           bool
	closed            bool
	shutdownActive    bool
	fatalErr          error
	observerCallbacks int
	observers         observerDispatcher
}

// NewTransactionProcessor constructs a Kafka-only consume-transform-produce
// runner. The franz-go client establishes broker connections lazily.
func NewTransactionProcessor(
	config TransactionProcessorConfig,
) (*TransactionProcessor, error) {
	return newTransactionProcessor(config, newFranzTransactionProcessorBackend)
}

func newTransactionProcessor(
	config TransactionProcessorConfig,
	factory transactionProcessorFactory,
) (*TransactionProcessor, error) {
	config, err := normalizeTransactionProcessorConfig(config)
	if err != nil {
		return nil, err
	}

	resetOffset := kgo.NewOffset().AtStart()
	if config.Group.ResetOffset == OffsetLatest {
		resetOffset = kgo.NewOffset().AtEnd()
	}
	options := []kgo.Opt{
		kgo.SeedBrokers(config.Connection.Brokers...),
		kgo.ClientID(config.Connection.ClientID),
		kgo.ConsumerGroup(config.Group.GroupID),
		kgo.ConsumeTopics(config.Group.Topics...),
		kgo.ConsumeResetOffset(resetOffset),
		kgo.DisableAutoCommit(),
		kgo.FetchIsolationLevel(kgo.ReadCommitted()),
		kgo.Balancers(consumerGroupBalancers(config.Group.BalancePolicy)...),
		kgo.MaxConcurrentFetches(config.Group.MaxConcurrentFetches),
		kgo.FetchMaxBytes(config.Group.FetchMaxBytes),
		kgo.FetchMaxPartitionBytes(config.Group.FetchMaxPartitionBytes),
		kgo.FetchMaxWait(config.Group.FetchMaxWait),
		kgo.SessionTimeout(config.Group.SessionTimeout),
		kgo.RebalanceTimeout(config.Group.RebalanceTimeout),
		kgo.HeartbeatInterval(config.Group.HeartbeatInterval),
		kgo.RecordPartitioner(newPolicyPartitioner(
			kgo.UniformBytesPartitioner(64<<10, true, true, nil),
		)),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.StopProducerOnDataLossDetected(),
		kgo.MaxBufferedRecords(config.Output.MaxBufferedRecords),
		kgo.MaxBufferedBytes(config.Output.MaxBufferedBytes),
		kgo.ProducerBatchMaxBytes(config.Output.MaxBatchBytes),
		kgo.RecordRetries(config.Output.RecordRetries),
		kgo.RetryBackoffFn(newProducerRetryBackoff(
			config.Connection.ClientID,
			config.Output.RetryBackoffMin,
			config.Output.RetryBackoffMax,
		)),
		kgo.MetadataMinAge(producerMetadataMinAge(
			config.Output.RetryBackoffMin,
		)),
		kgo.RecordDeliveryTimeout(config.Output.DeliveryTimeout),
		kgo.ProduceRequestTimeout(config.Output.RequestTimeout),
		kgo.ProducerLinger(config.Output.Linger),
		kgo.ProducerBatchCompression(
			franzCompressionCodecs(config.Output.CompressionPreferences)...,
		),
		kgo.TransactionalID(config.Output.TransactionalID),
		kgo.TransactionTimeout(config.Output.TransactionTimeout),
		kgo.DialTimeout(config.Connection.DialTimeout),
	}
	if config.Group.InstanceID != "" {
		options = append(options, kgo.InstanceID(config.Group.InstanceID))
	}
	if config.Group.Rack != "" {
		options = append(options, kgo.Rack(config.Group.Rack))
	}
	options = append(options, clientProtocolOptions(config.Connection.Protocol)...)
	options = append(options, clientSecurityOptions(config.Connection.Security)...)

	dispatcher := newObserverDispatcher(config.Observers)
	allowedTopics := make(map[string]struct{}, len(config.Output.AllowedTopics))
	for _, topic := range config.Output.AllowedTopics {
		allowedTopics[topic] = struct{}{}
	}
	processor := &TransactionProcessor{
		clientID:           strings.Clone(config.Connection.ClientID),
		groupID:            strings.Clone(config.Group.GroupID),
		limits:             config.Limits,
		maxPollRecords:     config.Group.MaxPollRecords,
		processingTimeout:  config.Group.ProcessingTimeout,
		transactionEndTime: config.Output.TransactionEndTimeout,
		shutdownTimeout:    config.ShutdownTimeout,
		keyRequired:        config.Output.KeyPolicy == KeyRequired,
		allowedTopics:      allowedTopics,
		maxOutputRecords:   config.Output.MaxOutputRecords,
		maxOutputBytes:     config.Output.MaxOutputBytes,
		staticMembership:   config.Group.InstanceID != "",
		observers:          dispatcher,
	}
	if dispatcher.enabled() {
		observerHook := newFranzObserverHook(
			config.Connection.ClientID,
			config.Group.GroupID,
			dispatcher,
		)
		observerHook.before = processor.beginObservation
		observerHook.after = processor.finishObservation
		options = append(options, kgo.WithHooks(observerHook))
	}

	client, err := factory(options...)
	if err != nil {
		return nil, err
	}
	processor.client = client

	return processor, nil
}

func normalizeTransactionProcessorConfig(
	config TransactionProcessorConfig,
) (TransactionProcessorConfig, error) {
	observers, err := normalizeObserverPolicy(config.Observers)
	if err != nil {
		return TransactionProcessorConfig{}, errors.Join(
			ErrInvalidTransactionProcessorConfig,
			err,
		)
	}
	config.Observers = observers
	if config.Connection.Protocol.MinimumVersion == "" {
		config.Connection.Protocol.MinimumVersion = "2.5"
	}
	if config.Output.TransactionTimeout == 0 {
		config.Output.TransactionTimeout = 60 * time.Second
	}
	if config.Output.TransactionEndTimeout == 0 {
		config.Output.TransactionEndTimeout = 10 * time.Second
	}

	producer, err := normalizeProducerConfig(ProducerConfig{
		Brokers:                config.Connection.Brokers,
		ClientID:               config.Connection.ClientID,
		Protocol:               config.Connection.Protocol,
		AllowedTopics:          config.Output.AllowedTopics,
		KeyPolicy:              config.Output.KeyPolicy,
		Limits:                 config.Limits,
		MaxBufferedRecords:     config.Output.MaxBufferedRecords,
		MaxBufferedBytes:       config.Output.MaxBufferedBytes,
		MaxBatchBytes:          config.Output.MaxBatchBytes,
		RecordRetries:          config.Output.RecordRetries,
		RetryBackoffMin:        config.Output.RetryBackoffMin,
		RetryBackoffMax:        config.Output.RetryBackoffMax,
		DeliveryTimeout:        config.Output.DeliveryTimeout,
		RequestTimeout:         config.Output.RequestTimeout,
		DialTimeout:            config.Connection.DialTimeout,
		Linger:                 config.Output.Linger,
		CompressionPreferences: config.Output.CompressionPreferences,
		TransactionalID:        config.Output.TransactionalID,
		TransactionTimeout:     config.Output.TransactionTimeout,
		TransactionEndTimeout:  config.Output.TransactionEndTimeout,
		Security:               config.Connection.Security,
	})
	if err != nil || producer.TransactionalID == "" {
		return TransactionProcessorConfig{}, errors.Join(
			ErrInvalidTransactionProcessorConfig,
			err,
		)
	}
	if !kafkaReleaseAtLeast(producer.Protocol.MinimumVersion, 2, 5) {
		return TransactionProcessorConfig{}, ErrInvalidTransactionProcessorConfig
	}
	maxOutputRecords := config.Output.MaxOutputRecords
	if maxOutputRecords == 0 {
		maxOutputRecords = 1_000
	}
	maxOutputBytes := config.Output.MaxOutputBytes
	if maxOutputBytes == 0 {
		maxOutputBytes = 10 << 20
	}
	if maxOutputRecords < 1 ||
		maxOutputRecords > 100_000 ||
		maxOutputBytes < maximumRecordPolicyBytes(producer.Limits) ||
		maxOutputBytes > 1<<30 {
		return TransactionProcessorConfig{}, ErrInvalidTransactionProcessorConfig
	}
	consumer, err := normalizeConsumerConfig(ConsumerConfig{
		Brokers:                config.Connection.Brokers,
		ClientID:               config.Connection.ClientID,
		Protocol:               config.Connection.Protocol,
		GroupID:                config.Group.GroupID,
		InstanceID:             config.Group.InstanceID,
		Rack:                   config.Group.Rack,
		Topics:                 config.Group.Topics,
		ResetOffset:            config.Group.ResetOffset,
		BalancePolicy:          config.Group.BalancePolicy,
		Limits:                 config.Limits,
		MaxPollRecords:         config.Group.MaxPollRecords,
		MaxConcurrentFetches:   config.Group.MaxConcurrentFetches,
		FetchMaxBytes:          config.Group.FetchMaxBytes,
		FetchMaxPartitionBytes: config.Group.FetchMaxPartitionBytes,
		FetchMaxWait:           config.Group.FetchMaxWait,
		SessionTimeout:         config.Group.SessionTimeout,
		RebalanceTimeout:       config.Group.RebalanceTimeout,
		HeartbeatInterval:      config.Group.HeartbeatInterval,
		HandlerTimeout:         config.Group.ProcessingTimeout,
		CommitTimeout:          producer.TransactionEndTimeout,
		ShutdownTimeout:        config.ShutdownTimeout,
		DialTimeout:            config.Connection.DialTimeout,
		Security:               config.Connection.Security,
	})
	if err != nil {
		return TransactionProcessorConfig{}, errors.Join(
			ErrInvalidTransactionProcessorConfig,
			err,
		)
	}
	if consumer.HandlerTimeout+producer.TransactionEndTimeout >=
		producer.TransactionTimeout {
		return TransactionProcessorConfig{}, ErrInvalidTransactionProcessorConfig
	}
	sourceTopics := make(map[string]struct{}, len(consumer.Topics))
	for _, topic := range consumer.Topics {
		sourceTopics[topic] = struct{}{}
	}
	for _, topic := range producer.AllowedTopics {
		if _, source := sourceTopics[topic]; source {
			return TransactionProcessorConfig{}, ErrInvalidTransactionProcessorConfig
		}
	}

	config.Connection = TransactionConnectionConfig{
		Brokers:     append([]string(nil), consumer.Brokers...),
		ClientID:    consumer.ClientID,
		Protocol:    consumer.Protocol,
		DialTimeout: consumer.DialTimeout,
		Security:    consumer.Security,
	}
	config.Group = TransactionGroupConfig{
		GroupID:                consumer.GroupID,
		InstanceID:             consumer.InstanceID,
		Rack:                   consumer.Rack,
		Topics:                 append([]string(nil), consumer.Topics...),
		ResetOffset:            consumer.ResetOffset,
		BalancePolicy:          consumer.BalancePolicy,
		MaxPollRecords:         consumer.MaxPollRecords,
		MaxConcurrentFetches:   consumer.MaxConcurrentFetches,
		FetchMaxBytes:          consumer.FetchMaxBytes,
		FetchMaxPartitionBytes: consumer.FetchMaxPartitionBytes,
		FetchMaxWait:           consumer.FetchMaxWait,
		SessionTimeout:         consumer.SessionTimeout,
		RebalanceTimeout:       consumer.RebalanceTimeout,
		HeartbeatInterval:      consumer.HeartbeatInterval,
		ProcessingTimeout:      consumer.HandlerTimeout,
	}
	config.Output = TransactionOutputConfig{
		AllowedTopics:      append([]string(nil), producer.AllowedTopics...),
		KeyPolicy:          producer.KeyPolicy,
		MaxBufferedRecords: producer.MaxBufferedRecords,
		MaxBufferedBytes:   producer.MaxBufferedBytes,
		MaxBatchBytes:      producer.MaxBatchBytes,
		MaxOutputRecords:   maxOutputRecords,
		MaxOutputBytes:     maxOutputBytes,
		RecordRetries:      producer.RecordRetries,
		RetryBackoffMin:    producer.RetryBackoffMin,
		RetryBackoffMax:    producer.RetryBackoffMax,
		DeliveryTimeout:    producer.DeliveryTimeout,
		RequestTimeout:     producer.RequestTimeout,
		Linger:             producer.Linger,
		CompressionPreferences: append(
			[]CompressionCodec(nil),
			producer.CompressionPreferences...,
		),
		TransactionalID:       producer.TransactionalID,
		TransactionTimeout:    producer.TransactionTimeout,
		TransactionEndTimeout: producer.TransactionEndTimeout,
	}
	config.Limits = producer.Limits
	config.Observers = observers
	config.ShutdownTimeout = consumer.ShutdownTimeout

	return config, nil
}

// RunOnce polls at most the configured source-record limit. Every fetched
// record must complete successfully before output records and all source
// offsets are committed in one Kafka transaction.
func (processor *TransactionProcessor) RunOnce(
	ctx context.Context,
	handler TransactionHandler,
) (TransactionPollResult, error) {
	if ctx == nil {
		return TransactionPollResult{}, ErrContextRequired
	}
	if isObserverContext(ctx) {
		return TransactionPollResult{}, ErrObserverReentry
	}
	if handler == nil {
		return TransactionPollResult{}, ErrTransactionHandlerRequired
	}
	if err := processor.beginRun(); err != nil {
		return TransactionPollResult{}, err
	}
	defer processor.endRun()

	return processor.runOnce(ctx, handler)
}

func (processor *TransactionProcessor) runOnce(
	ctx context.Context,
	handler TransactionHandler,
) (TransactionPollResult, error) {
	fetches := processor.client.PollRecords(ctx, processor.maxPollRecords)
	records := fetches.Records()
	result := TransactionPollResult{Polled: len(records)}
	if err := fetches.Err(); err != nil {
		if len(records) == 0 {
			return TransactionPollResult{}, err
		}
		if transactionErr := processor.beginTransaction(ctx); transactionErr != nil {
			processor.fence(transactionErr)

			return result, errors.Join(err, transactionErr)
		}
		abortErr := processor.abortTransaction(ctx)
		if abortErr != nil {
			processor.fence(abortErr)
		}

		return result, errors.Join(err, abortErr)
	}
	if len(records) == 0 {
		return TransactionPollResult{}, nil
	}
	if transactionErr := processor.beginTransaction(ctx); transactionErr != nil {
		processor.fence(transactionErr)

		return result, transactionErr
	}

	publisher := &processorTransactionPublisher{
		client:         processor.client,
		limits:         processor.limits,
		keyRequired:    processor.keyRequired,
		allowedTopics:  processor.allowedTopics,
		maxOutputCount: processor.maxOutputRecords,
		maxOutputBytes: processor.maxOutputBytes,
	}
	workCtx, cancelWork := context.WithTimeout(ctx, processor.processingTimeout)
	var processingErr error
	for _, record := range records {
		message, err := consumedMessageWithinLimits(record, processor.limits)
		if err == nil {
			session := &transactionSession{publisher: publisher}
			err = callTransactionHandler(
				workCtx,
				handler,
				message,
				Transaction{session: session},
			)
			session.closeAndWait()
			if publishErr := publisher.failure(); publishErr != nil {
				err = errors.Join(err, publishErr)
			}
			if cause := context.Cause(workCtx); cause != nil {
				err = errors.Join(err, cause)
			}
		}
		if err != nil {
			processingErr = err

			break
		}
		result.Processed++
	}
	cancelWork()
	result.Published = publisher.publishedCount()
	if processingErr != nil {
		abortErr := processor.abortTransaction(ctx)
		if abortErr != nil {
			processor.fence(abortErr)
		}

		return result, errors.Join(processingErr, abortErr)
	}

	_, err := processor.commitTransaction(ctx)
	if err != nil {
		if !errors.Is(err, ErrTransactionNotCommitted) {
			processor.fence(err)
		}

		return result, err
	}
	result.Committed = true

	return result, nil
}

func callTransactionHandler(
	ctx context.Context,
	handler TransactionHandler,
	record ConsumedRecord,
	transaction Transaction,
) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrHandlerPanic
		}
	}()

	return handler.Handle(ctx, record, transaction)
}

type processorTransactionPublisher struct {
	client         transactionProcessorBackend
	limits         MessageLimits
	keyRequired    bool
	allowedTopics  map[string]struct{}
	maxOutputCount int
	maxOutputBytes int64
	mu             sync.Mutex
	published      int
	reservedCount  int
	reservedBytes  int64
	err            error
}

func (publisher *processorTransactionPublisher) publish(
	ctx context.Context,
	record ProducerRecord,
) error {
	if err := record.validate(publisher.limits); err != nil {
		publisher.recordFailure(err)

		return err
	}
	if _, allowed := publisher.allowedTopics[record.Topic]; !allowed {
		publisher.recordFailure(ErrTopicNotAllowed)

		return ErrTopicNotAllowed
	}
	if publisher.keyRequired && len(record.Key) == 0 {
		publisher.recordFailure(ErrKeyRequired)

		return ErrKeyRequired
	}
	if err := publisher.reserve(record); err != nil {
		return err
	}
	deliveries := publisher.client.ProduceSync(ctx, franzRecord(record.owned()))
	if len(deliveries) != 1 || deliveries[0].Record == nil {
		err := newDeliveryError(ErrDeliveryResultMissing)
		publisher.recordFailure(err)

		return err
	}
	if deliveries[0].Err != nil {
		err := newDeliveryError(deliveries[0].Err)
		publisher.recordFailure(err)

		return err
	}
	publisher.mu.Lock()
	publisher.published++
	publisher.mu.Unlock()

	return nil
}

func (publisher *processorTransactionPublisher) reserve(
	record ProducerRecord,
) error {
	size := recordSize(record)
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	var err error
	if publisher.reservedCount >= publisher.maxOutputCount {
		err = ErrTooManyTransactionOutputRecords
	} else if size > publisher.maxOutputBytes-publisher.reservedBytes {
		err = ErrTransactionOutputTooLarge
	}
	if err != nil {
		if publisher.err == nil {
			publisher.err = err
		}

		return err
	}
	publisher.reservedCount++
	publisher.reservedBytes += size

	return nil
}

func (publisher *processorTransactionPublisher) recordFailure(err error) {
	publisher.mu.Lock()
	if publisher.err == nil {
		publisher.err = err
	}
	publisher.mu.Unlock()
}

func (publisher *processorTransactionPublisher) failure() error {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()

	return publisher.err
}

func (publisher *processorTransactionPublisher) publishedCount() int {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()

	return publisher.published
}

func (processor *TransactionProcessor) beginTransaction(ctx context.Context) error {
	var startedAt time.Time
	if processor.observers.enabled() {
		startedAt = time.Now()
	}
	err := newTransactionError(
		TransactionOperationBegin,
		processor.client.Begin(),
		false,
		true,
	)
	processor.observeTransaction(
		ctx,
		ObservationTransactionBegin,
		startedAt,
		err,
	)

	return err
}

func (processor *TransactionProcessor) commitTransaction(
	ctx context.Context,
) (bool, error) {
	var startedAt time.Time
	if processor.observers.enabled() {
		startedAt = time.Now()
	}
	endCtx, cancel := processor.transactionEndContext(ctx)
	committed, endErr := processor.client.End(endCtx, kgo.TryCommit)
	cancel()

	var err error
	if endErr != nil {
		err = transactionProcessorEndError(TransactionOperationCommit, endErr)
	} else if !committed {
		err = newTransactionErrorWithCategory(
			TransactionOperationCommit,
			ErrTransactionNotCommitted,
			ErrorRetryable,
			true,
			true,
		)
	}
	processor.observeTransaction(
		ctx,
		ObservationTransactionCommit,
		startedAt,
		err,
	)

	return committed, err
}

func (processor *TransactionProcessor) abortTransaction(ctx context.Context) error {
	var startedAt time.Time
	if processor.observers.enabled() {
		startedAt = time.Now()
	}
	endCtx, cancel := processor.transactionEndContext(ctx)
	committed, endErr := processor.client.End(endCtx, kgo.TryAbort)
	cancel()

	var err error
	if endErr != nil {
		err = transactionProcessorEndError(TransactionOperationAbort, endErr)
	} else if committed {
		err = newTransactionError(
			TransactionOperationAbort,
			ErrTransactionOutcomeUnknown,
			false,
			false,
		)
	}
	processor.observeTransaction(
		ctx,
		ObservationTransactionAbort,
		startedAt,
		err,
	)

	return err
}

func transactionProcessorEndError(
	operation TransactionOperation,
	err error,
) error {
	category := classifyError(err)
	if category == ErrorAuthorization ||
		category == ErrorFenced ||
		category == ErrorFatal {
		return newTransactionError(operation, err, false, true)
	}
	if errors.Is(err, kerr.TransactionAbortable) ||
		errors.Is(err, kerr.OperationNotAttempted) {
		return newTransactionError(operation, err, true, true)
	}

	return newTransactionError(
		operation,
		errors.Join(ErrTransactionOutcomeUnknown, err),
		false,
		false,
	)
}

func (processor *TransactionProcessor) transactionEndContext(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	return context.WithTimeout(
		context.WithoutCancel(ctx),
		processor.transactionEndTime,
	)
}

// Run executes bounded all-or-nothing polls until cancellation or the first
// processing or transaction failure. Caller cancellation is a clean stop only
// after the active transaction aborts successfully; cleanup failure is
// returned and fences the processor.
func (processor *TransactionProcessor) Run(
	ctx context.Context,
	handler TransactionHandler,
) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if isObserverContext(ctx) {
		return ErrObserverReentry
	}
	if handler == nil {
		return ErrTransactionHandlerRequired
	}
	if err := processor.beginRun(); err != nil {
		return err
	}
	defer processor.endRun()

	for ctx.Err() == nil {
		if _, err := processor.runOnce(ctx, handler); err != nil {
			if ctx.Err() != nil {
				processor.lifecycleMu.Lock()
				fatal := processor.fatalErr != nil
				processor.lifecycleMu.Unlock()
				if fatal {
					return err
				}

				return nil
			}

			return err
		}
	}

	return nil
}

func (processor *TransactionProcessor) beginRun() error {
	processor.lifecycleMu.Lock()
	defer processor.lifecycleMu.Unlock()
	if processor.closed {
		return ErrTransactionProcessorClosed
	}
	if processor.closing {
		return ErrTransactionProcessorClosing
	}
	if processor.fatalErr != nil {
		return processor.fatalErr
	}
	if processor.running {
		return ErrTransactionProcessorBusy
	}
	processor.running = true
	processor.runDone = make(chan struct{})

	return nil
}

func (processor *TransactionProcessor) fence(err error) {
	processor.lifecycleMu.Lock()
	if processor.fatalErr == nil {
		processor.fatalErr = errors.Join(ErrTransactionProcessorFatal, err)
	}
	processor.lifecycleMu.Unlock()
}

func (processor *TransactionProcessor) endRun() {
	processor.lifecycleMu.Lock()
	done := processor.runDone
	processor.runDone = nil
	processor.running = false
	processor.lifecycleMu.Unlock()
	close(done)
}

// Shutdown fences new runs, waits for the active runner, leaves a dynamic
// group, and closes the client. An incomplete shutdown remains retryable.
func (processor *TransactionProcessor) Shutdown(ctx context.Context) (err error) {
	if ctx == nil {
		return ErrContextRequired
	}
	if isObserverContext(ctx) {
		return ErrObserverReentry
	}
	processor.lifecycleMu.Lock()
	if processor.observerCallbacks != 0 {
		processor.lifecycleMu.Unlock()

		return ErrObserverReentry
	}
	if processor.closed {
		processor.lifecycleMu.Unlock()

		return nil
	}
	if processor.shutdownActive {
		processor.lifecycleMu.Unlock()

		return ErrTransactionProcessorShutdownActive
	}
	processor.closing = true
	processor.shutdownActive = true
	done := processor.runDone
	staticMembership := processor.staticMembership
	processor.lifecycleMu.Unlock()
	var startedAt time.Time
	if processor.observers.enabled() {
		startedAt = time.Now()
		defer func() {
			processor.observeShutdown(ctx, startedAt, err)
		}()
	}

	complete := false
	defer func() {
		processor.lifecycleMu.Lock()
		processor.shutdownActive = false
		if complete {
			processor.closed = true
		}
		processor.lifecycleMu.Unlock()
	}()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return errors.Join(
				ErrTransactionProcessorShutdownIncomplete,
				ctx.Err(),
			)
		}
	}
	if !staticMembership {
		if leaveErr := processor.client.LeaveGroupContext(ctx); leaveErr != nil {
			return errors.Join(
				ErrTransactionProcessorShutdownIncomplete,
				leaveErr,
			)
		}
	}
	processor.client.Close()
	complete = true

	return nil
}

// Close performs a bounded graceful shutdown using the configured timeout.
func (processor *TransactionProcessor) Close() error {
	processor.lifecycleMu.Lock()
	if processor.observerCallbacks != 0 {
		processor.lifecycleMu.Unlock()

		return ErrObserverReentry
	}
	processor.lifecycleMu.Unlock()

	ctx, cancel := context.WithTimeout(
		context.Background(),
		processor.shutdownTimeout,
	)
	defer cancel()

	return processor.Shutdown(ctx)
}

func (processor *TransactionProcessor) observeShutdown(
	ctx context.Context,
	startedAt time.Time,
	err error,
) {
	observation := Observation{
		Kind:      ObservationTransactionProcessorShutdown,
		StartedAt: startedAt,
		Duration:  time.Since(startedAt),
		ClientID:  processor.clientID,
		GroupID:   processor.groupID,
		Succeeded: err == nil,
	}
	if err != nil {
		observation.Category = classifyError(err)
	}
	processor.dispatchObservation(ctx, observation)
}

func (processor *TransactionProcessor) observeTransaction(
	ctx context.Context,
	kind ObservationKind,
	startedAt time.Time,
	err error,
) {
	if !processor.observers.enabled() {
		return
	}
	observation := Observation{
		Kind:      kind,
		StartedAt: startedAt,
		Duration:  time.Since(startedAt),
		ClientID:  processor.clientID,
		GroupID:   processor.groupID,
		Succeeded: err == nil,
	}
	if err != nil {
		observation.Category = transactionObservationCategory(err)
	}
	processor.dispatchObservation(ctx, observation)
}

func (processor *TransactionProcessor) dispatchObservation(
	ctx context.Context,
	observation Observation,
) {
	processor.beginObservation()
	defer processor.finishObservation()

	processor.observers.observe(ctx, observation)
}

func (processor *TransactionProcessor) beginObservation() {
	processor.lifecycleMu.Lock()
	processor.observerCallbacks++
	processor.lifecycleMu.Unlock()
}

func (processor *TransactionProcessor) finishObservation() {
	processor.lifecycleMu.Lock()
	processor.observerCallbacks--
	processor.lifecycleMu.Unlock()
}
