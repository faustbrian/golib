// Package hedge executes explicitly replay-safe operations with bounded,
// delayed concurrent attempts. It does not infer idempotency or clone request
// state; an AttemptFactory must create independently owned mutable state for
// every attempt.
package hedge

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"
)

// MaxHedges bounds policy allocation, retained failure metadata, and fan-out.
const MaxHedges uint = 64

// MaxResourceLength bounds endpoint-safe resource identity used by budgets and
// observations.
const MaxResourceLength = 128

// MaxBudgetCapacity prevents a nominally finite budget from acting as an
// operationally unbounded admission policy.
const MaxBudgetCapacity uint = 1_000_000

// ErrInvalidPolicy identifies implicit, contradictory, or unbounded policy
// configuration.
var ErrInvalidPolicy = errors.New("hedge: invalid policy")

// Timer is owned by one execution. Stop releases its resources; callers must
// not close C.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
}

// Clock supplies monotonic policy time, timers, and deadline contexts so
// scheduling can be tested deterministically. Implementations must be safe for
// concurrent use and must not panic.
type Clock interface {
	Now() time.Time
	NewTimer(time.Duration) Timer
	WithTimeout(context.Context, time.Duration) (context.Context, context.CancelFunc)
}

// RealClock uses the standard library's monotonic clock and timers.
type RealClock struct{}

// Now returns the current time.
func (RealClock) Now() time.Time { return time.Now() }

// NewTimer starts a standard-library timer.
func (RealClock) NewTimer(delay time.Duration) Timer { return realTimer{Timer: time.NewTimer(delay)} }

// WithTimeout derives a standard-library timeout context.
func (RealClock) WithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, timeout)
}

type realTimer struct{ *time.Timer }

func (timer realTimer) C() <-chan time.Time { return timer.Timer.C }

// Classification is the explicit policy decision for one completed attempt.
type Classification uint8

const (
	// ClassificationSuccess selects the attempt as the winner.
	ClassificationSuccess Classification = iota + 1
	// ClassificationFailure retains the failure and permits active or scheduled
	// attempts to continue.
	ClassificationFailure
	// ClassificationCanceled records a non-downstream cancellation and permits
	// other attempts to continue.
	ClassificationCanceled
	// ClassificationTerminal stops the logical operation without a winner.
	ClassificationTerminal
)

// AttemptResult is passed to a Classifier. Value and Err are never included in
// observations or error strings.
type AttemptResult[T any] struct {
	Value      T
	Err        error
	ContextErr error
	Ordinal    uint
	Hedge      bool
}

// Classifier decides whether one result wins, fails, was canceled, or
// terminally stops the logical operation. It must be safe for concurrent use.
type Classifier[T any] interface {
	Classify(context.Context, AttemptResult[T]) (Classification, error)
}

// ClassifyFunc adapts a function to Classifier.
type ClassifyFunc[T any] func(context.Context, AttemptResult[T]) (Classification, error)

// Classify invokes the adapted function.
func (function ClassifyFunc[T]) Classify(ctx context.Context, result AttemptResult[T]) (Classification, error) {
	return function(ctx, result)
}

// Disposer releases resources owned by every non-winning result. It must be
// concurrency-safe and honor its cleanup context.
type Disposer[T any] interface {
	Dispose(context.Context, T) error
}

// DisposeFunc adapts a function to Disposer.
type DisposeFunc[T any] func(context.Context, T) error

// Dispose invokes the adapted function.
func (function DisposeFunc[T]) Dispose(ctx context.Context, value T) error {
	return function(ctx, value)
}

// Permit accounts for one additional attempt. Release must be idempotent,
// concurrency-safe, non-blocking, and must not panic.
type Permit interface{ Release() }

// Budget bounds shared additional work. Implementations may account globally,
// per resource, or over a documented recent-work window. Capacity must be
// immutable, finite, and consistent with non-blocking, panic-free TryAcquire.
type Budget interface {
	Capacity() uint
	TryAcquire(resource string) (Permit, bool)
}

// FactoryFailureMode defines what happens when a hedge attempt cannot be
// constructed. Original-attempt factory failure always stops execution.
type FactoryFailureMode uint8

const (
	// FactoryFailureStop stops the logical operation and cancels active attempts.
	FactoryFailureStop FactoryFailureMode = iota + 1
	// FactoryFailureContinue records the failed hedge and permits later work.
	FactoryFailureContinue
)

// DelayInput gives a dynamic delay policy bounded metadata. Dynamic functions
// must not retain hidden unbounded latency history.
type DelayInput struct {
	Hedge    uint
	Previous time.Duration
}

// DelayFunc returns the delay after the preceding attempt launch.
type DelayFunc func(DelayInput) (time.Duration, error)

