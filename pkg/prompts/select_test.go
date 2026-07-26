package prompts_test

import (
	"context"
	"errors"
	"testing"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

func TestSelectPreservesStableIdentitySeparateFromLabelsAndValues(t *testing.T) {
	t.Parallel()

	options := []prompts.Option[int]{
		mustOption(t, prompts.OptionConfig[int]{ID: "primary", Label: "Same", Description: "First", Value: 10}),
		mustOption(t, prompts.OptionConfig[int]{ID: "secondary", Label: "Same", Description: "Second", Value: 10}),
		mustOption(t, prompts.OptionConfig[int]{ID: "legacy", Label: "Legacy\x1b", Value: 30, Disabled: true}),
	}
	prompt, err := prompts.NewSelect(prompts.SelectConfig[int]{
		ID: "account", Label: "Account", Options: options,
		DefaultID: prompts.Some("primary"), FallbackID: prompts.Some("secondary"),
		Headless: prompts.HeadlessUseFallback,
	})
	if err != nil {
		t.Fatalf("NewSelect() error = %v", err)
	}
	options[0] = mustOption(t, prompts.OptionConfig[int]{ID: "mutated", Label: "Mutated", Value: 99})

	if got := parseValue(t, prompt, "secondary"); got != 10 {
		t.Fatalf("Parse() = %d", got)
	}
	assertInvalidSubmission(t, prompt, "Same")
	assertInvalidSubmission(t, prompt, "legacy")

	value, err := prompts.Run(context.Background(), prompt, prompts.Execution{
		Policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly},
	})
	if err != nil || value != 10 {
		t.Fatalf("Run() = %d, %v", value, err)
	}
}

func TestSelectRejectsInvalidOptionDefinitions(t *testing.T) {
	t.Parallel()

	_, err := prompts.NewOption(prompts.OptionConfig[int]{Label: "Missing ID", Value: 1})
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("missing option identity error = %v", err)
	}
	_, err = prompts.NewOption(prompts.OptionConfig[int]{ID: "missing-label", Value: 1})
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("missing option label error = %v", err)
	}

	duplicate := mustOption(t, prompts.OptionConfig[int]{ID: "same", Label: "One", Value: 1})
	_, err = prompts.NewSelect(prompts.SelectConfig[int]{
		ID: "choice", Label: "Choice", Options: []prompts.Option[int]{duplicate, duplicate},
	})
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("duplicate option identity error = %v", err)
	}
	_, err = prompts.NewSelect(prompts.SelectConfig[int]{ID: "choice", Label: "Choice"})
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("empty options error = %v", err)
	}
}

func TestOptionAccessorsAreStableAndSanitizedAtRenderTime(t *testing.T) {
	t.Parallel()

	option := mustOption(t, prompts.OptionConfig[string]{
		ID: "id", Label: "Label\x1b", Description: "Description", Group: "Group",
		Value: "value", Disabled: true,
	})
	if option.ID() != "id" || option.Label() != "Label\x1b" || option.Description() != "Description" || option.Group() != "Group" || option.Value() != "value" || !option.Disabled() {
		t.Fatalf("option accessors = %q %q %q %q %q %v", option.ID(), option.Label(), option.Description(), option.Group(), option.Value(), option.Disabled())
	}
	rendered, err := (prompts.PlainRenderer{}).Render(
		prompts.NewFrame(prompts.Line(prompts.Text(prompts.RoleDisabled, option.Label()))),
		prompts.RenderOptions{},
	)
	if err != nil || rendered != "[disabled] Label\\u{1B}\n" {
		t.Fatalf("rendered option = %q, %v", rendered, err)
	}
}

