package nobackground_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/analysis/analyzers/nobackground"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()

	analysistest.Run(t, analysistest.TestData(), nobackground.Analyzer, "contextuse")
}

func TestAnalyzerAllowsCompositionRoot(t *testing.T) {
	t.Parallel()

	analyzer := nobackground.New(nobackground.Options{
		AllowedPackages: []string{"contextroot"},
	})
	analysistest.Run(t, analysistest.TestData(), analyzer, "contextroot")
}
