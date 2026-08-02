package concurrencylimit

import (
	"context"
	"errors"
	"math"
	"slices"
	"sync"
	"time"
)

// Limiter admits work up to an adaptive process-local concurrency limit.
type Limiter struct {
	config normalizedConfig

	mu          sync.Mutex
	algorithmMu sync.Mutex
	limit       int
	inFlight    int
	queue       []*waiter
	permits     map[uint64]*permitState
	nextID      uint64
	generation  uint64
	draining    bool

	recent              []time.Duration
	recentIndex         int
	windowStart         time.Time
	windowMax           int
	windowOutcomes      OutcomeCounts
	totalSamples        uint64
	baseline            time.Duration
	previousThroughput  float64
	previousMaxInFlight int
	algorithmState      AlgorithmState
	adapting            bool
	outcomes            OutcomeCounts
	rejections          uint64
	queueTimeouts       uint64
	expiredPermits      uint64
	algorithmErrors     uint64
	clockErrors         uint64

	observerPanics   uint64
	classifierPanics uint64
}

type waiter struct {
	metadata Metadata
	result   chan acquireResult
}

type acquireResult struct {
	permit *Permit
	err    error
}

type permitState struct {
	id         uint64
	generation uint64
	started    time.Time
	metadata   Metadata
}

// New validates config and constructs an independent process-local limiter.
func New(config Config) (*Limiter, error) {
	normalized, err := normalizeConfig(config)
	if err != nil {
		return nil, err
	}
	limiter := &Limiter{
		config: normalized, limit: normalized.initialLimit,
		permits: make(map[uint64]*permitState), generation: 1,
	}
	limiter.algorithmMu.Lock()
	resetOK := safeAlgorithmReset(normalized.algorithm, normalized.initialLimit)
	state, stateOK := safeAlgorithmState(normalized.algorithm)
	limiter.algorithmMu.Unlock()
	if !resetOK || !stateOK || !validAlgorithmState(state) {
		return nil, invalidConfig("Algorithm", "panicked or returned invalid initial state")
	}
	limiter.algorithmState = state
	return limiter, nil
}

