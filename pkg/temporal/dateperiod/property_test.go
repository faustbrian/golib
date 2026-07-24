package dateperiod_test

import (
	"math/rand"
	"testing"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/dateperiod"
)

func TestDateSetAlgebraProperties(t *testing.T) {
	t.Parallel()

	random := rand.New(rand.NewSource(20260717))
	must := func(set dateperiod.Set, err error) dateperiod.Set {
		if err != nil {
			t.Fatalf("date set operation: %v", err)
		}
		return set
	}
	for iteration := 0; iteration < 1_000; iteration++ {
		left := randomDateSet(t, random)
		right := randomDateSet(t, random)
		third := randomDateSet(t, random)

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
			t.Fatalf("date algebra identity failed at iteration %d", iteration)
		}
		for offset := 0; offset <= 32; offset++ {
			point := dayOffset(offset)
			if union.Includes(point) != (left.Includes(point) || right.Includes(point)) ||
				intersection.Includes(point) != (left.Includes(point) && right.Includes(point)) ||
				difference.Includes(point) != (left.Includes(point) && !right.Includes(point)) {
				t.Fatalf("date membership identity failed at iteration %d day %d", iteration, offset)
			}
		}
	}
}

func randomDateSet(t *testing.T, random *rand.Rand) dateperiod.Set {
	t.Helper()
	periods := make([]dateperiod.Period, random.Intn(6))
	for index := range periods {
		start := random.Intn(16)
		end := start + random.Intn(17-start)
		periods[index] = mustDatePeriod(t, dayOffset(start), dayOffset(end),
			temporal.AllBounds()[random.Intn(4)])
	}
	return mustDateSet(t, periods...)
}
