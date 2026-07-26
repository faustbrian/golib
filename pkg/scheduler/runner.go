package scheduler

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"slices"
	"sync"
	"time"

	"github.com/faustbrian/golib/pkg/scheduler/lease"
)

var (
	// ErrInvalidRunner reports missing or incompatible runner dependencies.
	ErrInvalidRunner = errors.New("scheduler: invalid runner dependencies")
	// ErrTaskPanic reports a recovered condition or executor panic.
	ErrTaskPanic = errors.New("scheduler: task panicked")
	// ErrDraining reports a new tick attempted after drain began.
	ErrDraining = errors.New("scheduler: runner is draining")
	// ErrUnsupportedHeartbeat reports a store unsafe for overlap leases.
	ErrUnsupportedHeartbeat = errors.New("scheduler: lease heartbeat is unsupported")
	// ErrUnsupportedOverlap reports unavailable safe replacement semantics.
	ErrUnsupportedOverlap = errors.New("scheduler: overlap replacement is unsupported")
	// ErrExecutionCapacity reports that the bounded executor pool is full.
	ErrExecutionCapacity = errors.New("scheduler: execution capacity exhausted")
	// ErrCallbackTimeout reports a condition or lifecycle callback deadline.
	ErrCallbackTimeout = errors.New("scheduler: callback timed out")
	// ErrCallbackCapacity reports that the bounded callback pool is full.
	ErrCallbackCapacity = errors.New("scheduler: callback capacity exhausted")
)

// DefaultMaxConcurrentExecutions bounds managed in-process executions.
const DefaultMaxConcurrentExecutions = 128

// DefaultLeaseOperationTimeout bounds each distributed lease backend call.
const DefaultLeaseOperationTimeout = 5 * time.Second

// DefaultCallbackTimeout bounds conditions, hooks, and observers.
const DefaultCallbackTimeout = time.Second

// DefaultMaxConcurrentCallbacks bounds active and timed-out callbacks.
const DefaultMaxConcurrentCallbacks = 128

// MaxObservers bounds lifecycle observer registration per runner.
const MaxObservers = 128

// Result classifies the outcome of a schedule decision.
type Result uint8

const (
	// ResultSucceeded reports successful execution or dispatch.
	ResultSucceeded Result = iota
	// ResultFailed reports a failed decision or execution.
	ResultFailed
	// ResultSkipped reports an intentional non-execution decision.
	ResultSkipped
)

// EventType identifies a scheduler lifecycle boundary.
type EventType uint8

const (
	// EventBefore is emitted immediately before execution.
	EventBefore EventType = iota
	// EventSuccess is emitted after successful execution.
	EventSuccess
	// EventFailure is emitted after a failed decision or execution.
	EventFailure
	// EventSkipped is emitted for a policy-based skip.
	EventSkipped
	// EventOverlap is emitted when an active overlap lease prevents execution.
	EventOverlap
	// EventCompleted terminates every emitted lifecycle.
	EventCompleted
)

// Event carries a bounded, structured scheduler lifecycle record.
type Event struct {
	Type       EventType
	Result     Result
	Occurrence Occurrence
	Context    context.Context
	Owner      string
	Fencing    uint64
	At         time.Time
	Err        error
}

