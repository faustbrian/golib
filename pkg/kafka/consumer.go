package kafka

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

var (
	ErrGroupIDRequired               = errors.New("kafka: consumer group ID is required")
	ErrGroupIDTooLarge               = errors.New("kafka: consumer group ID exceeds configured limit")
	ErrInvalidGroupID                = errors.New("kafka: consumer group ID is invalid")
	ErrInvalidInstanceID             = errors.New("kafka: consumer instance ID is invalid")
	ErrInvalidRack                   = errors.New("kafka: consumer rack is invalid")
	ErrInvalidBalancePolicy          = errors.New("kafka: consumer balance policy is invalid")
	ErrInvalidRebalanceHandlerPolicy = errors.New(
		"kafka: consumer rebalance handler policy is invalid",
	)
	ErrTopicsRequired          = errors.New("kafka: at least one topic is required")
	ErrTooManyTopics           = errors.New("kafka: topic count exceeds configured limit")
	ErrDuplicateTopic          = errors.New("kafka: topic is duplicated")
	ErrInvalidOffsetPolicy     = errors.New("kafka: consumer offset policy is invalid")
	ErrHandlerRequired         = errors.New("kafka: consumer handler is required")
	ErrBatchHandlerRequired    = errors.New("kafka: consumer batch handler is required")
	ErrHandlerPanic            = errors.New("kafka: consumer handler panicked")
	ErrConsumerBusy            = errors.New("kafka: consumer runner is already active")
	ErrConsumerDraining        = errors.New("kafka: consumer is draining")
	ErrConsumerDrainActive     = errors.New("kafka: consumer drain is already active")
	ErrConsumerDrainIncomplete = errors.New(
		"kafka: consumer drain is incomplete",
	)
	ErrConsumerClosing        = errors.New("kafka: consumer is shutting down")
	ErrConsumerClosed         = errors.New("kafka: consumer is closed")
	ErrConsumerFatal          = errors.New("kafka: consumer entered a fatal state")
	ErrConsumerInstanceFenced = errors.New(
		"kafka: consumer static instance was fenced",
	)
	ErrConsumerShutdownActive = errors.New(
		"kafka: consumer shutdown is already active",
	)
	ErrConsumerShutdownIncomplete = errors.New(
		"kafka: consumer shutdown is incomplete",
	)
	ErrPausePartitionsRequired = errors.New(
		"kafka: at least one pause partition is required",
	)
	ErrTooManyPausedPartitions = errors.New(
		"kafka: paused partition count exceeds configured limit",
	)
	ErrInvalidPausePartition = errors.New(
		"kafka: pause partition is invalid",
	)
	ErrDuplicatePausePartition = errors.New(
		"kafka: pause partition is duplicated",
	)
	ErrTooManyAssignedPartitions = errors.New(
		"kafka: assigned partition count exceeds configured limit",
	)
	ErrTooManyFetchedRecords = errors.New(
		"kafka: fetched record count exceeds configured limit",
	)
	ErrInvalidAssignment = errors.New(
		"kafka: consumer assignment is invalid",
	)
	ErrConsumerOwnershipLost = errors.New(
		"kafka: consumer partition ownership was lost",
	)
	ErrConsumerRebalance = errors.New(
		"kafka: consumer handler canceled for a pending rebalance",
	)
	ErrPauseTopicNotSubscribed = errors.New(
		"kafka: pause topic is not subscribed",
	)
	ErrInvalidConsumerConfig = errors.New(
		"kafka: consumer configuration is outside bounded limits",
	)
)

var errConsumerDrainRequested = errors.New("kafka: consumer drain requested")

// OffsetPolicy controls the first offset used when no committed group offset
// exists.
type OffsetPolicy uint8

const (
	OffsetEarliest OffsetPolicy = iota + 1
	OffsetLatest
)

// GroupBalancePolicy selects the consumer-group partition assignment and
// rebalance protocol.
type GroupBalancePolicy uint8

const (
	// BalanceCooperativeSticky is the safe default for new groups and avoids
	// revoking every assignment during a rebalance.
	BalanceCooperativeSticky GroupBalancePolicy = iota
	// BalanceEagerSticky revokes all assignments during each rebalance for
	// compatibility with eager group members.
	BalanceEagerSticky
	// BalanceEagerToCooperative advertises eager sticky first and cooperative
	// sticky second for the first rolling deployment of a migration. A second
	// deployment must select BalanceCooperativeSticky.
	BalanceEagerToCooperative
)

// RebalanceHandlerPolicy controls active handlers when franz-go reports that a
// group rebalance callback is waiting for the current poll to finish.
type RebalanceHandlerPolicy uint8

const (
	// RebalanceCancelHandler requests cancellation through every active handler
	// context. It is the safe zero-value policy because it releases rebalances
	// promptly.
	RebalanceCancelHandler RebalanceHandlerPolicy = iota
	// RebalanceDrainHandler lets active handlers finish within their configured
	// deadlines, then settles successful results before releasing the rebalance.
	RebalanceDrainHandler
)

