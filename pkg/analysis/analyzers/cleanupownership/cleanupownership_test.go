package cleanupownership_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/analysis/analyzers/cleanupownership"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()

	analyzer, err := cleanupownership.New(cleanupownership.Options{
		Constructors: []cleanupownership.Constructor{
			{Package: "resourceapi", Symbol: "Open", CleanupResult: 1},
			{Package: "resourceapi", Symbol: "OpenFor", CleanupResult: 1},
			{Package: "resourceapi", Symbol: "Manager.Open", CleanupResult: 1},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysistest.Run(t, analysistest.TestData(), analyzer, "consumer")
}

func TestAnalyzerAllowsReviewedPackages(t *testing.T) {
	t.Parallel()

	analyzer, err := cleanupownership.New(cleanupownership.Options{
		Constructors: []cleanupownership.Constructor{{
			Package:         "resourceapi",
			Symbol:          "Open",
			CleanupResult:   1,
			AllowedPackages: []string{"approved"},
		}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysistest.Run(t, analysistest.TestData(), analyzer, "approved")
}

func TestAnalyzerIgnoresUnconfiguredConstructors(t *testing.T) {
	t.Parallel()

	analysistest.Run(
		t,
		analysistest.TestData(),
		cleanupownership.Analyzer,
		"approved",
	)
}

func TestNewRejectsMalformedPolicy(t *testing.T) {
	t.Parallel()

	tests := []cleanupownership.Options{
		{Constructors: []cleanupownership.Constructor{{Package: "resourceapi/*", Symbol: "Open", CleanupResult: 1}}},
		{Constructors: []cleanupownership.Constructor{{Package: "resourceapi", Symbol: "bad-name", CleanupResult: 1}}},
		{Constructors: []cleanupownership.Constructor{{Package: "resourceapi", Symbol: "A.B.C", CleanupResult: 1}}},
		{Constructors: []cleanupownership.Constructor{{Package: "resourceapi", Symbol: "Open", CleanupResult: -1}}},
		{Constructors: []cleanupownership.Constructor{{Package: "resourceapi", Symbol: "Open", CleanupResult: 1, AllowedPackages: []string{"approved/*"}}}},
		{Constructors: []cleanupownership.Constructor{{Package: "resourceapi", Symbol: "Open", CleanupResult: 1}, {Package: "resourceapi", Symbol: "Open", CleanupResult: 1}}},
	}
	for _, options := range tests {
		if _, err := cleanupownership.New(options); err == nil {
			t.Fatalf("New(%#v) error = nil", options)
		}
	}
}
