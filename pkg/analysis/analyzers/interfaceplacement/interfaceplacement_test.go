package interfaceplacement_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/analysis/analyzers/interfaceplacement"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()

	analyzer, err := interfaceplacement.New(interfaceplacement.Options{
		Packages: []string{"interfaces/provider/..."},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysistest.Run(
		t,
		analysistest.TestData(),
		analyzer,
		"interfaces/provider",
		"interfaces/provider/child",
		"interfaces/consumer",
	)
}

func TestUnconfiguredAnalyzerIsInactive(t *testing.T) {
	t.Parallel()

	analysistest.Run(
		t,
		analysistest.TestData(),
		interfaceplacement.Analyzer,
		"interfaces/unconfigured",
	)
}

func TestNewRejectsAmbiguousPackagePatterns(t *testing.T) {
	t.Parallel()

	tests := map[string][]string{
		"empty":           {""},
		"glob":            {"interfaces/*"},
		"middle ellipsis": {"interfaces/.../provider"},
		"dirty":           {"interfaces/a/../provider"},
		"absolute":        {"/interfaces/provider"},
		"dot":             {"."},
		"overlap":         {"interfaces/provider/...", "interfaces/provider/child"},
	}
	for name, packages := range tests {
		if _, err := interfaceplacement.New(interfaceplacement.Options{
			Packages: packages,
		}); err == nil {
			t.Fatalf("New() accepted invalid %s policy", name)
		}
	}
}
