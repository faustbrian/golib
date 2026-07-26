package kafka

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

var (
	ErrGroupIDRequired       = errors.New("kafka: consumer group ID is required")
	ErrGroupIDTooLarge       = errors.New("kafka: consumer group ID exceeds configured limit")
	ErrInvalidGroupID        = errors.New("kafka: consumer group ID is invalid")
	ErrInvalidInstanceID     = errors.New("kafka: consumer instance ID is invalid")
	ErrInvalidRack           = errors.New("kafka: consumer rack is invalid")
	ErrInvalidBalancePolicy  = errors.New("kafka: consumer balance policy is invalid")
	ErrTopicsRequired        = errors.New("kafka: at least one topic is required")
	ErrTooManyTopics         = errors.New("kafka: topic count exceeds configured limit")
	ErrDuplicateTopic        = errors.New("kafka: topic is duplicated")
	ErrInvalidOffsetPolicy   = errors.New("kafka: consumer offset policy is invalid")
	ErrHandlerRequired       = errors.New("kafka: consumer handler is required")
	ErrHandlerPanic          = errors.New("kafka: consumer handler panicked")
	ErrInvalidConsumerConfig = errors.New(
		"kafka: consumer configuration is outside bounded limits",
	)
)

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

// ConsumerConfig defines one bounded consumer-group member.
type ConsumerConfig struct {
	Brokers                []string
	ClientID               string
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

	SessionTimeout    time.Duration
	RebalanceTimeout  time.Duration
	HeartbeatInterval time.Duration
	HandlerTimeout    time.Duration
	CommitTimeout     time.Duration
	DialTimeout       time.Duration
	Security          ClientSecurity
}

// Handler durably processes one consumed message before its offset may be
// committed.
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

type consumerBackend interface {
	PollRecords(context.Context, int) kgo.Fetches
	CommitRecords(context.Context, ...*kgo.Record) error
	AllowRebalance()
	Close()
}

type consumerClientFactory func(...kgo.Opt) (*kgo.Client, error)