func TestMultiSelectEnforcesBoundsAndDeclarationOrder(t *testing.T) {
	t.Parallel()

	options := []prompts.Option[string]{
		mustOption(t, prompts.OptionConfig[string]{ID: "a", Label: "A", Value: "first"}),
		mustOption(t, prompts.OptionConfig[string]{ID: "b", Label: "B", Value: "second"}),
		mustOption(t, prompts.OptionConfig[string]{ID: "c", Label: "C", Value: "third"}),
	}
	prompt, err := prompts.NewMultiSelect(prompts.MultiSelectConfig[string]{
		ID: "features", Label: "Features", Options: options, Min: 1, Max: 2,
		FallbackIDs: prompts.Some([]string{"c", "a"}), Headless: prompts.HeadlessUseFallback,
	})
	if err != nil {
		t.Fatalf("NewMultiSelect() error = %v", err)
	}
	got := parseValue(t, prompt, "c,a")
	if len(got) != 2 || got[0] != "first" || got[1] != "third" {
		t.Fatalf("Parse() = %#v", got)
	}
	assertInvalidSubmission(t, prompt, "")
	assertInvalidSubmission(t, prompt, "a,b,c")
	assertInvalidSubmission(t, prompt, "a,a")
	assertInvalidSubmission(t, prompt, "unknown")

	fallback, err := prompts.Run(context.Background(), prompt, prompts.Execution{
		Policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly},
	})
	if err != nil || len(fallback) != 2 || fallback[0] != "first" || fallback[1] != "third" {
		t.Fatalf("Run() = %#v, %v", fallback, err)
	}
	fallback[0] = "mutated"
	again, err := prompts.Run(context.Background(), prompt, prompts.Execution{
		Policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly},
	})
	if err != nil || again[0] != "first" {
		t.Fatal("multi-select result mutated reusable definition state")
	}
}

func TestMultiSelectRejectsInvalidBoundsAndDefaults(t *testing.T) {
	t.Parallel()

	option := mustOption(t, prompts.OptionConfig[int]{ID: "one", Label: "One", Value: 1})
	tests := []prompts.MultiSelectConfig[int]{
		{ID: "choice", Label: "Choice", Options: []prompts.Option[int]{option}, Min: 2, Max: 1},
		{ID: "choice", Label: "Choice", Options: []prompts.Option[int]{option}, Min: 2},
		{ID: "choice", Label: "Choice", Options: []prompts.Option[int]{option}, DefaultIDs: prompts.Some([]string{"missing"})},
		{ID: "choice", Label: "Choice", Options: []prompts.Option[int]{option}, Min: 1, DefaultIDs: prompts.Some([]string{})},
	}
	for _, config := range tests {
		_, err := prompts.NewMultiSelect(config)
		if !errors.Is(err, prompts.ErrInvalidDefinition) {
			t.Fatalf("NewMultiSelect() error = %v", err)
		}
	}
}

func TestSearchDefinesUnicodeRankingAndStableTies(t *testing.T) {
	t.Parallel()

	options := []prompts.Option[string]{
		mustOption(t, prompts.OptionConfig[string]{ID: "exact", Label: "CAFÉ", Value: "exact"}),
		mustOption(t, prompts.OptionConfig[string]{ID: "prefix", Label: "Café racer", Value: "prefix"}),
		mustOption(t, prompts.OptionConfig[string]{ID: "token", Label: "Great cafe", Value: "token"}),
		mustOption(t, prompts.OptionConfig[string]{ID: "description", Label: "Other", Description: "A cafe option", Value: "description"}),
		mustOption(t, prompts.OptionConfig[string]{ID: "tie", Label: "Cafe racer", Value: "tie"}),
	}
	results, err := prompts.Search(options, "cafe\u0301", prompts.SearchPolicy{MaxOptions: 10, MaxResults: 10, MaxQueryRunes: 20})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	want := []string{"exact", "prefix"}
	if len(results) != len(want) {
		t.Fatalf("Search() = %#v", optionIDs(results))
	}
	for index, id := range want {
		if results[index].ID() != id {
			t.Fatalf("Search() = %#v", optionIDs(results))
		}
	}

	results, err = prompts.Search(options, "cafe", prompts.SearchPolicy{MaxOptions: 10, MaxResults: 10, MaxQueryRunes: 20})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got := optionIDs(results); !equalStrings(got, []string{"tie", "token", "description"}) {
		t.Fatalf("Search() order = %#v", got)
	}
}

