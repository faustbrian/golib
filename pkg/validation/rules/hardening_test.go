package rules_test

import (
	"strings"
	"testing"
	"time"

	validation "github.com/faustbrian/golib/pkg/validation"
	"github.com/faustbrian/golib/pkg/validation/rules"
)

func TestCollectionFailureAndShortCircuitBranches(t *testing.T) {
	ctx := contextFor(t)
	if report := rules.MapSize[string, int](1, 1).Validate(ctx, map[string]int{}); !report.HasCode("size") {
		t.Fatalf("MapSize invalid = %v", report)
	}
	if report := rules.Unique[int]().Validate(ctx, []int{1, 2}); !report.Empty() {
		t.Fatalf("Unique valid = %v", report)
	}
	limits := validation.DefaultLimits()
	limits.MaxCollectionSize = 1
	bounded, err := validation.NewContext(limits)
	if err != nil {
		t.Fatal(err)
	}
	if report := rules.Unique[int]().Validate(bounded, []int{1, 2}); !report.HasCode("collection_limit") {
		t.Fatalf("Unique limit = %v", report)
	}
	calls := 0
	fail := validation.ValidatorFunc[int](func(ctx validation.Context, _ int) validation.Report {
		calls++
		return validation.NewReport(ctx.Limits()).Add(
			validation.NewViolation(ctx.Path(), "item", validation.Error, nil, nil))
	})
	rules.Items(validation.ShortCircuit, fail).Validate(ctx, []int{1, 2})
	if calls != 1 {
		t.Fatalf("item calls = %d", calls)
	}
	keyCalls := 0
	keyFail := validation.ValidatorFunc[string](func(ctx validation.Context, _ string) validation.Report {
		keyCalls++
		return validation.NewReport(ctx.Limits()).Add(
			validation.NewViolation(ctx.Path(), "key", validation.Error, nil, nil))
	})
	rules.Keys[string, int](validation.ShortCircuit, keyFail).
		Validate(ctx, map[string]int{"b": 2, "a": 1})
	if keyCalls != 1 {
		t.Fatalf("key calls = %d", keyCalls)
	}
	if report := rules.Keys[string, int](validation.CollectAll, keyFail).
		Validate(bounded, map[string]int{"a": 1, "b": 2}); !report.HasCode("collection_limit") {
		t.Fatalf("Keys limit = %v", report)
	}
}

func TestCrossFieldPassingAndExcludedBranches(t *testing.T) {
	ctx := contextFor(t)
	equal := rules.FieldsEqual("a", "b", func(v registration) string { return v.password },
		func(v registration) string { return v.confirm })
	if report := equal.Validate(ctx, registration{password: "x", confirm: "x"}); !report.Empty() {
		t.Fatalf("equal = %v", report)
	}
	excluded := rules.ExcludedWhen("guardian", func(v registration) bool { return v.age >= 18 },
		func(v registration) validation.Value[string] { return v.guardian })
	if report := excluded.Validate(ctx, registration{age: 17, guardian: validation.Present("x")}); !report.Empty() {
		t.Fatalf("condition false = %v", report)
	}
	if report := excluded.Validate(ctx, registration{age: 18, guardian: validation.Missing[string]()}); !report.Empty() {
		t.Fatalf("missing excluded = %v", report)
	}
}

