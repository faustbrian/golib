package kafka

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

var (
	ErrReplayRangesRequired    = errors.New("kafka: at least one replay range is required")
	ErrTooManyReplayRanges     = errors.New("kafka: replay range count exceeds configured limit")
	ErrInvalidReplayRange      = errors.New("kafka: replay range is invalid")
	ErrDuplicateReplayRange    = errors.New("kafka: replay range is duplicated")
	ErrInvalidReplayCheckpoint = errors.New(
		"kafka: replay checkpoint is invalid",
	)
	ErrDuplicateReplayCheckpoint = errors.New(
		"kafka: replay checkpoint position is duplicated",
	)
	ErrInvalidReplayConfig     = errors.New("kafka: replay configuration is outside bounded limits")
	ErrReplaySideEffectsDenied = errors.New(
		"kafka: replay side effects require explicit opt-in",
	)
	ErrReplayBusy               = errors.New("kafka: replay reader is already running")
	ErrReplayAlreadyRun         = errors.New("kafka: replay reader has already run")
	ErrReplayClosing            = errors.New("kafka: replay reader is shutting down")
	ErrReplayClosed             = errors.New("kafka: replay reader is closed")
	ErrReplayShutdownActive     = errors.New("kafka: replay shutdown is already active")
	ErrReplayShutdownIncomplete = errors.New("kafka: replay shutdown is incomplete")
	ErrUnexpectedReplayRecord   = errors.New(
		"kafka: replay returned a record outside the requested ranges",
	)
	ErrReplayOffsetGap        = errors.New("kafka: replay range contains an offset gap")
	ErrReplayOffsetOutOfRange = errors.New(
		"kafka: replay offset is outside broker retention bounds",
	)
	ErrReplayBoundsUnavailable = errors.New(
		"kafka: replay broker offset bounds are unavailable",
	)
	ErrReplayStalled = errors.New(
		"kafka: replay made no progress before its bounded deadline",
	)
)

// ReplayRange is one inclusive start and exclusive end partition range.
type ReplayRange struct {
	Topic       string
	Partition   int32
	StartOffset int64
	EndOffset   int64
}

// ReplayPosition is the next offset an external replay checkpoint requests for
// one configured topic partition.
type ReplayPosition struct {
	Topic      string
	Partition  int32
	NextOffset int64
}

// ReplayCheckpoint is an externally persisted set of next offsets. Positions
// may omit configured ranges, which then start at their inclusive start.
type ReplayCheckpoint struct {
	Positions []ReplayPosition
}

// Retain returns a checkpoint with an independently owned position slice.
func (checkpoint ReplayCheckpoint) Retain() ReplayCheckpoint {
	return ReplayCheckpoint{
		Positions: append([]ReplayPosition(nil), checkpoint.Positions...),
	}
}

// ReplaySideEffectPolicy controls whether Replay may invoke an application
// handler. The zero value fails closed so dry-run planning cannot accidentally
// execute side effects.
type ReplaySideEffectPolicy uint8

const (
	// ReplaySideEffectsDenied permits planning but rejects Replay.
	ReplaySideEffectsDenied ReplaySideEffectPolicy = iota
	// ReplaySideEffectsAllowed explicitly permits handler invocation.
	ReplaySideEffectsAllowed
)

// ReplayConfig defines a bounded direct-partition reader. Replay readers do not
// join consumer groups or mutate group offsets.
type ReplayConfig struct {
	Brokers     []string
	ClientID    string
	Protocol    ProtocolPolicy
	Ranges      []ReplayRange
	Checkpoint  ReplayCheckpoint
	SideEffects ReplaySideEffectPolicy
	Limits      MessageLimits
	Security    ClientSecurity
	// Observers receive payload-free plan, record, run, shutdown, and broker
	// events. Replay partition workers and broker goroutines can invoke the
	// copied callbacks concurrently.
	Observers ObserverPolicy

	MaxPollRecords int
	// MaxConcurrentFetches bounds broker fetch requests.
	MaxConcurrentFetches int
	// MaxConcurrentHandlers bounds simultaneous callbacks across independent
	// partitions. Records within one partition always remain sequential.
	MaxConcurrentHandlers  int
	FetchMaxBytes          int32
	FetchMaxPartitionBytes int32
	FetchMaxWait           time.Duration
	PlanningTimeout        time.Duration
	ProgressTimeout        time.Duration
	HandlerTimeout         time.Duration
	ShutdownTimeout        time.Duration
	DialTimeout            time.Duration
}

