package dateperiod_test

import (
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/dateperiod"
)

func dayOffset(offset int) calendar.Date {
	value, _ := date(2026, time.January, 1).AddDays(offset)
	return value
}

func TestDateAllenRelationsAcrossAllBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		aStart   int
		aEnd     int
		bStart   int
		bEnd     int
		relation temporal.Relation
	}{
		{"before", 0, 2, 4, 6, temporal.Before},
		{"meets", 0, 3, 3, 6, temporal.Meets},
		{"overlaps", 0, 4, 2, 6, temporal.Overlaps},
		{"starts", 0, 3, 0, 6, temporal.Starts},
		{"during", 2, 4, 0, 6, temporal.During},
		{"finishes", 2, 6, 0, 6, temporal.Finishes},
		{"equals", 0, 6, 0, 6, temporal.Equal},
		{"finished by", 0, 6, 2, 6, temporal.FinishedBy},
		{"contains", 0, 6, 2, 4, temporal.Contains},
		{"started by", 0, 6, 0, 3, temporal.StartedBy},
		{"overlapped by", 2, 6, 0, 4, temporal.OverlappedBy},
		{"met by", 3, 6, 0, 3, temporal.MetBy},
		{"after", 4, 6, 0, 2, temporal.After},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for _, leftBounds := range temporal.AllBounds() {
				for _, rightBounds := range temporal.AllBounds() {
					left := mustDatePeriod(t, dayOffset(test.aStart), dayOffset(test.aEnd), leftBounds)
					right := mustDatePeriod(t, dayOffset(test.bStart), dayOffset(test.bEnd), rightBounds)
					got, err := left.RelationTo(right)
					if err != nil || got != test.relation {
						t.Fatalf("RelationTo(%v,%v) = %v, %v", leftBounds, rightBounds, got, err)
					}
					inverse, err := right.RelationTo(left)
					if err != nil || inverse != test.relation.Converse() {
						t.Fatalf("inverse = %v, %v", inverse, err)
					}
				}
			}
		})
	}
}

func TestDateRelationRejectsEmptyPeriod(t *testing.T) {
	t.Parallel()

	empty := mustDatePeriod(t, dayOffset(0), dayOffset(1), temporal.Open)
	nonempty := mustDatePeriod(t, dayOffset(0), dayOffset(3), temporal.Closed)
	if _, err := empty.RelationTo(nonempty); !errors.Is(err, temporal.ErrEmpty) {
		t.Fatalf("RelationTo() error = %v", err)
	}
}

func TestDateSetEqualityCanonicalizesBoundsAndEmptyValues(t *testing.T) {
	t.Parallel()

	closedOpen := mustDatePeriod(t, dayOffset(0), dayOffset(2), temporal.ClosedOpen)
	closed := mustDatePeriod(t, dayOffset(0), dayOffset(1), temporal.Closed)
	if !closedOpen.SetEqual(closed) {
		t.Fatal("equivalent discrete bounds were not set-equal")
	}
	emptyA := mustDatePeriod(t, dayOffset(0), dayOffset(1), temporal.Open)
	emptyB := mustDatePeriod(t, dayOffset(3), dayOffset(4), temporal.Open)
	if !emptyA.SetEqual(emptyB) {
		t.Fatal("empty date periods were not set-equal")
	}
}

func TestDatePeriodAlgebraHandlesEmptyOperands(t *testing.T) {
	t.Parallel()

	empty := mustDatePeriod(t, dayOffset(0), dayOffset(1), temporal.Open)
	value := mustDatePeriod(t, dayOffset(0), dayOffset(3), temporal.Closed)
	if _, ok := value.Intersect(empty); ok {
		t.Fatal("intersection with empty period was non-empty")
	}
	if parts := empty.Subtract(value); parts != nil {
		t.Fatalf("empty subtraction = %+v, want nil", parts)
	}
}

func mustDateSet(t *testing.T, periods ...dateperiod.Period) dateperiod.Set {
	t.Helper()
	set, err := dateperiod.NewSet(temporal.Limits{}, periods...)
	if err != nil {
		t.Fatalf("NewSet(): %v", err)
	}
	return set
}

func TestDateSetNormalizesDiscreteAdjacencyAndCopiesSlices(t *testing.T) {
	t.Parallel()

	input := []dateperiod.Period{
		mustDatePeriod(t, dayOffset(2), dayOffset(3), temporal.Closed),
		mustDatePeriod(t, dayOffset(0), dayOffset(1), temporal.Closed),
	}
	set := mustDateSet(t, input...)
	if set.Len() != 1 || set.TotalDays() != 4 {
		t.Fatalf("normalized set = %+v, days=%d", set.Periods(), set.TotalDays())
	}
	input[0] = mustDatePeriod(t, dayOffset(8), dayOffset(9), temporal.Closed)
	returned := set.Periods()
	returned[0] = input[0]
	if !set.Includes(dayOffset(0)) || !set.Includes(dayOffset(3)) {
		t.Fatal("slice mutation changed date set")
	}
}

func TestDateSetCanonicalizesEqualStartsAndRejectsDifferentSets(t *testing.T) {
	t.Parallel()

	set := mustDateSet(t,
		mustDatePeriod(t, dayOffset(0), dayOffset(4), temporal.Closed),
		mustDatePeriod(t, dayOffset(0), dayOffset(2), temporal.Closed),
	)
	if set.Len() != 1 || set.TotalDays() != 5 {
		t.Fatalf("equal-start normalization = %+v", set.Periods())
	}
	if set.Equal(mustDateSet(t)) || set.Equal(mustDateSet(t, mustDatePeriod(t, dayOffset(8), dayOffset(9), temporal.Closed))) {
		t.Fatal("different date sets compared equal")
	}
}

