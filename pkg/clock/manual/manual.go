package manual

import (
	"container/heap"
	"context"
	"errors"
	"math"
	"sync"
	"time"

	clockpkg "github.com/faustbrian/golib/pkg/clock"
)

var (
	// ErrActiveLimit reports exhaustion of the configured active-object budget.
	ErrActiveLimit = errors.New("manual clock: active object limit exceeded")
	// ErrWorkLimit reports exhaustion of one advancement's work budget.
	ErrWorkLimit = errors.New("manual clock: advancement work limit exceeded")
	// ErrClosed reports an operation attempted after shutdown.
	ErrClosed = errors.New("manual clock: closed")
	// ErrBackwardAdvance reports an AdvanceTo target before the current wall time.
	ErrBackwardAdvance = errors.New("manual clock: backward advance")
	// ErrInvalidLimits reports a zero or negative resource budget.
	ErrInvalidLimits = errors.New("manual clock: invalid limits")
)

const (
	defaultMaxActive = 65_536
	defaultMaxWork   = 1_000_000
)

// Limits bounds scheduled objects, outstanding advancement waiters, and work
// processed by a Clock. MaxActive applies independently to scheduled objects
// and advancement waiters.
type Limits struct {
	MaxActive         int
	MaxWorkPerAdvance int
}

// Option configures a Clock during construction.
type Option func(*config) error

type config struct {
	limits Limits
}

// WithLimits replaces the default active-object and advancement budgets.
func WithLimits(limits Limits) Option {
	return func(config *config) error {
		if limits.MaxActive <= 0 || limits.MaxWorkPerAdvance <= 0 {
			return ErrInvalidLimits
		}
		config.limits = limits
		return nil
	}
}

// Clock is a concurrency-safe deterministic clock. Time changes only through
// Advance, AdvanceTo, or Jump. It starts no hidden goroutine.
type Clock struct {
	mu               sync.Mutex
	start            time.Time
	elapsed          time.Duration
	wallJump         time.Duration
	sequence         uint64
	events           eventHeap
	active           int
	limits           Limits
	closed           bool
	advancing        bool
	requests         []*advanceRequest
	notify           chan struct{}
	work             Result
	callbackSequence uint64
	advanceErr       error
}

// New constructs a clock at an explicit wall timestamp, including time.Time{}.
// It preserves the location and strips any process-local monotonic reading so
// Jump can model wall movement independently from elapsed progress.
func New(start time.Time, options ...Option) (*Clock, error) {
	configuration := config{limits: Limits{
		MaxActive:         defaultMaxActive,
		MaxWorkPerAdvance: defaultMaxWork,
	}}
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(&configuration); err != nil {
			return nil, err
		}
	}

	return &Clock{start: start.Round(0), limits: configuration.limits, notify: make(chan struct{}, 1)}, nil
}

// Now returns the current wall time. The starting location is preserved.
func (clock *Clock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.wallAt(clock.elapsed)
}

// Since returns wall-clock subtraction against Now. Use Mark and SinceMark for
// elapsed measurement that must remain correct across Jump.
func (clock *Clock) Since(start time.Time) time.Duration {
	return clock.Now().Sub(start)
}

// Mark captures the current monotonic progress as a time.Duration token.
func (clock *Clock) Mark() time.Duration {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.elapsed
}

// SinceMark returns monotonic progress since mark, independent of wall jumps.
func (clock *Clock) SinceMark(mark time.Duration) time.Duration {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.elapsed - mark
}

// Measure captures current monotonic progress and returns a concurrency-safe
// closure whose result is unaffected by Jump.
func (clock *Clock) Measure() func() time.Duration {
	mark := clock.Mark()
	return func() time.Duration { return clock.SinceMark(mark) }
}

// Jump changes wall time without changing monotonic progress or event deadlines.
func (clock *Clock) Jump(delta time.Duration) error {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	if clock.closed {
		return ErrClosed
	}
	jump, ok := addDuration(clock.wallJump, delta)
	if !ok {
		return clockpkg.ErrOverflow
	}
	clock.wallJump = jump
	return nil
}

