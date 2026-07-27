package competitors_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	golib "github.com/faustbrian/golib/pkg/event-sourcing"
	hallgren "github.com/hallgren/eventsourcing"
	hallgrenaggregate "github.com/hallgren/eventsourcing/aggregate"
	hallgrencore "github.com/hallgren/eventsourcing/core"
	hallgrenmemory "github.com/hallgren/eventsourcing/eventstore/memory"
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
	fixedTime        = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	fixedHorizonID   = uuid.MustParse("018f47a0-2f5d-7d6c-8a9b-0123456789ab")
	benchmarkState   int
	benchmarkEvents  int
	benchmarkVersion int
	registerHallgren sync.Once
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

func TestEquivalentReconstitutionOutcomes(t *testing.T) {
	for _, length := range []int{1, 10, 100, 1000} {
		t.Run(fmt.Sprintf("length_%d", length), func(t *testing.T) {
			for _, candidate := range reconstitutionCandidates(t, length) {
				t.Run(candidate.name, func(t *testing.T) {
					state, version, err := candidate.run()
					if err != nil {
						t.Fatalf("reconstitute: %v", err)
					}
					if state != length || version != length {
						t.Fatalf(
							"reconstituted outcome = state %d, version %d",
							state,
							version,
						)
					}
				})
			}
		})
	}
}

type reconstitutionCandidate struct {
	name string
	run  func() (int, int, error)
}

func BenchmarkEquivalentReconstitution(benchmark *testing.B) {
	for _, length := range []int{1, 10, 100, 1000} {
		candidates := reconstitutionCandidates(benchmark, length)
		benchmark.Run(fmt.Sprintf("length_%d", length), func(benchmark *testing.B) {
			for _, candidate := range candidates {
				benchmark.Run(candidate.name, func(benchmark *testing.B) {
					benchmark.ReportAllocs()
					benchmark.ResetTimer()
					var err error
					for benchmark.Loop() {
						benchmarkState, benchmarkVersion, err = candidate.run()
					}
					benchmark.StopTimer()
					if err != nil {
						benchmark.Fatal(err)
					}
					if benchmarkState != length || benchmarkVersion != length {
						benchmark.Fatalf(
							"reconstituted outcome = state %d, version %d",
							benchmarkState,
							benchmarkVersion,
						)
					}
				})
			}
		})
	}
}

func reconstitutionCandidates(
	t testing.TB,
	length int,
) []reconstitutionCandidate {
	t.Helper()

	return []reconstitutionCandidate{
		{name: "golib", run: prepareGolibReconstitution(t, length)},
		{
			name: "eventhorizon",
			run:  prepareEventHorizonReconstitution(length),
		},
		{name: "hallgren", run: prepareHallgrenReconstitution(t, length)},
		{name: "thefabric", run: prepareTheFabricReconstitution(length)},
	}
}

func prepareGolibReconstitution(
	t testing.TB,
	length int,
) func() (int, int, error) {
	t.Helper()
	decoded, err := golib.NewDecodedEvent(golib.DecodedEventInput{
		Name: eventName, Version: 1, Value: incremented{Amount: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	history := make([]golib.HistoricalEvent, length)
	for index := range history {
		history[index], err = golib.NewHistoricalEvent(
			golib.HistoricalEventInput{
				SourceVersion: uint64(index + 1),
				SegmentCount:  1,
				Event:         decoded,
			},
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	return func() (int, int, error) {
		var lifecycle golib.Lifecycle
		state := 0
		err := lifecycle.Reconstitute(
			0,
			history,
			func(event golib.DecodedEvent) error {
				state += event.Value().(incremented).Amount

				return nil
			},
		)

		return state, int(lifecycle.CommittedVersion()), err
	}
}

func prepareEventHorizonReconstitution(
	length int,
) func() (int, int, error) {
	history := make([]eventhorizon.Event, length)
	for index := range history {
		history[index] = eventhorizon.NewEvent(
			eventName,
			&incremented{Amount: 1},
			fixedTime,
			eventhorizon.ForAggregate(
				aggregateType,
				fixedHorizonID,
				index+1,
			),
		)
	}

	return func() (int, int, error) {
		aggregate := &horizonAggregate{
			AggregateBase: horizonevents.NewAggregateBase(
				aggregateType,
				fixedHorizonID,
			),
		}
		for _, event := range history {
			if err := aggregate.ApplyEvent(
				context.Background(),
				event,
			); err != nil {
				return 0, 0, err
			}
		}

		return aggregate.state, aggregate.AggregateVersion(), nil
	}
}

func prepareHallgrenReconstitution(
	t testing.TB,
	length int,
) func() (int, int, error) {
	t.Helper()
	registerHallgren.Do(func() {
		hallgrenaggregate.Register(&hallgrenAggregate{})
	})
	store := hallgrenmemory.Create()
	t.Cleanup(store.Close)
	data, err := json.Marshal(&incremented{Amount: 1})
	if err != nil {
		t.Fatal(err)
	}
	history := make([]hallgrencore.Event, length)
	for index := range history {
		history[index] = hallgrencore.Event{
			AggregateID:   "benchmark-counter",
			Version:       hallgrencore.Version(index + 1),
			AggregateType: "hallgrenAggregate",
			Timestamp:     fixedTime,
			Reason:        "incremented",
			Data:          data,
		}
	}
	if err := store.Save(history); err != nil {
		t.Fatal(err)
	}

	return func() (int, int, error) {
		aggregate := &hallgrenAggregate{}
		err := hallgrenaggregate.Load(
			context.Background(),
			store,
			"benchmark-counter",
			aggregate,
		)

		return aggregate.state, int(aggregate.Version()), err
	}
}

func prepareTheFabricReconstitution(
	length int,
) func() (int, int, error) {
	data := []byte(`{"amount":1}`)
	metadata := []byte(`{}`)
	ids := make([]string, length)
	for index := range ids {
		ids[index] = fmt.Sprintf("benchmark-event-%d", index+1)
	}

	return func() (int, int, error) {
		aggregate := fabric.InitZeroAggregate[*fabricState](&fabricState{})
		for index := range length {
			event, err := fabric.InitEvent[*fabricState](
				ids[index],
				fixedTime,
				&fabricIncremented{},
				nil,
				data,
				index+1,
				metadata,
			)
			if err != nil {
				return 0, 0, err
			}
			event.Apply(aggregate)
		}

		return aggregate.State().Count, aggregate.Version(), nil
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
