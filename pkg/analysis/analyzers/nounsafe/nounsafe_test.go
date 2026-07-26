package nounsafe_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/analysis/analyzers/nounsafe"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()

	analysistest.Run(t, analysistest.TestData(), nounsafe.Analyzer, "safeuse", "unsafeuse")
}
