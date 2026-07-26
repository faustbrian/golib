// Package eventtest provides ordinary testing values for aggregate scenarios
// and reusable infrastructure conformance checks.
package eventtest

import (
	"errors"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

// AggregateConfig supplies the explicit aggregate boundaries used by a
// scenario.
type AggregateConfig[Aggregate any] struct {
	New       func() (Aggregate, error)
	Lifecycle func(Aggregate) *eventsourcing.Lifecycle
	Apply     func(Aggregate, eventsourcing.DecodedEvent) error
}

// Scenario is an immutable aggregate-history setup that may be reused across
// independent behavior runs.
type Scenario[Aggregate any] struct {
	config  AggregateConfig[Aggregate]
	history []eventsourcing.HistoricalEvent
}

// NewScenario validates aggregate construction and application callbacks.
func NewScenario[Aggregate any](
	config AggregateConfig[Aggregate],
) (*Scenario[Aggregate], error) {
	if config.New == nil || config.Lifecycle == nil || config.Apply == nil {
		return nil, eventsourcing.ErrInvalidArgument
	}

	return &Scenario[Aggregate]{config: config}, nil
}

// GivenNone returns an independent scenario with no committed history.
func (scenario *Scenario[Aggregate]) GivenNone() *Scenario[Aggregate] {
	if scenario == nil {
		return nil
	}

	return &Scenario[Aggregate]{config: scenario.config}
}

// Given returns an independent scenario where each event represents one
// consecutive stored stream version.
func (scenario *Scenario[Aggregate]) Given(
	events ...eventsourcing.DecodedEvent,
) (*Scenario[Aggregate], error) {
	if scenario == nil || len(events) > eventsourcing.MaxReadMessages {
		return nil, eventsourcing.ErrInvalidArgument
	}

	history := make([]eventsourcing.HistoricalEvent, len(events))
	for index, event := range events {
		if event.IsZero() {
			return nil, eventsourcing.ErrInvalidArgument
		}
		// The source and segment coordinates are constructed within the
		// validated ranges, and the event was checked above.
		historical, _ := eventsourcing.NewHistoricalEvent(
			eventsourcing.HistoricalEventInput{
				SourceVersion: uint64(index) + 1,
				SegmentIndex:  0,
				SegmentCount:  1,
				Event:         event,
			},
		)
		history[index] = historical
	}

	return &Scenario[Aggregate]{
		config:  scenario.config,
		history: history,
	}, nil
}

// GivenHistory returns an independent scenario with explicit source-version
// and split-event coordinates. Sequence validation occurs during execution so
// corrupt-history behavior can be asserted.
func (scenario *Scenario[Aggregate]) GivenHistory(
	history ...eventsourcing.HistoricalEvent,
) (*Scenario[Aggregate], error) {
	if scenario == nil || len(history) > eventsourcing.MaxReadMessages {
		return nil, eventsourcing.ErrInvalidArgument
	}

	return &Scenario[Aggregate]{
		config:  scenario.config,
		history: cloneHistory(history),
	}, nil
}

// Reconstitute loads the configured history without running domain behavior.
func (scenario *Scenario[Aggregate]) Reconstitute() Result[Aggregate] {
	return scenario.run(nil, false, false)
}

// When reconstitutes a fresh aggregate and runs behavior. Behavior panics
// propagate to preserve Go's default test semantics.
func (scenario *Scenario[Aggregate]) When(
	action func(Aggregate) error,
) Result[Aggregate] {
	return scenario.run(action, true, false)
}

// WhenCapturingPanic reconstitutes a fresh aggregate and captures a behavior
// panic as explicit result data.
func (scenario *Scenario[Aggregate]) WhenCapturingPanic(
	action func(Aggregate) error,
) Result[Aggregate] {
	return scenario.run(action, true, true)
}

func (scenario *Scenario[Aggregate]) run(
	action func(Aggregate) error,
	runAction bool,
	capturePanic bool,
) Result[Aggregate] {
	var result Result[Aggregate]
	if scenario == nil || (runAction && action == nil) {
		result.err = eventsourcing.ErrInvalidArgument

		return result
	}

	aggregate, err := scenario.config.New()
	result.aggregate = aggregate
	if err != nil {
		result.err = err

		return result
	}
	lifecycle := scenario.config.Lifecycle(aggregate)
	if lifecycle == nil {
		result.err = eventsourcing.ErrInvalidArgument

		return result
	}

	if err := lifecycle.Reconstitute(
		0,
		cloneHistory(scenario.history),
		func(event eventsourcing.DecodedEvent) error {
			return scenario.config.Apply(aggregate, event)
		},
	); err != nil {
		result.err = err
		result.captureVersions(lifecycle)

		return result
	}
	if runAction {
		result.actionRan = true
		if capturePanic {
			result.panicValue, result.panicked, result.err = capture(action, aggregate)
		} else {
			result.err = action(aggregate)
		}
	}

	changes, changesErr := lifecycle.Changes()
	result.err = errors.Join(result.err, changesErr)
	if changesErr == nil {
		result.events = changes.Events()
	}
	result.captureVersions(lifecycle)

	return result
}

// Result contains one scenario run without owning assertions or a test runner.
type Result[Aggregate any] struct {
	aggregate        Aggregate
	err              error
	panicValue       any
	events           []eventsourcing.DecodedEvent
	committedVersion uint64
	version          uint64
	panicked         bool
	actionRan        bool
}

// Aggregate returns the independently constructed aggregate.
func (result Result[Aggregate]) Aggregate() Aggregate {
	return result.aggregate
}

// Error returns reconstitution, behavior, or lifecycle-state failures.
func (result Result[Aggregate]) Error() error {
	return result.err
}

// Panic returns a captured behavior panic. Panics are captured only by
// WhenCapturingPanic.
func (result Result[Aggregate]) Panic() (any, bool) {
	return result.panicValue, result.panicked
}

// Panicked reports whether behavior produced a captured panic.
func (result Result[Aggregate]) Panicked() bool {
	return result.panicked
}

// ActionRan reports whether history reconstitution completed and behavior was
// invoked.
func (result Result[Aggregate]) ActionRan() bool {
	return result.actionRan
}

// Events returns a defensive copy of events recorded by behavior.
func (result Result[Aggregate]) Events() []eventsourcing.DecodedEvent {
	output := make([]eventsourcing.DecodedEvent, len(result.events))
	copy(output, result.events)

	return output
}

// CommittedVersion returns the aggregate version established by history.
func (result Result[Aggregate]) CommittedVersion() uint64 {
	return result.committedVersion
}

// Version returns the aggregate version including events recorded by behavior.
func (result Result[Aggregate]) Version() uint64 {
	return result.version
}

func (result *Result[Aggregate]) captureVersions(
	lifecycle *eventsourcing.Lifecycle,
) {
	result.committedVersion = lifecycle.CommittedVersion()
	result.version = lifecycle.Version()
}

func cloneHistory(
	history []eventsourcing.HistoricalEvent,
) []eventsourcing.HistoricalEvent {
	output := make([]eventsourcing.HistoricalEvent, len(history))
	copy(output, history)

	return output
}

func capture[Aggregate any](
	action func(Aggregate) error,
	aggregate Aggregate,
) (panicValue any, panicked bool, err error) {
	defer func() {
		panicValue = recover()
		panicked = panicValue != nil
	}()

	err = action(aggregate)

	return nil, false, err
}
