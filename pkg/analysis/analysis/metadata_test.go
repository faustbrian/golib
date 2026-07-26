package analysis_test

import (
	"testing"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
)

func TestRuleValidateRequiresStableMetadata(t *testing.T) {
	t.Parallel()

	valid := shared.Rule{
		ID:                "architecture/import-boundary",
		Category:          shared.CategoryArchitecture,
		Severity:          shared.SeverityError,
		DefaultStatus:     shared.StatusAdvisory,
		Rationale:         "Layer boundaries prevent dependency inversion.",
		Remediation:       "Move the dependency behind an adapter.",
		IntroducedVersion: "0.1.0",
	}
	tests := map[string]func() shared.Rule{
		"missing ID": func() shared.Rule {
			rule := valid
			rule.ID = ""
			return rule
		},
		"unstable ID": func() shared.Rule {
			rule := valid
			rule.ID = "Architecture.Bad_Id"
			return rule
		},
		"unknown category": func() shared.Rule {
			rule := valid
			rule.Category = "style"
			return rule
		},
		"unknown severity": func() shared.Rule {
			rule := valid
			rule.Severity = "fatal"
			return rule
		},
		"unknown status": func() shared.Rule {
			rule := valid
			rule.DefaultStatus = "mandatory"
			return rule
		},
		"missing rationale": func() shared.Rule {
			rule := valid
			rule.Rationale = "  "
			return rule
		},
		"missing remediation": func() shared.Rule {
			rule := valid
			rule.Remediation = "\t"
			return rule
		},
		"invalid version": func() shared.Rule {
			rule := valid
			rule.IntroducedVersion = "next"
			return rule
		},
	}

	for name, makeRule := range tests {
		makeRule := makeRule
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rule := makeRule()
			if err := rule.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want metadata rejection")
			}
		})
	}
}

func TestRuleValidateAcceptsAllEnumValues(t *testing.T) {
	t.Parallel()

	categories := []shared.Category{
		shared.CategoryArchitecture,
		shared.CategoryContext,
		shared.CategoryHTTP,
		shared.CategoryLifecycle,
		shared.CategorySecurity,
		shared.CategoryAPI,
		shared.CategorySafety,
		shared.CategoryObservability,
	}
	severities := []shared.Severity{
		shared.SeverityInfo,
		shared.SeverityWarning,
		shared.SeverityError,
	}
	statuses := []shared.Status{
		shared.StatusDisabled,
		shared.StatusAdvisory,
		shared.StatusBlocking,
	}

	for _, category := range categories {
		for _, severity := range severities {
			for _, status := range statuses {
				rule := shared.Rule{
					ID:                "architecture/import-boundary",
					Category:          category,
					Severity:          severity,
					DefaultStatus:     status,
					Rationale:         "The policy has an explicit rationale.",
					Remediation:       "Apply the documented remediation.",
					IntroducedVersion: "1.0.0",
				}
				if err := rule.Validate(); err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
			}
		}
	}
}

func TestRuleValidateAcceptsCompleteMetadata(t *testing.T) {
	t.Parallel()

	rule := shared.Rule{
		ID:                "architecture/import-boundary",
		Category:          shared.CategoryArchitecture,
		Severity:          shared.SeverityError,
		DefaultStatus:     shared.StatusAdvisory,
		Rationale:         "Layer boundaries prevent dependency inversion.",
		Remediation:       "Move the dependency behind an adapter.",
		IntroducedVersion: "0.1.0",
		Configuration: shared.ConfigurationSchema{
			Properties: map[string]shared.ConfigurationProperty{
				"allowed": {Type: shared.ConfigurationArray},
			},
		},
	}

	if err := rule.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}
