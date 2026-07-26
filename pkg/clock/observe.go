package clock

import (
	"context"
	"errors"
	"maps"
	"time"
)

const (
	// MaxObservationTags bounds labels attached to one observation.
	MaxObservationTags = 16
	// MaxObservationTagBytes bounds each tag key and value.
	MaxObservationTagBytes = 64
)

var (
	// ErrInvalidClock reports a nil clock passed to Observe.
	ErrInvalidClock = errors.New("clock: invalid clock")
	// ErrInvalidObserver reports a nil observer passed to Observe.
	ErrInvalidObserver = errors.New("clock: invalid observer")
	// ErrObservationTags reports tags outside the documented bounds.
	ErrObservationTags = errors.New("clock: invalid observation tags")
)

// Kind identifies an observed time resource.
type Kind string

const (
	// KindSleep identifies a context-aware sleep.
	KindSleep Kind = "sleep"
	// KindTimer identifies a one-shot channel timer.
	KindTimer Kind = "timer"
	// KindTicker identifies a periodic channel ticker.
	KindTicker Kind = "ticker"
	// KindCallback identifies an AfterFunc callback.
	KindCallback Kind = "callback"
)

// Outcome identifies an observed lifecycle transition.
type Outcome string

const (
	// OutcomeCreated reports successful resource creation.
	OutcomeCreated Outcome = "created"
	// OutcomeCompleted reports successful synchronous completion.
	OutcomeCompleted Outcome = "completed"
	// OutcomeCanceled reports context cancellation.
	OutcomeCanceled Outcome = "canceled"
	// OutcomeStopped reports a successful active-to-stopped transition.
	OutcomeStopped Outcome = "stopped"
	// OutcomeInactive reports an operation on an inactive resource.
	OutcomeInactive Outcome = "inactive"
	// OutcomeReset reports successful rescheduling.
	OutcomeReset Outcome = "reset"
	// OutcomeFired reports callback execution.
	OutcomeFired Outcome = "fired"
	// OutcomePanicked reports a callback panic without its payload.
	OutcomePanicked Outcome = "panicked"
	// OutcomeRejected reports validation or resource rejection.
	OutcomeRejected Outcome = "rejected"
)

// Observation contains bounded lifecycle metadata. It never contains callback
// functions, panic payloads, timestamps, contexts, or other sensitive values.
type Observation struct {
	Kind      Kind
	Outcome   Outcome
	Requested time.Duration
	Elapsed   time.Duration
	Tags      map[string]string
}

// Observer consumes lifecycle metadata. Implementations must return promptly;
// panics are isolated from clock behavior.
type Observer interface {
	Observe(Observation)
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(Observation)

// Observe calls function with observation.
func (function ObserverFunc) Observe(observation Observation) {
	function(observation)
}

// ObserveOption configures an observed clock.
type ObserveOption func(*observeConfig) error

type observeConfig struct {
	tags map[string]string
}

// WithTags attaches a bounded defensive copy of tags to every observation.
func WithTags(tags map[string]string) ObserveOption {
	return func(config *observeConfig) error {
		if len(tags) > MaxObservationTags {
			return ErrObservationTags
		}
		config.tags = make(map[string]string, len(tags))
		for key, value := range tags {
			if key == "" || len(key) > MaxObservationTagBytes || len(value) > MaxObservationTagBytes {
				return ErrObservationTags
			}
			config.tags[key] = value
		}
		return nil
	}
}

// Observe decorates a FullClock with bounded synchronous lifecycle hooks. It
// starts no goroutine and owns no exporter or global registry.
func Observe(base FullClock, observer Observer, options ...ObserveOption) (FullClock, error) {
	if base == nil {
		return nil, ErrInvalidClock
	}
	if observer == nil {
		return nil, ErrInvalidObserver
	}
	if function, ok := observer.(ObserverFunc); ok && function == nil {
		return nil, ErrInvalidObserver
	}
	configuration := observeConfig{}
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(&configuration); err != nil {
			return nil, err
		}
	}
	return &observedClock{base: base, observer: observer, tags: configuration.tags}, nil
}

type observedClock struct {
	base     FullClock
	observer Observer
	tags     map[string]string
}

func (clock *observedClock) Now() time.Time { return clock.base.Now() }
func (clock *observedClock) Since(start time.Time) time.Duration {
	return clock.base.Since(start)
}
func (clock *observedClock) Measure() func() time.Duration { return clock.base.Measure() }