func TestSearchBoundsWorkAndCopiesResults(t *testing.T) {
	t.Parallel()

	options := []prompts.Option[int]{
		mustOption(t, prompts.OptionConfig[int]{ID: "a", Label: "Alpha", Value: 1}),
		mustOption(t, prompts.OptionConfig[int]{ID: "b", Label: "Alphabet", Value: 2}),
	}
	for _, policy := range []prompts.SearchPolicy{
		{MaxOptions: 1, MaxResults: 1, MaxQueryRunes: 10},
		{MaxOptions: 2, MaxResults: 1, MaxQueryRunes: 2},
		{MaxOptions: 2, MaxResults: 0, MaxQueryRunes: 10},
	} {
		_, err := prompts.Search(options, "alpha", policy)
		if !errors.Is(err, prompts.ErrUnsupported) {
			t.Fatalf("Search(%#v) error = %v", policy, err)
		}
	}

	results, err := prompts.Search(options, "alpha", prompts.SearchPolicy{})
	if err != nil || len(results) != 2 {
		t.Fatalf("Search() = %#v, %v", results, err)
	}
	results[0] = mustOption(t, prompts.OptionConfig[int]{ID: "mutated", Label: "Mutated", Value: 9})
	again, err := prompts.Search(options, "alpha", prompts.SearchPolicy{})
	if err != nil || again[0].ID() != "a" {
		t.Fatal("Search results retained caller mutation")
	}

	truncated, err := prompts.Search(options, "", prompts.SearchPolicy{MaxOptions: 2, MaxResults: 1, MaxQueryRunes: 10})
	if err != nil || len(truncated) != 1 || truncated[0].ID() != "a" {
		t.Fatalf("truncated Search() = %#v, %v", optionIDs(truncated), err)
	}
	substring, err := prompts.Search(options, "pha", prompts.SearchPolicy{})
	if err != nil || len(substring) != 2 {
		t.Fatalf("substring Search() = %#v, %v", optionIDs(substring), err)
	}
	empty, err := prompts.Search([]prompts.Option[int]{}, "", prompts.SearchPolicy{})
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty Search() = %#v, %v", empty, err)
	}
	_, err = prompts.Search([]prompts.Option[int]{options[0], options[0]}, "alpha", prompts.SearchPolicy{})
	if !errors.Is(err, prompts.ErrUnsupported) {
		t.Fatalf("duplicate Search() error = %v", err)
	}
}

func TestSearchSelectValidatesPolicyAndRetainsSearchKind(t *testing.T) {
	t.Parallel()

	option := mustOption(t, prompts.OptionConfig[int]{ID: "one", Label: "One", Value: 1})
	prompt, err := prompts.NewSearchSelect(prompts.SearchSelectConfig[int]{
		Select: prompts.SelectConfig[int]{ID: "choice", Label: "Choice", Options: []prompts.Option[int]{option}},
		Search: prompts.SearchPolicy{MaxOptions: 10, MaxResults: 5, MaxQueryRunes: 20},
	})
	if err != nil || prompt.Describe().Kind != prompts.KindSearchSelect || parseValue(t, prompt, "one") != 1 {
		t.Fatalf("NewSearchSelect() = %#v, %v", prompt.Describe(), err)
	}
	_, err = prompts.NewSearchSelect(prompts.SearchSelectConfig[int]{
		Select: prompts.SelectConfig[int]{ID: "choice", Label: "Choice", Options: []prompts.Option[int]{option}},
		Search: prompts.SearchPolicy{MaxOptions: 1},
	})
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("invalid search policy error = %v", err)
	}
	_, err = prompts.NewSearchSelect(prompts.SearchSelectConfig[int]{
		Select: prompts.SelectConfig[int]{ID: "choice", Label: "Choice"},
		Search: prompts.SearchPolicy{MaxOptions: 10, MaxResults: 5, MaxQueryRunes: 20},
	})
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("invalid searchable select error = %v", err)
	}
}

