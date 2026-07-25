package eventtest_test

import (
	"errors"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/eventtest"
)

type account struct {
	owner     string
	closed    bool
	lifecycle eventsourcing.Lifecycle
}

type accountOpened struct {
	Owner string
}

type accountClosed struct{}

func TestScenarioStagesHistoryAndReportsBehavior(t *testing.T) {
	t.Parallel()

	scenario := newAccountScenario(t)
	opened := decodedEvent(t, "account.opened", accountOpened{Owner: "Ada"})
	given, err := scenario.Given(opened)
	if err != nil {
		t.Fatal(err)
	}

	result := given.When(func(aggregate *account) error {
		return aggregate.lifecycle.Record(
			decodedEvent(t, "account.closed", accountClosed{}),
			aggregate.apply,
		)
	})
	if result.Error() != nil || result.Panicked() || !result.ActionRan() {
		t.Fatalf(
			"result = error %v, panicked %v, action ran %v",
			result.Error(),
			result.Panicked(),
			result.ActionRan(),
		)
	}
	if result.Aggregate().owner != "Ada" || !result.Aggregate().closed {
		t.Fatalf("aggregate = %#v", result.Aggregate())
	}
	if result.CommittedVersion() != 1 || result.Version() != 2 {
		t.Fatalf(
			"versions = committed %d current %d",
			result.CommittedVersion(),
			result.Version(),
		)
	}
	events := result.Events()
	if len(events) != 1 ||
		events[0].Name().String() != "account.closed" {
		t.Fatalf("events = %#v", events)
	}
	events[0] = eventsourcing.DecodedEvent{}
	if result.Events()[0].IsZero() {
		t.Fatal("Events() aliases result storage")
	}
}

func TestScenarioSupportsNoHistoryNoEventsAndBehaviorErrors(t *testing.T) {
	t.Parallel()

	scenario := newAccountScenario(t).GivenNone()
	behaviorFailure := errors.New("behavior failed")
	result := scenario.When(func(*account) error {
		return behaviorFailure
	})
	if !errors.Is(result.Error(), behaviorFailure) ||
		len(result.Events()) != 0 ||
		result.CommittedVersion() != 0 ||
		result.Version() != 0 {
		t.Fatalf("result = %#v", result)
	}

	reconstituted := scenario.Reconstitute()
	if reconstituted.Error() != nil ||
		reconstituted.ActionRan() ||
		len(reconstituted.Events()) != 0 {
		t.Fatalf("reconstitution = %#v", reconstituted)
	}
}

