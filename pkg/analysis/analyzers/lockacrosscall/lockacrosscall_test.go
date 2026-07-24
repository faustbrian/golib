package lockacrosscall_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/analysis/analyzers/lockacrosscall"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()

	analyzer, err := lockacrosscall.New(lockacrosscall.Options{
		Calls: []lockacrosscall.Call{
			{Package: "ioapi", Symbol: "Call"},
			{Package: "ioapi", Symbol: "CallFor"},
			{Package: "ioapi", Symbol: "Client.Call"},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysistest.Run(t, analysistest.TestData(), analyzer, "consumer")
}

func TestAnalyzerAllowsReviewedCallerPackages(t *testing.T) {
	t.Parallel()

	analyzer, err := lockacrosscall.New(lockacrosscall.Options{
		Calls: []lockacrosscall.Call{{
			Package:         "ioapi",
			Symbol:          "Call",
			AllowedPackages: []string{"approved"},
		}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysistest.Run(t, analysistest.TestData(), analyzer, "approved")
}

func TestAnalyzerIgnoresUnconfiguredCalls(t *testing.T) {
	t.Parallel()

	analysistest.Run(t, analysistest.TestData(), lockacrosscall.Analyzer, "approved")
}

func TestNewRejectsMalformedPolicy(t *testing.T) {
	t.Parallel()

	tests := []lockacrosscall.Options{
		{Calls: []lockacrosscall.Call{{Package: "ioapi/*", Symbol: "Call"}}},
		{Calls: []lockacrosscall.Call{{Package: "ioapi", Symbol: "bad-name"}}},
		{Calls: []lockacrosscall.Call{{Package: "ioapi", Symbol: "A.B.C"}}},
		{Calls: []lockacrosscall.Call{{Package: "ioapi", Symbol: "Call", AllowedPackages: []string{"approved/*"}}}},
		{Calls: []lockacrosscall.Call{{Package: "ioapi", Symbol: "Call"}, {Package: "ioapi", Symbol: "Call"}}},
	}
	for _, options := range tests {
		if _, err := lockacrosscall.New(options); err == nil {
			t.Fatalf("New(%#v) error = nil", options)
		}
	}
}
