package timeofday_test

import (
	"errors"
	"testing"
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
)

func hm(t *testing.T, hour, minute int) timeofday.Time {
	t.Helper()
	return mustTime(t, hour, minute, 0, 0, 0)
}

func mustInterval(t *testing.T, startHour, endHour int, bounds temporal.Bounds) timeofday.Interval {
	t.Helper()

	interval, err := timeofday.Between(hm(t, startHour, 0), hm(t, endHour, 0), bounds)
	if err != nil {
		t.Fatalf("Between(%d,%d,%v): %v", startHour, endHour, bounds, err)
	}

	return interval
}

func TestIntervalKindsAreExplicit(t *testing.T) {
	t.Parallel()

	ordinary := mustInterval(t, 8, 17, temporal.ClosedOpen)
	circular := mustInterval(t, 22, 2, temporal.ClosedOpen)
	collapsed := timeofday.Collapsed(hm(t, 8, 0))
	full := timeofday.FullDay()

	if ordinary.Kind() != timeofday.Ordinary || ordinary.Duration() != 9*time.Hour {
		t.Fatalf("ordinary = %+v, %v", ordinary, ordinary.Duration())
	}
	if circular.Kind() != timeofday.Circular || circular.Duration() != 4*time.Hour {
		t.Fatalf("circular = %+v, %v", circular, circular.Duration())
	}
	if collapsed.Kind() != timeofday.CollapsedKind || collapsed.Duration() != 0 {
		t.Fatalf("collapsed = %+v, %v", collapsed, collapsed.Duration())
	}
	if full.Kind() != timeofday.FullDayKind || full.Duration() != 24*time.Hour {
		t.Fatalf("full = %+v, %v", full, full.Duration())
	}
	if collapsed.SetEqual(full) {
		t.Fatal("collapsed interval silently equaled full day")
	}
	if !ordinary.Start().Equal(hm(t, 8, 0)) || !ordinary.End().Equal(hm(t, 17, 0)) || ordinary.Bounds() != temporal.ClosedOpen {
		t.Fatal("interval accessors changed construction values")
	}
	if !ordinary.Equal(mustInterval(t, 8, 17, temporal.ClosedOpen)) || ordinary.Equal(circular) {
		t.Fatal("structural interval equality failed")
	}
}

func TestIntervalKindNamesAreStable(t *testing.T) {
	t.Parallel()

	tests := map[timeofday.IntervalKind]string{
		timeofday.Ordinary:      "Ordinary",
		timeofday.Circular:      "Circular",
		timeofday.CollapsedKind: "Collapsed",
		timeofday.FullDayKind:   "FullDay",
		99:                      "",
	}
	for kind, want := range tests {
		if got := kind.String(); got != want {
			t.Fatalf("%d.String() = %q; want %q", kind, got, want)
		}
	}
}

func TestBetweenRejectsEqualEndpointsAndInvalidBounds(t *testing.T) {
	t.Parallel()

	value := hm(t, 8, 0)
	if _, err := timeofday.Between(value, value, temporal.ClosedOpen); !errors.Is(err, temporal.ErrInvalidTime) {
		t.Fatalf("Between(equal) error = %v", err)
	}
	if _, err := timeofday.Between(value, hm(t, 9, 0), temporal.Bounds(255)); !errors.Is(err, temporal.ErrBounds) {
		t.Fatalf("Between(bounds) error = %v", err)
	}
}

func TestCircularIncludesAcrossMidnightWithExactBounds(t *testing.T) {
	t.Parallel()

	interval := mustInterval(t, 22, 2, temporal.OpenClosed)
	for _, included := range []timeofday.Time{hm(t, 23, 0), timeofday.Midnight(), timeofday.EndOfDay(), hm(t, 2, 0)} {
		if !interval.Includes(included) {
			t.Fatalf("circular interval excluded %v", included)
		}
	}
	if interval.Includes(hm(t, 22, 0)) || interval.Includes(hm(t, 12, 0)) {
		t.Fatal("circular interval ignored its start bound or included daytime")
	}

	full := timeofday.FullDay()
	if !full.Includes(timeofday.Midnight()) || !full.Includes(timeofday.EndOfDay()) {
		t.Fatal("full-day universe excluded a day boundary")
	}
	if timeofday.Collapsed(hm(t, 1, 0)).Includes(hm(t, 1, 0)) {
		t.Fatal("collapsed interval included its endpoint")
	}
}