// Consumer processes records with explicit post-handler offset commits.
type Consumer struct {
	client         consumerBackend
	maxPollRecords int
	handlerTimeout time.Duration
	commitTimeout  time.Duration
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

	options := []kgo.Opt{
		kgo.SeedBrokers(config.Brokers...),
		kgo.ClientID(config.ClientID),
		kgo.ConsumerGroup(config.GroupID),
		kgo.ConsumeTopics(config.Topics...),
		kgo.ConsumeStartOffset(resetOffset),
		kgo.ConsumeResetOffset(resetOffset),
		kgo.DisableAutoCommit(),
		kgo.BlockRebalanceOnPoll(),
		kgo.Balancers(consumerGroupBalancers(config.BalancePolicy)...),
		kgo.MaxConcurrentFetches(config.MaxConcurrentFetches),
		kgo.FetchMaxBytes(config.FetchMaxBytes),
		kgo.FetchMaxPartitionBytes(config.FetchMaxPartitionBytes),
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
	options = append(options, clientSecurityOptions(config.Security)...)

	client, err := factory(options...)
	if err != nil {
		return nil, err
	}

	return &Consumer{
		client:         client,
		maxPollRecords: config.MaxPollRecords,
		handlerTimeout: config.HandlerTimeout,
		commitTimeout:  config.CommitTimeout,
	}, nil
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
	security, err := normalizeClientSecurity(config.Security)
	if err != nil {
		return ConsumerConfig{}, err
	}
	config.Security = security
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
	if len(config.Topics) == 0 {
		return ConsumerConfig{}, ErrTopicsRequired
	}
	if len(config.Topics) > 64 {
		return ConsumerConfig{}, ErrTooManyTopics
	}
	seenTopics := make(map[string]struct{}, len(config.Topics))
	for _, topic := range config.Topics {
		if !validKafkaTopicName(topic, 249) {
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
	if config.MaxConcurrentFetches == 0 {
		config.MaxConcurrentFetches = 4
	}
	if config.FetchMaxBytes == 0 {
		config.FetchMaxBytes = 50 << 20
	}
	if config.FetchMaxPartitionBytes == 0 {
		config.FetchMaxPartitionBytes = 1 << 20
	}
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
	if config.DialTimeout == 0 {
		config.DialTimeout = 10 * time.Second
	}
	if config.MaxPollRecords < 1 ||
		config.MaxPollRecords > 1_000 ||
		config.MaxConcurrentFetches < 1 ||
		config.MaxConcurrentFetches > 64 ||
		config.FetchMaxBytes < 1<<20 ||
		config.FetchMaxBytes > 100<<20 ||
		config.FetchMaxPartitionBytes < 1<<20 ||
		config.FetchMaxPartitionBytes > config.FetchMaxBytes ||
		config.FetchMaxWait < time.Millisecond ||
		config.FetchMaxWait > 30*time.Second ||
		config.SessionTimeout < time.Second ||
		config.SessionTimeout > 6*time.Minute ||
		config.RebalanceTimeout < time.Second ||
		config.RebalanceTimeout > 10*time.Minute ||
		config.HeartbeatInterval < 100*time.Millisecond ||
		config.HeartbeatInterval >= config.SessionTimeout ||
		config.HandlerTimeout < time.Second ||
		config.HandlerTimeout > 30*time.Minute ||
		config.CommitTimeout < 100*time.Millisecond ||
		config.CommitTimeout > 2*time.Minute ||
		config.DialTimeout < 100*time.Millisecond ||
		config.DialTimeout > 2*time.Minute {
		return ConsumerConfig{}, ErrInvalidConsumerConfig
	}

	return config, nil
}

func validOptionalConsumerIdentity(value string) bool {
	return value == "" ||
		(value == strings.TrimSpace(value) && validKafkaText(value, 255))
}

// RunOnce polls at most the configured record limit and processes records in
// fetch order. Each partition stops at its first handler failure; successful
// contiguous prefixes from that partition and independent partitions are
// committed before the first handler error is returned.
func (consumer *Consumer) RunOnce(ctx context.Context, handler Handler) (PollResult, error) {
	if handler == nil {
		return PollResult{}, ErrHandlerRequired
	}

	fetches := consumer.client.PollRecords(ctx, consumer.maxPollRecords)
	defer consumer.client.AllowRebalance()

	records := fetches.Records()
	result := PollResult{Polled: len(records)}
	if err := fetches.Err(); err != nil {
		return PollResult{}, err
	}
	if len(records) == 0 {
		return PollResult{}, nil
	}

	type partitionKey struct {
		topic     string
		partition int32
	}
	type partitionProgress struct {
		lastSuccessful *kgo.Record
		failed         bool
	}
	progress := make(map[partitionKey]*partitionProgress)
	partitionOrder := make([]partitionKey, 0)
	var handlerErr error
	for _, record := range records {
		key := partitionKey{topic: record.Topic, partition: record.Partition}
		state, exists := progress[key]
		if !exists {
			state = &partitionProgress{}
			progress[key] = state
			partitionOrder = append(partitionOrder, key)
		}
		if state.failed {
			continue
		}
		handlerCtx, cancel := context.WithTimeout(ctx, consumer.handlerTimeout)
		err := callHandler(handlerCtx, handler, consumedMessage(record))
		cancel()
		if err != nil {
			state.failed = true
			if handlerErr == nil {
				handlerErr = err
			}

			continue
		}
		state.lastSuccessful = record
		result.Processed++
	}

	committable := make([]*kgo.Record, 0, len(partitionOrder))
	for _, key := range partitionOrder {
		if record := progress[key].lastSuccessful; record != nil {
			committable = append(committable, record)
		}
	}
	if len(committable) == 0 {
		return result, handlerErr
	}
	commitCtx, cancel := context.WithTimeout(ctx, consumer.commitTimeout)
	err := consumer.client.CommitRecords(commitCtx, committable...)
	cancel()
	if err != nil {
		if handlerErr == nil {
			return result, err
		}

		return result, errors.Join(handlerErr, err)
	}
	result.Committed = result.Processed

	return result, handlerErr
}

// Run continuously executes bounded poll cycles until cancellation or the
// first processing failure. Context cancellation is a clean shutdown.
func (consumer *Consumer) Run(ctx context.Context, handler Handler) error {
	if handler == nil {
		return ErrHandlerRequired
	}

	for ctx.Err() == nil {
		if _, err := consumer.RunOnce(ctx, handler); err != nil {
			if ctx.Err() != nil {
				return nil
			}

			return err
		}
	}

	return nil
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

// Close leaves the consumer group and closes the underlying Kafka client.
func (consumer *Consumer) Close() {
	consumer.client.Close()
}