// Validate reports whether the replay policy is explicit, compatible, and
// bounded without constructing a Kafka client or retaining caller-owned data.
func (config ReplayConfig) Validate() error {
	_, err := normalizeReplayConfig(config)

	return err
}

// ReplayPlannedRange is one immutable dry-run range after applying the
// external checkpoint.
type ReplayPlannedRange struct {
	ReplayRange
	NextOffset int64
	Remaining  int64
}

// ReplayPlan is an owned dry-run plan. Plan produces it without broker
// validation; PlanAgainstBroker returns it only after validating broker bounds.
type ReplayPlan struct {
	Ranges         []ReplayPlannedRange
	TotalRemaining int64
}

// ReplayRangeResult reports exact progress for one configured range.
type ReplayRangeResult struct {
	ReplayRange
	NextOffset int64
	Processed  int64
	Skipped    int64
	Failed     int64
	Complete   bool
}

// ReplayResult summarizes completed and resumable replay progress.
type ReplayResult struct {
	Polled           int64
	Processed        int64
	Skipped          int64
	Failed           int64
	CompletedRanges  int
	IncompleteRanges int
	Ranges           []ReplayRangeResult
}

// Checkpoint returns an independently owned checkpoint for resuming every
// configured range after the last successfully processed offset.
func (result ReplayResult) Checkpoint() ReplayCheckpoint {
	positions := make([]ReplayPosition, len(result.Ranges))
	for index, replayRange := range result.Ranges {
		positions[index] = ReplayPosition{
			Topic: replayRange.Topic, Partition: replayRange.Partition,
			NextOffset: replayRange.NextOffset,
		}
	}

	return ReplayCheckpoint{Positions: positions}
}

type replayBackend interface {
	PollRecords(context.Context, int) kgo.Fetches
	PauseFetchPartitions(map[string][]int32) map[string][]int32
	Close()
}

type replayBoundsBackend interface {
	ListStartOffsets(context.Context, ...string) (kadm.ListedOffsets, error)
	ListEndOffsets(context.Context, ...string) (kadm.ListedOffsets, error)
}

func listReplayBounds(
	ctx context.Context,
	backend replayBoundsBackend,
	ranges []ReplayRange,
) (map[replayPartition][2]int64, error) {
	topics := make([]string, 0, len(ranges))
	seen := make(map[string]struct{}, len(ranges))
	for _, replayRange := range ranges {
		if _, exists := seen[replayRange.Topic]; exists {
			continue
		}
		seen[replayRange.Topic] = struct{}{}
		topics = append(topics, replayRange.Topic)
	}
	starts, err := backend.ListStartOffsets(ctx, topics...)
	if err != nil {
		return nil, err
	}
	ends, err := backend.ListEndOffsets(ctx, topics...)
	if err != nil {
		return nil, err
	}

	bounds := make(map[replayPartition][2]int64, len(ranges))
	for _, replayRange := range ranges {
		start, startExists := starts.Lookup(replayRange.Topic, replayRange.Partition)
		end, endExists := ends.Lookup(replayRange.Topic, replayRange.Partition)
		if !startExists || !endExists {
			return nil, fmt.Errorf(
				"%w: topic %q partition %d",
				ErrReplayBoundsUnavailable,
				replayRange.Topic,
				replayRange.Partition,
			)
		}
		if start.Err != nil {
			return nil, errors.Join(ErrReplayBoundsUnavailable, start.Err)
		}
		if end.Err != nil {
			return nil, errors.Join(ErrReplayBoundsUnavailable, end.Err)
		}
		bounds[replayPartition{
			topic: replayRange.Topic, partition: replayRange.Partition,
		}] = [2]int64{start.Offset, end.Offset}
	}

	return bounds, nil
}

