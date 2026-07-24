package policy_test

import (
	"reflect"
	"testing"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
	"github.com/faustbrian/golib/pkg/analysis/policy"
)

func TestRegistryRejectsUngovernedAndDuplicateRules(t *testing.T) {
	t.Parallel()

	entry := validEntry("security/no-unsafe")
	tests := map[string][]policy.Entry{
		"invalid metadata": {
			func() policy.Entry { value := entry; value.Rule.ID = "bad"; return value }(),
		},
		"missing owner": {
			func() policy.Entry { value := entry; value.Owner = ""; return value }(),
		},
		"duplicate ID": {entry, entry},
		"blocking without evidence": {
			func() policy.Entry {
				value := entry
				value.Rule.DefaultStatus = shared.StatusBlocking
				return value
			}(),
		},
		"missing canonical authority": {
			func() policy.Entry {
				value := entry
				value.Overlaps = []policy.Overlap{{Tool: "gosec"}}
				return value
			}(),
		},
		"missing overlap tool": {
			func() policy.Entry {
				value := entry
				value.Overlaps = []policy.Overlap{{
					CanonicalAuthority: "analysis",
				}}
				return value
			}(),
		},
		"duplicate overlap authority": {
			func() policy.Entry {
				value := entry
				value.Overlaps = []policy.Overlap{
					{
						Tool:               "Staticcheck/SA1019",
						CanonicalAuthority: "Staticcheck",
					},
					{
						Tool:               "Staticcheck/SA1019",
						CanonicalAuthority: "analysis",
					},
				}
				return value
			}(),
		},
	}
	for name, entries := range tests {
		entries := entries
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := policy.NewRegistry(entries); err == nil {
				t.Fatal("NewRegistry() error = nil, want governance rejection")
			}
		})
	}
}

func TestRegistryInventoryIsDeterministic(t *testing.T) {
	t.Parallel()

	registry, err := policy.NewRegistry([]policy.Entry{
		validEntry("security/no-unsafe"),
		validEntry("architecture/import-boundary"),
	})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	want := []string{"architecture/import-boundary", "security/no-unsafe"}
	if got := registry.IDs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("IDs() = %#v, want %#v", got, want)
	}
}

func validEntry(id string) policy.Entry {
	return policy.Entry{
		Rule: shared.Rule{
			ID:                id,
			Category:          shared.CategorySecurity,
			Severity:          shared.SeverityError,
			DefaultStatus:     shared.StatusAdvisory,
			Rationale:         "The operation bypasses safety policy.",
			Remediation:       "Use an approved safe API.",
			IntroducedVersion: "0.1.0",
		},
		Owner: "platform-security",
	}
}
