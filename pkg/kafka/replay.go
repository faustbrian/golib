package kafka

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

var (
	ErrReplayRangesRequired   = errors.New("kafka: at least one replay range is required")
	ErrTooManyReplayRanges    = errors.New("kafka: replay range count exceeds configured limit")
	ErrInvalidReplayRange     = errors.New("kafka: replay range is invalid")
	ErrDuplicateReplayRange   = errors.New("kafka: replay range is duplicated")
	ErrInvalidReplayConfig    = errors.New("kafka: replay configuration is outside bounded limits")
	ErrUnexpectedReplayRecord = errors.New(
		"kafka: replay returned a record outside the requested ranges",
	)
	ErrReplayOffsetGap = errors.New("kafka: replay range contains an offset gap")
)

// ReplayRange is one inclusive start and exclusive end partition range.
type ReplayRange struct {
	Topic       string
	Partition   int32
	StartOffset int64
	EndOffset   int64
}

// ReplayConfig defines a bounded direct-partition reader. Replay readers do not
// join consumer groups or mutate group offsets.
type ReplayConfig struct {
	Brokers  []string
	ClientID string
	Ranges   []ReplayRange
	Security ClientSecurity

	MaxPollRecords int
	FetchMaxBytes  int32
	FetchMaxWait   time.Duration
	HandlerTimeout time.Duration
	DialTimeout    time.Duration
}

// ReplayResult summarizes one completed replay.
type ReplayResult struct {
	Polled          int
	Processed       int
	CompletedRanges int
}

type replayBackend interface {
	PollRecords(context.Context, int) kgo.Fetches
	Close()
}

// ReplayReader processes exact offset ranges without changing consumer-group
// state.
type ReplayReader struct {
	client         replayBackend
	ranges         []ReplayRange
	maxPollRecords int
	handlerTimeout time.Duration
	mu             sync.Mutex
}

// NewReplayReader constructs a direct-partition replay reader.
func NewReplayReader(config ReplayConfig) (*ReplayReader, error) {
	return newReplayReader(config, kgo.NewClient)
}

func newReplayReader(
	config ReplayConfig,
	factory consumerClientFactory,
) (*ReplayReader, error) {
	config, err := normalizeReplayConfig(config)
	if err != nil {
		return nil, err
	}

	partitions := make(map[string]map[int32]kgo.Offset)
	for _, replayRange := range config.Ranges {
		if partitions[replayRange.Topic] == nil {
			partitions[replayRange.Topic] = make(map[int32]kgo.Offset)
		}
		partitions[replayRange.Topic][replayRange.Partition] =
			kgo.NewOffset().At(replayRange.StartOffset)
	}
	options := []kgo.Opt{
		kgo.SeedBrokers(config.Brokers...),
		kgo.ClientID(config.ClientID),
		kgo.ConsumePartitions(partitions),
		kgo.FetchMaxBytes(config.FetchMaxBytes),
		kgo.FetchMaxWait(config.FetchMaxWait),
		kgo.DialTimeout(config.DialTimeout),
	}
	options = append(options, clientSecurityOptions(config.Security)...)

	client, err := factory(options...)
	if err != nil {
		return nil, err
	}

	return &ReplayReader{
		client:         client,
		ranges:         append([]ReplayRange(nil), config.Ranges...),
		maxPollRecords: config.MaxPollRecords,
		handlerTimeout: config.HandlerTimeout,
	}, nil
}