// ReplayReader processes exact offset ranges without changing consumer-group
// state.
type ReplayReader struct {
	client                replayBackend
	bounds                replayBoundsBackend
	clientID              string
	ranges                []ReplayRange
	checkpoint            ReplayCheckpoint
	sideEffects           ReplaySideEffectPolicy
	limits                MessageLimits
	maxPollRecords        int
	maxConcurrentHandlers int
	planningTimeout       time.Duration
	progressTimeout       time.Duration
	handlerTimeout        time.Duration
	shutdownTimeout       time.Duration
	now                   func() time.Time
	observers             observerDispatcher

	mu                sync.Mutex
	running           bool
	used              bool
	runDone           chan struct{}
	closing           bool
	closed            bool
	shutdownActive    bool
	observerCallbacks int
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

	nextOffsets := replayNextOffsets(config.Ranges, config.Checkpoint)
	partitions := make(map[string]map[int32]kgo.Offset)
	for _, replayRange := range config.Ranges {
		if partitions[replayRange.Topic] == nil {
			partitions[replayRange.Topic] = make(map[int32]kgo.Offset)
		}
		partitions[replayRange.Topic][replayRange.Partition] =
			kgo.NoResetOffset().At(nextOffsets[replayPartition{
				topic: replayRange.Topic, partition: replayRange.Partition,
			}])
	}
	options := []kgo.Opt{
		kgo.SeedBrokers(config.Brokers...),
		kgo.ClientID(config.ClientID),
		kgo.ConsumePartitions(partitions),
		kgo.MaxConcurrentFetches(config.MaxConcurrentFetches),
		kgo.FetchMaxBytes(config.FetchMaxBytes),
		kgo.FetchMaxPartitionBytes(config.FetchMaxPartitionBytes),
		kgo.FetchMaxWait(config.FetchMaxWait),
		kgo.DialTimeout(config.DialTimeout),
	}
	options = append(options, clientProtocolOptions(config.Protocol)...)
	options = append(options, clientSecurityOptions(config.Security)...)
	dispatcher := newObserverDispatcher(config.Observers)
	reader := &ReplayReader{
		clientID:              strings.Clone(config.ClientID),
		ranges:                append([]ReplayRange(nil), config.Ranges...),
		checkpoint:            config.Checkpoint.Retain(),
		sideEffects:           config.SideEffects,
		limits:                config.Limits,
		maxPollRecords:        config.MaxPollRecords,
		maxConcurrentHandlers: config.MaxConcurrentHandlers,
		planningTimeout:       config.PlanningTimeout,
		progressTimeout:       config.ProgressTimeout,
		handlerTimeout:        config.HandlerTimeout,
		shutdownTimeout:       config.ShutdownTimeout,
		now:                   time.Now,
		observers:             dispatcher,
	}
	if dispatcher.enabled() {
		observerHook := newFranzObserverHook(config.ClientID, "", dispatcher)
		observerHook.before = reader.beginObservation
		observerHook.after = reader.finishObservation
		options = append(options, kgo.WithHooks(observerHook))
	}

	client, err := factory(options...)
	if err != nil {
		return nil, err
	}
	reader.client = client
	reader.bounds = kadm.NewClient(client)

	return reader, nil
}

