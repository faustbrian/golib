package match_test

import (
	"errors"
	"testing"

	language "github.com/faustbrian/golib/pkg/international/locale"
	localized "github.com/faustbrian/golib/pkg/localized"
	localizedmatch "github.com/faustbrian/golib/pkg/localized/match"
)

func TestPlanUsesLocaleParentsWithoutInventingEntries(t *testing.T) {
	t.Parallel()
	value, err := localized.TextFromMap(map[string]string{"zh-Hant": "您好", "en": "Hello"})
	if err != nil {
		t.Fatal(err)
	}
	requested := mustLocale(t, "zh-Hant-TW")
	plan, err := localizedmatch.NewPlan([]localizedmatch.Chain{{
		From:       requested,
		Candidates: []localizedmatch.Candidate{{Kind: localizedmatch.ParentRange, Locale: requested}},
	}}, localizedmatch.PlanOptions{MaxDepth: 4, MaxCandidates: 8})
	if err != nil {
		t.Fatal(err)
	}

	result := plan.Resolve(value, requested)
	if result.Kind != localizedmatch.Fallback || result.Locale != mustLocale(t, "zh-Hant") || result.Text != "您好" {
		t.Fatalf("Resolve() = %+v", result)
	}
	if value.Has(requested) || value.Len() != 2 {
		t.Fatalf("fallback materialized entry: %v", value.Locales())
	}
}

func TestPlanTraversesConfiguredChainsThenDefault(t *testing.T) {
	t.Parallel()
	value, _ := localized.TextFromMap(map[string]string{"en": "Hello"})
	swedish := mustLocale(t, "sv")
	finnish := mustLocale(t, "fi")
	english := mustLocale(t, "en")
	plan, err := localizedmatch.NewPlan([]localizedmatch.Chain{
		{From: swedish, Candidates: []localizedmatch.Candidate{{Kind: localizedmatch.ExactLocale, Locale: finnish}}},
		{From: finnish, Candidates: []localizedmatch.Candidate{{Kind: localizedmatch.ExactLocale, Locale: mustLocale(t, "de")}}},
	}, localizedmatch.PlanOptions{Default: &english, MaxDepth: 4, MaxCandidates: 8})
	if err != nil {
		t.Fatal(err)
	}
	result := plan.Resolve(value, swedish)
	if result.Kind != localizedmatch.Default || result.Locale != english {
		t.Fatalf("Resolve() = %+v", result)
	}
}

