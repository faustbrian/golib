package instant_test

import (
	"math/rand"
	"testing"
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
)

func TestInstantSetAlgebraProperties(t *testing.T) {
	t.Parallel()

	random := rand.New(rand.NewSource(20260716))
	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	must := func(set instant.Set, err error) instant.Set {
		if err != nil {
			t.Fatalf("instant set operation: %v", err)
		}
		return set
	}
	for iteration := 0; iteration < 1_000; iteration++ {
		left := randomInstantSet(t, random, base)
		right := randomInstantSet(t, random, base)
		third := randomInstantSet(t, random, base)
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
			t.Fatalf("algebra identity failed at iteration %d: left=%+v right=%+v union=%+v reverse=%+v intersection=%+v reverseIntersection=%+v difference=%+v reconstructed=%+v idempotent=%+v",
				iteration, left.Periods(), right.Periods(), union.Periods(), reverseUnion.Periods(), intersection.Periods(), reverseIntersection.Periods(), difference.Periods(), reconstructed.Periods(), idempotent.Periods())
		}
		for tick := 0; tick <= 32; tick++ {
			point := base.Add(time.Duration(tick) * 30 * time.Minute)
			if union.Includes(point) != (left.Includes(point) || right.Includes(point)) ||
				intersection.Includes(point) != (left.Includes(point) && right.Includes(point)) ||
				difference.Includes(point) != (left.Includes(point) && !right.Includes(point)) {
				t.Fatalf("membership identity failed at iteration %d tick %d", iteration, tick)
			}
		}
	}
}

func randomInstantSet(t *testing.T, random *rand.Rand, base time.Time) instant.Set {
	t.Helper()
	periods := make([]instant.Period, random.Intn(6))
	for index := range periods {
		start := random.Intn(16)
		end := start + random.Intn(17-start)
		period, err := instant.New(
			base.Add(time.Duration(start)*time.Hour),
			base.Add(time.Duration(end)*time.Hour),
			temporal.AllBounds()[random.Intn(4)],
		)
		if err != nil {
			t.Fatalf("instant.New(): %v", err)
		}
		periods[index] = period
	}
	set, err := instant.NewSet(temporal.Limits{}, periods...)
	if err != nil {
		t.Fatalf("NewSet(): %v", err)
	}
	return set
}

func BenchmarkInstantSetNormalize(b *testing.B) {
	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	periods := make([]instant.Period, 1_000)
	for index := range periods {
		start := base.Add(time.Duration(index%200) * time.Minute)
		periods[index], _ = instant.Range(start, start.Add(10*time.Minute))
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := instant.NewSet(temporal.Limits{}, periods...); err != nil {
			b.Fatal(err)
		}
	}
}
