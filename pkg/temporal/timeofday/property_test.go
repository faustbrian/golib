package timeofday_test

import (
	"math/rand"
	"testing"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
)

func TestCircularSetAlgebraProperties(t *testing.T) {
	t.Parallel()

	random := rand.New(rand.NewSource(20260717))
	must := func(set timeofday.IntervalSet, err error) timeofday.IntervalSet {
		if err != nil {
			t.Fatalf("daily set operation: %v", err)
		}
		return set
	}
	for iteration := 0; iteration < 1_000; iteration++ {
		left := randomDailySet(t, random)
		right := randomDailySet(t, random)
		third := randomDailySet(t, random)

		union := must(left.Union(right))
		reverseUnion := must(right.Union(left))
		intersection := must(left.Intersect(right))
		reverseIntersection := must(right.Intersect(left))
		difference := must(left.Subtract(right))
		reconstructed := must(intersection.Union(difference))
		idempotent := must(left.Union(left))
		leftUnionRight := must(left.Union(right))
		leftAssociatedUnion := must(leftUnionRight.Union(third))
		rightUnionThird := must(right.Union(third))
		rightAssociatedUnion := must(left.Union(rightUnionThird))
		leftIntersectRight := must(left.Intersect(right))
		leftAssociatedIntersection := must(leftIntersectRight.Intersect(third))
		rightIntersectThird := must(right.Intersect(third))
		rightAssociatedIntersection := must(left.Intersect(rightIntersectThird))

		if !union.Equal(reverseUnion) || !intersection.Equal(reverseIntersection) ||
			!reconstructed.Equal(left) || !idempotent.Equal(left) ||
			!leftAssociatedUnion.Equal(rightAssociatedUnion) ||
			!leftAssociatedIntersection.Equal(rightAssociatedIntersection) {
			t.Fatalf("daily algebra identity failed at iteration %d", iteration)
		}

		for halfHour := 0; halfHour < 48; halfHour++ {
			point, _ := timeofday.New(halfHour/2, (halfHour%2)*30, 0, 0, 0)
			if union.Includes(point) != (left.Includes(point) || right.Includes(point)) ||
				intersection.Includes(point) != (left.Includes(point) && right.Includes(point)) ||
				difference.Includes(point) != (left.Includes(point) && !right.Includes(point)) {
				t.Fatalf("daily membership identity failed at iteration %d point %s", iteration, point)
			}
		}
	}
}

func randomDailySet(t *testing.T, random *rand.Rand) timeofday.IntervalSet {
	t.Helper()
	intervals := make([]timeofday.Interval, random.Intn(6))
	for index := range intervals {
		startHour := random.Intn(24)
		endHour := random.Intn(24)
		if startHour == endHour {
			if random.Intn(2) == 0 {
				intervals[index] = timeofday.Collapsed(hm(t, startHour, 0))
			} else {
				intervals[index] = timeofday.FullDay()
			}
			continue
		}
		intervals[index] = mustInterval(t, startHour, endHour,
			temporal.AllBounds()[random.Intn(4)])
	}
	return mustDailySet(t, intervals...)
}

func TestCircularDailyAlgebraAcrossEveryHourAndBounds(t *testing.T) {
	t.Parallel()

	for startHour := 0; startHour < 24; startHour++ {
		for endHour := 0; endHour < 24; endHour++ {
			if startHour == endHour {
				continue
			}
			for _, bounds := range temporal.AllBounds() {
				interval := mustInterval(t, startHour, endHour, bounds)
				set := mustDailySet(t, interval)
				complement, err := set.Complement()
				if err != nil {
					t.Fatalf("Complement(): %v", err)
				}
				twice, _ := complement.Complement()
				intersection, _ := set.Intersect(complement)
				union, _ := set.Union(complement)
				if !twice.Equal(set) || intersection.Len() != 0 || !union.Equal(mustDailySet(t, timeofday.FullDay())) {
					t.Fatalf("daily identity failed for %02d-%02d %v: set=%+v complement=%+v twice=%+v intersection=%+v union=%+v",
						startHour, endHour, bounds, set.Intervals(), complement.Intervals(), twice.Intervals(), intersection.Intervals(), union.Intervals())
				}
				for hour := 0; hour < 24; hour++ {
					point := hm(t, hour, 0)
					if set.Includes(point) == complement.Includes(point) {
						t.Fatalf("complement membership failed for %02d-%02d %v at %02d", startHour, endHour, bounds, hour)
					}
				}
			}
		}
	}
}

func BenchmarkDailySetAlgebra(b *testing.B) {
	interval := func(startHour, endHour int, bounds temporal.Bounds) timeofday.Interval {
		start, _ := timeofday.New(startHour, 0, 0, 0, 0)
		end, _ := timeofday.New(endHour, 0, 0, 0, 0)
		value, _ := timeofday.Between(start, end, bounds)
		return value
	}
	left, _ := timeofday.NewIntervalSet(temporal.Limits{}, interval(22, 6, temporal.ClosedOpen), interval(8, 17, temporal.ClosedOpen))
	right, _ := timeofday.NewIntervalSet(temporal.Limits{}, interval(1, 9, temporal.OpenClosed), interval(12, 23, temporal.ClosedOpen))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := left.Intersect(right); err != nil {
			b.Fatal(err)
		}
	}
}
