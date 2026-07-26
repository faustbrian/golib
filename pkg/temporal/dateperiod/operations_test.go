package dateperiod_test

import (
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/dateperiod"
)

func TestDateImmutableReplacementAndCalendarMovement(t *testing.T) {
	period, _ := dateperiod.New(date(2026, time.January, 31), date(2026, time.February, 28), temporal.Closed)
	moved, err := period.MoveYears(1, calendar.Clamp)
	if err != nil || moved.Start().Year() != 2027 || moved.End().Year() != 2027 {
		t.Fatalf("MoveYears() = %+v, %v", moved, err)
	}
	weeks, err := period.MoveWeeks(2)
	if err != nil || weeks.Start().Day() != 14 || weeks.Start().Month() != time.February {
		t.Fatalf("MoveWeeks() = %+v, %v", weeks, err)
	}
	quarters, err := period.MoveQuarters(1, calendar.Clamp)
	if err != nil || quarters.Start().Month() != time.April {
		t.Fatalf("MoveQuarters() = %+v, %v", quarters, err)
	}
	expanded, err := period.ExpandDays(2)
	if err != nil || expanded.Start().Day() != 29 || expanded.End().Month() != time.March || expanded.End().Day() != 2 {
		t.Fatalf("ExpandDays() = %+v, %v", expanded, err)
	}
	withBounds, err := period.WithBounds(temporal.Open)
	if err != nil || withBounds.Bounds() != temporal.Open || period.Bounds() != temporal.Closed {
		t.Fatalf("WithBounds() = %+v, %v", withBounds, err)
	}
	if _, err := period.WithStart(date(2027, time.January, 1)); !errors.Is(err, temporal.ErrReversed) {
		t.Fatalf("WithStart(reversed) error = %v", err)
	}
	if _, err := period.ExpandDays(-15); !errors.Is(err, temporal.ErrReversed) {
		t.Fatalf("ExpandDays(shrink) error = %v", err)
	}
}

func TestDateGapUnionMergeAndPredicates(t *testing.T) {
	left, _ := dateperiod.New(date(2026, time.January, 1), date(2026, time.January, 2), temporal.Closed)
	right, _ := dateperiod.New(date(2026, time.January, 5), date(2026, time.January, 6), temporal.Closed)
	gap, ok := left.Gap(right)
	if !ok || gap.Start().Day() != 3 || gap.End().Day() != 4 {
		t.Fatalf("Gap() = %+v, %v", gap, ok)
	}
	union, err := left.Union(right, temporal.DefaultLimits())
	if err != nil || union.Len() != 2 {
		t.Fatalf("Union() = %+v, %v", union, err)
	}
	merge := left.Merge(right)
	if merge.Days() != 6 {
		t.Fatalf("Merge days = %d", merge.Days())
	}
	if !left.IsBefore(right) || !right.IsAfter(left) || left.Overlaps(right) || !merge.Contains(left) || !left.During(merge) {
		t.Fatal("date convenience predicates disagree with algebra")
	}
	adjacent, _ := dateperiod.New(date(2026, time.January, 2), date(2026, time.January, 3), temporal.OpenClosed)
	if _, ok := left.Gap(adjacent); ok {
		t.Fatal("Gap returned dates between adjacent represented sets")
	}
}

func TestDateOperationEdgesRemainTypedAndDeterministic(t *testing.T) {
	period, _ := dateperiod.New(date(2026, time.January, 1), date(2026, time.January, 2), temporal.Closed)
	withEnd, err := period.WithEnd(date(2026, time.January, 3))
	if err != nil || withEnd.End().Day() != 3 {
		t.Fatalf("WithEnd() = %+v, %v", withEnd, err)
	}
	semester, err := period.MoveSemesters(1, calendar.Clamp)
	if err != nil || semester.Start().Month() != time.July {
		t.Fatalf("MoveSemesters() = %+v, %v", semester, err)
	}
	if !period.Equal(period) || period.Equal(withEnd) {
		t.Fatal("Equal did not use structural values")
	}
	if got := withEnd.Difference(period); len(got) != 1 || got[0].Start().Day() != 3 {
		t.Fatalf("Difference() = %+v", got)
	}

	minimum, _ := dateperiod.New(date(1, time.January, 1), date(1, time.January, 2), temporal.Closed)
	if _, err := minimum.ExpandDays(1); err == nil {
		t.Fatal("ExpandDays accepted minimum-date underflow")
	}
	maximum, _ := dateperiod.New(date(9999, time.December, 30), date(9999, time.December, 31), temporal.Closed)
	if _, err := maximum.ExpandDays(1); err == nil {
		t.Fatal("ExpandDays accepted maximum-date overflow")
	}
	minimumInt := -int(^uint(0)>>1) - 1
	if _, err := period.ExpandDays(minimumInt); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("ExpandDays(min int) error = %v", err)
	}
	if _, err := maximum.MoveYears(1, calendar.Clamp); err == nil {
		t.Fatal("MoveYears accepted first endpoint overflow")
	}
	endOverflow, _ := dateperiod.New(date(9998, time.December, 31), date(9999, time.December, 31), temporal.Closed)
	if _, err := endOverflow.MoveYears(1, calendar.Clamp); err == nil {
		t.Fatal("MoveYears accepted second endpoint overflow")
	}
}

func TestDateMergeGapAndPredicateEdges(t *testing.T) {
	a, _ := dateperiod.New(date(2026, time.January, 2), date(2026, time.January, 4), temporal.Closed)
	b, _ := dateperiod.New(date(2026, time.January, 1), date(2026, time.January, 5), temporal.Closed)
	empty, _ := dateperiod.New(date(2026, time.January, 3), date(2026, time.January, 3), temporal.Open)
	if !empty.Merge(a).SetEqual(a) || !a.Merge(empty).SetEqual(a) || empty.Merge(empty).Start().IsValid() {
		t.Fatal("Merge did not ignore empty operands")
	}
	if !a.Merge(b).SetEqual(b) || !b.Merge(a).SetEqual(b) {
		t.Fatal("Merge did not choose outer endpoints")
	}
	if _, ok := a.Gap(b); ok {
		t.Fatal("Gap returned overlap")
	}
	if _, ok := empty.Gap(a); ok {
		t.Fatal("Gap returned result for empty operand")
	}
	left, _ := dateperiod.New(date(2026, time.January, 10), date(2026, time.January, 11), temporal.Closed)
	right, _ := dateperiod.New(date(2026, time.January, 6), date(2026, time.January, 7), temporal.Closed)
	gap, ok := left.Gap(right)
	if !ok || gap.Start().Day() != 8 || gap.End().Day() != 9 {
		t.Fatalf("reversed Gap() = %+v, %v", gap, ok)
	}
	starts, _ := dateperiod.New(b.Start(), date(2026, time.January, 3), temporal.Closed)
	finishes, _ := dateperiod.New(date(2026, time.January, 3), b.End(), temporal.Closed)
	if !starts.Starts(b) || !finishes.Finishes(b) {
		t.Fatal("Starts/Finishes did not map to Allen relations")
	}
	if !a.Contains(empty) || empty.Contains(a) {
		t.Fatal("Contains mishandled empty set")
	}
}
