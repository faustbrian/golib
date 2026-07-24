package timeofday_test

import (
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	calendartz "github.com/faustbrian/golib/pkg/calendar/timezone"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
)

func TestFromOffsetAndDurationConstructors(t *testing.T) {
	value, err := timeofday.FromOffset(12*time.Hour+34*time.Minute, 0)
	if err != nil || value.String() != "12:34:00" {
		t.Fatalf("FromOffset() = %v, %v", value, err)
	}
	end, err := timeofday.FromOffset(24*time.Hour, 0)
	if err != nil || !end.IsEndBoundary() {
		t.Fatalf("FromOffset(day) = %v, %v", end, err)
	}
	if _, err := timeofday.FromOffset(-1, 0); !errors.Is(err, temporal.ErrInvalidTime) {
		t.Fatalf("FromOffset(-1) error = %v", err)
	}
	if _, err := timeofday.FromOffset(time.Second+1, 8); !errors.Is(err, temporal.ErrPrecision) {
		t.Fatalf("FromOffset(precision) error = %v", err)
	}

	since, err := timeofday.Since(value, 2*time.Hour, temporal.ClosedOpen)
	if err != nil || since.End().Offset() != 14*time.Hour+34*time.Minute {
		t.Fatalf("Since() = %+v, %v", since, err)
	}
	until, err := timeofday.Until(value, 2*time.Hour, temporal.ClosedOpen)
	if err != nil || until.Start().Offset() != 10*time.Hour+34*time.Minute {
		t.Fatalf("Until() = %+v, %v", until, err)
	}
	around, err := timeofday.Around(value, time.Hour, temporal.ClosedOpen)
	if err != nil || around.Duration() != 2*time.Hour {
		t.Fatalf("Around() = %+v, %v", around, err)
	}
	zero, err := timeofday.Since(value, 0, temporal.ClosedOpen)
	if err != nil || zero.Kind() != timeofday.CollapsedKind {
		t.Fatalf("Since(zero) = %+v, %v", zero, err)
	}
	full, err := timeofday.Since(value, 24*time.Hour, temporal.ClosedOpen)
	if err != nil || full.Kind() != timeofday.FullDayKind {
		t.Fatalf("Since(day) = %+v, %v", full, err)
	}
	if _, err := timeofday.Until(value, -time.Second, temporal.Closed); !errors.Is(err, temporal.ErrStep) {
		t.Fatalf("Until(negative) error = %v", err)
	}
	if _, err := timeofday.Since(value, -time.Second, temporal.Closed); !errors.Is(err, temporal.ErrStep) {
		t.Fatalf("Since(negative) error = %v", err)
	}
	untilZero, err := timeofday.Until(value, 0, temporal.Closed)
	if err != nil || untilZero.Kind() != timeofday.CollapsedKind {
		t.Fatalf("Until(zero) = %+v, %v", untilZero, err)
	}
	untilFull, err := timeofday.Until(value, 24*time.Hour, temporal.Closed)
	if err != nil || untilFull.Kind() != timeofday.FullDayKind {
		t.Fatalf("Until(day) = %+v, %v", untilFull, err)
	}
	if _, err := timeofday.Around(value, -time.Second, temporal.Closed); !errors.Is(err, temporal.ErrStep) {
		t.Fatalf("Around(negative) error = %v", err)
	}
	aroundZero, err := timeofday.Around(value, 0, temporal.Closed)
	if err != nil || aroundZero.Kind() != timeofday.CollapsedKind {
		t.Fatalf("Around(zero) = %+v, %v", aroundZero, err)
	}
	aroundFull, err := timeofday.Around(value, 12*time.Hour, temporal.Closed)
	if err != nil || aroundFull.Kind() != timeofday.FullDayKind {
		t.Fatalf("Around(half day) = %+v, %v", aroundFull, err)
	}
	if _, err := timeofday.Around(value, time.Duration(1<<62), temporal.Closed); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("Around(overflow) error = %v", err)
	}
}

