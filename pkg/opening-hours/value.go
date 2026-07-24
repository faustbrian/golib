package openinghours

import (
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
)

const (
	nanosecondsPerDay = int64(24 * time.Hour)
	maxRangesPerDay   = 64
)

// LocalTime is a nanosecond-precision wall-clock time without a date or zone.
// Its zero value is midnight.
type LocalTime struct {
	nanosecond int64
}

// NewLocalTime constructs a wall-clock time in the half-open day [00:00,24:00).
func NewLocalTime(hour, minute, second, nanosecond int) (LocalTime, error) {
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 ||
		second < 0 || second > 59 || nanosecond < 0 || nanosecond >= int(time.Second) {
		return LocalTime{}, newError("new local time", CodeInvalidTime)
	}

	value := time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute +
		time.Duration(second)*time.Second + time.Duration(nanosecond)

	return LocalTime{nanosecond: int64(value)}, nil
}

// Hour returns the hour component.
func (t LocalTime) Hour() int { return int(time.Duration(t.nanosecond) / time.Hour) }

// Minute returns the minute component.
func (t LocalTime) Minute() int {
	return int((time.Duration(t.nanosecond) % time.Hour) / time.Minute)
}

// Second returns the second component.
func (t LocalTime) Second() int {
	return int((time.Duration(t.nanosecond) % time.Minute) / time.Second)
}

// Nanosecond returns the fractional second component.
func (t LocalTime) Nanosecond() int { return int(t.nanosecond % int64(time.Second)) }

// Range is a start-inclusive, end-exclusive local-time interval. If End is
// earlier than Start, the interval is overnight and belongs to its start date.
type Range struct {
	start LocalTime
	end   LocalTime
}

// NewRange creates a non-empty local-time range.
func NewRange(start, end LocalTime) (Range, error) {
	if !start.valid() || !end.valid() || start == end {
		return Range{}, newError("new range", CodeInvalidRange)
	}

	return Range{start: start, end: end}, nil
}

// Start returns the inclusive endpoint.
func (r Range) Start() LocalTime { return r.start }

// End returns the exclusive endpoint.
func (r Range) End() LocalTime { return r.end }

// Overnight reports whether the range ends on the following civil date.
func (r Range) Overnight() bool { return r.end.nanosecond < r.start.nanosecond }

func (t LocalTime) valid() bool { return t.nanosecond >= 0 && t.nanosecond < nanosecondsPerDay }

// Date is the calendar immutable Gregorian civil date value.
type Date = calendar.Date

// NewDate validates and constructs a Gregorian civil date.
func NewDate(year int, month time.Month, day int) (Date, error) {
	date, err := calendar.NewDate(year, month, day)
	if err != nil {
		return Date{}, newError("new date", CodeInvalidDate)
	}

	return date, nil
}

// MustDate returns a valid date or panics. It is intended for static fixtures.
func MustDate(year int, month time.Month, day int) Date {
	date, err := NewDate(year, month, day)
	if err != nil {
		panic(err)
	}

	return date
}

func compareDate(left, right Date) int {
	comparison, _ := left.Compare(right)

	return comparison
}

func addDate(date Date, days int) (Date, error) {
	result, err := date.AddDays(days)
	if err != nil {
		return Date{}, newError("add date", CodeInvalidDate)
	}

	return result, nil
}

func validDate(date Date) bool { return date.IsValid() }