// Observer receives bounded lifecycle metadata through a non-blocking method.
// TryObserve must return immediately and must not call back into an execution.
type Observer interface {
	TryObserve(Observation) bool
}

// Config defines every safety decision and dependency for a Policy. Exactly
// one of Delay, Schedule, or DynamicDelay must be configured.
type Config[T any] struct {
	MaxHedges          uint
	ReplaySafe         bool
	Delay              time.Duration
	Schedule           []time.Duration
	DynamicDelay       DelayFunc
	TotalTimeout       time.Duration
	AttemptTimeout     time.Duration
	CleanupTimeout     time.Duration
	Clock              Clock
	Budget             Budget
	Classifier         Classifier[T]
	Disposer           Disposer[T]
	Observer           Observer
	Resource           string
	FactoryFailureMode FactoryFailureMode
}

// Policy is immutable and safe for concurrent execution. Its Budget,
// Classifier, Disposer, Clock, and Observer dependencies must also be safe for
// concurrent use.
type Policy[T any] struct{ config Config[T] }

// NewPolicy validates finite amplification, time, replay, ownership, and
// dependency contracts and copies mutable configuration.
func NewPolicy[T any](config Config[T]) (*Policy[T], error) {
	delayModes := 0
	if config.Delay != 0 {
		delayModes++
	}
	if len(config.Schedule) != 0 {
		delayModes++
	}
	if config.DynamicDelay != nil {
		delayModes++
	}

	switch {
	case config.MaxHedges == 0 || config.MaxHedges > MaxHedges:
		return nil, fmt.Errorf("%w: max hedges must be between 1 and %d", ErrInvalidPolicy, MaxHedges)
	case !config.ReplaySafe:
		return nil, fmt.Errorf("%w: concurrent replay safety must be declared", ErrInvalidPolicy)
	case delayModes != 1:
		return nil, fmt.Errorf("%w: exactly one delay strategy is required", ErrInvalidPolicy)
	case config.Delay < 0:
		return nil, fmt.Errorf("%w: fixed delay must be positive", ErrInvalidPolicy)
	case config.TotalTimeout <= 0:
		return nil, fmt.Errorf("%w: total timeout must be positive", ErrInvalidPolicy)
	case config.AttemptTimeout < 0 || config.AttemptTimeout > config.TotalTimeout:
		return nil, fmt.Errorf("%w: attempt timeout must be non-negative and no greater than total timeout", ErrInvalidPolicy)
	case config.CleanupTimeout <= 0:
		return nil, fmt.Errorf("%w: cleanup timeout must be positive", ErrInvalidPolicy)
	case nilLike(config.Clock):
		return nil, fmt.Errorf("%w: clock is required", ErrInvalidPolicy)
	case nilLike(config.Budget):
		return nil, fmt.Errorf("%w: budget is required", ErrInvalidPolicy)
	case nilLike(config.Classifier):
		return nil, fmt.Errorf("%w: classifier is required", ErrInvalidPolicy)
	case nilLike(config.Disposer):
		return nil, fmt.Errorf("%w: disposer is required", ErrInvalidPolicy)
	case config.Resource == "" || len(config.Resource) > MaxResourceLength:
		return nil, fmt.Errorf("%w: bounded resource identity is required", ErrInvalidPolicy)
	case config.FactoryFailureMode != FactoryFailureStop && config.FactoryFailureMode != FactoryFailureContinue:
		return nil, fmt.Errorf("%w: factory failure mode is required", ErrInvalidPolicy)
	}
	capacity := safeBudgetCapacity(config.Budget)
	if capacity == 0 || capacity > MaxBudgetCapacity {
		return nil, fmt.Errorf("%w: budget capacity must be between 1 and %d", ErrInvalidPolicy, MaxBudgetCapacity)
	}

	if len(config.Schedule) != 0 {
		if uint(len(config.Schedule)) != config.MaxHedges {
			return nil, fmt.Errorf("%w: schedule must define every hedge delay", ErrInvalidPolicy)
		}
		for _, delay := range config.Schedule {
			if delay <= 0 {
				return nil, fmt.Errorf("%w: scheduled delays must be positive", ErrInvalidPolicy)
			}
		}
		config.Schedule = append([]time.Duration(nil), config.Schedule...)
	}
	if config.Observer != nil && nilLike(config.Observer) {
		config.Observer = nil
	}

	return &Policy[T]{config: config}, nil
}

func safeBudgetCapacity(budget Budget) (capacity uint) {
	defer func() {
		if recover() != nil {
			capacity = 0
		}
	}()
	return budget.Capacity()
}

func nilLike(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() { //nolint:exhaustive // Only nil-capable kinds require distinct handling.
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
