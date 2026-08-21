package prompts

import "testing"

func TestOwnOptionsExactBoundsAndRequiredFields(t *testing.T) {
	t.Parallel()

	valid := Option[int]{id: "one", label: "One", value: 1}
	options, byID, err := ownOptions([]Option[int]{valid}, 1)
	if err != nil || len(options) != 1 || byID["one"].value != 1 {
		t.Fatalf("exact ownOptions() = %#v, %#v, %v", options, byID, err)
	}
	for name, option := range map[string]Option[int]{
		"identity": {label: "One"},
		"label":    {id: "one"},
	} {
		if _, _, err := ownOptions([]Option[int]{option}, 1); err == nil {
			t.Fatalf("%s ownOptions() returned nil error", name)
		}
	}
}

func TestMultiSelectAcceptsExactDeclaredBounds(t *testing.T) {
	t.Parallel()

	options := []Option[int]{
		{id: "one", label: "One", value: 1},
		{id: "two", label: "Two", value: 2},
	}
	prompt, err := NewMultiSelect(MultiSelectConfig[int]{
		ID: "numbers", Label: "Numbers", Options: options,
		Min: 2, Max: 2, MaxOptions: 2,
	})
	if err != nil || prompt.definition.selection.minimum != 2 ||
		prompt.definition.selection.maximum != 2 {
		t.Fatalf("exact multi-select = %#v, %v", prompt.Describe(), err)
	}
}

func TestMultiSelectRejectsEachInvalidBoundIndependently(t *testing.T) {
	t.Parallel()

	options := []Option[int]{
		{id: "one", label: "One", value: 1},
		{id: "two", label: "Two", value: 2},
	}
	for name, bounds := range map[string]struct{ minimum, maximum int }{
		"negative minimum":      {-1, 2},
		"maximum below minimum": {2, 1},
		"maximum above options": {0, 3},
	} {
		_, err := NewMultiSelect(MultiSelectConfig[int]{
			ID: "numbers", Label: "Numbers", Options: options,
			Min: bounds.minimum, Max: bounds.maximum, MaxOptions: 2,
		})
		if err == nil {
			t.Fatalf("%s NewMultiSelect() returned nil error", name)
		}
	}
}