func normalizeReplayConfig(config ReplayConfig) (ReplayConfig, error) {
	if err := validateClientIdentity(config.Brokers, config.ClientID); err != nil {
		return ReplayConfig{}, err
	}
	if err := config.Protocol.Validate(); err != nil {
		return ReplayConfig{}, err
	}
	security, err := normalizeClientSecurity(config.Security)
	if err != nil {
		return ReplayConfig{}, err
	}
	config.Security = security
	observers, err := normalizeObserverPolicy(config.Observers)
	if err != nil {
		return ReplayConfig{}, err
	}
	config.Observers = observers
	if config.SideEffects > ReplaySideEffectsAllowed {
		return ReplayConfig{}, ErrInvalidReplayConfig
	}
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
	config.Ranges = append([]ReplayRange(nil), config.Ranges...)
	checkpoint, err := normalizeReplayCheckpoint(config.Ranges, config.Checkpoint)
	if err != nil {
		return ReplayConfig{}, err
	}
	config.Checkpoint = checkpoint
	if replayRemainingOverflows(config.Ranges, config.Checkpoint) {
		return ReplayConfig{}, ErrInvalidReplayConfig
	}
	if config.Limits == (MessageLimits{}) {
		config.Limits = DefaultMessageLimits()
	}
	if err := config.Limits.Validate(); err != nil {
		return ReplayConfig{}, ErrInvalidReplayConfig
	}
	for _, replayRange := range config.Ranges {
		if !validKafkaTopicName(replayRange.Topic, config.Limits.MaxTopicBytes) {
			return ReplayConfig{}, ErrInvalidReplayRange
		}
	}
	if config.MaxPollRecords == 0 {
		config.MaxPollRecords = 100
	}
	if config.MaxConcurrentFetches == 0 {
		config.MaxConcurrentFetches = 1
	}
	if config.MaxConcurrentHandlers == 0 {
		config.MaxConcurrentHandlers = 1
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
	if config.PlanningTimeout == 0 {
		config.PlanningTimeout = 10 * time.Second
	}
	if config.ProgressTimeout == 0 {
		config.ProgressTimeout = 30 * time.Second
	}
	if config.HandlerTimeout == 0 {
		config.HandlerTimeout = 30 * time.Second
	}
	if config.ShutdownTimeout == 0 {
		config.ShutdownTimeout = 30 * time.Second
	}
	if config.DialTimeout == 0 {
		config.DialTimeout = 10 * time.Second
	}
	if config.MaxPollRecords < 1 ||
		config.MaxPollRecords > 1_000 ||
		config.MaxConcurrentFetches < 1 ||
		config.MaxConcurrentFetches > 64 ||
		config.MaxConcurrentHandlers < 1 ||
		config.MaxConcurrentHandlers > 64 ||
		config.FetchMaxBytes < 1<<20 ||
		config.FetchMaxBytes > 100<<20 ||
		config.FetchMaxPartitionBytes < 1<<20 ||
		config.FetchMaxPartitionBytes > config.FetchMaxBytes ||
		config.FetchMaxWait < time.Millisecond ||
		config.FetchMaxWait > 30*time.Second ||
		config.PlanningTimeout < 100*time.Millisecond ||
		config.PlanningTimeout > 2*time.Minute ||
		config.ProgressTimeout < 100*time.Millisecond ||
		config.ProgressTimeout > 30*time.Minute ||
		config.ProgressTimeout < config.FetchMaxWait ||
		config.HandlerTimeout < time.Second ||
		config.HandlerTimeout > 30*time.Minute ||
		config.ShutdownTimeout < time.Second ||
		config.ShutdownTimeout > 10*time.Minute ||
		config.DialTimeout < 100*time.Millisecond ||
		config.DialTimeout > 2*time.Minute {
		return ReplayConfig{}, ErrInvalidReplayConfig
	}

	return config, nil
}

func normalizeReplayCheckpoint(
	ranges []ReplayRange,
	checkpoint ReplayCheckpoint,
) (ReplayCheckpoint, error) {
	rangeByPartition := make(map[replayPartition]ReplayRange, len(ranges))
	for _, replayRange := range ranges {
		rangeByPartition[replayPartition{
			topic: replayRange.Topic, partition: replayRange.Partition,
		}] = replayRange
	}
	positions := append([]ReplayPosition(nil), checkpoint.Positions...)
	seen := make(map[replayPartition]struct{}, len(positions))
	for _, position := range positions {
		key := replayPartition{topic: position.Topic, partition: position.Partition}
		replayRange, exists := rangeByPartition[key]
		if !exists ||
			position.NextOffset < replayRange.StartOffset ||
			position.NextOffset > replayRange.EndOffset {
			return ReplayCheckpoint{}, ErrInvalidReplayCheckpoint
		}
		if _, duplicate := seen[key]; duplicate {
			return ReplayCheckpoint{}, ErrDuplicateReplayCheckpoint
		}
		seen[key] = struct{}{}
	}

	return ReplayCheckpoint{Positions: positions}, nil
}

func replayNextOffsets(
	ranges []ReplayRange,
	checkpoint ReplayCheckpoint,
) map[replayPartition]int64 {
	next := make(map[replayPartition]int64, len(ranges))
	for _, replayRange := range ranges {
		next[replayPartition{
			topic: replayRange.Topic, partition: replayRange.Partition,
		}] = replayRange.StartOffset
	}
	for _, position := range checkpoint.Positions {
		next[replayPartition{
			topic: position.Topic, partition: position.Partition,
		}] = position.NextOffset
	}

	return next
}

func replayRemainingOverflows(
	ranges []ReplayRange,
	checkpoint ReplayCheckpoint,
) bool {
	next := replayNextOffsets(ranges, checkpoint)
	var total int64
	for _, replayRange := range ranges {
		remaining := replayRange.EndOffset - next[replayPartition{
			topic: replayRange.Topic, partition: replayRange.Partition,
		}]
		if remaining > math.MaxInt64-total {
			return true
		}
		total += remaining
	}

	return false
}

type replayPartition struct {
	topic     string
	partition int32
}

// Plan returns an owned dry-run plan after applying the external checkpoint.
// It performs no broker request and cannot prove current retention bounds.
func (reader *ReplayReader) Plan() ReplayPlan {
	next := replayNextOffsets(reader.ranges, reader.checkpoint)
	plan := ReplayPlan{Ranges: make([]ReplayPlannedRange, len(reader.ranges))}
	for index, replayRange := range reader.ranges {
		nextOffset := next[replayPartition{
			topic: replayRange.Topic, partition: replayRange.Partition,
		}]
		remaining := replayRange.EndOffset - nextOffset
		plan.Ranges[index] = ReplayPlannedRange{
			ReplayRange: replayRange,
			NextOffset:  nextOffset,
			Remaining:   remaining,
		}
		plan.TotalRemaining += remaining
	}

	return plan
}

// PlanAgainstBroker returns an owned dry-run plan after confirming that every
// effective start remains retained and every exclusive end is at or before the
// current broker high watermark. It does not poll records, invoke handlers,
// mutate group offsets, or consume the reader's single execution. Any error
// returns a zero plan so an unvalidated local plan cannot be mistaken for a
// broker-validated result.
func (reader *ReplayReader) PlanAgainstBroker(
	ctx context.Context,
) (plan ReplayPlan, resultErr error) {
	if ctx == nil {
		return ReplayPlan{}, ErrContextRequired
	}
	if isObserverContext(ctx) {
		return ReplayPlan{}, ErrObserverReentry
	}
	if cause := context.Cause(ctx); cause != nil {
		return ReplayPlan{}, cause
	}
	if err := reader.beginPlan(); err != nil {
		return ReplayPlan{}, err
	}
	defer reader.endRun()

	var startedAt time.Time
	if reader.observers.enabled() {
		startedAt = time.Now()
		defer func() {
			reader.observeReplayPlan(ctx, startedAt, plan, resultErr)
		}()
	}
	plan = reader.Plan()
	progress, _ := reader.initialReplayResult()
	if err := reader.validateReplayBounds(ctx, progress.Ranges); err != nil {
		return ReplayPlan{}, err
	}

	return plan, nil
}

// Replay performs one execution of every requested retained offset in
// partition order. A reader is single-use even after failure.
// Missing offsets fail closed; the caller must explicitly approve side effects
// and persist the returned checkpoint outside this package before resuming.
func (reader *ReplayReader) Replay(
	ctx context.Context,
	handler Handler,
) (result ReplayResult, resultErr error) {
	if ctx == nil {
		return ReplayResult{}, ErrContextRequired
	}
	if isObserverContext(ctx) {
		return ReplayResult{}, ErrObserverReentry
	}
	if handler == nil {
		return ReplayResult{}, ErrHandlerRequired
	}
	if reader.sideEffects != ReplaySideEffectsAllowed {
		return ReplayResult{}, ErrReplaySideEffectsDenied
	}
	if cause := context.Cause(ctx); cause != nil {
		return ReplayResult{}, cause
	}
	if err := reader.beginRun(); err != nil {
		return ReplayResult{}, err
	}
	defer reader.endRun()
	var startedAt time.Time
	if reader.observers.enabled() {
		startedAt = time.Now()
		defer func() {
			reader.observeReplayRun(ctx, startedAt, result, resultErr)
		}()
	}

	result, indexes := reader.initialReplayResult()
	if err := reader.validateReplayBounds(ctx, result.Ranges); err != nil {
		return result, err
	}
	progressDeadlines := reader.initialReplayProgressDeadlines(result.Ranges)
	for result.IncompleteRanges > 0 {
		if replayProgressExpired(progressDeadlines, reader.now()) {
			return result, ErrReplayStalled
		}
		progressCtx, cancelProgress := context.WithDeadline(
			ctx,
			earliestReplayProgressDeadline(progressDeadlines),
		)
		fetches := reader.client.PollRecords(progressCtx, reader.maxPollRecords)
		if cause := context.Cause(progressCtx); cause != nil {
			cancelProgress()
			if replayCause := context.Cause(ctx); replayCause != nil {
				return result, replayCause
			}

			return result, errors.Join(ErrReplayStalled, cause)
		}
		cancelProgress()
		if err := fetches.Err(); err != nil {
			if errors.Is(err, kerr.OffsetOutOfRange) {
				return result, errors.Join(ErrReplayOffsetOutOfRange, err)
			}

			return result, err
		}
		records := fetches.Records()
		result.Polled += int64(len(records))
		if len(records) > reader.maxPollRecords {
			return result, ErrTooManyFetchedRecords
		}
		if reader.maxConcurrentHandlers <= 1 {
			if err := reader.processReplayRecordsSerial(
				ctx,
				handler,
				records,
				&result,
				indexes,
				progressDeadlines,
			); err != nil {
				return result, err
			}

			continue
		}
		if err := reader.processReplayRecordsParallel(
			ctx,
			handler,
			records,
			&result,
			indexes,
			progressDeadlines,
		); err != nil {
			return result, err
		}
	}

	return result, nil
}

func (reader *ReplayReader) observeReplayPlan(
	ctx context.Context,
	startedAt time.Time,
	plan ReplayPlan,
	err error,
) {
	observation := Observation{
		Kind:            ObservationReplayPlan,
		StartedAt:       startedAt,
		Duration:        time.Since(startedAt),
		ClientID:        reader.clientID,
		PartitionCount:  len(reader.ranges),
		ReplayRemaining: plan.TotalRemaining,
		Succeeded:       err == nil,
	}
	if err != nil {
		observation.Category = classifyError(err)
	}
	reader.dispatchObservation(ctx, observation)
}

func (reader *ReplayReader) observeReplayRun(
	ctx context.Context,
	startedAt time.Time,
	result ReplayResult,
	err error,
) {
	observation := Observation{
		Kind:            ObservationReplayRun,
		StartedAt:       startedAt,
		Duration:        time.Since(startedAt),
		ClientID:        reader.clientID,
		PartitionCount:  len(reader.ranges),
		ReplayProcessed: result.Processed,
		ReplaySkipped:   result.Skipped,
		ReplayFailed:    result.Failed,
		ReplayRemaining: replayResultRemaining(result),
		Succeeded:       err == nil,
	}
	if err != nil {
		observation.Category = classifyConsumerObservationError(err)
	}
	reader.dispatchObservation(ctx, observation)
}

func replayResultRemaining(result ReplayResult) int64 {
	var remaining int64
	for _, replayRange := range result.Ranges {
		remaining += replayRange.EndOffset - replayRange.NextOffset
	}

	return remaining
}

func (reader *ReplayReader) initialReplayProgressDeadlines(
	progress []ReplayRangeResult,
) map[replayPartition]time.Time {
	now := reader.now()
	deadlines := make(map[replayPartition]time.Time, len(progress))
	for _, replayRange := range progress {
		if !replayRange.Complete {
			deadlines[replayPartition{
				topic: replayRange.Topic, partition: replayRange.Partition,
			}] = now.Add(reader.progressTimeout)
		}
	}

	return deadlines
}

func replayProgressExpired(
	deadlines map[replayPartition]time.Time,
	now time.Time,
) bool {
	for _, deadline := range deadlines {
		if !now.Before(deadline) {
			return true
		}
	}

	return false
}

func earliestReplayProgressDeadline(
	deadlines map[replayPartition]time.Time,
) time.Time {
	var earliest time.Time
	for _, deadline := range deadlines {
		if earliest.IsZero() || deadline.Before(earliest) {
			earliest = deadline
		}
	}

	return earliest
}

func (reader *ReplayReader) validateReplayBounds(
	ctx context.Context,
	progress []ReplayRangeResult,
) error {
	ranges := make([]ReplayRange, 0, len(progress))
	for _, replayRange := range progress {
		if !replayRange.Complete {
			ranges = append(ranges, replayRange.ReplayRange)
		}
	}
	if len(ranges) == 0 {
		return nil
	}

	planningCtx, cancel := context.WithTimeout(ctx, reader.planningTimeout)
	defer cancel()
	bounds, err := listReplayBounds(planningCtx, reader.bounds, ranges)
	if err != nil {
		return errors.Join(ErrReplayBoundsUnavailable, err)
	}
	if cause := context.Cause(planningCtx); cause != nil {
		return errors.Join(ErrReplayBoundsUnavailable, cause)
	}
	for _, replayRange := range progress {
		if replayRange.Complete {
			continue
		}
		bound := bounds[replayPartition{
			topic: replayRange.Topic, partition: replayRange.Partition,
		}]
		if replayRange.NextOffset < bound[0] || replayRange.EndOffset > bound[1] {
			return ErrReplayOffsetOutOfRange
		}
	}

	return nil
}

func (reader *ReplayReader) initialReplayResult() (
	ReplayResult,
	map[replayPartition]int,
) {
	next := replayNextOffsets(reader.ranges, reader.checkpoint)
	result := ReplayResult{Ranges: make([]ReplayRangeResult, len(reader.ranges))}
	indexes := make(map[replayPartition]int, len(reader.ranges))
	for index, replayRange := range reader.ranges {
		nextOffset := next[replayPartition{
			topic: replayRange.Topic, partition: replayRange.Partition,
		}]
		complete := nextOffset == replayRange.EndOffset
		result.Ranges[index] = ReplayRangeResult{
			ReplayRange: replayRange,
			NextOffset:  nextOffset,
			Complete:    complete,
		}
		indexes[replayPartition{
			topic: replayRange.Topic, partition: replayRange.Partition,
		}] = index
		if complete {
			result.CompletedRanges++
		} else {
			result.IncompleteRanges++
		}
	}

	return result, indexes
}

func (reader *ReplayReader) beginPlan() error {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	if reader.observerCallbacks != 0 {
		return ErrObserverReentry
	}
	if reader.closed {
		return ErrReplayClosed
	}
	if reader.closing {
		return ErrReplayClosing
	}
	if reader.running {
		return ErrReplayBusy
	}
	reader.running = true
	reader.runDone = make(chan struct{})

	return nil
}

func (reader *ReplayReader) beginRun() error {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	if reader.observerCallbacks != 0 {
		return ErrObserverReentry
	}
	if reader.closed {
		return ErrReplayClosed
	}
	if reader.closing {
		return ErrReplayClosing
	}
	if reader.running {
		return ErrReplayBusy
	}
	if reader.used {
		return ErrReplayAlreadyRun
	}
	reader.running = true
	reader.used = true
	reader.runDone = make(chan struct{})

	return nil
}

func (reader *ReplayReader) endRun() {
	reader.mu.Lock()
	done := reader.runDone
	reader.runDone = nil
	reader.running = false
	reader.mu.Unlock()
	close(done)
}

// Shutdown fences new replay work, waits for the active replay, and closes the
// direct Kafka client. An incomplete shutdown remains fenced and can be
// retried. Concurrent shutdown calls fail with ErrReplayShutdownActive.
func (reader *ReplayReader) Shutdown(
	ctx context.Context,
) (resultErr error) {
	if ctx == nil {
		return ErrContextRequired
	}
	if isObserverContext(ctx) {
		return ErrObserverReentry
	}

	reader.mu.Lock()
	if reader.observerCallbacks != 0 {
		reader.mu.Unlock()

		return ErrObserverReentry
	}
	if reader.closed {
		reader.mu.Unlock()

		return nil
	}
	if reader.shutdownActive {
		reader.mu.Unlock()

		return ErrReplayShutdownActive
	}
	reader.shutdownActive = true
	reader.closing = true
	done := reader.runDone
	reader.mu.Unlock()
	var startedAt time.Time
	if reader.observers.enabled() {
		startedAt = time.Now()
		defer func() {
			reader.observeReplayShutdown(ctx, startedAt, resultErr)
		}()
	}

	complete := false
	defer func() {
		reader.mu.Lock()
		reader.shutdownActive = false
		if complete {
			reader.closed = true
		}
		reader.mu.Unlock()
	}()

	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return errors.Join(ErrReplayShutdownIncomplete, ctx.Err())
		}
	}
	if cause := context.Cause(ctx); cause != nil {
		return errors.Join(ErrReplayShutdownIncomplete, cause)
	}

	reader.client.Close()
	complete = true

	return nil
}