func TestOrdinaryIncludesEveryBoundaryCombination(t *testing.T) {
	t.Parallel()

	for _, bounds := range temporal.AllBounds() {
		interval := mustInterval(t, 8, 17, bounds)
		if got := interval.Includes(hm(t, 8, 0)); got != bounds.IncludesStart() {
			t.Fatalf("%v start inclusion = %v", bounds, got)
		}
		if got := interval.Includes(hm(t, 17, 0)); got != bounds.IncludesEnd() {
			t.Fatalf("%v end inclusion = %v", bounds, got)
		}
		if !interval.Includes(hm(t, 12, 0)) || interval.Includes(hm(t, 7, 0)) || interval.Includes(hm(t, 18, 0)) {
			t.Fatalf("%v ordinary range membership failed", bounds)
		}
	}
}

func TestIntervalSetNormalizesCircularAndOrdinaryInputs(t *testing.T) {
	t.Parallel()

	set, err := timeofday.NewIntervalSet(
		temporal.Limits{},
		mustInterval(t, 22, 2, temporal.ClosedOpen),
		mustInterval(t, 1, 4, temporal.ClosedOpen),
		timeofday.Collapsed(hm(t, 12, 0)),
	)
	if err != nil {
		t.Fatalf("NewIntervalSet(): %v", err)
	}
	if set.Len() != 2 {
		t.Fatalf("Len() = %d, want two linear normalized segments", set.Len())
	}
	if !set.Intervals()[1].End().IsEndBoundary() {
		t.Fatal("linearized circular interval did not retain the day-end boundary")
	}
	if !set.Includes(hm(t, 0, 0)) || !set.Includes(hm(t, 3, 0)) || !set.Includes(hm(t, 23, 0)) || set.Includes(hm(t, 12, 0)) {
		t.Fatal("normalized set represented the wrong daily members")
	}
}

func TestIntervalComplementIsInvolutive(t *testing.T) {
	t.Parallel()

	interval := mustInterval(t, 8, 17, temporal.ClosedOpen)
	set, err := timeofday.NewIntervalSet(temporal.Limits{}, interval)
	if err != nil {
		t.Fatalf("NewIntervalSet(): %v", err)
	}
	complement, err := set.Complement()
	if err != nil {
		t.Fatalf("Complement(): %v", err)
	}
	twice, err := complement.Complement()
	if err != nil {
		t.Fatalf("second Complement(): %v", err)
	}
	if !twice.Equal(set) {
		t.Fatalf("complement was not involutive: %+v / %+v", twice.Intervals(), set.Intervals())
	}

	empty, _ := timeofday.NewIntervalSet(temporal.Limits{})
	whole, err := empty.Complement()
	if err != nil || !whole.Equal(mustDailySet(t, timeofday.FullDay())) {
		t.Fatalf("empty complement = %+v, %v", whole.Intervals(), err)
	}
	wholeIntervals := whole.Intervals()
	if len(wholeIntervals) != 1 || wholeIntervals[0].Kind() != timeofday.FullDayKind {
		t.Fatalf("full universe materialized as %+v", wholeIntervals)
	}
	back, err := whole.Complement()
	if err != nil || back.Len() != 0 {
		t.Fatalf("full complement = %+v, %v", back.Intervals(), err)
	}
}

func mustDailySet(t *testing.T, intervals ...timeofday.Interval) timeofday.IntervalSet {
	t.Helper()
	set, err := timeofday.NewIntervalSet(temporal.Limits{}, intervals...)
	if err != nil {
		t.Fatalf("NewIntervalSet(): %v", err)
	}
	return set
}