func TestPlanRejectsCyclesDuplicatesAndBounds(t *testing.T) {
	t.Parallel()
	en := mustLocale(t, "en")
	fi := mustLocale(t, "fi")
	tests := []struct {
		name    string
		chains  []localizedmatch.Chain
		options localizedmatch.PlanOptions
		want    error
	}{
		{"cycle", []localizedmatch.Chain{
			{From: en, Candidates: []localizedmatch.Candidate{{Kind: localizedmatch.ExactLocale, Locale: fi}}},
			{From: fi, Candidates: []localizedmatch.Candidate{{Kind: localizedmatch.ExactLocale, Locale: en}}},
		}, localizedmatch.PlanOptions{MaxDepth: 4, MaxCandidates: 8}, localizedmatch.ErrFallbackCycle},
		{"duplicate source", []localizedmatch.Chain{{From: en}, {From: en}}, localizedmatch.PlanOptions{MaxDepth: 4, MaxCandidates: 8}, localizedmatch.ErrDuplicateCandidate},
		{"duplicate candidate", []localizedmatch.Chain{{From: en, Candidates: []localizedmatch.Candidate{{Kind: localizedmatch.ExactLocale, Locale: fi}, {Kind: localizedmatch.ExactLocale, Locale: fi}}}}, localizedmatch.PlanOptions{MaxDepth: 4, MaxCandidates: 8}, localizedmatch.ErrDuplicateCandidate},
		{"candidate limit", []localizedmatch.Chain{{From: en, Candidates: []localizedmatch.Candidate{{Kind: localizedmatch.ExactLocale, Locale: fi}}}}, localizedmatch.PlanOptions{MaxDepth: 4, MaxCandidates: 0}, localizedmatch.ErrCandidateLimit},
		{"depth", []localizedmatch.Chain{{From: en}}, localizedmatch.PlanOptions{MaxDepth: 0, MaxCandidates: 8}, localizedmatch.ErrDepthLimit},
		{"candidate kind", []localizedmatch.Chain{{From: en, Candidates: []localizedmatch.Candidate{{Kind: localizedmatch.CandidateKind(99), Locale: fi}}}}, localizedmatch.PlanOptions{MaxDepth: 4, MaxCandidates: 8}, localizedmatch.ErrInvalidCandidate},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := localizedmatch.NewPlan(test.chains, test.options)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestPlanResolutionBoundaryMatrix(t *testing.T) {
	t.Parallel()
	zero := language.Tag{}
	if _, err := localizedmatch.NewPlan([]localizedmatch.Chain{{From: zero}}, localizedmatch.PlanOptions{MaxDepth: 1, MaxCandidates: 1}); !errors.Is(err, localizedmatch.ErrInvalidCandidate) {
		t.Fatalf("NewPlan(zero source) error = %v", err)
	}
	if _, err := localizedmatch.NewPlan([]localizedmatch.Chain{{From: mustLocale(t, "en"), Candidates: []localizedmatch.Candidate{{Locale: zero}}}}, localizedmatch.PlanOptions{MaxDepth: 1, MaxCandidates: 1}); !errors.Is(err, localizedmatch.ErrInvalidCandidate) {
		t.Fatalf("NewPlan(zero candidate) error = %v", err)
	}
	if _, err := localizedmatch.NewPlan(nil, localizedmatch.PlanOptions{Default: &zero, MaxDepth: 1, MaxCandidates: 1}); !errors.Is(err, localizedmatch.ErrInvalidCandidate) {
		t.Fatalf("NewPlan(zero default) error = %v", err)
	}

	if _, err := localizedmatch.NewPlan(nil, localizedmatch.PlanOptions{MaxDepth: 1, MaxCandidates: -1}); !errors.Is(err, localizedmatch.ErrCandidateLimit) {
		t.Fatalf("NewPlan(negative candidates) error = %v", err)
	}
	if _, err := localizedmatch.NewPlan([]localizedmatch.Chain{
		{From: mustLocale(t, "en"), Candidates: []localizedmatch.Candidate{{Kind: localizedmatch.ExactLocale, Locale: mustLocale(t, "fi")}}},
		{From: mustLocale(t, "fi"), Candidates: []localizedmatch.Candidate{{Kind: localizedmatch.ExactLocale, Locale: mustLocale(t, "de")}}},
	}, localizedmatch.PlanOptions{MaxDepth: 1, MaxCandidates: 2}); !errors.Is(err, localizedmatch.ErrDepthLimit) {
		t.Fatalf("NewPlan(graph depth) error = %v", err)
	}

	en := mustLocale(t, "en")
	fi := mustLocale(t, "fi")
	sv := mustLocale(t, "sv")
	de := mustLocale(t, "de")
	// The shared de node exercises the completed-node graph state.
	plan, err := localizedmatch.NewPlan([]localizedmatch.Chain{
		{From: en, Candidates: []localizedmatch.Candidate{{Kind: localizedmatch.ExactLocale, Locale: fi}, {Kind: localizedmatch.ExactLocale, Locale: sv}}},
		{From: fi, Candidates: []localizedmatch.Candidate{{Kind: localizedmatch.ExactLocale, Locale: de}}},
		{From: sv, Candidates: []localizedmatch.Candidate{{Kind: localizedmatch.ExactLocale, Locale: de}}},
		{From: de},
	}, localizedmatch.PlanOptions{Default: &de, MaxDepth: 4, MaxCandidates: 8})
	if err != nil {
		t.Fatal(err)
	}
	value, _ := localized.TextFromMap(map[string]string{"en": "exact", "de": "default"})
	if result := plan.Resolve(value, en); result.Kind != localizedmatch.Exact {
		t.Fatalf("exact = %+v", result)
	}
	if result := plan.Resolve(value, mustLocale(t, "nl")); result.Kind != localizedmatch.Default {
		t.Fatalf("default = %+v", result)
	}
	missingPlan, err := localizedmatch.NewPlan(nil, localizedmatch.PlanOptions{MaxDepth: 1, MaxCandidates: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result := missingPlan.Resolve(localized.Text{}, mustLocale(t, "nl")); result.Kind != localizedmatch.Missing {
		t.Fatalf("missing = %+v", result)
	}
	if result := missingPlan.Resolve(localized.Text{}, zero); result.Kind != localizedmatch.Missing {
		t.Fatalf("zero request = %+v", result)
	}
}

func TestPlanTraversesMissingAndParentCandidates(t *testing.T) {
	t.Parallel()

	requested := mustLocale(t, "zh-Hant-TW")
	missing := mustLocale(t, "fi")
	plan, err := localizedmatch.NewPlan([]localizedmatch.Chain{
		{From: requested, Candidates: []localizedmatch.Candidate{
			{Kind: localizedmatch.ParentRange, Locale: mustLocale(t, "zh-Hant-HK")},
			{Kind: localizedmatch.ExactLocale, Locale: missing},
		}},
		{From: missing},
	}, localizedmatch.PlanOptions{MaxDepth: 3, MaxCandidates: 4})
	if err != nil {
		t.Fatal(err)
	}
	value, _ := localized.TextFromMap(map[string]string{"zh-Hant": "parent"})
	if result := plan.Resolve(value, requested); result.Kind != localizedmatch.Fallback || result.Text != "parent" {
		t.Fatalf("parent result = %+v", result)
	}
	if result := plan.Resolve(localized.Text{}, requested); result.Kind != localizedmatch.Missing {
		t.Fatalf("missing chain result = %+v", result)
	}

	recursive, err := localizedmatch.NewPlan([]localizedmatch.Chain{
		{From: mustLocale(t, "en"), Candidates: []localizedmatch.Candidate{{Kind: localizedmatch.ExactLocale, Locale: mustLocale(t, "fi")}}},
		{From: mustLocale(t, "fi"), Candidates: []localizedmatch.Candidate{{Kind: localizedmatch.ExactLocale, Locale: mustLocale(t, "de")}}},
	}, localizedmatch.PlanOptions{MaxDepth: 3, MaxCandidates: 2})
	if err != nil {
		t.Fatal(err)
	}
	german, _ := localized.TextFromMap(map[string]string{"de": "recursive"})
	if result := recursive.Resolve(german, mustLocale(t, "en")); result.Text != "recursive" {
		t.Fatalf("recursive result = %+v", result)
	}

	self, err := localizedmatch.NewPlan([]localizedmatch.Chain{{
		From:       mustLocale(t, "en-US"),
		Candidates: []localizedmatch.Candidate{{Kind: localizedmatch.ParentRange, Locale: mustLocale(t, "en-US")}},
	}}, localizedmatch.PlanOptions{MaxDepth: 1, MaxCandidates: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result := self.Resolve(localized.Text{}, mustLocale(t, "en-US")); result.Kind != localizedmatch.Missing {
		t.Fatalf("self parent result = %+v", result)
	}
}
