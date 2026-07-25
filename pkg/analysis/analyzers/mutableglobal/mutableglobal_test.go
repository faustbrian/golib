package mutableglobal_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/analysis/analyzers/mutableglobal"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()

	analyzer, err := mutableglobal.New(mutableglobal.Options{Policies: []mutableglobal.Policy{{
		Package: "mutableglobals/...",
	}}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysistest.Run(
		t,
		analysistest.TestData(),
		analyzer,
		"mutableglobals",
		"otherglobals",
	)
}

func TestUnconfiguredAnalyzerIsInactive(t *testing.T) {
	t.Parallel()

	analysistest.Run(t, analysistest.TestData(), mutableglobal.Analyzer, "unconfigured")
}

func TestNewRejectsAmbiguousPolicy(t *testing.T) {
	t.Parallel()

	tests := map[string]mutableglobal.Options{
		"empty package":   {Policies: []mutableglobal.Policy{{Package: ""}}},
		"glob package":    {Policies: []mutableglobal.Policy{{Package: "example.com/*"}}},
		"middle ellipsis": {Policies: []mutableglobal.Policy{{Package: "example.com/.../service"}}},
		"dirty package":   {Policies: []mutableglobal.Policy{{Package: "example.com/a/../service"}}},
		"absolute package": {Policies: []mutableglobal.Policy{{
			Package: "/example.com/service",
		}}},
		"dot package": {Policies: []mutableglobal.Policy{{Package: "."}}},
		"overlapping packages": {Policies: []mutableglobal.Policy{
			{Package: "example.com/service/..."},
			{Package: "example.com/service/internal"},
		}},
	}
	for name, options := range tests {
		if _, err := mutableglobal.New(options); err == nil {
			t.Fatalf("New() accepted invalid %s policy", name)
		}
	}
}
