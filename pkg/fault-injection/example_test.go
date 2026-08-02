package faultinject_test

import (
	"context"
	"errors"
	"fmt"

	faultinject "github.com/faustbrian/golib/pkg/fault-injection"
)

func ExampleRun() {
	errUnavailable := errors.New("dependency unavailable")
	injector, _ := faultinject.New(faultinject.Config{Rules: []faultinject.Rule{{
		ID:          "second-call",
		Scope:       faultinject.BoundaryFunction,
		Activation:  faultinject.Active,
		Maximum:     1,
		Terminal:    faultinject.Continue,
		Observation: faultinject.Suppress,
		Schedule:    faultinject.Nth(2),
		Faults: []faultinject.Fault{
			faultinject.ErrorFault(faultinject.PhaseBefore, errUnavailable),
		},
	}}})
	operation := func(context.Context) (string, error) { return "ok", nil }

	value, err := faultinject.Run(context.Background(), injector,
		faultinject.Metadata{Boundary: faultinject.BoundaryFunction}, operation)
	fmt.Println(value, err)
	value, err = faultinject.Run(context.Background(), injector,
		faultinject.Metadata{Boundary: faultinject.BoundaryFunction}, operation)
	fmt.Println(value, err)

	// Output:
	// ok <nil>
	//  dependency unavailable
}

func ExampleProbability() {
	rule := faultinject.Rule{
		ID:          "seeded",
		Scope:       faultinject.BoundaryFunction,
		Activation:  faultinject.Active,
		Maximum:     8,
		Terminal:    faultinject.Continue,
		Observation: faultinject.Suppress,
		Schedule:    faultinject.Probability(0xfeed, 1, 3),
		Faults: []faultinject.Fault{
			faultinject.CancelFault(faultinject.PhaseBefore),
		},
	}
	injector, _ := faultinject.New(faultinject.Config{Rules: []faultinject.Rule{rule}})
	for range 6 {
		fmt.Print(injector.Decide(faultinject.Metadata{Boundary: faultinject.BoundaryFunction}).Injected(), " ")
	}

	// Output: false true false false false true
}

func ExampleInjector_Reset() {
	rule := faultinject.Rule{
		ID: "once", Scope: faultinject.BoundaryFunction,
		Activation: faultinject.Active, Maximum: 1,
		Terminal: faultinject.Continue, Observation: faultinject.Suppress,
		Schedule: faultinject.Every(1),
		Faults:   []faultinject.Fault{faultinject.CancelFault(faultinject.PhaseBefore)},
	}
	injector, _ := faultinject.New(faultinject.Config{Rules: []faultinject.Rule{rule}})
	first := injector.Decide(faultinject.Metadata{Boundary: faultinject.BoundaryFunction})
	injector.Reset()
	second := injector.Decide(faultinject.Metadata{Boundary: faultinject.BoundaryFunction})
	fmt.Println(first.Generation(), second.Generation())

	// Output: 1 2
}