func TestDailySetIntersectionUnionAndDifference(t *testing.T) {
	t.Parallel()

	a := mustDailySet(t, mustInterval(t, 8, 18, temporal.ClosedOpen))
	b := mustDailySet(t, mustInterval(t, 12, 22, temporal.ClosedOpen))

	intersection, err := a.Intersect(b)
	if err != nil || !intersection.Includes(hm(t, 12, 0)) || intersection.Includes(hm(t, 18, 0)) {
		t.Fatalf("Intersect() = %+v, %v", intersection.Intervals(), err)
	}
	union, err := a.Union(b)
	if err != nil || union.Len() != 1 || !union.Includes(hm(t, 21, 0)) {
		t.Fatalf("Union() = %+v, %v", union.Intervals(), err)
	}
	difference, err := a.Subtract(b)
	if err != nil || !difference.Includes(hm(t, 8, 0)) || difference.Includes(hm(t, 12, 0)) {
		t.Fatalf("Subtract() = %+v, %v", difference.Intervals(), err)
	}

	if reverse, err := b.Intersect(a); err != nil || !reverse.Equal(intersection) {
		t.Fatal("intersection was not commutative")
	}
	if reverse, err := b.Union(a); err != nil || !reverse.Equal(union) {
		t.Fatal("union was not commutative")
	}
}

func TestDailySetCopiesOutputsAndEnforcesExpansionLimits(t *testing.T) {
	t.Parallel()

	set := mustDailySet(t, mustInterval(t, 8, 9, temporal.ClosedOpen))
	items := set.Intervals()
	items[0] = timeofday.FullDay()
	if set.Includes(hm(t, 12, 0)) {
		t.Fatal("returned slice mutated interval set")
	}

	if _, err := timeofday.NewIntervalSet(
		temporal.Limits{InputPeriods: 1},
		mustInterval(t, 1, 2, temporal.Open),
		mustInterval(t, 3, 4, temporal.Open),
	); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("input limit error = %v", err)
	}
	if _, err := timeofday.NewIntervalSet(
		temporal.Limits{OutputPeriods: 1},
		mustInterval(t, 22, 2, temporal.ClosedOpen),
	); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("circular expansion limit error = %v", err)
	}
	if _, err := timeofday.NewIntervalSet(temporal.Limits{Precision: 10}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("invalid limits error = %v", err)
	}
}

func TestDailySetZeroValueAndAllOutputBoundsAreSafe(t *testing.T) {
	t.Parallel()

	set, err := timeofday.NewIntervalSet(temporal.Limits{}, timeofday.Interval{})
	if err != nil || set.Len() != 0 {
		t.Fatalf("zero interval set = %+v, %v", set.Intervals(), err)
	}

	for _, bounds := range temporal.AllBounds() {
		set := mustDailySet(t, mustInterval(t, 8, 17, bounds))
		items := set.Intervals()
		if len(items) != 1 || items[0].Bounds() != bounds {
			t.Fatalf("Intervals(%v) = %+v", bounds, items)
		}
		if set.Includes(hm(t, 7, 0)) {
			t.Fatalf("set with %v included an outside value", bounds)
		}
		if got := set.Includes(hm(t, 8, 0)); got != bounds.IncludesStart() {
			t.Fatalf("set with %v start inclusion = %v", bounds, got)
		}
		if got := set.Includes(hm(t, 17, 0)); got != bounds.IncludesEnd() {
			t.Fatalf("set with %v end inclusion = %v", bounds, got)
		}
	}
}

func TestDailySetCanonicalizesEqualStartsAndEnds(t *testing.T) {
	t.Parallel()

	set := mustDailySet(t,
		mustInterval(t, 8, 18, temporal.Open),
		mustInterval(t, 8, 12, temporal.ClosedOpen),
		mustInterval(t, 8, 18, temporal.OpenClosed),
		mustInterval(t, 8, 20, temporal.ClosedOpen),
	)
	if set.Len() != 1 || !set.Includes(hm(t, 8, 0)) || set.Includes(hm(t, 20, 0)) {
		t.Fatalf("canonical set = %+v", set.Intervals())
	}

	equalEnds := mustDailySet(t,
		mustInterval(t, 8, 18, temporal.Open),
		mustInterval(t, 8, 18, temporal.OpenClosed),
	)
	if equalEnds.Len() != 1 || !equalEnds.Includes(hm(t, 18, 0)) {
		t.Fatalf("equal-end merge = %+v", equalEnds.Intervals())
	}
}

func TestDailySetEqualityRejectsDifferentLengthsAndMembers(t *testing.T) {
	t.Parallel()

	a := mustDailySet(t, mustInterval(t, 8, 9, temporal.ClosedOpen))
	b := mustDailySet(t)
	c := mustDailySet(t, mustInterval(t, 10, 11, temporal.ClosedOpen))
	if a.Equal(b) || a.Equal(c) {
		t.Fatal("different daily sets compared equal")
	}
}

