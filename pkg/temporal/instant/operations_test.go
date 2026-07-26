package instant_test

import (
	"errors"
	"testing"
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
)

func TestDurationUsesElapsedInstantArithmetic(t *testing.T) {
	t.Parallel()

	period := mustPeriod(t, 1, 3, temporal.Open)
	duration, err := period.Duration()
	if err != nil {
		t.Fatalf("Duration(): %v", err)
	}
	if duration != 2*time.Hour {
		t.Fatalf("Duration() = %v, want 2h", duration)
	}
}

func TestCompareDurationUsesCheckedElapsedSpans(t *testing.T) {
	short := mustPeriod(t, 0, 1, temporal.ClosedOpen)
	long := mustPeriod(t, 0, 2, temporal.ClosedOpen)
	comparison, err := short.CompareDuration(long)
	if err != nil || comparison != -1 {
		t.Fatalf("CompareDuration(short, long) = %d, %v", comparison, err)
	}
	comparison, err = long.CompareDuration(short)
	if err != nil || comparison != 1 {
		t.Fatalf("CompareDuration(long, short) = %d, %v", comparison, err)
	}
	comparison, err = short.CompareDuration(short)
	if err != nil || comparison != 0 {
		t.Fatalf("CompareDuration(equal) = %d, %v", comparison, err)
	}
	overflow, _ := instant.New(time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), temporal.ClosedOpen)
	if _, err := overflow.CompareDuration(short); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("CompareDuration(overflow) error = %v", err)
	}
	if _, err := short.CompareDuration(overflow); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("CompareDuration(other overflow) error = %v", err)
	}
}

func TestDurationRejectsUnrepresentableSpan(t *testing.T) {
	t.Parallel()

	period, err := instant.New(
		time.Date(1000, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
		temporal.ClosedOpen,
	)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	if _, err := period.Duration(); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("Duration() error = %v, want ErrOverflow", err)
	}
}

func TestImmutableEndpointAndBoundReplacement(t *testing.T) {
	t.Parallel()

	original := mustPeriod(t, 1, 3, temporal.ClosedOpen)
	changed, err := original.WithStart(at(0))
	if err != nil {
		t.Fatalf("WithStart(): %v", err)
	}
	changed, err = changed.WithEnd(at(4))
	if err != nil {
		t.Fatalf("WithEnd(): %v", err)
	}
	changed, err = changed.WithBounds(temporal.OpenClosed)
	if err != nil {
		t.Fatalf("WithBounds(): %v", err)
	}

	if !original.Start().Equal(at(1)) || !original.End().Equal(at(3)) || original.Bounds() != temporal.ClosedOpen {
		t.Fatal("replacement mutated the original period")
	}
	if !changed.Start().Equal(at(0)) || !changed.End().Equal(at(4)) || changed.Bounds() != temporal.OpenClosed {
		t.Fatalf("changed period = %v", changed)
	}

	if _, err := original.WithStart(at(4)); !errors.Is(err, temporal.ErrReversed) {
		t.Fatalf("WithStart(reversed) error = %v", err)
	}
	if _, err := original.WithEnd(at(0)); !errors.Is(err, temporal.ErrReversed) {
		t.Fatalf("WithEnd(reversed) error = %v", err)
	}
	if _, err := original.WithBounds(temporal.Bounds(255)); !errors.Is(err, temporal.ErrBounds) {
		t.Fatalf("WithBounds(invalid) error = %v", err)
	}
}

func TestMoveAndExpandReturnNewPeriods(t *testing.T) {
	t.Parallel()

	original := mustPeriod(t, 1, 3, temporal.ClosedOpen)
	moved := original.Move(2 * time.Hour)
	if !moved.Start().Equal(at(3)) || !moved.End().Equal(at(5)) {
		t.Fatalf("Move() = [%v,%v]", moved.Start(), moved.End())
	}

	expanded, err := original.Expand(time.Hour)
	if err != nil {
		t.Fatalf("Expand(): %v", err)
	}
	if !expanded.Start().Equal(at(0)) || !expanded.End().Equal(at(4)) {
		t.Fatalf("Expand() = [%v,%v]", expanded.Start(), expanded.End())
	}

	if _, err := original.Expand(-2 * time.Hour); !errors.Is(err, temporal.ErrReversed) {
		t.Fatalf("Expand(shrink past empty) error = %v", err)
	}
	if _, err := original.Expand(time.Duration(-1 << 63)); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("Expand(min duration) error = %v, want ErrOverflow", err)
	}
}

