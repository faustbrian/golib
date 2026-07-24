package timeofday

import (
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	calendartz "github.com/faustbrian/golib/pkg/calendar/timezone"
	temporal "github.com/faustbrian/golib/pkg/temporal"
)

// Apply resolves t on date in location according to an explicit DST policy.
// The distinct 24:00 value resolves as midnight at the start of the next date.
func (t Time) Apply(date calendar.Date, location *time.Location, policy calendartz.Resolution) (time.Time, error) {
	if !date.IsValid() {
		return time.Time{}, calendar.ErrInvalidDate
	}
	if t.IsEndBoundary() {
		var err error
		date, err = date.AddDays(1)
		if err != nil {
			return time.Time{}, err
		}
	}
	hour, minute, second, nanosecond := t.Components()
	if t.IsEndBoundary() {
		hour = 0
	}
	local, err := calendartz.NewLocalDateTime(date, hour, minute, second, nanosecond)
	if err != nil {
		return time.Time{}, err
	}
	return calendartz.Resolve(local, location, policy)
}

// FromInstant returns the civil date and local time observed in location.
// Digits explicitly selects zero through nine fractional-second digits; an
// instant that cannot be represented exactly at that precision is rejected.
func FromInstant(value time.Time, location *time.Location, digits int) (Time, calendar.Date, error) {
	if digits < 0 || digits > 9 {
		return Time{}, calendar.Date{}, temporal.ErrPrecision
	}
	local, err := calendartz.FromInstant(value, location)
	if err != nil {
		return Time{}, calendar.Date{}, err
	}
	if local.Nanosecond()%precisionUnit(digits) != 0 {
		return Time{}, calendar.Date{}, temporal.ErrPrecision
	}
	result, _ := New(local.Hour(), local.Minute(), local.Second(), local.Nanosecond(), digits)
	return result, local.Date(), nil
}
