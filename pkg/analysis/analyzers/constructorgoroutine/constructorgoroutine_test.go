package constructorgoroutine_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/analysis/analyzers/constructorgoroutine"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()

	analyzer, err := constructorgoroutine.New(constructorgoroutine.Options{
		Policies: []constructorgoroutine.Policy{{
			Package: "constructors",
			Symbols: []string{
				"Builder.Build",
				"NewBuildTagged",
				"NewConditional",
				"NewDeferred",
				"NewGenerated",
				"NewGeneric",
				"NewImmediate",
				"NewService",
				"NewStoredCallback",
			},
		}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysistest.Run(t, analysistest.TestData(), analyzer, "constructors")
}

func TestAnalyzerIgnoresUnconfiguredPackages(t *testing.T) {
	t.Parallel()

	analysistest.Run(
		t,
		analysistest.TestData(),
		constructorgoroutine.Analyzer,
		"unconfigured",
	)
}

func TestNewRejectsMalformedPolicies(t *testing.T) {
	t.Parallel()

	tests := []constructorgoroutine.Policy{
		{Package: "constructors/*", Symbols: []string{"New"}},
		{Package: "constructors"},
		{Package: "constructors", Symbols: []string{"new-value"}},
		{Package: "constructors", Symbols: []string{"Type.bad-name"}},
		{Package: "constructors", Symbols: []string{"A.B.C"}},
	}
	for _, policy := range tests {
		if _, err := constructorgoroutine.New(constructorgoroutine.Options{
			Policies: []constructorgoroutine.Policy{policy},
		}); err == nil {
			t.Fatalf("New(%#v) error = nil", policy)
		}
	}
	duplicate := constructorgoroutine.Policy{
		Package: "constructors",
		Symbols: []string{"New", "New"},
	}
	if _, err := constructorgoroutine.New(constructorgoroutine.Options{
		Policies: []constructorgoroutine.Policy{duplicate},
	}); err == nil {
		t.Fatal("New() accepted a duplicate constructor")
	}
}