// Close performs bounded shutdown using the configured shutdown timeout.
func (reader *ReplayReader) Close() error {
	reader.mu.Lock()
	if reader.observerCallbacks != 0 {
		reader.mu.Unlock()

		return ErrObserverReentry
	}
	reader.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), reader.shutdownTimeout)
	defer cancel()

	return reader.Shutdown(ctx)
}

func (reader *ReplayReader) observeReplayShutdown(
	ctx context.Context,
	startedAt time.Time,
	err error,
) {
	observation := Observation{
		Kind:      ObservationReplayShutdown,
		StartedAt: startedAt,
		Duration:  time.Since(startedAt),
		ClientID:  reader.clientID,
		Succeeded: err == nil,
	}
	if err != nil {
		observation.Category = classifyError(err)
	}
	reader.dispatchObservation(ctx, observation)
}

func (reader *ReplayReader) dispatchObservation(
	ctx context.Context,
	observation Observation,
) {
	reader.beginObservation()
	defer reader.finishObservation()

	reader.observers.observe(ctx, observation)
}

func (reader *ReplayReader) beginObservation() {
	reader.mu.Lock()
	reader.observerCallbacks++
	reader.mu.Unlock()
}

func (reader *ReplayReader) finishObservation() {
	reader.mu.Lock()
	reader.observerCallbacks--
	reader.mu.Unlock()
}
