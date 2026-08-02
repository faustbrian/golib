package hedge

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// Attempt performs one independently owned execution. Cancellation is
// cooperative; implementations must return promptly when ctx is canceled.
type Attempt[T any] func(context.Context) (T, error)

// AttemptInfo identifies an original or additional execution. Ordinal zero is
// always the original; positive ordinals are hedges.
type AttemptInfo struct {
	Ordinal uint
	Hedge   bool
	Delay   time.Duration
}

// AttemptFactory creates independently owned mutable state for every attempt.
// Construction must honor the context, return promptly, and avoid starting the
// external operation. Endpoint must be a bounded, credential-free identity
// suitable for labels.
type AttemptFactory[T any] interface {
	NewAttempt(context.Context, AttemptInfo) (Attempt[T], string, error)
}

// AttemptFactoryFunc adapts a non-blocking factory function. Use a concrete
// AttemptFactory when construction itself needs the logical context.
type AttemptFactoryFunc[T any] func(AttemptInfo) (Attempt[T], string, error)

// NewAttempt invokes the adapted function.
func (function AttemptFactoryFunc[T]) NewAttempt(_ context.Context, info AttemptInfo) (Attempt[T], string, error) {
	return function(info)
}

// Reason identifies the terminal logical outcome.
type Reason uint8

const (
	// ReasonNoHedgeNeeded means the original won before additional work started.
	ReasonNoHedgeNeeded Reason = iota + 1
	// ReasonWinnerSelected means one of multiple started attempts won.
	ReasonWinnerSelected
	// ReasonAllAttemptsFailed means every admitted attempt completed unsuccessfully.
	ReasonAllAttemptsFailed
	// ReasonCallerCanceled means the caller canceled the logical operation.
	ReasonCallerCanceled
	// ReasonTotalDeadline means the configured total timeout elapsed.
	ReasonTotalDeadline
	// ReasonTerminalFailure means classification stopped the logical operation.
	ReasonTerminalFailure
	// ReasonFactoryFailure means attempt construction stopped the operation.
	ReasonFactoryFailure
	// ReasonDelayFailure means dynamic delay selection failed validation.
	ReasonDelayFailure
)

// Failure retains bounded attempt metadata without retaining or joining raw
// error messages.
type Failure struct {
	Ordinal        uint
	Hedge          bool
	Delay          time.Duration
	Duration       time.Duration
	Endpoint       string
	Classification Classification
}

// Report describes one logical execution. Wait lets callers and graceful
// shutdown code wait for cooperative loser cleanup without delaying winner
// delivery.
type Report struct {
	Reason          Reason
	AttemptsStarted uint
	HedgesStarted   uint
	BudgetDenied    uint
	WinnerOrdinal   uint
	SelectedOrdinal uint
	Failures        []Failure
	cleanup         *cleanupState
}

// Wait waits for all started attempt functions to return and for every
// non-winning returned value to be disposed. It reports cleanup failures by
// count without exposing raw disposer messages.
func (report Report) Wait(ctx context.Context) error {
	if report.cleanup == nil {
		return nil
	}
	return report.cleanup.wait(ctx)
}

// ExecutionError reports deterministic all-attempt failure. Its message never
// contains downstream errors; Unwrap exposes only the documented selected
// cause, which is the lowest ordinal failure.
type ExecutionError struct{ cause error }

func (err *ExecutionError) Error() string { return "hedge: all attempts failed" }
func (err *ExecutionError) Unwrap() error { return err.cause }

// CanceledError distinguishes caller cancellation from downstream failure.
type CanceledError struct{ cause error }

func (err *CanceledError) Error() string { return "hedge: caller canceled" }
func (err *CanceledError) Unwrap() error { return err.cause }

// DeadlineError distinguishes the total policy deadline from downstream
// failure.
type DeadlineError struct{}

func (*DeadlineError) Error() string { return "hedge: total deadline exceeded" }
func (*DeadlineError) Unwrap() error { return context.DeadlineExceeded }

// CleanupError reports how many result disposals failed.
type CleanupError struct{ Failures uint }

func (err *CleanupError) Error() string {
	return fmt.Sprintf("hedge: %d cleanup operation(s) failed", err.Failures)
}

type cleanupState struct {
	mu       sync.Mutex
	active   uint
	sealed   bool
	done     chan struct{}
	failures uint
	closed   bool
}

func newCleanupState() *cleanupState { return &cleanupState{done: make(chan struct{})} }

func (state *cleanupState) add() {
	state.mu.Lock()
	state.active++
	state.mu.Unlock()
}

