package breaker

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/faustbrian/golib/pkg/circuit-breaker/window"
)

type rollingWindow interface {
	add(time.Time, window.Record)
	snapshot(time.Time) window.Snapshot
}

type countRollingWindow struct{ value *window.Count }

func (w countRollingWindow) add(_ time.Time, record window.Record) {
	_ = w.value.Add(record)
}

func (w countRollingWindow) snapshot(_ time.Time) window.Snapshot {
	return w.value.Snapshot()
}

type timeRollingWindow struct{ value *window.Time }

func (w timeRollingWindow) add(at time.Time, record window.Record) {
	_ = w.value.Add(at, record)
}

func (w timeRollingWindow) snapshot(at time.Time) window.Snapshot {
	return w.value.Snapshot(at)
}

// Breaker protects calls to one caller-defined dependency boundary.
type Breaker struct {
	mu     sync.Mutex
	config normalizedConfig
	window rollingWindow

	state               State
	mode                Mode
	generation          uint64
	transitionCount     uint64
	lastTransition      time.Time
	consecutiveFailures uint64
	openCount           uint64
	currentOpenDuration time.Duration
	nextProbeAt         time.Time
	halfOpenActive      int
	halfOpenCompleted   int
	halfOpenSuccesses   int
	admitted            uint64
	rejected            uint64
	completed           uint64
	totalSuccesses      uint64
	totalFailures       uint64
	totalIgnored        uint64
	halfOpenPermits     map[*Permit]struct{}
	changed             chan struct{}
	pendingEvents       []TransitionEvent
	eventChannel        chan TransitionEvent
	eventStop           chan struct{}
	eventDone           chan struct{}
	eventCloseOnce      sync.Once
	eventClosed         atomic.Bool
	eventMu             sync.Mutex
	observerCounters    observerCounters
}

// New validates config and constructs a breaker in a fresh closed generation.
func New(config Config) (*Breaker, error) {
	normalized, err := normalizeConfig(config)
	if err != nil {
		return nil, err
	}
	rolling := newRollingWindow(normalized)

	b := &Breaker{
		config:          normalized,
		window:          rolling,
		state:           StateClosed,
		mode:            ModeNormal,
		generation:      1,
		halfOpenPermits: make(map[*Permit]struct{}),
		changed:         make(chan struct{}),
	}
	b.startObserver()
	return b, nil
}

func newRollingWindow(config normalizedConfig) rollingWindow {
	if config.timeWindow != nil {
		value, _ := window.NewTime(config.timeWindow.BucketDuration, config.timeWindow.BucketCount)
		return timeRollingWindow{value: value}
	}
	value, _ := window.NewCount(config.countWindowSize)
	return countRollingWindow{value: value}
}

// Permit represents one admitted execution bound to a state generation.
type Permit struct {
	breaker    *Breaker
	generation uint64
	state      State
	deadline   time.Time
	recording  bool
	execution  bool
	status     permitStatus
}

type permitStatus uint8

const (
	permitActive permitStatus = iota
	permitCompleted
	permitCanceled
	permitExpired
)

// Acquire requests permission to execute caller-owned work.
func (b *Breaker) Acquire(ctx context.Context) (*Permit, error) {
	return b.acquire(ctx, false)
}

func (b *Breaker) acquire(ctx context.Context, execution bool) (*Permit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var timer Timer
	var timeout <-chan time.Time
	var waitDeadline time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		now := b.config.clock.Now()
		b.mu.Lock()
		if err := ctx.Err(); err != nil {
			b.mu.Unlock()
			return nil, err
		}
		if timer != nil && !now.Before(waitDeadline) {
			err := b.rejectLocked(ErrHalfOpenWaitTimeout)
			b.unlockAndDispatch()
			return nil, err
		}
		permit, err, wait := b.tryAcquireLocked(now)
		if !wait {
			if permit != nil {
				permit.execution = execution
			}
			events := b.takeEventsLocked()
			b.mu.Unlock()
			b.dispatch(events)
			return permit, err
		}
		changed := b.changed
		createTimer := timer == nil
		if timer == nil {
			waitDeadline = now.Add(b.config.halfOpenMaxWait)
		}
		events := b.takeEventsLocked()
		b.mu.Unlock()
		b.dispatch(events)
		if createTimer {
			timer = b.config.clock.NewTimer(b.config.halfOpenMaxWait)
			timeout = timer.C()
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			b.mu.Lock()
			err := b.rejectLocked(ErrHalfOpenWaitTimeout)
			b.unlockAndDispatch()
			return nil, err
		case <-changed:
		}
	}
}

