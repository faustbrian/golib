package analysistestkit

import (
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestShippedAnalyzersOfferNoUnprovenSuggestedFixes(t *testing.T) {
	t.Parallel()

	pass, err := buildPass(
		representativeSource +
			"\nfunc suggestedFixTrigger() { fmt.Println(\"fixture\") }\n",
	)
	if err != nil {
		t.Fatalf("buildPass() error = %v", err)
	}
	analyzers := configuredAnalyzers(t)
	prepareRequirements(t, pass, analyzers)
	for _, analyzer := range analyzers {
		analyzer := analyzer
		t.Run(analyzer.Name, func(t *testing.T) {
			diagnosticCount := 0
			pass.Report = func(diagnostic analysis.Diagnostic) {
				diagnosticCount++
				if len(diagnostic.SuggestedFixes) != 0 {
					t.Errorf(
						"diagnostic at %s offers %d unproven suggested fixes",
						pass.Fset.Position(diagnostic.Pos),
						len(diagnostic.SuggestedFixes),
					)
				}
			}
			pass.Analyzer = analyzer
			if _, err := analyzer.Run(pass); err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if diagnosticCount == 0 {
				t.Fatal("representative source emitted no diagnostic")
			}
		})
	}
}