func (state *cleanupState) finish() {
	state.mu.Lock()
	state.active--
	state.closeIfDoneLocked()
	state.mu.Unlock()
}

func (state *cleanupState) fail() {
	state.mu.Lock()
	state.failures++
	state.mu.Unlock()
}

func (state *cleanupState) seal() {
	state.mu.Lock()
	state.sealed = true
	state.closeIfDoneLocked()
	state.mu.Unlock()
}

func (state *cleanupState) closeIfDoneLocked() {
	if state.sealed && state.active == 0 && !state.closed {
		state.closed = true
		close(state.done)
	}
}

func (state *cleanupState) wait(ctx context.Context) error {
	select {
	case <-state.done:
		state.mu.Lock()
		failures := state.failures
		state.mu.Unlock()
		if failures != 0 {
			return &CleanupError{Failures: failures}
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type attemptCompletion[T any] struct {
	result            AttemptResult[T]
	classification    Classification
	classificationErr error
	hasValue          bool
	started           time.Time
	completed         time.Time
	delay             time.Duration
	endpoint          string
	permit            Permit
}

type publicationState struct {
	mu     sync.Mutex
	closed bool
}

func publish[T any](state *publicationState, channel chan<- attemptCompletion[T], completion attemptCompletion[T]) bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.closed {
		return false
	}
	channel <- completion
	return true
}

func (state *publicationState) close() {
	state.mu.Lock()
	state.closed = true
	state.mu.Unlock()
}

// Do executes one original attempt and at most MaxHedges delayed concurrent
// attempts. The factory is required to create independently owned mutable
// state; Do never clones or interprets application requests.
func Do[T any](ctx context.Context, policy *Policy[T], factory AttemptFactory[T]) (T, Report, error) {
	var zero T
	if ctx == nil || policy == nil || nilLike(factory) {
		return zero, Report{}, fmt.Errorf("%w: context, policy, and factory are required", ErrInvalidPolicy)
	}

	config := policy.config
	if err := ctx.Err(); err != nil {
		emit(config.Observer, Observation{Outcome: OutcomeCallerCanceled, Resource: config.Resource})
		return zero, Report{Reason: ReasonCallerCanceled}, &CanceledError{cause: err}
	}
	totalCtx, totalCancel := config.Clock.WithTimeout(ctx, config.TotalTimeout)
	attemptsCtx, cancelAttempts := context.WithCancel(totalCtx)
	cleanup := newCleanupState()
	report := Report{cleanup: cleanup}
	publication := &publicationState{}
	completions := make(chan attemptCompletion[T], config.MaxHedges+1)

	closeExecution := func() {
		publication.close()
		cancelAttempts()
		totalCancel()
		drainAndDispose(config, completions, cleanup)
		cleanup.seal()
	}

	launch := func(info AttemptInfo, permit Permit) error {
		attempt, endpoint, err := safeFactory(totalCtx, factory, info)
		if err != nil {
			if permit != nil {
				permit.Release()
			}
			return err
		}
		if attempt == nil || len(endpoint) > MaxResourceLength {
			if permit != nil {
				permit.Release()
			}
			return fmt.Errorf("%w: factory returned invalid attempt metadata", ErrInvalidPolicy)
		}
		report.AttemptsStarted++
		if info.Hedge {
			report.HedgesStarted++
		}
		cleanup.add()
		go runAttempt(attemptsCtx, config, info, endpoint, attempt, permit, completions, publication, cleanup)
		return nil
	}

	if err := launch(AttemptInfo{}, nil); err != nil {
		report.Reason = ReasonFactoryFailure
		closeExecution()
		return zero, report, &ExecutionError{cause: err}
	}

	nextHedge := uint(1)
	previousDelay := time.Duration(0)
	active := uint(1)
	var failures []attemptCompletion[T]
	var timer Timer
	var timerC <-chan time.Time
	var scheduledDelay time.Duration

	scheduleNext := func() error {
		if nextHedge > config.MaxHedges {
			timer = nil
			timerC = nil
			return nil
		}
		delay, err := policy.delay(nextHedge, previousDelay)
		if err != nil || delay <= 0 {
			return fmt.Errorf("%w: dynamic delay for hedge %d", ErrInvalidPolicy, nextHedge)
		}
		scheduledDelay = delay
		timer = config.Clock.NewTimer(delay)
		timerC = timer.C()
		return nil
	}

	if err := scheduleNext(); err != nil {
		report.Reason = ReasonDelayFailure
		closeExecution()
		return zero, report, &ExecutionError{cause: err}
	}

	processCompletion := func(completion attemptCompletion[T]) (T, error, bool) {
		active--
		releaseCompletion(completion)
		if completion.classification == ClassificationSuccess && completion.classificationErr == nil {
			if timer != nil {
				timer.Stop()
			}
			publication.close()
			cancelAttempts()
			totalCancel()
			published := drainCompletions(completions)
			winner, losers := chooseWinner(completion, published, failures)
			report.WinnerOrdinal = winner.result.Ordinal
			if report.HedgesStarted == 0 {
				report.Reason = ReasonNoHedgeNeeded
				emit(config.Observer, Observation{Outcome: OutcomeNoHedgeNeeded, Ordinal: winner.result.Ordinal, Duration: winner.completed.Sub(winner.started), Resource: config.Resource, Endpoint: winner.endpoint, Winner: true})
			} else {
				report.Reason = ReasonWinnerSelected
				emit(config.Observer, Observation{Outcome: OutcomeWinnerSelected, Ordinal: winner.result.Ordinal, Delay: winner.delay, Duration: winner.completed.Sub(winner.started), Resource: config.Resource, Endpoint: winner.endpoint, Winner: true})
			}
			report.Failures = failureMetadata(nonSuccesses(losers))
			disposeAll(config, losers, cleanup)
			cleanup.seal()
			return winner.result.Value, nil, true
		}

		failures = append(failures, completion)
		if completion.classification == ClassificationTerminal || completion.classificationErr != nil {
			if timer != nil {
				timer.Stop()
			}
			report.Reason = ReasonTerminalFailure
			report.Failures = failureMetadata(failures)
			report.SelectedOrdinal = completion.result.Ordinal
			disposeExcept(config, failures, completion.result.Ordinal, cleanup)
			closeExecution()
			return completion.result.Value, &ExecutionError{cause: completionError(completion)}, true
		}
		return zero, nil, false
	}

	timerDue := false
	for {
		if active == 0 && nextHedge > config.MaxHedges {
			report.Reason = ReasonAllAttemptsFailed
			report.Failures = failureMetadata(failures)
			selected := deterministicSelection(failures)
			report.SelectedOrdinal = selected.result.Ordinal
			emit(config.Observer, Observation{Outcome: OutcomeAllAttemptsFailed, Ordinal: selected.result.Ordinal, Resource: config.Resource, Endpoint: selected.endpoint})
			disposeExcept(config, failures, selected.result.Ordinal, cleanup)
			closeExecution()
			return selected.result.Value, report, &ExecutionError{cause: completionError(selected)}
		}
		if timerDue {
			select {
			case completion := <-completions:
				value, completionErr, done := processCompletion(completion)
				if done {
					return value, report, completionErr
				}
				continue
			default:
			}

			timerDue = false
			permit, admitted := config.Budget.TryAcquire(config.Resource)
			if !admitted || permit == nil {
				report.BudgetDenied++
				emit(config.Observer, Observation{Outcome: OutcomeBudgetDenied, Ordinal: nextHedge, Delay: scheduledDelay, Resource: config.Resource})
			} else {
				info := AttemptInfo{Ordinal: nextHedge, Hedge: true, Delay: scheduledDelay}
				if err := launch(info, permit); err != nil {
					if config.FactoryFailureMode == FactoryFailureStop {
						report.Reason = ReasonFactoryFailure
						disposeAll(config, failures, cleanup)
						closeExecution()
						return zero, report, &ExecutionError{cause: err}
					}
					failures = append(failures, factoryCompletion[T](info, config.Clock.Now(), err))
				} else {
					active++
					emit(config.Observer, Observation{Outcome: OutcomeHedgeStarted, Ordinal: nextHedge, Delay: scheduledDelay, Resource: config.Resource})
				}
			}
			previousDelay = scheduledDelay
			nextHedge++
			if err := scheduleNext(); err != nil {
				report.Reason = ReasonDelayFailure
				disposeAll(config, failures, cleanup)
				closeExecution()
				return zero, report, &ExecutionError{cause: err}
			}
			continue
		}

		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			report.Reason = ReasonCallerCanceled
			emit(config.Observer, Observation{Outcome: OutcomeCallerCanceled, Resource: config.Resource})
			disposeAll(config, failures, cleanup)
			closeExecution()
			return zero, report, &CanceledError{cause: ctx.Err()}
		case <-totalCtx.Done():
			if err := ctx.Err(); err != nil {
				report.Reason = ReasonCallerCanceled
				emit(config.Observer, Observation{Outcome: OutcomeCallerCanceled, Resource: config.Resource})
				disposeAll(config, failures, cleanup)
				closeExecution()
				return zero, report, &CanceledError{cause: err}
			}
			if timer != nil {
				timer.Stop()
			}
			report.Reason = ReasonTotalDeadline
			emit(config.Observer, Observation{Outcome: OutcomeTotalDeadline, Resource: config.Resource})
			disposeAll(config, failures, cleanup)
			closeExecution()
			return zero, report, &DeadlineError{}
		case completion := <-completions:
			value, completionErr, done := processCompletion(completion)
			if done {
				return value, report, completionErr
			}
		case <-timerC:
			if timer != nil {
				timer.Stop()
			}
			timerC = nil
			timerDue = true
		}
	}
}

func safeFactory[T any](ctx context.Context, factory AttemptFactory[T], info AttemptInfo) (attempt Attempt[T], endpoint string, err error) {
	defer func() {
		if recover() != nil {
			attempt = nil
			endpoint = ""
			err = errors.New("hedge: attempt factory panicked")
		}
	}()
	return factory.NewAttempt(ctx, info)
}

func (policy *Policy[T]) delay(hedge uint, previous time.Duration) (time.Duration, error) {
	config := policy.config
	switch {
	case config.Delay > 0:
		return config.Delay, nil
	case len(config.Schedule) != 0:
		return config.Schedule[hedge-1], nil
	default:
		return safeDynamicDelay(config.DynamicDelay, DelayInput{Hedge: hedge, Previous: previous})
	}
}

func safeDynamicDelay(function DelayFunc, input DelayInput) (delay time.Duration, err error) {
	defer func() {
		if recover() != nil {
			delay = 0
			err = errors.New("hedge: dynamic delay panicked")
		}
	}()
	return function(input)
}

func runAttempt[T any](parent context.Context, config Config[T], info AttemptInfo, endpoint string, attempt Attempt[T], permit Permit, completions chan<- attemptCompletion[T], publication *publicationState, cleanup *cleanupState) {
	started := config.Clock.Now()
	attemptCtx := parent
	cancel := func() {}
	if config.AttemptTimeout > 0 {
		attemptCtx, cancel = config.Clock.WithTimeout(parent, config.AttemptTimeout)
	}
	defer cancel()

	var value T
	var attemptErr error
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				attemptErr = fmt.Errorf("hedge: attempt panicked")
			}
		}()
		value, attemptErr = attempt(attemptCtx)
	}()
	result := AttemptResult[T]{Value: value, Err: attemptErr, ContextErr: attemptCtx.Err(), Ordinal: info.Ordinal, Hedge: info.Hedge}
	classification, classificationErr := safeClassify(attemptCtx, config.Classifier, result)
	if classification < ClassificationSuccess || classification > ClassificationTerminal {
		classificationErr = fmt.Errorf("%w: classifier returned invalid classification", ErrInvalidPolicy)
	}
	completion := attemptCompletion[T]{result: result, classification: classification, classificationErr: classificationErr, hasValue: true, started: started, completed: config.Clock.Now(), delay: info.Delay, endpoint: endpoint, permit: permit}
	if publish(publication, completions, completion) {
		emit(config.Observer, Observation{Outcome: OutcomeAttemptCompleted, Ordinal: info.Ordinal, Delay: info.Delay, Duration: completion.completed.Sub(started), Resource: config.Resource, Endpoint: endpoint, Classification: classification})
	} else {
		releaseCompletion(completion)
		dispose(config, value, cleanup, info, endpoint, completion.completed.Sub(started))
	}
	cleanup.finish()
}