func (result Result) String() string {
	switch result {
	case ResultSucceeded:
		return "succeeded"
	case ResultFailed:
		return "failed"
	case ResultSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

func (eventType EventType) String() string {
	switch eventType {
	case EventBefore:
		return "before"
	case EventSuccess:
		return "success"
	case EventFailure:
		return "failure"
	case EventSkipped:
		return "skipped"
	case EventOverlap:
		return "overlap"
	case EventCompleted:
		return "completed"
	default:
		return "unknown"
	}
}

// Observer consumes scheduler lifecycle events.
type Observer interface {
	Observe(Event)
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(Event)

// Observe invokes the adapted observer function.
func (observe ObserverFunc) Observe(event Event) { observe(event) }

// Executor performs or dispatches one scheduled occurrence.
type Executor interface {
	Execute(context.Context, Context) error
}

// Clock provides current time and exact-boundary timers to a Runner.
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) After(duration time.Duration) <-chan time.Time { return time.After(duration) }

// RunnerOption configures a Runner during construction.
type RunnerOption func(*Runner) error

// Runner calculates due occurrences and coordinates their fenced execution.
type Runner struct {
	registry        *Registry
	leases          lease.Store
	executor        Executor
	owner           string
	environment     string
	maintenance     bool
	observers       []Observer
	clock           Clock
	maxExecutions   int
	executionSlots  chan struct{}
	leaseTimeout    time.Duration
	callbackTimeout time.Duration
	maxCallbacks    int
	callbackSlots   chan struct{}

	stateMu    sync.Mutex
	draining   bool
	inflight   sync.WaitGroup
	executions sync.WaitGroup
	callbacks  sync.WaitGroup
}

type heartbeatResult struct {
	owned lease.Lease
	err   error
}

type heartbeatMonitor struct {
	done   chan struct{}
	result heartbeatResult
}

type managedExecution struct {
	done      <-chan error
	heartbeat *heartbeatMonitor
}

type conditionResult struct {
	allowed bool
	err     error
}

// NewRunner validates dependencies and distributed safety capabilities.
func NewRunner(registry *Registry, leases lease.Store, executor Executor, options ...RunnerOption) (*Runner, error) {
	if registry == nil || leases == nil || executor == nil {
		return nil, ErrInvalidRunner
	}
	runner := &Runner{
		registry: registry, leases: leases, executor: executor, clock: realClock{},
		maxExecutions:   DefaultMaxConcurrentExecutions,
		leaseTimeout:    DefaultLeaseOperationTimeout,
		callbackTimeout: DefaultCallbackTimeout,
		maxCallbacks:    DefaultMaxConcurrentCallbacks,
	}
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(runner); err != nil {
			return nil, err
		}
	}
	if runner.owner == "" {
		return nil, fmt.Errorf("%w: owner is required", ErrInvalidRunner)
	}
	runner.executionSlots = make(chan struct{}, runner.maxExecutions)
	runner.callbackSlots = make(chan struct{}, runner.maxCallbacks)
	_, replacementSupported := leases.(lease.ReplacementStore)
	capabilities := leases.Capabilities()
	for _, name := range registry.names {
		schedule := registry.entries[name].schedule
		if schedule.WithoutOverlapping && !capabilities.Heartbeat {
			return nil, fmt.Errorf("%w: schedule %q", ErrUnsupportedHeartbeat, name)
		}
		if schedule.OverlapPolicy == OverlapReplace && !replacementSupported {
			return nil, fmt.Errorf("%w: schedule %q", ErrUnsupportedOverlap, name)
		}
	}
	return runner, nil
}

// WithCallbackTimeout bounds individual conditions, hooks, and observers.
func WithCallbackTimeout(timeout time.Duration) RunnerOption {
	return func(runner *Runner) error {
		if timeout <= 0 {
			return fmt.Errorf("%w: callback timeout must be positive", ErrInvalidRunner)
		}
		runner.callbackTimeout = timeout
		return nil
	}
}

// WithMaxConcurrentCallbacks bounds active and timed-out callback goroutines.
func WithMaxConcurrentCallbacks(limit int) RunnerOption {
	return func(runner *Runner) error {
		if limit < 1 {
			return fmt.Errorf("%w: callback limit must be positive", ErrInvalidRunner)
		}
		runner.maxCallbacks = limit
		return nil
	}
}

// WithLeaseOperationTimeout bounds individual lease backend operations.
func WithLeaseOperationTimeout(timeout time.Duration) RunnerOption {
	return func(runner *Runner) error {
		if timeout <= 0 {
			return fmt.Errorf("%w: lease timeout must be positive", ErrInvalidRunner)
		}
		runner.leaseTimeout = timeout
		return nil
	}
}

// WithMaxConcurrentExecutions bounds active and timed-out managed executions.
func WithMaxConcurrentExecutions(limit int) RunnerOption {
	return func(runner *Runner) error {
		if limit < 1 {
			return fmt.Errorf("%w: execution limit must be positive", ErrInvalidRunner)
		}
		runner.maxExecutions = limit
		return nil
	}
}

// WithOwner sets the unique identity used for lease ownership.
func WithOwner(owner string) RunnerOption {
	return func(runner *Runner) error {
		runner.owner = owner
		return nil
	}
}

// WithEnvironment selects schedules allowed in an application environment.
func WithEnvironment(environment string) RunnerOption {
	return func(runner *Runner) error {
		runner.environment = environment
		return nil
	}
}

// WithMaintenanceMode controls maintenance-policy schedule filtering.
func WithMaintenanceMode(enabled bool) RunnerOption {
	return func(runner *Runner) error {
		runner.maintenance = enabled
		return nil
	}
}

// WithObserver appends a lifecycle observer when non-nil.
func WithObserver(observer Observer) RunnerOption {
	return func(runner *Runner) error {
		if observer != nil {
			if len(runner.observers) == MaxObservers {
				return fmt.Errorf("%w: observers exceed %d entries", ErrInvalidRunner, MaxObservers)
			}
			runner.observers = append(runner.observers, observer)
		}
		return nil
	}
}

// WithClock replaces wall time with an injectable scheduler clock.
func WithClock(clock Clock) RunnerOption {
	return func(runner *Runner) error {
		if clock == nil {
			return fmt.Errorf("%w: clock is required", ErrInvalidRunner)
		}
		runner.clock = clock
		return nil
	}
}

// Run waits for exact schedule boundaries and processes due occurrences.
func (runner *Runner) Run(ctx context.Context) error {
	cursor := runner.clock.Now()
	for {
		next, ok := runner.next(cursor)
		if !ok {
			<-ctx.Done()
			return ctx.Err()
		}
		delay := next.Sub(runner.clock.Now())
		if delay < 0 {
			delay = 0
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-runner.clock.After(delay):
		}
		now := runner.clock.Now()
		if err := runner.Tick(ctx, cursor, now); err != nil {
			return err
		}
		cursor = now
	}
}

func (runner *Runner) next(after time.Time) (time.Time, bool) {
	var earliest time.Time
	for _, name := range runner.registry.names {
		entry := runner.registry.entries[name]
		if !entry.schedule.Enabled {
			continue
		}
		next := entry.cron.Next(after)
		if earliest.IsZero() || next.Before(earliest) {
			earliest = next
		}
	}
	return earliest, !earliest.IsZero()
}

// Tick processes every bounded decision in an explicit instant range.
func (runner *Runner) Tick(ctx context.Context, after, through time.Time) error {
	if err := runner.begin(); err != nil {
		return err
	}
	defer runner.inflight.Done()

	var errs []error
	for _, name := range runner.registry.names {
		entry := runner.registry.entries[name]
		occurrences, err := runner.registry.Due(name, after, through)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, occurrence := range occurrences {
			if err := runner.decide(ctx, entry.schedule, occurrence, through); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// Drain rejects new ticks and waits for active decisions until ctx ends.
func (runner *Runner) Drain(ctx context.Context) error {
	runner.stateMu.Lock()
	runner.draining = true
	runner.stateMu.Unlock()

	done := make(chan struct{})
	go func() {
		runner.inflight.Wait()
		runner.executions.Wait()
		runner.callbacks.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (runner *Runner) begin() error {
	runner.stateMu.Lock()
	defer runner.stateMu.Unlock()
	if runner.draining {
		return ErrDraining
	}
	runner.inflight.Add(1)
	return nil
}

func (runner *Runner) decide(ctx context.Context, schedule Schedule, occurrence Occurrence, now time.Time) error {
	if reason := runner.skipReason(schedule); reason != nil {
		runner.skipped(ctx, occurrence, now, reason)
		return nil
	}

	scheduled := Context{
		Schedule:       cloneSchedule(schedule),
		Now:            now,
		Due:            occurrence.ScheduledAt,
		Attempt:        occurrence.Attempt,
		Owner:          runner.owner,
		IdempotencyKey: occurrence.IdempotencyKey,
		Metadata:       slicesToMap(schedule.Metadata),
	}
	for _, condition := range schedule.Conditions {
		allowed, err := runner.runCondition(ctx, condition, scheduled)
		if err != nil {
			runner.failed(ctx, occurrence, now, err, 0)
			return err
		}
		if !allowed {
			runner.skipped(ctx, occurrence, now, errors.New("scheduler: condition rejected occurrence"))
			return nil
		}
	}

	if schedule.OnOneServer {
		key := "occurrence:" + occurrence.IdempotencyKey
		leaseCtx, cancelLease := runner.leaseContext(ctx)
		owned, err := runner.leases.Acquire(leaseCtx, key, runner.owner, schedule.LeaseTTL, now)
		cancelLease()
		if errors.Is(err, lease.ErrHeld) {
			runner.skipped(ctx, occurrence, now, err)
			return nil
		}
		if err != nil {
			runner.failed(ctx, occurrence, now, err, 0)
			return err
		}
		scheduled.Fencing = owned.FencingToken
	}

	var taskLease lease.Lease
	if schedule.WithoutOverlapping {
		var err error
		taskLease, err = runner.acquireTask(ctx, schedule, now)
		if errors.Is(err, lease.ErrHeld) {
			runner.emit(Event{Type: EventOverlap, Result: ResultSkipped, Occurrence: occurrence, Context: ctx, Owner: runner.owner, At: now, Err: err})
			runner.emit(Event{Type: EventCompleted, Result: ResultSkipped, Occurrence: occurrence, Context: ctx, Owner: runner.owner, At: now, Err: err})
			return nil
		}
		if err != nil {
			runner.failed(ctx, occurrence, now, err, 0)
			return err
		}
		scheduled.Fencing = taskLease.FencingToken
	}

	runner.emit(Event{Type: EventBefore, Occurrence: occurrence, Context: ctx, Owner: runner.owner, Fencing: scheduled.Fencing, At: now})
	runCtx, cancel := context.WithTimeout(ctx, schedule.RunTimeout)
	managed, err := runner.startExecution(ctx, runCtx, cancel, scheduled, taskLease, schedule.LeaseTTL)
	if err == nil {
		err = awaitExecution(runCtx, managed)
	}
	cancel()
	if err != nil {
		runner.failed(ctx, occurrence, now, err, scheduled.Fencing)
		return err
	}
	runner.emit(Event{Type: EventSuccess, Result: ResultSucceeded, Occurrence: occurrence, Context: ctx, Owner: runner.owner, Fencing: scheduled.Fencing, At: now})
	runner.emit(Event{Type: EventCompleted, Result: ResultSucceeded, Occurrence: occurrence, Context: ctx, Owner: runner.owner, Fencing: scheduled.Fencing, At: now})
	return nil
}

func (runner *Runner) runCondition(
	ctx context.Context,
	condition Condition,
	scheduled Context,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	select {
	case runner.callbackSlots <- struct{}{}:
	default:
		return false, ErrCallbackCapacity
	}
	runner.callbacks.Add(1)
	done := make(chan conditionResult, 1)
	go func() {
		defer func() {
			<-runner.callbackSlots
			runner.callbacks.Done()
		}()
		allowed, err := runCondition(condition, scheduled)
		done <- conditionResult{allowed: allowed, err: err}
	}()
	timer := time.NewTimer(runner.callbackTimeout)
	defer timer.Stop()
	select {
	case result := <-done:
		return result.allowed, result.err
	case <-ctx.Done():
		return false, ctx.Err()
	case <-timer.C:
		return false, ErrCallbackTimeout
	}
}

func (runner *Runner) startExecution(
	ctx context.Context,
	runCtx context.Context,
	cancelRun context.CancelFunc,
	scheduled Context,
	taskLease lease.Lease,
	ttl time.Duration,
) (managedExecution, error) {
	if err := ctx.Err(); err != nil {
		return managedExecution{}, err
	}
	select {
	case runner.executionSlots <- struct{}{}:
	default:
		return managedExecution{}, ErrExecutionCapacity
	}
	runner.executions.Add(1)
	leaseCtx, cancelLease := context.WithCancel(context.WithoutCancel(ctx))
	var monitor *heartbeatMonitor
	if taskLease.Key != "" {
		monitor = &heartbeatMonitor{done: make(chan struct{})}
		go runner.heartbeatTaskLease(leaseCtx, cancelRun, taskLease, ttl, monitor)
	}
	done := make(chan error, 1)
	go func() {
		defer func() {
			<-runner.executionSlots
			runner.executions.Done()
		}()
		err := runExecutor(runCtx, runner.executor, scheduled)
		cancelLease()
		if monitor != nil {
			<-monitor.done
			taskLease = monitor.result.owned
			err = errors.Join(err, monitor.result.err)
		}
		if taskLease.Key != "" {
			releaseCtx, cancelRelease := runner.leaseContext(context.WithoutCancel(ctx))
			err = errors.Join(err, runner.leases.Release(releaseCtx, taskLease))
			cancelRelease()
		}
		done <- err
	}()
	return managedExecution{done: done, heartbeat: monitor}, nil
}

func awaitExecution(ctx context.Context, managed managedExecution) error {
	var heartbeatDone <-chan struct{}
	if managed.heartbeat != nil {
		heartbeatDone = managed.heartbeat.done
	}
	for {
		select {
		case err := <-managed.done:
			return errors.Join(err, ctx.Err())
		case <-heartbeatDone:
			if managed.heartbeat.result.err != nil {
				return managed.heartbeat.result.err
			}
			heartbeatDone = nil
		case <-ctx.Done():
			if managed.heartbeat != nil {
				select {
				case <-managed.heartbeat.done:
					return errors.Join(ctx.Err(), managed.heartbeat.result.err)
				default:
				}
			}
			return ctx.Err()
		}
	}
}

func (runner *Runner) heartbeatTaskLease(
	ctx context.Context,
	cancel context.CancelFunc,
	owned lease.Lease,
	ttl time.Duration,
	monitor *heartbeatMonitor,
) {
	defer close(monitor.done)
	interval := max(ttl/3, time.Nanosecond)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	started := time.Now()
	current := owned
	for {
		select {
		case <-ctx.Done():
			monitor.result = heartbeatResult{owned: current}
			return
		case <-ticker.C:
			now := owned.AcquiredAt.Add(time.Since(started))
			heartbeatCtx, cancelHeartbeat := runner.leaseContext(ctx)
			renewed, err := runner.leases.Heartbeat(heartbeatCtx, current, ttl, now)
			cancelHeartbeat()
			if err != nil {
				cancel()
				monitor.result = heartbeatResult{owned: current, err: err}
				return
			}
			current = renewed
		}
	}
}

func (runner *Runner) acquireTask(ctx context.Context, schedule Schedule, now time.Time) (lease.Lease, error) {
	leaseCtx, cancel := runner.leaseContext(ctx)
	defer cancel()
	key := "task:" + schedule.CoordinationID
	owned, err := runner.leases.Acquire(leaseCtx, key, runner.owner, schedule.LeaseTTL, now)
	if !errors.Is(err, lease.ErrHeld) || schedule.OverlapPolicy != OverlapReplace {
		return owned, err
	}
	current, inspectErr := runner.leases.Inspect(leaseCtx, key)
	if inspectErr != nil {
		return lease.Lease{}, inspectErr
	}
	replacement := runner.leases.(lease.ReplacementStore)
	return replacement.Replace(leaseCtx, current, runner.owner, schedule.LeaseTTL, now)
}

func (runner *Runner) leaseContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, runner.leaseTimeout)
}

func (runner *Runner) skipReason(schedule Schedule) error {
	if len(schedule.Environments) > 0 && !slices.Contains(schedule.Environments, runner.environment) {
		return errors.New("scheduler: environment not allowed")
	}
	if runner.maintenance && schedule.MaintenancePolicy != MaintenanceRun {
		return errors.New("scheduler: maintenance mode")
	}
	return nil
}

func (runner *Runner) skipped(ctx context.Context, occurrence Occurrence, now time.Time, err error) {
	runner.emit(Event{Type: EventSkipped, Result: ResultSkipped, Occurrence: occurrence, Context: ctx, Owner: runner.owner, At: now, Err: err})
	runner.emit(Event{Type: EventCompleted, Result: ResultSkipped, Occurrence: occurrence, Context: ctx, Owner: runner.owner, At: now, Err: err})
}

func (runner *Runner) failed(ctx context.Context, occurrence Occurrence, now time.Time, err error, fencing uint64) {
	runner.emit(Event{Type: EventFailure, Result: ResultFailed, Occurrence: occurrence, Context: ctx, Owner: runner.owner, Fencing: fencing, At: now, Err: err})
	runner.emit(Event{Type: EventCompleted, Result: ResultFailed, Occurrence: occurrence, Context: ctx, Owner: runner.owner, Fencing: fencing, At: now, Err: err})
}

func (runner *Runner) emit(event Event) {
	callbackCtx := event.Context
	if callbackCtx == nil {
		callbackCtx = context.Background()
	} else {
		callbackCtx = context.WithoutCancel(callbackCtx)
	}
	if entry, ok := runner.registry.entries[event.Occurrence.ScheduleName]; ok {
		hook := hookFor(entry.schedule.Hooks, event.Type)
		if hook != nil {
			runner.runCallback(callbackCtx, func() { hook(event) })
		}
	}
	for _, observer := range runner.observers {
		runner.runCallback(callbackCtx, func() { observer.Observe(event) })
	}
}

func (runner *Runner) runCallback(ctx context.Context, callback func()) {
	if ctx.Err() != nil {
		return
	}
	select {
	case runner.callbackSlots <- struct{}{}:
	default:
		return
	}
	runner.callbacks.Add(1)
	done := make(chan struct{})
	go func() {
		defer func() {
			_ = recover()
			close(done)
			<-runner.callbackSlots
			runner.callbacks.Done()
		}()
		callback()
	}()
	timer := time.NewTimer(runner.callbackTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-ctx.Done():
	case <-timer.C:
	}
}

func hookFor(hooks Hooks, eventType EventType) Hook {
	switch eventType {
	case EventBefore:
		return hooks.Before
	case EventSuccess:
		return hooks.Success
	case EventFailure:
		return hooks.Failure
	case EventSkipped:
		return hooks.Skipped
	case EventOverlap:
		return hooks.Overlap
	case EventCompleted:
		return hooks.Completed
	default:
		return nil
	}
}

func runCondition(condition Condition, scheduled Context) (allowed bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w in condition: %v", ErrTaskPanic, recovered)
		}
	}()
	return condition(scheduled)
}

func runExecutor(ctx context.Context, executor Executor, scheduled Context) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: %v\n%s", ErrTaskPanic, recovered, debug.Stack())
		}
	}()
	return executor.Execute(ctx, scheduled)
}

func slicesToMap(metadata map[string]string) map[string]string {
	result := make(map[string]string, len(metadata))
	for key, value := range metadata {
		result[key] = value
	}
	return result
}