func TestHostnameAndTemporalFailureBranches(t *testing.T) {
	ctx := contextFor(t)
	for _, value := range []string{"", strings.Repeat("a", 254), "bad..name"} {
		if report := rules.Hostname().Validate(ctx, value); !report.HasCode("hostname") {
			t.Errorf("Hostname(%q) = %v", value, report)
		}
	}
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	if report := rules.Before(now).Validate(ctx, now.Add(-time.Second)); !report.Empty() {
		t.Fatalf("Before valid = %v", report)
	}
	if report := rules.Past(fixedClock{now}).Validate(ctx, now.Add(-time.Second)); !report.Empty() {
		t.Fatalf("Past valid = %v", report)
	}
	tests := []struct {
		name string
		run  func() validation.Report
		code string
	}{
		{"range", func() validation.Report {
			return rules.TimeBetween(now, now.Add(time.Hour)).Validate(ctx, now.Add(2*time.Hour))
		}, "time_range"},
		{"before", func() validation.Report { return rules.Before(now).Validate(ctx, now.Add(time.Second)) }, "before"},
		{"after", func() validation.Report { return rules.After(now).Validate(ctx, now) }, "after"},
		{"future", func() validation.Report { return rules.Future(fixedClock{now}).Validate(ctx, now) }, "future"},
		{"past", func() validation.Report { return rules.Past(fixedClock{now}).Validate(ctx, now.Add(time.Second)) }, "past"},
		{"date", func() validation.Report { return rules.Date("2006-01-02").Validate(ctx, "2026-07-16") }, ""},
		{"interval", func() validation.Report {
			return rules.OrderedInterval().Validate(ctx, rules.Interval{Start: now, End: now.Add(-time.Second)})
		}, "interval_order"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := tt.run()
			if tt.code == "" && !report.Empty() {
				t.Fatalf("report = %v", report)
			}
			if tt.code != "" && !report.HasCode(tt.code) {
				t.Fatalf("report = %v, want %s", report, tt.code)
			}
		})
	}
}

func TestStringFacingRulesRejectOversizedInputBeforeParsing(t *testing.T) {
	limits := validation.DefaultLimits()
	limits.MaxStringLength = 3
	ctx, err := validation.NewContext(limits)
	if err != nil {
		t.Fatal(err)
	}
	pattern, err := rules.Pattern(".*", limits)
	if err != nil {
		t.Fatal(err)
	}
	if report := rules.ByteLength(0, 100).Validate(ctx, "abc"); !report.Empty() {
		t.Fatalf("exact string limit report = %#v", report.Violations())
	}
	validators := []validation.Validator[string]{
		rules.ByteLength(0, 100), rules.RuneLength(0, 100), pattern,
		rules.Prefix("a"), rules.Suffix("d"), rules.OneOf("abcd"),
		rules.URL(), rules.Hostname(), rules.IP(), rules.CIDR(), rules.Email(),
		rules.UUID(), rules.Identifier(), rules.Date("2006-01-02"),
		rules.Range("a", "z"), rules.GreaterThan("a"), rules.LessThan("z"),
	}
	for index, validator := range validators {
		report := validator.Validate(ctx, "abcd")
		if !report.HasCode("string_limit") || report.Len() != 1 {
			t.Errorf("validator %d report = %#v", index, report.Violations())
		}
	}
	type alias string
	if report := rules.Range[alias]("a", "z").Validate(ctx, "abcd"); !report.HasCode("string_limit") {
		t.Fatalf("ordered alias report = %#v", report.Violations())
	}
	if report := rules.Unique[string]().Validate(ctx, []string{"abcd"}); !report.HasCode("string_limit") {
		t.Fatalf("unique string report = %#v", report.Violations())
	}
	passKey := validation.ValidatorFunc[string](func(ctx validation.Context,
		_ string,
	) validation.Report {
		return validation.NewReport(ctx.Limits())
	})
	for name, report := range map[string]validation.Report{
		"keys": rules.Keys[string, int](validation.CollectAll, passKey).
			Validate(ctx, map[string]int{"abcd": 1}),
		"values": rules.Values[string, int](validation.CollectAll,
			validation.ValidatorFunc[int](func(ctx validation.Context,
				_ int,
			) validation.Report {
				return validation.NewReport(ctx.Limits())
			})).Validate(ctx, map[string]int{"abcd": 1}),
	} {
		if !report.HasCode("string_limit") || report.Violations()[0].Path().String() != "" {
			t.Errorf("%s oversized key report = %#v", name, report.Violations())
		}
	}
}