func (b *Breaker) tryAcquireLocked(now time.Time) (*Permit, error, bool) {
	switch b.mode {
	case ModeForceOpen:
		return nil, b.rejectLocked(ErrForceOpen), false
	case ModeIsolated:
		return nil, b.rejectLocked(ErrIsolated), false
	case ModeDisabled:
		permit := &Permit{
			breaker:    b,
			generation: b.generation,
			state:      b.state,
			deadline:   now.Add(b.config.permitTTL),
			recording:  false,
		}
		b.admitted++
		return permit, nil, false
	}

	if b.state == StateOpen {
		if now.Before(b.nextProbeAt) {
			return nil, b.rejectLocked(ErrOpen), false
		}
		b.transitionLocked(StateHalfOpen, ReasonOpenIntervalElapsed, now)
	}
	if b.state == StateHalfOpen {
		b.expireHalfOpenLocked(now)
	}
	if b.state == StateHalfOpen &&
		b.halfOpenCompleted+b.halfOpenActive >= b.config.halfOpen.MaxProbes {
		if b.config.halfOpenMaxWait > 0 {
			return nil, nil, true
		}
		return nil, b.rejectLocked(ErrHalfOpenExhausted), false
	}

	permit := &Permit{
		breaker:    b,
		generation: b.generation,
		state:      b.state,
		deadline:   now.Add(b.config.permitTTL),
		recording:  true,
	}
	b.admitted++
	if b.state == StateHalfOpen {
		b.halfOpenActive++
		b.halfOpenPermits[permit] = struct{}{}
	}
	return permit, nil, false
}

func (b *Breaker) rejectLocked(cause error) error {
	b.rejected++
	return &RejectionError{
		Name:       b.config.name,
		State:      b.state,
		Mode:       b.mode,
		Generation: b.generation,
		RetryAt:    b.nextProbeAt,
		Cause:      cause,
	}
}

// Complete reports exactly one classified completion for an admitted permit.
func (p *Permit) Complete(outcome Outcome, slow bool) error {
	if outcome > OutcomeIgnored {
		return &InvalidOutcomeError{Outcome: outcome}
	}
	b := p.breaker
	now, clockPanic := safeClockNow(b.config.clock)
	jitterSample := b.jitterSample()
	if clockPanic != nil {
		_ = p.completeAt(outcome, slow, p.admissionTime(), jitterSample)
		panic(clockPanic)
	}
	return p.completeAt(outcome, slow, now, jitterSample)
}

func (p *Permit) completeAt(outcome Outcome, slow bool, now time.Time, jitterSample float64) error {
	if outcome > OutcomeIgnored {
		return &InvalidOutcomeError{Outcome: outcome}
	}
	b := p.breaker
	b.mu.Lock()
	defer b.unlockAndDispatch()
	switch p.status {
	case permitCompleted:
		return ErrPermitCompleted
	case permitCanceled:
		return ErrPermitCanceled
	case permitExpired:
		if p.execution {
			p.status = permitCompleted
			p.recordLifetimeLocked(outcome)
			return nil
		}
		return ErrPermitExpired
	}
	if !p.execution && !now.Before(p.deadline) {
		p.expireLocked()
		return ErrPermitExpired
	}
	p.status = permitCompleted
	p.recordLifetimeLocked(outcome)
	if p.state == StateHalfOpen && p.generation == b.generation && b.state == StateHalfOpen {
		delete(b.halfOpenPermits, p)
	}
	if !p.recording {
		return nil
	}
	if p.generation != b.generation || p.state != b.state {
		return nil
	}

	if b.state == StateHalfOpen {
		b.completeHalfOpenLocked(outcome, now, jitterSample)
		return nil
	}
	record := window.Record{Class: window.Class(outcome), Slow: slow && outcome != OutcomeIgnored}
	b.window.add(now, record)
	switch outcome {
	case OutcomeSuccess:
		b.consecutiveFailures = 0
	case OutcomeFailure:
		b.consecutiveFailures++
	case OutcomeIgnored:
		if b.config.opening.IgnoredBehavior == ResetConsecutiveFailures {
			b.consecutiveFailures = 0
		}
	}
	snapshot := b.window.snapshot(now)
	if openingDecision(b.config.opening, b.config.minimumThroughput, b.consecutiveFailures, snapshot) {
		b.openLocked(now, ReasonPolicyOpened, jitterSample)
	}
	return nil
}

