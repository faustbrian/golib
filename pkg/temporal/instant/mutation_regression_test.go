package instant

import (
	"errors"
	"testing"
	"time"

	calendartz "github.com/faustbrian/golib/pkg/calendar/timezone"
	temporal "github.com/faustbrian/golib/pkg/temporal"
)

func instantAt(hour int) time.Time {
	return time.Date(2026, time.January, 1, hour, 0, 0, 0, time.UTC)
}

func assertInstantLimitError(t *testing.T, err error, field string, value, maximum int) {
	t.Helper()
	var limitError *temporal.LimitError
	if !errors.As(err, &limitError) {
		t.Fatalf("error = %v, want LimitError", err)
	}
	if limitError.Field != field || limitError.Value != value || limitError.Max != maximum {
		t.Fatalf("LimitError = %+v, want field=%q value=%d max=%d", limitError, field, value, maximum)
	}
}

func TestZeroDurationConstructorsAndResizersRemainValid(t *testing.T) {
	t.Parallel()

	for name, construct := range map[string]func() (Period, error){
		"after":  func() (Period, error) { return After(instantAt(1), 0, temporal.Closed) },
		"before": func() (Period, error) { return Before(instantAt(1), 0, temporal.Closed) },
		"around": func() (Period, error) { return Around(instantAt(1), 0, temporal.Closed) },
	} {
		period, err := construct()
		if err != nil || !period.IsSingleton() {
			t.Fatalf("%s zero duration = %+v, %v", name, period, err)
		}
	}

	period, _ := New(instantAt(1), instantAt(3), temporal.ClosedOpen)
	after, err := period.WithDurationAfterStart(0)
	if err != nil || !after.Start().Equal(instantAt(1)) || !after.End().Equal(instantAt(1)) {
		t.Fatalf("WithDurationAfterStart(0) = %+v, %v", after, err)
	}
	before, err := period.WithDurationBeforeEnd(0)
	if err != nil || !before.Start().Equal(instantAt(3)) || !before.End().Equal(instantAt(3)) {
		t.Fatalf("WithDurationBeforeEnd(0) = %+v, %v", before, err)
	}
}

func TestSnapOutwardDistinguishesIncludedExactEnd(t *testing.T) {
	t.Parallel()

	start := instantAt(1).Add(15 * time.Minute)
	end := instantAt(2)
	closed, _ := New(start, end, temporal.Closed)
	open, _ := New(start, end, temporal.ClosedOpen)
	closedResult, err := closed.SnapOutward(Hour, time.UTC, calendartz.Reject)
	if err != nil || !closedResult.End().Equal(instantAt(3)) {
		t.Fatalf("closed SnapOutward() end = %v, %v", closedResult.End(), err)
	}
	openResult, err := open.SnapOutward(Hour, time.UTC, calendartz.Reject)
	if err != nil || !openResult.End().Equal(instantAt(2)) {
		t.Fatalf("open SnapOutward() end = %v, %v", openResult.End(), err)
	}
}

func TestNextISOYearBoundaryAdvancesExactlyOneISOYear(t *testing.T) {
	t.Parallel()

	boundary := time.Date(2025, time.December, 29, 0, 0, 0, 0, time.UTC)
	got, err := nextCivilBoundary(boundary, ISOYear, time.UTC, calendartz.Reject)
	want := time.Date(2027, time.January, 4, 0, 0, 0, 0, time.UTC)
	if err != nil || !got.Equal(want) {
		t.Fatalf("next ISO year = %v, %v; want %v", got, err, want)
	}
}

func TestEmptyOperandsAndEndpointSubtractionRemainExact(t *testing.T) {
	t.Parallel()

	period, _ := New(instantAt(0), instantAt(4), temporal.Closed)
	empty, _ := New(instantAt(2), instantAt(2), temporal.Open)
	if !empty.SetEqual(Period{}) || !(Period{}).SetEqual(empty) || period.SetEqual(empty) || empty.SetEqual(period) {
		t.Fatal("SetEqual did not distinguish empty and non-empty periods")
	}
	if _, ok := period.Intersect(empty); ok {
		t.Fatal("Intersect accepted an empty right operand")
	}
	if _, ok := period.Gap(empty); ok {
		t.Fatal("Gap accepted an empty right operand")
	}
	if _, ok := empty.Gap(period); ok {
		t.Fatal("Gap accepted an empty left operand")
	}

	interior, _ := New(instantAt(0), instantAt(4), temporal.Open)
	fragments := period.Subtract(interior)
	if len(fragments) != 2 || !fragments[0].IsSingleton() || !fragments[0].Includes(instantAt(0)) ||
		!fragments[1].IsSingleton() || !fragments[1].Includes(instantAt(4)) {
		t.Fatalf("endpoint subtraction = %+v", fragments)
	}
}

