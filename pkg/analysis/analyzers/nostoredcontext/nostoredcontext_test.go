package nostoredcontext_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/analysis/analyzers/nostoredcontext"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()

	analysistest.Run(
		t,
		analysistest.TestData(),
		nostoredcontext.Analyzer,
		"contextstore",
		"nocontext",
	)
}

func TestAnalyzerAllowsConfiguredOwner(t *testing.T) {
	t.Parallel()

	analyzer := nostoredcontext.New(nostoredcontext.Options{
		AllowedPackages: []string{"contextowner"},
	})
	analysistest.Run(t, analysistest.TestData(), analyzer, "contextowner")
}
