package noinit_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/analysis/analyzers/noinit"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()

	analysistest.Run(t, analysistest.TestData(), noinit.Analyzer, "inituse")
}

func TestAnalyzerAllowsConfiguredPackage(t *testing.T) {
	t.Parallel()

	analyzer := noinit.New(noinit.Options{AllowedPackages: []string{"initroot"}})
	analysistest.Run(t, analysistest.TestData(), analyzer, "initroot")
}
