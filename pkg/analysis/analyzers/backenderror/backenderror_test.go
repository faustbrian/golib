package backenderror_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/analysis/analyzers/backenderror"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()

	variadicFrom := 1
	variadicFromStart := 0
	analyzer, err := backenderror.New(backenderror.Options{
		Boundaries: []string{"boundary", "boundarytree/..."},
		Sources: []backenderror.Flow{
			{Package: "backend", Symbol: "Load", Result: 1},
			{Package: "backend", Symbol: "Save", Result: 0},
			{Package: "backend", Symbol: "Concrete", Result: 0},
			{Package: "backend", Symbol: "Client.Load", Result: 1},
			{Package: "backend", Symbol: "Generic", Result: 1},
		},
		Passthroughs: []backenderror.Passthrough{{
			Package: "fmt", Symbol: "Errorf", Result: 0,
			VariadicFrom: &variadicFrom,
		}, {
			Package: "wrapper", Symbol: "Wrapper.Wrap", Result: 0,
			Arguments: []int{0},
		}, {
			Package: "wrapper", Symbol: "Fixed", Result: 0,
			Arguments: []int{0},
		}, {
			Package: "wrapper", Symbol: "All", Result: 0,
			VariadicFrom: &variadicFromStart,
		}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysistest.Run(t, analysistest.TestData(), analyzer, "boundary", "boundarytree/child", "outside")
}

func TestAnalyzerBuildsRootSSAWithoutFactBearingPrerequisite(t *testing.T) {
	t.Parallel()

	analyzer, err := backenderror.New(backenderror.Options{
		Boundaries: []string{"boundary"},
		Sources: []backenderror.Flow{{
			Package: "backend",
			Symbol:  "Load",
			Result:  1,
		}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if len(analyzer.Requires) != 0 {
		t.Fatalf("Requires = %d, want root-only SSA construction", len(analyzer.Requires))
	}
}

func TestNewAcceptsZeroVariadicPosition(t *testing.T) {
	t.Parallel()

	zero := 0
	_, err := backenderror.New(backenderror.Options{
		Boundaries: []string{"boundary"},
		Sources:    []backenderror.Flow{{Package: "backend", Symbol: "Load", Result: 1}},
		Passthroughs: []backenderror.Passthrough{{
			Package: "wrapper", Symbol: "All", Result: 0, VariadicFrom: &zero,
		}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
}

func TestUnconfiguredAnalyzerIsInactive(t *testing.T) {
	t.Parallel()

	analysistest.Run(t, analysistest.TestData(), backenderror.Analyzer, "boundaryunconfigured")
}

func TestNewRejectsMalformedPolicy(t *testing.T) {
	t.Parallel()

	zero := 0
	negative := -1
	validSource := []backenderror.Flow{{Package: "backend", Symbol: "Load", Result: 1}}
	validBoundary := []string{"boundary"}
	tests := []backenderror.Options{
		{Boundaries: []string{"bad/*"}, Sources: validSource},
		{Boundaries: []string{"boundary/...", "boundary/child"}, Sources: validSource},
		{Boundaries: validBoundary, Sources: []backenderror.Flow{{Package: "bad/*", Symbol: "Load", Result: 1}}},
		{Boundaries: validBoundary, Sources: []backenderror.Flow{{Package: "backend", Symbol: "bad-name", Result: 1}}},
		{Boundaries: validBoundary, Sources: []backenderror.Flow{{Package: "backend", Symbol: "A.B.C", Result: 1}}},
		{Boundaries: validBoundary, Sources: []backenderror.Flow{{Package: "backend", Symbol: "Load", Result: -1}}},
		{Boundaries: validBoundary, Sources: append(validSource, validSource...)},
		{Boundaries: validBoundary, Sources: validSource, Passthroughs: []backenderror.Passthrough{{Package: "fmt", Symbol: "Errorf", Result: 0}}},
		{Boundaries: validBoundary, Sources: validSource, Passthroughs: []backenderror.Passthrough{{Package: "fmt", Symbol: "Errorf", Result: -1, Arguments: []int{1}}}},
		{Boundaries: validBoundary, Sources: validSource, Passthroughs: []backenderror.Passthrough{{Package: "fmt", Symbol: "Errorf", Result: 0, Arguments: []int{-1}}}},
		{Boundaries: validBoundary, Sources: validSource, Passthroughs: []backenderror.Passthrough{{Package: "fmt", Symbol: "Errorf", Result: 0, Arguments: []int{1, 1}}}},
		{Boundaries: validBoundary, Sources: validSource, Passthroughs: []backenderror.Passthrough{{Package: "fmt", Symbol: "Errorf", Result: 0, VariadicFrom: &negative}}},
		{Boundaries: validBoundary, Sources: validSource, Passthroughs: []backenderror.Passthrough{{Package: "fmt", Symbol: "Errorf", Result: 0, Arguments: []int{1}}, {Package: "fmt", Symbol: "Errorf", Result: 0, VariadicFrom: &zero}}},
	}
	for _, options := range tests {
		if _, err := backenderror.New(options); err == nil {
			t.Fatalf("New(%#v) error = nil", options)
		}
	}
}