// Advance moves monotonic time forward and synchronously processes every event
// due through the target. Callbacks run outside internal locks in deterministic
// timestamp and registration order.
func (clock *Clock) Advance(duration time.Duration) (*Waiter, error) {
	if duration < 0 {
		return nil, clockpkg.ErrInvalidDuration
	}

	clock.mu.Lock()
	if clock.closed {
		clock.mu.Unlock()
		return nil, ErrClosed
	}
	if len(clock.requests) >= clock.limits.MaxActive {
		clock.mu.Unlock()
		return nil, ErrActiveLimit
	}
	start := clock.elapsed
	target, ok := addDuration(start, duration)
	if !ok {
		clock.mu.Unlock()
		return nil, clockpkg.ErrOverflow
	}
	waiter := newWaiter(clock)
	request := &advanceRequest{
		startedAt: start, target: target, waiter: waiter,
		baseTriggered: clock.work.Triggered, baseCallbacks: clock.work.Callbacks,
		basePanics: clock.work.Panics, baseCallbackID: clock.callbackSequence,
	}
	if clock.advancing {
		if clock.advanceErr != nil {
			err := clock.advanceErr
			clock.mu.Unlock()
			return nil, err
		}
		clock.requests = append(clock.requests, request)
		clock.signalLocked()
		clock.mu.Unlock()
		return waiter, nil
	}
	clock.advancing = true
	request.baseTriggered = 0
	request.baseCallbacks = 0
	request.basePanics = 0
	request.baseCallbackID = clock.callbackSequence
	clock.requests = append(clock.requests, request)
	clock.work = Result{}
	clock.advanceErr = nil
	clock.mu.Unlock()

	err := clock.runAdvancement()
	return waiter, err
}

// AdvanceTo moves forward until target wall time. Backward wall movement must
// use Jump so it cannot accidentally reverse monotonic progress.
func (clock *Clock) AdvanceTo(target time.Time) (*Waiter, error) {
	now := clock.Now()
	if target.Before(now) {
		return nil, ErrBackwardAdvance
	}
	duration := target.Sub(now)
	if !now.Add(duration).Equal(target) {
		return nil, clockpkg.ErrOverflow
	}
	return clock.Advance(duration)
}

// Sleep blocks until manual advancement reaches the deadline or ctx is done.
func (clock *Clock) Sleep(ctx context.Context, duration time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if duration <= 0 {
		return nil
	}

	clock.mu.Lock()
	waiter := &sleepWaiter{done: make(chan error, 1)}
	if err := clock.activateLocked(&waiter.state, duration, waiter); err != nil {
		clock.mu.Unlock()
		return err
	}
	clock.mu.Unlock()

	select {
	case err := <-waiter.done:
		return err
	case <-ctx.Done():
		clock.mu.Lock()
		if waiter.state.active {
			waiter.state.active = false
			clock.removeScheduledLocked(&waiter.state)
			clock.active--
		}
		clock.mu.Unlock()
		return ctx.Err()
	}
}

// NewTimer creates an owned one-shot timer.
func (clock *Clock) NewTimer(duration time.Duration) (clockpkg.Timer, error) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	timer := &Timer{clock: clock, channel: make(chan time.Time, 1)}
	if err := clock.activateLocked(&timer.state, duration, timer); err != nil {
		return nil, err
	}
	return timer, nil
}

// NewTicker creates an owned ticker. Its one-element channel drops ticks while
// the receiver is backpressured, matching the standard library policy.
func (clock *Clock) NewTicker(duration time.Duration) (clockpkg.Ticker, error) {
	if duration <= 0 {
		return nil, clockpkg.ErrInvalidDuration
	}
	clock.mu.Lock()
	defer clock.mu.Unlock()
	ticker := &Ticker{clock: clock, channel: make(chan time.Time, 1), interval: duration}
	if err := clock.activateLocked(&ticker.state, duration, ticker); err != nil {
		return nil, err
	}
	return ticker, nil
}

// AfterFunc schedules an owned callback. A panic is recovered and counted in
// the advancement Result; panic payloads are never retained.
func (clock *Clock) AfterFunc(duration time.Duration, function func()) (clockpkg.Callback, error) {
	if function == nil {
		return nil, clockpkg.ErrInvalidCallback
	}
	clock.mu.Lock()
	defer clock.mu.Unlock()
	callback := &Callback{clock: clock, function: function}
	if err := clock.activateLocked(&callback.state, duration, callback); err != nil {
		return nil, err
	}
	return callback, nil
}

