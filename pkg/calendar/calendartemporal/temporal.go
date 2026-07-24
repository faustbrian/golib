// Package calendartemporal converts civil boundaries into values suitable for
// temporal without adding interval algebra to calendar core.
package calendartemporal

import (
	"errors"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	calendartz "github.com/faustbrian/golib/pkg/calendar/timezone"
)

var (
	// ErrReversed identifies civil endpoints in descending order.
	ErrReversed = errors.New("calendar/temporal: reversed date range")
	// ErrRangeLimit identifies a date sequence exceeding its explicit bound.
	ErrRangeLimit = errors.New("calendar/temporal: sequence limit exceeded")
)

// InstantRange is a canonical start-inclusive, end-exclusive adapter value.
type InstantRange struct{ start, end time.Time }

// Start returns the inclusive instant boundary.
func (r InstantRange) Start() time.Time { return r.start }

// End returns the exclusive instant boundary.
func (r InstantRange) End() time.Time { return r.end }

// Includes reports membership using [start,end) semantics.
func (r InstantRange) Includes(value time.Time) bool {
	return !value.Before(r.start) && value.Before(r.end)
}

// InclusiveDates converts inclusive civil endpoints to an exclusive instant
// range. Its boundaries can be passed directly to temporal/instant.Range.
func InclusiveDates(first, last calendar.Date, location *time.Location, policy calendartz.Resolution) (InstantRange, error) {
	comparison, err := first.Compare(last)
	if err != nil {
		return InstantRange{}, err
	}
	if comparison > 0 {
		return InstantRange{}, ErrReversed
	}
	afterLast, err := last.AddDays(1)
	if err != nil {
		return InstantRange{}, err
	}
	start, err := calendartz.Resolve(calendartz.MustLocalDateTime(first, 0, 0, 0, 0), location, policy)
	if err != nil {
		return InstantRange{}, err
	}
	end, err := calendartz.Resolve(calendartz.MustLocalDateTime(afterLast, 0, 0, 0, 0), location, policy)
	if err != nil {
		return InstantRange{}, err
	}
	return InstantRange{start: start, end: end}, nil
}

// Sequence returns every date in the inclusive range with explicit work bound.
func Sequence(first, last calendar.Date, limit int) ([]calendar.Date, error) {
	comparison, err := first.Compare(last)
	if err != nil {
		return nil, err
	}
	if comparison > 0 {
		return nil, ErrReversed
	}
	length := first.DaysUntil(last) + 1
	if limit <= 0 || length > limit {
		return nil, ErrRangeLimit
	}
	result := make([]calendar.Date, length)
	current := first
	for i := range length {
		result[i] = current
		current, _ = current.AddDays(1)
	}
	return result, nil
}