func TestSelectDefinitionEdgeCases(t *testing.T) {
	t.Parallel()

	enabled := mustOption(t, prompts.OptionConfig[int]{ID: "enabled", Label: "Enabled", Value: 1})
	disabled := mustOption(t, prompts.OptionConfig[int]{ID: "disabled", Label: "Disabled", Value: 2, Disabled: true})
	tests := []prompts.SelectConfig[int]{
		{ID: "choice", Label: "Choice", Options: []prompts.Option[int]{enabled}, MaxOptions: -1},
		{ID: "choice", Label: "Choice", Options: []prompts.Option[int]{enabled, disabled}, MaxOptions: 1},
		{ID: "choice", Label: "Choice", Options: []prompts.Option[int]{{}}},
		{ID: "choice", Label: "Choice", Options: []prompts.Option[int]{enabled}, DefaultID: prompts.Some("missing")},
		{ID: "choice", Label: "Choice", Options: []prompts.Option[int]{enabled, disabled}, FallbackID: prompts.Some("disabled")},
		{ID: "choice", Label: "Choice", Options: []prompts.Option[int]{enabled}, InitialID: "missing"},
		{Label: "Choice", Options: []prompts.Option[int]{enabled}},
	}
	for _, config := range tests {
		_, err := prompts.NewSelect(config)
		if !errors.Is(err, prompts.ErrInvalidDefinition) {
			t.Fatalf("NewSelect(%#v) error = %v", config, err)
		}
	}
	if _, err := prompts.NewSelect(prompts.SelectConfig[int]{
		ID: "choice", Label: "Choice", Options: []prompts.Option[int]{enabled}, InitialID: "enabled",
	}); err != nil {
		t.Fatalf("valid initial selection error = %v", err)
	}
}

func TestMultiSelectDefinitionEdgeCases(t *testing.T) {
	t.Parallel()

	enabled := mustOption(t, prompts.OptionConfig[int]{ID: "enabled", Label: "Enabled", Value: 1})
	disabled := mustOption(t, prompts.OptionConfig[int]{ID: "disabled", Label: "Disabled", Value: 2, Disabled: true})
	tests := []prompts.MultiSelectConfig[int]{
		{ID: "choice", Label: "Choice"},
		{ID: "choice", Label: "Choice", Options: []prompts.Option[int]{enabled}, FallbackIDs: prompts.Some([]string{"missing"})},
		{ID: "choice", Label: "Choice", Options: []prompts.Option[int]{enabled}, InitialIDs: []string{"missing"}},
		{ID: "choice", Label: "Choice", Options: []prompts.Option[int]{enabled, disabled}, InitialIDs: []string{"disabled"}},
		{Label: "Choice", Options: []prompts.Option[int]{enabled}},
		{ID: "choice", Label: "Choice", Options: []prompts.Option[int]{enabled}, PreValidate: []prompts.Validator[[]int]{nil}},
	}
	for _, config := range tests {
		_, err := prompts.NewMultiSelect(config)
		if !errors.Is(err, prompts.ErrInvalidDefinition) {
			t.Fatalf("NewMultiSelect() error = %v", err)
		}
	}

	prompt, err := prompts.NewMultiSelect(prompts.MultiSelectConfig[int]{
		ID: "choice", Label: "Choice", Options: []prompts.Option[int]{enabled}, Min: 1,
		Transform: []prompts.Transformer[[]int]{func(context.Context, []int, prompts.ValidationContext) ([]int, error) {
			return nil, nil
		}},
	})
	if err != nil {
		t.Fatalf("NewMultiSelect() error = %v", err)
	}
	_, err = prompts.Parse(context.Background(), prompt, "enabled", nil)
	if !errors.Is(err, prompts.ErrValidationExhausted) {
		t.Fatalf("transformed selection count error = %v", err)
	}
}

func mustOption[T any](t *testing.T, config prompts.OptionConfig[T]) prompts.Option[T] {
	t.Helper()

	option, err := prompts.NewOption(config)
	if err != nil {
		t.Fatalf("NewOption() error = %v", err)
	}

	return option
}

func optionIDs[T any](options []prompts.Option[T]) []string {
	ids := make([]string, len(options))
	for index, option := range options {
		ids[index] = option.ID()
	}

	return ids
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}

	return true
}