func TestScenarioSupportsExplicitSplitHistory(t *testing.T) {
	t.Parallel()

	first, err := eventsourcing.NewHistoricalEvent(eventsourcing.HistoricalEventInput{
		SourceVersion: 1,
		SegmentIndex:  0,
		SegmentCount:  2,
		Event: decodedEvent(
			t,
			"account.opened",
			accountOpened{Owner: "Ada"},
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := eventsourcing.NewHistoricalEvent(eventsourcing.HistoricalEventInput{
		SourceVersion: 1,
		SegmentIndex:  1,
		SegmentCount:  2,
		Event:         decodedEvent(t, "account.closed", accountClosed{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	scenario, err := newAccountScenario(t).GivenHistory(first, second)
	if err != nil {
		t.Fatal(err)
	}

	result := scenario.Reconstitute()
	if result.Error() != nil ||
		result.CommittedVersion() != 1 ||
		result.Aggregate().owner != "Ada" ||
		!result.Aggregate().closed {
		t.Fatalf("result = %#v, error %v", result, result.Error())
	}
}

func TestScenarioReportsCorruptHistoryWithoutRunningBehavior(t *testing.T) {
	t.Parallel()

	historical, err := eventsourcing.NewHistoricalEvent(
		eventsourcing.HistoricalEventInput{
			SourceVersion: 2,
			SegmentIndex:  0,
			SegmentCount:  1,
			Event: decodedEvent(
				t,
				"account.opened",
				accountOpened{Owner: "Ada"},
			),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	scenario, err := newAccountScenario(t).GivenHistory(historical)
	if err != nil {
		t.Fatal(err)
	}
	result := scenario.When(func(*account) error {
		t.Fatal("behavior ran after corrupt history")

		return nil
	})
	if !errors.Is(result.Error(), eventsourcing.ErrCorruptHistory) ||
		result.ActionRan() {
		t.Fatalf("result = %#v, error %v", result, result.Error())
	}
}

func TestScenarioPanicPolicyIsExplicit(t *testing.T) {
	t.Parallel()

	scenario := newAccountScenario(t)
	const panicValue = "behavior panic"
	captured := scenario.WhenCapturingPanic(func(*account) error {
		panic(panicValue)
	})
	value, panicked := captured.Panic()
	if !panicked ||
		value != panicValue ||
		captured.Error() != nil ||
		!captured.ActionRan() {
		t.Fatalf(
			"captured = value %v, panicked %v, error %v, action ran %v",
			value,
			panicked,
			captured.Error(),
			captured.ActionRan(),
		)
	}

	defer func() {
		if recover() != panicValue {
			t.Fatal("When() did not propagate the behavior panic")
		}
	}()
	scenario.When(func(*account) error {
		panic(panicValue)
	})
}

func TestScenarioValidatesConfigurationAndCalls(t *testing.T) {
	t.Parallel()

	complete := eventtest.AggregateConfig[*account]{
		New: func() (*account, error) { return &account{}, nil },
		Lifecycle: func(aggregate *account) *eventsourcing.Lifecycle {
			return &aggregate.lifecycle
		},
		Apply: func(aggregate *account, event eventsourcing.DecodedEvent) error {
			return aggregate.apply(event)
		},
	}
	cases := map[string]eventtest.AggregateConfig[*account]{
		"factory":   complete,
		"lifecycle": complete,
		"apply":     complete,
	}
	factory := cases["factory"]
	factory.New = nil
	cases["factory"] = factory
	lifecycle := cases["lifecycle"]
	lifecycle.Lifecycle = nil
	cases["lifecycle"] = lifecycle
	apply := cases["apply"]
	apply.Apply = nil
	cases["apply"] = apply
	for name, config := range cases {
		config := config
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := eventtest.NewScenario(config); !errors.Is(
				err,
				eventsourcing.ErrInvalidArgument,
			) {
				t.Fatalf("NewScenario() error = %v", err)
			}
		})
	}

	scenario := newAccountScenario(t)
	if _, err := scenario.Given(eventsourcing.DecodedEvent{}); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("Given() error = %v", err)
	}
	if result := scenario.When(nil); !errors.Is(
		result.Error(),
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("When(nil) error = %v", result.Error())
	}
}

func TestScenarioReportsFactoryLifecycleAndApplyFailures(t *testing.T) {
	t.Parallel()

	factoryFailure := errors.New("factory failed")
	factoryScenario, err := eventtest.NewScenario(
		eventtest.AggregateConfig[*account]{
			New: func() (*account, error) {
				return nil, factoryFailure
			},
			Lifecycle: func(aggregate *account) *eventsourcing.Lifecycle {
				return &aggregate.lifecycle
			},
			Apply: func(aggregate *account, event eventsourcing.DecodedEvent) error {
				return aggregate.apply(event)
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result := factoryScenario.Reconstitute(); !errors.Is(
		result.Error(),
		factoryFailure,
	) {
		t.Fatalf("factory result error = %v", result.Error())
	}

	nilLifecycle, err := eventtest.NewScenario(
		eventtest.AggregateConfig[*account]{
			New: func() (*account, error) { return &account{}, nil },
			Lifecycle: func(*account) *eventsourcing.Lifecycle {
				return nil
			},
			Apply: func(aggregate *account, event eventsourcing.DecodedEvent) error {
				return aggregate.apply(event)
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result := nilLifecycle.Reconstitute(); !errors.Is(
		result.Error(),
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("nil lifecycle result error = %v", result.Error())
	}

	applyFailure := errors.New("historical apply failed")
	failingApply, err := eventtest.NewScenario(
		eventtest.AggregateConfig[*account]{
			New: func() (*account, error) { return &account{}, nil },
			Lifecycle: func(aggregate *account) *eventsourcing.Lifecycle {
				return &aggregate.lifecycle
			},
			Apply: func(*account, eventsourcing.DecodedEvent) error {
				return applyFailure
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	withHistory, err := failingApply.Given(
		decodedEvent(t, "account.opened", accountOpened{Owner: "Ada"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	result := withHistory.Reconstitute()
	if !errors.Is(result.Error(), applyFailure) ||
		result.CommittedVersion() != 0 ||
		result.Version() != 0 {
		t.Fatalf(
			"apply result = error %v, committed %d, version %d",
			result.Error(),
			result.CommittedVersion(),
			result.Version(),
		)
	}
}

func TestScenarioPreservesCompletedEventsBeforeBehaviorFailure(t *testing.T) {
	t.Parallel()

	scenario := newAccountScenario(t)
	behaviorFailure := errors.New("behavior failed")
	result := scenario.WhenCapturingPanic(func(aggregate *account) error {
		if err := aggregate.lifecycle.Record(
			decodedEvent(t, "account.closed", accountClosed{}),
			aggregate.apply,
		); err != nil {
			t.Fatal(err)
		}

		return behaviorFailure
	})
	if !errors.Is(result.Error(), behaviorFailure) ||
		result.Panicked() ||
		len(result.Events()) != 1 ||
		result.Version() != 1 {
		t.Fatalf("result = %#v, error %v", result, result.Error())
	}

	poisoned := scenario.When(func(aggregate *account) error {
		return aggregate.lifecycle.Record(
			decodedEvent(t, "account.closed", accountClosed{}),
			func(eventsourcing.DecodedEvent) error {
				return errors.New("apply failed")
			},
		)
	})
	if !errors.Is(poisoned.Error(), eventsourcing.ErrLifecyclePoisoned) ||
		len(poisoned.Events()) != 0 {
		t.Fatalf("poisoned result = %#v, error %v", poisoned, poisoned.Error())
	}
}

func TestScenarioNilAndBoundedSetupIsSafe(t *testing.T) {
	t.Parallel()

	var nilScenario *eventtest.Scenario[*account]
	if nilScenario.GivenNone() != nil {
		t.Fatal("nil GivenNone() returned a scenario")
	}
	if _, err := nilScenario.Given(); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("nil Given() error = %v", err)
	}
	if _, err := nilScenario.GivenHistory(); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("nil GivenHistory() error = %v", err)
	}
	if result := nilScenario.Reconstitute(); !errors.Is(
		result.Error(),
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("nil Reconstitute() error = %v", result.Error())
	}

	scenario := newAccountScenario(t)
	events := make([]eventsourcing.DecodedEvent, eventsourcing.MaxReadMessages+1)
	if _, err := scenario.Given(events...); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("Given(oversized) error = %v", err)
	}
	history := make(
		[]eventsourcing.HistoricalEvent,
		eventsourcing.MaxReadMessages+1,
	)
	if _, err := scenario.GivenHistory(history...); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("GivenHistory(oversized) error = %v", err)
	}
}

func newAccountScenario(t *testing.T) *eventtest.Scenario[*account] {
	t.Helper()

	scenario, err := eventtest.NewScenario(eventtest.AggregateConfig[*account]{
		New: func() (*account, error) { return &account{}, nil },
		Lifecycle: func(aggregate *account) *eventsourcing.Lifecycle {
			return &aggregate.lifecycle
		},
		Apply: func(aggregate *account, event eventsourcing.DecodedEvent) error {
			return aggregate.apply(event)
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	return scenario
}

func (aggregate *account) apply(event eventsourcing.DecodedEvent) error {
	switch value := event.Value().(type) {
	case accountOpened:
		aggregate.owner = value.Owner
	case accountClosed:
		aggregate.closed = true
	default:
		return errors.New("unknown event")
	}

	return nil
}

func decodedEvent(t *testing.T, name string, value any) eventsourcing.DecodedEvent {
	t.Helper()

	event, err := eventsourcing.NewDecodedEvent(eventsourcing.DecodedEventInput{
		Name:    name,
		Version: 1,
		Value:   value,
	})
	if err != nil {
		t.Fatal(err)
	}

	return event
}
