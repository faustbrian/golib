package competitors_test

import (
	"context"
	"testing"
	"time"

	golib "github.com/faustbrian/golib/pkg/event-sourcing"
	hallgren "github.com/hallgren/eventsourcing"
	hallgrenaggregate "github.com/hallgren/eventsourcing/aggregate"
	eventhorizon "github.com/looplab/eventhorizon"
	horizonevents "github.com/looplab/eventhorizon/aggregatestore/events"
	"github.com/looplab/eventhorizon/uuid"
	fabric "github.com/thefabric-io/eventsourcing"
)

const (
	aggregateType = "benchmark.counter"
	eventName     = "benchmark.counter.incremented"
)

var (
	fixedTime       = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	fixedHorizonID  = uuid.MustParse("018f47a0-2f5d-7d6c-8a9b-0123456789ab")
	benchmarkState  int
	benchmarkEvents int
)

type incremented struct {
	Amount int `json:"amount"`
}

func BenchmarkEquivalentRecordAndApply(benchmark *testing.B) {
	benchmarks := []struct {
		name string
		run  func() (int, int, error)
	}{
		{name: "golib", run: recordGolib},
		{name: "eventhorizon", run: recordEventHorizon},
		{name: "hallgren", run: recordHallgren},
		{name: "thefabric", run: recordTheFabric},
	}
	for _, candidate := range benchmarks {
		benchmark.Run(candidate.name, func(benchmark *testing.B) {
			benchmark.ReportAllocs()
			benchmark.ResetTimer()
			var err error
			for benchmark.Loop() {
				benchmarkState, benchmarkEvents, err = candidate.run()
			}
			benchmark.StopTimer()
			if err != nil {
				benchmark.Fatal(err)
			}
			if benchmarkState != 1 || benchmarkEvents != 1 {
				benchmark.Fatalf(
					"recorded outcome = state %d, events %d",
					benchmarkState,
					benchmarkEvents,
				)
			}
		})
	}
}

func TestEquivalentRecordAndApplyOutcomes(t *testing.T) {
	t.Parallel()
	for _, candidate := range []struct {
		name string
		run  func() (int, int, error)
	}{
		{name: "golib", run: recordGolib},
		{name: "eventhorizon", run: recordEventHorizon},
		{name: "hallgren", run: recordHallgren},
		{name: "thefabric", run: recordTheFabric},
	} {
		t.Run(candidate.name, func(t *testing.T) {
			state, events, err := candidate.run()
			if err != nil {
				t.Fatalf("record and apply: %v", err)
			}
			if state != 1 || events != 1 {
				t.Fatalf("recorded outcome = state %d, events %d", state, events)
			}
		})
	}
}

func recordGolib() (int, int, error) {
	var lifecycle golib.Lifecycle
	state := 0
	event, err := golib.NewDecodedEvent(golib.DecodedEventInput{
		Name: eventName, Version: 1, Value: incremented{Amount: 1},
	})
	if err != nil {
		return 0, 0, err
	}
	if err := lifecycle.Record(event, func(event golib.DecodedEvent) error {
		state += event.Value().(incremented).Amount
		return nil
	}); err != nil {
		return 0, 0, err
	}
	changes, err := lifecycle.Changes()
	if err != nil {
		return 0, 0, err
	}
	return state, changes.Len(), nil
}

type horizonAggregate struct {
	*horizonevents.AggregateBase
	state int
}

func recordEventHorizon() (int, int, error) {
	aggregate := &horizonAggregate{
		AggregateBase: horizonevents.NewAggregateBase(aggregateType, fixedHorizonID),
	}
	event := aggregate.AppendEvent(
		eventName,
		&incremented{Amount: 1},
		fixedTime,
	)
	if err := aggregate.ApplyEvent(context.Background(), event); err != nil {
		return 0, 0, err
	}
	return aggregate.state, len(aggregate.UncommittedEvents()), nil
}

func (aggregate *horizonAggregate) ApplyEvent(
	_ context.Context,
	event eventhorizon.Event,
) error {
	aggregate.state += event.Data().(*incremented).Amount
	aggregate.SetAggregateVersion(aggregate.AggregateVersion() + 1)
	return nil
}

type hallgrenAggregate struct {
	hallgrenaggregate.Root
	state int
}

func recordHallgren() (int, int, error) {
	aggregate := &hallgrenAggregate{}
	if err := aggregate.SetID("benchmark-counter"); err != nil {
		return 0, 0, err
	}
	hallgrenaggregate.TrackChange(aggregate, &incremented{Amount: 1})
	return aggregate.state, len(aggregate.Events()), nil
}

func (aggregate *hallgrenAggregate) Register(register hallgrenaggregate.RegisterFunc) {
	register(&incremented{})
}

func (aggregate *hallgrenAggregate) Transition(event hallgren.Event) {
	aggregate.state += event.Data().(*incremented).Amount
}

type fabricState struct {
	Count int `json:"count"`
}

func (*fabricState) Type() string { return aggregateType }

func (*fabricState) Zero() fabric.AggregateState { return &fabricState{} }

type fabricIncremented struct {
	Amount int `json:"amount"`
}

func (*fabricIncremented) Type() string { return eventName }

func (event *fabricIncremented) Apply(aggregate *fabric.Aggregate[*fabricState]) {
	aggregate.State().Count += event.Amount
}

func recordTheFabric() (int, int, error) {
	aggregate := fabric.InitZeroAggregate[*fabricState](&fabricState{})
	event := fabric.NewEvent[*fabricState](&fabricIncremented{Amount: 1}, nil)
	event.Apply(aggregate)
	return aggregate.State().Count, len(aggregate.Changes()), nil
}