func (p *Permit) recordLifetimeLocked(outcome Outcome) {
	b := p.breaker
	b.completed++
	switch outcome {
	case OutcomeSuccess:
		b.totalSuccesses++
	case OutcomeFailure:
		b.totalFailures++
	case OutcomeIgnored:
		b.totalIgnored++
	}
}

// Cancel abandons an admitted permit and releases any half-open capacity.
func (p *Permit) Cancel() error {
	b := p.breaker
	now, clockPanic := safeClockNow(b.config.clock)
	if clockPanic != nil {
		_ = p.cancelAt(p.admissionTime())
		panic(clockPanic)
	}
	return p.cancelAt(now)
}

func (p *Permit) admissionTime() time.Time {
	return p.deadline.Add(-p.breaker.config.permitTTL)
}

func (p *Permit) cancelAt(now time.Time) error {
	b := p.breaker
	b.mu.Lock()
	defer b.unlockAndDispatch()
	switch p.status {
	case permitCompleted:
		return ErrPermitCompleted
	case permitCanceled:
		return ErrPermitCanceled
	case permitExpired:
		return ErrPermitExpired
	}
	if !now.Before(p.deadline) {
		p.expireLocked()
		return ErrPermitExpired
	}
	p.status = permitCanceled
	p.releaseHalfOpenLocked()
	return nil
}

func (b *Breaker) jitterSample() float64 {
	if b.config.openDurationJitter == 0 {
		return 0
	}
	return safeRandomSample(b.config.random)
}

func (p *Permit) expireLocked() {
	if p.status != permitActive {
		return
	}
	p.status = permitExpired
	p.releaseHalfOpenLocked()
}

func (p *Permit) releaseHalfOpenLocked() {
	b := p.breaker
	if p.state != StateHalfOpen || p.generation != b.generation || b.state != StateHalfOpen {
		return
	}
	if _, exists := b.halfOpenPermits[p]; !exists {
		return
	}
	delete(b.halfOpenPermits, p)
	b.halfOpenActive--
	b.signalLocked()
}

func (b *Breaker) expireHalfOpenLocked(now time.Time) {
	for permit := range b.halfOpenPermits {
		if !now.Before(permit.deadline) {
			permit.expireLocked()
		}
	}
}

func (b *Breaker) completeHalfOpenLocked(outcome Outcome, now time.Time, jitterSample float64) {
	b.halfOpenActive--
	b.signalLocked()
	if outcome == OutcomeIgnored {
		return
	}
	b.halfOpenCompleted++
	if outcome == OutcomeSuccess {
		b.halfOpenSuccesses++
	} else if b.config.halfOpen.FailureAction == ReopenImmediately {
		b.openLocked(now, ReasonHalfOpenFailed, jitterSample)
		return
	}

	policy := b.config.halfOpen
	if policy.RequiredSuccesses > 0 && b.halfOpenSuccesses >= policy.RequiredSuccesses {
		b.closeLocked(now, ReasonHalfOpenRecovered)
		return
	}
	if policy.SuccessRatio > 0 && b.halfOpenCompleted >= policy.MaxProbes {
		if ratio(uint64(b.halfOpenSuccesses), uint64(b.halfOpenCompleted)) >= policy.SuccessRatio {
			b.closeLocked(now, ReasonHalfOpenRecovered)
		} else {
			b.openLocked(now, ReasonHalfOpenFailed, jitterSample)
		}
		return
	}
	if policy.FailureAction == ReopenAfterSample && b.halfOpenCompleted >= policy.MaxProbes {
		b.openLocked(now, ReasonHalfOpenFailed, jitterSample)
	}
}

