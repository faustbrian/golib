package faultinject

import (
	"context"
	"time"
)

// WrapSleeper returns a clock-boundary adapter around a caller-owned sleeper.
func WrapSleeper(base Sleeper, injector *Injector, operation uint32) (Sleeper, error) {
	if base == nil {
		return nil, invalid("Sleeper", "must be non-nil")
	}
	if injector == nil || !injector.enabled {
		return base, nil
	}
	return &injectedSleeper{base: base, injector: injector, operation: operation}, nil
}

type injectedSleeper struct {
	base      Sleeper
	injector  *Injector
	operation uint32
}

func (sleeper *injectedSleeper) Sleep(ctx context.Context, delay time.Duration) error {
	_, err := Run(ctx, sleeper.injector, Metadata{Boundary: BoundaryClock, Operation: sleeper.operation}, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, sleeper.base.Sleep(ctx, delay)
	})
	return err
}

// Timer exposes the standard timer lifecycle without exposing a concrete
// *time.Timer.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(time.Duration) bool
}

// TimerFactory creates caller-owned timers through a bounded context.
type TimerFactory interface {
	NewTimer(context.Context, time.Duration) (Timer, error)
}

// WrapTimerFactory injects clock-boundary creation faults. A timer rejected
// after construction is stopped before the injected error is returned.
func WrapTimerFactory(base TimerFactory, injector *Injector, operation uint32) (TimerFactory, error) {
	if base == nil {
		return nil, invalid("TimerFactory", "must be non-nil")
	}
	if injector == nil || !injector.enabled {
		return base, nil
	}
	return &injectedTimerFactory{base: base, injector: injector, operation: operation}, nil
}

type injectedTimerFactory struct {
	base      TimerFactory
	injector  *Injector
	operation uint32
}

func (factory *injectedTimerFactory) NewTimer(ctx context.Context, delay time.Duration) (Timer, error) {
	decision := factory.injector.Decide(Metadata{Boundary: BoundaryClock, Operation: factory.operation})
	if err := faultPhaseError(ctx, decision.faults, PhaseBefore, factory.injector.sleeper); err != nil {
		return nil, err
	}
	if err := faultPhaseError(ctx, decision.faults, PhaseDuring, factory.injector.sleeper); err != nil {
		return nil, err
	}
	timer, organicError := factory.base.NewTimer(ctx, delay)
	if err := faultPhaseError(ctx, decision.faults, PhaseAfter, factory.injector.sleeper); err != nil {
		if timer != nil {
			timer.Stop()
		}
		return nil, err
	}
	return timer, organicError
}