func (clock *observedClock) Sleep(ctx context.Context, duration time.Duration) error {
	elapsed := clock.base.Measure()
	err := clock.base.Sleep(ctx, duration)
	outcome := OutcomeCompleted
	if err != nil {
		outcome = OutcomeCanceled
	}
	clock.report(Observation{Kind: KindSleep, Outcome: outcome, Requested: duration, Elapsed: elapsed()})
	return err
}

func (clock *observedClock) NewTimer(duration time.Duration) (Timer, error) {
	timer, err := clock.base.NewTimer(duration)
	if err != nil {
		clock.report(Observation{Kind: KindTimer, Outcome: OutcomeRejected, Requested: duration})
		return nil, err
	}
	clock.report(Observation{Kind: KindTimer, Outcome: OutcomeCreated, Requested: duration})
	return &observedTimer{Timer: timer, clock: clock}, nil
}

func (clock *observedClock) NewTicker(duration time.Duration) (Ticker, error) {
	ticker, err := clock.base.NewTicker(duration)
	if err != nil {
		clock.report(Observation{Kind: KindTicker, Outcome: OutcomeRejected, Requested: duration})
		return nil, err
	}
	clock.report(Observation{Kind: KindTicker, Outcome: OutcomeCreated, Requested: duration})
	return &observedTicker{Ticker: ticker, clock: clock}, nil
}

func (clock *observedClock) AfterFunc(duration time.Duration, function func()) (Callback, error) {
	if function == nil {
		clock.report(Observation{Kind: KindCallback, Outcome: OutcomeRejected, Requested: duration})
		return nil, ErrInvalidCallback
	}
	elapsed := clock.base.Measure()
	wrapper := func() {
		defer func() {
			if payload := recover(); payload != nil {
				clock.report(Observation{Kind: KindCallback, Outcome: OutcomePanicked, Requested: duration, Elapsed: elapsed()})
				panic(payload)
			}
			clock.report(Observation{Kind: KindCallback, Outcome: OutcomeFired, Requested: duration, Elapsed: elapsed()})
		}()
		function()
	}
	callback, err := clock.base.AfterFunc(duration, wrapper)
	if err != nil {
		clock.report(Observation{Kind: KindCallback, Outcome: OutcomeRejected, Requested: duration})
		return nil, err
	}
	clock.report(Observation{Kind: KindCallback, Outcome: OutcomeCreated, Requested: duration})
	return &observedCallback{Callback: callback, clock: clock}, nil
}

func (clock *observedClock) report(observation Observation) {
	observation.Tags = cloneTags(clock.tags)
	defer func() { _ = recover() }()
	clock.observer.Observe(observation)
}

func cloneTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	clone := make(map[string]string, len(tags))
	maps.Copy(clone, tags)
	return clone
}

type observedTimer struct {
	Timer
	clock *observedClock
}

func (timer *observedTimer) Stop() bool {
	stopped := timer.Timer.Stop()
	outcome := OutcomeInactive
	if stopped {
		outcome = OutcomeStopped
	}
	timer.clock.report(Observation{Kind: KindTimer, Outcome: outcome})
	return stopped
}

func (timer *observedTimer) Reset(duration time.Duration) (bool, error) {
	active, err := timer.Timer.Reset(duration)
	outcome := OutcomeReset
	if err != nil {
		outcome = OutcomeRejected
	}
	timer.clock.report(Observation{Kind: KindTimer, Outcome: outcome, Requested: duration})
	return active, err
}

type observedTicker struct {
	Ticker
	clock *observedClock
}

func (ticker *observedTicker) Stop() {
	ticker.Ticker.Stop()
	ticker.clock.report(Observation{Kind: KindTicker, Outcome: OutcomeStopped})
}

func (ticker *observedTicker) Reset(duration time.Duration) error {
	err := ticker.Ticker.Reset(duration)
	outcome := OutcomeReset
	if err != nil {
		outcome = OutcomeRejected
	}
	ticker.clock.report(Observation{Kind: KindTicker, Outcome: outcome, Requested: duration})
	return err
}

type observedCallback struct {
	Callback
	clock *observedClock
}

func (callback *observedCallback) Stop() bool {
	stopped := callback.Callback.Stop()
	outcome := OutcomeInactive
	if stopped {
		outcome = OutcomeStopped
	}
	callback.clock.report(Observation{Kind: KindCallback, Outcome: outcome})
	return stopped
}

func (callback *observedCallback) Reset(duration time.Duration) (bool, error) {
	active, err := callback.Callback.Reset(duration)
	outcome := OutcomeReset
	if err != nil {
		outcome = OutcomeRejected
	}
	callback.clock.report(Observation{Kind: KindCallback, Outcome: outcome, Requested: duration})
	return active, err
}
