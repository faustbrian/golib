package blockingcontext_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/analysis/analyzers/blockingcontext"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()

	analyzer, err := blockingcontext.New(blockingcontext.Options{Policies: []blockingcontext.Policy{{
		Package:   "blockingapi",
		Functions: []string{"BuildTagged", "Client.Load", "Fetch", "Generated", "Store"},
	}}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysistest.Run(t, analysistest.TestData(), analyzer, "blockingapi", "otherapi")
}

func TestNewRejectsInvalidPolicy(t *testing.T) {
	t.Parallel()

	tests := []blockingcontext.Policy{
		{},
		{Package: "blockingapi"},
		{Package: "blockingapi", Functions: []string{"fetch"}},
		{Package: "blockingapi", Functions: []string{"Client.load"}},
	}
	for _, policy := range tests {
		if _, err := blockingcontext.New(blockingcontext.Options{
			Policies: []blockingcontext.Policy{policy},
		}); err == nil {
			t.Fatalf("New() accepted %#v", policy)
		}
	}
}
