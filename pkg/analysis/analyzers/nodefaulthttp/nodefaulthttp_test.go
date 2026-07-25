package nodefaulthttp_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/analysis/analyzers/nodefaulthttp"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()

	analysistest.Run(
		t,
		analysistest.TestData(),
		nodefaulthttp.Analyzer,
		"httpdefaults",
		"httpother",
	)
}
