package calendar

import "time"

// StartOfISOWeek returns Monday of d's ISO week.
func (d Date) StartOfISOWeek() Date {
	if !d.IsValid() {
		return Date{}
	}
	offset := (int(d.Weekday()) + 6) % 7
	start, _ := d.AddDays(-offset)
	return start
}

// EndOfISOWeek returns Sunday of d's ISO week.
func (d Date) EndOfISOWeek() Date {
	end, _ := d.StartOfISOWeek().AddDays(6)
	return end
}

// StartOfMonth returns the first date in d's month.
func (d Date) StartOfMonth() Date {
	if !d.IsValid() {
		return Date{}
	}
	return MustDate(d.Year(), d.Month(), 1)
}

// EndOfMonth returns the last date in d's month.
func (d Date) EndOfMonth() Date {
	if !d.IsValid() {
		return Date{}
	}
	return MustDate(d.Year(), d.Month(), d.DaysInMonth())
}

// StartOfQuarter returns the first date in d's Gregorian quarter.
func (d Date) StartOfQuarter() Date {
	if !d.IsValid() {
		return Date{}
	}
	return MustDate(d.Year(), time.Month((int(d.Month())-1)/3*3+1), 1)
}

// EndOfQuarter returns the last date in d's Gregorian quarter.
func (d Date) EndOfQuarter() Date {
	if !d.IsValid() {
		return Date{}
	}
	month := time.Month(((int(d.Month())-1)/3 + 1) * 3)
	return MustDate(d.Year(), month, daysInMonth(d.Year(), month))
}

// StartOfSemester returns the first date in d's Gregorian half-year.
func (d Date) StartOfSemester() Date {
	if !d.IsValid() {
		return Date{}
	}
	return MustDate(d.Year(), time.Month((int(d.Month())-1)/6*6+1), 1)
}

// EndOfSemester returns the last date in d's Gregorian half-year.
func (d Date) EndOfSemester() Date {
	if !d.IsValid() {
		return Date{}
	}
	month := time.Month(((int(d.Month())-1)/6 + 1) * 6)
	return MustDate(d.Year(), month, daysInMonth(d.Year(), month))
}

// StartOfYear returns January 1 of d's year.
func (d Date) StartOfYear() Date {
	if !d.IsValid() {
		return Date{}
	}
	return MustDate(d.Year(), time.January, 1)
}

// EndOfYear returns December 31 of d's year.
func (d Date) EndOfYear() Date {
	if !d.IsValid() {
		return Date{}
	}
	return MustDate(d.Year(), time.December, 31)
}

// StartOfISOYear returns Monday of week 1 in d's ISO week-year.
func (d Date) StartOfISOYear() Date {
	year, _ := d.ISOWeek()
	w, err := NewISOWeek(year, 1)
	if err != nil {
		return Date{}
	}
	return w.FirstDate()
}

// EndOfISOYear returns Sunday of the last week in d's ISO week-year.
func (d Date) EndOfISOYear() Date {
	year, _ := d.ISOWeek()
	w, err := NewISOWeek(year, isoWeeksInYear(year))
	if err != nil {
		return Date{}
	}
	return w.LastDate()
}