func TestDailyAlgebraEnforcesOutputLimits(t *testing.T) {
	t.Parallel()

	limited, err := timeofday.NewIntervalSet(
		temporal.Limits{OutputPeriods: 1},
		mustInterval(t, 0, 23, temporal.Closed),
	)
	if err != nil {
		t.Fatalf("NewIntervalSet(limited): %v", err)
	}
	disjoint := mustDailySet(t,
		mustInterval(t, 1, 2, temporal.Closed),
		mustInterval(t, 3, 4, temporal.Closed),
	)
	if _, err := limited.Intersect(disjoint); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Intersect(limit) error = %v", err)
	}
	point, err := timeofday.Between(hm(t, 2, 0), hm(t, 3, 0), temporal.Closed)
	if err != nil {
		t.Fatalf("Between(): %v", err)
	}
	removed := mustDailySet(t, point)
	cover, err := timeofday.NewIntervalSet(
		temporal.Limits{OutputPeriods: 1},
		mustInterval(t, 1, 4, temporal.Closed),
	)
	if err != nil {
		t.Fatalf("NewIntervalSet(cover): %v", err)
	}
	if _, err := cover.Subtract(removed); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Subtract(limit) error = %v", err)
	}
}

func TestIntervalPredicatesUseRepresentedSets(t *testing.T) {
	t.Parallel()

	outer := mustInterval(t, 22, 4, temporal.ClosedOpen)
	inner := mustInterval(t, 23, 2, temporal.OpenClosed)
	disjoint := mustInterval(t, 8, 10, temporal.ClosedOpen)
	if !outer.Contains(inner) || !outer.Overlaps(inner) {
		t.Fatal("circular containment or overlap failed")
	}
	if outer.Contains(disjoint) || outer.Overlaps(disjoint) {
		t.Fatal("disjoint interval satisfied a set predicate")
	}
	if !mustInterval(t, 8, 10, temporal.ClosedOpen).Abuts(
		mustInterval(t, 10, 12, temporal.ClosedOpen),
	) {
		t.Fatal("adjacent intervals did not abut")
	}
}

func TestIntervalWithBoundsShiftAndExpandAreImmutable(t *testing.T) {
	t.Parallel()

	original := mustInterval(t, 22, 2, temporal.ClosedOpen)
	bounded, err := original.WithBounds(temporal.OpenClosed)
	if err != nil || bounded.Bounds() != temporal.OpenClosed || original.Bounds() != temporal.ClosedOpen {
		t.Fatalf("WithBounds() = %+v, %v", bounded, err)
	}
	if _, err := original.WithBounds(temporal.Bounds(255)); !errors.Is(err, temporal.ErrBounds) {
		t.Fatalf("WithBounds(invalid) error = %v", err)
	}
	if got, err := timeofday.FullDay().WithBounds(temporal.Open); err != nil || got.Kind() != timeofday.FullDayKind {
		t.Fatalf("full WithBounds() = %+v, %v", got, err)
	}

	shifted, err := original.Shift(3 * time.Hour)
	if err != nil || shifted.Kind() != timeofday.Ordinary ||
		!shifted.Start().Equal(hm(t, 1, 0)) || !shifted.End().Equal(hm(t, 5, 0)) {
		t.Fatalf("Shift() = %+v, %v", shifted, err)
	}
	expanded, err := original.Expand(time.Hour, 2*time.Hour)
	if err != nil || expanded.Duration() != 7*time.Hour ||
		!expanded.Start().Equal(hm(t, 21, 0)) || !expanded.End().Equal(hm(t, 4, 0)) {
		t.Fatalf("Expand() = %+v, %v", expanded, err)
	}
	whole, err := original.Expand(10*time.Hour, 10*time.Hour)
	if err != nil || whole.Kind() != timeofday.FullDayKind {
		t.Fatalf("Expand(full) = %+v, %v", whole, err)
	}
	if _, err := original.Expand(-3*time.Hour, -2*time.Hour); !errors.Is(err, temporal.ErrEmpty) {
		t.Fatalf("Expand(empty) error = %v", err)
	}

	collapsed := timeofday.Collapsed(hm(t, 23, 0))
	shiftedCollapsed, err := collapsed.Shift(2 * time.Hour)
	if err != nil || shiftedCollapsed.Kind() != timeofday.CollapsedKind ||
		!shiftedCollapsed.Start().Equal(hm(t, 1, 0)) {
		t.Fatalf("collapsed Shift() = %+v, %v", shiftedCollapsed, err)
	}
	shiftedFull, err := timeofday.FullDay().Shift(time.Hour)
	if err != nil || shiftedFull.Kind() != timeofday.FullDayKind {
		t.Fatalf("full Shift() = %+v, %v", shiftedFull, err)
	}
	dayRange, err := timeofday.Between(timeofday.Midnight(), timeofday.EndOfDay(), temporal.ClosedOpen)
	if err != nil {
		t.Fatalf("Between(day range): %v", err)
	}
	if _, err := dayRange.Shift(time.Hour); !errors.Is(err, temporal.ErrInvalidTime) {
		t.Fatalf("day-range Shift() error = %v", err)
	}
	if _, err := original.Expand(time.Duration(1<<63-1), 0); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("Expand(first overflow) error = %v", err)
	}
	if _, err := original.Expand(-3*time.Hour, time.Duration(1<<63-1)); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("Expand(second overflow) error = %v", err)
	}
	if _, err := original.Expand(time.Duration(-1<<63), 0); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("Expand(negation overflow) error = %v", err)
	}
}

