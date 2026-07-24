package instant_test

import (
	"testing"
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
)

func TestRelationConveniencePredicatesMapToAllenModel(t *testing.T) {
	a := mustPeriod(t, 0, 2, temporal.ClosedOpen)
	b := mustPeriod(t, 1, 3, temporal.ClosedOpen)
	outer := mustPeriod(t, 0, 4, temporal.ClosedOpen)
	sameStart := mustPeriod(t, 0, 3, temporal.ClosedOpen)
	sameEnd := mustPeriod(t, 1, 4, temporal.ClosedOpen)
	if !a.Overlaps(b) || !a.IsBefore(mustPeriod(t, 3, 4, temporal.Closed)) {
		t.Fatal("overlaps/before predicate disagrees with relation")
	}
	if !b.IsAfter(mustPeriod(t, -2, 0, temporal.Closed)) || !outer.Contains(b) || !b.During(outer) {
		t.Fatal("after/contains/during predicate disagrees with relation")
	}
	if !a.Starts(sameStart) || !sameEnd.Finishes(outer) {
		t.Fatal("starts/finishes predicate disagrees with relation")
	}
	empty := mustPeriod(t, 1, 1, temporal.Open)
	if empty.IsBefore(a) || empty.Overlaps(a) || empty.Contains(a) {
		t.Fatal("empty interval had an Allen predicate")
	}
	if !a.Contains(empty) {
		t.Fatal("every period must contain the empty set")
	}
}

func TestRelationConveniencePredicatesAcrossAllBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		leftStart, leftEnd   int
		rightStart, rightEnd int
		relation             temporal.Relation
	}{
		{0, 1, 2, 3, temporal.Before},
		{0, 1, 1, 2, temporal.Meets},
		{0, 2, 1, 3, temporal.Overlaps},
		{0, 1, 0, 2, temporal.Starts},
		{1, 2, 0, 3, temporal.During},
		{1, 3, 0, 3, temporal.Finishes},
		{0, 3, 0, 3, temporal.Equal},
		{0, 3, 1, 3, temporal.FinishedBy},
		{0, 3, 1, 2, temporal.Contains},
		{0, 2, 0, 1, temporal.StartedBy},
		{1, 3, 0, 2, temporal.OverlappedBy},
		{1, 2, 0, 1, temporal.MetBy},
		{2, 3, 0, 1, temporal.After},
	}

	for _, test := range tests {
		for _, leftBounds := range temporal.AllBounds() {
			for _, rightBounds := range temporal.AllBounds() {
				left := mustPeriod(t, test.leftStart, test.leftEnd, leftBounds)
				right := mustPeriod(t, test.rightStart, test.rightEnd, rightBounds)
				wantOverlap, wantContains, wantSetEqual := probeSetPredicates(left, right)
				wantAbuts := test.leftEnd == test.rightStart || test.rightEnd == test.leftStart
				wantBorders := test.leftEnd == test.rightStart &&
					leftBounds.IncludesEnd() && rightBounds.IncludesStart() ||
					test.rightEnd == test.leftStart &&
						rightBounds.IncludesEnd() && leftBounds.IncludesStart()

				checks := []struct {
					name string
					got  bool
					want bool
				}{
					{"before", left.IsBefore(right), test.relation == temporal.Before},
					{"after", left.IsAfter(right), test.relation == temporal.After},
					{"starts", left.Starts(right), test.relation == temporal.Starts},
					{"finishes", left.Finishes(right), test.relation == temporal.Finishes},
					{"during", left.During(right), test.relation == temporal.During},
					{"overlaps", left.Overlaps(right), wantOverlap},
					{"contains", left.Contains(right), wantContains},
					{"set equal", left.SetEqual(right), wantSetEqual},
					{"structurally equal", left.Equal(right),
						test.leftStart == test.rightStart && test.leftEnd == test.rightEnd &&
							leftBounds == rightBounds},
					{"abuts", left.Abuts(right), wantAbuts},
					{"borders", left.Borders(right), wantBorders},
					{"meets", left.Meets(right), wantAbuts && !wantBorders},
				}
				for _, check := range checks {
					if check.got != check.want {
						t.Fatalf("%s for %s with %v/%v = %v, want %v",
							check.name, test.relation, leftBounds, rightBounds,
							check.got, check.want)
					}
				}
			}
		}
	}
}

func probeSetPredicates(left, right instant.Period) (overlap, contains, equal bool) {
	contains = true
	reverseContains := true
	for halfHour := -2; halfHour <= 8; halfHour++ {
		point := at(0).Add(time.Duration(halfHour) * 30 * time.Minute)
		leftIncludes := left.Includes(point)
		rightIncludes := right.Includes(point)
		overlap = overlap || leftIncludes && rightIncludes
		contains = contains && (!rightIncludes || leftIncludes)
		reverseContains = reverseContains && (!leftIncludes || rightIncludes)
	}

	return overlap, contains, contains && reverseContains
}

func TestPeriodUnionDifferenceAndMergeAreDistinct(t *testing.T) {
	a := mustPeriod(t, 0, 1, temporal.Closed)
	b := mustPeriod(t, 3, 4, temporal.Closed)
	union, err := a.Union(b, temporal.DefaultLimits())
	if err != nil || union.Len() != 2 {
		t.Fatalf("Union() = %+v, %v", union, err)
	}
	merged := a.Merge(b)
	if merged.Start() != at(0) || merged.End() != at(4) || merged.Bounds() != temporal.Closed {
		t.Fatalf("Merge() = %+v", merged)
	}
	difference := merged.Difference(a)
	if len(difference) != 1 || difference[0].Includes(at(0)) || !difference[0].Includes(at(4)) {
		t.Fatalf("Difference() = %+v", difference)
	}
}

func TestCheckedResizeAndEndpointMovement(t *testing.T) {
	period := mustPeriod(t, 1, 3, temporal.ClosedOpen)
	after, err := period.WithDurationAfterStart(4 * time.Hour)
	if err != nil || after.End() != at(5) {
		t.Fatalf("WithDurationAfterStart() = %+v, %v", after, err)
	}
	before, err := period.WithDurationBeforeEnd(4 * time.Hour)
	if err != nil || before.Start() != at(-1) {
		t.Fatalf("WithDurationBeforeEnd() = %+v, %v", before, err)
	}
	movedStart, err := period.MoveStart(time.Hour)
	if err != nil || movedStart.Start() != at(2) || movedStart.End() != at(3) {
		t.Fatalf("MoveStart() = %+v, %v", movedStart, err)
	}
	movedEnd, err := period.MoveEnd(-time.Hour)
	if err != nil || movedEnd.Start() != at(1) || movedEnd.End() != at(2) {
		t.Fatalf("MoveEnd() = %+v, %v", movedEnd, err)
	}
	if _, err := period.WithDurationAfterStart(-time.Second); err == nil {
		t.Fatal("resize accepted negative duration")
	}
	if _, err := period.WithDurationBeforeEnd(-time.Second); err == nil {
		t.Fatal("resize accepted negative duration")
	}
	if _, err := period.MoveStart(3 * time.Hour); err == nil {
		t.Fatal("MoveStart accepted reversed period")
	}
	if _, err := period.MoveEnd(-3 * time.Hour); err == nil {
		t.Fatal("MoveEnd accepted reversed period")
	}
}
