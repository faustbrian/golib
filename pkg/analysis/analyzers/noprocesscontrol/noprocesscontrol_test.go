package noprocesscontrol_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/analysis/analyzers/noprocesscontrol"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()

	analysistest.Run(
		t,
		analysistest.TestData(),
		noprocesscontrol.Analyzer,
		"processuse",
	)
}

func TestAnalyzerAllowsApprovedEntrypoint(t *testing.T) {
	t.Parallel()

	analyzer := noprocesscontrol.New(noprocesscontrol.Options{
		AllowedPackages: []string{"approved"},
	})
	analysistest.Run(t, analysistest.TestData(), analyzer, "approved")
}
