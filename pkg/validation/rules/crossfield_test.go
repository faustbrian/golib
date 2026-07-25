package rules_test

import (
	"testing"

	validation "github.com/faustbrian/golib/pkg/validation"
	"github.com/faustbrian/golib/pkg/validation/rules"
)

type registration struct {
	password string
	confirm  string
	age      int
	guardian validation.Value[string]
}

type profile struct{ name string }

func TestCrossFieldRulesUseExplicitTypedAccessors(t *testing.T) {
	ctx := contextFor(t)
	equal := rules.FieldsEqual(
		"password", "confirm",
		func(value registration) string { return value.password },
		func(value registration) string { return value.confirm },
	)
	report := equal.Validate(ctx, registration{password: "secret", confirm: "wrong"})
	if !report.HasCode("equal") || report.Violations()[0].Path().String() != "confirm" {
		t.Fatalf("FieldsEqual = %#v", report.Violations())
	}

	required := rules.RequiredWhen(
		"guardian",
		func(value registration) bool { return value.age < 18 },
		func(value registration) validation.Value[string] { return value.guardian },
	)
	if report := required.Validate(ctx, registration{age: 17, guardian: validation.Missing[string]()}); !report.HasCode("required") || report.Violations()[0].Path().String() != "guardian" {
		t.Fatalf("RequiredWhen = %#v", report.Violations())
	}
	if report := required.Validate(ctx, registration{age: 18, guardian: validation.Missing[string]()}); !report.Empty() {
		t.Fatalf("RequiredWhen false = %v", report)
	}

	excluded := rules.ExcludedWhen(
		"guardian",
		func(value registration) bool { return value.age >= 18 },
		func(value registration) validation.Value[string] { return value.guardian },
	)
	if report := excluded.Validate(ctx, registration{age: 18, guardian: validation.Present("secret")}); !report.HasCode("excluded") {
		t.Fatalf("ExcludedWhen = %v", report)
	}
}

func TestCrossFieldOrderingAndNestedPaths(t *testing.T) {
	ctx := contextFor(t)
	ordered := rules.FieldsOrdered(
		"minimum", "maximum",
		func(value [2]int) int { return value[0] },
		func(value [2]int) int { return value[1] },
	)
	if report := ordered.Validate(ctx, [2]int{2, 1}); !report.HasCode("ordered") ||
		report.Violations()[0].Path().String() != "maximum" {
		t.Fatalf("ordered report = %#v", report.Violations())
	}
	if report := ordered.Validate(ctx, [2]int{1, 1}); !report.Empty() {
		t.Fatalf("ordered equal = %v", report)
	}

	nested := rules.Nested("profile", func(value struct{ Profile profile }) profile {
		return value.Profile
	}, validation.ValidatorFunc[profile](func(ctx validation.Context, value profile) validation.Report {
		if value.name != "" {
			return validation.NewReport(ctx.Limits())
		}
		return validation.NewReport(ctx.Limits()).Add(
			validation.NewViolation(ctx.WithPath(validation.Field("name")).Path(),
				"required", validation.Error, nil, nil))
	}))
	report := nested.Validate(ctx, struct{ Profile profile }{})
	if got := report.Violations()[0].Path().String(); got != "profile.name" {
		t.Fatalf("nested path = %q", got)
	}
}