func (b *Breaker) openLocked(now time.Time, reason TransitionReason, jitterSample float64) {
	b.transitionLocked(StateOpen, reason, now, func() {
		b.openCount++
		b.currentOpenDuration = b.openDurationLocked(jitterSample)
		b.nextProbeAt = now.Add(b.currentOpenDuration)
	})
}

func (b *Breaker) closeLocked(now time.Time, reason TransitionReason) {
	b.transitionLocked(StateClosed, reason, now, func() {
		b.window = newRollingWindow(b.config)
		b.consecutiveFailures = 0
		b.openCount = 0
		b.currentOpenDuration = 0
		b.nextProbeAt = time.Time{}
	})
}

func (b *Breaker) transitionLocked(
	state State,
	reason TransitionReason,
	now time.Time,
	mutate ...func(),
) {
	before := b.snapshotLocked(now)
	for _, mutation := range mutate {
		mutation()
	}
	b.state = state
	b.generation++
	b.transitionCount++
	b.lastTransition = now
	b.halfOpenActive = 0
	b.halfOpenCompleted = 0
	b.halfOpenSuccesses = 0
	b.halfOpenPermits = make(map[*Permit]struct{})
	b.signalLocked()
	b.recordEventLocked(before, reason, now)
}

func (b *Breaker) signalLocked() {
	close(b.changed)
	b.changed = make(chan struct{})
}

func (b *Breaker) recordEventLocked(before Snapshot, reason TransitionReason, now time.Time) {
	if b.config.observer.observer == nil {
		return
	}
	after := b.snapshotLocked(now)
	b.pendingEvents = append(b.pendingEvents, TransitionEvent{
		Before:     before,
		After:      after,
		Reason:     reason,
		Generation: after.Generation,
		Timestamp:  now,
	})
}

// SetMode applies an explicit administrative mode without changing policy state.
func (b *Breaker) SetMode(mode Mode) error {
	if mode > ModeIsolated {
		return fmt.Errorf("breaker: unknown administrative mode %d", mode)
	}
	now := b.config.clock.Now()
	b.mu.Lock()
	defer b.unlockAndDispatch()
	if b.mode == mode {
		return nil
	}
	before := b.snapshotLocked(now)
	b.mode = mode
	b.generation++
	b.transitionCount++
	b.lastTransition = now
	b.halfOpenActive = 0
	b.halfOpenCompleted = 0
	b.halfOpenSuccesses = 0
	b.halfOpenPermits = make(map[*Permit]struct{})
	b.signalLocked()
	b.recordEventLocked(before, modeReason(mode), now)
	return nil
}

func modeReason(mode Mode) TransitionReason {
	switch mode {
	case ModeForceOpen:
		return ReasonForceOpen
	case ModeDisabled:
		return ReasonDisabled
	case ModeIsolated:
		return ReasonIsolated
	case ModeNormal:
		return ReasonReleased
	default:
		panic("breaker: unreachable mode")
	}
}

// ForceOpen rejects work independently of the policy-driven state.
func (b *Breaker) ForceOpen() error { return b.SetMode(ModeForceOpen) }

// Disable admits work without recording outcomes.
func (b *Breaker) Disable() error { return b.SetMode(ModeDisabled) }

// Isolate rejects work for operator-controlled maintenance.
func (b *Breaker) Isolate() error { return b.SetMode(ModeIsolated) }

// Release returns administrative control to normal policy-driven operation.
func (b *Breaker) Release() error { return b.SetMode(ModeNormal) }

// Reset returns to a new normal closed generation with an empty window.
func (b *Breaker) Reset() error {
	now := b.config.clock.Now()
	b.mu.Lock()
	defer b.unlockAndDispatch()
	b.transitionLocked(StateClosed, ReasonReset, now, func() {
		b.window = newRollingWindow(b.config)
		b.mode = ModeNormal
		b.consecutiveFailures = 0
		b.openCount = 0
		b.currentOpenDuration = 0
		b.nextProbeAt = time.Time{}
	})
	return nil
}