// ConsumerConfig defines one bounded consumer-group member.
type ConsumerConfig struct {
	Brokers               []string
	ClientID              string
	Protocol              ProtocolPolicy
	GroupID               string
	InstanceID            string
	Rack                  string
	Topics                []string
	ResetOffset           OffsetPolicy
	BalancePolicy         GroupBalancePolicy
	RebalanceHandler      RebalanceHandlerPolicy
	Limits                MessageLimits
	MaxPollRecords        int
	MaxPausedPartitions   int
	MaxAssignedPartitions int
	MaxConcurrentFetches  int
	// MaxConcurrentHandlers bounds simultaneous callbacks across independent
	// topic partitions. One partition always remains sequential. The zero
	// value defaults to one.
	MaxConcurrentHandlers int
	// FetchMinBytes is the minimum encoded record bytes a broker tries to
	// collect before answering a fetch. Zero defaults to one byte, values must
	// not exceed FetchMaxBytes, and FetchMaxWait bounds the wait.
	FetchMinBytes          int32
	FetchMaxBytes          int32
	FetchMaxPartitionBytes int32
	// BrokerMaxReadBytes is the hard maximum encoded Kafka response accepted
	// from one broker connection. It must be at least FetchMaxBytes.
	BrokerMaxReadBytes int32
	// MaxDecompressedBatchBytes is the hard decoded-byte limit for one Kafka
	// record batch before records are admitted to package handlers.
	MaxDecompressedBatchBytes int64
	// MaxBufferedDecompressedBytes bounds decoded compressed-batch memory held
	// across active and prefetched Kafka responses.
	MaxBufferedDecompressedBytes int64
	FetchMaxWait                 time.Duration

	SessionTimeout    time.Duration
	RebalanceTimeout  time.Duration
	HeartbeatInterval time.Duration
	HandlerTimeout    time.Duration
	CommitTimeout     time.Duration
	ShutdownTimeout   time.Duration
	DialTimeout       time.Duration
	Security          ClientSecurity
	Observers         ObserverPolicy
}

// Validate reports whether the consumer configuration satisfies the bounded
// group policy without constructing a client or dialing brokers.
func (config ConsumerConfig) Validate() error {
	_, err := normalizeConsumerConfig(config)

	return err
}

