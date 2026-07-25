package dateperiod_test

import (
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/dateperiod"
)

func TestSplitDaysConservesDiscreteMembershipAcrossBounds(t *testing.T) {
	t.Parallel()

	start := calendar.MustDate(2026, time.January, 1)
	end := calendar.MustDate(2026, time.January, 10)
	for _, bounds := range temporal.AllBounds() {
		period, _ := dateperiod.New(start, end, bounds)
		parts, err := period.SplitDays(3, temporal.Limits{})
		if err != nil {
			t.Fatalf("SplitDays(%v): %v", bounds, err)
		}
		set, err := dateperiod.NewSet(temporal.Limits{}, parts...)
		want, wantErr := dateperiod.NewSet(temporal.Limits{}, period)
		if err != nil || wantErr != nil || !set.Equal(want) {
			t.Fatalf("SplitDays(%v) conservation = %+v, %v, %v", bounds, parts, err, wantErr)
		}
		for _, part := range parts {
			if part.Days() > 3 || part.Bounds() != temporal.Closed {
				t.Fatalf("split part = %+v", part)
			}
		}
	}
}

func TestSplitDaysHandlesEmptyOversizedAndBoundedWork(t *testing.T) {
	t.Parallel()

	date := calendar.MustDate(2026, time.January, 1)
	empty, _ := dateperiod.New(date, date, temporal.Open)
	if parts, err := empty.SplitDays(1, temporal.Limits{}); err != nil || len(parts) != 0 {
		t.Fatalf("empty SplitDays() = %+v, %v", parts, err)
	}
	period, _ := dateperiod.New(date, calendar.MustDate(2026, time.January, 3), temporal.Closed)
	parts, err := period.SplitDays(100, temporal.Limits{})
	if err != nil || len(parts) != 1 || !parts[0].SetEqual(period) {
		t.Fatalf("oversized SplitDays() = %+v, %v", parts, err)
	}
	for _, step := range []int{0, -1} {
		if _, err := period.SplitDays(step, temporal.Limits{}); !errors.Is(err, temporal.ErrStep) {
			t.Fatalf("SplitDays(%d) error = %v", step, err)
		}
	}
	if _, err := period.SplitDays(1, temporal.Limits{Steps: 2}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("SplitDays(limit) error = %v", err)
	}
	if _, err := period.SplitDays(1, temporal.Limits{Precision: 10}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("SplitDays(invalid limits) error = %v", err)
	}
}
