package dateperiod_test

import (
	"errors"
	"testing"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/dateperiod"
)

func assertDatePeriods(t *testing.T, got []dateperiod.Period, want ...[2]int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("period count = %d, want %d: %+v", len(got), len(want), got)
	}
	for index, endpoints := range want {
		if !got[index].Start().Equal(dayOffset(endpoints[0])) ||
			!got[index].End().Equal(dayOffset(endpoints[1])) ||
			got[index].Bounds() != temporal.Closed {
			t.Fatalf("period %d = %s..%s %v, want offsets %d..%d closed",
				index, got[index].Start(), got[index].End(), got[index].Bounds(), endpoints[0], endpoints[1])
		}
	}
}

func assertDateLimitError(t *testing.T, err error, field string, value, maximum int) {
	t.Helper()
	var limitError *temporal.LimitError
	if !errors.As(err, &limitError) {
		t.Fatalf("error = %v, want LimitError", err)
	}
	if limitError.Field != field || limitError.Value != value || limitError.Max != maximum {
		t.Fatalf("LimitError = %+v, want field=%q value=%d max=%d", limitError, field, value, maximum)
	}
}

func TestDatePeriodBoundaryAlgebraReturnsExactFragments(t *testing.T) {
	t.Parallel()

	outer := mustDatePeriod(t, dayOffset(1), dayOffset(7), temporal.Closed)
	inner := mustDatePeriod(t, dayOffset(3), dayOffset(5), temporal.Closed)
	left := mustDatePeriod(t, dayOffset(0), dayOffset(2), temporal.Closed)
	right := mustDatePeriod(t, dayOffset(6), dayOffset(8), temporal.Closed)

	intersection, ok := outer.Intersect(inner)
	if !ok || !intersection.Equal(inner) {
		t.Fatalf("Intersect(inner) = %+v, %v", intersection, ok)
	}
	assertDatePeriods(t, outer.Subtract(inner), [2]int{1, 2}, [2]int{6, 7})
	assertDatePeriods(t, outer.Subtract(left), [2]int{3, 7})
	assertDatePeriods(t, outer.Subtract(right), [2]int{1, 5})

	if outer.Contains(left) || left.Contains(outer) || !outer.Contains(inner) {
		t.Fatal("Contains did not distinguish overlap from full containment")
	}
	if left.IsBefore(dateperiod.Period{}) || left.IsAfter(dateperiod.Period{}) ||
		left.Starts(dateperiod.Period{}) || left.Finishes(dateperiod.Period{}) {
		t.Fatal("relation predicate accepted an empty period")
	}
}

func TestDatePeriodEmptyOperandsRemainOutsideNonEmptySets(t *testing.T) {
	t.Parallel()

	period := mustDatePeriod(t, dayOffset(1), dayOffset(2), temporal.Closed)
	empty := mustDatePeriod(t, dayOffset(1), dayOffset(1), temporal.Open)
	if period.SetEqual(empty) {
		t.Fatal("non-empty period is set-equal to an empty period")
	}
	if empty.Includes(dayOffset(1)) {
		t.Fatal("empty period includes its excluded anchor")
	}
}

func TestDatePeriodMergeAndGapChooseExactOuterEndpoints(t *testing.T) {
	t.Parallel()

	left := mustDatePeriod(t, dayOffset(1), dayOffset(3), temporal.Closed)
	right := mustDatePeriod(t, dayOffset(6), dayOffset(9), temporal.Closed)
	merged := left.Merge(right)
	if !merged.Start().Equal(dayOffset(1)) || !merged.End().Equal(dayOffset(9)) {
		t.Fatalf("Merge() = %s..%s", merged.Start(), merged.End())
	}
	merged = right.Merge(left)
	if !merged.Start().Equal(dayOffset(1)) || !merged.End().Equal(dayOffset(9)) {
		t.Fatalf("reverse Merge() = %s..%s", merged.Start(), merged.End())
	}

	gap, ok := right.Gap(left)
	if !ok {
		t.Fatal("Gap() did not find disjoint dates")
	}
	assertDatePeriods(t, []dateperiod.Period{gap}, [2]int{4, 5})

	empty := mustDatePeriod(t, dayOffset(4), dayOffset(4), temporal.Open)
	if _, ok := left.Gap(empty); ok {
		t.Fatal("Gap() accepted an empty right operand")
	}
}

func TestDateSetSearchReturnsEveryStableBoundaryIndex(t *testing.T) {
	t.Parallel()

	set := mustDateSet(t,
		mustDatePeriod(t, dayOffset(2), dayOffset(4), temporal.Closed),
		mustDatePeriod(t, dayOffset(7), dayOffset(9), temporal.Closed),
	)
	tests := []struct {
		offset int
		index  int
		found  bool
	}{
		{-1, 0, false},
		{2, 0, true},
		{3, 0, true},
		{4, 0, true},
		{5, 1, false},
		{6, 1, false},
		{7, 1, true},
		{9, 1, true},
		{10, 2, false},
	}
	for _, test := range tests {
		index, found := set.Search(dayOffset(test.offset))
		if index != test.index || found != test.found {
			t.Fatalf("Search(%d) = %d, %v; want %d, %v", test.offset, index, found, test.index, test.found)
		}
	}
}