func safeClassify[T any](ctx context.Context, classifier Classifier[T], result AttemptResult[T]) (classification Classification, err error) {
	defer func() {
		if recover() != nil {
			classification = ClassificationTerminal
			err = errors.New("hedge: classifier panicked")
		}
	}()
	return classifier.Classify(ctx, result)
}

func dispose[T any](config Config[T], value T, cleanup *cleanupState, info AttemptInfo, endpoint string, duration time.Duration) {
	cleanupCtx, cancel := config.Clock.WithTimeout(context.Background(), config.CleanupTimeout)
	err := safeDispose(cleanupCtx, config.Disposer, value)
	cancel()
	if err != nil {
		cleanup.fail()
		emit(config.Observer, Observation{Outcome: OutcomeCleanupFailed, Ordinal: info.Ordinal, Delay: info.Delay, Duration: duration, Resource: config.Resource, Endpoint: endpoint, Loser: true})
	}
}

func safeDispose[T any](ctx context.Context, disposer Disposer[T], value T) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("hedge: result disposer panicked")
		}
	}()
	return disposer.Dispose(ctx, value)
}

func drainAndDispose[T any](config Config[T], completions <-chan attemptCompletion[T], cleanup *cleanupState) {
	disposeAll(config, drainCompletions(completions), cleanup)
}