// Acquire admits immediately or waits in the bounded FIFO queue. At most one
// Metadata value may be supplied.
func (limiter *Limiter) Acquire(ctx context.Context, optional ...Metadata) (*Permit, error) {
	if ctx == nil {
		return nil, context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	metadata, err := limiter.metadata(optional)
	if err != nil {
		return nil, err
	}
	now, ok := safeNow(limiter.config.clock)
	if !ok {
		limiter.incrementClockError()
		return nil, ErrClock
	}

	limiter.mu.Lock()
	events := limiter.reapExpiredLocked(now)
	before := limiter.snapshotLocked()
	var permit *Permit
	switch {
	case limiter.draining:
		err = ErrDraining
	case limiter.inFlight < limiter.limit:
		permit, err = limiter.newPermitLocked(now, metadata)
		if err == nil {
			events = append(events, Event{Type: EventAdmitted, Metadata: metadata, Before: before, After: limiter.snapshotLocked(), Timestamp: now})
		} else {
			saturatingIncrement(&limiter.rejections)
		}
	case limiter.config.queue.MaxQueued == 0:
		saturatingIncrement(&limiter.rejections)
		err = ErrLimitExceeded
	case len(limiter.queue) >= limiter.config.queue.MaxQueued:
		saturatingIncrement(&limiter.rejections)
		err = ErrQueueFull
	default:
		wait := &waiter{metadata: metadata, result: make(chan acquireResult, 1)}
		limiter.queue = append(limiter.queue, wait)
		limiter.mu.Unlock()
		limiter.dispatch(events)
		return limiter.wait(ctx, wait)
	}
	if err != nil {
		events = append(events, Event{Type: EventRejected, Metadata: metadata, Before: before, After: limiter.snapshotLocked(), Timestamp: now})
	}
	limiter.mu.Unlock()
	limiter.dispatch(events)
	return permit, err
}

func (limiter *Limiter) wait(ctx context.Context, wait *waiter) (*Permit, error) {
	timer, ok := safeTimer(limiter.config.clock, limiter.config.queue.MaxWait)
	if !ok {
		limiter.incrementClockError()
		return limiter.cancelWaiter(wait, ErrClock)
	}
	defer timer.Stop()
	select {
	case result := <-wait.result:
		return result.permit, result.err
	case <-ctx.Done():
		return limiter.cancelWaiter(wait, ctx.Err())
	case <-timer.C():
		return limiter.cancelWaiter(wait, ErrQueueTimeout)
	}
}

func (limiter *Limiter) cancelWaiter(wait *waiter, cause error) (*Permit, error) {
	now, ok := safeNow(limiter.config.clock)
	if !ok {
		now = time.Time{}
		limiter.incrementClockError()
	}
	limiter.mu.Lock()
	index := slices.Index(limiter.queue, wait)
	if index >= 0 {
		limiter.queue = slices.Delete(limiter.queue, index, index+1)
		saturatingIncrement(&limiter.rejections)
		if errors.Is(cause, ErrQueueTimeout) {
			saturatingIncrement(&limiter.queueTimeouts)
		}
		before := limiter.snapshotLocked()
		after := before
		limiter.mu.Unlock()
		limiter.dispatch([]Event{{Type: EventRejected, Metadata: wait.metadata, Before: before, After: after, Timestamp: now}})
		return nil, cause
	}
	limiter.mu.Unlock()
	select {
	case result := <-wait.result:
		if result.permit != nil {
			_ = result.permit.Complete(OutcomeIgnored)
		}
	default:
		return nil, cause
	}
	return nil, cause
}

// Snapshot returns a value-only bounded copy of current state.
func (limiter *Limiter) Snapshot() Snapshot {
	limiter.mu.Lock()
	snapshot := limiter.snapshotLocked()
	limiter.mu.Unlock()
	return snapshot
}

func (limiter *Limiter) snapshotLocked() Snapshot {
	return Snapshot{
		Limit: limiter.limit, InFlight: limiter.inFlight, Queued: len(limiter.queue),
		Samples: limiter.totalSamples, RecentSamples: len(limiter.recent), Baseline: limiter.baseline,
		Rejections: limiter.rejections, QueueTimeouts: limiter.queueTimeouts,
		ExpiredPermits: limiter.expiredPermits, ObserverPanics: limiter.observerPanics,
		ClassifierPanics: limiter.classifierPanics, AlgorithmErrors: limiter.algorithmErrors,
		ClockErrors: limiter.clockErrors, Generation: limiter.generation, Draining: limiter.draining,
		Outcomes: limiter.outcomes, Algorithm: limiter.algorithmState,
	}
}

// Reset invalidates all active and queued permits, clears learned state, and
// returns to the configured initial policy for a new pod lifecycle generation.
func (limiter *Limiter) Reset() {
	limiter.algorithmMu.Lock()
	resetOK := safeAlgorithmReset(limiter.config.algorithm, limiter.config.initialLimit)
	state, ok := safeAlgorithmState(limiter.config.algorithm)
	limiter.mu.Lock()
	before := limiter.snapshotLocked()
	saturatingIncrement(&limiter.generation)
	limiter.limit = limiter.config.initialLimit
	limiter.inFlight = 0
	limiter.permits = make(map[uint64]*permitState)
	queued := limiter.queue
	limiter.queue = nil
	limiter.recent = nil
	limiter.recentIndex = 0
	limiter.windowStart = time.Time{}
	limiter.windowMax = 0
	limiter.windowOutcomes = OutcomeCounts{}
	limiter.adapting = false
	limiter.totalSamples = 0
	limiter.baseline = 0
	limiter.previousThroughput = 0
	limiter.previousMaxInFlight = 0
	limiter.outcomes = OutcomeCounts{}
	limiter.rejections = 0
	limiter.queueTimeouts = 0
	limiter.expiredPermits = 0
	limiter.draining = false
	if resetOK && ok && validAlgorithmState(state) {
		limiter.algorithmState = state
	} else {
		saturatingIncrement(&limiter.algorithmErrors)
	}
	after := limiter.snapshotLocked()
	limiter.mu.Unlock()
	limiter.algorithmMu.Unlock()
	for _, wait := range queued {
		wait.result <- acquireResult{err: ErrReset}
	}
	limiter.dispatch([]Event{{Type: EventReset, Before: before, After: after}})
}

// BeginDrain rejects new admission and releases queued callers. Active permits
// remain owned by callers and should complete as OutcomeIgnored when shutdown
// cancellation is local.
func (limiter *Limiter) BeginDrain() {
	limiter.mu.Lock()
	before := limiter.snapshotLocked()
	limiter.draining = true
	queued := limiter.queue
	limiter.queue = nil
	after := limiter.snapshotLocked()
	limiter.mu.Unlock()
	for _, wait := range queued {
		wait.result <- acquireResult{err: ErrDraining}
	}
	limiter.dispatch([]Event{{Type: EventDrainStarted, Before: before, After: after}})
}

// ReapExpired invalidates permits older than PermitTTL and admits queued work.
// It provides explicit bounded abandoned-permit handling without background goroutines.
func (limiter *Limiter) ReapExpired() int {
	now, ok := safeNow(limiter.config.clock)
	if !ok {
		limiter.incrementClockError()
		return 0
	}
	limiter.mu.Lock()
	before := limiter.expiredPermits
	events := limiter.reapExpiredLocked(now)
	reaped := int(limiter.expiredPermits - before)
	limiter.mu.Unlock()
	limiter.dispatch(events)
	return reaped
}

func (limiter *Limiter) metadata(optional []Metadata) (Metadata, error) {
	if len(optional) > 1 {
		return Metadata{}, ErrInvalidMetadata
	}
	metadata := Metadata{}
	if len(optional) == 1 {
		metadata = optional[0]
	}
	if metadata.Priority < 0 || metadata.Priority > limiter.config.maxPriority {
		return Metadata{}, ErrInvalidMetadata
	}
	if metadata.Partition != "" {
		if len(metadata.Partition) > MaxPartitionBytes {
			return Metadata{}, ErrInvalidMetadata
		}
		if _, ok := limiter.config.partitions[metadata.Partition]; !ok {
			return Metadata{}, ErrInvalidMetadata
		}
	}
	return metadata, nil
}

func (limiter *Limiter) newPermitLocked(now time.Time, metadata Metadata) (*Permit, error) {
	if limiter.nextID == math.MaxUint64 {
		return nil, ErrIdentifierExhausted
	}
	limiter.nextID++
	state := &permitState{id: limiter.nextID, generation: limiter.generation, started: now, metadata: metadata}
	limiter.permits[state.id] = state
	limiter.inFlight++
	limiter.windowMax = max(limiter.windowMax, limiter.inFlight)
	return &Permit{limiter: limiter, id: state.id, generation: state.generation, metadata: metadata, started: now}, nil
}

func (limiter *Limiter) grantQueuedLocked(now time.Time) []Event {
	if limiter.draining {
		return nil
	}
	events := make([]Event, 0)
	available := min(limiter.limit-limiter.inFlight, len(limiter.queue))
	for range max(available, 0) {
		before := limiter.snapshotLocked()
		wait := limiter.queue[0]
		limiter.queue = limiter.queue[1:]
		permit, err := limiter.newPermitLocked(now, wait.metadata)
		eventType := EventAdmitted
		if err != nil {
			saturatingIncrement(&limiter.rejections)
			eventType = EventRejected
		}
		wait.result <- acquireResult{permit: permit, err: err}
		events = append(events, Event{Type: eventType, Metadata: wait.metadata, Before: before, After: limiter.snapshotLocked(), Timestamp: now})
	}
	return events
}

func (limiter *Limiter) reapExpiredLocked(now time.Time) []Event {
	events := make([]Event, 0)
	for id, permit := range limiter.permits {
		elapsed := now.Sub(permit.started)
		if elapsed >= limiter.config.permitTTL {
			before := limiter.snapshotLocked()
			delete(limiter.permits, id)
			limiter.inFlight--
			saturatingIncrement(&limiter.expiredPermits)
			saturatingIncrement(&limiter.outcomes.Ignored)
			after := limiter.snapshotLocked()
			events = append(events, Event{Type: EventCompleted, Outcome: OutcomeIgnored, Duration: max(elapsed, 0), Metadata: permit.metadata, Before: before, After: after, Timestamp: now})
		}
	}
	events = append(events, limiter.grantQueuedLocked(now)...)
	return events
}

func (limiter *Limiter) addSampleLocked(now time.Time, duration time.Duration, outcome Outcome) *Window {
	if !learns(outcome) {
		return nil
	}
	if limiter.windowStart.IsZero() {
		limiter.windowStart = now
	} else if now.Sub(limiter.windowStart) > limiter.config.sampling.MaxDuration && len(limiter.recent) < limiter.config.sampling.MinSamples {
		limiter.recent = limiter.recent[:0]
		limiter.recentIndex = 0
		limiter.windowStart = now
		limiter.windowMax = limiter.inFlight
		limiter.windowOutcomes = OutcomeCounts{}
	}
	if len(limiter.recent) < limiter.config.sampling.Capacity {
		limiter.recent = append(limiter.recent, duration)
	} else {
		limiter.recent[limiter.recentIndex] = duration
		limiter.recentIndex = (limiter.recentIndex + 1) % len(limiter.recent)
	}
	saturatingIncrement(&limiter.totalSamples)
	incrementOutcome(&limiter.windowOutcomes, outcome)
	elapsed := now.Sub(limiter.windowStart)
	if len(limiter.recent) < limiter.config.sampling.MinSamples || elapsed < limiter.config.sampling.MinDuration || limiter.adapting {
		return nil
	}
	values := append([]time.Duration(nil), limiter.recent...)
	slices.Sort(values)
	rank := int(math.Ceil(limiter.config.sampling.Quantile*float64(len(values)))) - 1
	recentLatency := values[max(rank, 0)]
	if limiter.baseline == 0 {
		limiter.baseline = recentLatency
	} else {
		delta := max(float64(recentLatency-limiter.baseline), 0)
		smoothed := limiter.baseline + time.Duration(delta*limiter.config.sampling.BaselineSmoothing)
		limiter.baseline = min(recentLatency, smoothed)
	}
	seconds := max(elapsed.Seconds(), float64(time.Nanosecond)/float64(time.Second))
	window := &Window{
		CurrentLimit: limiter.limit, Samples: len(values), MaxInFlight: limiter.windowMax,
		RecentLatency: recentLatency, BaselineLatency: limiter.baseline,
		Throughput:         float64(len(values)) / seconds,
		PreviousThroughput: limiter.previousThroughput, PreviousMaxInFlight: limiter.previousMaxInFlight,
		Overloads: limiter.windowOutcomes.Overload, DependencyFailures: limiter.windowOutcomes.DependencyFailure,
	}
	limiter.previousThroughput = window.Throughput
	limiter.previousMaxInFlight = window.MaxInFlight
	limiter.recent = limiter.recent[:0]
	limiter.recentIndex = 0
	limiter.windowStart = time.Time{}
	limiter.windowMax = limiter.inFlight
	limiter.windowOutcomes = OutcomeCounts{}
	limiter.adapting = true
	return window
}

func (limiter *Limiter) applyWindow(window Window, now time.Time) []Event {
	limiter.algorithmMu.Lock()
	decision, ok := safeAlgorithmUpdate(limiter.config.algorithm, window)
	state, stateOK := safeAlgorithmState(limiter.config.algorithm)
	limiter.mu.Lock()
	limiter.algorithmMu.Unlock()
	defer limiter.mu.Unlock()
	limiter.adapting = false
	if !ok {
		saturatingIncrement(&limiter.algorithmErrors)
		return nil
	}
	if !stateOK {
		saturatingIncrement(&limiter.algorithmErrors)
		return nil
	}
	if !validAlgorithmState(decision.State) {
		saturatingIncrement(&limiter.algorithmErrors)
		return nil
	}
	if !validAlgorithmState(state) {
		saturatingIncrement(&limiter.algorithmErrors)
		return nil
	}
	if decision.State != state {
		saturatingIncrement(&limiter.algorithmErrors)
		return nil
	}
	before := limiter.snapshotLocked()
	minimum := int(max(int64(limiter.config.minLimit), int64(limiter.limit)-int64(limiter.config.sampling.MaxDecrease)))
	maximum := int(min(int64(limiter.config.maxLimit), int64(limiter.limit)+int64(limiter.config.sampling.MaxIncrease)))
	limiter.limit = min(max(decision.Limit, minimum), maximum)
	limiter.algorithmState = decision.State
	limiter.algorithmState.Estimate = float64(limiter.limit)
	events := make([]Event, 0)
	if before.Limit != limiter.limit {
		events = append(events, Event{Type: EventLimitChanged, Before: before, After: limiter.snapshotLocked(), Timestamp: now})
	}
	events = append(events, limiter.grantQueuedLocked(now)...)
	return events
}

func (limiter *Limiter) dispatch(events []Event) {
	if limiter.config.observer == nil {
		return
	}
	for _, event := range events {
		func() {
			defer func() {
				if recover() != nil {
					limiter.incrementObserverPanic()
				}
			}()
			limiter.config.observer(event)
		}()
	}
}

func (limiter *Limiter) incrementClockError() {
	limiter.mu.Lock()
	saturatingIncrement(&limiter.clockErrors)
	limiter.mu.Unlock()
}

func (limiter *Limiter) incrementObserverPanic() {
	limiter.mu.Lock()
	saturatingIncrement(&limiter.observerPanics)
	limiter.mu.Unlock()
}

func (limiter *Limiter) incrementClassifierPanic() {
	limiter.mu.Lock()
	saturatingIncrement(&limiter.classifierPanics)
	limiter.mu.Unlock()
}

func safeAlgorithmReset(algorithm Algorithm, limit int) (ok bool) {
	defer func() { ok = recover() == nil }()
	algorithm.Reset(limit)
	return true
}

func safeAlgorithmUpdate(algorithm Algorithm, window Window) (decision Decision, ok bool) {
	defer func() { ok = recover() == nil }()
	return algorithm.Update(window), true
}

func safeAlgorithmState(algorithm Algorithm) (state AlgorithmState, ok bool) {
	defer func() { ok = recover() == nil }()
	return algorithm.State(), true
}

func validAlgorithmState(state AlgorithmState) bool {
	return state.Name != "" && len(state.Name) <= maxAlgorithmNameBytes && len(state.Reason) <= maxAlgorithmStateBytes &&
		finite(state.Estimate) && finite(state.Gradient) && finite(state.QueueEstimate) && finite(state.Throughput)
}

func learns(outcome Outcome) bool {
	return outcome == OutcomeSuccess || outcome == OutcomeDependencyFailure || outcome == OutcomeOverload
}

func incrementOutcome(counts *OutcomeCounts, outcome Outcome) {
	switch outcome {
	case OutcomeSuccess:
		saturatingIncrement(&counts.Success)
	case OutcomeDependencyFailure:
		saturatingIncrement(&counts.DependencyFailure)
	case OutcomeLocalDrop:
		saturatingIncrement(&counts.LocalDrop)
	case OutcomeIgnored:
		saturatingIncrement(&counts.Ignored)
	case OutcomeOverload:
		saturatingIncrement(&counts.Overload)
	}
}

func saturatingIncrement(value *uint64) {
	if *value != math.MaxUint64 {
		*value++
	}
}