// Snapshot returns bounded diagnostic counters without callback payloads.
func (clock *Clock) Snapshot() Snapshot {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return Snapshot{Now: clock.wallAt(clock.elapsed), Elapsed: clock.elapsed, Active: clock.active, Closed: clock.closed}
}

// Shutdown idempotently releases every active object owned by the clock.
func (clock *Clock) Shutdown() error {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	if clock.closed {
		return nil
	}
	clock.closed = true
	if len(clock.requests) > 0 {
		clock.failRequestsLocked(ErrClosed)
	}
	for clock.events.Len() > 0 {
		event := heap.Pop(&clock.events).(*scheduledEvent) //nolint:forcetypeassert // The owned heap stores only scheduled events.
		event.state.event = nil
		switch owner := event.owner.(type) {
		case *Timer:
			owner.state.active = false
		case *Ticker:
			owner.state.active = false
		case *Callback:
			owner.state.active = false
		case *sleepWaiter:
			if owner.state.active {
				owner.state.active = false
				owner.done <- ErrClosed
			}
		}
	}
	clock.active = 0
	clock.signalLocked()
	return nil
}

// Result summarizes bounded work performed by one advancement.
type Result struct {
	StartedAt time.Duration
	EndedAt   time.Duration
	Triggered int
	Callbacks int
	Panics    int
}

// Waiter synchronizes completion of work triggered by an advancement.
type Waiter struct {
	clock   *Clock
	done    chan struct{}
	waiting bool
	result  Result
	err     error
}

