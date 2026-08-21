package structplan

import (
	"math"
	"reflect"
	"strings"
	"testing"

	validation "github.com/faustbrian/golib/pkg/validation"
)

func TestTypedPlanAcceptsFieldNameAtExactPathLimit(t *testing.T) {
	limits := validation.DefaultLimits()
	limits.MaxPathLength = 2
	builder := New[struct{ Name string }](limits)
	validator := validation.ValidatorFunc[string](func(ctx validation.Context, _ string) validation.Report {
		return validation.NewReport(ctx.Limits())
	})
	if err := Add(builder, "ab", func(value struct{ Name string }) string {
		return value.Name
	}, validator); err != nil {
		t.Fatalf("Add() at exact path limit: %v", err)
	}
}

func TestCompileTypeAcceptsExactDepthTagAndFieldLimits(t *testing.T) {
	type leaf struct {
		Value string `validate:"required"`
	}
	type root struct{ Leaf leaf }
	limits := validation.DefaultLimits()
	limits.MaxDepth = 1
	limits.MaxTagLength = len("required")
	limits.MaxStructFields = 2

	plan, err := CompileTags[root](limits)
	if err != nil {
		t.Fatalf("CompileTags() at exact limits: %v", err)
	}
	ctx, err := validation.NewContext(limits)
	if err != nil {
		t.Fatal(err)
	}
	if report := plan.Validate(ctx, root{}); !report.HasCode("required") {
		t.Fatalf("compiled report = %#v", report.Violations())
	}
}

func TestCompileTypeContinuesPastIgnoredAndUntaggedFields(t *testing.T) {
	type shape struct {
		_       string
		Ignored string `validate:"-"`
		Plain   string
		Value   string `validate:"required"`
	}
	plan, err := CompileTags[shape](validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := validation.NewContext(validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	report := plan.Validate(ctx, shape{})
	if report.Len() != 1 || !report.HasCode("required") {
		t.Fatalf("report = %#v", report.Violations())
	}
}

func TestTagPlanEnforcesExactRuntimeLimitsAndContinues(t *testing.T) {
	type shape struct {
		Text  string
		Items []int
		Next  string
	}
	limits := validation.DefaultLimits()
	limits.MaxPathLength = len("Items")
	limits.MaxStringLength = 1
	limits.MaxCollectionSize = 1
	ctx, err := validation.NewContext(limits)
	if err != nil {
		t.Fatal(err)
	}
	plan := &TagPlan[shape]{fields: []compiledField{
		{index: []int{0}, path: []string{"Text"}, rules: []tagRule{{name: "required"}}},
		{index: []int{1}, path: []string{"Items"}, rules: []tagRule{{name: "min", parameter: 1}}},
		{index: []int{2}, path: []string{"Next"}, rules: []tagRule{{name: "required"}}},
	}}
	if report := plan.Validate(ctx, shape{Text: "a", Items: []int{1}, Next: "x"}); !report.Empty() {
		t.Fatalf("exact-limit report = %#v", report.Violations())
	}

	report := plan.Validate(ctx, shape{Text: "ab", Items: []int{1, 2}})
	if !report.HasCode("string_limit") || !report.HasCode("collection_limit") ||
		!report.HasCode("required") || report.Len() != 3 {
		t.Fatalf("continued limit report = %#v", report.Violations())
	}
}

func TestEmailRuleRejectsEachIndependentInvalidForm(t *testing.T) {
	for name, value := range map[string]string{
		"malformed":    "not-an-email",
		"display name": "User <user@example.com>",
		"missing at":   "localhost",
	} {
		if code := evaluateTagRule(tagRule{name: "email"}, reflect.ValueOf(value)); code != "email" {
			t.Errorf("%s code = %q", name, code)
		}
	}
	if code := evaluateTagRule(tagRule{name: "email"}, reflect.ValueOf("user@example.com")); code != "" {
		t.Fatalf("valid email code = %q", code)
	}
}

func TestWithinBoundRejectsNaNAndAcceptsExactFiniteBoundary(t *testing.T) {
	if withinBound(reflect.ValueOf(math.NaN()), 1, true) {
		t.Fatal("withinBound accepted NaN")
	}
	if !withinBound(reflect.ValueOf(1.0), 1, true) ||
		!withinBound(reflect.ValueOf(1.0), 1, false) {
		t.Fatal("withinBound rejected an exact finite boundary")
	}
}

func TestTagPlanPathLimitAcceptsExactRenderedLength(t *testing.T) {
	type shape struct{ Value string }
	name := strings.Repeat("x", 4)
	limits := validation.DefaultLimits()
	limits.MaxPathLength = len(name)
	ctx, err := validation.NewContext(limits)
	if err != nil {
		t.Fatal(err)
	}
	plan := &TagPlan[shape]{fields: []compiledField{{
		index: []int{0}, path: []string{name}, rules: []tagRule{{name: "required"}},
	}}}
	if report := plan.Validate(ctx, shape{Value: "ok"}); !report.Empty() {
		t.Fatalf("exact path report = %#v", report.Violations())
	}
}

func TestTagPlanPathLimitSuppressesFieldRulesAndContinues(t *testing.T) {
	type shape struct {
		First string
		Next  string
	}
	limits := validation.DefaultLimits()
	limits.MaxPathLength = 4
	ctx, err := validation.NewContext(limits)
	if err != nil {
		t.Fatal(err)
	}
	plan := &TagPlan[shape]{fields: []compiledField{
		{index: []int{0}, path: []string{"TooLong"}, rules: []tagRule{{name: "required"}}},
		{index: []int{1}, path: []string{"Next"}, rules: []tagRule{{name: "required"}}},
	}}
	report := plan.Validate(ctx, shape{})
	if report.Len() != 2 || !report.HasCode("path_limit") || !report.HasCode("required") {
		t.Fatalf("path-limit report = %#v", report.Violations())
	}
}
