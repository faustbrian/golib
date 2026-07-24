package dateperiod

import (
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	temporal "github.com/faustbrian/golib/pkg/temporal"
)

// Day constructs a one-day closed period.
func Day(date calendar.Date) (Period, error) {
	return New(date, date, temporal.Closed)
}

// Month constructs a closed Gregorian calendar month.
func Month(year int, month time.Month) (Period, error) {
	unit, err := calendar.NewYearMonth(year, month)
	if err != nil {
		return Period{}, err
	}
	return New(unit.FirstDate(), unit.LastDate(), temporal.Closed)
}

// Quarter constructs a closed Gregorian quarter.
func Quarter(year, quarter int) (Period, error) {
	unit, err := calendar.NewQuarter(year, quarter)
	if err != nil {
		return Period{}, err
	}
	return New(unit.FirstDate(), unit.LastDate(), temporal.Closed)
}

// Semester constructs a closed Gregorian half-year.
func Semester(year, semester int) (Period, error) {
	unit, err := calendar.NewSemester(year, semester)
	if err != nil {
		return Period{}, err
	}
	return New(unit.FirstDate(), unit.LastDate(), temporal.Closed)
}

// Year constructs a closed Gregorian year.
func Year(year int) (Period, error) {
	unit, err := calendar.NewYear(year)
	if err != nil {
		return Period{}, err
	}
	return New(unit.FirstDate(), unit.LastDate(), temporal.Closed)
}

// ISOWeek constructs a closed ISO week.
func ISOWeek(year, week int) (Period, error) {
	unit, err := calendar.NewISOWeek(year, week)
	if err != nil {
		return Period{}, err
	}
	return New(unit.FirstDate(), unit.LastDate(), temporal.Closed)
}

// ISOYear constructs a closed ISO week-numbering year.
func ISOYear(year int) (Period, error) {
	anchor, err := calendar.NewDate(year, time.January, 4)
	if err != nil {
		return Period{}, err
	}
	return New(anchor.StartOfISOYear(), anchor.EndOfISOYear(), temporal.Closed)
}
