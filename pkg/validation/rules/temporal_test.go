package rules_test

import (
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/validation/rules"
)

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

func TestTemporalRulesUseExplicitBoundariesAndClocks(t *testing.T) {
	ctx := contextFor(t)
	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	if report := rules.TimeBetween(now, now.Add(time.Hour)).Validate(ctx, now); !report.Empty() {
		t.Fatalf("TimeBetween inclusive = %v", report)
	}
	if report := rules.TimeBetween(now, now.Add(time.Hour)).Validate(ctx,
		now.Add(time.Hour)); !report.Empty() {
		t.Fatalf("TimeBetween upper inclusive = %v", report)
	}
	if report := rules.Before(now).Validate(ctx, now); !report.HasCode("before") {
		t.Fatalf("Before equal = %v", report)
	}
	if report := rules.After(now).Validate(ctx, now.Add(time.Second)); !report.Empty() {
		t.Fatalf("After = %v", report)
	}
	if report := rules.DurationBetween(time.Second, time.Minute).Validate(ctx, time.Minute); !report.Empty() {
		t.Fatalf("DurationBetween = %v", report)
	}
	if report := rules.Future(fixedClock{now}).Validate(ctx, now.Add(time.Second)); !report.Empty() {
		t.Fatalf("Future = %v", report)
	}
	if report := rules.Past(fixedClock{now}).Validate(ctx, now); !report.HasCode("past") {
		t.Fatalf("Past equal = %v", report)
	}
	if report := rules.Date("2006-01-02").Validate(ctx, "2026-02-29"); !report.HasCode("date") {
		t.Fatalf("invalid date = %v", report)
	}
	if report := rules.OrderedInterval().Validate(ctx, rules.Interval{Start: now, End: now.Add(time.Hour)}); !report.Empty() {
		t.Fatalf("OrderedInterval = %v", report)
	}
}
