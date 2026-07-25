// Package openinghourscalendar adapts calendar civil dates and bounded
// business-calendar holidays to opening-hours values.
package openinghourscalendar

import (
	"errors"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	"github.com/faustbrian/golib/pkg/calendar/business"
	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

var (
	// ErrInvalidInput reports an invalid date, calendar, source, or range.
	ErrInvalidInput = errors.New("openinghourscalendar: invalid input")
	// ErrExpansionLimit reports a non-positive or exhausted date bound.
	ErrExpansionLimit = errors.New("openinghourscalendar: expansion limit")
)

// FromDate converts a valid calendar civil date without timezone inference.
func FromDate(date calendar.Date) (openinghours.Date, error) {
	if !date.IsValid() {
		return openinghours.Date{}, ErrInvalidInput
	}
	return openinghours.NewDate(date.Year(), date.Month(), date.Day())
}

// ToDate converts a valid opening-hours civil date without timezone inference.
func ToDate(date openinghours.Date) (calendar.Date, error) {
	converted, err := calendar.NewDate(date.Year(), date.Month(), date.Day())
	if err != nil {
		return calendar.Date{}, ErrInvalidInput
	}
	return converted, nil
}

// HolidayClosures resolves holidays in an inclusive, explicitly bounded civil
// date range to deterministic exact-date closure exceptions.
func HolidayClosures(businessCalendar business.Calendar, start, end calendar.Date,
	maximumDates, priority int, source string,
) ([]openinghours.Exception, error) {
	if !businessCalendar.IsValid() || !start.IsValid() || !end.IsValid() ||
		start.DaysUntil(end) < 0 || maximumDates <= 0 || source == "" {
		return nil, ErrInvalidInput
	}
	result := make([]openinghours.Exception, 0)
	date := start
	for step := 0; ; step++ {
		if step >= maximumDates {
			return nil, ErrExpansionLimit
		}
		if businessCalendar.IsHoliday(date) {
			converted, _ := FromDate(date)
			exception, err := openinghours.NewException(openinghours.ExceptionConfig{
				Date: converted, Operation: openinghours.ExceptionClose,
				Priority: priority, Source: source, Revision: businessCalendar.Revision(),
			})
			if err != nil {
				return nil, err
			}
			result = append(result, exception)
		}
		if date.Equal(end) {
			return result, nil
		}
		next, _ := date.AddDays(1)
		date = next
	}
}