func TestSetSearchAndLimitsUseExactBoundaries(t *testing.T) {
	t.Parallel()

	periods := []Period{
		mustInternalPeriod(t, 1, 2, temporal.ClosedOpen),
		mustInternalPeriod(t, 3, 4, temporal.ClosedOpen),
	}
	set, err := NewSet(temporal.Limits{InputPeriods: 2, OutputPeriods: 2}, periods...)
	if err != nil {
		t.Fatalf("NewSet(exact limits): %v", err)
	}
	for _, test := range []struct {
		hour  int
		index int
		found bool
	}{{0, 0, false}, {1, 0, true}, {2, 0, false}, {3, 1, true}, {4, 1, false}, {5, 2, false}} {
		index, found := set.Search(instantAt(test.hour))
		if index != test.index || found != test.found {
			t.Fatalf("Search(%d) = %d, %v; want %d, %v", test.hour, index, found, test.index, test.found)
		}
	}

	_, err = NewSet(temporal.Limits{InputPeriods: 1}, periods...)
	assertInstantLimitError(t, err, "input_periods", 2, 1)
	_, err = NewSet(temporal.Limits{OutputPeriods: 1}, periods...)
	assertInstantLimitError(t, err, "output_periods", 2, 1)
}

func TestSetOperationLimitErrorsReportAttemptedCardinality(t *testing.T) {
	t.Parallel()

	limited, err := NewSet(
		temporal.Limits{InputPeriods: 3, OutputPeriods: 1},
		mustInternalPeriod(t, 0, 5, temporal.Closed),
	)
	if err != nil {
		t.Fatalf("NewSet(limited): %v", err)
	}
	disjoint, _ := NewSet(temporal.Limits{},
		mustInternalPeriod(t, 0, 1, temporal.Closed),
		mustInternalPeriod(t, 3, 4, temporal.Closed),
	)
	_, err = limited.Intersect(disjoint)
	assertInstantLimitError(t, err, "output_periods", 2, 1)

	removed, _ := NewSet(temporal.Limits{}, mustInternalPeriod(t, 2, 2, temporal.Closed))
	_, err = limited.Subtract(removed)
	assertInstantLimitError(t, err, "output_periods", 2, 1)
	limitedTwo, err := NewSet(
		temporal.Limits{InputPeriods: 3, OutputPeriods: 2},
		mustInternalPeriod(t, 0, 5, temporal.Closed),
	)
	if err != nil {
		t.Fatalf("NewSet(limited two): %v", err)
	}
	difference, err := limitedTwo.Subtract(removed)
	if err != nil || difference.Len() != 2 {
		t.Fatalf("Subtract(exact output limit) = %+v, %v", difference.Periods(), err)
	}
}

func TestTotalDurationAcceptsTheExactMaximum(t *testing.T) {
	t.Parallel()

	maximum := time.Duration(1<<63 - 1)
	start := time.Unix(0, 0).UTC()
	set := Set{periods: []Period{{start: start, end: start.Add(maximum), bounds: temporal.ClosedOpen}}}
	got, err := set.TotalDuration()
	if err != nil || got != maximum {
		t.Fatalf("TotalDuration(maximum) = %v, %v", got, err)
	}
}

func TestPeriodOrderingCoversEveryTieBreaker(t *testing.T) {
	t.Parallel()

	earlier := mustInternalPeriod(t, 0, 2, temporal.Closed)
	later := mustInternalPeriod(t, 1, 2, temporal.Closed)
	if !lessPeriod(earlier, later) || lessPeriod(later, earlier) {
		t.Fatal("lessPeriod did not order starts")
	}
	closedStart := mustInternalPeriod(t, 0, 2, temporal.ClosedOpen)
	openStart := mustInternalPeriod(t, 0, 2, temporal.Open)
	if !lessPeriod(closedStart, openStart) || lessPeriod(openStart, closedStart) {
		t.Fatal("lessPeriod did not order start inclusion")
	}
	short := mustInternalPeriod(t, 0, 1, temporal.Closed)
	long := mustInternalPeriod(t, 0, 2, temporal.Closed)
	if !lessPeriod(short, long) || lessPeriod(long, short) {
		t.Fatal("lessPeriod did not order ends")
	}
	closedEnd := mustInternalPeriod(t, 0, 2, temporal.Closed)
	openEnd := mustInternalPeriod(t, 0, 2, temporal.ClosedOpen)
	if !lessPeriod(closedEnd, openEnd) || lessPeriod(openEnd, closedEnd) || lessPeriod(closedEnd, closedEnd) {
		t.Fatal("lessPeriod did not order end inclusion")
	}
}

func TestSplitLimitsIdentifyTheActiveExactLimit(t *testing.T) {
	t.Parallel()

	assertInstantLimitError(t, splitLimitError(3, temporal.Limits{Steps: 2, OutputPeriods: 4}), "steps", 3, 2)
	assertInstantLimitError(t, splitLimitError(3, temporal.Limits{Steps: 4, OutputPeriods: 2}), "output_periods", 3, 2)
	assertInstantLimitError(t, splitLimitError(3, temporal.Limits{Steps: 2, OutputPeriods: 2}), "steps", 3, 2)

	period := mustInternalPeriod(t, 0, 3, temporal.ClosedOpen)
	_, err := period.SplitForward(time.Hour, temporal.Limits{Steps: 2})
	assertInstantLimitError(t, err, "steps", 3, 2)
	_, err = period.SplitBackward(time.Hour, temporal.Limits{OutputPeriods: 2})
	assertInstantLimitError(t, err, "output_periods", 3, 2)
}

func mustInternalPeriod(t *testing.T, startHour, endHour int, bounds temporal.Bounds) Period {
	t.Helper()
	period, err := New(instantAt(startHour), instantAt(endHour), bounds)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return period
}