// Handler durably processes one consumed message before its offset may be
// committed. Implementations must be concurrency-safe when the consumer
// permits more than one concurrent handler.
type Handler interface {
	Handle(context.Context, ConsumedMessage) error
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(context.Context, ConsumedMessage) error

// Handle invokes handler.
func (handler HandlerFunc) Handle(ctx context.Context, message ConsumedMessage) error {
	return handler(ctx, message)
}

// PollResult summarizes one bounded fetch, processing, and commit cycle.
type PollResult struct {
	Polled    int
	Processed int
	Committed int
}

// ConsumerAssignment is a copied snapshot of the member's current partition
// ownership. Epoch is a package-local lifecycle fence, not Kafka's broker
// generation ID. Lost reports that the latest lifecycle transition was a
// fatal ownership loss.
type ConsumerAssignment struct {
	Epoch      uint64
	Partitions []TopicPartition
	Lost       bool
}

type consumerBackend interface {
	PollRecords(context.Context, int) kgo.Fetches
	CommitRecords(context.Context, ...*kgo.Record) error
	AllowRebalance()
	LeaveGroupContext(context.Context) error
	PauseFetchPartitions(map[string][]int32) map[string][]int32
	ResumeFetchPartitions(map[string][]int32)
	Close()
}

type consumerClientFactory func(...kgo.Opt) (*kgo.Client, error)

// Consumer processes records with explicit post-handler offset commits. One
// Run, RunOnce, or RunBatchOnce call may be active at a time. Handler callbacks
// can overlap only across independent partitions when MaxConcurrentHandlers is
// greater than one. Duplicate static-instance fencing permanently rejects new
// runners with ErrConsumerFatal and ErrConsumerInstanceFenced.
// Its methods are safe for concurrent lifecycle coordination.
type Consumer struct {
	client                consumerBackend
	clientID              string
	groupID               string
	limits                MessageLimits
	maxPollRecords        int
	maxPausedPartitions   int
	maxConcurrentHandlers int
	assignment            *consumerAssignmentState
	rebalance             *consumerRebalanceState
	handlerTimeout        time.Duration
	commitTimeout         time.Duration
	shutdownTimeout       time.Duration
	staticMembership      bool
	observers             observerDispatcher

	lifecycleMu       sync.Mutex
	running           bool
	runDone           chan struct{}
	drainRequested    bool
	drainActive       bool
	pollCancel        context.CancelCauseFunc
	closing           bool
	closed            bool
	shutdownActive    bool
	fatalErr          error
	subscribedTopics  map[string]struct{}
	pausedPartitions  map[TopicPartition]struct{}
	observerCallbacks int
}

// NewConsumer constructs a group consumer with automatic commits disabled and
// cooperative rebalancing blocked while each bounded poll is processed.
func NewConsumer(config ConsumerConfig) (*Consumer, error) {
	return newConsumer(config, kgo.NewClient)
}

func newConsumer(
	config ConsumerConfig,
	factory consumerClientFactory,
) (*Consumer, error) {
	config, err := normalizeConsumerConfig(config)
	if err != nil {
		return nil, err
	}

	resetOffset := kgo.NewOffset().AtStart()
	if config.ResetOffset == OffsetLatest {
		resetOffset = kgo.NewOffset().AtEnd()
	}

	assignment := newConsumerAssignmentState(
		config.MaxAssignedPartitions,
		config.Topics,
	)
	rebalance := newConsumerRebalanceState(config.RebalanceHandler)
	decompressor, decompressionBudget := newFetchDecompressionPolicy(
		config.MaxDecompressedBatchBytes,
		config.MaxBufferedDecompressedBytes,
	)
	var consumer *Consumer
	options := []kgo.Opt{
		kgo.SeedBrokers(config.Brokers...),
		kgo.AlwaysRetryEOF(),
		kgo.ClientID(config.ClientID),
		kgo.ConsumerGroup(config.GroupID),
		kgo.ConsumeTopics(config.Topics...),
		kgo.ConsumeStartOffset(resetOffset),
		kgo.ConsumeResetOffset(resetOffset),
		kgo.DisableAutoCommit(),
		kgo.BlockRebalanceOnPoll(),
		kgo.OnPartitionsCallbackBlocked(func(context.Context, *kgo.Client) {
			consumer.onRebalanceBlocked()
		}),
		kgo.OnPartitionsAssigned(func(
			_ context.Context,
			_ *kgo.Client,
			partitions map[string][]int32,
		) {
			consumer.onPartitionsAssigned(partitions)
		}),
		kgo.OnPartitionsRevoked(func(
			_ context.Context,
			_ *kgo.Client,
			partitions map[string][]int32,
		) {
			consumer.onPartitionsRevoked(partitions)
		}),
		kgo.OnPartitionsLost(func(
			_ context.Context,
			_ *kgo.Client,
			partitions map[string][]int32,
		) {
			consumer.onPartitionsLost(partitions)
		}),
		kgo.Balancers(consumerGroupBalancers(config.BalancePolicy)...),
		kgo.MaxConcurrentFetches(config.MaxConcurrentFetches),
		kgo.FetchMinBytes(config.FetchMinBytes),
		kgo.FetchMaxBytes(config.FetchMaxBytes),
		kgo.FetchMaxPartitionBytes(config.FetchMaxPartitionBytes),
		kgo.BrokerMaxReadBytes(config.BrokerMaxReadBytes),
		kgo.WithDecompressor(decompressor),
		kgo.WithPools(decompressionBudget),
		kgo.FetchMaxWait(config.FetchMaxWait),
		kgo.SessionTimeout(config.SessionTimeout),
		kgo.RebalanceTimeout(config.RebalanceTimeout),
		kgo.HeartbeatInterval(config.HeartbeatInterval),
		kgo.DialTimeout(config.DialTimeout),
	}
	if config.InstanceID != "" {
		options = append(options, kgo.InstanceID(config.InstanceID))
	}
	if config.Rack != "" {
		options = append(options, kgo.Rack(config.Rack))
	}
	options = append(options, clientProtocolOptions(config.Protocol)...)
	options = append(options, clientSecurityOptions(config.Security)...)
	dispatcher := newObserverDispatcher(config.Observers)
	subscribedTopics := make(map[string]struct{}, len(config.Topics))
	for _, topic := range config.Topics {
		subscribedTopics[topic] = struct{}{}
	}
	consumer = &Consumer{
		clientID:              strings.Clone(config.ClientID),
		groupID:               strings.Clone(config.GroupID),
		limits:                config.Limits,
		maxPollRecords:        config.MaxPollRecords,
		maxPausedPartitions:   config.MaxPausedPartitions,
		maxConcurrentHandlers: config.MaxConcurrentHandlers,
		assignment:            assignment,
		rebalance:             rebalance,
		handlerTimeout:        config.HandlerTimeout,
		commitTimeout:         config.CommitTimeout,
		shutdownTimeout:       config.ShutdownTimeout,
		staticMembership:      config.InstanceID != "",
		observers:             dispatcher,
		subscribedTopics:      subscribedTopics,
		pausedPartitions:      make(map[TopicPartition]struct{}),
	}
	if dispatcher.enabled() {
		observerHook := newFranzObserverHook(
			config.ClientID,
			config.GroupID,
			dispatcher,
		)
		observerHook.before = consumer.beginObservation
		observerHook.after = consumer.finishObservation
		options = append(options, kgo.WithHooks(observerHook))
	}
	options = append(options, kgo.WithHooks(
		consumerGroupManageErrorHook{consumer: consumer},
	))

	client, err := factory(options...)
	if err != nil {
		return nil, err
	}
	consumer.client = client

	return consumer, nil
}

func consumerGroupBalancers(policy GroupBalancePolicy) []kgo.GroupBalancer {
	if policy == BalanceEagerSticky {
		return []kgo.GroupBalancer{kgo.StickyBalancer()}
	}
	if policy == BalanceEagerToCooperative {
		return []kgo.GroupBalancer{
			kgo.StickyBalancer(),
			kgo.CooperativeStickyBalancer(),
		}
	}

	return []kgo.GroupBalancer{kgo.CooperativeStickyBalancer()}
}

func normalizeConsumerConfig(config ConsumerConfig) (ConsumerConfig, error) {
	if err := validateClientIdentity(config.Brokers, config.ClientID); err != nil {
		return ConsumerConfig{}, err
	}
	if err := config.Protocol.Validate(); err != nil {
		return ConsumerConfig{}, err
	}
	security, err := normalizeClientSecurity(config.Security)
	if err != nil {
		return ConsumerConfig{}, err
	}
	config.Security = security
	observers, err := normalizeObserverPolicy(config.Observers)
	if err != nil {
		return ConsumerConfig{}, err
	}
	config.Observers = observers
	if strings.TrimSpace(config.GroupID) == "" {
		return ConsumerConfig{}, ErrGroupIDRequired
	}
	if len(config.GroupID) > 255 {
		return ConsumerConfig{}, ErrGroupIDTooLarge
	}
	if config.GroupID != strings.TrimSpace(config.GroupID) ||
		!validKafkaText(config.GroupID, 255) {
		return ConsumerConfig{}, ErrInvalidGroupID
	}
	if !validOptionalConsumerIdentity(config.InstanceID) {
		return ConsumerConfig{}, ErrInvalidInstanceID
	}
	if !validOptionalConsumerIdentity(config.Rack) {
		return ConsumerConfig{}, ErrInvalidRack
	}
	if config.BalancePolicy > BalanceEagerToCooperative {
		return ConsumerConfig{}, ErrInvalidBalancePolicy
	}
	if config.RebalanceHandler > RebalanceDrainHandler {
		return ConsumerConfig{}, ErrInvalidRebalanceHandlerPolicy
	}
	if config.Limits == (MessageLimits{}) {
		config.Limits = DefaultMessageLimits()
	}
	if err := config.Limits.Validate(); err != nil {
		return ConsumerConfig{}, err
	}
	if len(config.Topics) == 0 {
		return ConsumerConfig{}, ErrTopicsRequired
	}
	if len(config.Topics) > 64 {
		return ConsumerConfig{}, ErrTooManyTopics
	}
	seenTopics := make(map[string]struct{}, len(config.Topics))
	for _, topic := range config.Topics {
		if !validKafkaTopicName(topic, config.Limits.MaxTopicBytes) {
			return ConsumerConfig{}, ErrInvalidTopic
		}
		if _, exists := seenTopics[topic]; exists {
			return ConsumerConfig{}, ErrDuplicateTopic
		}
		seenTopics[topic] = struct{}{}
	}
	if config.ResetOffset != OffsetEarliest && config.ResetOffset != OffsetLatest {
		return ConsumerConfig{}, ErrInvalidOffsetPolicy
	}
	if config.MaxPollRecords == 0 {
		config.MaxPollRecords = 100
	}
	if config.MaxPausedPartitions == 0 {
		config.MaxPausedPartitions = 256
	}
	if config.MaxAssignedPartitions == 0 {
		config.MaxAssignedPartitions = 1_024
	}
	if config.MaxConcurrentFetches == 0 {
		config.MaxConcurrentFetches = 4
	}
	if config.MaxConcurrentHandlers == 0 {
		config.MaxConcurrentHandlers = 1
	}
	if config.FetchMinBytes == 0 {
		config.FetchMinBytes = 1
	}
	if config.FetchMaxBytes == 0 {
		config.FetchMaxBytes = 50 << 20
	}
	if config.FetchMaxPartitionBytes == 0 {
		config.FetchMaxPartitionBytes = 1 << 20
	}
	brokerMaximumBytes, decompressedMaximumBytes,
		bufferedDecompressedMaximumBytes, validFetchSafety :=
		normalizeFetchSafety(
			config.FetchMaxBytes,
			config.Limits,
			config.BrokerMaxReadBytes,
			config.MaxDecompressedBatchBytes,
			config.MaxBufferedDecompressedBytes,
		)
	if !validFetchSafety {
		return ConsumerConfig{}, ErrInvalidConsumerConfig
	}
	config.BrokerMaxReadBytes = brokerMaximumBytes
	config.MaxDecompressedBatchBytes = decompressedMaximumBytes
	config.MaxBufferedDecompressedBytes = bufferedDecompressedMaximumBytes
	if config.FetchMaxWait == 0 {
		config.FetchMaxWait = 500 * time.Millisecond
	}
	if config.SessionTimeout == 0 {
		config.SessionTimeout = 45 * time.Second
	}
	if config.RebalanceTimeout == 0 {
		config.RebalanceTimeout = 60 * time.Second
	}
	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = 3 * time.Second
	}
	if config.HandlerTimeout == 0 {
		config.HandlerTimeout = 30 * time.Second
	}
	if config.CommitTimeout == 0 {
		config.CommitTimeout = 10 * time.Second
	}
	if config.ShutdownTimeout == 0 {
		config.ShutdownTimeout = 30 * time.Second
	}
	if config.DialTimeout == 0 {
		config.DialTimeout = 10 * time.Second
	}
	if config.MaxPollRecords < 1 ||
		config.MaxPollRecords > 1_000 ||
		config.MaxPausedPartitions < 1 ||
		config.MaxPausedPartitions > 1_024 ||
		config.MaxAssignedPartitions < 1 ||
		config.MaxAssignedPartitions > 65_536 ||
		config.MaxConcurrentFetches < 1 ||
		config.MaxConcurrentFetches > 64 ||
		config.MaxConcurrentHandlers < 1 ||
		config.MaxConcurrentHandlers > 64 ||
		config.FetchMinBytes < 1 ||
		config.FetchMinBytes > config.FetchMaxBytes ||
		config.FetchMaxBytes > 100<<20 ||
		config.FetchMaxPartitionBytes < 1<<20 ||
		config.FetchMaxPartitionBytes > config.FetchMaxBytes ||
		config.FetchMaxWait < time.Millisecond ||
		config.FetchMaxWait > 30*time.Second ||
		config.SessionTimeout < time.Second ||
		config.SessionTimeout > 6*time.Minute ||
		config.RebalanceTimeout > 10*time.Minute ||
		config.HeartbeatInterval < 100*time.Millisecond ||
		config.HeartbeatInterval >= config.SessionTimeout ||
		config.HandlerTimeout < time.Second ||
		config.CommitTimeout < 100*time.Millisecond ||
		config.CommitTimeout > 2*time.Minute ||
		config.ShutdownTimeout < 100*time.Millisecond ||
		config.ShutdownTimeout > 15*time.Minute ||
		config.DialTimeout < 100*time.Millisecond ||
		config.DialTimeout > 2*time.Minute ||
		config.HeartbeatInterval+config.HandlerTimeout+config.CommitTimeout >=
			config.RebalanceTimeout {
		return ConsumerConfig{}, ErrInvalidConsumerConfig
	}

	return config, nil
}

