package goroutinefanout_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/analysis/analyzers/goroutinefanout"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()

	analyzer, err := goroutinefanout.New(goroutinefanout.Options{Policies: []goroutinefanout.Policy{
		{Package: "fanout", MaxStatic: 8},
		{Package: "fanouttree/...", MaxStatic: 8},
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysistest.Run(t, analysistest.TestData(), analyzer, "fanout", "fanouttree/child", "fanoutoutside")
}

func TestUnconfiguredAnalyzerIsInactive(t *testing.T) {
	t.Parallel()

	analysistest.Run(t, analysistest.TestData(), goroutinefanout.Analyzer, "fanoutunconfigured")
}

func TestNewRejectsMalformedPolicy(t *testing.T) {
	t.Parallel()

	tests := [][]goroutinefanout.Policy{
		{{Package: "", MaxStatic: 8}},
		{{Package: "bad/*", MaxStatic: 8}},
		{{Package: "fanout", MaxStatic: 0}},
		{{Package: "fanout", MaxStatic: -1}},
		{{Package: "fanout", MaxStatic: 1025}},
		{{Package: "fanout/...", MaxStatic: 8}, {Package: "fanout/child", MaxStatic: 8}},
	}
	for _, policies := range tests {
		if _, err := goroutinefanout.New(goroutinefanout.Options{Policies: policies}); err == nil {
			t.Fatalf("New(%#v) error = nil", policies)
		}
	}
}

func TestNewAcceptsLimitBoundaries(t *testing.T) {
	t.Parallel()

	for _, limit := range []int{1, 1024} {
		if _, err := goroutinefanout.New(goroutinefanout.Options{Policies: []goroutinefanout.Policy{{
			Package: "fanout", MaxStatic: limit,
		}}}); err != nil {
			t.Fatalf("New(max_static=%d) error = %v", limit, err)
		}
	}
}