func drainCompletions[T any](completions <-chan attemptCompletion[T]) []attemptCompletion[T] {
	drained := make([]attemptCompletion[T], 0)
	for {
		select {
		case completion := <-completions:
			releaseCompletion(completion)
			drained = append(drained, completion)
		default:
			return drained
		}
	}
}

func releaseCompletion[T any](completion attemptCompletion[T]) {
	if completion.permit != nil {
		completion.permit.Release()
	}
}

func completionLess[T any](left, right attemptCompletion[T]) bool {
	if left.completed.Equal(right.completed) {
		return left.result.Ordinal < right.result.Ordinal
	}
	return left.completed.Before(right.completed)
}

func chooseWinner[T any](current attemptCompletion[T], published, priorFailures []attemptCompletion[T]) (attemptCompletion[T], []attemptCompletion[T]) {
	winner := current
	losers := append([]attemptCompletion[T](nil), priorFailures...)
	for _, candidate := range published {
		if candidate.classification == ClassificationSuccess && candidate.classificationErr == nil && completionLess(candidate, winner) {
			losers = append(losers, winner)
			winner = candidate
		} else {
			losers = append(losers, candidate)
		}
	}
	return winner, losers
}

func nonSuccesses[T any](completions []attemptCompletion[T]) []attemptCompletion[T] {
	failures := make([]attemptCompletion[T], 0, len(completions))
	for _, completion := range completions {
		if completion.classification != ClassificationSuccess || completion.classificationErr != nil {
			failures = append(failures, completion)
		}
	}
	return failures
}

