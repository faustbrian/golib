package rules_test

import (
	"math"
	"testing"

	validation "github.com/faustbrian/golib/pkg/validation"
	"github.com/faustbrian/golib/pkg/validation/rules"
)

func TestNumericRules(t *testing.T) {
	ctx := contextFor(t)
	tests := []struct {
		name  string
		valid bool
		code  string
		run   func() bool
	}{
		{"range lower inclusive", true, "range", func() bool { return rules.Range(1, 3).Validate(ctx, 1).Empty() }},
		{"range upper inclusive", true, "range", func() bool { return rules.Range(1, 3).Validate(ctx, 3).Empty() }},
		{"range fail", false, "range", func() bool { return rules.Range(1, 3).Validate(ctx, 4).Empty() }},
		{"greater", true, "greater_than", func() bool { return rules.GreaterThan(2).Validate(ctx, 3).Empty() }},
		{"greater fail", false, "greater_than", func() bool { return rules.GreaterThan(2).Validate(ctx, 2).Empty() }},
		{"less", true, "less_than", func() bool { return rules.LessThan(2).Validate(ctx, 1).Empty() }},
		{"less fail", false, "less_than", func() bool { return rules.LessThan(2).Validate(ctx, 2).Empty() }},
		{"finite", true, "finite", func() bool { return rules.Finite().Validate(ctx, 1.5).Empty() }},
		{"infinite", false, "finite", func() bool { return rules.Finite().Validate(ctx, math.Inf(1)).Empty() }},
		{"precision", true, "precision", func() bool { return rules.Precision(2).Validate(ctx, 1.25).Empty() }},
		{"precision fail", false, "precision", func() bool { return rules.Precision(2).Validate(ctx, 1.251).Empty() }},
		{"multiple", true, "multiple_of", func() bool { return rules.MultipleOf(0.25).Validate(ctx, 1.5).Empty() }},
		{"multiple fail", false, "multiple_of", func() bool { return rules.MultipleOf(0.25).Validate(ctx, 1.6).Empty() }},
		{"multiple infinite divisor", false, "multiple_of", func() bool { return rules.MultipleOf(math.Inf(1)).Validate(ctx, 1).Empty() }},
		{"multiple negative divisor", false, "multiple_of", func() bool { return rules.MultipleOf(-1).Validate(ctx, 2).Empty() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.run(); got != tt.valid {
				t.Fatalf("valid = %v, want %v (%s)", got, tt.valid, tt.code)
			}
		})
	}
}

func TestNumericRulesRejectHostileEdgesWithoutOverflow(t *testing.T) {
	ctx := contextFor(t)
	if report := rules.Range(math.MinInt, math.MaxInt).Validate(ctx, math.MinInt); !report.Empty() {
		t.Fatalf("minimum integer = %#v", report.Violations())
	}
	if report := rules.Range(math.MinInt, math.MaxInt).Validate(ctx, math.MaxInt); !report.Empty() {
		t.Fatalf("maximum integer = %#v", report.Violations())
	}
	for name, report := range map[string]validation.Report{
		"nan finite":         rules.Finite().Validate(ctx, math.NaN()),
		"negative infinity":  rules.Finite().Validate(ctx, math.Inf(-1)),
		"nan range":          rules.Range(-1.0, 1.0).Validate(ctx, math.NaN()),
		"negative precision": rules.Precision(-1).Validate(ctx, 1),
		"nan precision":      rules.Precision(2).Validate(ctx, math.NaN()),
		"infinite precision": rules.Precision(2).Validate(ctx, math.Inf(1)),
		"zero divisor":       rules.MultipleOf(0).Validate(ctx, 0),
		"nan divisor":        rules.MultipleOf(math.NaN()).Validate(ctx, 1),
		"nan multiple":       rules.MultipleOf(1).Validate(ctx, math.NaN()),
		"infinite multiple":  rules.MultipleOf(1).Validate(ctx, math.Inf(1)),
	} {
		if !report.HasErrors() {
			t.Errorf("%s report = %#v", name, report.Violations())
		}
	}
	if report := rules.Finite().Validate(ctx, math.Copysign(0, -1)); !report.Empty() {
		t.Fatalf("negative zero = %#v", report.Violations())
	}
	if report := rules.Precision(0).Validate(ctx, 1); !report.Empty() {
		t.Fatalf("zero precision = %#v", report.Violations())
	}
}