func normalizeReplayConfig(config ReplayConfig) (ReplayConfig, error) {
	if err := validateClientIdentity(config.Brokers, config.ClientID); err != nil {
		return ReplayConfig{}, err
	}
	security, err := normalizeClientSecurity(config.Security)
	if err != nil {
		return ReplayConfig{}, err
	}
	config.Security = security
	if len(config.Ranges) == 0 {
		return ReplayConfig{}, ErrReplayRangesRequired
	}
	if len(config.Ranges) > 1_024 {
		return ReplayConfig{}, ErrTooManyReplayRanges
	}
	seen := make(map[replayPartition]struct{}, len(config.Ranges))
	for _, replayRange := range config.Ranges {
		if !validKafkaTopicName(replayRange.Topic, 249) ||
			replayRange.Partition < 0 ||
			replayRange.StartOffset < 0 ||
			replayRange.EndOffset <= replayRange.StartOffset {
			return ReplayConfig{}, ErrInvalidReplayRange
		}
		key := replayPartition{topic: replayRange.Topic, partition: replayRange.Partition}
		if _, exists := seen[key]; exists {
			return ReplayConfig{}, ErrDuplicateReplayRange
		}
		seen[key] = struct{}{}
	}
	if config.MaxPollRecords == 0 {
		config.MaxPollRecords = 100
	}
	if config.FetchMaxBytes == 0 {
		config.FetchMaxBytes = 50 << 20
	}
	if config.FetchMaxWait == 0 {
		config.FetchMaxWait = 500 * time.Millisecond
	}
	if config.HandlerTimeout == 0 {
		config.HandlerTimeout = 30 * time.Second
	}
	if config.DialTimeout == 0 {
		config.DialTimeout = 10 * time.Second
	}
	if config.MaxPollRecords < 1 ||
		config.MaxPollRecords > 1_000 ||
		config.FetchMaxBytes < 1<<20 ||
		config.FetchMaxBytes > 100<<20 ||
		config.FetchMaxWait < time.Millisecond ||
		config.FetchMaxWait > 30*time.Second ||
		config.HandlerTimeout < time.Second ||
		config.HandlerTimeout > 30*time.Minute ||
		config.DialTimeout < 100*time.Millisecond ||
		config.DialTimeout > 2*time.Minute {
		return ReplayConfig{}, ErrInvalidReplayConfig
	}

	return config, nil
}

type replayPartition struct {
	topic     string
	partition int32
}

type replayProgress struct {
	next int64
	end  int64
	done bool
}

// Replay processes every requested offset exactly once in ascending fetch order.
// Missing offsets fail closed; the caller must explicitly approve a new range.
func (reader *ReplayReader) Replay(
	ctx context.Context,
	handler Handler,
) (ReplayResult, error) {
	if handler == nil {
		return ReplayResult{}, ErrHandlerRequired
	}

	reader.mu.Lock()
	defer reader.mu.Unlock()

	progress := make(map[replayPartition]*replayProgress, len(reader.ranges))
	for _, replayRange := range reader.ranges {
		progress[replayPartition{
			topic: replayRange.Topic, partition: replayRange.Partition,
		}] = &replayProgress{next: replayRange.StartOffset, end: replayRange.EndOffset}
	}
	result := ReplayResult{}
	for result.CompletedRanges < len(progress) {
		fetches := reader.client.PollRecords(ctx, reader.maxPollRecords)
		if err := fetches.Err(); err != nil {
			return result, err
		}
		for _, record := range fetches.Records() {
			result.Polled++
			state, exists := progress[replayPartition{
				topic: record.Topic, partition: record.Partition,
			}]
			if !exists {
				return result, ErrUnexpectedReplayRecord
			}
			if state.done {
				continue
			}
			if record.Offset < state.next || record.Offset >= state.end {
				return result, ErrReplayOffsetGap
			}
			if record.Offset != state.next {
				return result, ErrReplayOffsetGap
			}

			handlerCtx, cancel := context.WithTimeout(ctx, reader.handlerTimeout)
			err := callHandler(handlerCtx, handler, consumedMessage(record))
			cancel()
			if err != nil {
				return result, err
			}
			result.Processed++
			state.next++
			if state.next == state.end {
				state.done = true
				result.CompletedRanges++
			}
		}
	}

	return result, nil
}

// Close closes the direct Kafka client.
func (reader *ReplayReader) Close() {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	reader.client.Close()
}