func TestDateSetNormalizationAndLimitsUseExactBoundaries(t *testing.T) {
	t.Parallel()

	separated := []dateperiod.Period{
		mustDatePeriod(t, dayOffset(0), dayOffset(1), temporal.Closed),
		mustDatePeriod(t, dayOffset(3), dayOffset(4), temporal.Closed),
	}
	set, err := dateperiod.NewSet(temporal.Limits{InputPeriods: 2, OutputPeriods: 2}, separated...)
	if err != nil {
		t.Fatalf("NewSet(exact limits): %v", err)
	}
	assertDatePeriods(t, set.Periods(), [2]int{0, 1}, [2]int{3, 4})
	if set.TotalDays() != 4 {
		t.Fatalf("TotalDays() = %d, want 4", set.TotalDays())
	}
	if _, err := dateperiod.NewSet(temporal.Limits{InputPeriods: 1}, separated...); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("NewSet(input limit) error = %v", err)
	}
	_, err = dateperiod.NewSet(temporal.Limits{OutputPeriods: 1}, separated...)
	assertDateLimitError(t, err, "output_periods", 2, 1)

	adjacent := mustDateSet(t,
		mustDatePeriod(t, dayOffset(0), dayOffset(1), temporal.Closed),
		mustDatePeriod(t, dayOffset(2), dayOffset(3), temporal.Closed),
	)
	assertDatePeriods(t, adjacent.Periods(), [2]int{0, 3})
	if gaps := set.Gaps(); len(gaps) != 1 {
		t.Fatalf("Gaps() count = %d, want 1", len(gaps))
	} else {
		assertDatePeriods(t, gaps, [2]int{2, 2})
	}
}

func TestDateSetOperationsEnforceExactOutputLimit(t *testing.T) {
	t.Parallel()

	limited := mustDateSet(t, mustDatePeriod(t, dayOffset(0), dayOffset(8), temporal.Closed))
	limited, err := dateperiod.NewSet(
		temporal.Limits{InputPeriods: 3, OutputPeriods: 2},
		limited.Periods()...,
	)
	if err != nil {
		t.Fatalf("NewSet(limited): %v", err)
	}
	removed := mustDateSet(t,
		mustDatePeriod(t, dayOffset(2), dayOffset(2), temporal.Closed),
		mustDatePeriod(t, dayOffset(6), dayOffset(6), temporal.Closed),
	)
	_, err = limited.Subtract(removed)
	assertDateLimitError(t, err, "output_periods", 3, 2)

	intersections := mustDateSet(t,
		mustDatePeriod(t, dayOffset(0), dayOffset(1), temporal.Closed),
		mustDatePeriod(t, dayOffset(4), dayOffset(5), temporal.Closed),
	)
	got, err := limited.Intersect(intersections)
	if err != nil {
		t.Fatalf("Intersect(exact output limit): %v", err)
	}
	assertDatePeriods(t, got.Periods(), [2]int{0, 1}, [2]int{4, 5})
	limitedOne, err := dateperiod.NewSet(
		temporal.Limits{InputPeriods: 3, OutputPeriods: 1},
		limited.Periods()...,
	)
	if err != nil {
		t.Fatalf("NewSet(limited one): %v", err)
	}
	_, err = limitedOne.Intersect(intersections)
	assertDateLimitError(t, err, "output_periods", 2, 1)
}

func TestSplitDaysReturnsExactChunksAtDivisionBoundaries(t *testing.T) {
	t.Parallel()

	period := mustDatePeriod(t, dayOffset(0), dayOffset(6), temporal.Closed)
	parts, err := period.SplitDays(3, temporal.Limits{Steps: 3})
	if err != nil {
		t.Fatalf("SplitDays(remainder): %v", err)
	}
	assertDatePeriods(t, parts, [2]int{0, 2}, [2]int{3, 5}, [2]int{6, 6})

	exact := mustDatePeriod(t, dayOffset(0), dayOffset(5), temporal.Closed)
	parts, err = exact.SplitDays(3, temporal.Limits{Steps: 2})
	if err != nil {
		t.Fatalf("SplitDays(exact): %v", err)
	}
	assertDatePeriods(t, parts, [2]int{0, 2}, [2]int{3, 5})
	if _, err := period.SplitDays(3, temporal.Limits{Steps: 2}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("SplitDays(limit) error = %v", err)
	}
}
