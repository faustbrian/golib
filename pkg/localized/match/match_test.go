package match_test

import (
	"errors"
	"testing"

	language "github.com/faustbrian/golib/pkg/international/locale"
	localized "github.com/faustbrian/golib/pkg/localized"
	localizedmatch "github.com/faustbrian/golib/pkg/localized/match"
)

func fixture(t *testing.T) localized.Text {
	t.Helper()
	value, err := localized.NewText(
		localized.Entry{Locale: mustLocale(t, "en-US"), Text: "Hello"},
		localized.Entry{Locale: mustLocale(t, "en-GB"), Text: "Hallo"},
		localized.Entry{Locale: mustLocale(t, "fi"), Text: ""},
		localized.Entry{Locale: mustLocale(t, "zh-Hant"), Text: "您好"},
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestBestDistinguishesExactMatchedEmptyAndMissing(t *testing.T) {
	t.Parallel()
	value := fixture(t)
	if _, err := localizedmatch.Best(value, localizedmatch.Preference{Weight: 1}); !errors.Is(err, localizedmatch.ErrInvalidCandidate) {
		t.Fatalf("Best(zero locale) error = %v", err)
	}

	tests := []struct {
		name        string
		preferences []localizedmatch.Preference
		wantKind    localizedmatch.Kind
		wantLocale  language.Tag
		wantText    string
		wantPresent bool
		wantEmpty   bool
	}{
		{"exact", []localizedmatch.Preference{{Locale: mustLocale(t, "en-GB"), Weight: 1}}, localizedmatch.Exact, mustLocale(t, "en-GB"), "Hallo", true, false},
		{"matched", []localizedmatch.Preference{{Locale: mustLocale(t, "en-CA"), Weight: 1}}, localizedmatch.Matched, mustLocale(t, "en-GB"), "Hallo", true, false},
		{"empty", []localizedmatch.Preference{{Locale: mustLocale(t, "fi"), Weight: 1}}, localizedmatch.Exact, mustLocale(t, "fi"), "", true, true},
		{"missing", nil, localizedmatch.Missing, language.Tag{}, "", false, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := localizedmatch.Best(value, test.preferences...)
			if err != nil {
				t.Fatalf("Best() error = %v", err)
			}
			if result.Kind != test.wantKind || result.Locale != test.wantLocale || result.Text != test.wantText || result.Present != test.wantPresent || result.Empty != test.wantEmpty {
				t.Fatalf("Best() = %+v", result)
			}
		})
	}
}

func TestBestUsesWeightThenStableInputOrder(t *testing.T) {
	t.Parallel()
	value := fixture(t)
	result, err := localizedmatch.Best(value,
		localizedmatch.Preference{Locale: mustLocale(t, "en-GB"), Weight: 0.5},
		localizedmatch.Preference{Locale: mustLocale(t, "en-US"), Weight: 0.9},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Locale != mustLocale(t, "en-US") {
		t.Fatalf("locale = %s", result.Locale)
	}

	if _, err := localizedmatch.Best(value, localizedmatch.Preference{Locale: mustLocale(t, "en"), Weight: 1.1}); !errors.Is(err, localizedmatch.ErrInvalidWeight) {
		t.Fatalf("weight error = %v", err)
	}
}

func TestFallbackPlanIsExplicitBoundedAndImmutable(t *testing.T) {
	t.Parallel()
	value := fixture(t)
	defaultLocale := mustLocale(t, "en-US")
	plan, err := localizedmatch.NewFallbackPlan([]language.Tag{mustLocale(t, "sv-SE"), mustLocale(t, "fi")}, &defaultLocale, 4)
	if err != nil {
		t.Fatal(err)
	}
	result := plan.Resolve(value)
	if result.Kind != localizedmatch.Fallback || result.Locale != mustLocale(t, "fi") || !result.Empty {
		t.Fatalf("Resolve() = %+v", result)
	}

	if _, err := localizedmatch.NewFallbackPlan([]language.Tag{mustLocale(t, "en"), mustLocale(t, "en")}, nil, 4); !errors.Is(err, localizedmatch.ErrDuplicateCandidate) {
		t.Fatalf("duplicate error = %v", err)
	}
	if _, err := localizedmatch.NewFallbackPlan([]language.Tag{mustLocale(t, "en"), mustLocale(t, "fi")}, nil, 1); !errors.Is(err, localizedmatch.ErrCandidateLimit) {
		t.Fatalf("limit error = %v", err)
	}
}

func TestFallbackUsesDefaultOnlyAfterChain(t *testing.T) {
	t.Parallel()
	value := fixture(t)
	defaultLocale := mustLocale(t, "en-US")
	plan, err := localizedmatch.NewFallbackPlan([]language.Tag{mustLocale(t, "sv")}, &defaultLocale, 4)
	if err != nil {
		t.Fatal(err)
	}
	result := plan.Resolve(value)
	if result.Kind != localizedmatch.Default || result.Locale != defaultLocale || result.Text != "Hello" {
		t.Fatalf("Resolve() = %+v", result)
	}
}

func TestMatchAndFallbackBoundaryMatrix(t *testing.T) {
	t.Parallel()

	if got := localizedmatch.ErrCandidateLimit.Error(); got != "localized match: candidate limit exceeded" {
		t.Fatalf("Error() = %q", got)
	}
	value := fixture(t)
	if _, err := localizedmatch.BestWithOptions(value, localizedmatch.Options{MaxCandidates: -1}); !errors.Is(err, localizedmatch.ErrCandidateLimit) {
		t.Fatalf("Best(negative limit) error = %v", err)
	}
	if _, err := localizedmatch.BestWithOptions(value, localizedmatch.Options{MaxCandidates: 1},
		localizedmatch.Preference{Locale: mustLocale(t, "en"), Weight: 1},
		localizedmatch.Preference{Locale: mustLocale(t, "fi"), Weight: 1}); !errors.Is(err, localizedmatch.ErrCandidateLimit) {
		t.Fatalf("Best(candidate limit) error = %v", err)
	}
	result, err := localizedmatch.Best(localized.Text{}, localizedmatch.Preference{Locale: mustLocale(t, "en"), Weight: 1})
	if err != nil || result.Kind != localizedmatch.Missing {
		t.Fatalf("Best(empty) = %+v, %v", result, err)
	}
	result, err = localizedmatch.Best(value, localizedmatch.Preference{Locale: mustLocale(t, "qaa"), Weight: 1})
	if err != nil || result.Kind != localizedmatch.Missing {
		t.Fatalf("Best(unsupported) = %+v, %v", result, err)
	}

	english := mustLocale(t, "en")
	zero := language.Tag{}
	if _, err := localizedmatch.NewFallbackPlan([]language.Tag{zero}, nil, 1); !errors.Is(err, localizedmatch.ErrInvalidCandidate) {
		t.Fatalf("NewFallbackPlan(zero candidate) error = %v", err)
	}
	if _, err := localizedmatch.NewFallbackPlan(nil, &zero, 1); !errors.Is(err, localizedmatch.ErrInvalidCandidate) {
		t.Fatalf("NewFallbackPlan(zero default) error = %v", err)
	}
	if _, err := localizedmatch.NewFallbackPlan(nil, nil, -1); !errors.Is(err, localizedmatch.ErrCandidateLimit) {
		t.Fatalf("NewFallbackPlan(negative) error = %v", err)
	}
	if _, err := localizedmatch.NewFallbackPlan([]language.Tag{english}, &english, 2); !errors.Is(err, localizedmatch.ErrDuplicateCandidate) {
		t.Fatalf("NewFallbackPlan(default duplicate) error = %v", err)
	}
	plan, err := localizedmatch.NewFallbackPlan([]language.Tag{mustLocale(t, "sv")}, nil, 1)
	if err != nil || plan.Resolve(value).Kind != localizedmatch.Missing {
		t.Fatalf("Fallback missing = %+v, %v", plan.Resolve(value), err)
	}
}