func validOptionalConsumerIdentity(value string) bool {
	return value == "" ||
		(value == strings.TrimSpace(value) && validKafkaText(value, 255))
}

// RunOnce polls at most the configured record limit and processes each
// partition in fetch order, with bounded concurrency across partitions. Each
// partition stops at its first handler failure; successful contiguous prefixes
// from that partition and independent partitions are committed before the
// first handler error in stable poll-partition order is returned. It returns
// ErrContextRequired for a nil context, ErrConsumerBusy when another runner
// owns the consumer, ErrConsumerFatal and ErrConsumerInstanceFenced after
// static-membership fencing, and a lifecycle error once shutdown begins.
func (consumer *Consumer) RunOnce(ctx context.Context, handler Handler) (PollResult, error) {
	if ctx == nil {
		return PollResult{}, ErrContextRequired
	}
	if isObserverContext(ctx) {
		return PollResult{}, ErrObserverReentry
	}
	if handler == nil {
		return PollResult{}, ErrHandlerRequired
	}
	if err := consumer.beginRun(); err != nil {
		return PollResult{}, err
	}
	defer consumer.endRun()

	return consumer.runOnce(ctx, handler)
}

func (consumer *Consumer) runOnce(
	ctx context.Context,
	handler Handler,
) (result PollResult, resultErr error) {
	var startedAt time.Time
	if consumer.observers.enabled() {
		startedAt = time.Now()
	}
	consumer.rebalance.beginPoll()
	defer consumer.rebalance.endPoll()

	pollCtx, finishPoll, admitted := consumer.beginPoll(ctx)
	if !admitted {
		return PollResult{}, nil
	}
	fetches := consumer.client.PollRecords(pollCtx, consumer.maxPollRecords)
	drainInterrupted := errors.Is(
		context.Cause(pollCtx),
		errConsumerDrainRequested,
	)
	finishPoll()
	defer consumer.client.AllowRebalance()

	records := fetches.Records()
	defer recycleFetchedRecords(records)
	if consumer.observers.enabled() {
		defer func() {
			consumer.observeConsumerPoll(
				ctx,
				startedAt,
				records,
				result,
				resultErr,
			)
		}()
	}
	defer func() {
		resultErr = consumer.groupError(resultErr)
	}()
	result = PollResult{Polled: len(records)}
	if len(records) > consumer.maxPollRecords {
		return result, errors.Join(
			ErrTooManyFetchedRecords,
			newConsumerError(ConsumerOperationPoll, fetches.Err()),
		)
	}
	if err := fetches.Err(); err != nil {
		if drainInterrupted && errors.Is(err, context.Canceled) {
			return PollResult{}, nil
		}
		return PollResult{}, consumer.groupError(
			newConsumerError(ConsumerOperationPoll, err),
		)
	}
	token, err := consumer.assignment.token()
	if err != nil {
		return result, err
	}
	if len(records) == 0 {
		return PollResult{}, nil
	}

	batches := partitionBatches(records)
	partitionResults := runConsumerPartitionWorkers(
		batches,
		consumer.maxConcurrentHandlers,
		func(batch consumerPartitionBatch) consumerPartitionResult {
			return consumer.processRecordPartition(ctx, token, handler, batch)
		},
	)

	return consumer.settlePartitionResults(ctx, token, result, partitionResults)
}

