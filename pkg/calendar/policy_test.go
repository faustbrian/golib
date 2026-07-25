package calendar_test

import (
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
)

func TestImmutableWeekPolicyBoundaries(t *testing.T) {
	t.Parallel()

	policy, err := calendar.NewWeekPolicy(time.Sunday)
	if err != nil || policy.WeekStart() != time.Sunday {
		t.Fatalf("NewWeekPolicy() = %v, %v", policy, err)
	}
	wednesday := calendar.MustDate(2024, time.May, 22)
	if policy.StartOfWeek(wednesday).String() != "2024-05-19" || policy.EndOfWeek(wednesday).String() != "2024-05-25" {
		t.Fatalf("week boundaries = %s..%s", policy.StartOfWeek(wednesday), policy.EndOfWeek(wednesday))
	}
	if _, err := calendar.NewWeekPolicy(time.Weekday(7)); !errors.Is(err, calendar.ErrInvalidDate) {
		t.Fatalf("invalid weekday error = %v", err)
	}
}

func TestNamedSubtractionAndComponentDifference(t *testing.T) {
	t.Parallel()

	start := calendar.MustDate(2023, time.January, 31)
	end := calendar.MustDate(2024, time.March, 2)
	difference, err := start.ComponentsUntil(end, calendar.Clamp)
	if err != nil || difference.Years != 1 || difference.Months != 1 || difference.Days != 2 {
		t.Fatalf("ComponentsUntil() = %#v, %v", difference, err)
	}
	if got, err := end.SubDays(2); err != nil || got.String() != "2024-02-29" {
		t.Fatalf("SubDays() = %s, %v", got, err)
	}
	if got, err := end.SubMonths(1, calendar.Clamp); err != nil || got.String() != "2024-02-02" {
		t.Fatalf("SubMonths() = %s, %v", got, err)
	}
	negative, err := end.ComponentsUntil(start, calendar.Clamp)
	if err != nil || negative.Years != -1 || negative.Months != -1 || negative.Days != -2 {
		t.Fatalf("negative ComponentsUntil() = %#v, %v", negative, err)
	}
}