func failureMetadata[T any](completions []attemptCompletion[T]) []Failure {
	metadata := make([]Failure, 0, len(completions))
	for _, completion := range completions {
		metadata = append(metadata, Failure{Ordinal: completion.result.Ordinal, Hedge: completion.result.Hedge, Delay: completion.delay, Duration: completion.completed.Sub(completion.started), Endpoint: completion.endpoint, Classification: completion.classification})
	}
	slices.SortFunc(metadata, func(left, right Failure) int {
		return cmp.Compare(left.Ordinal, right.Ordinal)
	})
	return metadata
}

func deterministicSelection[T any](completions []attemptCompletion[T]) attemptCompletion[T] {
	if len(completions) == 0 {
		return attemptCompletion[T]{}
	}
	slices.SortFunc(completions, func(left, right attemptCompletion[T]) int {
		return cmp.Compare(left.result.Ordinal, right.result.Ordinal)
	})
	return completions[0]
}

func completionError[T any](completion attemptCompletion[T]) error {
	if completion.classificationErr != nil {
		return completion.classificationErr
	}
	if completion.result.Err != nil {
		return completion.result.Err
	}
	if completion.result.ContextErr != nil {
		return completion.result.ContextErr
	}
	return errors.New("hedge: attempt classified as failure without an error")
}

func factoryCompletion[T any](info AttemptInfo, at time.Time, err error) attemptCompletion[T] {
	return attemptCompletion[T]{result: AttemptResult[T]{Err: err, Ordinal: info.Ordinal, Hedge: true}, classification: ClassificationFailure, started: at, completed: at, delay: info.Delay}
}

func disposeAll[T any](config Config[T], completions []attemptCompletion[T], cleanup *cleanupState) {
	for _, completion := range completions {
		if completion.hasValue {
			disposeAsync(config, completion, cleanup)
		}
	}
}

func disposeExcept[T any](config Config[T], completions []attemptCompletion[T], selected uint, cleanup *cleanupState) {
	for _, completion := range completions {
		if completion.hasValue && completion.result.Ordinal != selected {
			disposeAsync(config, completion, cleanup)
		}
	}
}

func disposeAsync[T any](config Config[T], completion attemptCompletion[T], cleanup *cleanupState) {
	cleanup.add()
	go func() {
		defer cleanup.finish()
		dispose(config, completion.result.Value, cleanup, AttemptInfo{Ordinal: completion.result.Ordinal, Hedge: completion.result.Hedge, Delay: completion.delay}, completion.endpoint, completion.completed.Sub(completion.started))
	}()
}

func emit(observer Observer, observation Observation) {
	if observer == nil {
		return
	}
	defer func() { _ = recover() }()
	observer.TryObserve(observation)
}
