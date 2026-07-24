package httpclienttimeout_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/analysis/analyzers/httpclienttimeout"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()

	analysistest.Run(
		t,
		analysistest.TestData(),
		httpclienttimeout.Analyzer,
		"httptimeout",
	)
}

func TestAnalyzerAllowsReviewedPackages(t *testing.T) {
	t.Parallel()

	analyzer, err := httpclienttimeout.New(httpclienttimeout.Options{
		AllowedPackages: []string{"approved"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysistest.Run(t, analysistest.TestData(), analyzer, "approved")
}

func TestNewRejectsMalformedPackageExceptions(t *testing.T) {
	t.Parallel()

	for _, packagePath := range []string{"", "approved/*", "approved/../other"} {
		if _, err := httpclienttimeout.New(httpclienttimeout.Options{
			AllowedPackages: []string{packagePath},
		}); err == nil {
			t.Fatalf("New(%q) error = nil", packagePath)
		}
	}
}