func (consumer *Consumer) observeConsumerPoll(
	ctx context.Context,
	startedAt time.Time,
	records []*kgo.Record,
	result PollResult,
	err error,
) {
	topic, partitions, bytes := consumer.consumedObservationMetadata(records)
	observation := Observation{
		Kind:           ObservationConsumePoll,
		StartedAt:      startedAt,
		Duration:       time.Since(startedAt),
		ClientID:       consumer.clientID,
		GroupID:        consumer.groupID,
		Topic:          topic,
		RecordCount:    min(result.Polled, consumer.maxPollRecords),
		PartitionCount: partitions,
		ProcessedCount: result.Processed,
		CommittedCount: result.Committed,
		RecordBytes:    bytes,
		Succeeded:      err == nil,
		Truncated:      result.Polled > consumer.maxPollRecords,
	}
	if err != nil {
		observation.Category = classifyConsumerObservationError(err)
	}
	consumer.dispatchObservation(ctx, observation)
}

func (consumer *Consumer) consumedObservationMetadata(
	records []*kgo.Record,
) (string, int, int64) {
	if len(records) == 0 || len(records) > consumer.maxPollRecords {
		return "", 0, 0
	}

	topic := records[0].Topic
	partitions := make(map[TopicPartition]struct{}, len(records))
	var bytes int64
	for _, record := range records {
		if _, err := consumedMessageWithinLimits(record, consumer.limits); err != nil {
			return "", 0, 0
		}
		if record.Topic != topic {
			topic = ""
		}
		partitions[TopicPartition{
			Topic: record.Topic, Partition: record.Partition,
		}] = struct{}{}
		bytes += consumedRecordSize(record)
	}

	return strings.Clone(topic), len(partitions), bytes
}

// Assignment returns a sorted, copied snapshot of current assignment state.
// Its package-local epoch changes at every assign, revoke, or loss callback.
// Invalid or oversized broker-controlled callback metadata fails closed and is
// returned until the member loses its assignment and rejoins.
func (consumer *Consumer) Assignment() (ConsumerAssignment, error) {
	return consumer.assignment.snapshot()
}