func TestDateSetAlgebraIsNormalizedAndExact(t *testing.T) {
	t.Parallel()

	a := mustDateSet(t, mustDatePeriod(t, dayOffset(0), dayOffset(6), temporal.Closed))
	b := mustDateSet(t,
		mustDatePeriod(t, dayOffset(1), dayOffset(2), temporal.Open),
		mustDatePeriod(t, dayOffset(4), dayOffset(5), temporal.Closed),
	)

	intersection, err := a.Intersect(b)
	if err != nil || !intersection.Equal(b) {
		t.Fatalf("Intersect() = %+v, %v", intersection.Periods(), err)
	}
	difference, err := a.Subtract(b)
	if err != nil {
		t.Fatalf("Subtract(): %v", err)
	}
	for offset, included := range map[int]bool{0: true, 1: true, 2: true, 3: true, 4: false, 5: false, 6: true} {
		if got := difference.Includes(dayOffset(offset)); got != included {
			t.Fatalf("difference Includes(%d) = %v", offset, got)
		}
	}
	union, err := difference.Union(b)
	if err != nil || !union.Equal(a) {
		t.Fatalf("conservation union = %+v, %v", union.Periods(), err)
	}
}

func TestDateAlgebraHandlesEmptyDisjointEqualAndLimitedOutputs(t *testing.T) {
	t.Parallel()

	emptyPeriod := mustDatePeriod(t, dayOffset(0), dayOffset(1), temporal.Open)
	empty := mustDateSet(t, emptyPeriod)
	a := mustDateSet(t, mustDatePeriod(t, dayOffset(0), dayOffset(6), temporal.Closed))
	disjoint := mustDateSet(t, mustDatePeriod(t, dayOffset(8), dayOffset(9), temporal.Closed))

	if intersection, err := a.Intersect(empty); err != nil || intersection.Len() != 0 {
		t.Fatalf("empty intersection = %+v, %v", intersection.Periods(), err)
	}
	if intersection, err := a.Intersect(disjoint); err != nil || intersection.Len() != 0 {
		t.Fatalf("disjoint intersection = %+v, %v", intersection.Periods(), err)
	}
	if difference, err := a.Subtract(disjoint); err != nil || !difference.Equal(a) {
		t.Fatalf("disjoint subtraction = %+v, %v", difference.Periods(), err)
	}
	if difference, err := a.Subtract(a); err != nil || difference.Len() != 0 {
		t.Fatalf("equal subtraction = %+v, %v", difference.Periods(), err)
	}
	if difference, err := empty.Subtract(a); err != nil || difference.Len() != 0 {
		t.Fatalf("empty subtraction = %+v, %v", difference.Periods(), err)
	}

	equalEnd := mustDateSet(t, mustDatePeriod(t, dayOffset(2), dayOffset(6), temporal.Closed))
	if intersection, err := a.Intersect(equalEnd); err != nil || !intersection.Equal(equalEnd) {
		t.Fatalf("equal-end intersection = %+v, %v", intersection.Periods(), err)
	}
	earlierEnd := mustDateSet(t, mustDatePeriod(t, dayOffset(0), dayOffset(2), temporal.Closed))
	if intersection, err := earlierEnd.Intersect(a); err != nil || !intersection.Equal(earlierEnd) {
		t.Fatalf("earlier-end intersection = %+v, %v", intersection.Periods(), err)
	}

	limited, err := dateperiod.NewSet(
		temporal.Limits{OutputPeriods: 1},
		mustDatePeriod(t, dayOffset(0), dayOffset(8), temporal.Closed),
	)
	if err != nil {
		t.Fatalf("NewSet(limited): %v", err)
	}
	two := mustDateSet(t,
		mustDatePeriod(t, dayOffset(0), dayOffset(1), temporal.Closed),
		mustDatePeriod(t, dayOffset(4), dayOffset(5), temporal.Closed),
	)
	if _, err := limited.Intersect(two); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Intersect(limit) error = %v", err)
	}
	removed := mustDateSet(t, mustDatePeriod(t, dayOffset(3), dayOffset(4), temporal.Closed))
	if _, err := limited.Subtract(removed); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Subtract(limit) error = %v", err)
	}
}

func TestDateSetSpanGapsAndLimits(t *testing.T) {
	t.Parallel()

	set := mustDateSet(t,
		mustDatePeriod(t, dayOffset(0), dayOffset(1), temporal.Closed),
		mustDatePeriod(t, dayOffset(4), dayOffset(5), temporal.Closed),
	)
	span, ok := set.Span()
	if !ok || span.Days() != 6 {
		t.Fatalf("Span() = %+v, %v", span, ok)
	}
	gaps := set.Gaps()
	if len(gaps) != 1 || gaps[0].Days() != 2 || !gaps[0].Includes(dayOffset(2)) || !gaps[0].Includes(dayOffset(3)) {
		t.Fatalf("Gaps() = %+v", gaps)
	}
	if _, ok := mustDateSet(t).Span(); ok {
		t.Fatal("empty set returned span")
	}
	if gaps := mustDateSet(t).Gaps(); gaps != nil {
		t.Fatalf("empty Gaps() = %+v", gaps)
	}

	periods := []dateperiod.Period{
		mustDatePeriod(t, dayOffset(0), dayOffset(0), temporal.Closed),
		mustDatePeriod(t, dayOffset(2), dayOffset(2), temporal.Closed),
	}
	if _, err := dateperiod.NewSet(temporal.Limits{InputPeriods: 1}, periods...); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("input limit error = %v", err)
	}
	if _, err := dateperiod.NewSet(temporal.Limits{OutputPeriods: 1}, periods...); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("output limit error = %v", err)
	}
	if _, err := dateperiod.NewSet(temporal.Limits{Precision: 10}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("invalid limit error = %v", err)
	}
}
