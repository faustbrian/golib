package instant_test

import (
	"errors"
	"testing"
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
)

func mustSet(t *testing.T, periods ...instant.Period) instant.Set {
	t.Helper()

	set, err := instant.NewSet(temporal.Limits{}, periods...)
	if err != nil {
		t.Fatalf("NewSet(): %v", err)
	}

	return set
}

func TestSetNormalizesOrderOverlapAndContiguousAdjacency(t *testing.T) {
	t.Parallel()

	set := mustSet(t,
		mustPeriod(t, 2, 3, temporal.ClosedOpen),
		mustPeriod(t, 0, 1, temporal.ClosedOpen),
		mustPeriod(t, 1, 2, temporal.ClosedOpen),
		mustPeriod(t, 1, 1, temporal.Open),
	)

	if set.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", set.Len())
	}
	if !set.Periods()[0].SetEqual(mustPeriod(t, 0, 3, temporal.ClosedOpen)) {
		t.Fatalf("normalized period = %+v", set.Periods()[0])
	}
}

func TestSetPreservesARealExcludedBoundaryGap(t *testing.T) {
	t.Parallel()

	set := mustSet(t,
		mustPeriod(t, 0, 1, temporal.Open),
		mustPeriod(t, 1, 2, temporal.Open),
	)
	if set.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", set.Len())
	}

	gaps := set.Gaps()
	if len(gaps) != 1 || !gaps[0].IsSingleton() || !gaps[0].Includes(at(1)) {
		t.Fatalf("Gaps() = %+v", gaps)
	}
}

func TestSetCopiesCallerAndReturnedSlices(t *testing.T) {
	t.Parallel()

	input := []instant.Period{mustPeriod(t, 0, 1, temporal.ClosedOpen)}
	set := mustSet(t, input...)
	input[0] = mustPeriod(t, 4, 5, temporal.ClosedOpen)

	returned := set.Periods()
	returned[0] = input[0]
	if !set.Periods()[0].Start().Equal(at(0)) {
		t.Fatal("caller mutation changed the immutable set")
	}
}

func TestSetCanonicalizesEqualStartsAndEnds(t *testing.T) {
	t.Parallel()

	set := mustSet(t,
		mustPeriod(t, 0, 2, temporal.Open),
		mustPeriod(t, 0, 1, temporal.ClosedOpen),
		mustPeriod(t, 0, 3, temporal.ClosedOpen),
		mustPeriod(t, 0, 2, temporal.OpenClosed),
	)
	if set.Len() != 1 || !set.Periods()[0].SetEqual(mustPeriod(t, 0, 3, temporal.ClosedOpen)) {
		t.Fatalf("canonicalized set = %+v", set.Periods())
	}
}

func TestSetEnforcesInputAndOutputLimits(t *testing.T) {
	t.Parallel()

	periods := []instant.Period{
		mustPeriod(t, 0, 1, temporal.Open),
		mustPeriod(t, 2, 3, temporal.Open),
	}
	if _, err := instant.NewSet(temporal.Limits{InputPeriods: 1}, periods...); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("NewSet(input limit) error = %v", err)
	}
	if _, err := instant.NewSet(temporal.Limits{OutputPeriods: 1}, periods...); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("NewSet(output limit) error = %v", err)
	}
	if _, err := instant.NewSet(temporal.Limits{Precision: 10}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("NewSet(invalid limits) error = %v", err)
	}
}

func TestSetSpanDurationAndContainment(t *testing.T) {
	t.Parallel()

	set := mustSet(t,
		mustPeriod(t, 0, 1, temporal.ClosedOpen),
		mustPeriod(t, 2, 4, temporal.ClosedOpen),
	)
	span, ok := set.Span()
	if !ok || !span.SetEqual(mustPeriod(t, 0, 4, temporal.ClosedOpen)) {
		t.Fatalf("Span() = %+v, %v", span, ok)
	}
	duration, err := set.TotalDuration()
	if err != nil || duration != 3*time.Hour {
		t.Fatalf("TotalDuration() = %v, %v", duration, err)
	}
	if !set.Includes(at(0)) || set.Includes(at(1)) || !set.Includes(at(3)) {
		t.Fatal("Includes() did not search normalized periods")
	}

	empty := mustSet(t)
	if _, ok := empty.Span(); ok {
		t.Fatal("empty set returned a span")
	}
	if gaps := empty.Gaps(); gaps != nil {
		t.Fatalf("empty Gaps() = %+v, want nil", gaps)
	}
}

