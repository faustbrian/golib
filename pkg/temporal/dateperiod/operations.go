package dateperiod

import (
	calendar "github.com/faustbrian/golib/pkg/calendar"
	temporal "github.com/faustbrian/golib/pkg/temporal"
)

// WithStart returns a period with its civil start replaced.
func (p Period) WithStart(start calendar.Date) (Period, error) {
	return New(start, p.end, p.bounds)
}

// WithEnd returns a period with its civil end replaced.
func (p Period) WithEnd(end calendar.Date) (Period, error) {
	return New(p.start, end, p.bounds)
}

// WithBounds returns a period with its endpoint inclusion replaced.
func (p Period) WithBounds(bounds temporal.Bounds) (Period, error) {
	return New(p.start, p.end, bounds)
}

// MoveWeeks shifts both endpoints by exact civil weeks.
func (p Period) MoveWeeks(weeks int) (Period, error) {
	return p.move(func(date calendar.Date) (calendar.Date, error) { return date.AddWeeks(weeks) })
}

// MoveQuarters shifts both endpoints using explicit invalid-day arithmetic.
func (p Period) MoveQuarters(quarters int, policy calendar.ArithmeticPolicy) (Period, error) {
	return p.move(func(date calendar.Date) (calendar.Date, error) {
		return date.AddQuarters(quarters, policy)
	})
}

// MoveSemesters shifts both endpoints using explicit invalid-day arithmetic.
func (p Period) MoveSemesters(semesters int, policy calendar.ArithmeticPolicy) (Period, error) {
	return p.move(func(date calendar.Date) (calendar.Date, error) {
		return date.AddSemesters(semesters, policy)
	})
}

// MoveYears shifts both endpoints using explicit leap-day arithmetic.
func (p Period) MoveYears(years int, policy calendar.ArithmeticPolicy) (Period, error) {
	return p.move(func(date calendar.Date) (calendar.Date, error) {
		return date.AddYears(years, policy)
	})
}

// ExpandDays moves the start backward and end forward by civil days. Negative
// values shrink and are rejected if they reverse the endpoints.
func (p Period) ExpandDays(days int) (Period, error) {
	if days == -int(^uint(0)>>1)-1 {
		return Period{}, temporal.ErrOverflow
	}
	start, err := p.start.AddDays(-days)
	if err != nil {
		return Period{}, err
	}
	end, err := p.end.AddDays(days)
	if err != nil {
		return Period{}, err
	}
	return New(start, end, p.bounds)
}

// Equal reports structural endpoint and bound equality.
func (p Period) Equal(other Period) bool {
	return p.start == other.start && p.end == other.end && p.bounds == other.bounds
}

// Difference is the exact set difference and is an alias for Subtract.
func (p Period) Difference(other Period) []Period { return p.Subtract(other) }

// Union returns the normalized exact union.
func (p Period) Union(other Period, limits temporal.Limits) (Set, error) {
	return NewSet(limits, p, other)
}

// Merge returns the canonical closed convex hull of represented dates. Empty
// operands are ignored; two empty operands return the invalid zero value.
func (p Period) Merge(other Period) Period {
	p, pOK := p.canonical()
	other, otherOK := other.canonical()
	if !pOK {
		return other
	}
	if !otherOK {
		return p
	}
	start := p.start
	if compareDate(other.start, start) < 0 {
		start = other.start
	}
	end := p.end
	if compareDate(other.end, end) > 0 {
		end = other.end
	}
	return Period{start: start, end: end, bounds: temporal.Closed}
}

// Gap returns the canonical closed dates strictly between represented sets.
func (p Period) Gap(other Period) (Period, bool) {
	p, pOK := p.canonical()
	other, otherOK := other.canonical()
	if !pOK || !otherOK {
		return Period{}, false
	}
	if _, ok := p.intersect(other); ok {
		return Period{}, false
	}
	left, right := p, other
	if compareDate(left.start, right.start) > 0 {
		left, right = right, left
	}
	start, err := left.end.AddDays(1)
	if err != nil || compareDate(start, right.start) >= 0 {
		return Period{}, false
	}
	end, _ := right.start.AddDays(-1) // disjoint ordering proves no underflow
	return Period{start: start, end: end, bounds: temporal.Closed}, true
}

// IsBefore reports the formal Allen before relation.
func (p Period) IsBefore(other Period) bool { return p.hasRelation(other, temporal.Before) }

// IsAfter reports the formal Allen after relation.
func (p Period) IsAfter(other Period) bool { return p.hasRelation(other, temporal.After) }

// Starts reports the formal Allen starts relation.
func (p Period) Starts(other Period) bool { return p.hasRelation(other, temporal.Starts) }

// Finishes reports the formal Allen finishes relation.
func (p Period) Finishes(other Period) bool { return p.hasRelation(other, temporal.Finishes) }

// Overlaps reports whether the represented sets share a civil date.
func (p Period) Overlaps(other Period) bool {
	_, ok := p.Intersect(other)
	return ok
}

// Contains reports whether every represented date of other belongs to p.
func (p Period) Contains(other Period) bool {
	if other.IsEmpty() {
		return true
	}
	intersection, ok := p.Intersect(other)
	return ok && intersection.SetEqual(other)
}

// During reports whether every represented date of p belongs to other.
func (p Period) During(other Period) bool { return other.Contains(p) }

func (p Period) move(operation func(calendar.Date) (calendar.Date, error)) (Period, error) {
	start, err := operation(p.start)
	if err != nil {
		return Period{}, err
	}
	end, err := operation(p.end)
	if err != nil {
		return Period{}, err
	}
	return New(start, end, p.bounds)
}

func (p Period) hasRelation(other Period, want temporal.Relation) bool {
	relation, err := p.RelationTo(other)
	return err == nil && relation == want
}
