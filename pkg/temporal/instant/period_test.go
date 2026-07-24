package instant_test

import (
	"errors"
	"testing"
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
)

var epoch = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

func at(hour int) time.Time {
	return epoch.Add(time.Duration(hour) * time.Hour)
}

func mustPeriod(t *testing.T, start, end int, bounds temporal.Bounds) instant.Period {
	t.Helper()

	period, err := instant.New(at(start), at(end), bounds)
	if err != nil {
		t.Fatalf("New(%d, %d, %v): %v", start, end, bounds, err)
	}

	return period
}

func TestNewRejectsReversedAndInvalidBounds(t *testing.T) {
	t.Parallel()

	if _, err := instant.New(at(2), at(1), temporal.ClosedOpen); !errors.Is(err, temporal.ErrReversed) {
		t.Fatalf("New(reversed) error = %v, want ErrReversed", err)
	}

	if _, err := instant.New(at(1), at(2), temporal.Bounds(255)); !errors.Is(err, temporal.ErrBounds) {
		t.Fatalf("New(invalid bounds) error = %v, want ErrBounds", err)
	}
}

func TestEqualEndpointsHaveExplicitEmptyAndSingletonSemantics(t *testing.T) {
	t.Parallel()

	for _, bounds := range temporal.AllBounds() {
		period := mustPeriod(t, 1, 1, bounds)
		wantEmpty := bounds != temporal.Closed

		if got := period.IsEmpty(); got != wantEmpty {
			t.Fatalf("New(equal, %v).IsEmpty() = %v, want %v", bounds, got, wantEmpty)
		}
		if got := period.IsSingleton(); got != !wantEmpty {
			t.Fatalf("New(equal, %v).IsSingleton() = %v, want %v", bounds, got, !wantEmpty)
		}
	}
}

func TestDefaultRangeIsHalfOpen(t *testing.T) {
	t.Parallel()

	period, err := instant.Range(at(1), at(2))
	if err != nil {
		t.Fatalf("Range(): %v", err)
	}

	if period.Bounds() != temporal.ClosedOpen {
		t.Fatalf("Bounds() = %v, want ClosedOpen", period.Bounds())
	}
	if !period.End().Equal(at(2)) {
		t.Fatalf("End() = %v, want %v", period.End(), at(2))
	}
	if !period.Includes(at(1)) || period.Includes(at(2)) {
		t.Fatal("half-open range did not include only its start boundary")
	}
}

func TestIncludesRespectsRangeAndEveryBoundary(t *testing.T) {
	t.Parallel()

	closed := mustPeriod(t, 1, 2, temporal.Closed)
	open := mustPeriod(t, 1, 2, temporal.Open)
	empty := mustPeriod(t, 1, 1, temporal.Open)

	if !closed.Includes(at(2)) {
		t.Fatal("closed period excluded its end")
	}
	if open.Includes(at(1)) || open.Includes(at(2)) {
		t.Fatal("open period included a boundary")
	}
	if !open.Includes(at(1).Add(time.Minute)) {
		t.Fatal("open period excluded an interior instant")
	}
	if closed.Includes(at(0)) || closed.Includes(at(3)) || empty.Includes(at(1)) {
		t.Fatal("period included an instant outside its represented set")
	}
}

func TestPeriodStripsMonotonicReadingWithoutChangingInstantOrLocation(t *testing.T) {
	t.Parallel()

	start := time.Now()
	period, err := instant.Range(start, start.Add(time.Second))
	if err != nil {
		t.Fatalf("Range(): %v", err)
	}

	if !period.Start().Equal(start) || period.Start().Location() != start.Location() {
		t.Fatal("Start() changed the represented instant or location")
	}
	//nolint:staticcheck // Structural equality is required to detect monotonic metadata.
	if period.Start() == start {
		t.Fatal("Start() retained the process-local monotonic reading")
	}
}

func TestAllenRelationsAcrossAllBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		aStart   int
		aEnd     int
		bStart   int
		bEnd     int
		relation temporal.Relation
	}{
		{"before", 0, 1, 2, 3, temporal.Before},
		{"meets", 0, 1, 1, 2, temporal.Meets},
		{"overlaps", 0, 2, 1, 3, temporal.Overlaps},
		{"starts", 0, 1, 0, 2, temporal.Starts},
		{"during", 1, 2, 0, 3, temporal.During},
		{"finishes", 1, 3, 0, 3, temporal.Finishes},
		{"equals", 0, 3, 0, 3, temporal.Equal},
		{"finished by", 0, 3, 1, 3, temporal.FinishedBy},
		{"contains", 0, 3, 1, 2, temporal.Contains},
		{"started by", 0, 2, 0, 1, temporal.StartedBy},
		{"overlapped by", 1, 3, 0, 2, temporal.OverlappedBy},
		{"met by", 1, 2, 0, 1, temporal.MetBy},
		{"after", 2, 3, 0, 1, temporal.After},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			for _, aBounds := range temporal.AllBounds() {
				for _, bBounds := range temporal.AllBounds() {
					a := mustPeriod(t, test.aStart, test.aEnd, aBounds)
					b := mustPeriod(t, test.bStart, test.bEnd, bBounds)

					got, err := a.RelationTo(b)
					if err != nil {
						t.Fatalf("RelationTo(%v, %v): %v", aBounds, bBounds, err)
					}
					if got != test.relation {
						t.Fatalf("RelationTo(%v, %v) = %v, want %v", aBounds, bBounds, got, test.relation)
					}

					inverse, err := b.RelationTo(a)
					if err != nil || inverse != test.relation.Converse() {
						t.Fatalf("inverse = %v, %v; want %v", inverse, err, test.relation.Converse())
					}
				}
			}
		})
	}
}

func TestRelationRejectsEmptyIntervals(t *testing.T) {
	t.Parallel()

	empty := mustPeriod(t, 1, 1, temporal.ClosedOpen)
	other := mustPeriod(t, 0, 2, temporal.ClosedOpen)
	if _, err := empty.RelationTo(other); !errors.Is(err, temporal.ErrEmpty) {
		t.Fatalf("RelationTo() error = %v, want ErrEmpty", err)
	}
	if _, err := other.RelationTo(empty); !errors.Is(err, temporal.ErrEmpty) {
		t.Fatalf("RelationTo(empty) error = %v, want ErrEmpty", err)
	}
}

func TestBoundaryAdjacencyDistinguishesMeetingAndBordering(t *testing.T) {
	t.Parallel()

	leftClosed := mustPeriod(t, 0, 1, temporal.Closed)
	rightClosed := mustPeriod(t, 1, 2, temporal.Closed)
	if !leftClosed.Abuts(rightClosed) || !leftClosed.Borders(rightClosed) || leftClosed.Meets(rightClosed) {
		t.Fatal("closed adjacency was not classified as bordering")
	}

	leftOpen := mustPeriod(t, 0, 1, temporal.ClosedOpen)
	rightOpen := mustPeriod(t, 1, 2, temporal.ClosedOpen)
	if !leftOpen.Abuts(rightOpen) || leftOpen.Borders(rightOpen) || !leftOpen.Meets(rightOpen) {
		t.Fatal("half-open adjacency was not classified as meeting")
	}
	if !rightClosed.Borders(leftClosed) {
		t.Fatal("bordering was not symmetric")
	}
	if leftClosed.Borders(mustPeriod(t, 3, 4, temporal.Closed)) {
		t.Fatal("disjoint periods were classified as bordering")
	}
}