func TestIntervalEndpointReplacementPreservesExplicitKinds(t *testing.T) {
	interval := mustInterval(t, 8, 17, temporal.ClosedOpen)
	start := hm(t, 9, 0)
	end := hm(t, 18, 0)
	withStart, err := interval.WithStart(start)
	if err != nil || !withStart.Start().Equal(start) || !interval.Start().Equal(hm(t, 8, 0)) {
		t.Fatalf("WithStart() = %+v, %v", withStart, err)
	}
	withEnd, err := interval.WithEnd(end)
	if err != nil || !withEnd.End().Equal(end) || !interval.End().Equal(hm(t, 17, 0)) {
		t.Fatalf("WithEnd() = %+v, %v", withEnd, err)
	}
	collapsed := timeofday.Collapsed(hm(t, 12, 0))
	moved, err := collapsed.WithStart(start)
	if err != nil || moved.Kind() != timeofday.CollapsedKind || !moved.Start().Equal(start) {
		t.Fatalf("collapsed WithStart() = %+v, %v", moved, err)
	}
	moved, err = collapsed.WithEnd(end)
	if err != nil || moved.Kind() != timeofday.CollapsedKind || !moved.End().Equal(end) {
		t.Fatalf("collapsed WithEnd() = %+v, %v", moved, err)
	}
	if _, err := timeofday.FullDay().WithStart(start); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("full-day WithStart error = %v", err)
	}
	if _, err := timeofday.FullDay().WithEnd(end); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("full-day WithEnd error = %v", err)
	}
}

func TestIntervalSetOperationsAndGapPreserveExactBounds(t *testing.T) {
	t.Parallel()

	left := mustInterval(t, 8, 12, temporal.ClosedOpen)
	right := mustInterval(t, 12, 16, temporal.OpenClosed)
	intersection, err := left.Intersection(right, temporal.Limits{})
	if err != nil || intersection.Len() != 0 {
		t.Fatalf("Intersection() = %+v, %v", intersection.Intervals(), err)
	}
	union, err := left.Union(right, temporal.Limits{})
	if err != nil || union.Len() != 2 {
		t.Fatalf("Union() = %+v, %v", union.Intervals(), err)
	}
	difference, err := left.Difference(right, temporal.Limits{})
	if err != nil || !difference.Equal(mustDailySet(t, left)) {
		t.Fatalf("Difference() = %+v, %v", difference.Intervals(), err)
	}
	gap, err := left.Gap(right)
	if err != nil || gap.Kind() != timeofday.CollapsedKind || !gap.Start().Equal(hm(t, 12, 0)) {
		t.Fatalf("Gap(abutting) = %+v, %v", gap, err)
	}

	disjoint := mustInterval(t, 14, 16, temporal.ClosedOpen)
	gap, err = left.Gap(disjoint)
	if err != nil || gap.Bounds() != temporal.ClosedOpen ||
		!gap.Start().Equal(hm(t, 12, 0)) || !gap.End().Equal(hm(t, 14, 0)) {
		t.Fatalf("Gap(disjoint) = %+v, %v", gap, err)
	}
	if _, err := left.Gap(mustInterval(t, 10, 15, temporal.ClosedOpen)); !errors.Is(err, temporal.ErrEmpty) {
		t.Fatalf("Gap(overlap) error = %v", err)
	}
	reverse, err := disjoint.Gap(left)
	if err != nil || !reverse.SetEqual(gap) {
		t.Fatalf("Gap(reverse) = %+v, %v", reverse, err)
	}
	if _, err := left.Gap(mustInterval(t, 22, 2, temporal.ClosedOpen)); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("Gap(circular) error = %v", err)
	}

	circular := mustInterval(t, 22, 2, temporal.ClosedOpen)
	if _, err := circular.Intersection(left, temporal.Limits{OutputPeriods: 1}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Intersection(left limit) error = %v", err)
	}
	if _, err := left.Intersection(circular, temporal.Limits{OutputPeriods: 1}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Intersection(right limit) error = %v", err)
	}
	if _, err := circular.Difference(left, temporal.Limits{OutputPeriods: 1}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Difference(left limit) error = %v", err)
	}
	if _, err := left.Difference(circular, temporal.Limits{OutputPeriods: 1}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Difference(right limit) error = %v", err)
	}
}