func (consumer *Consumer) onPartitionsAssigned(partitions map[string][]int32) {
	startedAt := time.Now()
	transition := consumer.assignment.assigned(partitions)
	consumer.observeConsumerRebalance(
		ObservationConsumeAssigned,
		startedAt,
		transition,
	)
}

func (consumer *Consumer) onPartitionsRevoked(partitions map[string][]int32) {
	startedAt := time.Now()
	transition := consumer.assignment.revoked(partitions)
	consumer.observeConsumerRebalance(
		ObservationConsumeRevoked,
		startedAt,
		transition,
	)
}

func (consumer *Consumer) onPartitionsLost(partitions map[string][]int32) {
	startedAt := time.Now()
	partitionCount, truncated := boundedCallbackPartitionCount(
		partitions,
		consumer.assignment.maximum,
	)
	consumer.assignment.lost()
	consumer.observeConsumerRebalance(
		ObservationConsumeLost,
		startedAt,
		consumerAssignmentTransition{
			partitionCount: partitionCount,
			truncated:      truncated,
			err:            ErrConsumerOwnershipLost,
			category:       ErrorFenced,
		},
	)
}

func (consumer *Consumer) onRebalanceBlocked() {
	startedAt := time.Now()
	if !consumer.rebalance.blocked() {
		return
	}
	consumer.observeConsumerRebalance(
		ObservationConsumeBlocked,
		startedAt,
		consumerAssignmentTransition{},
	)
}

func (consumer *Consumer) observeConsumerRebalance(
	kind ObservationKind,
	startedAt time.Time,
	transition consumerAssignmentTransition,
) {
	if !consumer.observers.enabled() {
		return
	}
	observation := Observation{
		Kind:           kind,
		StartedAt:      startedAt,
		Duration:       time.Since(startedAt),
		ClientID:       consumer.clientID,
		GroupID:        consumer.groupID,
		PartitionCount: transition.partitionCount,
		Succeeded:      transition.err == nil,
		Truncated:      transition.truncated,
	}
	if transition.err != nil {
		observation.Category = transition.category
		if observation.Category == ErrorUnknown {
			observation.Category = classifyError(transition.err)
		}
	}
	consumer.dispatchObservation(context.Background(), observation)
}

func boundedCallbackPartitionCount(
	partitions map[string][]int32,
	maximum int,
) (int, bool) {
	if len(partitions) > maximum {
		return 0, true
	}
	count := 0
	for _, topicPartitions := range partitions {
		if len(topicPartitions) > maximum-count {
			return maximum, true
		}
		count += len(topicPartitions)
	}

	return count, false
}

// Run continuously executes bounded poll cycles until cancellation or the
// first processing failure. Context cancellation is a clean runner stop. It
// returns ErrContextRequired for a nil context, ErrConsumerBusy when another
// runner owns the consumer, ErrConsumerFatal and ErrConsumerInstanceFenced
// after static-membership fencing, and a lifecycle error once shutdown begins.
func (consumer *Consumer) Run(ctx context.Context, handler Handler) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if isObserverContext(ctx) {
		return ErrObserverReentry
	}
	if handler == nil {
		return ErrHandlerRequired
	}
	if err := consumer.beginRun(); err != nil {
		return err
	}
	defer consumer.endRun()

	for ctx.Err() == nil {
		if _, err := consumer.runOnce(ctx, handler); err != nil {
			if ctx.Err() != nil {
				return nil
			}

			return err
		}
		if consumer.drainPending() {
			return nil
		}
	}

	return consumer.groupError(nil)
}

func (consumer *Consumer) beginRun() error {
	consumer.lifecycleMu.Lock()
	defer consumer.lifecycleMu.Unlock()

	if err := consumer.lifecycleErrorLocked(); err != nil {
		return err
	}
	if consumer.running {
		return ErrConsumerBusy
	}
	consumer.running = true
	consumer.runDone = make(chan struct{})

	return nil
}

func (consumer *Consumer) beginPoll(
	ctx context.Context,
) (context.Context, func(), bool) {
	consumer.lifecycleMu.Lock()
	defer consumer.lifecycleMu.Unlock()

	if consumer.drainRequested || consumer.closing || consumer.closed {
		return nil, nil, false
	}
	pollCtx, cancel := context.WithCancelCause(ctx)
	consumer.pollCancel = cancel

	return pollCtx, func() {
		consumer.lifecycleMu.Lock()
		consumer.pollCancel = nil
		consumer.lifecycleMu.Unlock()
		cancel(nil)
	}, true
}

func (consumer *Consumer) drainPending() bool {
	consumer.lifecycleMu.Lock()
	defer consumer.lifecycleMu.Unlock()

	return consumer.drainRequested
}

func (consumer *Consumer) lifecycleErrorLocked() error {
	if consumer.closed {
		return ErrConsumerClosed
	}
	if consumer.closing {
		return ErrConsumerClosing
	}
	if consumer.fatalErr != nil {
		return errors.Join(ErrConsumerFatal, consumer.fatalErr)
	}
	if consumer.drainRequested {
		return ErrConsumerDraining
	}

	return nil
}