func TestSetTotalDurationDetectsPeriodAndSumOverflow(t *testing.T) {
	t.Parallel()

	wide, err := instant.New(
		time.Date(1000, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
		temporal.ClosedOpen,
	)
	if err != nil {
		t.Fatalf("New(wide): %v", err)
	}
	if _, err := mustSet(t, wide).TotalDuration(); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("wide TotalDuration() error = %v", err)
	}

	first, _ := instant.New(
		time.Date(1000, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(1200, time.January, 1, 0, 0, 0, 0, time.UTC),
		temporal.ClosedOpen,
	)
	second, _ := instant.New(
		time.Date(1300, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(1500, time.January, 1, 0, 0, 0, 0, time.UTC),
		temporal.ClosedOpen,
	)
	if _, err := mustSet(t, first, second).TotalDuration(); !errors.Is(err, temporal.ErrOverflow) {
		t.Fatalf("summed TotalDuration() error = %v", err)
	}
}

func TestSetUnionIsNormalizedCommutativeAndIdempotent(t *testing.T) {
	t.Parallel()

	a := mustSet(t, mustPeriod(t, 0, 2, temporal.ClosedOpen))
	b := mustSet(t, mustPeriod(t, 1, 3, temporal.ClosedOpen))

	ab, err := a.Union(b)
	if err != nil {
		t.Fatalf("Union(): %v", err)
	}
	ba, err := b.Union(a)
	if err != nil {
		t.Fatalf("reverse Union(): %v", err)
	}
	aa, err := a.Union(a)
	if err != nil {
		t.Fatalf("idempotent Union(): %v", err)
	}

	if !ab.Equal(ba) || !aa.Equal(a) || !ab.Periods()[0].SetEqual(mustPeriod(t, 0, 3, temporal.ClosedOpen)) {
		t.Fatal("union algebra or normalization failed")
	}
}

func TestSetEqualityRejectsDifferentLengthsAndMembers(t *testing.T) {
	t.Parallel()

	a := mustSet(t, mustPeriod(t, 0, 1, temporal.ClosedOpen))
	b := mustSet(t)
	c := mustSet(t, mustPeriod(t, 2, 3, temporal.ClosedOpen))
	if a.Equal(b) || a.Equal(c) {
		t.Fatal("different normalized sets compared equal")
	}
}

func TestSetIntersectionIsCommutative(t *testing.T) {
	t.Parallel()

	a := mustSet(t,
		mustPeriod(t, 0, 2, temporal.Closed),
		mustPeriod(t, 4, 6, temporal.ClosedOpen),
	)
	b := mustSet(t, mustPeriod(t, 1, 5, temporal.Open))

	ab, err := a.Intersect(b)
	if err != nil {
		t.Fatalf("Intersect(): %v", err)
	}
	ba, err := b.Intersect(a)
	if err != nil {
		t.Fatalf("reverse Intersect(): %v", err)
	}
	if !ab.Equal(ba) || ab.Len() != 2 {
		t.Fatalf("intersection = %+v / %+v", ab.Periods(), ba.Periods())
	}
}

func TestSetIntersectionAdvancesEqualEndsAndEnforcesOutputLimit(t *testing.T) {
	t.Parallel()

	a := mustSet(t, mustPeriod(t, 0, 2, temporal.Closed))
	b := mustSet(t, mustPeriod(t, 1, 2, temporal.Closed))
	if result, err := a.Intersect(b); err != nil || result.Len() != 1 {
		t.Fatalf("equal-end Intersect() = %+v, %v", result.Periods(), err)
	}

	limited, err := instant.NewSet(
		temporal.Limits{OutputPeriods: 1},
		mustPeriod(t, 0, 5, temporal.Closed),
	)
	if err != nil {
		t.Fatalf("NewSet(limited): %v", err)
	}
	disjoint := mustSet(t,
		mustPeriod(t, 0, 1, temporal.Closed),
		mustPeriod(t, 3, 4, temporal.Closed),
	)
	if _, err := limited.Intersect(disjoint); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Intersect(output limit) error = %v", err)
	}
}

func TestSetDifferenceConservesExpectedMembers(t *testing.T) {
	t.Parallel()

	set := mustSet(t, mustPeriod(t, 0, 5, temporal.Closed))
	removed := mustSet(t,
		mustPeriod(t, 1, 2, temporal.Open),
		mustPeriod(t, 3, 4, temporal.Closed),
	)

	difference, err := set.Subtract(removed)
	if err != nil {
		t.Fatalf("Subtract(): %v", err)
	}
	if difference.Len() != 3 {
		t.Fatalf("Subtract().Len() = %d, want 3", difference.Len())
	}

	for hour, included := range map[int]bool{0: true, 1: true, 2: true, 3: false, 4: false, 5: true} {
		if got := difference.Includes(at(hour)); got != included {
			t.Fatalf("Includes(%d) = %v, want %v", hour, got, included)
		}
	}
}

func TestSetDifferenceEnforcesFragmentLimit(t *testing.T) {
	t.Parallel()

	limited, err := instant.NewSet(
		temporal.Limits{OutputPeriods: 1},
		mustPeriod(t, 0, 2, temporal.Closed),
	)
	if err != nil {
		t.Fatalf("NewSet(limited): %v", err)
	}
	removed := mustSet(t, mustPeriod(t, 1, 1, temporal.Closed))
	if _, err := limited.Subtract(removed); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Subtract(output limit) error = %v", err)
	}
}
