// Package dateperiod provides immutable bounded intervals over civil calendar
// dates. Calendar arithmetic is delegated to calendar.
package dateperiod

import (
	"fmt"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	calendartemporal "github.com/faustbrian/golib/pkg/calendar/calendartemporal"
	calendartz "github.com/faustbrian/golib/pkg/calendar/timezone"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
)

// Period is an immutable bounded interval over discrete civil dates.
type Period struct {
	start  calendar.Date
	end    calendar.Date
	bounds temporal.Bounds
}

// New constructs a bounded civil-date period.
func New(start, end calendar.Date, bounds temporal.Bounds) (Period, error) {
	if !bounds.Valid() {
		return Period{}, fmt.Errorf("%w: %d", temporal.ErrBounds, bounds)
	}
	comparison, err := start.Compare(end)
	if err != nil {
		return Period{}, err
	}
	if comparison > 0 {
		return Period{}, temporal.ErrReversed
	}

	return Period{start: start, end: end, bounds: bounds}, nil
}

// Start returns the declared civil start endpoint.
func (p Period) Start() calendar.Date {
	return p.start
}

// End returns the declared civil end endpoint.
func (p Period) End() calendar.Date {
	return p.end
}

// Bounds returns endpoint inclusion.
func (p Period) Bounds() temporal.Bounds {
	return p.bounds
}

// Days returns the number of represented civil dates.
func (p Period) Days() int {
	if !p.start.IsValid() || !p.end.IsValid() {
		return 0
	}
	days := p.start.DaysUntil(p.end) + 1
	if !p.bounds.IncludesStart() {
		days--
	}
	if !p.bounds.IncludesEnd() {
		days--
	}
	if days < 0 {
		return 0
	}

	return days
}

// IsEmpty reports whether no discrete civil date is represented.
func (p Period) IsEmpty() bool {
	return p.Days() == 0
}

// IsSingleton reports whether exactly one civil date is represented.
func (p Period) IsSingleton() bool {
	return p.Days() == 1
}

// Includes reports whether date is a represented member.
func (p Period) Includes(date calendar.Date) bool {
	if p.IsEmpty() || !date.IsValid() {
		return false
	}
	startComparison, _ := date.Compare(p.start)
	endComparison, _ := date.Compare(p.end)
	if startComparison < 0 || endComparison > 0 {
		return false
	}
	if startComparison == 0 && !p.bounds.IncludesStart() {
		return false
	}
	if endComparison == 0 && !p.bounds.IncludesEnd() {
		return false
	}

	return true
}

// MoveDays shifts both endpoints by calendar days.
func (p Period) MoveDays(days int) (Period, error) {
	start, err := p.start.AddDays(days)
	if err != nil {
		return Period{}, err
	}
	end, err := p.end.AddDays(days)
	if err != nil {
		return Period{}, err
	}

	return New(start, end, p.bounds)
}

// MoveMonths shifts both endpoints by calendar months using policy.
func (p Period) MoveMonths(months int, policy calendar.ArithmeticPolicy) (Period, error) {
	start, err := p.start.AddMonths(months, policy)
	if err != nil {
		return Period{}, err
	}
	end, err := p.end.AddMonths(months, policy)
	if err != nil {
		return Period{}, err
	}

	return New(start, end, p.bounds)
}

// ToInstant converts represented civil dates to a canonical next-boundary
// exclusive instant period using an explicit location and DST policy.
func (p Period) ToInstant(location *time.Location, policy calendartz.Resolution) (instant.Period, error) {
	first, last, ok := p.includedRange()
	if !ok {
		return instant.Period{}, temporal.ErrEmpty
	}
	rangeValue, err := calendartemporal.InclusiveDates(first, last, location, policy)
	if err != nil {
		return instant.Period{}, err
	}

	return instant.Range(rangeValue.Start(), rangeValue.End())
}

func (p Period) includedRange() (calendar.Date, calendar.Date, bool) {
	if p.IsEmpty() {
		return calendar.Date{}, calendar.Date{}, false
	}
	first := p.start
	last := p.end
	if !p.bounds.IncludesStart() {
		first, _ = first.AddDays(1)
	}
	if !p.bounds.IncludesEnd() {
		last, _ = last.AddDays(-1)
	}

	return first, last, true
}