func TestIntersectionConjoinsBoundaryMembership(t *testing.T) {
	t.Parallel()

	for _, leftBounds := range temporal.AllBounds() {
		for _, rightBounds := range temporal.AllBounds() {
			left := mustPeriod(t, 0, 2, leftBounds)
			right := mustPeriod(t, 1, 3, rightBounds)
			intersection, ok := left.Intersect(right)
			if !ok {
				t.Fatalf("Intersect(%v, %v) unexpectedly empty", leftBounds, rightBounds)
			}

			if !intersection.Start().Equal(at(1)) || !intersection.End().Equal(at(2)) {
				t.Fatalf("intersection endpoints = [%v,%v]", intersection.Start(), intersection.End())
			}
			if intersection.Bounds().IncludesStart() != rightBounds.IncludesStart() {
				t.Fatalf("start inclusion = %v, want %v", intersection.Bounds(), rightBounds)
			}
			if intersection.Bounds().IncludesEnd() != leftBounds.IncludesEnd() {
				t.Fatalf("end inclusion = %v, want %v", intersection.Bounds(), leftBounds)
			}
		}
	}
}

func TestIntersectionHandlesDisjointAndSingletonResults(t *testing.T) {
	t.Parallel()

	closedLeft := mustPeriod(t, 0, 1, temporal.Closed)
	closedRight := mustPeriod(t, 1, 2, temporal.Closed)
	intersection, ok := closedLeft.Intersect(closedRight)
	if !ok || !intersection.IsSingleton() || !intersection.Includes(at(1)) {
		t.Fatal("closed border did not produce a singleton intersection")
	}

	halfOpen := mustPeriod(t, 0, 1, temporal.ClosedOpen)
	if _, ok := halfOpen.Intersect(closedRight); ok {
		t.Fatal("excluded adjacency produced a non-empty intersection")
	}
	if _, ok := halfOpen.Intersect(mustPeriod(t, 2, 3, temporal.Closed)); ok {
		t.Fatal("disjoint periods produced an intersection")
	}
	if _, ok := mustPeriod(t, 2, 3, temporal.Closed).Intersect(halfOpen); ok {
		t.Fatal("reverse disjoint periods produced an intersection")
	}
	if _, ok := mustPeriod(t, 1, 1, temporal.Open).Intersect(closedLeft); ok {
		t.Fatal("empty period produced an intersection")
	}
}

func TestDifferenceInvertsRemovedBoundaries(t *testing.T) {
	t.Parallel()

	period := mustPeriod(t, 0, 3, temporal.Closed)
	removed := mustPeriod(t, 1, 2, temporal.Open)
	parts := period.Subtract(removed)
	if len(parts) != 2 {
		t.Fatalf("len(Subtract()) = %d, want 2", len(parts))
	}

	if !parts[0].SetEqual(mustPeriod(t, 0, 1, temporal.Closed)) {
		t.Fatalf("left difference = %+v", parts[0])
	}
	if !parts[1].SetEqual(mustPeriod(t, 2, 3, temporal.Closed)) {
		t.Fatalf("right difference = %+v", parts[1])
	}

	if got := period.Subtract(period); len(got) != 0 {
		t.Fatalf("equal subtraction returned %d parts", len(got))
	}
	disjoint := period.Subtract(mustPeriod(t, 4, 5, temporal.Closed))
	if len(disjoint) != 1 || !disjoint[0].Equal(period) {
		t.Fatal("disjoint subtraction changed the period")
	}
}

func TestGapReturnsExactMissingSet(t *testing.T) {
	t.Parallel()

	left := mustPeriod(t, 0, 1, temporal.Closed)
	right := mustPeriod(t, 2, 3, temporal.OpenClosed)
	gap, ok := left.Gap(right)
	if !ok || !gap.SetEqual(mustPeriod(t, 1, 2, temporal.OpenClosed)) {
		t.Fatalf("Gap() = %+v, %v", gap, ok)
	}

	openLeft := mustPeriod(t, 0, 1, temporal.Open)
	openRight := mustPeriod(t, 1, 2, temporal.Open)
	point, ok := openLeft.Gap(openRight)
	if !ok || !point.IsSingleton() {
		t.Fatal("two excluded adjacent endpoints did not leave a singleton gap")
	}

	if _, ok := left.Gap(mustPeriod(t, 1, 2, temporal.ClosedOpen)); ok {
		t.Fatal("covered adjacency produced a gap")
	}
	if _, ok := mustPeriod(t, 0, 1, temporal.ClosedOpen).Gap(
		mustPeriod(t, 1, 2, temporal.ClosedOpen),
	); ok {
		t.Fatal("contiguous half-open periods produced a gap")
	}
	if _, ok := left.Gap(mustPeriod(t, 0, 2, temporal.Closed)); ok {
		t.Fatal("overlap produced a gap")
	}
	reverse, ok := right.Gap(left)
	if !ok || !reverse.SetEqual(gap) {
		t.Fatal("gap was not symmetric")
	}
	if _, ok := left.Gap(mustPeriod(t, 1, 1, temporal.Open)); ok {
		t.Fatal("empty period produced a gap")
	}
}

func TestStructuralAndSetEqualityAreDistinctForEmptyPeriods(t *testing.T) {
	t.Parallel()

	a := mustPeriod(t, 1, 1, temporal.Open)
	b := mustPeriod(t, 2, 2, temporal.ClosedOpen)
	if a.Equal(b) {
		t.Fatal("structurally different empty periods compared equal")
	}
	if !a.SetEqual(b) {
		t.Fatal("empty periods were not set-equal")
	}
}