// Drain stops an idle poll, lets an admitted poll finish processing and
// settlement, and waits for the active runner without leaving the group or
// closing the client. New runs are fenced until a successful drain completes.
// A context failure returns ErrConsumerDrainIncomplete and leaves the drain
// retriable. Concurrent drains return ErrConsumerDrainActive.
func (consumer *Consumer) Drain(ctx context.Context) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if isObserverContext(ctx) {
		return ErrObserverReentry
	}

	consumer.lifecycleMu.Lock()
	if consumer.observerCallbacks != 0 {
		consumer.lifecycleMu.Unlock()

		return ErrObserverReentry
	}
	if consumer.closed {
		consumer.lifecycleMu.Unlock()

		return ErrConsumerClosed
	}
	if consumer.closing {
		consumer.lifecycleMu.Unlock()

		return ErrConsumerClosing
	}
	if consumer.fatalErr != nil {
		err := errors.Join(ErrConsumerFatal, consumer.fatalErr)
		consumer.lifecycleMu.Unlock()

		return err
	}
	if consumer.drainActive {
		consumer.lifecycleMu.Unlock()

		return ErrConsumerDrainActive
	}
	consumer.drainRequested = true
	consumer.drainActive = true
	done := consumer.runDone
	cancelPoll := consumer.pollCancel
	consumer.lifecycleMu.Unlock()

	if cancelPoll != nil {
		cancelPoll(errConsumerDrainRequested)
	}
	complete := false
	defer func() {
		consumer.lifecycleMu.Lock()
		consumer.drainActive = false
		if complete {
			consumer.drainRequested = false
		}
		consumer.lifecycleMu.Unlock()
	}()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return errors.Join(ErrConsumerDrainIncomplete, ctx.Err())
		}
	}
	complete = true

	return consumer.groupError(nil)
}

type consumerGroupManageErrorHook struct {
	consumer *Consumer
}

func (hook consumerGroupManageErrorHook) OnGroupManageError(err error) {
	hook.consumer.recordFatalGroupError(err)
}

func (consumer *Consumer) recordFatalGroupError(err error) {
	if !errors.Is(err, kerr.FencedInstanceID) {
		return
	}
	consumer.lifecycleMu.Lock()
	if consumer.fatalErr == nil {
		consumer.fatalErr = errors.Join(ErrConsumerInstanceFenced, err)
	}
	consumer.lifecycleMu.Unlock()
}

func (consumer *Consumer) groupError(err error) error {
	consumer.recordFatalGroupError(err)
	consumer.lifecycleMu.Lock()
	fatalErr := consumer.fatalErr
	consumer.lifecycleMu.Unlock()
	if fatalErr == nil {
		return err
	}
	if errors.Is(err, ErrConsumerFatal) {
		return err
	}

	return errors.Join(ErrConsumerFatal, fatalErr, err)
}

func (consumer *Consumer) endRun() {
	consumer.lifecycleMu.Lock()
	done := consumer.runDone
	consumer.runDone = nil
	consumer.running = false
	consumer.lifecycleMu.Unlock()
	close(done)
}

// PausePartitions stops future fetches for explicit subscribed partitions.
// Records already buffered or returned by the current poll can still be
// processed. Pauses persist across rebalances until explicitly resumed.
func (consumer *Consumer) PausePartitions(partitions ...TopicPartition) error {
	consumer.lifecycleMu.Lock()
	defer consumer.lifecycleMu.Unlock()

	if consumer.observerCallbacks != 0 {
		return ErrObserverReentry
	}
	if err := consumer.lifecycleErrorLocked(); err != nil {
		return err
	}
	requested, err := consumer.pausePartitionMap(partitions)
	if err != nil {
		return err
	}
	additional := 0
	for _, partition := range partitions {
		if _, exists := consumer.pausedPartitions[partition]; !exists {
			additional++
		}
	}
	if len(consumer.pausedPartitions)+additional > consumer.maxPausedPartitions {
		return ErrTooManyPausedPartitions
	}

	consumer.client.PauseFetchPartitions(requested)
	for _, partition := range partitions {
		consumer.pausedPartitions[partition] = struct{}{}
	}

	return nil
}

// ResumePartitions resumes future fetches for explicit subscribed partitions.
// Partitions that are not paused are unchanged.
func (consumer *Consumer) ResumePartitions(partitions ...TopicPartition) error {
	consumer.lifecycleMu.Lock()
	defer consumer.lifecycleMu.Unlock()

	if consumer.observerCallbacks != 0 {
		return ErrObserverReentry
	}
	if err := consumer.lifecycleErrorLocked(); err != nil {
		return err
	}
	requested, err := consumer.pausePartitionMap(partitions)
	if err != nil {
		return err
	}

	consumer.client.ResumeFetchPartitions(requested)
	for _, partition := range partitions {
		delete(consumer.pausedPartitions, partition)
	}

	return nil
}

// PausedPartitions returns a sorted snapshot of explicitly paused partitions.
func (consumer *Consumer) PausedPartitions() []TopicPartition {
	consumer.lifecycleMu.Lock()
	defer consumer.lifecycleMu.Unlock()

	paused := make([]TopicPartition, 0, len(consumer.pausedPartitions))
	for partition := range consumer.pausedPartitions {
		paused = append(paused, partition)
	}
	slices.SortFunc(paused, func(left, right TopicPartition) int {
		if topicOrder := cmp.Compare(left.Topic, right.Topic); topicOrder != 0 {
			return topicOrder
		}

		return cmp.Compare(left.Partition, right.Partition)
	})

	return paused
}