// Wait returns the completed result or context cancellation.
func (waiter *Waiter) Wait(ctx context.Context) (Result, error) {
	select {
	case <-waiter.done:
		return waiter.result, waiter.err
	default:
	}
	waiter.clock.mu.Lock()
	waiter.waiting = true
	waiter.clock.signalLocked()
	waiter.clock.mu.Unlock()
	select {
	case <-waiter.done:
		return waiter.result, waiter.err
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

// Snapshot is a bounded view of manual-clock state.
type Snapshot struct {
	Now     time.Time
	Elapsed time.Duration
	Active  int
	Closed  bool
}

func newWaiter(clock *Clock) *Waiter {
	return &Waiter{clock: clock, done: make(chan struct{})}
}

func (clock *Clock) wallAt(elapsed time.Duration) time.Time {
	return clock.start.Add(elapsed).Add(clock.wallJump)
}

func addDuration(left, right time.Duration) (time.Duration, bool) {
	if right > 0 && left > time.Duration(math.MaxInt64)-right {
		return 0, false
	}
	if right < 0 && left < time.Duration(math.MinInt64)-right {
		return 0, false
	}
	return left + right, true
}

func callAndRecover(function func()) (panicked bool) {
	defer func() {
		if recover() != nil {
			panicked = true
		}
	}()
	function()
	return false
}

type advanceRequest struct {
	startedAt      time.Duration
	target         time.Duration
	waiter         *Waiter
	baseTriggered  int
	baseCallbacks  int
	basePanics     int
	baseCallbackID uint64
}

type callbackResult struct {
	id       uint64
	panicked bool
}

func (clock *Clock) runAdvancement() error {
	callbackDone := make(chan callbackResult)
	activeCallbacks := make(map[uint64]struct{})
	for {
		clock.mu.Lock()
		clock.completeEligibleLocked(activeCallbacks)
		if len(clock.requests) == 0 && len(activeCallbacks) == 0 {
			clock.advancing = false
			clock.mu.Unlock()
			return nil
		}

		maxTarget := clock.elapsed
		for _, request := range clock.requests {
			if request.target > maxTarget {
				maxTarget = request.target
			}
		}
		if len(activeCallbacks) > 0 {
			allowedTarget := clock.elapsed
			for _, request := range clock.requests {
				if request.waiter.waiting && request.target > allowedTarget {
					allowedTarget = request.target
				}
			}
			if maxTarget > allowedTarget {
				maxTarget = allowedTarget
			}
		}
		nextTarget, hasNextTarget := nextRequestTarget(clock.requests, clock.elapsed, maxTarget)
		event := clock.nextValidLocked()
		if event != nil && event.deadline <= maxTarget {
			if clock.work.Triggered >= clock.limits.MaxWorkPerAdvance {
				clock.advanceErr = ErrWorkLimit
				clock.failRequestsLocked(ErrWorkLimit)
				clock.mu.Unlock()
				clock.drainCallbacks(callbackDone, activeCallbacks)
				clock.mu.Lock()
				clock.advancing = false
				clock.mu.Unlock()
				return ErrWorkLimit
			}
			heap.Pop(&clock.events)
			clock.elapsed = event.deadline
			callback := clock.fireLocked(event)
			clock.work.Triggered++
			if callback == nil {
				clock.mu.Unlock()
				continue
			}
			clock.work.Callbacks++
			clock.callbackSequence++
			callbackID := clock.callbackSequence
			activeCallbacks[callbackID] = struct{}{}
			clock.mu.Unlock()
			go func() { callbackDone <- callbackResult{id: callbackID, panicked: callAndRecover(callback)} }()
			clock.waitForCallbackSignal(callbackDone, activeCallbacks)
			continue
		}
		if hasNextTarget {
			clock.elapsed = nextTarget
			clock.mu.Unlock()
			continue
		}
		clock.mu.Unlock()
		clock.waitForCallbackSignal(callbackDone, activeCallbacks)
	}
}

func nextRequestTarget(requests []*advanceRequest, elapsed, maxTarget time.Duration) (time.Duration, bool) {
	nextTarget := time.Duration(0)
	hasNextTarget := false
	for _, request := range requests {
		if request.target > elapsed && request.target <= maxTarget &&
			(!hasNextTarget || request.target < nextTarget) {
			nextTarget, hasNextTarget = request.target, true
		}
	}
	return nextTarget, hasNextTarget
}

func (clock *Clock) drainCallbacks(done <-chan callbackResult, active map[uint64]struct{}) {
	for len(active) > 0 {
		result := <-done
		clock.mu.Lock()
		delete(active, result.id)
		if result.panicked {
			clock.work.Panics++
		}
		clock.mu.Unlock()
	}
}

func (clock *Clock) waitForCallbackSignal(done <-chan callbackResult, active map[uint64]struct{}) {
	select {
	case result := <-done:
		clock.mu.Lock()
		delete(active, result.id)
		if result.panicked {
			clock.work.Panics++
		}
		clock.mu.Unlock()
	case <-clock.notify:
	}
}

func (clock *Clock) completeEligibleLocked(active map[uint64]struct{}) {
	event := clock.nextValidLocked()
	remaining := clock.requests[:0]
	for _, request := range clock.requests {
		blocked := request.target > clock.elapsed || (event != nil && event.deadline <= request.target)
		if !blocked {
			for callbackID := range active {
				if callbackID > request.baseCallbackID {
					blocked = true
					break
				}
			}
		}
		if blocked {
			remaining = append(remaining, request)
			continue
		}
		request.waiter.result = Result{
			StartedAt: request.startedAt, EndedAt: request.target,
			Triggered: clock.work.Triggered - request.baseTriggered,
			Callbacks: clock.work.Callbacks - request.baseCallbacks,
			Panics:    clock.work.Panics - request.basePanics,
		}
		close(request.waiter.done)
	}
	clock.requests = remaining
}

func (clock *Clock) failRequestsLocked(err error) {
	for _, request := range clock.requests {
		request.waiter.err = err
		request.waiter.result = Result{
			StartedAt: request.startedAt, EndedAt: clock.elapsed,
			Triggered: clock.work.Triggered - request.baseTriggered,
			Callbacks: clock.work.Callbacks - request.baseCallbacks,
			Panics:    clock.work.Panics - request.basePanics,
		}
		close(request.waiter.done)
	}
	clock.requests = nil
}

func (clock *Clock) signalLocked() {
	if !clock.advancing {
		return
	}
	select {
	case clock.notify <- struct{}{}:
	default:
	}
}
