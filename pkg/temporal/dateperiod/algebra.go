package dateperiod

import (
	calendar "github.com/faustbrian/golib/pkg/calendar"
	temporal "github.com/faustbrian/golib/pkg/temporal"
)

// RelationTo returns the unique Allen endpoint relation to other. Empty date
// periods do not have an Allen relation.
func (p Period) RelationTo(other Period) (temporal.Relation, error) {
	if p.IsEmpty() || other.IsEmpty() {
		return temporal.RelationInvalid, temporal.ErrEmpty
	}

	if compareDate(p.end, other.start) < 0 {
		return temporal.Before, nil
	}
	if compareDate(p.end, other.start) == 0 {
		return temporal.Meets, nil
	}
	if compareDate(p.start, other.end) > 0 {
		return temporal.After, nil
	}
	if compareDate(p.start, other.end) == 0 {
		return temporal.MetBy, nil
	}

	startComparison := compareDate(p.start, other.start)
	endComparison := compareDate(p.end, other.end)
	switch {
	case startComparison < 0 && endComparison < 0:
		return temporal.Overlaps, nil
	case startComparison < 0 && endComparison == 0:
		return temporal.FinishedBy, nil
	case startComparison < 0 && endComparison > 0:
		return temporal.Contains, nil
	case startComparison == 0 && endComparison < 0:
		return temporal.Starts, nil
	case startComparison == 0 && endComparison == 0:
		return temporal.Equal, nil
	case startComparison == 0 && endComparison > 0:
		return temporal.StartedBy, nil
	case startComparison > 0 && endComparison < 0:
		return temporal.During, nil
	case startComparison > 0 && endComparison == 0:
		return temporal.Finishes, nil
	default:
		return temporal.OverlappedBy, nil
	}
}

// SetEqual reports equality of represented civil dates.
func (p Period) SetEqual(other Period) bool {
	if p.IsEmpty() || other.IsEmpty() {
		return p.IsEmpty() && other.IsEmpty()
	}
	pStart, pEnd, _ := p.includedRange()
	oStart, oEnd, _ := other.includedRange()
	return pStart.Equal(oStart) && pEnd.Equal(oEnd)
}

// Intersect returns the exact non-empty intersection as a canonical closed
// included-date range.
func (p Period) Intersect(other Period) (Period, bool) {
	return p.intersect(other)
}

// Subtract returns canonical closed fragments of p not represented by other.
func (p Period) Subtract(other Period) []Period {
	return p.subtract(other)
}

func (p Period) canonical() (Period, bool) {
	start, end, ok := p.includedRange()
	if !ok {
		return Period{}, false
	}
	return Period{start: start, end: end, bounds: temporal.Closed}, true
}

func (p Period) intersect(other Period) (Period, bool) {
	p, pOK := p.canonical()
	other, otherOK := other.canonical()
	if !pOK || !otherOK {
		return Period{}, false
	}
	start := p.start
	if compareDate(other.start, start) > 0 {
		start = other.start
	}
	end := p.end
	if compareDate(other.end, end) < 0 {
		end = other.end
	}
	if compareDate(start, end) > 0 {
		return Period{}, false
	}
	return Period{start: start, end: end, bounds: temporal.Closed}, true
}

func (p Period) subtract(other Period) []Period {
	p, ok := p.canonical()
	if !ok {
		return nil
	}
	intersection, ok := p.intersect(other)
	if !ok {
		return []Period{p}
	}
	if intersection.SetEqual(p) {
		return nil
	}

	result := make([]Period, 0, 2)
	if compareDate(p.start, intersection.start) < 0 {
		end, _ := intersection.start.AddDays(-1)
		result = append(result, Period{start: p.start, end: end, bounds: temporal.Closed})
	}
	if compareDate(intersection.end, p.end) < 0 {
		start, _ := intersection.end.AddDays(1)
		result = append(result, Period{start: start, end: p.end, bounds: temporal.Closed})
	}
	return result
}

func compareDate(left, right calendar.Date) int {
	comparison, _ := left.Compare(right)
	return comparison
}