func TestIntervalSplitAndStepsAreBoundedAndCrossMidnight(t *testing.T) {
	t.Parallel()

	interval := mustInterval(t, 22, 2, temporal.OpenClosed)
	parts, err := interval.Split(90*time.Minute, temporal.Limits{})
	if err != nil || len(parts) != 3 {
		t.Fatalf("Split() = %+v, %v", parts, err)
	}
	combined, err := timeofday.NewIntervalSet(temporal.Limits{}, parts...)
	if err != nil || !combined.Equal(mustDailySet(t, interval)) {
		t.Fatalf("split conservation = %+v, %v", combined.Intervals(), err)
	}
	steps, err := interval.Steps(time.Hour, temporal.Limits{})
	if err != nil || len(steps) != 4 || !steps[0].Equal(hm(t, 23, 0)) ||
		!steps[1].Equal(timeofday.Midnight()) || !steps[3].Equal(hm(t, 2, 0)) {
		t.Fatalf("Steps() = %+v, %v", steps, err)
	}

	for _, step := range []time.Duration{0, -time.Second} {
		if _, err := interval.Split(step, temporal.Limits{}); !errors.Is(err, temporal.ErrStep) {
			t.Fatalf("Split(%v) error = %v", step, err)
		}
		if _, err := interval.Steps(step, temporal.Limits{}); !errors.Is(err, temporal.ErrStep) {
			t.Fatalf("Steps(%v) error = %v", step, err)
		}
	}
	if _, err := interval.Split(time.Hour, temporal.Limits{Steps: 3}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Split(limit) error = %v", err)
	}
	if _, err := interval.Steps(time.Hour, temporal.Limits{Steps: 3}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Steps(limit) error = %v", err)
	}
	if _, err := interval.Split(time.Hour, temporal.Limits{Precision: 10}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Split(invalid limits) error = %v", err)
	}
	if parts, err := timeofday.Collapsed(hm(t, 3, 0)).Split(time.Hour, temporal.Limits{}); err != nil || len(parts) != 0 {
		t.Fatalf("collapsed Split() = %+v, %v", parts, err)
	}
	if steps, err := timeofday.Collapsed(hm(t, 3, 0)).Steps(time.Hour, temporal.Limits{}); err != nil || len(steps) != 0 {
		t.Fatalf("collapsed Steps() = %+v, %v", steps, err)
	}
	if _, err := interval.Steps(time.Hour, temporal.Limits{Precision: 10}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Steps(invalid limits) error = %v", err)
	}
	endRange, err := timeofday.Between(hm(t, 22, 0), timeofday.EndOfDay(), temporal.Closed)
	if err != nil {
		t.Fatalf("Between(end range): %v", err)
	}
	endSteps, err := endRange.Steps(2*time.Hour, temporal.Limits{})
	if err != nil || len(endSteps) != 2 || !endSteps[1].IsEndBoundary() {
		t.Fatalf("end Steps() = %+v, %v", endSteps, err)
	}
}
