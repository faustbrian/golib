package rules_test

import (
	"testing"

	validation "github.com/faustbrian/golib/pkg/validation"
	"github.com/faustbrian/golib/pkg/validation/rules"
)

func TestCollectionRulesPreserveIndexesAndBounds(t *testing.T) {
	ctx := contextFor(t)
	if report := rules.SliceSize[int](1, 2).Validate(ctx, []int{1, 2}); !report.Empty() {
		t.Fatalf("SliceSize valid = %v", report)
	}
	if report := rules.SliceSize[int](1, 2).Validate(ctx, []int{1}); !report.Empty() {
		t.Fatalf("SliceSize lower boundary = %v", report)
	}
	if report := rules.SliceSize[int](1, 2).Validate(ctx, []int{}); !report.HasCode("size") {
		t.Fatalf("SliceSize invalid = %v", report)
	}
	if report := rules.Unique[int]().Validate(ctx, []int{1, 2, 1}); !report.HasCode("unique") || report.Violations()[0].Path().String() != "[2]" {
		t.Fatalf("Unique = %v at %#v", report, report.Violations())
	}

	positive := validation.ValidatorFunc[int](func(ctx validation.Context, value int) validation.Report {
		if value > 0 {
			return validation.NewReport(ctx.Limits())
		}
		return validation.NewReport(ctx.Limits()).Add(
			validation.NewViolation(ctx.Path(), "positive", validation.Error, nil, nil),
		)
	})
	report := rules.Items(validation.CollectAll, positive).Validate(ctx, []int{0, 1, -1})
	violations := report.Violations()
	if len(violations) != 2 || violations[0].Path().String() != "[0]" ||
		violations[1].Path().String() != "[2]" {
		t.Fatalf("Items paths = %#v", violations)
	}

	limits := validation.DefaultLimits()
	limits.MaxCollectionSize = 2
	bounded, err := validation.NewContext(limits)
	if err != nil {
		t.Fatal(err)
	}
	if report := rules.Items(validation.CollectAll, positive).Validate(bounded, []int{1, 2, 3}); !report.HasCode("collection_limit") {
		t.Fatalf("Items oversized = %v", report)
	}
}

func TestMapKeysAreValidatedDeterministically(t *testing.T) {
	ctx := contextFor(t)
	nonempty := validation.ValidatorFunc[string](func(ctx validation.Context, value string) validation.Report {
		if value != "" {
			return validation.NewReport(ctx.Limits())
		}
		return validation.NewReport(ctx.Limits()).Add(
			validation.NewViolation(ctx.Path(), "key", validation.Error, nil, nil),
		)
	})
	if report := rules.MapSize[string, int](1, 2).Validate(ctx, map[string]int{"a": 1}); !report.Empty() {
		t.Fatalf("MapSize = %v", report)
	}
	if report := rules.MapSize[string, int](1, 2).Validate(ctx,
		map[string]int{"a": 1, "b": 2}); !report.Empty() {
		t.Fatalf("MapSize upper boundary = %v", report)
	}
	report := rules.Keys[string, int](validation.CollectAll, nonempty).
		Validate(ctx, map[string]int{"z": 1, "": 2})
	if report.Len() != 1 || report.Violations()[0].Path().String() != "[]" {
		t.Fatalf("key report = %#v", report.Violations())
	}
}

func TestMapValuesUseSortedKeyPaths(t *testing.T) {
	ctx := contextFor(t)
	positive := validation.ValidatorFunc[int](func(ctx validation.Context, value int) validation.Report {
		if value > 0 {
			return validation.NewReport(ctx.Limits())
		}
		return validation.NewReport(ctx.Limits()).Add(
			validation.NewViolation(ctx.Path(), "positive", validation.Error, nil, nil))
	})
	report := rules.Values[string, int](validation.CollectAll, positive).
		Validate(ctx, map[string]int{"z": 0, "a": -1})
	violations := report.Violations()
	if len(violations) != 2 || violations[0].Path().String() != "[a]" ||
		violations[1].Path().String() != "[z]" {
		t.Fatalf("value paths = %#v", violations)
	}
	short := rules.Values[string, int](validation.ShortCircuit, positive).
		Validate(ctx, map[string]int{"a": -1, "z": 0})
	if short.Len() != 1 {
		t.Fatalf("short values = %#v", short.Violations())
	}
	valid := rules.Values[string, int](validation.CollectAll, positive).
		Validate(ctx, map[string]int{"a": 1, "z": 2})
	if !valid.Empty() {
		t.Fatalf("valid values = %#v", valid.Violations())
	}
	limits := validation.DefaultLimits()
	limits.MaxCollectionSize = 1
	bounded, err := validation.NewContext(limits)
	if err != nil {
		t.Fatal(err)
	}
	if report := rules.Values[string, int](validation.CollectAll, positive).
		Validate(bounded, map[string]int{"a": 1, "b": 2}); !report.HasCode("collection_limit") {
		t.Fatalf("bounded values = %v", report)
	}
}