func (b *Breaker) openDurationLocked(jitterSample float64) time.Duration {
	var duration time.Duration
	switch policy := b.config.openDuration.(type) {
	case FixedOpenDuration:
		duration = time.Duration(policy)
	case ExponentialOpenDuration:
		duration = policy.Initial
		if policy.Multiplier == 1 {
			break
		}
		for attempt := uint64(1); attempt < b.openCount; attempt++ {
			next := time.Duration(float64(duration) * policy.Multiplier)
			if next <= duration || next >= policy.Maximum {
				duration = policy.Maximum
				break
			}
			duration = next
		}
	default:
		panic("breaker: unreachable open duration policy")
	}
	return b.jitterDuration(duration, jitterSample)
}

func (b *Breaker) jitterDuration(duration time.Duration, sample float64) time.Duration {
	if b.config.openDurationJitter == 0 {
		return duration
	}
	jittered := time.Duration(float64(duration) * (1 - b.config.openDurationJitter*sample))
	if jittered <= 0 {
		return 1
	}
	return jittered
}

func safeRandomSample(random Random) (sample float64) {
	defer func() {
		if recover() != nil {
			sample = 0
		}
	}()
	sample = random.Float64()
	if !finite(sample) || sample < 0 || sample >= 1 {
		return 0
	}
	return sample
}

// Snapshot is an immutable, internally consistent view of breaker state.
type Snapshot struct {
	Name                string
	State               State
	Mode                Mode
	Generation          uint64
	TransitionCount     uint64
	LastTransition      time.Time
	WindowClassified    uint64
	WindowSize          uint64
	WindowCapacity      int
	MinimumThroughput   int
	Successes           uint64
	Failures            uint64
	Ignored             uint64
	SlowSuccesses       uint64
	SlowFailures        uint64
	Admitted            uint64
	Rejected            uint64
	Completed           uint64
	TotalSuccesses      uint64
	TotalFailures       uint64
	TotalIgnored        uint64
	ActiveHalfOpen      int
	HalfOpenCompleted   int
	HalfOpenSuccesses   int
	FailureRatio        float64
	FailureRatioDefined bool
	SlowRatio           float64
	SlowRatioDefined    bool
	CurrentOpenDuration time.Duration
	NextProbeAt         time.Time
	ObserverFailures    uint64
	DroppedEvents       uint64
}

// Snapshot returns the breaker's current state.
func (b *Breaker) Snapshot() Snapshot {
	now := b.config.clock.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == StateHalfOpen {
		b.expireHalfOpenLocked(now)
	}
	return b.snapshotLocked(now)
}

func (b *Breaker) snapshotLocked(now time.Time) Snapshot {
	aggregate := b.window.snapshot(now)
	slow := aggregate.SlowSuccess + aggregate.SlowFailure
	windowCapacity := b.config.countWindowSize
	if b.config.timeWindow != nil {
		windowCapacity = b.config.timeWindow.BucketCount
	}
	ratioDefined := aggregate.Classified > 0
	return Snapshot{
		Name:                b.config.name,
		State:               b.state,
		Mode:                b.mode,
		Generation:          b.generation,
		TransitionCount:     b.transitionCount,
		LastTransition:      b.lastTransition,
		WindowClassified:    aggregate.Classified,
		WindowSize:          aggregate.Classified,
		WindowCapacity:      windowCapacity,
		MinimumThroughput:   b.config.minimumThroughput,
		Successes:           aggregate.Successes,
		Failures:            aggregate.Failures,
		Ignored:             aggregate.Ignored,
		SlowSuccesses:       aggregate.SlowSuccess,
		SlowFailures:        aggregate.SlowFailure,
		Admitted:            b.admitted,
		Rejected:            b.rejected,
		Completed:           b.completed,
		TotalSuccesses:      b.totalSuccesses,
		TotalFailures:       b.totalFailures,
		TotalIgnored:        b.totalIgnored,
		ActiveHalfOpen:      b.halfOpenActive,
		HalfOpenCompleted:   b.halfOpenCompleted,
		HalfOpenSuccesses:   b.halfOpenSuccesses,
		FailureRatio:        ratio(aggregate.Failures, aggregate.Classified),
		FailureRatioDefined: ratioDefined,
		SlowRatio:           ratio(slow, aggregate.Classified),
		SlowRatioDefined:    ratioDefined,
		CurrentOpenDuration: b.currentOpenDuration,
		NextProbeAt:         b.nextProbeAt,
		ObserverFailures:    b.observerCounters.failures.Load(),
		DroppedEvents:       b.observerCounters.dropped.Load(),
	}
}
