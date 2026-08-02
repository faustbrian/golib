package faultinject_test

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	faultinject "github.com/faustbrian/golib/pkg/fault-injection"
)

var errInjected = errors.New("injected")

func TestZeroInjectorIsDisabledAndDoesNotAllocate(t *testing.T) {
	var injector faultinject.Injector
	metadata := faultinject.Metadata{Boundary: faultinject.BoundaryFunction}

	if decision := injector.Decide(metadata); decision.Injected() {
		t.Fatal("zero injector selected a fault")
	}
	if allocations := testing.AllocsPerRun(1000, func() { injector.Decide(metadata) }); allocations != 0 {
		t.Fatalf("disabled Decide allocated %v times per call", allocations)
	}

	snapshot := injector.Snapshot()
	if snapshot.Generation != 0 || snapshot.Evaluations != 0 || snapshot.Injections != 0 {
		t.Fatalf("zero injector snapshot = %+v", snapshot)
	}
}

func TestDeterministicSchedulesComposeByOrderAndStop(t *testing.T) {
	t.Parallel()

	injector, err := faultinject.New(faultinject.Config{Rules: []faultinject.Rule{
		{
			ID:          "later",
			Scope:       faultinject.BoundaryFunction,
			Activation:  faultinject.Active,
			Maximum:     3,
			Terminal:    faultinject.Continue,
			Order:       20,
			Observation: faultinject.Observe,
			Schedule:    faultinject.Every(1),
			Faults:      []faultinject.Fault{faultinject.ErrorFault(faultinject.PhaseAfter, errInjected)},
		},
		{
			ID:          "first",
			Scope:       faultinject.BoundaryFunction,
			Activation:  faultinject.Active,
			Maximum:     2,
			Terminal:    faultinject.Continue,
			Order:       10,
			Observation: faultinject.Observe,
			Schedule:    faultinject.Nth(2),
			Faults:      []faultinject.Fault{faultinject.LatencyFault(faultinject.PhaseBefore, time.Millisecond)},
		},
		{
			ID:          "stop",
			Scope:       faultinject.BoundaryFunction,
			Activation:  faultinject.Active,
			Maximum:     1,
			Terminal:    faultinject.Stop,
			Order:       15,
			Observation: faultinject.Observe,
			Schedule:    faultinject.Sequence([]bool{false, true}, false),
			Faults:      []faultinject.Fault{faultinject.CancelFault(faultinject.PhaseDuring)},
		},
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	metadata := faultinject.Metadata{Boundary: faultinject.BoundaryFunction, Operation: 7}
	first := injector.Decide(metadata)
	second := injector.Decide(metadata)
	third := injector.Decide(metadata)

	if got := kinds(first.Faults()); !reflect.DeepEqual(got, []faultinject.Kind{faultinject.KindError}) {
		t.Fatalf("first decision kinds = %v", got)
	}
	if got := kinds(second.Faults()); !reflect.DeepEqual(got, []faultinject.Kind{faultinject.KindLatency, faultinject.KindCancel}) {
		t.Fatalf("second decision kinds = %v", got)
	}
	if got := kinds(third.Faults()); !reflect.DeepEqual(got, []faultinject.Kind{faultinject.KindError}) {
		t.Fatalf("third decision kinds = %v", got)
	}
	if first.Sequence() != 1 || second.Sequence() != 2 || third.Sequence() != 3 {
		t.Fatalf("decision sequences = %d, %d, %d", first.Sequence(), second.Sequence(), third.Sequence())
	}

	snapshot := injector.Snapshot()
	if snapshot.Evaluations != 3 || snapshot.Injections != 4 {
		t.Fatalf("snapshot totals = %+v", snapshot)
	}
	wantRules := []faultinject.RuleSnapshot{
		{ID: "first", Calls: 3, Injections: 1},
		{ID: "stop", Calls: 3, Injections: 1},
		{ID: "later", Calls: 2, Injections: 2},
	}
	if !reflect.DeepEqual(snapshot.Rules, wantRules) {
		t.Fatalf("rule snapshots = %#v, want %#v", snapshot.Rules, wantRules)
	}
}

func TestSeededProbabilityAndSequenceAreReproducible(t *testing.T) {
	t.Parallel()

	configuration := faultinject.Config{Rules: []faultinject.Rule{{
		ID:          "seeded",
		Scope:       faultinject.BoundaryFunction,
		Activation:  faultinject.Active,
		Maximum:     64,
		Terminal:    faultinject.Continue,
		Observation: faultinject.Suppress,
		Schedule:    faultinject.Probability(0xfeed, 1, 3),
		Faults:      []faultinject.Fault{faultinject.ErrorFault(faultinject.PhaseBefore, errInjected)},
	}}}

	left, err := faultinject.New(configuration)
	if err != nil {
		t.Fatalf("New(left) error = %v", err)
	}
	right, err := faultinject.New(configuration)
	if err != nil {
		t.Fatalf("New(right) error = %v", err)
	}

	var leftSequence, rightSequence []bool
	for range 24 {
		leftSequence = append(leftSequence, left.Decide(faultinject.Metadata{Boundary: faultinject.BoundaryFunction}).Injected())
		rightSequence = append(rightSequence, right.Decide(faultinject.Metadata{Boundary: faultinject.BoundaryFunction}).Injected())
	}
	if !reflect.DeepEqual(leftSequence, rightSequence) {
		t.Fatalf("seeded decisions differ:\nleft  %v\nright %v", leftSequence, rightSequence)
	}
	want := []bool{false, true, false, false, false, true, false, false, true, false, true, false, false, false, true, false, false, false, false, true, false, false, false, false}
	if !reflect.DeepEqual(leftSequence, want) {
		t.Fatalf("seeded golden sequence = %v, want %v", leftSequence, want)
	}
}

func TestPredicateUsesBoundedMetadataAndRulesAreImmutable(t *testing.T) {
	t.Parallel()

	faults := []faultinject.Fault{faultinject.ErrorFault(faultinject.PhaseBefore, errInjected)}
	pattern := []bool{true}
	injector, err := faultinject.New(faultinject.Config{Rules: []faultinject.Rule{{
		ID:          "metadata",
		Scope:       faultinject.BoundaryHTTP,
		Activation:  faultinject.Active,
		Maximum:     1,
		Terminal:    faultinject.Continue,
		Observation: faultinject.Suppress,
		Schedule:    faultinject.Sequence(pattern, true),
		Predicate: func(metadata faultinject.Metadata) bool {
			return metadata.Operation == 42 && metadata.Attempt == 2
		},
		Faults: faults,
	}}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	pattern[0] = false
	faults[0] = faultinject.PanicFault(faultinject.PhaseBefore, "changed")
	if injector.Decide(faultinject.Metadata{Boundary: faultinject.BoundaryHTTP, Operation: 42, Attempt: 1}).Injected() {
		t.Fatal("predicate matched the wrong attempt")
	}
	decision := injector.Decide(faultinject.Metadata{Boundary: faultinject.BoundaryHTTP, Operation: 42, Attempt: 2})
	if got := kinds(decision.Faults()); !reflect.DeepEqual(got, []faultinject.Kind{faultinject.KindError}) {
		t.Fatalf("immutable decision kinds = %v", got)
	}
}

func TestResetIsGenerationSafeAndObserverRunsOutsideLocks(t *testing.T) {
	t.Parallel()

	clock := &fixedClock{now: time.Unix(123, 0)}
	var injector *faultinject.Injector
	var eventsMu sync.Mutex
	var events []faultinject.Event
	observer := faultinject.ObserverFunc(func(event faultinject.Event) {
		eventsMu.Lock()
		events = append(events, event)
		eventsMu.Unlock()
		_ = injector.Snapshot()
	})

	var err error
	injector, err = faultinject.New(faultinject.Config{
		Clock:    clock,
		Observer: observer,
		Rules: []faultinject.Rule{{
			ID:          "observed",
			Scope:       faultinject.BoundaryFunction,
			Activation:  faultinject.Active,
			Maximum:     1,
			Terminal:    faultinject.Continue,
			Observation: faultinject.Observe,
			Schedule:    faultinject.Every(1),
			Faults:      []faultinject.Fault{faultinject.ErrorFault(faultinject.PhaseBefore, errInjected)},
		}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	oldDecision := injector.Decide(faultinject.Metadata{Boundary: faultinject.BoundaryFunction})
	if generation := injector.Reset(); generation != 2 {
		t.Fatalf("Reset() generation = %d, want 2", generation)
	}
	newDecision := injector.Decide(faultinject.Metadata{Boundary: faultinject.BoundaryFunction})
	if oldDecision.Generation() != 1 || newDecision.Generation() != 2 {
		t.Fatalf("decision generations = %d and %d", oldDecision.Generation(), newDecision.Generation())
	}

	eventsMu.Lock()
	defer eventsMu.Unlock()
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	for index, event := range events {
		if event.RuleID != "observed" || event.Boundary != faultinject.BoundaryFunction || event.Kind != faultinject.KindError || event.At != clock.now {
			t.Fatalf("event %d = %+v", index, event)
		}
		if event.Generation != uint64(index+1) || event.SeedIdentity != 0 {
			t.Fatalf("event %d attribution = %+v", index, event)
		}
	}
}

func TestNewRejectsIncompleteOrUnboundedRules(t *testing.T) {
	t.Parallel()

	tests := map[string]faultinject.Config{
		"missing identity":   {Rules: []faultinject.Rule{{}}},
		"duplicate identity": {Rules: []faultinject.Rule{validRule("same"), validRule("same")}},
		"invalid identity": func() faultinject.Config {
			rule := validRule("contains secret whitespace")
			return faultinject.Config{Rules: []faultinject.Rule{rule}}
		}(),
		"missing scope": func() faultinject.Config {
			rule := validRule("scope")
			rule.Scope = ""
			return faultinject.Config{Rules: []faultinject.Rule{rule}}
		}(),
		"inactive enum": func() faultinject.Config {
			rule := validRule("activation")
			rule.Activation = 99
			return faultinject.Config{Rules: []faultinject.Rule{rule}}
		}(),
		"zero maximum": func() faultinject.Config {
			rule := validRule("maximum")
			rule.Maximum = 0
			return faultinject.Config{Rules: []faultinject.Rule{rule}}
		}(),
		"missing schedule": func() faultinject.Config {
			rule := validRule("schedule")
			rule.Schedule = nil
			return faultinject.Config{Rules: []faultinject.Rule{rule}}
		}(),
		"missing fault": func() faultinject.Config {
			rule := validRule("fault")
			rule.Faults = nil
			return faultinject.Config{Rules: []faultinject.Rule{rule}}
		}(),
		"invalid probability": func() faultinject.Config {
			rule := validRule("probability")
			rule.Schedule = faultinject.Probability(1, 2, 1)
			return faultinject.Config{Rules: []faultinject.Rule{rule}}
		}(),
		"unbounded latency": func() faultinject.Config {
			rule := validRule("latency")
			rule.Faults = []faultinject.Fault{faultinject.LatencyFault(faultinject.PhaseBefore, time.Hour)}
			return faultinject.Config{MaxLatency: time.Second, Rules: []faultinject.Rule{rule}}
		}(),
	}

	for name, configuration := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := faultinject.New(configuration); !errors.Is(err, faultinject.ErrInvalidConfig) {
				t.Fatalf("New() error = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func validRule(id string) faultinject.Rule {
	return faultinject.Rule{
		ID:          id,
		Scope:       faultinject.BoundaryFunction,
		Activation:  faultinject.Active,
		Maximum:     1,
		Terminal:    faultinject.Continue,
		Observation: faultinject.Suppress,
		Schedule:    faultinject.Every(1),
		Faults:      []faultinject.Fault{faultinject.ErrorFault(faultinject.PhaseBefore, errInjected)},
	}
}

func kinds(faults []faultinject.Fault) []faultinject.Kind {
	result := make([]faultinject.Kind, len(faults))
	for index := range faults {
		result[index] = faults[index].Kind
	}
	return result
}

type fixedClock struct{ now time.Time }

func (clock *fixedClock) Now() time.Time { return clock.now }