func (consumer *Consumer) pausePartitionMap(
	partitions []TopicPartition,
) (map[string][]int32, error) {
	if len(partitions) == 0 {
		return nil, ErrPausePartitionsRequired
	}
	if len(partitions) > consumer.maxPausedPartitions {
		return nil, ErrTooManyPausedPartitions
	}

	seen := make(map[TopicPartition]struct{}, len(partitions))
	requested := make(map[string][]int32)
	for _, partition := range partitions {
		if !validKafkaTopicName(partition.Topic, consumer.limits.MaxTopicBytes) ||
			partition.Partition < 0 {
			return nil, ErrInvalidPausePartition
		}
		if _, subscribed := consumer.subscribedTopics[partition.Topic]; !subscribed {
			return nil, ErrPauseTopicNotSubscribed
		}
		if _, duplicate := seen[partition]; duplicate {
			return nil, ErrDuplicatePausePartition
		}
		seen[partition] = struct{}{}
		requested[partition.Topic] = append(
			requested[partition.Topic],
			partition.Partition,
		)
	}

	return requested, nil
}

func callHandler(
	ctx context.Context,
	handler Handler,
	message ConsumedMessage,
) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrHandlerPanic
		}
	}()

	return handler.Handle(ctx, message)
}

func consumedMessage(record *kgo.Record) ConsumedMessage {
	headers := make([]Header, len(record.Headers))
	for index, header := range record.Headers {
		headers[index] = Header{Key: header.Key, Value: header.Value}
	}

	return ConsumedMessage{
		Topic:         record.Topic,
		Key:           record.Key,
		Value:         record.Value,
		Headers:       headers,
		Timestamp:     record.Timestamp,
		TimestampType: TimestampType(record.Attrs.TimestampType()),
		Partition:     record.Partition,
		Offset:        record.Offset,
		LeaderEpoch:   record.LeaderEpoch,
	}
}

func consumedMessageWithinLimits(
	record *kgo.Record,
	limits MessageLimits,
) (ConsumedMessage, error) {
	if len(record.Headers) > limits.MaxHeaders {
		return ConsumedMessage{}, ErrTooManyHeaders
	}

	message := consumedMessage(record)
	err := (ProducerRecord{
		Topic:   message.Topic,
		Key:     message.Key,
		Value:   message.Value,
		Headers: message.Headers,
	}).validate(limits)
	if err != nil {
		return ConsumedMessage{}, err
	}

	return message, nil
}

// Shutdown fences new runs, interrupts an idle poll without canceling admitted
// handlers, waits for their settlement, and closes the Kafka client. Dynamic
// members leave the group before close; static members preserve their
// membership window. A context or leave failure is joined with
// ErrConsumerShutdownIncomplete and leaves the consumer fenced so Shutdown can
// be retried. A nil context returns ErrContextRequired without fencing the
// consumer. Concurrent shutdown calls return ErrConsumerShutdownActive;
// shutdown during Drain returns ErrConsumerDrainActive.
func (consumer *Consumer) Shutdown(ctx context.Context) (err error) {
	if ctx == nil {
		return ErrContextRequired
	}
	if isObserverContext(ctx) {
		return ErrObserverReentry
	}

	consumer.lifecycleMu.Lock()
	if consumer.observerCallbacks != 0 {
		consumer.lifecycleMu.Unlock()

		return ErrObserverReentry
	}
	if consumer.closed {
		consumer.lifecycleMu.Unlock()

		return nil
	}
	if consumer.shutdownActive {
		consumer.lifecycleMu.Unlock()

		return ErrConsumerShutdownActive
	}
	if consumer.drainActive {
		consumer.lifecycleMu.Unlock()

		return ErrConsumerDrainActive
	}
	consumer.closing = true
	consumer.drainRequested = true
	consumer.shutdownActive = true
	done := consumer.runDone
	cancelPoll := consumer.pollCancel
	staticMembership := consumer.staticMembership
	consumer.lifecycleMu.Unlock()
	if cancelPoll != nil {
		cancelPoll(errConsumerDrainRequested)
	}
	var startedAt time.Time
	if consumer.observers.enabled() {
		startedAt = time.Now()
		defer func() {
			consumer.observeShutdown(ctx, startedAt, err)
		}()
	}

	complete := false
	defer func() {
		consumer.lifecycleMu.Lock()
		consumer.shutdownActive = false
		if complete {
			consumer.closed = true
		}
		consumer.lifecycleMu.Unlock()
	}()

	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return errors.Join(ErrConsumerShutdownIncomplete, ctx.Err())
		}
	}
	if !staticMembership {
		if leaveErr := consumer.client.LeaveGroupContext(ctx); leaveErr != nil {
			return errors.Join(
				ErrConsumerShutdownIncomplete,
				newConsumerError(ConsumerOperationLeave, leaveErr),
			)
		}
	}
	consumer.client.Close()
	complete = true

	return nil
}

// Close performs a bounded graceful shutdown using the configured timeout.
func (consumer *Consumer) Close() error {
	consumer.lifecycleMu.Lock()
	if consumer.observerCallbacks != 0 {
		consumer.lifecycleMu.Unlock()

		return ErrObserverReentry
	}
	consumer.lifecycleMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), consumer.shutdownTimeout)
	defer cancel()

	return consumer.Shutdown(ctx)
}

func (consumer *Consumer) observeShutdown(
	ctx context.Context,
	startedAt time.Time,
	err error,
) {
	observation := Observation{
		Kind:      ObservationConsumerShutdown,
		StartedAt: startedAt,
		Duration:  time.Since(startedAt),
		ClientID:  consumer.clientID,
		GroupID:   consumer.groupID,
		Succeeded: err == nil,
	}
	if err != nil {
		observation.Category = classifyError(err)
	}
	consumer.dispatchObservation(ctx, observation)
}
