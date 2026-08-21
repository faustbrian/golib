package rules

import (
	"math"
	"strings"
	"testing"

	validation "github.com/faustbrian/golib/pkg/validation"
)

func TestCollectionRulesAcceptEveryExactResourceLimit(t *testing.T) {
	limits := validation.DefaultLimits()
	limits.MaxCollectionSize = 1
	limits.MaxStringLength = 1
	ctx, err := validation.NewContext(limits)
	if err != nil {
		t.Fatal(err)
	}
	passInt := validation.ValidatorFunc[int](func(ctx validation.Context, _ int) validation.Report {
		return validation.NewReport(ctx.Limits())
	})
	passString := validation.ValidatorFunc[string](func(ctx validation.Context, _ string) validation.Report {
		return validation.NewReport(ctx.Limits())
	})

	reports := map[string]validation.Report{
		"unique": Unique[string]().Validate(ctx, []string{"a"}),
		"items":  Items(validation.CollectAll, passInt).Validate(ctx, []int{1}),
		"keys": Keys[string, int](validation.CollectAll, passString).
			Validate(ctx, map[string]int{"a": 1}),
		"values": Values[string, int](validation.CollectAll, passInt).
			Validate(ctx, map[string]int{"a": 1}),
	}
	for name, report := range reports {
		if !report.Empty() {
			t.Errorf("%s exact-limit report = %#v", name, report.Violations())
		}
	}
}

func TestKeysCollectAllContinuesAfterBlockingFindings(t *testing.T) {
	ctx, err := validation.NewContext(validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	validator := validation.ValidatorFunc[string](func(
		ctx validation.Context, _ string,
	) validation.Report {
		return validation.NewReport(ctx.Limits()).Add(
			validation.NewViolation(ctx.Path(), "key", validation.Error, nil, nil),
		)
	})
	report := Keys[string, int](validation.CollectAll, validator).
		Validate(ctx, map[string]int{"a": 1, "b": 2})
	if report.Len() != 2 {
		t.Fatalf("Keys(CollectAll) = %#v", report.Violations())
	}
}

func TestPrecisionAndMultipleOfHonorToleranceAndInvalidInputs(t *testing.T) {
	ctx, err := validation.NewContext(validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if report := precisionValidator(0, 1).Validate(ctx, 1e-9); !report.Empty() {
		t.Fatalf("precision tolerance boundary = %#v", report.Violations())
	}
	if report := precisionValidator(-1, 1).Validate(ctx, 1); !report.HasCode("precision") {
		t.Fatalf("negative precision = %#v", report.Violations())
	}
	if report := Precision(309).Validate(ctx, 1); !report.HasCode("precision") {
		t.Fatalf("overflowed precision scale = %#v", report.Violations())
	}
	if report := MultipleOf(1).Validate(ctx, 1e-9); !report.Empty() {
		t.Fatalf("multiple tolerance boundary = %#v", report.Violations())
	}
	for name, report := range map[string]validation.Report{
		"zero divisor":             MultipleOf(0).Validate(ctx, 1),
		"negative divisor":         MultipleOf(-1).Validate(ctx, 1),
		"infinite divisor":         MultipleOf(math.Inf(1)).Validate(ctx, 1),
		"not-a-number divisor":     MultipleOf(math.NaN()).Validate(ctx, 1),
		"not-a-number quotient":    MultipleOf(1).Validate(ctx, math.NaN()),
		"infinite quotient":        MultipleOf(1).Validate(ctx, math.Inf(1)),
		"above tolerance boundary": MultipleOf(1).Validate(ctx, math.Nextafter(1e-9, 1)),
	} {
		if !report.HasCode("multiple_of") {
			t.Errorf("%s = %#v", name, report.Violations())
		}
	}
}

func TestURLAndHostnameValidateEachIndependentBoundary(t *testing.T) {
	ctx, err := validation.NewContext(validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"malformed":    "://",
		"wrong scheme": "ftp://example.com",
		"missing host": "http:/path",
	} {
		if report := URL().Validate(ctx, value); !report.HasCode("url") {
			t.Errorf("%s URL(%q) = %#v", name, value, report.Violations())
		}
	}
	if report := URL().Validate(ctx, "http://example.com"); !report.Empty() {
		t.Fatalf("HTTP URL = %#v", report.Violations())
	}

	exact := strings.Join([]string{
		strings.Repeat("a", 63),
		strings.Repeat("b", 63),
		strings.Repeat("c", 63),
		strings.Repeat("d", 61),
	}, ".")
	if len(exact) != 253 {
		t.Fatalf("hostname fixture length = %d", len(exact))
	}
	if report := Hostname().Validate(ctx, exact); !report.Empty() {
		t.Fatalf("exact hostname boundary = %#v", report.Violations())
	}
	if report := Hostname().Validate(ctx, exact+"e"); !report.HasCode("hostname") {
		t.Fatalf("oversized hostname = %#v", report.Violations())
	}
}

func TestPatternAcceptsExpressionAtExactLimit(t *testing.T) {
	limits := validation.DefaultLimits()
	limits.MaxRegexPatternLength = 2
	validator, err := Pattern("a+", limits)
	if err != nil {
		t.Fatalf("Pattern() at exact limit: %v", err)
	}
	ctx, err := validation.NewContext(limits)
	if err != nil {
		t.Fatal(err)
	}
	if report := validator.Validate(ctx, "aa"); !report.Empty() {
		t.Fatalf("pattern report = %#v", report.Violations())
	}
}