func TestIntervalToInstantUsesNextDateForCircularCoverage(t *testing.T) {
	date := calendar.MustDate(2026, time.March, 28)
	location, err := time.LoadLocation("Europe/Helsinki")
	if err != nil {
		t.Fatal(err)
	}
	start, _ := timeofday.New(23, 0, 0, 0, 0)
	end, _ := timeofday.New(4, 0, 0, 0, 0)
	interval, _ := timeofday.Between(start, end, temporal.ClosedOpen)
	got, err := interval.ToInstant(date, location, calendartz.Reject)
	if err != nil {
		t.Fatal(err)
	}
	duration, err := got.Duration()
	if err != nil || duration != 4*time.Hour {
		t.Fatalf("circular ToInstant duration = %v, %v", duration, err)
	}

	full, err := timeofday.FullDay().ToInstant(date, location, calendartz.Reject)
	if err != nil || full.Bounds() != temporal.ClosedOpen {
		t.Fatalf("full ToInstant() = %+v, %v", full, err)
	}
	fullDuration, _ := full.Duration()
	if fullDuration != 24*time.Hour {
		t.Fatalf("pre-DST full day duration = %v", fullDuration)
	}
	collapsed, err := timeofday.Collapsed(start).ToInstant(date, location, calendartz.Reject)
	if err != nil || !collapsed.IsEmpty() {
		t.Fatalf("collapsed ToInstant() = %+v, %v", collapsed, err)
	}
}

func TestIntervalToInstantPropagatesCalendarFailures(t *testing.T) {
	date := calendar.MustDate(2026, time.March, 29)
	location, err := time.LoadLocation("Europe/Helsinki")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := timeofday.FullDay().ToInstant(date, nil, calendartz.Reject); !errors.Is(err, calendartz.ErrInvalidLocation) {
		t.Fatalf("FullDay.ToInstant(nil) error = %v", err)
	}
	start := mustTime(t, 1, 0, 0, 0, 0)
	end := mustTime(t, 3, 30, 0, 0, 0)
	ordinary, _ := timeofday.Between(start, end, temporal.ClosedOpen)
	if _, err := ordinary.ToInstant(date, nil, calendartz.Reject); !errors.Is(err, calendartz.ErrInvalidLocation) {
		t.Fatalf("ToInstant(start error) = %v", err)
	}
	if _, err := ordinary.ToInstant(date, location, calendartz.Reject); !errors.Is(err, calendartz.ErrNonexistent) {
		t.Fatalf("ToInstant(end error) = %v", err)
	}
	maximum := calendar.MustDate(9999, time.December, 31)
	circular, _ := timeofday.Between(mustTime(t, 23, 0, 0, 0, 0), mustTime(t, 1, 0, 0, 0, 0), temporal.ClosedOpen)
	if _, err := circular.ToInstant(maximum, time.UTC, calendartz.Reject); err == nil {
		t.Fatal("circular ToInstant accepted maximum date overflow")
	}
}

func TestIntervalSetQueriesExposeNormalizedValues(t *testing.T) {
	a, _ := timeofday.Between(timeofday.Midnight(), mustTime(t, 2, 0, 0, 0, 0), temporal.ClosedOpen)
	b, _ := timeofday.Between(mustTime(t, 4, 0, 0, 0, 0), mustTime(t, 5, 0, 0, 0, 0), temporal.ClosedOpen)
	set, err := timeofday.NewIntervalSet(temporal.DefaultLimits(), b, a)
	if err != nil {
		t.Fatal(err)
	}
	if set.Duration() != 3*time.Hour {
		t.Fatalf("Duration() = %v", set.Duration())
	}
	index, ok := set.Search(mustTime(t, 4, 0, 0, 0, 0))
	if !ok || index != 1 {
		t.Fatalf("Search() = %d, %v", index, ok)
	}
	if _, ok := set.Search(mustTime(t, 3, 0, 0, 0, 0)); ok {
		t.Fatal("Search found gap")
	}
	count := 0
	for range set.All() {
		count++
	}
	if count != 2 {
		t.Fatalf("All count = %d", count)
	}
	for range set.All() {
		break
	}
	gaps, err := set.Gaps()
	if err != nil || !gaps.Includes(mustTime(t, 3, 0, 0, 0, 0)) || gaps.Includes(mustTime(t, 1, 0, 0, 0, 0)) {
		t.Fatalf("Gaps() = %+v, %v", gaps, err)
	}
}
