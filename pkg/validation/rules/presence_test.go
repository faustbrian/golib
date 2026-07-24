package rules_test

import (
	"testing"

	validation "github.com/faustbrian/golib/pkg/validation"
	"github.com/faustbrian/golib/pkg/validation/rules"
)

func TestPresenceTruthTable(t *testing.T) {
	ctx := contextFor(t)
	values := map[string]validation.Value[string]{
		"missing": validation.Missing[string](),
		"null":    validation.Null[string](),
		"empty":   validation.Present(""),
		"zero":    validation.Present(""),
		"value":   validation.Present("go"),
	}
	tests := []struct {
		name      string
		validator validation.Validator[validation.Value[string]]
		valid     map[string]bool
		code      string
	}{
		{"required", rules.Required[string](), map[string]bool{"value": true}, "required"},
		{"present", rules.Present[string](), map[string]bool{"empty": true, "zero": true, "value": true}, "present"},
		{"omitted", rules.Omitted[string](), map[string]bool{"missing": true}, "omitted"},
		{"prohibited", rules.Prohibited[string](), map[string]bool{"missing": true}, "prohibited"},
		{"empty", rules.Empty[string](), map[string]bool{"empty": true, "zero": true}, "empty"},
		{"zero", rules.ZeroValue[string](), map[string]bool{"empty": true, "zero": true}, "zero"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for name, value := range values {
				report := tt.validator.Validate(ctx, value)
				if got := report.Empty(); got != tt.valid[name] {
					t.Errorf("%s: Empty() = %v, want %v (%v)", name, got, tt.valid[name], report)
				}
				if !tt.valid[name] && !report.HasCode(tt.code) {
					t.Errorf("%s: missing code %q", name, tt.code)
				}
			}
		})
	}
}

type presenceCase[T any] struct {
	name                  string
	value                 validation.Value[T]
	required, empty, zero bool
}

func TestPresenceTruthTableAcrossGoValueKinds(t *testing.T) {
	integer := 0
	assertPresenceCases(t, []presenceCase[int]{
		{"zero", validation.Present(0), false, true, true},
		{"nonzero", validation.Present(1), true, false, false},
	})
	assertPresenceCases(t, []presenceCase[bool]{
		{"false", validation.Present(false), false, true, true},
		{"true", validation.Present(true), true, false, false},
	})
	assertPresenceCases(t, []presenceCase[[]int]{
		{"nil", validation.Present[[]int](nil), false, true, true},
		{"empty", validation.Present([]int{}), false, true, false},
		{"value", validation.Present([]int{1}), true, false, false},
	})
	assertPresenceCases(t, []presenceCase[map[string]int]{
		{"nil", validation.Present[map[string]int](nil), false, true, true},
		{"empty", validation.Present(map[string]int{}), false, true, false},
		{"value", validation.Present(map[string]int{"a": 1}), true, false, false},
	})
	assertPresenceCases(t, []presenceCase[*int]{
		{"nil", validation.Present[*int](nil), false, true, true},
		{"value", validation.Present(&integer), true, false, false},
	})
	assertPresenceCases(t, []presenceCase[[1]int]{
		{"zero", validation.Present([1]int{}), true, false, true},
		{"value", validation.Present([1]int{1}), true, false, false},
	})
	type record struct{ Count int }
	assertPresenceCases(t, []presenceCase[record]{
		{"zero", validation.Present(record{}), false, true, true},
		{"value", validation.Present(record{Count: 1}), true, false, false},
	})
}

func assertPresenceCases[T any](t *testing.T, cases []presenceCase[T]) {
	t.Helper()
	cases = append(cases,
		presenceCase[T]{name: "missing", value: validation.Missing[T]()},
		presenceCase[T]{name: "null", value: validation.Null[T]()},
	)
	ctx := contextFor(t)
	for _, current := range cases {
		t.Run(current.name, func(t *testing.T) {
			present := current.value.Presence() == validation.PresentState
			checks := []struct {
				name string
				got  bool
				want bool
			}{
				{"required", rules.Required[T]().Validate(ctx, current.value).Empty(), current.required},
				{"present", rules.Present[T]().Validate(ctx, current.value).Empty(), present},
				{"omitted", rules.Omitted[T]().Validate(ctx, current.value).Empty(), !present && current.value.Presence() == validation.MissingState},
				{"prohibited", rules.Prohibited[T]().Validate(ctx, current.value).Empty(), !present && current.value.Presence() == validation.MissingState},
				{"empty", rules.Empty[T]().Validate(ctx, current.value).Empty(), current.empty},
				{"zero", rules.ZeroValue[T]().Validate(ctx, current.value).Empty(), current.zero},
			}
			for _, check := range checks {
				if check.got != check.want {
					t.Errorf("%s = %v, want %v", check.name, check.got, check.want)
				}
			}
		})
	}
}

func contextFor(t *testing.T) validation.Context {
	t.Helper()
	ctx, err := validation.NewContext(validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}
